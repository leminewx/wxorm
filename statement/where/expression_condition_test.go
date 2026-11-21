package where

import (
	"testing"

	"gitee.com/leminewx/wxorm/dialect/clause"
)

func TestExpression(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond1 := &expression{
		column:   "name",
		operator: "=",
		value:    "John Doe",
	}
	if err := cond1.Build(ctx, cla); err != nil {
		t.Errorf("Error building raw condition: %v", err)
	}
	t.Log(ctx.GetResult())
	ctx.Reset()

	cond2 := cond1.AND(&expression{
		column:   "age",
		operator: ">=",
		value:    18,
	})
	if err := cond2.Build(ctx, cla); err != nil {
		t.Errorf("Error building raw condition: %v", err)
	}
	t.Log(ctx.GetResult())
	ctx.Reset()
}
