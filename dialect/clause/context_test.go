package clause

import (
	"context"
	"testing"
)

func TestNewContext(t *testing.T) {
	ctx := NewContext()
	if ctx == nil {
		t.Fatal("NewContext() returned nil")
	}
	if ctx.builder.Len() != 0 {
		t.Errorf("Expected empty builder, got length %d", ctx.builder.Len())
	}
	if len(ctx.args) != 0 {
		t.Errorf("Expected empty args, got length %d", len(ctx.args))
	}
	if ctx.argIndex != 0 {
		t.Errorf("Expected argIndex 0, got %d", ctx.argIndex)
	}

	_ = NewContextFrom(context.Background())
}

func TestContextWriteMethods(t *testing.T) {
	ctx := NewContext()

	// Test WriteByte
	if err := ctx.WriteByte('A'); err != nil {
		t.Errorf("WriteByte failed: %v", err)
	}
	if str := ctx.builder.String(); str != "A" {
		t.Errorf("Expected 'A', got %s", str)
	}

	// Test WriteString
	if err := ctx.WriteString("BC"); err != nil {
		t.Errorf("WriteString failed: %v", err)
	}
	if str := ctx.builder.String(); str != "ABC" {
		t.Errorf("Expected 'ABC', got %s", str)
	}

	// Test Write
	if err := ctx.Write([]byte("DE")); err != nil {
		t.Errorf("Write failed: %v", err)
	}
	if str := ctx.builder.String(); str != "ABCDE" {
		t.Errorf("Expected 'ABCDE', got %s", str)
	}

	// Test WriteInt
	if err := ctx.WriteInt(123); err != nil {
		t.Errorf("WriteInt failed: %v", err)
	}
	if str := ctx.builder.String(); str != "ABCDE123" {
		t.Errorf("Expected 'ABCDE123', got %s", str)
	}
}

func TestContextWritePlaceholderWithArg(t *testing.T) {
	ctx := NewContext()
	cla := NewMySQLClause()

	// Test with different arguments
	tests := []struct {
		name string
		arg  any
		want string
	}{
		{"string", "test", "?"},
		{"int", 123, "?"},
		{"float", 3.14, "?"},
		{"nil", nil, "?"},
		{"bool", true, "?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.builder.Reset()
			ctx.args = nil
			ctx.argIndex = 0

			if err := ctx.WritePlaceholderWithArg(cla, tt.arg); err != nil {
				t.Errorf("WritePlaceholderWithArg failed: %v", err)
			}

			if str := ctx.builder.String(); str != tt.want {
				t.Errorf("Expected %s, got %s", tt.want, str)
			}

			if len(ctx.args) != 1 {
				t.Errorf("Expected 1 arg, got %d", len(ctx.args))
			}

			if ctx.args[0] != tt.arg {
				t.Errorf("Expected arg %v, got %v", tt.arg, ctx.args[0])
			}

			if ctx.argIndex != 1 {
				t.Errorf("Expected argIndex 1, got %d", ctx.argIndex)
			}
		})
	}
}

func TestContextAddArgs(t *testing.T) {
	ctx := NewContext()

	// Test with no args
	ctx.AddArgs()
	if len(ctx.args) != 0 {
		t.Errorf("Expected 0 args, got %d", len(ctx.args))
	}

	// Test with single arg
	ctx.AddArgs("test")
	if len(ctx.args) != 1 {
		t.Errorf("Expected 1 arg, got %d", len(ctx.args))
	}
	if ctx.args[0] != "test" {
		t.Errorf("Expected arg 'test', got %v", ctx.args[0])
	}

	// Test with multiple args
	ctx.AddArgs(1, 2, 3)
	if len(ctx.args) != 4 {
		t.Errorf("Expected 4 args, got %d", len(ctx.args))
	}
	if ctx.argIndex != 4 {
		t.Errorf("Expected argIndex 4, got %d", ctx.argIndex)
	}
}

func TestContextTruncate(t *testing.T) {
	ctx := NewContext()

	// Build some content
	_ = ctx.WriteString("SELECT * FROM users WHERE id = ? AND name = ?")
	ctx.AddArgs(1, "John")

	sqlLen, argNum := ctx.GetSQLLengthAndArgNumber()
	if sqlLen == 0 || argNum == 0 {
		t.Fatal("Failed to build test content")
	}

	// Truncate to half SQL and half args
	newSqlLen := sqlLen / 2
	newArgNum := argNum / 2

	if err := ctx.Truncate(newSqlLen, newArgNum); err != nil {
		t.Errorf("Truncate failed: %v", err)
	}

	if ctx.builder.Len() != newSqlLen {
		t.Errorf("Expected SQL length %d, got %d", newSqlLen, ctx.builder.Len())
	}

	if len(ctx.args) != newArgNum {
		t.Errorf("Expected %d args, got %d", newArgNum, len(ctx.args))
	}

	if ctx.argIndex != newArgNum {
		t.Errorf("Expected argIndex %d, got %d", newArgNum, ctx.argIndex)
	}

	// Test truncate beyond bounds
	_ = ctx.Truncate(1000, 1000) // Should handle gracefully
	_ = ctx.Truncate(-1, -1)     // Should handle gracefully
}

func TestContextGetResult(t *testing.T) {
	ctx := NewContext()

	sql := "SELECT * FROM users WHERE id = ?"
	args := []any{1, "test"}

	_ = ctx.WriteString(sql)
	ctx.AddArgs(args...)

	resultSQL, resultArgs := ctx.GetResult()

	if resultSQL != sql {
		t.Errorf("Expected SQL %s, got %s", sql, resultSQL)
	}

	if len(resultArgs) != len(args) {
		t.Errorf("Expected %d args, got %d", len(args), len(resultArgs))
	}

	for i, arg := range args {
		if resultArgs[i] != arg {
			t.Errorf("Arg %d: expected %v, got %v", i, arg, resultArgs[i])
		}
	}
}

func TestContextReset(t *testing.T) {
	ctx := NewContext()

	// Add content
	_ = ctx.WriteString("SELECT * FROM users")
	ctx.AddArgs(1, 2, 3)

	// Reset
	ctx.Reset()

	if ctx.builder.Len() != 0 {
		t.Errorf("Expected empty builder after reset, got length %d", ctx.builder.Len())
	}

	if len(ctx.args) != 0 {
		t.Errorf("Expected empty args after reset, got length %d", len(ctx.args))
	}

	if ctx.argIndex != 0 {
		t.Errorf("Expected argIndex 0 after reset, got %d", ctx.argIndex)
	}
}

func TestContextGetSQLLengthAndArgNumber(t *testing.T) {
	ctx := NewContext()

	// Initially should be 0, 0
	sqlLen, argNum := ctx.GetSQLLengthAndArgNumber()
	if sqlLen != 0 || argNum != 0 {
		t.Errorf("Expected (0, 0), got (%d, %d)", sqlLen, argNum)
	}

	// Add content
	_ = ctx.WriteString("test")
	ctx.AddArgs(1, 2)

	sqlLen, argNum = ctx.GetSQLLengthAndArgNumber()
	if sqlLen != 4 {
		t.Errorf("Expected SQL length 4, got %d", sqlLen)
	}
	if argNum != 2 {
		t.Errorf("Expected 2 args, got %d", argNum)
	}
}

func BenchmarkContext_(b *testing.B) {

}
