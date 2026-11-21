package where

// Operator 定义操作符的数据类型
type Operator string

const (
	_NONE Operator = ""

	_AND Operator = "AND"
	_OR  Operator = "OR"

	_EQ Operator = "="
	_NE Operator = "<>"

	_GTE Operator = ">="
	_GT  Operator = ">"

	_LT  Operator = "<"
	_LTE Operator = "<="

	_IN    Operator = "IN"
	_NOTIN Operator = "NOT IN"

	_ISNULL  Operator = "IS NULL"
	_NOTNULL Operator = "IS NOT NULL"

	_LIKE    Operator = "LIKE"
	_NOTLIKE Operator = "NOT LIKE"

	_BETWEEN    Operator = "BETWEEN"
	_NOTBETWEEN Operator = "NOT BETWEEN"
)
