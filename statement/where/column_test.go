package where

import (
	"testing"

	"gitee.com/leminewx/wxorm/dialect"
)

// TestEQCOLUMN 列对列比较应写标识符而不是占位符
func TestEQCOLUMN(t *testing.T) {
	tests := []struct {
		name string
		cond Condition
		cla  dialect.Dialect
		want string
	}{
		{
			name: "mysql",
			cond: EQ_COLUMN("articles.author_id", "author.id"),
			cla:  dialect.NewMySQLDialect(),
			want: "`articles`.`author_id` = `author`.`id`",
		},
		{
			name: "postgres",
			cond: EQ_COLUMN("articles.author_id", "author.id"),
			cla:  dialect.NewPostgresDialect(),
			want: "\"articles\".\"author_id\" = \"author\".\"id\"",
		},
		{
			name: "sqlite",
			cond: EQ_COLUMN("articles.author_id", "author.id"),
			cla:  dialect.NewSQLiteDialect(),
			want: "\"articles\".\"author_id\" = \"author\".\"id\"",
		},
		{
			name: "与普通条件组合",
			cond: EQ_COLUMN("a.x", "b.x").AND(EQ("b.status", 1)),
			cla:  dialect.NewPostgresDialect(),
			want: "\"a\".\"x\" = \"b\".\"x\" AND \"b\".\"status\" = $1",
		},
		{
			name: "取反",
			cond: EQ_COLUMN("a.x", "b.x").NOT(),
			cla:  dialect.NewMySQLDialect(),
			want: "NOT (`a`.`x` = `b`.`x`)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args := buildCondition(t, tt.cond, tt.cla)
			if sql != tt.want {
				t.Errorf("SQL = %q, want %q", sql, tt.want)
			}

			// 列对列比较不产生参数，最后一个用例会带 AND 的值参数
			if tt.name == "与普通条件组合" {
				if len(args) != 1 || args[0] != 1 {
					t.Errorf("args = %v, want [1]", args)
				}
				return
			}

			if len(args) != 0 {
				t.Errorf("args = %v, want empty", args)
			}
		})
	}
}

// TestEQCOLUMNClone 克隆后仍保持列引用语义
func TestEQCOLUMNClone(t *testing.T) {
	cond := EQ_COLUMN("a.x", "b.x")
	clone := cond.Clone()

	sql, args := buildCondition(t, clone, dialect.NewMySQLDialect())
	if sql != "`a`.`x` = `b`.`x`" {
		t.Errorf("clone SQL = %q", sql)
	}
	if len(args) != 0 {
		t.Errorf("clone args = %v, want empty", args)
	}
}
