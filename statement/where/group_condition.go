package where

import "gitee.com/leminewx/wxorm/dialect/clause"

type groupCondition struct {
	operator   Operator
	conditions []Condition
	negated    bool
}

var _ Condition = (*groupCondition)(nil)

func (own *groupCondition) AND(conditions ...Condition) Condition {
	return own.buildAndOr(_AND, conditions...)
}

func (own *groupCondition) OR(conditions ...Condition) Condition {
	return own.buildAndOr(_OR, conditions...)
}

func (own *groupCondition) buildAndOr(operator Operator, conditions ...Condition) Condition {
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

func (own *groupCondition) NOT() Condition {
	return &groupCondition{
		operator:   own.operator,
		conditions: own.conditions,
		negated:    !own.negated,
	}
}

func (own *groupCondition) Build(ctx *clause.Context, cla clause.Clause) error {
	// 如果没有条件，直接返回
	if own.IsEmpty() {
		return nil
	}

	// 记录开始位置
	sqlLengthBefore, argNumberBefore := ctx.GetSQLLengthAndArgNumber()

	if own.negated {
		// 构建 NOT
		_ = ctx.WriteString("NOT (")
	} else if !own.negated && len(own.conditions) > 1 {
		// 多个非 NOT 条件需要使用 () 括号
		_ = ctx.WriteByte('(')
	}

	// 构建条件
	for i := 0; i < len(own.conditions); i++ {
		if i > 0 {
			_ = ctx.WriteByte(' ')
			_ = ctx.WriteString(string(own.operator))
			_ = ctx.WriteByte(' ')
		}

		if err := own.conditions[i].Build(ctx, cla); err != nil {
			return err
		}
	}

	// 如果只有 NOT，但没有有效构建，需要回退
	sqlLengthAfter, _ := ctx.GetSQLLengthAndArgNumber()
	if (own.negated && sqlLengthAfter == sqlLengthBefore+5) || (!own.negated && sqlLengthAfter == sqlLengthBefore+1) {
		_ = ctx.Truncate(sqlLengthBefore, argNumberBefore)
	} else if own.negated || (!own.negated && len(own.conditions) > 1) {
		_ = ctx.WriteByte(')')
	}

	return nil
}

func (own *groupCondition) Clone() Condition {
	conditions := make([]Condition, len(own.conditions))
	for i, cond := range own.conditions {
		conditions[i] = cond.Clone()
	}

	return &groupCondition{
		operator:   own.operator,
		conditions: conditions,
		negated:    own.negated,
	}
}

func (own *groupCondition) IsEmpty() bool {
	return len(own.conditions) == 0
}

func (own *groupCondition) GetOperator() Operator {
	return own.operator
}

func (own *groupCondition) GetChildren() []Condition {
	return own.conditions
}
