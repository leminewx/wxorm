package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/harness/examples"
	"gitee.com/leminewx/wxorm/schema"
)

// setupSession 为指定模型创建 Session，并自动建表、清理旧表
func setupGroupSession(t testing.TB) *Session {
	// 创建数据库连接
	db, err := examples.SetupTestDB()
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}

	dial := dialect.Get("mysql")
	sch, err := schema.Parse(dial, &examples.Group{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	// 创建会话
	sess := NewSession(1, examples.DBName, db, dial, &sync.Map{})
	sess.SetSchema(sch)

	// 先删除，再创建
	_ = sess.DeleteTable()
	if err := sess.CreateTable(); err != nil {
		t.Fatalf("create table failed: %v", err)
	}

	return sess
}

// testInsertGroup 测试插入数据
func testInsertGroup(sess *Session, name, createdBy string) int64 {
	group := &examples.Group{
		Name:      name,
		CreatedBy: createdBy,
	}

	result, err := sess.INSERT(false, group)
	if err != nil {
		panic(fmt.Sprintf("insert failed: %v", err))
	}

	id, _ := result.LastInsertId()
	return id
}

func setupFilterSession(t testing.TB) *Session {
	// 创建数据库连接
	db, err := examples.SetupTestDB()
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}

	dial := dialect.Get("mysql")
	sch, err := schema.Parse(dial, &examples.Filter{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	// 创建会话
	sess := NewSession(1, examples.DBName, db, dial, &sync.Map{})
	sess.SetSchema(sch)

	// 先删除，再创建
	_ = sess.DeleteTable()
	if err := sess.CreateTable(); err != nil {
		t.Fatalf("create table failed: %v", err)
	}

	return sess
}

// teardownSession 清理表、关闭预编译缓存和数据库连接
func teardownSession(sess *Session) {
	_ = sess.DeleteTable()
	_ = sess.Close()
	_ = sess.db.Close()
}

// 全局测试变量
var (
	testDB          *sql.DB
	testUserSchema  *schema.Schema
	testGroupSchema *schema.Schema
	testDialect     dialect.Dialect
)

// setupTestDB 初始化测试数据库
func setupTestDB(t *testing.T) (group, user *Session) {
	// 创建方言实例
	testDialect = dialect.Get("mysql")

	// 创建数据库连接
	var err error
	if testDB, err = sql.Open(testDialect.GetDriverName(), examples.DBSource); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// 调优连接池参数
	testDB.SetMaxOpenConns(50)                  // 最大连接数
	testDB.SetMaxIdleConns(10)                  // 最大空闲连接数
	testDB.SetConnMaxLifetime(30 * time.Minute) // 连接最大生命周期
	testDB.SetConnMaxIdleTime(10 * time.Minute) // 连接最大空闲时间

	// 解析 groups 表
	testGroupSchema, err = schema.Parse(testDialect, &examples.Group{})
	if err != nil {
		t.Fatalf("Failed to parse 'groups' schema: %v", err)
	}

	// 创建 groups 表
	group = NewSession(0, examples.DBName, testDB, testDialect, &sync.Map{})
	group.SetSchema(testGroupSchema)
	group.SetDebug(true)
	_ = group.DeleteTable()
	if err = group.CreateTable(); err != nil {
		t.Fatalf("Failed to create 'groups' table: %v", err)
	}

	// 预写入一些 groups 数据
	if res, err := group.INSERT(false,
		&examples.Group{Name: "group1"},
		&examples.Group{Name: "group2"},
		&examples.Group{Name: "group3"},
	); err != nil {
		t.Fatalf("Failed to insert 'groups' table: %v", err)
	} else if rows, err := res.RowsAffected(); err != nil {
		t.Fatalf("Failed to get affected rows: %v", err)
	} else if rows != 3 {
		t.Fatalf("Failed to get affected rows: %v", rows)
	}

	// 解析 users 表
	testUserSchema, err = schema.Parse(testDialect, &examples.User{})
	if err != nil {
		t.Fatalf("Failed to parse 'users' schema: %v", err)
	}

	// 清空表
	user = NewSession(0, examples.DBName, testDB, testDialect, &sync.Map{})
	user.SetSchema(testUserSchema)
	user.SetDebug(true)
	_ = user.DeleteTable()
	if err = user.CreateTable(); err != nil {
		t.Fatalf("Failed to create 'users' table: %v", err)
	}

	return
}

// teardownTestDB 清理测试数据库
func teardownTestDB(t *testing.T, group, user *Session) {
	if testDB != nil {
		if user != nil {
			if err := user.DeleteTable(); err != nil {
				t.Errorf("Failed to delete 'users' table: %v", err)
			}
		}

		if group != nil {
			if err := group.DeleteTable(); err != nil {
				t.Errorf("Failed to delete 'groups' table: %v", err)
			}
		}

		if err := testDB.Close(); err != nil {
			t.Errorf("Failed to close 'test' database: %v", err)
		}
	}
}

// TestSession_ExecRespectsContext 验证原生 SQL 方法统一受会话上下文约束
//
// 历史上 Exec/Query/QueryRow 直接透传给 database/sql，SetTimeout 对它们无效，
// 导致混用 DSL 与原生 SQL 时只有部分路径受保护。
func TestSession_ExecRespectsContext(t *testing.T) {
	sess := setupGroupSession(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sess.SetContext(ctx)

	if _, err := sess.Exec("SELECT 1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Exec 应返回 context.Canceled，实际: %v", err)
	}

	if _, err := sess.Query("SELECT 1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Query 应返回 context.Canceled，实际: %v", err)
	}

	if err := sess.QueryRow("SELECT 1").Scan(new(int)); !errors.Is(err, context.Canceled) {
		t.Fatalf("QueryRow 应返回 context.Canceled，实际: %v", err)
	}
}

// TestSession_Exec 测试Exec方法
func TestSession_Exec(t *testing.T) {
	group, user := setupTestDB(t)
	defer func() {
		if err := recover(); err != nil {
			t.Error(err)
		}

		teardownTestDB(t, group, user)
	}()

	// 测试插入数据
	var _ = examples.User{}
	sqlStr := fmt.Sprintf("INSERT INTO `%s` (`name`, `age`, `email`, `group_id`) VALUES (?, ?, ?, ?)", user.schema.TableName)
	result, err := user.Exec(sqlStr, "John Doe", 18, "john@example.com", 1)
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// 检查插入结果
	lastID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("Failed to get last insert ID: %v", err)
	}
	if lastID != 1 {
		t.Error("Expected last insert ID is not equal to 1")
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("Failed to get rows affected: %v", err)
	}

	if rowsAffected != 1 {
		t.Errorf("Expected 1 row affected, got %d", rowsAffected)
	}
}

// TestSession_ExecContext 测试ExecContext方法
func TestSession_ExecContext(t *testing.T) {
	group, user := setupTestDB(t)
	defer func() {
		if err := recover(); err != nil {
			t.Error(err)
		}

		teardownTestDB(t, group, user)
	}()

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 测试插入数据
	var _ = examples.User{}
	sqlStr := fmt.Sprintf("INSERT INTO %s (`name`, `age`, `email`, `group_id`) VALUES (?, ?, ?, ?)", user.schema.TableName)
	result, err := user.ExecContext(ctx, sqlStr, "John Doe", 18, "john@example.com", 1)
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	// 检查插入结果
	lastID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("Failed to get last insert ID: %v", err)
	}

	if lastID != 1 {
		t.Error("Expected last insert ID is not equal to 1")
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("Failed to get rows affected: %v", err)
	}

	if rowsAffected != 1 {
		t.Errorf("Expected 1 row affected, got %d", rowsAffected)
	}
}

// TestSession_Query 测试Query方法
func TestSession_Query(t *testing.T) {
	group, user := setupTestDB(t)
	defer func() {
		if err := recover(); err != nil {
			t.Error(err)
		}

		teardownTestDB(t, group, user)
	}()

	// 先插入测试数据
	var _ = examples.User{}
	insertSQL := fmt.Sprintf("INSERT INTO %s (`name`, `email`, `group_id`) VALUES (?, ?, ?), (?, ?, ?)", user.schema.TableName)
	_, err := user.Exec(insertSQL, "User1", "user1@example.com", 1, "User2", "user2@example.com", 2)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// 查询数据
	querySQL := fmt.Sprintf("SELECT `id`, `name`, `email` FROM %s ORDER BY `id`", user.schema.TableName)
	rows, err := user.Query(querySQL)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	defer func() { _ = rows.Close() }()

	// 检查查询结果
	count := 0
	for rows.Next() {
		count++
		var id int
		var name, email string
		err := rows.Scan(&id, &name, &email)
		if err != nil {
			t.Fatalf("Failed to scan row: %v", err)
		}

		if id <= 0 {
			t.Error("Expected ID to be greater than 0")
		}

		if name == "" {
			t.Error("Expected name to not be empty")
		}

		if email == "" {
			t.Error("Expected email to not be empty")
		}
	}

	if count != 2 {
		t.Errorf("Expected 2 rows, got %d", count)
	}
}

// TestSession_QueryContext 测试QueryContext方法
func TestSession_QueryContext(t *testing.T) {
	group, user := setupTestDB(t)
	defer func() {
		if err := recover(); err != nil {
			t.Error(err)
		}

		teardownTestDB(t, group, user)
	}()

	// 先插入测试数据
	var _ = examples.User{}
	insertSQL := fmt.Sprintf("INSERT INTO %s (`name`, `email`, `group_id`) VALUES (?, ?, ?), (?, ?, ?)", user.schema.TableName)
	_, err := user.Exec(insertSQL, "User1", "user1@example.com", 1, "User2", "user2@example.com", 2)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 查询数据
	querySQL := fmt.Sprintf("SELECT `id`, `name`, `email` FROM %s", user.schema.TableName)
	rows, err := user.QueryContext(ctx, querySQL)
	if err != nil {
		t.Fatalf("QueryContext failed: %v", err)
	}
	defer func() { _ = rows.Close() }()

	// 检查查询结果
	count := 0
	for rows.Next() {
		count++
	}

	if count != 2 {
		t.Errorf("Expected 1 row, got %d", count)
	}
}

// TestSession_QueryRow 测试QueryRow方法
func TestSession_QueryRow(t *testing.T) {
	group, user := setupTestDB(t)
	defer func() {
		if err := recover(); err != nil {
			t.Error(err)
		}

		teardownTestDB(t, group, user)
	}()

	// 先插入测试数据
	var _ = examples.User{}
	insertSQL := fmt.Sprintf("INSERT INTO %s (`name`, `email`, `group_id`) VALUES (?, ?, ?)", user.schema.TableName)
	result, err := user.Exec(insertSQL, "QueryRow User", "queryrow@example.com", 2)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	lastID, _ := result.LastInsertId()

	// 查询单行数据
	querySQL := fmt.Sprintf("SELECT `name`, `email` FROM %s WHERE `id` = ?", user.schema.TableName)
	row := user.QueryRow(querySQL, lastID)

	var name, email string
	err = row.Scan(&name, &email)
	if err != nil {
		t.Fatalf("QueryRow failed: %v", err)
	}

	if name != "QueryRow User" {
		t.Errorf("Expected name 'QueryRow User', got '%s'", name)
	}

	if email != "queryrow@example.com" {
		t.Errorf("Expected email 'queryrow@example.com', got '%s'", email)
	}
}

// TestSession_QueryRowContext 测试QueryRowContext方法
func TestSession_QueryRowContext(t *testing.T) {
	group, user := setupTestDB(t)
	defer func() {
		if err := recover(); err != nil {
			t.Error(err)
		}

		teardownTestDB(t, group, user)
	}()

	// 先插入测试数据
	var _ = examples.User{}
	insertSQL := fmt.Sprintf("INSERT INTO %s (`name`, `email`, `group_id`) VALUES (?, ?, ?)", user.schema.TableName)
	result, err := user.Exec(insertSQL, "QueryRowContext User", "queryrowcontext@example.com", 2)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	lastID, _ := result.LastInsertId()

	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 查询单行数据
	querySQL := fmt.Sprintf("SELECT name, email FROM %s WHERE id = ?", user.schema.TableName)
	row := user.QueryRowContext(ctx, querySQL, lastID)

	var name, email string
	err = row.Scan(&name, &email)
	if err != nil {
		t.Fatalf("QueryRowContext failed: %v", err)
	}

	if name != "QueryRowContext User" {
		t.Errorf("Expected name 'QueryRowContext User', got '%s'", name)
	}

	if email != "queryrowcontext@example.com" {
		t.Errorf("Expected email 'queryrowcontext@example.com', got '%s'", email)
	}
}

// TestSession_GetDatabase 测试GetDatabase方法
func TestSession_GetDatabase(t *testing.T) {
	group, user := setupTestDB(t)
	defer func() {
		if err := recover(); err != nil {
			t.Error(err)
		}

		teardownTestDB(t, group, user)
	}()

	db := user.GetDatabase()
	if db == nil {
		t.Error("GetDatabase returned nil")
	}

	// 确认返回的是*sql.DB类型
	if _, ok := db.(*sql.DB); !ok {
		t.Error("GetDatabase did not return *sql.DB")
	}
}
