package where

import (
	"testing"

	"gitee.com/leminewx/wxorm/dialect/clause"
)

func TestEQ(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := EQ("name", "John")

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("EQ build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	expectedSQL := "`name` = ?"
	expectedArgs := []interface{}{"John"}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if len(args) != len(expectedArgs) || args[0] != expectedArgs[0] {
		t.Errorf("Expected args %v, got %v", expectedArgs, args)
	}
}

func TestNE(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := NE("status", "active")

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("NE build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	expectedSQL := "`status` <> ?"
	expectedArgs := []interface{}{"active"}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if len(args) != len(expectedArgs) || args[0] != expectedArgs[0] {
		t.Errorf("Expected args %v, got %v", expectedArgs, args)
	}
}

func TestGT(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := GT("age", 18)

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("GT build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	expectedSQL := "`age` > ?"
	expectedArgs := []interface{}{18}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if len(args) != len(expectedArgs) || args[0] != expectedArgs[0] {
		t.Errorf("Expected args %v, got %v", expectedArgs, args)
	}
}

func TestGTE(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := GTE("score", 80)

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("GTE build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	expectedSQL := "`score` >= ?"
	expectedArgs := []interface{}{80}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if len(args) != len(expectedArgs) || args[0] != expectedArgs[0] {
		t.Errorf("Expected args %v, got %v", expectedArgs, args)
	}
}

func TestLT(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := LT("price", 100)

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("LT build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	expectedSQL := "`price` < ?"
	expectedArgs := []interface{}{100}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if len(args) != len(expectedArgs) || args[0] != expectedArgs[0] {
		t.Errorf("Expected args %v, got %v", expectedArgs, args)
	}
}

func TestLTE(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := LTE("count", 10)

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("LTE build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	expectedSQL := "`count` <= ?"
	expectedArgs := []interface{}{10}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if len(args) != len(expectedArgs) || args[0] != expectedArgs[0] {
		t.Errorf("Expected args %v, got %v", expectedArgs, args)
	}
}

func TestIN(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := IN("status", "active", "pending", "review")

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("IN build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	expectedSQL := "`status` IN (?, ?, ?)"
	expectedArgs := []interface{}{"active", "pending", "review"}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if len(args) != len(expectedArgs) {
		t.Errorf("Expected %d args, got %d", len(expectedArgs), len(args))
	}

	for i, exp := range expectedArgs {
		if args[i] != exp {
			t.Errorf("Expected arg at index %d to be %v, got %v", i, exp, args[i])
		}
	}
}

func TestNOT_IN(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := NOTIN("category", "admin", "moderator")

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("NOT_IN build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	expectedSQL := "`category` NOT IN (?, ?)"
	expectedArgs := []interface{}{"admin", "moderator"}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if len(args) != len(expectedArgs) {
		t.Errorf("Expected %d args, got %d", len(expectedArgs), len(args))
	}

	for i, exp := range expectedArgs {
		if args[i] != exp {
			t.Errorf("Expected arg at index %d to be %v, got %v", i, exp, args[i])
		}
	}
}

func TestLIKE(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := LIKE("name", "%john%")

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("LIKE build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	expectedSQL := "`name` LIKE ?"
	expectedArgs := []interface{}{"%john%"}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if len(args) != len(expectedArgs) || args[0] != expectedArgs[0] {
		t.Errorf("Expected args %v, got %v", expectedArgs, args)
	}
}

func TestNOT_LIKE(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := NOTLIKE("email", "%@spam.com")

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("NOT_LIKE build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	expectedSQL := "`email` NOT LIKE ?"
	expectedArgs := []interface{}{"%@spam.com"}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if len(args) != len(expectedArgs) || args[0] != expectedArgs[0] {
		t.Errorf("Expected args %v, got %v", expectedArgs, args)
	}
}

func TestBETWEEN(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := BETWEEN("age", 18, 65)

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("BETWEEN build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	expectedSQL := "`age` BETWEEN ? AND ?"
	expectedArgs := []interface{}{18, 65}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if len(args) != len(expectedArgs) {
		t.Errorf("Expected %d args, got %d", len(expectedArgs), len(args))
	}

	for i, exp := range expectedArgs {
		if args[i] != exp {
			t.Errorf("Expected arg at index %d to be %v, got %v", i, exp, args[i])
		}
	}
}

func TestNOT_BETWEEN(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := NOTBETWEEN("score", 0, 100)

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("NOT_BETWEEN build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	expectedSQL := "`score` NOT BETWEEN ? AND ?"
	expectedArgs := []interface{}{0, 100}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if len(args) != len(expectedArgs) {
		t.Errorf("Expected %d args, got %d", len(expectedArgs), len(args))
	}

	for i, exp := range expectedArgs {
		if args[i] != exp {
			t.Errorf("Expected arg at index %d to be %v, got %v", i, exp, args[i])
		}
	}
}

func TestIS_NULL(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := ISNULL("deleted_at")

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("IS_NULL build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	expectedSQL := "`deleted_at` IS NULL"
	var expectedArgs []any

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if len(args) != len(expectedArgs) {
		t.Errorf("Expected %d args, got %d", len(expectedArgs), len(args))
	}
}

func TestIS_NOT_NULL(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := ISNOTNULL("created_at")

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("IS_NOT_NULL build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	expectedSQL := "`created_at` IS NOT NULL"
	var expectedArgs []any

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if len(args) != len(expectedArgs) {
		t.Errorf("Expected %d args, got %d", len(expectedArgs), len(args))
	}
}

func TestRAW(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := RAW("`name` = ? AND `age` >= ?", "John", 25)

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("RAW build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	expectedSQL := "`name` = ? AND `age` >= ?"
	expectedArgs := []interface{}{"John", 25}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if len(args) != len(expectedArgs) {
		t.Errorf("Expected %d args, got %d", len(expectedArgs), len(args))
	}

	for i, exp := range expectedArgs {
		if args[i] != exp {
			t.Errorf("Expected arg at index %d to be %v, got %v", i, exp, args[i])
		}
	}
}

func TestConditionCombination(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := EQ("name", "John").
		AND(GT("age", 18)).
		OR(LTE("balance", 100))

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("Combined condition build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	if sql == "" {
		t.Error("Expected non-empty SQL for combined condition")
	}
	if len(args) == 0 {
		t.Error("Expected non-empty args for combined condition")
	}
}

func TestConditionNegation(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	cond := EQ("status", "active").NOT()

	if err := cond.Build(ctx, cla); err != nil {
		t.Fatalf("NOT condition build failed: %v", err)
	}

	sql, args := ctx.GetResult()
	expectedSQL := "NOT (`status` = ?)"
	expectedArgs := []interface{}{"active"}

	if sql != expectedSQL {
		t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql)
	}

	if len(args) != len(expectedArgs) || args[0] != expectedArgs[0] {
		t.Errorf("Expected args %v, got %v", expectedArgs, args)
	}
}

func TestGetValidConditions(t *testing.T) {
	conditions := []Condition{
		EQ("name", "John"),
		nil,
		ISNULL("deleted_at"),
		func() Condition {
			c := EQ("empty", "")
			if c.IsEmpty() {
				return nil
			}

			return c
		}(),
	}

	validConditions := GetValidConditions(conditions)
	if len(validConditions) != 3 {
		t.Errorf("Expected 3 valid conditions, got %d", len(validConditions))
	}
}
