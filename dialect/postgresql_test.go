package dialect

import (
	"strings"
	"testing"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/dialect/column"
)

func newPostgresTableColumns(t *testing.T) []column.Column {
	t.Helper()

	tags := []string{
		"column('id') type(bigint) pk auto comment('主键')",
		"column('name') type(varchar(64)) notnull index comment('名称')",
		"column('email') type(varchar(255)) unique",
		"column('group_id') type(bigint) index(idx_user_group)",
		"column('role_id') type(bigint) index(idx_user_group)",
		"column('created_at') type(timestamptz) created",
	}

	dial := NewPostgresDialect()
	columns := make([]column.Column, 0, len(tags))
	for _, tag := range tags {
		col, err := dial.ParseColumn(tag)
		if err != nil {
			t.Fatalf("ParseColumn(%q) error: %v", tag, err)
		}

		columns = append(columns, col)
	}

	return columns
}

func TestPostgresBuildTableCreateSQL(t *testing.T) {
	dial := NewPostgresDialect()
	columns := newPostgresTableColumns(t)

	ctx := clause.NewContext()
	if err := dial.BuildTableCreateSQL(ctx, "user", columns, TableMetadata{Comment: "用户表"}); err != nil {
		t.Fatalf("BuildTableCreateSQL error: %v", err)
	}

	sql, _ := ctx.GetResult()

	// 自增主键映射为 BIGSERIAL，PG 会隐式创建序列并绑定默认值
	if !strings.Contains(sql, `"id" BIGSERIAL`) {
		t.Errorf("自增主键应为 BIGSERIAL，实际:\n%s", sql)
	}

	// 建表后时间字段使用 DEFAULT CURRENT_TIMESTAMP
	if !strings.Contains(sql, `"created_at" TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP`) {
		t.Errorf("时间字段默认值不正确，实际:\n%s", sql)
	}

	if !strings.Contains(sql, `PRIMARY KEY ("id")`) {
		t.Errorf("缺少主键约束，实际:\n%s", sql)
	}

	// MySQL 专有的表选项不应出现在 PostgreSQL DDL 中
	for _, unwanted := range []string{"ENGINE", "CHARSET", "AUTO_INCREMENT", "COMMENT '"} {
		if strings.Contains(sql, unwanted) {
			t.Errorf("PostgreSQL DDL 不应包含 %q，实际:\n%s", unwanted, sql)
		}
	}

	if !strings.HasSuffix(sql, ");") {
		t.Errorf("DDL 应以 ); 结尾，实际:\n%s", sql)
	}
}

func TestPostgresBuildTableIndexSQLs(t *testing.T) {
	dial := NewPostgresDialect()
	columns := newPostgresTableColumns(t)

	indexer, ok := dial.(TableIndexer)
	if !ok {
		t.Fatal("PostgreSQL 方言应实现 TableIndexer")
	}

	stmts := indexer.BuildTableIndexSQLs("user", columns)
	if len(stmts) != 2 {
		t.Fatalf("期望 2 条索引语句，实际 %d: %#v", len(stmts), stmts)
	}

	// 未命名 index 标记生成单列索引，名字按 表名_字段名_idx 生成
	if want := `CREATE INDEX "user_name_idx" ON "user" ("name");`; stmts[0] != want {
		t.Errorf("单列索引 = %q, want %q", stmts[0], want)
	}

	// 同名 index 标记生成复合索引
	if want := `CREATE INDEX "idx_user_group" ON "user" ("group_id", "role_id");`; stmts[1] != want {
		t.Errorf("复合索引 = %q, want %q", stmts[1], want)
	}
}

func TestPostgresBuildTableCommentSQLs(t *testing.T) {
	dial := NewPostgresDialect()
	columns := newPostgresTableColumns(t)

	commenter, ok := dial.(TableCommenter)
	if !ok {
		t.Fatal("PostgreSQL 方言应实现 TableCommenter")
	}

	stmts := commenter.BuildTableCommentSQLs("user", columns, TableMetadata{Comment: "用户表"})
	if len(stmts) != 3 {
		t.Fatalf("期望 3 条注释语句，实际 %d: %#v", len(stmts), stmts)
	}

	if want := `COMMENT ON TABLE "user" IS '用户表';`; stmts[0] != want {
		t.Errorf("表注释 = %q, want %q", stmts[0], want)
	}

	if want := `COMMENT ON COLUMN "user"."id" IS '主键';`; stmts[1] != want {
		t.Errorf("列注释 = %q, want %q", stmts[1], want)
	}
}

func TestPostgresBuildTableQuerySQL(t *testing.T) {
	dial := NewPostgresDialect()

	// 未指定 schema 时按 current_schema() 过滤，表名走占位符
	ctx := clause.NewContext()
	if err := dial.BuildTableQuerySQL(ctx, "", "user"); err != nil {
		t.Fatalf("BuildTableQuerySQL error: %v", err)
	}

	sql, args := ctx.GetResult()
	want := "SELECT\n    table_name\nFROM\n    information_schema.tables\nWHERE\n    table_schema = current_schema() AND table_name = $1"
	if sql != want {
		t.Errorf("SQL = %q, want %q", sql, want)
	}

	if len(args) != 1 || args[0] != "user" {
		t.Errorf("表名应作为参数传入，实际: %#v", args)
	}

	// 显式指定 schema 时两个条件都走占位符，不再拼字符串
	ctx = clause.NewContext()
	if err := dial.BuildTableQuerySQL(ctx, "tenant1", "user"); err != nil {
		t.Fatalf("BuildTableQuerySQL error: %v", err)
	}

	sql, args = ctx.GetResult()
	if !strings.Contains(sql, "table_schema = $1 AND table_name = $2") {
		t.Errorf("schema 与表名都应参数化，实际:\n%s", sql)
	}

	if len(args) != 2 || args[0] != "tenant1" || args[1] != "user" {
		t.Errorf("参数顺序应为 [schema, table]，实际: %#v", args)
	}
}

// 方言能力矩阵：语法差异走通用子句，能力差异走可选接口
func TestDialectCapabilityMatrix(t *testing.T) {
	mysqlDialect := NewMySQLDialect()
	postgresDialect := NewPostgresDialect()

	cases := []struct {
		name      string
		assertion func(any) bool
		mysql     bool
		postgres  bool
	}{
		{"Replacer", func(v any) bool { _, ok := v.(clause.Replacer); return ok }, true, false},
		// DUPLICATE 是 MySQL 语义（冲突目标由唯一索引隐式确定），PostgreSQL 不支持；
		// 跨方言 UPSERT 走 ConflictUpserter
		{"Upserter", func(v any) bool { _, ok := v.(clause.Upserter); return ok }, true, false},
		{"ConflictUpserter", func(v any) bool { _, ok := v.(clause.ConflictUpserter); return ok }, false, true},
		{"IgnoreInserter", func(v any) bool { _, ok := v.(clause.IgnoreInserter); return ok }, false, true},
		{"ReturningInserter", func(v any) bool { _, ok := v.(clause.ReturningInserter); return ok }, false, true},
		{"BatchReturningInserter", func(v any) bool { _, ok := v.(clause.BatchReturningInserter); return ok }, false, true},
		{"JSONQuerier", func(v any) bool { _, ok := v.(clause.JSONQuerier); return ok }, true, true},
		{"FullTextSearcher", func(v any) bool { _, ok := v.(clause.FullTextSearcher); return ok }, true, true},
		// MySQL 同样不支持建表内联 INDEX，与 PG/SQLite 一样改为建表后逐条 CREATE INDEX
		{"TableIndexer", func(v any) bool { _, ok := v.(TableIndexer); return ok }, true, true},
		{"TableCommenter", func(v any) bool { _, ok := v.(TableCommenter); return ok }, false, true},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.assertion(mysqlDialect); got != tt.mysql {
				t.Errorf("MySQL 方言 %s = %v, want %v", tt.name, got, tt.mysql)
			}

			if got := tt.assertion(postgresDialect); got != tt.postgres {
				t.Errorf("PostgreSQL 方言 %s = %v, want %v", tt.name, got, tt.postgres)
			}
		})
	}
}
