package session

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/harness/examples"
	"gitee.com/leminewx/wxorm/schema"
)

// mustExistsTable 断言表是否存在
//
// ExistsTable 会把查询失败当作错误返回，测试里必须显式处理，
// 否则驱动或权限问题会被当成“表不存在”而掩盖真实故障。
func mustExistsTable(t *testing.T, sess *Session) bool {
	t.Helper()

	exists, err := sess.ExistsTable()
	if err != nil {
		t.Fatalf("ExistsTable failed: %v", err)
	}

	return exists
}

// TestSession_CreateTable 测试创建表
func TestSession_CreateTable(t *testing.T) {
	// 创建方言实例
	testDialect = dialect.Get("mysql")

	// 创建数据库连接
	var err error
	if testDB, err = sql.Open(testDialect.GetDriverName(), examples.DBSource); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// 解析 groups 表
	testGroupSchema, err = schema.Parse(testDialect, &examples.Group{})
	if err != nil {
		t.Fatalf("Failed to parse 'groups' schema: %v", err)
	}

	// 创建 groups 表
	group := NewSession(0, examples.DBName, testDB, testDialect, &sync.Map{})
	group.SetSchema(testGroupSchema)
	group.SetDebug(false)
	if err = group.CreateTable(); err != nil {
		t.Fatalf("CreateTable failed: %v", err)
	}
	defer func() { _ = group.DeleteTable() }()

	// 验证表是否存在
	if !mustExistsTable(t, group) {
		t.Error("Table should exist after creation")
	}
}

// TestSession_CreateTable_WithParams 测试带参数创建表
func TestSession_CreateTable_WithParams(t *testing.T) {
	// 创建方言实例
	testDialect = dialect.Get("mysql")

	// 创建数据库连接
	var err error
	if testDB, err = sql.Open(testDialect.GetDriverName(), examples.DBSource); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// 解析 groups 表
	testGroupSchema, err = schema.Parse(testDialect, &examples.Group{})
	if err != nil {
		t.Fatalf("Failed to parse 'groups' schema: %v", err)
	}

	// 创建 session
	group := NewSession(0, examples.DBName, testDB, testDialect, &sync.Map{})
	group.SetSchema(testGroupSchema)
	group.SetDebug(false)

	// 创建 groups 表
	engine := "InnoDB"
	charset := "utf8mb4"
	collate := "utf8mb4_unicode_ci"
	comment := "Test user table"
	if err = group.CreateTable(dialect.TableMetadata{
		Engine:  engine,
		Charset: charset,
		Collate: collate,
		Comment: comment,
	}); err != nil {
		t.Fatalf("CreateTable with params failed: %v", err)
	}
	defer func() { _ = group.DeleteTable() }()

	// 验证表是否存在
	if !mustExistsTable(t, group) {
		t.Error("Table should exist after creation")
	}

	// 验证表属性
	query := fmt.Sprintf(`SELECT ENGINE, TABLE_COLLATION, TABLE_COMMENT
FROM INFORMATION_SCHEMA.TABLES
WHERE TABLE_SCHEMA = '%s' AND TABLE_NAME = '%s';`, examples.DBName, group.schema.TableName)
	row := group.QueryRow(query)

	// 校验数据
	var actualEngine, actualCollation, actualComment string
	err = row.Scan(&actualEngine, &actualCollation, &actualComment)
	if err != nil {
		t.Fatalf("Failed to query table properties: %v", err)
	}

	if actualEngine != engine {
		t.Errorf("Expected engine %s, got %s", engine, actualEngine)
	}

	if actualCollation != collate {
		t.Errorf("Expected collation %s, got %s", collate, actualCollation)
	}

	if actualComment != comment {
		t.Errorf("Expected comment %s, got %s", comment, actualComment)
	}
}

// TestSession_DeleteTable 测试删除表
func TestSession_DeleteTable(t *testing.T) {
	// 创建方言实例
	testDialect = dialect.Get("mysql")

	// 创建数据库连接
	var err error
	if testDB, err = sql.Open(testDialect.GetDriverName(), examples.DBSource); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// 解析 groups 表
	testGroupSchema, err = schema.Parse(testDialect, &examples.Group{})
	if err != nil {
		t.Fatalf("Failed to parse 'groups' schema: %v", err)
	}

	// 创建 groups 表
	group := NewSession(0, examples.DBName, testDB, testDialect, &sync.Map{})
	group.SetSchema(testGroupSchema)
	group.SetDebug(false)
	if err = group.CreateTable(); err != nil {
		t.Fatalf("CreateTable failed: %v", err)
	}
	defer func() { _ = group.DeleteTable() }()

	// 验证表存在
	if !mustExistsTable(t, group) {
		t.Error("Table should exist before deletion")
	}

	// 删除表
	if err = group.DeleteTable(); err != nil {
		t.Fatalf("DeleteTable failed: %v", err)
	}

	// 验证表不存在
	if mustExistsTable(t, group) {
		t.Error("Table should not exist after deletion")
	}
}

// TestSession_DeleteTable_NonExistent 测试删除不存在的表
func TestSession_DeleteTable_NonExistent(t *testing.T) {
	// 创建方言实例
	testDialect = dialect.Get("mysql")

	// 创建数据库连接
	var err error
	if testDB, err = sql.Open(testDialect.GetDriverName(), examples.DBSource); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// 解析 groups 表
	testGroupSchema, err = schema.Parse(testDialect, &examples.Group{})
	if err != nil {
		t.Fatalf("Failed to parse 'groups' schema: %v", err)
	}

	// 创建 session，但是不创建 groups 表
	group := NewSession(0, examples.DBName, testDB, testDialect, &sync.Map{})
	group.SetSchema(testGroupSchema)
	group.SetDebug(false)

	// 查询 groups
	if _, _, err := group.COUNT(); err == nil {
		t.Fatal("Select table on non-existent table should not fail, got <nil>")
	}
}

// TestSession_ExistsTable 测试检查表是否存在
func TestSession_ExistsTable(t *testing.T) {
	// 创建方言实例
	testDialect = dialect.Get("mysql")

	// 创建数据库连接
	var err error
	if testDB, err = sql.Open(testDialect.GetDriverName(), examples.DBSource); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// 解析 groups 表
	testGroupSchema, err = schema.Parse(testDialect, &examples.Group{})
	if err != nil {
		t.Fatalf("Failed to parse 'groups' schema: %v", err)
	}

	// 创建 session，但是不创建 groups 表
	group := NewSession(0, examples.DBName, testDB, testDialect, &sync.Map{})
	group.SetSchema(testGroupSchema)
	group.SetDebug(false)

	// 初始状态下表应该不存在
	if mustExistsTable(t, group) {
		t.Error("Table should not exist initially")
	}

	// 创建表
	if err = group.CreateTable(); err != nil {
		t.Fatalf("CreateTable failed: %v", err)
	}
	defer func() { _ = group.DeleteTable() }()

	// 现在表应该存在
	if !mustExistsTable(t, group) {
		t.Error("Table should exist after creation")
	}

	// 删除表
	if err = group.DeleteTable(); err != nil {
		t.Fatalf("DeleteTable failed: %v", err)
	}

	// 表应该不存在了
	if mustExistsTable(t, group) {
		t.Error("Table should not exist after deletion")
	}
}

// TestSession_GetSchema_NoModel 测试没有设置模型时获取Schema
func TestSession_GetSchema_NoModel(t *testing.T) {
	// 创建 session，但是不创建 groups 表
	sess := &Session{}

	// 没有设置schema时应该返回错误
	if _, err := sess.GetSchema(); !errors.Is(err, ErrModelNotSpecified) {
		t.Errorf("Expected error '%v', got '%v'", ErrModelNotSpecified, err)
	}
}

// TestSession_CreateTable_NoModel 测试没有模型时创建表
func TestSession_CreateTable_NoModel(t *testing.T) {
	sess := &Session{}

	// 没有设置schema时应该返回错误
	if err := sess.CreateTable(); !errors.Is(err, ErrModelNotSpecified) {
		t.Errorf("Expected error '%v', got '%v'", ErrModelNotSpecified, err)
	}
}

// TestSession_DeleteTable_NoModel 测试没有模型时删除表
func TestSession_DeleteTable_NoModel(t *testing.T) {
	sess := &Session{}

	// 没有设置schema时应该返回错误
	if err := sess.DeleteTable(); !errors.Is(err, ErrModelNotSpecified) {
		t.Errorf("Expected error '%v', got '%v'", ErrModelNotSpecified, err)
	}
}

// TestSession_ExistsTable_NoModel 测试没有模型时检查表是否存在
func TestSession_ExistsTable_NoModel(t *testing.T) {
	sess := &Session{}

	// 没有设置 schema 时应该返回错误，而不是伪装成“表不存在”
	exists, err := sess.ExistsTable()
	if !errors.Is(err, ErrModelNotSpecified) {
		t.Errorf("Expected error '%v', got '%v'", ErrModelNotSpecified, err)
	}

	if exists {
		t.Error("ExistsTable should return false when no model is specified")
	}
}

// TestSession_TableOperations_Integration 测试表操作的综合集成测试
func TestSession_TableOperations_Integration(t *testing.T) {
	group, user := setupTestDB(t)
	defer func() {
		if err := recover(); err != nil {
			t.Error(err)
		}

		teardownTestDB(t, group, user)
	}()

	// 验证表存在
	if !mustExistsTable(t, user) {
		t.Error("Table should exist after creation")
	}

	// 向表中插入一些数据
	var _ = &examples.User{}
	insertSQL := fmt.Sprintf("INSERT INTO `%s` (`name`, `email`) VALUES (?, ?), (?, ?)", testUserSchema.TableName)
	if _, err := user.Exec(insertSQL, "User1", "user1@example.com", "User2", "user2@example.com"); err != nil {
		t.Fatalf("Failed to insert data: %v", err)
	}

	// 验证数据插入成功
	querySQL := fmt.Sprintf("SELECT COUNT(*) FROM `%s`", testUserSchema.TableName)
	row := user.QueryRow(querySQL)

	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Failed to query data: %v", err)
	}

	if count != 2 {
		t.Errorf("Expected 2 rows, got %d", count)
	}

	// 删除表（应该同时删除数据）
	if err := user.DeleteTable(); err != nil {
		t.Fatalf("DeleteTable failed: %v", err)
	}

	// 验证表不存在
	if mustExistsTable(t, user) {
		t.Error("Table should not exist after deletion")
	}

	// 再次创建表
	if err := user.CreateTable(); err != nil {
		t.Fatalf("CreateTable failed: %v", err)
	}

	// 验证表存在
	if !mustExistsTable(t, user) {
		t.Error("Table should exist after recreation")
	}

	// 验证表是空的
	row = user.QueryRow(querySQL)
	if err := row.Scan(&count); err != nil {
		t.Fatalf("Failed to query data: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected 0 rows after recreation, got %d", count)
	}
}

// TestSession_CreateTable_SpecialChars 测试表注释包含特殊字符的情况
func TestSession_CreateTable_SpecialChars(t *testing.T) {
	// 创建方言实例
	testDialect = dialect.Get("mysql")

	// 创建数据库连接
	var err error
	if testDB, err = sql.Open(testDialect.GetDriverName(), examples.DBSource); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// 解析 groups 表
	testGroupSchema, err = schema.Parse(testDialect, &examples.Group{})
	if err != nil {
		t.Fatalf("Failed to parse 'groups' schema: %v", err)
	}

	// 创建 groups 表
	group := NewSession(0, examples.DBName, testDB, testDialect, &sync.Map{})
	group.SetSchema(testGroupSchema)
	group.SetDebug(false)

	// 创建表，注释包含单引号
	comment := "Test user's table with 'special' characters"
	if err = group.CreateTable(dialect.TableMetadata{
		Comment: comment,
	}); err != nil {
		t.Fatalf("CreateTable with special chars failed: %v", err)
	}
	defer func() { _ = group.DeleteTable() }()

	// 验证表存在
	if !mustExistsTable(t, group) {
		t.Error("Table should exist after creation")
	}

	// 验证注释正确处理
	query := fmt.Sprintf(`SELECT TABLE_COMMENT
FROM INFORMATION_SCHEMA.TABLES
WHERE TABLE_SCHEMA = '%s' AND TABLE_NAME = '%s'`, examples.DBName, testGroupSchema.TableName)

	row := group.QueryRow(query)
	var actualComment string
	if err = row.Scan(&actualComment); err != nil {
		t.Fatalf("Failed to query table comment: %v", err)
	}

	// 注释应该被正确处理（单引号被转义）
	if actualComment != comment {
		t.Errorf("Expected comment '%s', got '%s'", comment, actualComment)
	}
}

// TestSession_GetTableColumnTypes 验证列名与类型查询走数据字典
func TestSession_GetTableColumnTypes(t *testing.T) {
	sess := setupGroupSession(t)
	defer teardownSession(sess)

	columns, err := sess.GetTableColumnTypes()
	if err != nil {
		t.Fatalf("GetTableColumnTypes failed: %v", err)
	}

	// 内存驱动不记录列类型，只验证列名与顺序
	sch, err := sess.GetSchema()
	if err != nil {
		t.Fatalf("GetSchema failed: %v", err)
	}

	if len(columns) != len(sch.ColumnNames) {
		t.Fatalf("列数量 = %d, want %d", len(columns), len(sch.ColumnNames))
	}

	for i, name := range sch.ColumnNames {
		if columns[i].Name != name {
			t.Fatalf("第 %d 列 = %q, want %q", i, columns[i].Name, name)
		}
	}
}

// TestSession_GetTableIndexes 验证索引查询能反映已创建的索引
func TestSession_GetTableIndexes(t *testing.T) {
	sess := setupGroupSession(t)
	defer teardownSession(sess)

	// groups 模型没有声明索引
	indexes, err := sess.GetTableIndexes()
	if err != nil {
		t.Fatalf("GetTableIndexes failed: %v", err)
	}

	if len(indexes) != 0 {
		t.Fatalf("初始不应有索引，实际: %v", indexes)
	}

	if _, err = sess.Exec("CREATE INDEX `groups_name_idx` ON `groups` (`name`);"); err != nil {
		t.Fatalf("创建索引失败: %v", err)
	}

	indexes, err = sess.GetTableIndexes()
	if err != nil {
		t.Fatalf("GetTableIndexes failed: %v", err)
	}

	if len(indexes) != 1 || indexes[0] != "groups_name_idx" {
		t.Fatalf("索引列表 = %v, want [groups_name_idx]", indexes)
	}
}
