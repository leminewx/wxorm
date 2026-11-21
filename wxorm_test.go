// testEngine_mysql_test.go
package wxorm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"gitee.com/leminewx/wxorm/harness/examples"
	"gitee.com/leminewx/wxorm/naming"
	"gitee.com/leminewx/wxorm/session"
	"gitee.com/leminewx/wxorm/session/result"
	"gitee.com/leminewx/wxorm/statement/where"
)

// 全局测试变量
var testEngine *Engine

// setup 测试初始化
func setup() error {
	// 创建 MySQL 数据库引擎
	var err error
	testEngine, err = NewEngine("mysql", examples.DBSource)
	if err != nil {
		return err
	}
	testEngine.SetDebug(false)

	// 创建 groups 数据表
	if err = testEngine.Migrate(&examples.Group{}); err != nil {
		return err
	}

	// 预写入一些 groups 数据
	sess := testEngine.NewSession(&examples.Group{})
	if res, err := sess.INSERT(false,
		&examples.Group{Name: "group1"},
		&examples.Group{Name: "group2"},
		&examples.Group{Name: "group3"},
	); err != nil {
		return fmt.Errorf("failed to insert 'groups' table: %v", err)
	} else if rows, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("failed to get affected rows: %v", err)
	} else if rows != 3 {
		return fmt.Errorf("failed to get affected rows: %v", rows)
	}

	// 创建 users 数据表
	return testEngine.Migrate(&examples.User{})
}

// teardown 测试清理
func teardown() error {
	if testEngine != nil {
		sess := testEngine.NewSession(&examples.User{})
		if err := sess.DeleteTable(); err != nil {
			return fmt.Errorf("failed to delete 'users' table: %v", err)
		}

		sess = testEngine.NewSession(&examples.Group{})
		if err := sess.DeleteTable(); err != nil {
			return fmt.Errorf("failed to delete 'groups' table: %v", err)
		}

		if err := testEngine.Close(); err != nil {
			return fmt.Errorf("failed to close 'test' database: %v", err)
		}
	}

	return nil
}

func TestMySQL_NewEngine(t *testing.T) {
	if err := setup(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := teardown(); err != nil {
			t.Fatal(err)
		}
	}()

	if testEngine == nil {
		t.Fatal("Engine should not be nil")
	}
}

func TestMySQL_NewSession(t *testing.T) {
	if err := setup(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := teardown(); err != nil {
			t.Fatal(err)
		}
	}()

	if sess := testEngine.NewSession(); sess == nil {
		t.Fatal("NewSession returned nil")
	}
}

func TestMySQL_NewSession_UseModel(t *testing.T) {
	if err := setup(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := teardown(); err != nil {
			t.Fatal(err)
		}
	}()

	sess := testEngine.NewSession(&examples.User{})
	if sess == nil {
		t.Fatal("NewSession with model returned nil")
	}

	// 测试 Schema 是否正确解析
	sch, err := sess.GetSchema()
	if err != nil {
		t.Fatal("Schema should not be nil")
	}

	if sch.TableName != "users" {
		t.Fatalf("Expected table name 'users', got '%s'", sch.TableName)
	}
}

func TestMySQL_Transact_Success(t *testing.T) {
	if err := setup(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := teardown(); err != nil {
			t.Fatal(err)
		}
	}()

	r, err := testEngine.Transact(func(sess *session.Session) (any, error) {
		return sess.UseModel(&examples.User{}).INSERT(false, examples.User{
			Name:    "John Doe",
			Email:   "john@example.com",
			Age:     30,
			GroupID: 2,
		})
	})
	if err != nil {
		t.Fatalf("Transaction failed: %v", err)
	}
	if r == nil {
		t.Fatal("Transaction returned nil result")
	}

	// 验证数据是否插入成功
	count, res, err := testEngine.NewSession(&examples.User{}).WHERE(where.EQ("name", "John Doe")).COUNT()
	if err != nil {
		t.Fatalf("Failed to query count: %v", err)
	}
	if count != 1 {
		t.Fatalf("Expected 1 record, got %d", count)
	}
	fmt.Println(res.GetExecutionSQL())
	t.Log(res.GetExecutionCost())
}

func TestMySQL_Transact_Failure(t *testing.T) {
	if err := setup(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := teardown(); err != nil {
			t.Fatal(err)
		}
	}()

	// 第一次插入成功
	if _, err := testEngine.Transact(func(session *session.Session) (any, error) {
		return session.UseModel(&examples.User{}).INSERT(false, examples.User{
			Name:    "John Doe",
			Email:   "john@example.com",
			Age:     30,
			GroupID: 2,
		})
	}); err != nil {
		t.Fatalf("First transaction failed: %v", err)
	}

	// 第二次插入相同的email（违反唯一约束），应该失败
	if _, err := testEngine.Transact(func(session *session.Session) (any, error) {
		return session.UseModel(&examples.User{}).INSERT(false, examples.User{
			Name:    "John Doe",
			Email:   "john@example.com",
			Age:     30,
			GroupID: 2,
		})
	}); err == nil {
		t.Fatal("Expected transaction to fail due to duplicate email")
	}

	// 验证只有一条记录（事务回滚）
	count, res, err := testEngine.NewSession(&examples.User{}).WHERE(where.EQ("email", "john@example.com")).COUNT()
	if err != nil {
		t.Fatalf("Failed to query count: %v", err)
	}
	if count != 1 {
		t.Fatalf("Expected 1 record, got %d", count)
	}
	fmt.Println(res.GetExecutionSQL())
	t.Log(res.GetExecutionCost())
}

func TestMySQL_Transact_Panic(t *testing.T) {
	if err := setup(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := teardown(); err != nil {
			t.Fatal(err)
		}
	}()

	_, err := testEngine.Transact(func(s *session.Session) (any, error) {
		panic(ErrPanicSentinel)
	})

	// panic 不能被抹成字符串：必须保留原始值与调用栈，并支持 errors.Is
	var panicErr *PanicError
	if !errors.As(err, &panicErr) {
		t.Fatalf("panic 应包装为 *PanicError，实际: %T %v", err, err)
	}

	if !errors.Is(err, ErrPanicSentinel) {
		t.Errorf("PanicError.Unwrap 应能回到原始错误，实际: %v", err)
	}

	if len(panicErr.Stack) == 0 {
		t.Error("PanicError 应保留调用栈")
	}
}

// ErrPanicSentinel 用于验证 panic 包装后的错误链
var ErrPanicSentinel = errors.New("forced panic")

// TestMySQL_Naming 验证引擎下发的命名策略会作用于新建会话
func TestMySQL_Naming(t *testing.T) {
	engine, err := NewEngine("mysql", examples.DBSource)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()

	engine.SetNaming(naming.Strategy{Prefix: "it_"})

	sess := engine.NewSession(&examples.Group{})
	if got := sess.GetTableName(); got != "it_groups" {
		t.Fatalf("引擎命名策略未下发: GetTableName = %q", got)
	}

	_ = sess.DeleteTable()
	if err := sess.CreateTable(); err != nil {
		t.Fatalf("带前缀建表失败: %v", err)
	}
	defer func() { _ = sess.DeleteTable() }()

	exists, err := sess.ExistsTable()
	if err != nil {
		t.Fatalf("ExistsTable failed: %v", err)
	}

	if !exists {
		t.Fatal("带前缀的表应存在")
	}
}

func TestMySQL_ConnectionSettings(t *testing.T) {
	if err := setup(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := teardown(); err != nil {
			t.Fatal(err)
		}
	}()

	testEngine.SetMaxIdleConnections(5)
	testEngine.SetMaxOpenConnections(20)
	testEngine.SetConnMaxIdleTime(30 * time.Second)
	testEngine.SetConnMaxLifetime(60 * time.Second)

	// 验证设置是否生效
	stats := testEngine.GetStatus()
	if stats.MaxOpenConnections != 20 {
		t.Fatalf("Expected MaxOpenConnections to be 5, got %d", stats.MaxOpenConnections)
	}
}

func TestMySQL_SchemaCaching(t *testing.T) {
	if err := setup(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := teardown(); err != nil {
			t.Fatal(err)
		}
	}()

	// 第一次创建session，应该解析并缓存schema
	schema1, err := testEngine.NewSession(&examples.User{}).GetSchema()
	if err != nil {
		t.Fatal("Schema should be parsed and cached")
	}

	// 第二次创建session，应该使用缓存的schema
	schema2, err := testEngine.NewSession(&examples.User{}).GetSchema()
	if err != nil {
		t.Fatal("Cached schema should be used")
	}

	// 验证两次session使用的是同一个schema对象
	if !reflect.DeepEqual(schema1, schema2) {
		t.Fatal("Schema instances should be the same")
	}
}

func TestMySQL_CRUD_Operations(t *testing.T) {
	if err := setup(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := teardown(); err != nil {
			t.Fatal(err)
		}
	}()

	// 插入
	r, err := testEngine.Transact(func(session *session.Session) (any, error) {
		return session.UseModel(&examples.User{}).INSERT(false, examples.User{
			Name:    "Test User",
			Email:   "test@example.com",
			Age:     25,
			GroupID: 2,
		})
	})
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	res := r.(result.Result)
	userID, _ := res.LastInsertId()
	fmt.Println(res.GetExecutionSQL())
	t.Log(res.GetExecutionCost())

	// 验证插入
	var user examples.User
	res, err = testEngine.NewSession(&examples.User{}).WHERE(where.EQ("id", userID)).SELECT(&user)
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if user.Name != "Test User" {
		t.Fatalf("Expected name 'Test User', got '%s'", user.Name)
	}
	if user.Email != "test@example.com" {
		t.Fatalf("Expected email 'test@example.com', got '%s'", user.Name)
	}
	if user.Age != 25 {
		t.Fatalf("Expected age '25', got '%s'", user.Name)
	}
	if user.GroupID != 2 {
		t.Fatalf("Expected group_id '2', got '%s'", user.Name)
	}
	fmt.Println(res.GetExecutionSQL())
	t.Log(res.GetExecutionCost())

	// 更新
	res, err = testEngine.NewSession(&examples.User{}).WHERE(where.EQ("id", userID)).
		UPDATE(map[string]any{"name": "wx", "email": "wx@qq.com", "age": 26})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	fmt.Println(res.GetExecutionSQL())
	t.Log(res.GetExecutionCost())

	// 验证更新
	res, err = testEngine.NewSession(&examples.User{}).WHERE(where.EQ("id", userID)).SELECT(&user)
	if err != nil {
		t.Fatalf("Select failed: %v", err)
	}
	if user.Name != "wx" {
		t.Fatalf("Expected name 'wx', got '%s'", user.Name)
	}
	if user.Email != "wx@qq.com" {
		t.Fatalf("Expected email 'wx@qq.com', got '%s'", user.Name)
	}
	if user.Age != 26 {
		t.Fatalf("Expected age '26', got '%s'", user.Name)
	}
	if user.GroupID != 2 {
		t.Fatalf("Expected group_id '2', got '%s'", user.Name)
	}
	fmt.Println(res.GetExecutionSQL())
	t.Log(res.GetExecutionCost())

	// 删除
	res, err = testEngine.NewSession(&examples.User{}).WHERE(where.EQ("id", userID)).DELETE()
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	fmt.Println(res.GetExecutionSQL())
	t.Log(res.GetExecutionCost())

	// 验证删除
	count, res, err := testEngine.NewSession(&examples.User{}).WHERE(where.EQ("id", userID)).COUNT()
	if err != nil {
		t.Fatalf("Failed to query count: %v", err)
	}
	if count != 0 {
		t.Fatalf("Expected 0 record, got %d", count)
	}
	fmt.Println(res.GetExecutionSQL())
	t.Log(res.GetExecutionCost())
}

func TestMySQL_MultipleModels(t *testing.T) {
	if err := setup(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := teardown(); err != nil {
			t.Fatal(err)
		}
	}()

	// 测试用户模型
	userSession := testEngine.NewSession(&examples.User{})
	userSchema, err := userSession.GetSchema()
	if err != nil {
		t.Fatalf("User schema should be parsed")
	}
	if userSchema.TableName != "users" {
		t.Fatalf("Expected table name 'users', got '%s'", userSchema.TableName)
	}

	// 测试产品模型
	groupSession := testEngine.NewSession(&examples.Group{})
	groupSchema, err := groupSession.GetSchema()
	if err != nil {
		t.Fatalf("Group schema should be parsed")
	}
	if groupSchema.TableName != "groups" {
		t.Fatalf("Expected table name 'groups', got '%s'", groupSchema.TableName)
	}

	// 验证两个schema不同
	if reflect.DeepEqual(userSchema, groupSchema) {
		t.Fatal("User and product schemas should be different")
	}
}

func TestMySQL_ExtractDBName(t *testing.T) {
	testCases := []struct {
		dsn      string
		expected string
		hasFatal bool
	}{
		{
			dsn:      "user:password@tcp(localhost:3306)/test?charset=utf8mb4",
			expected: "test",
			hasFatal: false,
		},
		{
			dsn:      "user:password@tcp(localhost:3306)/",
			expected: "",
			hasFatal: true,
		},
		{
			dsn:      "user:password@tcp(localhost:3306)",
			expected: "",
			hasFatal: true,
		},
	}

	for _, tc := range testCases {
		res, err := extractDBName("mysql", tc.dsn)
		if tc.hasFatal && err == nil {
			t.Fatalf("Expected error for DSN: %s", tc.dsn)
		}
		if !tc.hasFatal && err != nil {
			t.Fatalf("Unexpected error for DSN: %s: %v", tc.dsn, err)
		}
		if !tc.hasFatal && res != tc.expected {
			t.Fatalf("For DSN: %s, expected: %s, got: %s", tc.dsn, tc.expected, res)
		}
	}
}

func TestSQLite_ExtractDBName(t *testing.T) {
	testCases := []struct {
		driverName string
		dsn        string
		expected   string
		hasFatal   bool
	}{
		{driverName: "sqlite", dsn: "data/app.db", expected: "app.db"},
		{driverName: "sqlite3", dsn: "file:app.db?cache=shared&mode=rwc", expected: "app.db"},
		{driverName: "sqlite", dsn: ":memory:", expected: ":memory:"},
		{driverName: "sqlite", dsn: "file::memory:?cache=shared", expected: ":memory:"},
		{driverName: "sqlite", dsn: `C:\data\app.db`, expected: "app.db"},
		{driverName: "sqlite", dsn: "", expected: "", hasFatal: true},
		{driverName: "sqlite", dsn: "file:?cache=shared", expected: "", hasFatal: true},
	}

	for _, tc := range testCases {
		res, err := extractDBName(tc.driverName, tc.dsn)
		if tc.hasFatal {
			if err == nil {
				t.Fatalf("Expected error for DSN: %s", tc.dsn)
			}

			continue
		}

		if err != nil {
			t.Fatalf("Unexpected error for DSN: %s: %v", tc.dsn, err)
		}
		if res != tc.expected {
			t.Fatalf("For DSN: %s, expected: %s, got: %s", tc.dsn, tc.expected, res)
		}
	}
}

// engineRecordingLogger 记录引擎下发后会话上报的 SQL 观测事件
type engineRecordingLogger struct {
	mu     sync.Mutex
	events []QueryEvent
}

// LogQuery 记录事件（实现 Logger 接口）
func (own *engineRecordingLogger) LogQuery(_ context.Context, event QueryEvent) {
	own.mu.Lock()
	own.events = append(own.events, event)
	own.mu.Unlock()
}

// count 返回已记录的事件数量
func (own *engineRecordingLogger) count() int {
	own.mu.Lock()
	defer own.mu.Unlock()

	return len(own.events)
}

// TestMySQL_Logger 验证引擎配置的日志器与慢 SQL 阈值会下发到新建会话
func TestMySQL_Logger(t *testing.T) {
	if err := setup(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := teardown(); err != nil {
			t.Fatal(err)
		}
	}()

	// 使用独立引擎，避免受 setup 中开启的调试模式影响
	engine, err := NewEngine("mysql", examples.DBSource)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()

	logger := &engineRecordingLogger{}
	engine.SetLogger(logger).SetSlowThreshold(time.Second)

	// 未开调试、未达阈值：不应产生事件
	if _, _, err := engine.NewSession(&examples.Group{}).COUNT(); err != nil {
		t.Fatalf("count failed: %v", err)
	}

	if got := logger.count(); got != 0 {
		t.Fatalf("未开调试且未达阈值时不应上报，实际 %d 条", got)
	}

	// 开启调试后，新建会话应把事件交给引擎配置的日志器
	engine.SetDebug(false)
	if _, _, err := engine.NewSession(&examples.Group{}).COUNT(); err != nil {
		t.Fatalf("count failed: %v", err)
	}

	if got := logger.count(); got != 1 {
		t.Fatalf("调试模式下应收到 1 条事件，实际 %d 条", got)
	}
}

// TestEngine_CloseClearsStatements 验证关闭引擎会清空预编译语句表
//
// 只关闭不删除会让关闭后的引擎继续交出已关闭的语句。
func TestEngine_CloseClearsStatements(t *testing.T) {
	engine, err := NewEngine("mysql", examples.DBSource)
	if err != nil {
		t.Fatal(err)
	}

	const key = "unit-test-close"
	if err := engine.Prepare(key, "SELECT 1"); err != nil {
		t.Fatalf("prepare failed: %v", err)
	}

	if _, ok := engine.stmts.Load(key); !ok {
		t.Fatal("Prepare 后应写入语句表")
	}

	if err = engine.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	if _, ok := engine.stmts.Load(key); ok {
		t.Fatal("Close 后语句表应清空")
	}
}

/*
cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		with model			8		3475077	       347.2 ns/op	    1376 B/op	       6 allocs/op
cpu: Intel(R) Core(TM) i5-14600KF						with model			8		4974956	       229.5 ns/op	    1376 B/op	       6 allocs/op

cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		without model		8		4447438	       270.6 ns/op	    1280 B/op	       5 allocs/op
cpu: Intel(R) Core(TM) i5-14600KF						without model		8		6423063	       194.5 ns/op	    1280 B/op	       5 allocs/op
*/
func BenchmarkEngine_NewSession(b *testing.B) {
	if err := setup(); err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := teardown(); err != nil {
			b.Fatal(err)
		}
	}()

	b.Run("with model", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			testEngine.NewSession(&examples.User{})
		}
	})

	b.Run("without model", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			testEngine.NewSession(nil)
		}
	})
}
