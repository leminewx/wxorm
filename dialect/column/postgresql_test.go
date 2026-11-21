package column

import (
	"strings"
	"testing"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/dialect/datatype"
)

func buildPostgresColumnSQL(t *testing.T, tag string) string {
	t.Helper()

	col, err := NewPostgresColumn(tag)
	if err != nil {
		t.Fatalf("NewPostgresColumn(%q) error: %v", tag, err)
	}

	ctx := clause.NewContext()
	if err := col.BuildCreateSQL(ctx); err != nil {
		t.Fatalf("BuildCreateSQL error: %v", err)
	}

	sql, _ := ctx.GetResult()
	return sql
}

func TestPostgresColumnAutoIncrement(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		expected string
	}{
		{"bigint to bigserial", "column(id) type(bigint) pk auto", `"id" BIGSERIAL`},
		{"int to serial", "column(id) type(int) pk auto", `"id" SERIAL`},
		{"integer to serial", "column(id) type(integer) pk auto", `"id" SERIAL`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if sql := buildPostgresColumnSQL(t, tt.tag); sql != tt.expected {
				t.Errorf("BuildCreateSQL = %q, want %q", sql, tt.expected)
			}
		})
	}
}

func TestPostgresColumnCreatedTimestamp(t *testing.T) {
	sql := buildPostgresColumnSQL(t, "column(created_at) type(timestamptz) created")
	if want := `"created_at" TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP`; sql != want {
		t.Errorf("BuildCreateSQL = %q, want %q", sql, want)
	}
}

func TestPostgresColumnGeneratedColumn(t *testing.T) {
	sql := buildPostgresColumnSQL(t, "column(total) type(numeric) generated(price * qty)")
	if want := `"total" NUMERIC GENERATED ALWAYS AS (price * qty) STORED`; sql != want {
		t.Errorf("BuildCreateSQL = %q, want %q", sql, want)
	}
}

func TestPostgresColumnConstraints(t *testing.T) {
	sql := buildPostgresColumnSQL(t, "column(email) type(varchar(255)) notnull unique check(length(email) > 3)")

	for _, want := range []string{`"email" VARCHAR(255)`, "NOT NULL", "UNIQUE", "CHECK (length(email) > 3)"} {
		if !strings.Contains(sql, want) {
			t.Errorf("BuildCreateSQL 缺失 %q，实际: %s", want, sql)
		}
	}
}

// PostgreSQL 不支持内联注释、AUTO_INCREMENT 与 ON UPDATE，字段 DDL 不应生成这些片段
func TestPostgresColumnIgnoresMySQLOnlyFeatures(t *testing.T) {
	sql := buildPostgresColumnSQL(t, "column(login_at) type(datetime) created updated comment('登录时间') collate(utf8_unicode_ci) index")

	for _, unwanted := range []string{"AUTO_INCREMENT", "ON UPDATE", "COMMENT", "CHARACTER SET", "COLLATE", "INDEX"} {
		if strings.Contains(sql, unwanted) {
			t.Errorf("BuildCreateSQL 不应包含 %q，实际: %s", unwanted, sql)
		}
	}
}

func TestPostgresColumnAutoFlags(t *testing.T) {
	col, err := NewPostgresColumn("column(created_at) type(timestamptz) created")
	if err != nil {
		t.Fatalf("NewPostgresColumn error: %v", err)
	}

	if !col.IsAutoInsert() {
		t.Error("created 时间戳字段应参与自动插入")
	}

	// PostgreSQL 没有 ON UPDATE CURRENT_TIMESTAMP
	if col.IsAutoUpdate() {
		t.Error("PostgreSQL 不应声明自动更新")
	}

	if col.GetDatatype() != datatype.TIMESTAMPTZ {
		t.Errorf("数据类型 = %q, want TIMESTAMPTZ", col.GetDatatype())
	}
}

func TestPostgresColumnQuotedName(t *testing.T) {
	// 标签里的引号会被剔除，转义只发生在 DDL 生成阶段
	sql := buildPostgresColumnSQL(t, "column('user_name') type(varchar(64))")
	if want := `"user_name" VARCHAR(64)`; sql != want {
		t.Errorf("BuildCreateSQL = %q, want %q", sql, want)
	}
}
