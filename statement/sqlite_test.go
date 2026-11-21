package statement

import (
	"errors"
	"strings"
	"testing"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/dialect/clause"
)

// SQLite 的 IGNORE 内联在 INSERT OR IGNORE 中，不需要 VALUES 之后的额外子句
func TestBuildSQL_SQLiteInsertIgnore(t *testing.T) {
	st := NewStatement("user")
	st.AddClause(INSERT("user", true, "name"))
	st.AddClause(VALUES([]any{"wxorm"}))
	st.AddClause(INSERT_IGNORE())

	sql, args, err := buildWith(t, dialect.NewSQLiteDialect(), st, NAME_INSERT, NAME_VALUES, NAME_IGNORE)
	if err != nil {
		t.Fatalf("BuildSQL error: %v", err)
	}

	want := "INSERT OR IGNORE INTO\n    \"user\" (\"name\")\nVALUES\n    (?);"
	if sql != want {
		t.Errorf("SQL = %q, want %q", sql, want)
	}

	if len(args) != 1 || args[0] != "wxorm" {
		t.Errorf("参数不符合预期: %#v", args)
	}
}

// SQLite 的 UPSERT 必须带冲突目标，且更新列与冲突目标可以不同
func TestBuildSQL_SQLiteUpsert(t *testing.T) {
	st := NewStatement("user")
	st.AddClause(INSERT("user", false, "name", "age"))
	st.AddClause(VALUES([]any{"wxorm", 12}))
	st.AddClause(UPSERT([]string{"id"}, "name"))

	sql, args, err := buildWith(t, dialect.NewSQLiteDialect(), st, NAME_INSERT, NAME_VALUES, NAME_DUPLICATE)
	if err != nil {
		t.Fatalf("BuildSQL error: %v", err)
	}

	want := "INSERT INTO\n    \"user\" (\"name\", \"age\")\nVALUES\n    (?, ?)\nON CONFLICT (\"id\") DO UPDATE SET\n    \"name\" = excluded.\"name\";"
	if sql != want {
		t.Errorf("SQL = %q, want %q", sql, want)
	}

	if len(args) != 2 {
		t.Errorf("参数数量 = %d, want 2", len(args))
	}
}

// DUPLICATE 是 MySQL 语义，SQLite 与 PostgreSQL 一样必须显式拒绝
func TestBuildSQL_SQLiteRejectsDuplicate(t *testing.T) {
	st := NewStatement("user")
	st.AddClause(INSERT("user", false, "name"))
	st.AddClause(VALUES([]any{"wxorm"}))
	st.AddClause(DUPLICATE("name"))

	_, _, err := buildWith(t, dialect.NewSQLiteDialect(), st, NAME_INSERT, NAME_VALUES, NAME_DUPLICATE)
	if !errors.Is(err, clause.ErrUnsupportedFeature) {
		t.Fatalf("期望 ErrUnsupportedFeature，实际: %v", err)
	}

	if !strings.Contains(err.Error(), "UPSERT") {
		t.Errorf("错误信息应指引改用 UPSERT，实际: %v", err)
	}
}

// SQLite 的 REPLACE 走 REPLACE INTO（INSERT OR REPLACE 的别名）
func TestBuildSQL_SQLiteReplace(t *testing.T) {
	st := NewStatement("user")
	st.AddClause(REPLACE("user", "name", "age"))
	st.AddClause(VALUES([]any{"wxorm", 12}))

	sql, _, err := buildWith(t, dialect.NewSQLiteDialect(), st, NAME_REPLACE, NAME_VALUES)
	if err != nil {
		t.Fatalf("BuildSQL error: %v", err)
	}

	want := "REPLACE INTO\n    \"user\" (\"name\", \"age\")\nVALUES\n    (?, ?);"
	if sql != want {
		t.Errorf("SQL = %q, want %q", sql, want)
	}
}

// 完整的查询链路：SELECT / FROM / WHERE / ORDER / LIMIT / OFFSET
func TestBuildSQL_SQLiteSelect(t *testing.T) {
	st := NewStatement("user")
	st.AddClause(SELECT("user", "id", "name"))
	st.AddClause(FROM("user"))
	st.AddClause(LIMIT(10))

	sql, _, err := buildWith(t, dialect.NewSQLiteDialect(), st, NAME_SELECT, NAME_FROM, NAME_LIMIT, NAME_OFFSET)
	if err != nil {
		t.Fatalf("BuildSQL error: %v", err)
	}

	want := "SELECT\n    \"id\", \"name\"\nFROM\n    \"user\"\nLIMIT\n    10;"
	if sql != want {
		t.Errorf("SQL = %q, want %q", sql, want)
	}
}

// SQLite 的批量插入用 RETURNING 逐行回填主键（3.35+）
func TestBuildSQL_SQLiteBatchReturning(t *testing.T) {
	st := NewStatement("user")
	st.AddClause(INSERT("user", false, "name", "age"))
	st.AddClause(VALUES([]any{"wxorm", 12}, []any{"leminewx", 13}))
	st.AddClause(RETURNING("id"))

	sql, args, err := buildWith(t, dialect.NewSQLiteDialect(), st, NAME_INSERT, NAME_VALUES, NAME_RETURNING)
	if err != nil {
		t.Fatalf("BuildSQL error: %v", err)
	}

	if !strings.HasSuffix(sql, "\nRETURNING \"id\";") {
		t.Errorf("SQL 应以 RETURNING 结尾，实际: %s", sql)
	}

	if len(args) != 4 {
		t.Errorf("参数数量 = %d, want 4", len(args))
	}
}
