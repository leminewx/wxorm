package wxorm

import (
	"testing"

	"gitee.com/leminewx/wxorm/harness/examples"
)

// TestNormalizeDataType 验证方言类型写法归一化
func TestNormalizeDataType(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{"空值", "  ", ""},
		{"已是模型写法", "varchar(50)", "varchar(50)"},
		{"大小写与空白", "  VARCHAR(50)  ", "varchar(50)"},
		// PostgreSQL 的 format_type 输出
		{"character varying", "character varying(100)", "varchar(100)"},
		{"timestamp 不带时区", "timestamp without time zone", "timestamp"},
		{"timestamp 带时区", "timestamp with time zone", "timestamptz"},
		{"double precision", "double precision", "double"},
		{"integer 别名", "integer", "int"},
		{"boolean 别名", "boolean", "bool"},
		// MySQL 旧版本的整数显示宽度
		{"int 显示宽度", "int(11)", "int"},
		{"bigint 显示宽度", "bigint(20)", "bigint"},
		{"无符号整数保留符号", "int(11) unsigned", "int unsigned"},
		{"tinyint(1) 保留语义", "tinyint(1)", "tinyint"},
		// 长度信息必须保留，否则会漏掉精度变化
		{"numeric 精度", "numeric(10, 2)", "numeric(10, 2)"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeDataType(tc.input); got != tc.expected {
				t.Fatalf("normalizeDataType(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

// TestParseIndexName 验证从 CREATE INDEX 语句提取索引名
func TestParseIndexName(t *testing.T) {
	cases := []struct {
		stmt     string
		expected string
	}{
		{"CREATE INDEX `groups_name_idx` ON `groups` (`name`);", "groups_name_idx"},
		{`CREATE INDEX "idx_users_email" ON "users" ("email");`, "idx_users_email"},
		{"CREATE INDEX idx_group ON groups (id, name);", "idx_group"},
		{"ALTER TABLE groups ADD COLUMN name varchar(50);", ""},
		{"", ""},
	}

	for _, tc := range cases {
		if got := parseIndexName(tc.stmt); got != tc.expected {
			t.Errorf("parseIndexName(%q) = %q, want %q", tc.stmt, got, tc.expected)
		}
	}
}

// TestMigrate_DiffTable 验证迁移前后差异报告与缺失索引补建
func TestMigrate_DiffTable(t *testing.T) {
	engine, err := NewEngine("mysql", examples.DBSource)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	defer func() { _ = engine.Close() }()

	// 建表并写入基础列
	if err = engine.Migrate(&migrateBase{}); err != nil {
		t.Fatalf("Migrate base failed: %v", err)
	}

	defer func() {
		if err := engine.NewSession(&migrateNext{}).DeleteTable(); err != nil {
			t.Logf("cleanup failed: %v", err)
		}
	}()

	// 模型新增带索引的列，差异应报告出来但不执行
	diff, err := engine.DiffTable(&migrateNext{})
	if err != nil {
		t.Fatalf("DiffTable failed: %v", err)
	}

	if !diff.Exists {
		t.Fatal("表应视为已存在")
	}

	if len(diff.AddedColumns) != 1 || diff.AddedColumns[0] != "email" {
		t.Fatalf("AddedColumns = %v, want [email]", diff.AddedColumns)
	}

	if len(diff.MissingIndexes) != 1 || diff.MissingIndexes[0] != "migrate_diff_demo_email_idx" {
		t.Fatalf("MissingIndexes = %v, want [migrate_diff_demo_email_idx]", diff.MissingIndexes)
	}

	if diff.IsEmpty() {
		t.Fatal("存在差异时 IsEmpty 应为 false")
	}

	// 执行迁移后差异应清空（含索引补建）
	if err = engine.Migrate(&migrateNext{}); err != nil {
		t.Fatalf("Migrate next failed: %v", err)
	}

	diff, err = engine.DiffTable(&migrateNext{})
	if err != nil {
		t.Fatalf("DiffTable after migrate failed: %v", err)
	}

	if !diff.IsEmpty() {
		t.Fatalf("迁移后应无差异，实际: %+v", diff)
	}
}

// migrateBase 迁移测试的基础模型
type migrateBase struct {
	ID   int64  `orm:"column(id) type(bigint) pk auto"`
	Name string `orm:"column(name) type(varchar(32))"`
}

func (migrateBase) GetTableName() string { return "migrate_diff_demo" }

// migrateNext 在基础模型上新增带索引的列
type migrateNext struct {
	ID    int64  `orm:"column(id) type(bigint) pk auto"`
	Name  string `orm:"column(name) type(varchar(32))"`
	Email string `orm:"column(email) type(varchar(64)) index"`
}

func (migrateNext) GetTableName() string { return "migrate_diff_demo" }

// TestTableDiffIsEmpty 验证差异报告的空值判断
func TestTableDiffIsEmpty(t *testing.T) {
	var nilDiff *TableDiff
	if !nilDiff.IsEmpty() {
		t.Error("nil 报告应视为无差异")
	}

	// 表不存在不属于“结构一致”
	if (&TableDiff{}).IsEmpty() {
		t.Error("表不存在时不应视为无差异")
	}

	if !(&TableDiff{Exists: true}).IsEmpty() {
		t.Error("存在且无差异时应视为一致")
	}

	dirty := []*TableDiff{
		{Exists: true, AddedColumns: []string{"name"}},
		{Exists: true, DroppedColumns: []string{"age"}},
		{Exists: true, ChangedColumns: []ColumnDiff{{Name: "age", Expected: "int", Actual: "bigint"}}},
		{Exists: true, MissingIndexes: []string{"groups_name_idx"}},
	}

	for i, diff := range dirty {
		if diff.IsEmpty() {
			t.Errorf("第 %d 个报告存在差异，不应视为一致: %+v", i, diff)
		}
	}
}
