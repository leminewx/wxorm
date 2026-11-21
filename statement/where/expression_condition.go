package where

import (
	"fmt"
	"reflect"

	"gitee.com/leminewx/wxorm/dialect/clause"
)

// expression 定义表达式条件的结构类型
type expression struct {
	column   string   // 列名
	operator Operator // 操作符
	value    any      // 条件表达式的值
	negated  bool     // 当前条件是否为 NOT
}

var _ Condition = (*expression)(nil)

func (own *expression) AND(conditions ...Condition) Condition {
	return own.buildAndOr(_AND, conditions...)
}

func (own *expression) OR(conditions ...Condition) Condition {
	return own.buildAndOr(_OR, conditions...)
}

func (own *expression) buildAndOr(operator Operator, conditions ...Condition) Condition {
	validConditions := GetValidConditions(conditions)
	switch len(validConditions) {
	case 0:
		return own
	case 1:
		return &listCondition{
			operator:   operator,
			conditions: append([]Condition{own}, validConditions[0]),
		}
	default:
		return &listCondition{
			operator: operator,
			conditions: append([]Condition{own}, &groupCondition{
				operator:   operator,
				conditions: validConditions,
			}),
		}
	}
}

func (own *expression) NOT() Condition {
	return &expression{
		column:   own.column,
		operator: own.operator,
		value:    own.value,
		negated:  !own.negated,
	}
}

func (own *expression) Build(ctx *clause.Context, cla clause.Clause) error {
	if own.IsEmpty() {
		return nil
	}

	if own.negated {
		_ = ctx.WriteString("NOT (")
	}

	if err := own.buildExpression(ctx, cla); err != nil {
		ctx.Reset()
		return err
	}

	if own.negated {
		_ = ctx.WriteByte(')')
	}

	return nil
}

func (own *expression) buildExpression(ctx *clause.Context, cla clause.Clause) error {
	// 空集合的 IN / NOT IN：SQL 不允许 `IN ()`，按集合语义写成恒假 / 恒真，
	// 避免调用方以「切片为空」表达的过滤条件让整条语句报错
	if own.operator == _IN || own.operator == _NOTIN {
		if values, ok := own.value.([]any); ok && len(values) == 0 {
			if own.operator == _IN {
				_ = ctx.WriteString("1 = 0")
			} else {
				_ = ctx.WriteString("1 = 1")
			}

			return nil
		}
	}

	// 写入字段名、操作符
	_ = ctx.WriteString(cla.QuoteIdentifier(own.column))
	_ = ctx.WriteByte(' ')
	_ = ctx.WriteString(own.buildOperatorSQL(cla))

	// 根据不同的操作符处理参数
	switch own.operator {
	case _IN, _NOTIN:
		values, ok := own.value.([]any)
		if !ok {
			return fmt.Errorf("wxorm: invalid type of arguments for IN (comlum: %s, type: %v)", own.column, reflect.TypeOf(own.value))
		}

		_ = ctx.WriteString(" (")
		for i := range values {
			if i > 0 {
				_ = ctx.WriteString(", ")
			}

			_ = ctx.WritePlaceholderWithArg(cla, values[i])
		}
		_ = ctx.WriteByte(')')
		return nil

	case _BETWEEN, _NOTBETWEEN:
		values, ok := own.value.([2]any)
		if !ok {
			return fmt.Errorf("wxorm: invalid type of arguments for BETWEEN (comlum: %s, type: %v)", own.column, reflect.TypeOf(own.value))
		}

		_ = ctx.WriteByte(' ')
		_ = ctx.WritePlaceholderWithArg(cla, values[0])
		_ = ctx.WriteString(" AND ")
		_ = ctx.WritePlaceholderWithArg(cla, values[1])
		return nil

	case _ISNULL, _NOTNULL:
		// 没有参数
		return nil

	case _LIKE, _NOTLIKE:
		_ = ctx.WriteByte(' ')
		_ = ctx.WritePlaceholderWithArg(cla, own.value)
		return nil

	default:
		// 列对列比较（JOIN 的 ON 条件）：右侧写标识符而不是占位符
		if column, ok := own.value.(Column); ok {
			_ = ctx.WriteByte(' ')
			return ctx.WriteString(cla.QuoteIdentifier(string(column)))
		}

		// 普通操作符：=, >, <, >=, <=, !=, <>
		_ = ctx.WriteByte(' ')
		_ = ctx.WritePlaceholderWithArg(cla, own.value)
		return nil
	}
}

/*
buildOperatorSQL 返回当前方言下实际使用的操作符

MySQL 默认排序规则大小写不敏感，PostgreSQL 的 LIKE 则区分大小写，
因此 PostgreSQL 用 ILIKE 保持与 MySQL 一致的匹配语义。
*/
func (own *expression) buildOperatorSQL(cla clause.Clause) string {
	switch own.operator {
	case _LIKE:
		return cla.GetLikeOperator()
	case _NOTLIKE:
		if like := cla.GetLikeOperator(); like != string(_LIKE) {
			return "NOT " + like
		}

		return string(_NOTLIKE)
	default:
		return string(own.operator)
	}
}

func (own *expression) Clone() Condition {
	return &expression{
		column:   own.column,
		operator: own.operator,
		value:    own.value,
		negated:  own.negated,
	}
}

func (own *expression) IsEmpty() bool {
	return own.column == "" || own.operator == ""
}

func (own *expression) GetOperator() Operator {
	return own.operator
}

func (own *expression) GetChildren() []Condition {
	return []Condition{own}
}
