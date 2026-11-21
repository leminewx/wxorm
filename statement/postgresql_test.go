package statement

import (
	"errors"
	"strings"
	"testing"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/dialect/clause"
)

func buildWith(t *testing.T, dial clause.Clause, st *Statement, names ...Name) (string, []any, error) {
	t.Helper()

	return st.BuildSQL(dial, names...)
}

// PostgreSQL 不接受 MySQL 语义的 DUPLICATE：冲突目标与更新列无法用同一组参数表达
func TestBuildSQL_PostgresRejectsDuplicate(t *testing.T) {
	st := NewStatement("user")
	st.AddClause(INSERT("user", false, "name", "age"))
	st.AddClause(VALUES([]any{"wxorm", 12}))
	st.AddClause(DUPLICATE("id"))

	_, _, err := buildWith(t, dialect.NewPostgresDialect(), st, NAME_INSERT, NAME_VALUES, NAME_DUPLICATE)
	if !errors.Is(err, clause.ErrUnsupportedFeature) {
		t.Fatalf("期望 ErrUnsupportedFeature，实际: %v", err)
	}

	if !strings.Contains(err.Error(), "UPSERT") {
		t.Errorf("错误信息应指引改用 UPSERT，实际: %v", err)
	}
}

// PostgreSQL 的 IGNORE 通过独立子句追加，MySQL 则内联在 INSERT 中
func TestBuildSQL_InsertIgnorePerDialect(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		st := NewStatement("user")
		st.AddClause(INSERT("user", true, "name"))
		st.AddClause(VALUES([]any{"wxorm"}))
		st.AddClause(INSERT_IGNORE())

		sql, _, err := buildWith(t, dialect.NewPostgresDialect(), st, NAME_INSERT, NAME_VALUES, NAME_IGNORE)
		if err != nil {
			t.Fatalf("BuildSQL error: %v", err)
		}

		if strings.Contains(sql, "INSERT IGNORE") {
			t.Errorf("PostgreSQL 不应生成 INSERT IGNORE，实际: %s", sql)
		}
		if !strings.Contains(sql, "ON CONFLICT DO NOTHING") {
			t.Errorf("PostgreSQL 应生成 ON CONFLICT DO NOTHING，实际: %s", sql)
		}
	})

	t.Run("mysql", func(t *testing.T) {
		st := NewStatement("user")
		st.AddClause(INSERT("user", true, "name"))
		st.AddClause(VALUES([]any{"wxorm"}))
		st.AddClause(INSERT_IGNORE())

		sql, _, err := buildWith(t, dialect.NewMySQLDialect(), st, NAME_INSERT, NAME_VALUES, NAME_IGNORE)
		if err != nil {
			t.Fatalf("BuildSQL error: %v", err)
		}

		if !strings.Contains(sql, "INSERT IGNORE INTO") {
			t.Errorf("MySQL 应内联 INSERT IGNORE，实际: %s", sql)
		}
		if strings.Contains(sql, "ON CONFLICT") {
			t.Errorf("MySQL 不应生成 ON CONFLICT，实际: %s", sql)
		}
	})
}

// 方言缺少能力时，子句必须返回可被 errors.Is 识别的错误，而不是静默降级
func TestBuildSQL_UnsupportedCapabilities(t *testing.T) {
	postgres := dialect.NewPostgresDialect()
	mysql := dialect.NewMySQLDialect()

	t.Run("REPLACE on postgres", func(t *testing.T) {
		st := NewStatement("user")
		st.AddClause(REPLACE("user", "name"))

		if _, _, err := buildWith(t, postgres, st, NAME_REPLACE); !errors.Is(err, clause.ErrUnsupportedFeature) {
			t.Errorf("期望 ErrUnsupportedFeature，实际: %v", err)
		}
	})

	t.Run("RETURNING on mysql", func(t *testing.T) {
		st := NewStatement("user")
		st.AddClause(RETURNING("id"))

		if _, _, err := buildWith(t, mysql, st, NAME_RETURNING); !errors.Is(err, clause.ErrUnsupportedFeature) {
			t.Errorf("期望 ErrUnsupportedFeature，实际: %v", err)
		}
	})
}

func TestBuildSQL_PostgresReturning(t *testing.T) {
	st := NewStatement("user")
	st.AddClause(INSERT("user", false, "name"))
	st.AddClause(VALUES([]any{"wxorm"}))
	st.AddClause(RETURNING("id"))

	sql, _, err := buildWith(t, dialect.NewPostgresDialect(), st, NAME_INSERT, NAME_VALUES, NAME_RETURNING)
	if err != nil {
		t.Fatalf("BuildSQL error: %v", err)
	}

	if !strings.HasSuffix(sql, "\nRETURNING \"id\";") {
		t.Errorf("SQL 应以 RETURNING 结尾，实际: %s", sql)
	}
}

// PostgreSQL 的批量插入同样用 RETURNING 逐行回填主键
func TestBuildSQL_PostgresBatchReturning(t *testing.T) {
	st := NewStatement("user")
	st.AddClause(INSERT("user", false, "name", "age"))
	st.AddClause(VALUES([]any{"wxorm", 12}, []any{"leminewx", 13}))
	st.AddClause(RETURNING("id"))

	sql, args, err := buildWith(t, dialect.NewPostgresDialect(), st, NAME_INSERT, NAME_VALUES, NAME_RETURNING)
	if err != nil {
		t.Fatalf("BuildSQL error: %v", err)
	}

	if !strings.HasSuffix(sql, "\nRETURNING \"id\";") {
		t.Errorf("SQL 应以 RETURNING 结尾，实际: %s", sql)
	}

	if !strings.Contains(sql, "$4") {
		t.Errorf("多行 VALUES 的占位符应递增到 $4，实际: %s", sql)
	}

	if len(args) != 4 {
		t.Fatalf("参数个数 = %d, want 4", len(args))
	}
}

// UPSERT 把冲突目标与更新列分开：PostgreSQL 按主键冲突，只覆盖指定列
func TestBuildSQL_UpsertSeparatesConflictAndUpdate(t *testing.T) {
	t.Run("postgres", func(t *testing.T) {
		st := NewStatement("user")
		st.AddClause(INSERT("user", false, "name", "age"))
		st.AddClause(VALUES([]any{"wxorm", 12}))
		st.AddClause(UPSERT([]string{"id"}, "name"))

		sql, _, err := buildWith(t, dialect.NewPostgresDialect(), st, NAME_INSERT, NAME_VALUES, NAME_DUPLICATE)
		if err != nil {
			t.Fatalf("BuildSQL error: %v", err)
		}

		want := "\nON CONFLICT (\"id\") DO UPDATE SET\n    \"name\" = EXCLUDED.\"name\";"
		if !strings.HasSuffix(sql, want) {
			t.Errorf("SQL 应以 %q 结尾，实际: %s", want, sql)
		}
	})

	t.Run("mysql falls back to duplicate", func(t *testing.T) {
		st := NewStatement("user")
		st.AddClause(INSERT("user", false, "name", "age"))
		st.AddClause(VALUES([]any{"wxorm", 12}))
		st.AddClause(UPSERT([]string{"id"}, "name"))

		sql, _, err := buildWith(t, dialect.NewMySQLDialect(), st, NAME_INSERT, NAME_VALUES, NAME_DUPLICATE)
		if err != nil {
			t.Fatalf("BuildSQL error: %v", err)
		}

		if !strings.Contains(sql, "ON DUPLICATE KEY UPDATE") {
			t.Errorf("MySQL 应生成 ON DUPLICATE KEY UPDATE，实际: %s", sql)
		}
		if strings.Contains(sql, "ON CONFLICT") {
			t.Errorf("MySQL 不应生成 ON CONFLICT，实际: %s", sql)
		}
	})
}

// 需要显式冲突目标的方言在没有主键时必须报错，而不是退化为 DO NOTHING
func TestBuildSQL_UpsertRequiresConflictTarget(t *testing.T) {
	st := NewStatement("user")
	st.AddClause(INSERT("user", false, "name"))
	st.AddClause(VALUES([]any{"wxorm"}))
	st.AddClause(UPSERT(nil, "name"))

	_, _, err := buildWith(t, dialect.NewPostgresDialect(), st, NAME_INSERT, NAME_VALUES, NAME_DUPLICATE)
	if !errors.Is(err, ErrConflictTargetRequired) {
		t.Fatalf("期望 ErrConflictTargetRequired，实际: %v", err)
	}
}
