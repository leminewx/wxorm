package dialect

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/dialect/column"
	"gitee.com/leminewx/wxorm/dialect/datatype"
)

func TestMySQLBuildCreateSQL(t *testing.T) {
	tags := []string{
		"column(id) type(int) unique pk auto comment('ID')",
		"column(name) type(varchar(30)) notnull index(index_name1) default('') comment('用户名')",
		"column(age) type(smallint) notnull unique(unique_name1) default(0) comment('年龄') collate(utf8_unicode_ci)",
		"column(sex) type(enum('boy','girl')) index default('boy') comment('性别')",
		"column(email) type(varchar(255)) notnull unique(unique_name2) default('') comment('邮箱地址') collate(utf8_unicode_ci)",
		"column(role_id) type(bigint(32)) index(index_name1) fk(role(id),'fk_user_role',odc) comment('所属角色')",
		"column(auth_id) type(bigint(32)) index(index_name2) fk(group(auth_id),'fk_user_group',odc) pk comment('所属组权限')",
		"column(group_id) type(bigint(32)) unique(unique_name2) fk(group(id),ouc,fk_user_group,odc) comment('所属组')",
		"column(register_at) type(timestamp) created comment('注册时间')",
		"column(login_at) type(datetime) created updated comment('登录时间')",
	}

	dial := NewMySQLDialect()
	cols := make([]column.Column, 0)
	for _, tag := range tags {
		col, err := dial.ParseColumn(tag)
		if err != nil {
			t.Fatal(err)
		}

		cols = append(cols, col)
	}

	ctx := clause.NewContext()
	if err := dial.BuildTableCreateSQL(ctx, "users", cols, TableMetadata{
		Engine:  "InnoDB",
		Charset: "utf8mb4",
		Collate: "utf8mb4_general_ci",
		Comment: "用户表",
	}); err != nil {
		t.Fatal(err)
	}

	fmt.Println(ctx.GetResult())
}

// 测试完整 CREATE TABLE 语句生成
func TestMySQL_BuildCreateSQL(t *testing.T) {
	tags := []string{
		"column(id) type(int) pk auto comment('ID')",
		"column(name) type(varchar(30)) index(index1) default('') comment('用户名')",
		"column(age) type(smallint) unique(unique2) default(0) comment('年龄')",
		"column(sex) type(enum('boy','girl')) notnull comment('性别')",
		"column(email) type(varchar(255)) unique default('') comment('邮箱地址')",
		"column(register_at) type(timestamp) created comment('注册时间')",
		"column(login_at) type(datetime) created updated comment('登录时间')",
		"column(group_id) type(bigint(32)) unique(unique2) fk(group(id),'fk_user_group',odc,ouc) pk comment('所属组')",
	}

	mysql := NewMySQLDialect()
	columns := make([]column.Column, 0, len(tags))
	for _, tag := range tags {
		col, err := mysql.ParseColumn(tag)
		if err != nil {
			t.Fatalf("创建列失败: %v (tag: %s)", err, tag)
		}
		columns = append(columns, col)
	}

	ctx := clause.NewContext()
	err := mysql.BuildTableCreateSQL(ctx, "users", columns, TableMetadata{
		Engine:  "InnoDB",
		Charset: "utf8mb4",
		Collate: "utf8mb4_general_ci",
		Comment: "用户表",
	})
	if err != nil {
		t.Fatalf("生成 SQL 失败: %v", err)
	}

	sql, _ := ctx.GetResult()

	// 验证基本结构
	assertContains(t, sql, "CREATE TABLE `users`", "表名错误")
	assertContains(t, sql, "ENGINE=InnoDB", "引擎设置错误")
	assertContains(t, sql, "DEFAULT CHARSET=utf8mb4", "字符集错误")
	assertContains(t, sql, "COLLATE=utf8mb4_general_ci", "排序规则错误")
	assertContains(t, sql, "COMMENT='用户表'", "表注释错误")

	// 验证字段定义
	assertContains(t, sql, "`id` INT AUTO_INCREMENT COMMENT 'ID'", "id 字段错误")
	assertContains(t, sql, "`name` VARCHAR(30) DEFAULT '' COMMENT '用户名'", "name 字段错误")
	assertContains(t, sql, "`sex` ENUM('boy','girl') NOT NULL COMMENT '性别'", "sex 字段错误")

	// 验证主键约束
	assertContains(t, sql, "-- 命名主键", "主键注释缺失")
	assertContains(t, sql, "PRIMARY KEY (`id`, `group_id`)", "复合主键错误")

	// 索引不在建表语句内联，由 BuildTableIndexSQLs 建表后创建
	for _, unwanted := range []string{"-- 命名复合索引", "INDEX `index1`"} {
		if strings.Contains(sql, unwanted) {
			t.Errorf("CREATE TABLE 不应内联索引 %q，实际:\n%s", unwanted, sql)
		}
	}

	// 验证唯一约束
	assertContains(t, sql, "-- 命名唯一约束", "唯一约束注释缺失")
	assertContains(t, sql, "CONSTRAINT `unique2` UNIQUE (`age`, `group_id`)", "联合唯一约束错误")
	assertContains(t, sql, "`email` VARCHAR(255) DEFAULT '' UNIQUE COMMENT '邮箱地址'", "单列唯一约束错误")

	// 验证外键约束
	assertContains(t, sql, "-- 命名外键约束", "外键注释缺失")
	assertContains(t, sql, "CONSTRAINT `fk_user_group`", "外键名称错误")
	assertContains(t, sql, "REFERENCES `group` (`id`)", "外键关联错误")
	assertContains(t, sql, "ON DELETE CASCADE", "级联删除错误")
	assertContains(t, sql, "ON UPDATE CASCADE", "级联更新错误")
}

// MySQL 的列定义与表定义都不内联普通索引，索引由会话在建表后逐条执行
func TestMySQLBuildTableIndexSQLs(t *testing.T) {
	tags := []string{
		"column(name) type(varchar(30)) index",
		"column(group_id) type(bigint) index(idx_user_group)",
		"column(role_id) type(bigint) index(idx_user_group)",
	}

	dial := NewMySQLDialect()
	columns := make([]column.Column, 0, len(tags))
	for _, tag := range tags {
		col, err := dial.ParseColumn(tag)
		if err != nil {
			t.Fatalf("创建列失败: %v (tag: %s)", err, tag)
		}

		columns = append(columns, col)
	}

	indexer, ok := dial.(TableIndexer)
	if !ok {
		t.Fatal("MySQL 方言应实现 TableIndexer")
	}

	stmts := indexer.BuildTableIndexSQLs("users", columns)
	want := []string{
		"CREATE INDEX `users_name_idx` ON `users` (`name`);",
		"CREATE INDEX `idx_user_group` ON `users` (`group_id`, `role_id`);",
	}
	if len(stmts) != len(want) {
		t.Fatalf("索引语句数量 = %d，want %d: %#v", len(stmts), len(want), stmts)
	}

	for i := range want {
		if stmts[i] != want[i] {
			t.Errorf("索引语句[%d] = %q, want %q", i, stmts[i], want[i])
		}
	}

	if stmts := indexer.BuildTableIndexSQLs("", columns); stmts != nil {
		t.Errorf("空表名应返回 nil，实际: %#v", stmts)
	}

	// 带库名限定时，索引名只能取表名部分：索引名不允许跨段
	qualified := indexer.BuildTableIndexSQLs("tenant1.users", columns)
	if len(qualified) != len(want) {
		t.Fatalf("限定表名的索引语句数量 = %d，want %d: %#v", len(qualified), len(want), qualified)
	}

	if wantQualified := "CREATE INDEX `users_name_idx` ON `tenant1`.`users` (`name`);"; qualified[0] != wantQualified {
		t.Errorf("限定表名索引语句 = %q, want %q", qualified[0], wantQualified)
	}
}

// 测试主键提取
func TestMySQL_GetPrimaryKeyColumns(t *testing.T) {
	mysql := NewMySQLDialect()

	col1, _ := mysql.ParseColumn("column(id) type(int) pk")
	col2, _ := mysql.ParseColumn("column(name) type(varchar(30))")
	col3, _ := mysql.ParseColumn("column(group_id) type(int) pk")

	columns := []column.Column{col1, col2, col3}
	pkCols := mysql.GetPrimaryKeyColumns(columns)

	assertLen(t, len(pkCols), 2, "主键数量错误")
	assertEqual(t, pkCols[0].GetName(), "id", "主键列名错误")
	assertEqual(t, pkCols[1].GetName(), "group_id", "主键列名错误")
}

// 测试外键提取
func TestMySQL_GetForeignKeyColumns(t *testing.T) {
	mysql := NewMySQLDialect()

	col1, _ := mysql.ParseColumn("column(group_id) type(int) fk(group(id),'fk1',odc,ouc)")
	col2, _ := mysql.ParseColumn("column(role_id) type(int) fk(role(id),'fk2')")
	col3, _ := mysql.ParseColumn("column(dept_id) type(int) fk(department(id),'fk3')")
	col4, _ := mysql.ParseColumn("column(name) type(varchar(30))") // 无外键

	columns := []column.Column{col1, col2, col3, col4}
	fkCols := mysql.GetForeignKeyColumns(columns)

	assertLen(t, len(fkCols), 3, "外键表数量错误")
	assertLen(t, len(fkCols["group"]), 1, "group 表外键数量错误")
	assertLen(t, len(fkCols["role"]), 1, "role 表外键数量错误")
	assertLen(t, len(fkCols["department"]), 1, "department 表外键数量错误")
}

// 测试联合索引提取
func TestMySQL_GetUnionIndexColumns(t *testing.T) {
	mysql := NewMySQLDialect()

	col1, _ := mysql.ParseColumn("column(name) type(varchar(30)) index(idx_name)")
	col2, _ := mysql.ParseColumn("column(age) type(int) index(idx_name_age)")
	col3, _ := mysql.ParseColumn("column(email) type(varchar(255)) index(idx_name_age)")
	col4, _ := mysql.ParseColumn("column(phone) type(varchar(20)) index") // 匿名索引不被联合索引捕获

	columns := []column.Column{col1, col2, col3, col4}
	indexCols := mysql.GetUnionIndexColumns(columns)

	assertLen(t, len(indexCols), 2, "联合索引数量错误")
	assertLen(t, len(indexCols["idx_name"]), 1, "idx_name 索引列数量错误")
	assertLen(t, len(indexCols["idx_name_age"]), 2, "idx_name_age 索引列数量错误")
}

// 测试联合唯一约束提取
func TestMySQL_GetUnionUniqueColumns(t *testing.T) {
	mysql := NewMySQLDialect()

	col1, _ := mysql.ParseColumn("column(username) type(varchar(30)) unique(uk_username)")
	col2, _ := mysql.ParseColumn("column(email) type(varchar(255)) unique(uk_email_phone)")
	col3, _ := mysql.ParseColumn("column(phone) type(varchar(20)) unique(uk_email_phone)")
	col4, _ := mysql.ParseColumn("column(id_card) type(varchar(18)) unique") // 匿名唯一不被联合唯一捕获

	columns := []column.Column{col1, col2, col3, col4}
	uniqueCols := mysql.GetUnionUniqueColumns(columns)

	assertLen(t, len(uniqueCols), 2, "联合唯一约束数量错误")
	assertLen(t, len(uniqueCols["uk_username"]), 1, "uk_username 约束列数量错误")
	assertLen(t, len(uniqueCols["uk_email_phone"]), 2, "uk_email_phone 约束列数量错误")
}

// 测试数据类型转换
func TestMySQL_ConvertDatatype(t *testing.T) {
	mysql := NewMySQLDialect()

	testCases := []struct {
		name     string
		input    reflect.Type
		expected datatype.Datatype
	}{
		{"int", reflect.TypeOf(0), "BIGINT"},
		{"string", reflect.TypeOf(""), "VARCHAR(255)"},
		{"bool", reflect.TypeOf(false), "TINYINT(1)"},
		{"int64", reflect.TypeOf(int64(0)), "BIGINT"},
		{"float64", reflect.TypeOf(float64(0)), "DOUBLE"},
		{"[]byte", reflect.TypeOf([]byte{}), "BLOB"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := mysql.ConvertDatatype(tc.input)
			assertEqual(t, result, tc.expected, "数据类型转换错误")
		})
	}
}

// 测试特殊场景：无主键表
func TestMySQL_NoPrimaryKey(t *testing.T) {
	mysql := NewMySQLDialect()
	col, _ := mysql.ParseColumn("column(content) type(text) comment('日志内容')")
	columns := []column.Column{col}

	ctx := clause.NewContext()
	if err := mysql.BuildTableCreateSQL(ctx, "logs", columns); err != nil {
		t.Fatalf("生成 SQL 失败: %v", err)
	}
	sql, _ := ctx.GetResult()

	assertContains(t, sql, "CREATE TABLE `logs`", "表名错误")
	assertContains(t, sql, "`content` TEXT COMMENT '日志内容'", "字段错误")
	assertContains(t, sql, ");", "SQL 结尾错误，不应包含主键定义")
}

// 测试特殊场景：包含单引号的注释
func TestMySQL_CommentWithApostrophe(t *testing.T) {
	mysql := NewMySQLDialect()
	col, err := mysql.ParseColumn("column(description) type(varchar(255)) comment('user's comment')")
	if err != nil {
		t.Fatalf("创建列失败: %v", err)
	}

	ctx := clause.NewContext()
	if err := col.BuildCreateSQL(ctx); err != nil {
		t.Fatalf("生成字段 SQL 失败: %v", err)
	}

	sql, _ := ctx.GetResult()
	assertEqual(t, sql, "`description` VARCHAR(255) COMMENT 'user''s comment'", "单引号转义错误")
}

// 辅助断言函数
func assertEqual(t *testing.T, got, want interface{}, msg string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", msg, got, want)
	}
}

func assertLen(t *testing.T, got, want int, msg string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: length got %d, want %d", msg, got, want)
	}
}

func assertContains(t *testing.T, str, substr string, msg string) {
	t.Helper()
	if !strings.Contains(str, substr) {
		t.Errorf("%s: %q does not contain %q", msg, str, substr)
	}
}
