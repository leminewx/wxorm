package where

import "gitee.com/leminewx/wxorm/dialect/clause"

// Condition 定义条件的接口类型
type Condition interface {
	// AND 组合条件（与）
	AND(conditions ...Condition) Condition
	// OR 组合条件（或）
	OR(conditions ...Condition) Condition
	// NOT 条件取反（非）
	NOT() Condition

	// Build 构建 SQL 语句和参数
	Build(ctx *clause.Context, cla clause.Clause) error
	// Clone 复制条件
	Clone() Condition
	// IsEmpty 判断条件是否为空
	IsEmpty() bool
	// GetOperator 获取条件操作符
	GetOperator() Operator
	// GetChildren 获取条件子项（用于 AND/OR/NOT 组合条件）
	GetChildren() []Condition
}

// GetValidConditions 获取有效的条件
func GetValidConditions(conditions []Condition) []Condition {
	validConditions := make([]Condition, 0, len(conditions))
	for _, cond := range conditions {
		if cond != nil && !cond.IsEmpty() {
			validConditions = append(validConditions, cond)
		}
	}

	return validConditions
}
