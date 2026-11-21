package where

import "strings"

// AND 与条件组合
func AND(conditions ...Condition) Condition {
	switch len(conditions) {
	case 0:
		return nil
	case 1:
		return conditions[0]
	default:
		return &groupCondition{
			operator:   "AND",
			conditions: conditions,
		}
	}
}

// OR 或条件组合
func OR(conditions ...Condition) Condition {
	switch len(conditions) {
	case 0:
		return nil
	case 1:
		return conditions[0]
	default:
		return &groupCondition{
			operator:   "OR",
			conditions: conditions,
		}
	}
}

// NOT 非条件
func NOT(condition Condition) Condition {
	return condition.NOT()
}

/*
EQ 等于
  - `column` = value
*/
func EQ(column string, value any) Condition {
	return &expression{
		column:   column,
		operator: _EQ,
		value:    value,
	}
}

// Column 表示条件表达式右侧的列引用
//
// 与普通值不同，它会被写成标识符（加引号）而不是占位符，
// 用于联表 ON 条件里的列对列比较。
type Column string

/*
EQ_COLUMN 列与列相等（联表 ON 条件）
  - `left` = `right`
*/
func EQ_COLUMN(left, right string) Condition {
	return &expression{
		column:   left,
		operator: _EQ,
		value:    Column(right),
	}
}

/*
NE 不等于
  - `column` <> value
*/
func NE(column string, value any) Condition {
	return &expression{
		column:   column,
		operator: _NE,
		value:    value,
	}
}

/*
GT 大于
  - `column` > value
*/
func GT(column string, value any) Condition {
	return &expression{
		column:   column,
		operator: _GT,
		value:    value,
	}
}

/*
GTE 大于等于
  - `column` >= value
*/
func GTE(column string, value any) Condition {
	return &expression{
		column:   column,
		operator: _GTE,
		value:    value,
	}
}

/*
LT 小于
  - `column` < value
*/
func LT(column string, value any) Condition {
	return &expression{
		column:   column,
		operator: _LT,
		value:    value,
	}
}

/*
LTE 小于等于
  - `column` <= value
*/
func LTE(column string, value any) Condition {
	return &expression{
		column:   column,
		operator: _LTE,
		value:    value,
	}
}

/*
IN 枚举匹配
  - `column` IN (value1, value2, ...)
*/
func IN[T any](column string, values ...T) Condition {
	return &expression{
		column:   column,
		operator: _IN,
		value:    convertToAnySlice(values),
	}
}

/*
NOTIN 不在枚举范围中
  - `column` NOT IN (value1, value2, ...)
*/
func NOTIN[T any](column string, values ...T) Condition {
	return &expression{
		column:   column,
		operator: _NOTIN,
		value:    convertToAnySlice(values),
	}
}

/*
LIKE 模糊匹配
  - `column` LIKE "%"
  - `column` LIKE "value"
  - `column` LIKE "%value"
  - `column` LIKE "value%"
  - `column` LIKE "%value%"
*/
func LIKE(column string, pattern string) Condition {
	return &expression{
		column:   column,
		operator: _LIKE,
		value:    pattern,
	}
}

/*
NOTLIKE 模糊匹配
  - `column` NOT LIKE "%"
  - `column` NOT LIKE "value"
  - `column` NOT LIKE "%value"
  - `column` NOT LIKE "value%"
  - `column` NOT LIKE "%value%"
*/
func NOTLIKE(column string, pattern string) Condition {
	return &expression{
		column:   column,
		operator: _NOTLIKE,
		value:    pattern,
	}
}

/*
ISNULL 是否为 NULL
  - `column` IS NULL
*/
func ISNULL(column string) Condition {
	return &expression{
		column:   column,
		operator: _ISNULL,
	}
}

/*
ISNOTNULL 是否非 NULL
  - `column` IS NOT NULL
*/
func ISNOTNULL(column string) Condition {
	return &expression{
		column:   column,
		operator: _NOTNULL,
	}
}

/*
BETWEEN 范围匹配
  - `column` BETWEEN start AND end
*/
func BETWEEN(column string, start, end any) Condition {
	return &expression{
		column:   column,
		operator: _BETWEEN,
		value:    [2]any{start, end},
	}
}

/*
NOTBETWEEN 不在匹配范围中
  - `column` NOT BETWEEN start AND end
*/
func NOTBETWEEN(column string, start, end any) Condition {
	return &expression{
		column:   column,
		operator: _NOTBETWEEN,
		value:    [2]any{start, end},
	}
}

// RAW 直接插入 SQL
func RAW(sql string, args ...any) Condition {
	return &rawCondition{
		sql:  strings.TrimSpace(sql),
		args: args,
	}
}

// WHEN 用于动态生成条件
func WHEN(condition bool, build func() Condition) Condition {
	if !condition {
		return nil
	}

	return build()
}

func convertToAnySlice[T any](values []T) []any {
	vals := make([]any, len(values))
	for i, v := range values {
		vals[i] = v
	}

	return vals
}
