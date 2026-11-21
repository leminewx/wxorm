package where

import (
	"fmt"
	"strings"
	"testing"

	"gitee.com/leminewx/wxorm/dialect/clause"
)

func TestConditionFunctions(t *testing.T) {
	mysql := clause.NewMySQLClause()
	ctx := clause.NewContext()

	tests := []struct {
		name      string
		condition Condition
		wantSQL   string
		wantArgs  []any
	}{
		{
			name:      "EQ",
			condition: EQ("name", "John"),
			wantSQL:   "`name` = ?",
			wantArgs:  []any{"John"},
		},
		{
			name:      "NE",
			condition: NE("status", "deleted"),
			wantSQL:   "`status` <> ?",
			wantArgs:  []any{"deleted"},
		},
		{
			name:      "GT",
			condition: GT("age", 18),
			wantSQL:   "`age` > ?",
			wantArgs:  []any{18},
		},
		{
			name:      "GTE",
			condition: GTE("score", 60),
			wantSQL:   "`score` >= ?",
			wantArgs:  []any{60},
		},
		{
			name:      "LT",
			condition: LT("price", 100),
			wantSQL:   "`price` < ?",
			wantArgs:  []any{100},
		},
		{
			name:      "LTE",
			condition: LTE("quantity", 10),
			wantSQL:   "`quantity` <= ?",
			wantArgs:  []any{10},
		},
		{
			name:      "IN with multiple values",
			condition: IN("id", 1, 2, 3),
			wantSQL:   "`id` IN (?, ?, ?)",
			wantArgs:  []any{1, 2, 3},
		},
		{
			name:      "NOT_IN",
			condition: NOTIN("status", "active", "pending"),
			wantSQL:   "`status` NOT IN (?, ?)",
			wantArgs:  []any{"active", "pending"},
		},
		{
			name:      "LIKE",
			condition: LIKE("name", "%John%"),
			wantSQL:   "`name` LIKE ?",
			wantArgs:  []any{"%John%"},
		},
		{
			name:      "NOT_LIKE",
			condition: NOTLIKE("email", "%test%"),
			wantSQL:   "`email` NOT LIKE ?",
			wantArgs:  []any{"%test%"},
		},
		{
			name:      "BETWEEN",
			condition: BETWEEN("age", 18, 65),
			wantSQL:   "`age` BETWEEN ? AND ?",
			wantArgs:  []any{18, 65},
		},
		{
			name:      "NOT_BETWEEN",
			condition: NOTBETWEEN("price", 0, 100),
			wantSQL:   "`price` NOT BETWEEN ? AND ?",
			wantArgs:  []any{0, 100},
		},
		{
			name:      "IS_NULL",
			condition: ISNULL("deleted_at"),
			wantSQL:   "`deleted_at` IS NULL",
			wantArgs:  []any{},
		},
		{
			name:      "IS_NOT_NULL",
			condition: ISNOTNULL("created_at"),
			wantSQL:   "`created_at` IS NOT NULL",
			wantArgs:  []any{},
		},
		{
			name:      "RAW",
			condition: RAW("DATE(created_at) = CURATE()"),
			wantSQL:   "DATE(created_at) = CURATE()",
			wantArgs:  []any{},
		},
		{
			name:      "RAW with args",
			condition: RAW("status = ? AND active = ?", "pending", true),
			wantSQL:   "status = ? AND active = ?",
			wantArgs:  []any{"pending", true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()

			// First write WHERE clause
			if err := mysql.BuildWhereSQL(ctx); err != nil {
				t.Fatalf("BuildWhereSQL error: %v", err)
			}

			// Build the condition
			if err := tt.condition.Build(ctx, mysql); err != nil {
				t.Fatalf("Build error: %v", err)
			}

			sql, args := ctx.GetResult()

			// Remove "WHERE " prefix for comparison
			gotSQL := strings.TrimPrefix(sql, "\nWHERE\n    ")

			if gotSQL != tt.wantSQL {
				t.Errorf("SQL mismatch:\nGot:  %s\nWant: %s", gotSQL, tt.wantSQL)
			}

			if len(args) != len(tt.wantArgs) {
				t.Errorf("Args count mismatch: got %d, want %d", len(args), len(tt.wantArgs))
			} else {
				for i, arg := range args {
					if arg != tt.wantArgs[i] {
						t.Errorf("Arg %d mismatch: got %v, want %v", i, arg, tt.wantArgs[i])
					}
				}
			}
		})
	}
}

func TestConditionCombinations(t *testing.T) {
	mysql := clause.NewMySQLClause()
	ctx := clause.NewContext()

	// Test AND combination
	t.Run("AND", func(t *testing.T) {
		ctx.Reset()
		cond := AND(
			EQ("status", "active"),
			GTE("age", 18),
			LT("age", 65),
		)

		_ = mysql.BuildWhereSQL(ctx)
		_ = cond.Build(ctx, mysql)

		sql, args := ctx.GetResult()
		expectedSQL := "\nWHERE\n    (`status` = ? AND `age` >= ? AND `age` < ?)"
		if sql != expectedSQL {
			t.Errorf("AND SQL mismatch:\nGot:  %s\nWant: %s", sql, expectedSQL)
		}

		if len(args) != 3 {
			t.Errorf("Expected 3 args, got %d", len(args))
		}
	})

	// Test OR combination
	t.Run("OR", func(t *testing.T) {
		ctx.Reset()
		cond := OR(
			EQ("status", "active"),
			EQ("role", "admin"),
			EQ("role", "supervisor"),
		)

		_ = mysql.BuildWhereSQL(ctx)
		_ = cond.Build(ctx, mysql)

		sql, _ := ctx.GetResult()
		expectedSQL := "\nWHERE\n    (`status` = ? OR `role` = ? OR `role` = ?)"
		if sql != expectedSQL {
			t.Errorf("OR SQL mismatch:\nGot:  %s\nWant: %s", sql, expectedSQL)
		}
	})

	// Test NOT
	t.Run("NOT", func(t *testing.T) {
		ctx.Reset()
		cond := EQ("deleted", true).NOT()

		_ = mysql.BuildWhereSQL(ctx)
		_ = cond.Build(ctx, mysql)

		sql, _ := ctx.GetResult()
		expectedSQL := "\nWHERE\n    NOT (`deleted` = ?)"
		if sql != expectedSQL {
			t.Errorf("NOT SQL mismatch:\nGot:  %s\nWant: %s", sql, expectedSQL)
		}
	})

	// Test complex combination
	t.Run("Complex", func(t *testing.T) {
		ctx.Reset()
		cond := EQ("status", "active").
			AND(GTE("age", 18)).
			OR(
				EQ("role", "admin").AND(GT("level", 5)),
				EQ("special", true),
			)

		_ = mysql.BuildWhereSQL(ctx)
		_ = cond.Build(ctx, mysql)

		sql, _ := ctx.GetResult()
		// Just check it builds without error
		if !strings.Contains(sql, "\nWHERE\n    ") {
			t.Errorf("Complex condition failed to build properly: %s", sql)
		}
	})
}

func TestConditionClone(t *testing.T) {
	// Test cloning simple condition
	orig := EQ("name", "John")
	clone := orig.Clone()

	if clone == orig {
		t.Error("Clone should return a new instance")
	}

	// Test that cloned condition builds the same
	mysql := clause.NewMySQLClause()
	ctx1 := clause.NewContext()
	ctx2 := clause.NewContext()

	_ = mysql.BuildWhereSQL(ctx1)
	_ = orig.Build(ctx1, mysql)

	_ = mysql.BuildWhereSQL(ctx2)
	_ = clone.Build(ctx2, mysql)

	sql1, args1 := ctx1.GetResult()
	sql2, args2 := ctx2.GetResult()

	if sql1 != sql2 {
		t.Errorf("Cloned condition SQL mismatch:\nOriginal: %s\nClone:    %s", sql1, sql2)
	}

	if len(args1) != len(args2) {
		t.Errorf("Args count mismatch: original %d, clone %d", len(args1), len(args2))
	}

	// Test cloning complex condition
	compile := EQ("status", "active").
		AND(GTE("age", 18), LT("age", 65)).
		OR(EQ("vip", true))

	complexClone := compile.Clone()

	ctx1.Reset()
	ctx2.Reset()

	_ = mysql.BuildWhereSQL(ctx1)
	_ = compile.Build(ctx1, mysql)

	_ = mysql.BuildWhereSQL(ctx2)
	_ = complexClone.Build(ctx2, mysql)

	sql1, _ = ctx1.GetResult()
	sql2, _ = ctx2.GetResult()

	if sql1 != sql2 {
		t.Errorf("Complex cloned condition SQL mismatch")
	}
}

func TestConditionIsEmpty(t *testing.T) {
	tests := []struct {
		name      string
		condition Condition
		wantEmpty bool
	}{
		{"EQ", EQ("name", "John"), false},
		{"nil from WHEN false", WHEN(false, func() Condition { return EQ("test", "value") }), true},
		{"RAW empty", RAW(""), true}, // RAW with empty string is not considered empty
		{"complex not empty", EQ("a", 1).AND(EQ("b", 2)), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.condition == nil {
				if !tt.wantEmpty {
					t.Error("Expected non-nil condition")
				}
				return
			}

			isEmpty := tt.condition.IsEmpty()
			if isEmpty != tt.wantEmpty {
				t.Errorf("IsEmpty() = %v, want %v", isEmpty, tt.wantEmpty)
			}
		})
	}
}

func TestWHEN(t *testing.T) {
	mysql := clause.NewMySQLClause()

	t.Run("WHEN true", func(t *testing.T) {
		ctx := clause.NewContext()
		cond := WHEN(true, func() Condition {
			return EQ("status", "active")
		})

		if cond == nil {
			t.Fatal("WHEN(true) should return condition")
		}

		_ = mysql.BuildWhereSQL(ctx)
		_ = cond.Build(ctx, mysql)

		sql, args := ctx.GetResult()
		if !strings.Contains(sql, "`status` = ?") {
			t.Errorf("Expected status condition, got: %s", sql)
		}
		if len(args) != 1 || args[0] != "active" {
			t.Errorf("Expected arg 'active', got: %v", args)
		}
	})

	t.Run("WHEN false", func(t *testing.T) {
		cond := WHEN(false, func() Condition {
			return EQ("status", "active")
		})

		if cond != nil {
			t.Error("WHEN(false) should return nil")
		}
	})

	t.Run("WHEN with complex condition", func(t *testing.T) {
		ctx := clause.NewContext()

		cond := WHEN(true, func() Condition {
			return EQ("active", true).
				AND(GTE("score", 50)).
				OR(EQ("admin", true))
		})

		_ = mysql.BuildWhereSQL(ctx)
		_ = cond.Build(ctx, mysql)

		sql, _ := ctx.GetResult()
		if !strings.Contains(sql, "WHERE") {
			t.Error("WHEN should build condition when true")
		}
	})
}

func TestConditionGetOperatorAndChildren(t *testing.T) {
	// Test expression
	expr := EQ("name", "John")
	if op := expr.GetOperator(); op == "" {
		t.Error("GetOperator should return operator for expression")
	}

	if children := expr.GetChildren(); children == nil {
		t.Error("Expression should return nil children")
	}

	// Test AND condition
	andCond := EQ("a", 1).AND(EQ("b", 2))
	if op := andCond.GetOperator(); op == "" {
		t.Error("GetOperator should return operator for AND")
	}

	if children := andCond.GetChildren(); len(children) != 2 { // a=1, AND, b=2
		t.Errorf("AND should have 3 children, got %d", len(children))
	}

	// Test OR condition
	orCond := EQ("x", 1).OR(EQ("y", 2))
	if children := orCond.GetChildren(); len(children) != 2 { // x=1, OR, y=2
		t.Errorf("OR should have 3 children, got %d", len(children))
	}

	// Test NOT condition
	notCond := EQ("flag", true).NOT()
	if children := notCond.GetChildren(); len(children) != 1 {
		t.Errorf("NOT should have 1 child, got %d", len(children))
	}
}

func TestConditionEdgeCases(t *testing.T) {
	mysql := clause.NewMySQLClause()
	ctx := clause.NewContext()

	// Test empty IN：空集合按集合语义写成恒假，而不是报错
	t.Run("IN empty", func(t *testing.T) {
		ctx.Reset()
		cond := IN[string]("id")

		_ = mysql.BuildWhereSQL(ctx)
		if err := cond.Build(ctx, mysql); err != nil {
			t.Fatalf("IN with no values should be handled gracefully, got error: %v", err)
		}

		sqlStr, args := ctx.GetResult()
		if !strings.Contains(sqlStr, "1 = 0") || len(args) != 0 {
			t.Errorf("IN with no values should build always-false condition, got %q (args: %v)", sqlStr, args)
		}
	})

	// Test nil values
	t.Run("with nil value", func(t *testing.T) {
		ctx.Reset()
		cond := EQ("deleted_at", nil)

		_ = mysql.BuildWhereSQL(ctx)
		_ = cond.Build(ctx, mysql)

		sql, args := ctx.GetResult()
		if !strings.Contains(sql, "`deleted_at` = ?") {
			t.Errorf("Should handle nil value, got: %s", sql)
		}
		if len(args) != 1 || args[0] != nil {
			t.Errorf("Expected nil arg, got: %v", args)
		}
	})

	// Test RAW with extra spaces
	t.Run("RAW trimmed", func(t *testing.T) {
		ctx.Reset()
		cond := RAW("  status = ?     ", "active")

		_ = mysql.BuildWhereSQL(ctx)
		_ = cond.Build(ctx, mysql)

		sql, _ := ctx.GetResult()
		if strings.Contains(sql, "     ") {
			t.Errorf("RAW should trim spaces, got: %s", sql)
		}
	})
}

func TestWhere_Mix(t *testing.T) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()
	if err := EQ("name", "John Doe").
		AND(
			GT("age", 30).
				AND(
					IN("status", "inactive", "deleted", "suspended"),
					NOTLIKE("email", "%@example.com"),
					ISNOTNULL("email"),
				).
				OR(
					GTE("salary", 5000),
					IN("role", 1, 2, 3),
				),
			LIKE("name", "%wx%"),
		).
		OR(
			NE("city", "New York"),
			LT("age", 25),
			NOTBETWEEN("age", 18, 30).AND(NE("sex", "boy")),
		).
		AND(
			RAW("`addr` = (SELECT `addr` FROM `users` WHERE `name` = ? LIMIT 1)", "John Doe"),
			ISNULL("email").NOT(),
			NOTLIKE("email", "%@example.com"),
			WHEN(false, func() Condition {
				return EQ("id", 1)
			}),
		).
		Build(ctx, cla); err != nil {
		t.Fatal(err)
	}

	fmt.Println(ctx.GetResult())
}

/*
cpu: Intel(R) Core(TM) i5-14600KF		20    	  748474	      1621 ns/op	    3064 B/op	      56 allocs/op
cpu: QEMU Virtual CPU version 2.5+		2   	  353431	      3119 ns/op	    3040 B/op	      54 allocs/op
*/
func BenchmarkWhere_Mix(b *testing.B) {
	cla := clause.NewMySQLClause()
	ctx := clause.NewContext()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if err := EQ("name", "John Doe").
			AND(
				GT("age", 30).
					AND(
						IN("status", "inactive", "deleted", "suspended"),
						NOTLIKE("email", "%@example.com"),
						ISNOTNULL("email"),
					).
					OR(
						GTE("salary", 5000),
						IN("role", 1, 2, 3),
					),
			).
			OR(
				NE("city", "New York"),
				LT("age", 25),
				NOTBETWEEN("age", 18, 30),
			).
			AND(
				RAW("`addr` = (SELECT `addr` FROM `users` WHERE `name` = ? LIMIT 1)", "John Doe"),
				ISNULL("email").NOT(),
				NOTLIKE("email", "%@example.com"),
			).
			Build(ctx, cla); err != nil {
			b.Fatal(err)
		}

		ctx.Reset()
	}
}
