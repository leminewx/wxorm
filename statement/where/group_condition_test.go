package where

import (
	"testing"

	"gitee.com/leminewx/wxorm/dialect/clause"
)

func TestGroupCondition_Build(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	group := &groupCondition{
		operator: _AND,
		conditions: []Condition{
			EQ("name", "John"),
			GT("age", 18),
		},
	}

	if err := group.Build(ctx, cla); err != nil {
		t.Fatalf("Group condition build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	// Expected: ([name](file:///root/automation/test_new_1/TestFlash/echarts-5.4.3/src/util/types.ts#L1457-L1457) = ? AND `age` > ?)
	if sql == "" {
		t.Error("Expected non-empty SQL for group condition")
	}
	if len(args) != 2 {
		t.Errorf("Expected 2 args, got %d", len(args))
	}
}

func TestGroupCondition_Negation(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	group := &groupCondition{
		operator: _AND,
		conditions: []Condition{
			EQ("name", "John"),
			GT("age", 18),
		},
		negated: true,
	}

	if err := group.Build(ctx, cla); err != nil {
		t.Fatalf("Negated group condition build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	// Expected: NOT ([name](file:///root/automation/test_new_1/TestFlash/echarts-5.4.3/src/util/types.ts#L1457-L1457) = ? AND `age` > ?)
	if sql == "" {
		t.Error("Expected non-empty SQL for negated group condition")
	}
	if len(args) != 2 {
		t.Errorf("Expected 2 args, got %d", len(args))
	}
}

func TestGroupCondition_Clone(t *testing.T) {
	original := &groupCondition{
		operator: _OR,
		conditions: []Condition{
			EQ("status", "active"),
			ISNULL("deleted_at"),
		},
		negated: true,
	}

	cloned := original.Clone().(*groupCondition)

	if cloned.operator != original.operator ||
		len(cloned.conditions) != len(original.conditions) ||
		cloned.negated != original.negated {
		t.Error("Cloned group condition does not match original")
	}
}

func TestGroupCondition_IsEmpty(t *testing.T) {
	emptyGroup := &groupCondition{
		operator:   _AND,
		conditions: []Condition{},
	}

	if !emptyGroup.IsEmpty() {
		t.Error("Empty group should return true for IsEmpty")
	}

	notEmptyGroup := &groupCondition{
		operator: _AND,
		conditions: []Condition{
			EQ("name", "John"),
		},
	}

	if notEmptyGroup.IsEmpty() {
		t.Error("Non-empty group should return false for IsEmpty")
	}
}

func TestGroupCondition_AND(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	group := &groupCondition{
		operator: _AND,
		conditions: []Condition{
			EQ("name", "John"),
			GT("age", 18),
		},
	}

	newCond := EQ("city", "NYC")
	combined := group.AND(newCond)

	if err := combined.Build(ctx, cla); err != nil {
		t.Fatalf("Combined group condition build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	// Should create a list condition combining the group and new condition
	if sql == "" {
		t.Error("Expected non-empty SQL for combined condition")
	}
	if len(args) == 0 {
		t.Error("Expected non-empty args for combined condition")
	}
}

func TestGroupCondition_OR(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	group := &groupCondition{
		operator: _AND,
		conditions: []Condition{
			EQ("name", "John"),
			GT("age", 18),
		},
	}

	newCond := EQ("city", "LA")
	combined := group.OR(newCond)

	if err := combined.Build(ctx, cla); err != nil {
		t.Fatalf("OR combined group condition build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	// Should create a list condition combining the group and new condition
	if sql == "" {
		t.Error("Expected non-empty SQL for OR combined condition")
	}
	if len(args) == 0 {
		t.Error("Expected non-empty args for OR combined condition")
	}
}
