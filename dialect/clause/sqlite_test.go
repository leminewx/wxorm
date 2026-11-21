package clause

import (
	"strings"
	"testing"
)

func newSQLiteForTest(t *testing.T) SQLiteClause {
	t.Helper()

	cla := NewSQLiteClause()
	if cla == nil {
		t.Fatal("NewSQLiteClause 返回 nil")
	}

	return cla
}

func TestSQLiteQuoteAndPlaceholder(t *testing.T) {
	cla := newSQLiteForTest(t)

	cases := map[string]string{
		"user":         `"user"`,
		"user.name":    `"user"."name"`,
		`a"b`:          `"a""b"`,
		"DISTINCT id":  "DISTINCT \"id\"",
		"id DESC":      "\"id\" DESC",
		"id asc":       "\"id\" ASC",
		"column name":  "\"column name\"",
		"count(*)":     `"count(*)"`,
		"a.b.c":        `"a"."b"."c"`,
		"id":           `"id"`,
		"users.id ASC": "\"users\".\"id\" ASC",
	}
	for raw, want := range cases {
		if got := cla.QuoteIdentifier(raw); got != want {
			t.Errorf("QuoteIdentifier(%q) = %q, want %q", raw, got, want)
		}
	}

	if got := cla.GetPlaceholder(7); got != "?" {
		t.Errorf("GetPlaceholder = %q, want ?", got)
	}

	if got := cla.GetLikeOperator(); got != "LIKE" {
		t.Errorf("GetLikeOperator = %q, want LIKE", got)
	}

	var builder strings.Builder
	if err := cla.BuildPlaceholders(&builder, 3, 4); err != nil {
		t.Fatalf("BuildPlaceholders error: %v", err)
	}
	if want := "?, ?, ?, ?"; builder.String() != want {
		t.Errorf("BuildPlaceholders = %q, want %q", builder.String(), want)
	}
}

func TestSQLiteBuildInsertAndReplace(t *testing.T) {
	cla := newSQLiteForTest(t)

	ctx := NewContext()
	if err := cla.BuildInsertSQL(ctx, "user", false, "name", "age"); err != nil {
		t.Fatalf("BuildInsertSQL error: %v", err)
	}
	if sql, _ := ctx.GetResult(); sql != "INSERT INTO\n    \"user\" (\"name\", \"age\")" {
		t.Errorf("INSERT = %q", sql)
	}

	ctx = NewContext()
	if err := cla.BuildInsertSQL(ctx, "user", true, "name"); err != nil {
		t.Fatalf("BuildInsertSQL(ignore) error: %v", err)
	}
	if sql, _ := ctx.GetResult(); sql != "INSERT OR IGNORE INTO\n    \"user\" (\"name\")" {
		t.Errorf("INSERT OR IGNORE = %q", sql)
	}

	ctx = NewContext()
	if err := cla.BuildReplaceSQL(ctx, "user", "name"); err != nil {
		t.Fatalf("BuildReplaceSQL error: %v", err)
	}
	if sql, _ := ctx.GetResult(); sql != "REPLACE INTO\n    \"user\" (\"name\")" {
		t.Errorf("REPLACE = %q", sql)
	}
}

func TestSQLiteBuildValues(t *testing.T) {
	cla := newSQLiteForTest(t)
	ctx := NewContext()

	if err := cla.BuildValuesSQL(ctx, 0, []any{1, "wxorm"}, []any{2, "go"}); err != nil {
		t.Fatalf("BuildValuesSQL error: %v", err)
	}

	sql, args := ctx.GetResult()
	if want := "\nVALUES\n    (?, ?),\n    (?, ?)"; sql != want {
		t.Errorf("VALUES = %q, want %q", sql, want)
	}

	if len(args) != 4 || args[0] != 1 || args[3] != "go" {
		t.Errorf("参数不符合预期: %#v", args)
	}

	// 缓存命中路径同样要补齐参数
	ctx = NewContext()
	if err := cla.BuildValuesSQL(ctx, 0, []any{3, "x"}, []any{4, "y"}); err != nil {
		t.Fatalf("BuildValuesSQL(缓存) error: %v", err)
	}
	if sql, args := ctx.GetResult(); sql != "\nVALUES\n    (?, ?),\n    (?, ?)" || len(args) != 4 {
		t.Errorf("缓存命中后 SQL = %q, args = %#v", sql, args)
	}

	if err := cla.BuildValuesSQL(NewContext(), 0); err == nil {
		t.Error("没有数据时 BuildValuesSQL 应返回错误")
	}
}

// SQLite 的 UPSERT 要求显式冲突目标，且更新列与冲突目标可以不同
func TestSQLiteBuildUpsert(t *testing.T) {
	cla := newSQLiteForTest(t)

	ctx := NewContext()
	if err := cla.BuildUpsertSQL(ctx, []string{"id"}, []string{"name", "age"}); err != nil {
		t.Fatalf("BuildUpsertSQL error: %v", err)
	}

	sql, _ := ctx.GetResult()
	want := "\nON CONFLICT (\"id\") DO UPDATE SET\n    \"name\" = excluded.\"name\",\n    \"age\" = excluded.\"age\""
	if sql != want {
		t.Errorf("UPSERT = %q, want %q", sql, want)
	}

	// 缓存键必须同时包含冲突目标与更新列，否则 (a)(b) 与 (b)(a) 会互相串味
	ctx = NewContext()
	if err := cla.BuildUpsertSQL(ctx, []string{"name"}, []string{"id"}); err != nil {
		t.Fatalf("BuildUpsertSQL error: %v", err)
	}
	if sql, _ := ctx.GetResult(); sql != "\nON CONFLICT (\"name\") DO UPDATE SET\n    \"id\" = excluded.\"id\"" {
		t.Errorf("反向 UPSERT = %q", sql)
	}

	// 缺冲突目标时必须报错，不能降级为 DO NOTHING
	if err := cla.BuildUpsertSQL(NewContext(), nil, []string{"name"}); err == nil {
		t.Error("冲突目标为空时应返回错误")
	}
	if err := cla.BuildUpsertSQL(NewContext(), []string{"id"}, nil); err == nil {
		t.Error("更新列为空时应返回错误")
	}
}

func TestSQLiteBuildUpdateAndSet(t *testing.T) {
	cla := newSQLiteForTest(t)

	ctx := NewContext()
	if err := cla.BuildUpdateSQL(ctx, "user"); err != nil {
		t.Fatalf("BuildUpdateSQL error: %v", err)
	}
	if sql, _ := ctx.GetResult(); sql != "UPDATE\n    \"user\"" {
		t.Errorf("UPDATE = %q", sql)
	}

	ctx = NewContext()
	if err := cla.BuildSetSQL(ctx, 0, map[string]any{"name": "wxorm", "age": 12}); err != nil {
		t.Fatalf("BuildSetSQL error: %v", err)
	}

	sql, args := ctx.GetResult()
	if want := "\nSET\n    \"age\" = ?, \"name\" = ?"; sql != want {
		t.Errorf("SET = %q, want %q", sql, want)
	}
	if len(args) != 2 || args[0] != 12 || args[1] != "wxorm" {
		t.Errorf("SET 参数顺序必须与占位符一致: %#v", args)
	}
}

func TestSQLiteBuildSelectFamily(t *testing.T) {
	cla := newSQLiteForTest(t)

	cases := []struct {
		name string
		run  func(*Context) error
		want string
	}{
		{"select all", func(ctx *Context) error { return cla.BuildSelectSQL(ctx, "user") }, "SELECT\n    *"},
		{"select columns", func(ctx *Context) error { return cla.BuildSelectSQL(ctx, "user", "id", "name") }, "SELECT\n    \"id\", \"name\""},
		{"count", func(ctx *Context) error { return cla.BuildCountSQL(ctx) }, "SELECT\n    COUNT(*)"},
		{"count column", func(ctx *Context) error { return cla.BuildCountSQL(ctx, "id") }, "SELECT\n    COUNT(\"id\")"},
		{"from", func(ctx *Context) error { return cla.BuildFromSQL(ctx, "user") }, "\nFROM\n    \"user\""},
		{"where", func(ctx *Context) error { return cla.BuildWhereSQL(ctx) }, "\nWHERE\n    "},
		{"group", func(ctx *Context) error { return cla.BuildGroupBySQL(ctx, "city") }, "\nGROUP BY\n    \"city\""},
		{"having", func(ctx *Context) error { return cla.BuildHavingSQL(ctx) }, "\nHAVING\n    "},
		{"order", func(ctx *Context) error { return cla.BuildOrderBySQL(ctx, "id DESC") }, "\nORDER BY\n    \"id\" DESC"},
		{"limit", func(ctx *Context) error { return cla.BuildLimitSQL(ctx, 10) }, "\nLIMIT\n    10"},
		{"offset", func(ctx *Context) error { return cla.BuildOffsetSQL(ctx, 20) }, "\nOFFSET\n    20"},
		{"delete", func(ctx *Context) error { return cla.BuildDeleteSQL(ctx, "user") }, "DELETE FROM\n    \"user\""},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewContext()
			if err := tt.run(ctx); err != nil {
				t.Fatalf("build error: %v", err)
			}

			if sql, _ := ctx.GetResult(); sql != tt.want {
				t.Errorf("SQL = %q, want %q", sql, tt.want)
			}

			// 再跑一次，覆盖缓存命中路径
			ctx = NewContext()
			if err := tt.run(ctx); err != nil {
				t.Fatalf("build error (缓存): %v", err)
			}
			if sql, _ := ctx.GetResult(); sql != tt.want {
				t.Errorf("SQL (缓存) = %q, want %q", sql, tt.want)
			}
		})
	}

	// LIMIT 必须标记，否则会话层无法判断 OFFSET 是否合法
	ctx := NewContext()
	if err := cla.BuildLimitSQL(ctx, 5); err != nil {
		t.Fatalf("BuildLimitSQL error: %v", err)
	}
	if !ctx.HasLimit() {
		t.Error("LIMIT 之后应标记 hasLimit")
	}
}

func TestSQLiteJSONConditions(t *testing.T) {
	cla := newSQLiteForTest(t)

	ctx := NewContext()
	if err := cla.BuildJSONExtractSQL(ctx, "payload", "$.user.name"); err != nil {
		t.Fatalf("BuildJSONExtractSQL error: %v", err)
	}
	if sql, _ := ctx.GetResult(); sql != `json_extract("payload", '$.user.name')` {
		t.Errorf("JSON_EXTRACT = %q", sql)
	}

	ctx = NewContext()
	if err := cla.BuildJSONContainsSQL(ctx, "payload", "$.tags[0]", "vip"); err != nil {
		t.Fatalf("BuildJSONContainsSQL error: %v", err)
	}

	sql, args := ctx.GetResult()
	want := `EXISTS (SELECT 1 FROM json_each("payload", '$.tags[0]') WHERE json_each.value = ?)`
	if sql != want {
		t.Errorf("JSON_CONTAINS = %q, want %q", sql, want)
	}
	if len(args) != 1 || args[0] != "vip" {
		t.Errorf("JSON_CONTAINS 参数不符合预期: %#v", args)
	}
}

// JSON 路径来自调用方输入，不能通过单引号闭合字面量
func TestSQLiteJSONPathEscape(t *testing.T) {
	cla := newSQLiteForTest(t)
	ctx := NewContext()

	if err := cla.BuildJSONExtractSQL(ctx, "payload", "a') OR 1=1 --"); err != nil {
		t.Fatalf("BuildJSONExtractSQL error: %v", err)
	}

	if sql, _ := ctx.GetResult(); sql != `json_extract("payload", '$.a'') OR 1=1 --')` {
		t.Errorf("路径中的单引号未被转义: %s", sql)
	}
}

// SQLite 不具备这些能力，断言必须失败：调用方应拿到 ErrUnsupportedFeature，
// 而不是一段「看起来合法、执行报错」的 SQL
func TestSQLiteLacksUnsupportedCapabilities(t *testing.T) {
	var cla any = newSQLiteForTest(t)

	if _, ok := cla.(Upserter); ok {
		t.Error("SQLite 不应实现 Upserter，DUPLICATE 在 SQLite 下必须报不支持")
	}
	if _, ok := cla.(IgnoreInserter); ok {
		t.Error("SQLite 的 IGNORE 已内联在 INSERT OR IGNORE 中，不应再实现 IgnoreInserter")
	}
	if _, ok := cla.(ReturningInserter); ok {
		t.Error("SQLite 通过 LastInsertId 回填主键，不应实现 ReturningInserter")
	}
	if _, ok := cla.(FullTextSearcher); ok {
		t.Error("SQLite 的全文检索依赖 FTS5 虚拟表，不应实现 FullTextSearcher")
	}

	if _, ok := cla.(Replacer); !ok {
		t.Error("SQLite 应实现 Replacer")
	}
	if _, ok := cla.(ConflictUpserter); !ok {
		t.Error("SQLite 应实现 ConflictUpserter")
	}
	if _, ok := cla.(JSONQuerier); !ok {
		t.Error("SQLite 应实现 JSONQuerier（JSON1 扩展）")
	}
}
