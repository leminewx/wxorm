package dialect

import (
	"errors"
	"strings"
	"testing"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/dialect/column"
)

func newSQLiteForTest(t *testing.T) Dialect {
	t.Helper()

	dial := NewSQLiteDialect()
	if dial == nil {
		t.Fatal("NewSQLiteDialect 返回 nil")
	}

	return dial
}

// parseSQLiteColumns 解析一组列标签，供建表测试使用
func parseSQLiteColumns(t *testing.T, dial Dialect, tags ...string) []column.Column {
	t.Helper()

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

func buildSQLiteCreateSQL(t *testing.T, dial Dialect, table string, columns []column.Column) string {
	t.Helper()

	ctx := clause.NewContext()
	if err := dial.BuildTableCreateSQL(ctx, table, columns); err != nil {
		t.Fatalf("BuildTableCreateSQL error: %v", err)
	}

	sql, _ := ctx.GetResult()
	return sql
}

func TestSQLiteDialectRegistered(t *testing.T) {
	if Get(SQLite) == nil {
		t.Fatal("sqlite 方言未注册")
	}

	if Get(SQLite3) != Get(SQLite) {
		t.Error("sqlite 与 sqlite3 应共用同一个方言实例")
	}

	if _, err := Lookup(SQLite3); err != nil {
		t.Errorf("Lookup(sqlite3) error: %v", err)
	}

	if got := Get(SQLite).GetDriverName(); got != SQLite {
		t.Errorf("GetDriverName = %q, want %q", got, SQLite)
	}
}

// 自增主键必须内联，且不能再出现表级 PRIMARY KEY
func TestSQLiteCreateTableWithAutoPrimaryKey(t *testing.T) {
	dial := newSQLiteForTest(t)
	columns := parseSQLiteColumns(t, dial,
		"column(id) type(integer) pk auto",
		"column(name) type(text) notnull",
	)

	sql := buildSQLiteCreateSQL(t, dial, "users", columns)
	want := "CREATE TABLE \"users\" (\n" +
		"    -- 命名字段\n" +
		"    \"id\" INTEGER PRIMARY KEY AUTOINCREMENT,\n" +
		"    \"name\" TEXT NOT NULL\n" +
		");"
	if sql != want {
		t.Errorf("CREATE TABLE = \n%s\nwant:\n%s", sql, want)
	}

	if strings.Contains(sql, "PRIMARY KEY (\"id\")") {
		t.Errorf("自增主键不应重复声明为表级主键: %s", sql)
	}
}

// 非自增主键走表级 PRIMARY KEY，复合主键同理
func TestSQLiteCreateTableWithTableLevelPrimaryKey(t *testing.T) {
	dial := newSQLiteForTest(t)
	columns := parseSQLiteColumns(t, dial,
		"column(tenant_id) type(integer) pk",
		"column(user_id) type(integer) pk",
	)

	sql := buildSQLiteCreateSQL(t, dial, "members", columns)
	if want := `PRIMARY KEY ("tenant_id", "user_id")`; !strings.Contains(sql, want) {
		t.Errorf("复合主键缺失: %q，实际:\n%s", want, sql)
	}
}

// SQLite 无法表达「复合主键 + 自增」，必须显式报错而不是生成非法 DDL
func TestSQLiteCreateTableRejectsCompositeAutoPrimaryKey(t *testing.T) {
	dial := newSQLiteForTest(t)
	columns := parseSQLiteColumns(t, dial,
		"column(id) type(integer) pk auto",
		"column(tenant_id) type(integer) pk",
	)

	err := dial.BuildTableCreateSQL(clause.NewContext(), "members", columns)
	if !errors.Is(err, ErrSQLiteCompositeAutoPrimaryKey) {
		t.Fatalf("期望 ErrSQLiteCompositeAutoPrimaryKey，实际: %v", err)
	}
}

func TestSQLiteCreateTableWithUniques(t *testing.T) {
	dial := newSQLiteForTest(t)
	columns := parseSQLiteColumns(t, dial,
		"column(id) type(integer) pk auto",
		"column(email) type(text) unique('uk_email')",
		"column(phone) type(text) unique('uk_email')",
	)

	sql := buildSQLiteCreateSQL(t, dial, "users", columns)
	if want := `CONSTRAINT "uk_email" UNIQUE ("email", "phone")`; !strings.Contains(sql, want) {
		t.Errorf("复合唯一约束缺失: %q，实际:\n%s", want, sql)
	}
}

func TestSQLiteCreateTableWithForeignKeys(t *testing.T) {
	dial := newSQLiteForTest(t)
	columns := parseSQLiteColumns(t, dial,
		"column(id) type(integer) pk auto",
		"column(role_id) type(integer) fk(role(id),'fk_user_role',odc,ouc)",
	)

	sql := buildSQLiteCreateSQL(t, dial, "users", columns)
	for _, want := range []string{
		`CONSTRAINT "fk_user_role"`,
		`FOREIGN KEY ("role_id")`,
		`REFERENCES "role" ("id")`,
		"ON DELETE CASCADE",
		"ON UPDATE CASCADE",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("外键定义缺失 %q，实际:\n%s", want, sql)
		}
	}
}

// 表级选项（ENGINE/CHARSET/COLLATE）在 SQLite 中没有对应语法，必须被忽略
func TestSQLiteCreateTableIgnoresTableMetadata(t *testing.T) {
	dial := newSQLiteForTest(t)
	columns := parseSQLiteColumns(t, dial, "column(id) type(integer) pk auto")

	ctx := clause.NewContext()
	metadata := TableMetadata{Engine: "InnoDB", Charset: "utf8mb4", Collate: "utf8mb4_general_ci", Comment: "用户表"}
	if err := dial.BuildTableCreateSQL(ctx, "users", columns, metadata); err != nil {
		t.Fatalf("BuildTableCreateSQL error: %v", err)
	}

	sql, _ := ctx.GetResult()
	for _, unwanted := range []string{"ENGINE", "CHARSET", "COLLATE", "COMMENT"} {
		if strings.Contains(sql, unwanted) {
			t.Errorf("SQLite 建表不应包含 %q，实际:\n%s", unwanted, sql)
		}
	}
}

func TestSQLiteBuildTableQuerySQL(t *testing.T) {
	dial := newSQLiteForTest(t)
	ctx := clause.NewContext()

	if err := dial.BuildTableQuerySQL(ctx, "", "users"); err != nil {
		t.Fatalf("BuildTableQuerySQL error: %v", err)
	}

	sql, args := ctx.GetResult()
	want := "SELECT\n    name\nFROM\n    sqlite_master\nWHERE\n    type = 'table' AND name = ?"
	if sql != want {
		t.Errorf("SQL = %q, want %q", sql, want)
	}

	if len(args) != 1 || args[0] != "users" {
		t.Errorf("表名应作为参数传入，实际: %#v", args)
	}

	// 表名中的单引号、注释符都只能作为参数值，不能进入 SQL 文本
	ctx = clause.NewContext()
	injection := "users' OR 1=1 --"
	if err := dial.BuildTableQuerySQL(ctx, "", injection); err != nil {
		t.Fatalf("BuildTableQuerySQL error: %v", err)
	}

	sql, args = ctx.GetResult()
	if strings.Contains(sql, injection) {
		t.Errorf("恶意表名不应拼进 SQL: %s", sql)
	}

	if len(args) != 1 || args[0] != injection {
		t.Errorf("恶意表名应原样作为参数，实际: %#v", args)
	}

	if err := dial.BuildTableQuerySQL(clause.NewContext(), "", ""); !errors.Is(err, ErrTableNameIsEmpty) {
		t.Errorf("空表名应返回 ErrTableNameIsEmpty，实际: %v", err)
	}
}

// SQLite 不支持内联 INDEX，索引要由会话在建表后逐条执行
func TestSQLiteBuildTableIndexSQLs(t *testing.T) {
	dial := newSQLiteForTest(t)
	columns := parseSQLiteColumns(t, dial,
		"column(name) type(text) index",
		"column(city) type(text) index('idx_city_age')",
		"column(age) type(integer) index('idx_city_age')",
	)

	indexer, ok := dial.(TableIndexer)
	if !ok {
		t.Fatal("SQLite 方言应实现 TableIndexer")
	}

	stmts := indexer.BuildTableIndexSQLs("users", columns)
	want := []string{
		`CREATE INDEX "users_name_idx" ON "users" ("name");`,
		`CREATE INDEX "idx_city_age" ON "users" ("city", "age");`,
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
}

func TestSQLiteCapabilityMatrix(t *testing.T) {
	mysqlDialect := Get("mysql")
	postgresDialect := Get("postgres")
	sqliteDialect := Get(SQLite)

	cases := []struct {
		name                  string
		assertion             func(any) bool
		mysql, postgres, s3db bool
	}{
		{"Replacer", func(v any) bool { _, ok := v.(clause.Replacer); return ok }, true, false, true},
		// SQLite 的 DO UPDATE 同样需要冲突目标，DUPLICATE 不可用
		{"Upserter", func(v any) bool { _, ok := v.(clause.Upserter); return ok }, true, false, false},
		{"ConflictUpserter", func(v any) bool { _, ok := v.(clause.ConflictUpserter); return ok }, false, true, true},
		// SQLite 的 IGNORE 内联在 INSERT OR IGNORE 中，与 MySQL 一样不需要额外子句
		{"IgnoreInserter", func(v any) bool { _, ok := v.(clause.IgnoreInserter); return ok }, false, true, false},
		// SQLite 用 LastInsertId 回填单条主键，不需要 RETURNING；批量插入才用 RETURNING
		{"ReturningInserter", func(v any) bool { _, ok := v.(clause.ReturningInserter); return ok }, false, true, false},
		// MySQL 不支持 RETURNING，批量插入只能拿最后一条自增 ID
		{"BatchReturningInserter", func(v any) bool { _, ok := v.(clause.BatchReturningInserter); return ok }, false, true, true},
		{"JSONQuerier", func(v any) bool { _, ok := v.(clause.JSONQuerier); return ok }, true, true, true},
		// SQLite 的全文检索需要 FTS5 虚拟表，单条子句无法表达
		{"FullTextSearcher", func(v any) bool { _, ok := v.(clause.FullTextSearcher); return ok }, true, true, false},
		// MySQL 同样不支持建表内联 INDEX，与 PG/SQLite 一样改为建表后逐条 CREATE INDEX
		{"TableIndexer", func(v any) bool { _, ok := v.(TableIndexer); return ok }, true, true, true},
		{"TableCommenter", func(v any) bool { _, ok := v.(TableCommenter); return ok }, false, true, false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.assertion(mysqlDialect); got != tt.mysql {
				t.Errorf("MySQL 方言 %s = %v, want %v", tt.name, got, tt.mysql)
			}
			if got := tt.assertion(postgresDialect); got != tt.postgres {
				t.Errorf("PostgreSQL 方言 %s = %v, want %v", tt.name, got, tt.postgres)
			}
			if got := tt.assertion(sqliteDialect); got != tt.s3db {
				t.Errorf("SQLite 方言 %s = %v, want %v", tt.name, got, tt.s3db)
			}
		})
	}
}
