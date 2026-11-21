package where

import (
	"testing"

	"gitee.com/leminewx/wxorm/dialect/clause"
)

func TestRaw(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond1 := &rawCondition{sql: "`name`=? AND `age`>=?", args: []any{"John", 20}}
	if err := cond1.Build(ctx, cla); err != nil {
		t.Errorf("Error building raw condition: %v", err)
	}
	t.Log(ctx.GetResult())
	ctx.Reset()

	cond2 := cond1.NOT()
	if err := cond2.Build(ctx, cla); err != nil {
		t.Errorf("Error building raw condition: %v", err)
	}
	t.Log(ctx.GetResult())
	ctx.Reset()
}

// 测试 PostgreSQL 方言下的占位符改写
//
// 只有真正的占位符才改写为 $n：字符串字面量、引用标识符、注释与美元引用内的 ?
// 都必须原样保留。jsonb 的 ? 运算符与占位符形态完全相同，词法无法区分，
// 因此要求占位符个数与参数个数严格一致，不一致时按原样输出。
func TestRaw_PostgresPlaceholders(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		args     []any
		expected string
	}{
		{
			name:     "字面量中的问号不改写",
			sql:      "name = '?' AND age = ?",
			args:     []any{18},
			expected: "name = '?' AND age = $1",
		},
		{
			name:     "jsonb 运算符无法与占位符区分时按原样输出",
			sql:      "data ? 'nickname' AND age = ?",
			args:     []any{18},
			expected: "data ? 'nickname' AND age = ?",
		},
		{
			name:     "引用标识符中的问号不改写",
			sql:      `"odd?" = ? AND age = ?`,
			args:     []any{"x", 18},
			expected: `"odd?" = $1 AND age = $2`,
		},
		{
			name:     "行注释中的问号不改写",
			sql:      "age = ? -- score? yes\nAND name = ?",
			args:     []any{18, "a"},
			expected: "age = $1 -- score? yes\nAND name = $2",
		},
		{
			name:     "块注释中的问号不改写",
			sql:      "age = ? /* what? */ AND name = ?",
			args:     []any{18, "a"},
			expected: "age = $1 /* what? */ AND name = $2",
		},
		{
			name:     "美元引用中的问号不改写",
			sql:      "body = $tag$a?b$tag$ AND age = ?",
			args:     []any{18},
			expected: "body = $tag$a?b$tag$ AND age = $1",
		},
		{
			name:     "转义引号不会提前结束字面量",
			sql:      "name = 'it\\'s?' AND age = ?",
			args:     []any{18},
			expected: "name = 'it\\'s?' AND age = $1",
		},
		{
			name:     "参数个数不一致时按原样输出",
			sql:      "age = ? AND name = ?",
			args:     []any{18},
			expected: "age = ? AND name = ?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := clause.NewContext()
			cond := &rawCondition{sql: tt.sql, args: tt.args}
			if err := cond.Build(ctx, clause.NewPostgresClause()); err != nil {
				t.Fatalf("build raw condition: %v", err)
			}

			sql, args := ctx.GetResult()
			if sql != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, sql)
			}

			if len(args) != len(tt.args) {
				t.Errorf("expected %d args, got %d", len(tt.args), len(args))
			}
		})
	}
}

// 测试 MySQL 方言下保持 ? 占位符
func TestRaw_MySQLKeepsQuestionMark(t *testing.T) {
	ctx := clause.NewContext()
	cond := &rawCondition{sql: "name = '?' AND age = ?", args: []any{18}}
	if err := cond.Build(ctx, clause.NewMySQLClause()); err != nil {
		t.Fatalf("build raw condition: %v", err)
	}

	sql, args := ctx.GetResult()
	if sql != "name = '?' AND age = ?" {
		t.Errorf("mysql should keep question marks, got %q", sql)
	}

	if len(args) != 1 {
		t.Errorf("expected 1 arg, got %d", len(args))
	}
}
