package where

import "gitee.com/leminewx/wxorm/dialect/clause"

type listCondition struct {
	operator   Operator
	conditions []Condition
	negated    bool
}

var _ Condition = (*listCondition)(nil)

func (own *listCondition) AND(conditions ...Condition) Condition {
	return own.buildAndOr(_AND, conditions...)
}

func (own *listCondition) OR(conditions ...Condition) Condition {
	return own.buildAndOr(_OR, conditions...)
}

func (own *listCondition) buildAndOr(operator Operator, conditions ...Condition) Condition {
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

func (own *listCondition) NOT() Condition {
	return &listCondition{
		operator:   own.operator,
		conditions: own.conditions,
		negated:    !own.negated,
	}
}

func (own *listCondition) Build(ctx *clause.Context, cla clause.Clause) error {
	// 如果没有条件，直接返回
	if own.IsEmpty() {
		return nil
	}

	// 记录开始位置
	sqlLengthBefore, argNumberBefore := ctx.GetSQLLengthAndArgNumber()

	// 构建 NOT
	if own.negated {
		_ = ctx.WriteString("NOT (")
	}

	// 构建条件
	for i := 0; i < len(own.conditions); i++ {
		cond := own.conditions[i]
		if i > 0 {
			_ = ctx.WriteByte(' ')
			_ = ctx.WriteString(string(own.operator))
			_ = ctx.WriteByte(' ')
		}

		if err := cond.Build(ctx, cla); err != nil {
			return err
		}
	}

	// 如果只有 NOT，但没有有效构建，需要回退
	sqlLengthAfter, _ := ctx.GetSQLLengthAndArgNumber()
	if own.negated && sqlLengthAfter == sqlLengthBefore+5 {
		_ = ctx.Truncate(sqlLengthBefore, argNumberBefore)
	} else if own.negated {
		_ = ctx.WriteByte(')')
	}

	return nil
}

func (own *listCondition) Clone() Condition {
	conditions := make([]Condition, len(own.conditions))
	for i, cond := range own.conditions {
		conditions[i] = cond.Clone()
	}

	return &listCondition{
		operator:   own.operator,
		conditions: conditions,
		negated:    own.negated,
	}
}

func (own *listCondition) IsEmpty() bool {
	return len(own.conditions) == 0
}

func (own *listCondition) GetOperator() Operator {
	return own.operator
}

func (own *listCondition) GetChildren() []Condition {
	return own.conditions
}
