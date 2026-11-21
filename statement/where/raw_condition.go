package where

import (
	"strings"

	"gitee.com/leminewx/wxorm/dialect/clause"
)

type rawCondition struct {
	sql     string
	args    []any
	negated bool
}

var _ Condition = (*rawCondition)(nil)

func (own *rawCondition) AND(conditions ...Condition) Condition {
	return own.buildAndOr(_AND, conditions...)
}

func (own *rawCondition) OR(conditions ...Condition) Condition {
	return own.buildAndOr(_OR, conditions...)
}

func (own *rawCondition) buildAndOr(operator Operator, conditions ...Condition) Condition {
	validConditions := GetValidConditions(conditions)
	if len(validConditions) == 0 {
		return own
	}

	return &groupCondition{
		operator:   operator,
		conditions: append([]Condition{own}, validConditions...),
	}
}

func (own *rawCondition) NOT() Condition {
	return &rawCondition{
		sql:     own.sql,
		args:    own.args,
		negated: !own.negated,
	}
}

// Build 使用上下文构建
func (own *rawCondition) Build(ctx *clause.Context, cla clause.Clause) error {
	if own.IsEmpty() {
		return nil
	}

	if own.negated {
		_ = ctx.WriteString("NOT (")
	}

	if err := own.buildSQL(ctx, cla); err != nil {
		ctx.Reset()
		return err
	}

	if own.negated {
		_ = ctx.WriteByte(')')
	}

	return nil
}

// buildSQL 写入原生 SQL
//
// 原生 SQL 统一用 ? 书写，方言不使用 ? 时（如 PostgreSQL 的 $n）在这里改写占位符，
// 这样同一段条件可以跨方言复用。
//
// 只把引号与注释之外的 ? 当作占位符：字符串字面量、引用标识符、行注释、块注释与
// PostgreSQL 的美元引用内的 ? 原样保留，避免把字面量里的问号误当占位符。
// 占位符个数与参数个数不一致时按原样输出，交给数据库报错，避免静默错位。
func (own *rawCondition) buildSQL(ctx *clause.Context, cla clause.Clause) error {
	positions := findPlaceholders(own.sql)
	if len(positions) != len(own.args) || cla.GetPlaceholder(1) == "?" {
		_ = ctx.WriteString(own.sql)
		ctx.AddArgs(own.args...)
		return nil
	}

	_, argIndex := ctx.GetSQLLengthAndArgNumber()
	last := 0
	for i, pos := range positions {
		_ = ctx.WriteString(own.sql[last:pos])
		_ = ctx.WriteString(cla.GetPlaceholder(argIndex + i + 1))
		ctx.AddArgs(own.args[i])
		last = pos + 1
	}

	_ = ctx.WriteString(own.sql[last:])
	return nil
}

// findPlaceholders 返回 SQL 中属于占位符的 ? 下标
func findPlaceholders(sql string) []int {
	var positions []int

	for i := 0; i < len(sql); i++ {
		switch sql[i] {
		case '\'', '"', '`':
			i = skipQuoted(sql, i, sql[i])
		case '-':
			if i+1 < len(sql) && sql[i+1] == '-' {
				i = skipLineComment(sql, i+2)
			}
		case '/':
			if i+1 < len(sql) && sql[i+1] == '*' {
				i = skipBlockComment(sql, i+2)
			}
		case '$':
			if end, ok := skipDollarQuoted(sql, i); ok {
				i = end
			}
		case '?':
			positions = append(positions, i)
		}
	}

	return positions
}

// skipQuoted 跳过一段引号包裹的内容，返回闭合引号的下标
func skipQuoted(sql string, start int, quote byte) int {
	for i := start + 1; i < len(sql); i++ {
		switch sql[i] {
		case '\\':
			// MySQL 默认把反斜杠当转义符，跳过被转义的字符
			i++
		case quote:
			// 连续两个引号是本字符的转义写法
			if i+1 < len(sql) && sql[i+1] == quote {
				i++
				continue
			}

			return i
		}
	}

	return len(sql) - 1
}

// skipLineComment 跳过 -- 行注释，返回换行符的下标
func skipLineComment(sql string, start int) int {
	for i := start; i < len(sql); i++ {
		if sql[i] == '\n' {
			return i
		}
	}

	return len(sql) - 1
}

// skipBlockComment 跳过 /* */ 块注释，返回结束标记的下标
func skipBlockComment(sql string, start int) int {
	for i := start; i+1 < len(sql); i++ {
		if sql[i] == '*' && sql[i+1] == '/' {
			return i + 1
		}
	}

	return len(sql) - 1
}

// skipDollarQuoted 跳过 PostgreSQL 的 $tag$...$tag$ 美元引用
func skipDollarQuoted(sql string, start int) (int, bool) {
	// $1 这类参数引用不是美元引用，tag 也不能以数字开头
	if start+1 >= len(sql) || (sql[start+1] >= '0' && sql[start+1] <= '9') {
		return 0, false
	}

	end := start + 1
	for end < len(sql) && isDollarTagChar(sql[end]) {
		end++
	}

	if end >= len(sql) || sql[end] != '$' {
		return 0, false
	}

	tag := sql[start : end+1]
	if idx := strings.Index(sql[end+1:], tag); idx >= 0 {
		return end + idx + len(tag), true
	}

	return len(sql) - 1, true
}

// isDollarTagChar 判断字符是否可用于美元引用的标签
func isDollarTagChar(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func (own *rawCondition) Clone() Condition {
	args := make([]any, len(own.args))
	copy(args, own.args)

	return &rawCondition{
		sql:     own.sql,
		args:    args,
		negated: own.negated,
	}
}

func (own *rawCondition) IsEmpty() bool {
	return own.sql == ""
}

func (own *rawCondition) GetOperator() Operator {
	return _NONE
}

func (own *rawCondition) GetChildren() []Condition {
	return []Condition{own}
}
