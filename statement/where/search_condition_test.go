package where

import (
	"errors"
	"strings"
	"testing"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/dialect/clause"
)

// baseClause 只暴露通用子句，用于验证方言缺少可选能力时的行为
type baseClause struct {
	clause.Clause
}

func buildCondition(t *testing.T, cond Condition, cla clause.Clause) (string, []any) {
	t.Helper()

	ctx := clause.NewContext()
	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("Build error: %v", err)
	}

	return ctx.GetResult()
}

func TestJSONContainsPerDialect(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		sql, args := buildCondition(t, JSON_CONTAINS("payload", "$.user.name", "wxorm"), dialect.NewPostgresDialect())
		if want := `("payload" #> '{user,name}') @> $1::jsonb`; sql != want {
			t.Errorf("SQL = %q, want %q", sql, want)
		}

		if len(args) != 1 || args[0] != "wxorm" {
			t.Errorf("参数不符合预期: %#v", args)
		}
	})

	t.Run("mysql", func(t *testing.T) {
		sql, args := buildCondition(t, JSON_CONTAINS("payload", "$.user.name", "wxorm"), dialect.NewMySQLDialect())
		if want := "JSON_CONTAINS(`payload`, ?, '$.user.name')"; sql != want {
			t.Errorf("SQL = %q, want %q", sql, want)
		}

		if len(args) != 1 || args[0] != "wxorm" {
			t.Errorf("参数不符合预期: %#v", args)
		}
	})
}

func TestJSONContainsUnsupportedDialect(t *testing.T) {
	cond := JSON_CONTAINS("payload", "$.name", "wxorm")

	ctx := clause.NewContext()
	err := cond.Build(ctx, baseClause{clause.NewMySQLClause()})
	if !errors.Is(err, clause.ErrUnsupportedFeature) {
		t.Fatalf("期望 ErrUnsupportedFeature，实际: %v", err)
	}
}

func TestFullTextPerDialect(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		sql, args := buildCondition(t, FULLTEXT([]string{"title", "body"}, "wxorm orm"), dialect.NewPostgresDialect())
		if !strings.Contains(sql, "to_tsvector('simple'") || !strings.Contains(sql, "@@ plainto_tsquery($1)") {
			t.Errorf("PostgreSQL 全文检索 SQL 不符合预期: %s", sql)
		}

		if len(args) != 1 || args[0] != "wxorm orm" {
			t.Errorf("参数不符合预期: %#v", args)
		}
	})

	t.Run("mysql", func(t *testing.T) {
		sql, _ := buildCondition(t, FULLTEXT([]string{"title", "body"}, "wxorm orm"), dialect.NewMySQLDialect())
		if !strings.Contains(sql, "MATCH") || !strings.Contains(sql, "AGAINST") {
			t.Errorf("MySQL 全文检索 SQL 不符合预期: %s", sql)
		}
	})

	t.Run("unsupported mode", func(t *testing.T) {
		cond := FULLTEXT([]string{"title"}, "wxorm", "WITH QUERY EXPANSION")

		ctx := clause.NewContext()
		err := cond.Build(ctx, dialect.NewPostgresDialect())
		if !errors.Is(err, clause.ErrUnsupportedFeature) {
			t.Fatalf("期望 ErrUnsupportedFeature，实际: %v", err)
		}
	})
}

// RAW 统一用 ? 书写，PostgreSQL 下必须改写为 $n
func TestRawPlaceholderRewrite(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		sql, args := buildCondition(t, RAW("age > ? AND name = ?", 18, "wxorm"), dialect.NewPostgresDialect())
		if want := "age > $1 AND name = $2"; sql != want {
			t.Errorf("SQL = %q, want %q", sql, want)
		}

		if len(args) != 2 || args[0] != 18 || args[1] != "wxorm" {
			t.Errorf("参数不符合预期: %#v", args)
		}
	})

	t.Run("mysql", func(t *testing.T) {
		sql, _ := buildCondition(t, RAW("age > ? AND name = ?", 18, "wxorm"), dialect.NewMySQLDialect())
		if want := "age > ? AND name = ?"; sql != want {
			t.Errorf("SQL = %q, want %q", sql, want)
		}
	})

	t.Run("placeholder mismatch keeps raw sql", func(t *testing.T) {
		// 占位符与参数个数不一致时按原样输出，交给数据库报错
		sql, _ := buildCondition(t, RAW("age > ?", 18, "wxorm"), dialect.NewPostgresDialect())
		if want := "age > ?"; sql != want {
			t.Errorf("SQL = %q, want %q", sql, want)
		}
	})
}

func TestLIKEUsesDialectOperator(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		sql, _ := buildCondition(t, LIKE("name", "%wxorm%"), dialect.NewPostgresDialect())
		if want := `"name" ILIKE $1`; sql != want {
			t.Errorf("SQL = %q, want %q", sql, want)
		}

		sql, _ = buildCondition(t, NOTLIKE("name", "%wxorm%"), dialect.NewPostgresDialect())
		if want := `"name" NOT ILIKE $1`; sql != want {
			t.Errorf("SQL = %q, want %q", sql, want)
		}
	})

	t.Run("mysql", func(t *testing.T) {
		sql, _ := buildCondition(t, LIKE("name", "%wxorm%"), dialect.NewMySQLDialect())
		if want := "`name` LIKE ?"; sql != want {
			t.Errorf("SQL = %q, want %q", sql, want)
		}
	})
}

func TestSearchConditionsCombine(t *testing.T) {
	cond := JSON_CONTAINS("payload", "$.role", "admin").AND(EQ("enabled", true))
	if cond.IsEmpty() {
		t.Fatal("组合后的条件不应为空")
	}

	sql, args := buildCondition(t, cond, dialect.NewPostgresDialect())
	if !strings.Contains(sql, "AND") || !strings.Contains(sql, `"enabled" = $2`) {
		t.Errorf("组合条件 SQL 不符合预期: %s", sql)
	}

	if len(args) != 2 {
		t.Errorf("参数数量 = %d, want 2", len(args))
	}
}
