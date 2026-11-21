package column

import (
	"errors"
	"strings"
	"testing"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/dialect/datatype"
)

func buildSQLiteColumnSQL(t *testing.T, tag string) string {
	t.Helper()

	col, err := NewSQLiteColumn(tag)
	if err != nil {
		t.Fatalf("NewSQLiteColumn(%q) error: %v", tag, err)
	}

	ctx := clause.NewContext()
	if err := col.BuildCreateSQL(ctx); err != nil {
		t.Fatalf("BuildCreateSQL error: %v", err)
	}

	sql, _ := ctx.GetResult()
	return sql
}

func TestSQLiteColumnAutoIncrement(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		expected string
	}{
		// 自增主键必须内联为 INTEGER PRIMARY KEY AUTOINCREMENT，否则不是 rowid 别名
		{"integer pk auto", "column(id) type(integer) pk auto", `"id" INTEGER PRIMARY KEY AUTOINCREMENT`},
		{"bigint pk auto", "column(id) type(bigint) pk auto", `"id" INTEGER PRIMARY KEY AUTOINCREMENT`},
		{"int pk auto notnull", "column(id) type(int) pk auto notnull", `"id" INTEGER PRIMARY KEY AUTOINCREMENT`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if sql := buildSQLiteColumnSQL(t, tt.tag); sql != tt.expected {
				t.Errorf("BuildCreateSQL = %q, want %q", sql, tt.expected)
			}
		})
	}
}

// auto 标记落在非主键列上时，SQLite 无法生成合法 DDL，必须显式报错
func TestSQLiteColumnAutoIncrementWithoutPrimaryKey(t *testing.T) {
	col, err := NewSQLiteColumn("column(id) type(integer) auto")
	if err != nil {
		t.Fatalf("NewSQLiteColumn error: %v", err)
	}

	err = col.BuildCreateSQL(clause.NewContext())
	if !errors.Is(err, ErrAutoIncrementRequiresPrimaryKey) {
		t.Fatalf("期望 ErrAutoIncrementRequiresPrimaryKey，实际: %v", err)
	}
}

func TestSQLiteColumnCreatedTimestamp(t *testing.T) {
	sql := buildSQLiteColumnSQL(t, "column(created_at) type(datetime) created")
	if want := `"created_at" DATETIME DEFAULT CURRENT_TIMESTAMP`; sql != want {
		t.Errorf("BuildCreateSQL = %q, want %q", sql, want)
	}
}

func TestSQLiteColumnGeneratedColumn(t *testing.T) {
	sql := buildSQLiteColumnSQL(t, "column(total) type(integer) generated(price * qty)")
	if want := `"total" INTEGER GENERATED ALWAYS AS (price * qty) VIRTUAL`; sql != want {
		t.Errorf("BuildCreateSQL = %q, want %q", sql, want)
	}
}

func TestSQLiteColumnConstraints(t *testing.T) {
	sql := buildSQLiteColumnSQL(t, "column(email) type(text) notnull unique default('') check(length(email) > 3)")

	for _, want := range []string{`"email" TEXT`, "NOT NULL", "DEFAULT ''", "UNIQUE", "CHECK (length(email) > 3)"} {
		if !strings.Contains(sql, want) {
			t.Errorf("BuildCreateSQL 缺失 %q，实际: %s", want, sql)
		}
	}
}

// SQLite 不支持列注释、字符集与内联索引，字段 DDL 不应生成这些片段
func TestSQLiteColumnIgnoresUnsupportedFeatures(t *testing.T) {
	sql := buildSQLiteColumnSQL(t, "column(login_at) type(datetime) created updated comment('登录时间') charset(utf8mb4) collate(binary) index")

	for _, unwanted := range []string{"COMMENT", "CHARACTER SET", "COLLATE", "INDEX", "ON UPDATE"} {
		if strings.Contains(sql, unwanted) {
			t.Errorf("BuildCreateSQL 不应包含 %q，实际: %s", unwanted, sql)
		}
	}
}

func TestSQLiteColumnAutoFlags(t *testing.T) {
	col, err := NewSQLiteColumn("column(created_at) type(datetime) created")
	if err != nil {
		t.Fatalf("NewSQLiteColumn error: %v", err)
	}

	if !col.IsAutoInsert() {
		t.Error("created 时间戳字段应参与自动插入")
	}

	// SQLite 没有 ON UPDATE CURRENT_TIMESTAMP
	if col.IsAutoUpdate() {
		t.Error("SQLite 不应声明自动更新")
	}

	if col.GetDatatype() != datatype.DATETIME {
		t.Errorf("数据类型 = %q, want DATETIME", col.GetDatatype())
	}
}

// 非自增主键不内联，交给 dialect 生成表级 PRIMARY KEY，便于支持复合主键
func TestSQLiteColumnPrimaryKeyWithoutAutoIsNotInline(t *testing.T) {
	col, err := NewSQLiteColumn("column(id) type(text) pk")
	if err != nil {
		t.Fatalf("NewSQLiteColumn error: %v", err)
	}

	if col.IsInlinePrimaryKey() {
		t.Error("非自增主键不应内联")
	}

	if want := `"id" TEXT`; buildSQLiteColumnSQL(t, "column(id) type(text) pk") != want {
		t.Errorf("BuildCreateSQL = %q, want %q", buildSQLiteColumnSQL(t, "column(id) type(text) pk"), want)
	}
}

func TestSQLiteColumnQuotedName(t *testing.T) {
	sql := buildSQLiteColumnSQL(t, "column('user_name') type(text)")
	if want := `"user_name" TEXT`; sql != want {
		t.Errorf("BuildCreateSQL = %q, want %q", sql, want)
	}
}
