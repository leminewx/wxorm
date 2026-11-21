package clause

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

func newPostgresForTest(t *testing.T) PostgresClause {
	t.Helper()

	pg := NewPostgresClause()
	if pg == nil {
		t.Fatal("NewPostgresClause() returned nil")
	}

	return pg
}

func TestPostgresPlaceholderAndLike(t *testing.T) {
	pg := newPostgresForTest(t)

	if op := pg.GetLikeOperator(); op != "ILIKE" {
		t.Errorf("GetLikeOperator() = %q, want ILIKE", op)
	}

	for _, index := range []int{0, 1, 7} {
		if got, want := pg.GetPlaceholder(index), "$"+strconv.Itoa(index); got != want {
			t.Errorf("GetPlaceholder(%d) = %q, want %q", index, got, want)
		}
	}
}

func TestPostgresBuildPlaceholders(t *testing.T) {
	pg := newPostgresForTest(t)

	var builder strings.Builder
	if err := pg.BuildPlaceholders(&builder, 2, 3); err != nil {
		t.Fatalf("BuildPlaceholders error: %v", err)
	}

	if got, want := builder.String(), "$3, $4, $5"; got != want {
		t.Errorf("BuildPlaceholders = %q, want %q", got, want)
	}
}

func TestPostgresQuoteIdentifier(t *testing.T) {
	pg := newPostgresForTest(t)

	tests := []struct {
		input    string
		expected string
	}{
		{"id", `"id"`},
		{"users.id", `"users"."id"`},
		{"db.users.id", `"db"."users"."id"`},
		{`special"name`, `"special""name"`},
		{"DISTINCT name", `DISTINCT "name"`},
		{"age DESC", `"age" DESC`},
		{"empty", `"empty"`},
	}

	for _, tt := range tests {
		if got := pg.QuoteIdentifier(tt.input); got != tt.expected {
			t.Errorf("QuoteIdentifier(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestPostgresInsertWithIgnoreAndReturning(t *testing.T) {
	pg := newPostgresForTest(t)
	ctx := NewContext()

	if err := pg.BuildInsertSQL(ctx, "users", true, "name", "age"); err != nil {
		t.Fatalf("BuildInsertSQL error: %v", err)
	}
	if err := pg.BuildValuesSQL(ctx, 0, []any{"wxorm", 12}); err != nil {
		t.Fatalf("BuildValuesSQL error: %v", err)
	}
	if err := pg.BuildInsertIgnoreSQL(ctx); err != nil {
		t.Fatalf("BuildInsertIgnoreSQL error: %v", err)
	}
	if err := pg.BuildInsertReturningSQL(ctx, "id"); err != nil {
		t.Fatalf("BuildInsertReturningSQL error: %v", err)
	}

	sql, args := ctx.GetResult()
	// ignore 必须内联为普通的 INSERT：IGNORE 由 ON CONFLICT 在 VALUES 之后表达
	for _, want := range []string{
		"INSERT INTO\n    \"users\" (\"name\", \"age\")",
		"\nVALUES\n    ($1, $2)",
		"\nON CONFLICT DO NOTHING",
		"\nRETURNING \"id\"",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL 缺失片段 %q，实际为:\n%s", want, sql)
		}
	}

	if strings.Contains(sql, "INSERT IGNORE") {
		t.Errorf("PostgreSQL 不支持 INSERT IGNORE，实际 SQL:\n%s", sql)
	}

	if len(args) != 2 || args[0] != "wxorm" || args[1] != 12 {
		t.Errorf("参数不符合预期: %#v", args)
	}
}

func TestPostgresValuesPlaceholderStartsAtArgIndex(t *testing.T) {
	pg := newPostgresForTest(t)
	ctx := NewContext()

	// 先占用两个参数，模拟 SET 子句已写入参数的情形
	ctx.AddArgs("a", "b")

	if err := pg.BuildValuesSQL(ctx, 2, []any{1}, []any{2}); err != nil {
		t.Fatalf("BuildValuesSQL error: %v", err)
	}

	sql, args := ctx.GetResult()
	if want := "\nVALUES\n    ($3),\n    ($4)"; sql != want {
		t.Errorf("BuildValuesSQL = %q, want %q", sql, want)
	}

	if len(args) != 4 {
		t.Errorf("参数数量 = %d, want 4", len(args))
	}
}

// PostgreSQL 不实现 MySQL 语义的 Upserter：DUPLICATE 在 PG 下必须显式失败，
// 而不是把「更新列」当成「冲突目标」生成一条看似合法的 SQL。
func TestPostgresDoesNotImplementUpserter(t *testing.T) {
	var cla any = newPostgresForTest(t)

	if _, ok := cla.(Upserter); ok {
		t.Error("PostgreSQL 不应实现 Upserter，否则 DUPLICATE 会静默产生错位的冲突目标")
	}

	if _, ok := cla.(ConflictUpserter); !ok {
		t.Error("PostgreSQL 应实现 ConflictUpserter")
	}
}

// UPSERT 允许冲突目标与更新列分离：按主键冲突，只覆盖指定列
func TestPostgresUpsertSeparatesConflictAndUpdate(t *testing.T) {
	pg := newPostgresForTest(t)

	t.Run("conflict differs from update", func(t *testing.T) {
		ctx := NewContext()
		if err := pg.BuildUpsertSQL(ctx, []string{"id"}, []string{"name", "age"}); err != nil {
			t.Fatalf("BuildUpsertSQL error: %v", err)
		}

		sql, _ := ctx.GetResult()
		want := "\nON CONFLICT (\"id\") DO UPDATE SET\n    \"name\" = EXCLUDED.\"name\",\n    \"age\" = EXCLUDED.\"age\""
		if sql != want {
			t.Errorf("BuildUpsertSQL = %q, want %q", sql, want)
		}
	})

	t.Run("composite conflict target", func(t *testing.T) {
		ctx := NewContext()
		if err := pg.BuildUpsertSQL(ctx, []string{"tenant_id", "id"}, []string{"name"}); err != nil {
			t.Fatalf("BuildUpsertSQL error: %v", err)
		}

		sql, _ := ctx.GetResult()
		want := "\nON CONFLICT (\"tenant_id\", \"id\") DO UPDATE SET\n    \"name\" = EXCLUDED.\"name\""
		if sql != want {
			t.Errorf("BuildUpsertSQL = %q, want %q", sql, want)
		}
	})

	t.Run("empty conflict target is an error", func(t *testing.T) {
		ctx := NewContext()
		if err := pg.BuildUpsertSQL(ctx, nil, []string{"name"}); !errors.Is(err, ErrValuesIsEmpty) {
			t.Errorf("期望 ErrValuesIsEmpty，实际: %v", err)
		}
	})

	t.Run("empty update is an error", func(t *testing.T) {
		ctx := NewContext()
		if err := pg.BuildUpsertSQL(ctx, []string{"id"}, nil); !errors.Is(err, ErrValuesIsEmpty) {
			t.Errorf("期望 ErrValuesIsEmpty，实际: %v", err)
		}
	})
}

func TestPostgresSetAndUpdate(t *testing.T) {
	pg := newPostgresForTest(t)
	ctx := NewContext()

	if err := pg.BuildUpdateSQL(ctx, "users"); err != nil {
		t.Fatalf("BuildUpdateSQL error: %v", err)
	}
	if err := pg.BuildSetSQL(ctx, 0, map[string]any{"name": "wxorm", "age": 1}); err != nil {
		t.Fatalf("BuildSetSQL error: %v", err)
	}

	sql, args := ctx.GetResult()
	want := "UPDATE\n    \"users\"\nSET\n    \"age\" = $1, \"name\" = $2"
	if sql != want {
		t.Errorf("UPDATE SQL = %q, want %q", sql, want)
	}

	if len(args) != 2 || args[0] != 1 || args[1] != "wxorm" {
		t.Errorf("SET 参数顺序必须与占位符一致: %#v", args)
	}
}

func TestPostgresJSONConditions(t *testing.T) {
	pg := newPostgresForTest(t)
	ctx := NewContext()

	if err := pg.BuildJSONExtractSQL(ctx, "payload", "$.user.name"); err != nil {
		t.Fatalf("BuildJSONExtractSQL error: %v", err)
	}

	if err := pg.BuildJSONContainsSQL(ctx, "payload", "$.tags[0]", "vip"); err != nil {
		t.Fatalf("BuildJSONContainsSQL error: %v", err)
	}

	sql, args := ctx.GetResult()
	if want := `("payload" #>> '{user,name}')`; !strings.Contains(sql, want) {
		t.Errorf("JSON 取值应为 %q，实际:\n%s", want, sql)
	}

	if want := `("payload" #> '{tags,0}') @> $1::jsonb`; !strings.Contains(sql, want) {
		t.Errorf("JSON 包含判断应为 %q，实际:\n%s", want, sql)
	}

	if len(args) != 1 || args[0] != "vip" {
		t.Errorf("JSON 参数不符合预期: %#v", args)
	}
}

func TestPostgresFullTextSearch(t *testing.T) {
	pg := newPostgresForTest(t)

	t.Run("natural language", func(t *testing.T) {
		ctx := NewContext()
		if err := pg.BuildFullTextSearchSQL(ctx, []string{"title", "body"}, "wxorm orm", ""); err != nil {
			t.Fatalf("BuildFullTextSearchSQL error: %v", err)
		}

		sql, args := ctx.GetResult()
		if want := "to_tsvector('simple', coalesce(\"title\", '') || ' ' || coalesce(\"body\", '')) @@ plainto_tsquery($1)"; sql != want {
			t.Errorf("全文检索 SQL = %q, want %q", sql, want)
		}

		if len(args) != 1 || args[0] != "wxorm orm" {
			t.Errorf("全文检索参数不符合预期: %#v", args)
		}
	})

	t.Run("boolean mode", func(t *testing.T) {
		ctx := NewContext()
		if err := pg.BuildFullTextSearchSQL(ctx, []string{"title"}, "wxorm & orm", "IN BOOLEAN MODE"); err != nil {
			t.Fatalf("BuildFullTextSearchSQL error: %v", err)
		}

		if sql, _ := ctx.GetResult(); !strings.Contains(sql, "websearch_to_tsquery($1)") {
			t.Errorf("布尔模式应映射为 websearch_to_tsquery，实际:\n%s", sql)
		}
	})

	t.Run("unsupported mode", func(t *testing.T) {
		ctx := NewContext()
		err := pg.BuildFullTextSearchSQL(ctx, []string{"title"}, "wxorm", "WITH QUERY EXPANSION")
		if !errors.Is(err, ErrUnsupportedFeature) {
			t.Fatalf("期望 ErrUnsupportedFeature，实际: %v", err)
		}
	})
}

func TestPostgresSelectPagination(t *testing.T) {
	pg := newPostgresForTest(t)
	ctx := NewContext()

	if err := pg.BuildSelectSQL(ctx, "users", "id", "name"); err != nil {
		t.Fatalf("BuildSelectSQL error: %v", err)
	}
	if err := pg.BuildFromSQL(ctx, "users"); err != nil {
		t.Fatalf("BuildFromSQL error: %v", err)
	}
	if err := pg.BuildLimitSQL(ctx, 10); err != nil {
		t.Fatalf("BuildLimitSQL error: %v", err)
	}
	if err := pg.BuildOffsetSQL(ctx, 20); err != nil {
		t.Fatalf("BuildOffsetSQL error: %v", err)
	}

	sql, _ := ctx.GetResult()
	want := "SELECT\n    \"id\", \"name\"\nFROM\n    \"users\"\nLIMIT\n    10\nOFFSET\n    20"
	if sql != want {
		t.Errorf("SELECT SQL = %q, want %q", sql, want)
	}

	if !ctx.HasLimit() {
		t.Error("LIMIT 之后应标记 hasLimit")
	}
}
