package clause

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewMySQLClause(t *testing.T) {
	mysql := NewMySQLClause()
	if mysql == nil {
		t.Fatal("NewMySQLClause() returned nil")
	}
}

func TestMySQLGetLikeOperator(t *testing.T) {
	mysql := NewMySQLClause()
	op := mysql.GetLikeOperator()
	if op != "LIKE" {
		t.Errorf("Expected 'LIKE', got %s", op)
	}
}

func TestMySQLGetPlaceholder(t *testing.T) {
	mysql := NewMySQLClause()

	// MySQL should always return "?" regardless of index
	tests := []int{0, 1, 10, 100}
	for _, idx := range tests {
		placeholder := mysql.GetPlaceholder(idx)
		if placeholder != "?" {
			t.Errorf("Expected '?' for index %d, got %s", idx, placeholder)
		}
	}
}

func TestMySQLQuoteIdentifier(t *testing.T) {
	mysql := NewMySQLClause()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple column", "id", "`id`"},
		{"table.column", "users.id", "`users`.`id`"},
		{"multiple dots", "db.table.column", "`db`.`table`.`column`"},
		{"backticks in name", "`special`", "```special```"},
		{"empty string", "", "``"},
		{"with spaces", "column name", "`column name`"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mysql.QuoteIdentifier(tt.input)
			if result != tt.expected {
				t.Errorf("QuoteIdentifier(%q) = %q, want %q", tt.input, result, tt.expected)
			}

			// Test caching by calling again
			result2 := mysql.QuoteIdentifier(tt.input)
			if result2 != tt.expected {
				t.Errorf("Cached QuoteIdentifier(%q) = %q, want %q", tt.input, result2, tt.expected)
			}
		})
	}
}

func TestMySQLBuildPlaceholders(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		num      int
		expected string
	}{
		{"1 placeholder", 1, "?"},
		{"2 placeholders", 2, "?, ?"},
		{"3 placeholders", 3, "?, ?, ?"},
		{"10 placeholders", 10, strings.Repeat("?, ", 9) + "?"},
		{"30 placeholders", 30, strings.Repeat("?, ", 29) + "?"},
		{"31 placeholders", 31, strings.Repeat("?, ", 30) + "?"},
		{"0 placeholders", 0, ""}, // Should handle gracefully
		{"negative", -1, ""},      // Should handle gracefully
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildPlaceholders(&ctx.builder, 0, tt.num)

			if tt.num <= 0 {
				// Should handle edge cases
				return
			}

			if err != nil {
				t.Errorf("BuildPlaceholders(%d) error: %v", tt.num, err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildPlaceholders(%d) = %q, want %q", tt.num, result, tt.expected)
			}

			// Verify number of ? matches
			count := strings.Count(result, "?")
			if count != tt.num {
				t.Errorf("Expected %d placeholders, got %d", tt.num, count)
			}
		})
	}
}

func TestMySQLBuildInsertSQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		table    string
		ignore   bool
		columns  []string
		expected string
	}{
		{
			name:     "simple insert",
			table:    "users",
			ignore:   false,
			columns:  []string{"id", "name"},
			expected: "INSERT INTO\n    `users` (`id`, `name`)",
		},
		{
			name:     "insert ignore",
			table:    "users",
			ignore:   true,
			columns:  []string{"id"},
			expected: "INSERT IGNORE INTO\n    `users` (`id`)",
		},
		{
			name:     "no columns",
			table:    "users",
			ignore:   false,
			columns:  []string{},
			expected: "INSERT INTO\n    `users`",
		},
		{
			name:     "special chars in table name",
			table:    "user_profiles",
			ignore:   false,
			columns:  []string{"user_id", "profile_data"},
			expected: "INSERT INTO\n    `user_profiles` (`user_id`, `profile_data`)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildInsertSQL(ctx, tt.table, tt.ignore, tt.columns...)
			if err != nil {
				t.Errorf("BuildInsertSQL error: %v", err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildInsertSQL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestMySQLBuildReplaceSQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		table    string
		columns  []string
		expected string
	}{
		{
			name:     "simple replace",
			table:    "users",
			columns:  []string{"id", "name"},
			expected: "REPLACE INTO\n    `users` (`id`, `name`)",
		},
		{
			name:     "single column",
			table:    "settings",
			columns:  []string{"value"},
			expected: "REPLACE INTO\n    `settings` (`value`)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildReplaceSQL(ctx, tt.table, tt.columns...)
			if err != nil {
				t.Errorf("BuildReplaceSQL error: %v", err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildReplaceSQL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestMySQLBuildValuesSQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		values   [][]any
		expected string
	}{
		{
			name: "single row",
			values: [][]any{
				{1, "John"},
			},
			expected: "\nVALUES\n    (?, ?)",
		},
		{
			name: "multiple rows",
			values: [][]any{
				{1, "John"},
				{2, "Jane"},
				{3, "Bob"},
			},
			expected: "\nVALUES\n    (?, ?),\n    (?, ?),\n    (?, ?)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildValuesSQL(ctx, 0, tt.values...)
			if err != nil {
				t.Errorf("BuildValuesSQL error: %v", err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildValuesSQL() = %q, want %q", result, tt.expected)
			}

			// Verify args were added
			expectedArgCount := 0
			for _, row := range tt.values {
				expectedArgCount += len(row)
			}
			if len(ctx.args) != expectedArgCount {
				t.Errorf("Expected %d args, got %d", expectedArgCount, len(ctx.args))
			}
		})
	}

	// 空行集合必须返回错误，而不是 panic
	t.Run("empty", func(t *testing.T) {
		ctx.Reset()
		if err := mysql.BuildValuesSQL(ctx, 0); !errors.Is(err, ErrValuesIsEmpty) {
			t.Errorf("BuildValuesSQL() error = %v, want ErrValuesIsEmpty", err)
		}

		if ctx.builder.Len() != 0 {
			t.Errorf("BuildValuesSQL() = %q, want empty", ctx.builder.String())
		}
	})
}

func TestMySQLBuildDuplicateSQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		columns  []string
		expected string
	}{
		{
			name:     "single column",
			columns:  []string{"name"},
			expected: "\nON DUPLICATE KEY UPDATE\n    `name` = VALUES(`name`)",
		},
		{
			name:     "multiple columns",
			columns:  []string{"name", "email", "age"},
			expected: "\nON DUPLICATE KEY UPDATE\n    `name` = VALUES(`name`),\n    `email` = VALUES(`email`),\n    `age` = VALUES(`age`)",
		},
		{
			name:     "no columns",
			columns:  []string{},
			expected: "\nON DUPLICATE KEY UPDATE\n    ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildDuplicateSQL(ctx, tt.columns...)
			if err != nil {
				t.Errorf("BuildDuplicateSQL error: %v", err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildDuplicateSQL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestMySQLBuildUpdateSQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		table    string
		expected string
	}{
		{"simple table", "users", "UPDATE\n    `users`"},
		{"table with dots", "db.users", "UPDATE\n    `db`.`users`"},
		{"special chars", "user_settings", "UPDATE\n    `user_settings`"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildUpdateSQL(ctx, tt.table)
			if err != nil {
				t.Errorf("BuildUpdateSQL error: %v", err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildUpdateSQL() = %q, want %q", result, tt.expected)
			}

			// Test caching by calling again
			ctx.Reset()
			_ = mysql.BuildUpdateSQL(ctx, tt.table)
			if ctx.builder.String() != tt.expected {
				t.Errorf("Cached BuildUpdateSQL() = %q, want %q", ctx.builder.String(), tt.expected)
			}
		})
	}
}

func TestMySQLBuildSetSQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		values   map[string]any
		expected string
	}{
		{
			name: "single value",
			values: map[string]any{
				"name": "John",
			},
			expected: "\nSET\n    `name` = ?",
		},
		{
			name: "multiple values",
			values: map[string]any{
				"name":  "John",
				"email": "john@example.com",
				"age":   25,
			},
			// 字段按名称排序，保证同一份数据生成的 SQL 文本稳定
			expected: "\nSET\n    `age` = ?, `email` = ?, `name` = ?",
		},
		{
			name:     "empty map",
			values:   map[string]any{},
			expected: "\nSET\n    ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildSetSQL(ctx, 0, tt.values)
			if err != nil {
				t.Errorf("BuildSetSQL error: %v", err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildSetSQL() = %q, want %q", result, tt.expected)
			}

			// Verify args were added
			if len(ctx.args) != len(tt.values) {
				t.Errorf("Expected %d args, got %d", len(tt.values), len(ctx.args))
			}
		})
	}
}

func TestMySQLBuildDeleteSQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		table    string
		expected string
	}{
		{"simple table", "users", "DELETE FROM\n    `users`"},
		{"table with schema", "prod.users", "DELETE FROM\n    `prod`.`users`"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildDeleteSQL(ctx, tt.table)
			if err != nil {
				t.Errorf("BuildDeleteSQL error: %v", err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildDeleteSQL() = %q, want %q", result, tt.expected)
			}

			// Test caching
			ctx.Reset()
			_ = mysql.BuildDeleteSQL(ctx, tt.table)
			if ctx.builder.String() != tt.expected {
				t.Errorf("Cached BuildDeleteSQL() = %q, want %q", ctx.builder.String(), tt.expected)
			}
		})
	}
}

func TestMySQLBuildSelectSQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		columns  []string
		expected string
	}{
		{
			name:     "star",
			columns:  []string{},
			expected: "SELECT\n    *",
		},
		{
			name:     "single column",
			columns:  []string{"id"},
			expected: "SELECT\n    `id`",
		},
		{
			name:     "multiple columns",
			columns:  []string{"id", "name", "email"},
			expected: "SELECT\n    `id`, `name`, `email`",
		},
		{
			name:     "table qualified",
			columns:  []string{"users.id", "users.name"},
			expected: "SELECT\n    `users`.`id`, `users`.`name`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildSelectSQL(ctx, "test", tt.columns...)
			if err != nil {
				t.Errorf("BuildSelectSQL error: %v", err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildSelectSQL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestMySQLBuildFromSQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		table    string
		expected string
	}{
		{"simple table", "users", "\nFROM\n    `users`"},
		{"qualified table", "db.users", "\nFROM\n    `db`.`users`"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildFromSQL(ctx, tt.table)
			if err != nil {
				t.Errorf("BuildFromSQL error: %v", err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildFromSQL() = %q, want %q", result, tt.expected)
			}

			// Test caching
			ctx.Reset()
			_ = mysql.BuildFromSQL(ctx, tt.table)
			if ctx.builder.String() != tt.expected {
				t.Errorf("Cached BuildFromSQL() = %q, want %q", ctx.builder.String(), tt.expected)
			}
		})
	}
}

func TestMySQLBuildWhereSQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	err := mysql.BuildWhereSQL(ctx)
	if err != nil {
		t.Errorf("BuildWhereSQL error: %v", err)
	}

	result := ctx.builder.String()
	if result != "\nWHERE\n    " {
		t.Errorf("BuildWhereSQL() = %q, want 'WHERE'", result)
	}
}

func TestMySQLBuildGroupBySQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		columns  []string
		expected string
	}{
		{
			name:     "single column",
			columns:  []string{"department"},
			expected: "\nGROUP BY\n    `department`",
		},
		{
			name:     "multiple columns",
			columns:  []string{"department", "status"},
			expected: "\nGROUP BY\n    `department`, `status`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildGroupBySQL(ctx, tt.columns...)
			if err != nil {
				t.Errorf("BuildGroupBySQL error: %v", err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildGroupBySQL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestMySQLBuildHavingSQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	err := mysql.BuildHavingSQL(ctx)
	if err != nil {
		t.Errorf("BuildHavingSQL error: %v", err)
	}

	result := ctx.builder.String()
	if result != "\nHAVING\n    " {
		t.Errorf("BuildHavingSQL() = %q, want 'HAVING'", result)
	}
}

func TestMySQLBuildOrderBySQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		columns  []string
		expected string
	}{
		{
			name:     "single column",
			columns:  []string{"name"},
			expected: "\nORDER BY\n    `name`",
		},
		{
			name:     "multiple columns",
			columns:  []string{"last_name", "first_name"},
			expected: "\nORDER BY\n    `last_name`, `first_name`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildOrderBySQL(ctx, tt.columns...)
			if err != nil {
				t.Errorf("BuildOrderBySQL error: %v", err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildOrderBySQL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestMySQLBuildLimitSQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		limit    int
		expected string
	}{
		{"limit 1", 1, "\nLIMIT\n    1"},
		{"limit 10", 10, "\nLIMIT\n    10"},
		{"limit 0", 0, "\nLIMIT\n    0"},
		{"limit large", 1000, "\nLIMIT\n    1000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildLimitSQL(ctx, tt.limit)
			if err != nil {
				t.Errorf("BuildLimitSQL error: %v", err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildLimitSQL(%d) = %q, want %q", tt.limit, result, tt.expected)
			}

			// Test caching
			ctx.Reset()
			_ = mysql.BuildLimitSQL(ctx, tt.limit)
			if ctx.builder.String() != tt.expected {
				t.Errorf("Cached BuildLimitSQL(%d) = %q, want %q", tt.limit, ctx.builder.String(), tt.expected)
			}
		})
	}
}

func TestMySQLBuildOffsetSQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		offset   int
		expected string
	}{
		{"offset 0", 0, "\nOFFSET\n    0"},
		{"offset 10", 10, "\nOFFSET\n    10"},
		{"offset 100", 100, "\nOFFSET\n    100"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildOffsetSQL(ctx, tt.offset)
			if err != nil {
				t.Errorf("BuildOffsetSQL error: %v", err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildOffsetSQL(%d) = %q, want %q", tt.offset, result, tt.expected)
			}

			// Test caching (uses same cache as limit)
			ctx.Reset()
			_ = mysql.BuildOffsetSQL(ctx, tt.offset)
			if ctx.builder.String() != tt.expected {
				t.Errorf("Cached BuildOffsetSQL(%d) = %q, want %q", tt.offset, ctx.builder.String(), tt.expected)
			}
		})
	}
}

func TestMySQLBuildJSONExtractSQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		column   string
		path     string
		expected string
	}{
		{
			name:     "simple path",
			column:   "metadata",
			path:     "settings.theme",
			expected: "JSON_EXTRACT(`metadata`, '$.settings.theme')",
		},
		{
			name:     "array path",
			column:   "data",
			path:     "tags[0]",
			expected: "JSON_EXTRACT(`data`, '$.tags[0]')",
		},
		{
			name:     "nested array",
			column:   "json_column",
			path:     "users[0].name",
			expected: "JSON_EXTRACT(`json_column`, '$.users[0].name')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildJSONExtractSQL(ctx, tt.column, tt.path)
			if err != nil {
				t.Errorf("BuildJSONExtractSQL error: %v", err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildJSONExtractSQL() = %q, want %q", result, tt.expected)
			}

			// Test caching
			ctx.Reset()
			_ = mysql.BuildJSONExtractSQL(ctx, tt.column, tt.path)
			if ctx.builder.String() != tt.expected {
				t.Errorf("Cached BuildJSONExtractSQL() = %q, want %q", ctx.builder.String(), tt.expected)
			}
		})
	}
}

func TestMySQLBuildJSONContainsSQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		column   string
		path     string
		value    any
		expected string
	}{
		{
			name:     "string value",
			column:   "metadata",
			path:     "tags",
			value:    "golang",
			expected: "JSON_CONTAINS(`metadata`, ?, '$.tags')",
		},
		{
			name:     "numeric value",
			column:   "data",
			path:     "ids",
			value:    123,
			expected: "JSON_CONTAINS(`data`, ?, '$.ids')",
		},
		{
			name:     "root path",
			column:   "json_col",
			path:     "",
			value:    `{"key": "value"}`,
			expected: "JSON_CONTAINS(`json_col`, ?, '$')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildJSONContainsSQL(ctx, tt.column, tt.path, tt.value)
			if err != nil {
				t.Errorf("BuildJSONContainsSQL error: %v", err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildJSONContainsSQL() = %q, want %q", result, tt.expected)
			}

			// Verify arg was added
			if len(ctx.args) != 1 || ctx.args[0] != tt.value {
				t.Errorf("Expected arg %v, got %v", tt.value, ctx.args)
			}

			// Test caching
			ctx.Reset()
			_ = mysql.BuildJSONContainsSQL(ctx, tt.column, tt.path, tt.value)
			if ctx.builder.String() != tt.expected {
				t.Errorf("Cached BuildJSONContainsSQL() = %q, want %q", ctx.builder.String(), tt.expected)
			}
		})
	}
}

func TestMySQLBuildFullTextSearchSQL(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	tests := []struct {
		name     string
		columns  []string
		query    string
		mode     string
		expected string
	}{
		{
			name:     "single column",
			columns:  []string{"content"},
			query:    "search term",
			mode:     "IN BOOLEAN MODE",
			expected: "MATCH(`content`) AGAINST(? IN BOOLEAN MODE)",
		},
		{
			name:     "multiple columns",
			columns:  []string{"title", "body"},
			query:    "important",
			mode:     "IN NATURAL LANGUAGE MODE",
			expected: "MATCH(`title`, `body`) AGAINST(? IN NATURAL LANGUAGE MODE)",
		},
		{
			name:     "with expansion",
			columns:  []string{"description"},
			query:    "query",
			mode:     "WITH QUERY EXPANSION",
			expected: "MATCH(`description`) AGAINST(? WITH QUERY EXPANSION)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx.Reset()
			err := mysql.BuildFullTextSearchSQL(ctx, tt.columns, tt.query, tt.mode)
			if err != nil {
				t.Errorf("BuildFullTextSearchSQL error: %v", err)
			}

			result := ctx.builder.String()
			if result != tt.expected {
				t.Errorf("BuildFullTextSearchSQL() = %q, want %q", result, tt.expected)
			}

			// Verify arg was added
			if len(ctx.args) != 1 || ctx.args[0] != tt.query {
				t.Errorf("Expected arg %v, got %v", tt.query, ctx.args)
			}
		})
	}
}

func TestMySQLConcurrentAccess(t *testing.T) {
	mysql := NewMySQLClause()

	// Test concurrent access to caches
	var wg sync.WaitGroup
	iterations := 100

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			ctx := NewContext()
			table := "users"

			// Test various methods concurrently
			_ = mysql.BuildFromSQL(ctx, table)
			_ = mysql.BuildUpdateSQL(ctx, table)
			_ = mysql.BuildDeleteSQL(ctx, table)
			_ = mysql.QuoteIdentifier(table)

			// Test with different tables
			otherTable := "table_" + string(rune('A'+idx%26))
			_ = mysql.BuildFromSQL(ctx, otherTable)
		}(i)
	}

	wg.Wait()
	// If we get here without panics, concurrent access is safe
}

func TestMySQLClause_QuoteIdentifier(t *testing.T) {
	cla := NewMySQLClause()
	var testIdentifiers = []string{
		"id",
		"user_id",
		"user.name",
		"table.column",
		"database.table.column",
		"`already_quoted`",
		"name.with`backtick",
		"very.long.identifier.name.with.multiple.parts.and.special`characters",
	}

	for _, identifier := range testIdentifiers {
		t.Log(cla.QuoteIdentifier(identifier))
	}
}

// cpu: QEMU Virtual CPU version 2.5+		2   	 61846736	        19.38 ns/op	       0 B/op	       0 allocs/op
func BenchmarkMySQLClause_QuoteIdentifier(b *testing.B) {
	cla := NewMySQLClause()
	column := "user_id"

	b.ReportAllocs()

	for b.Loop() {
		cla.QuoteIdentifier(column)
	}
}

func TestMySQLClause_BuildPlaceholders(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildPlaceholders(&ctx.builder, 0, 10); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())

	ctx.Reset()
	if err := cla.BuildPlaceholders(&ctx.builder, 0, 30); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())

	ctx.Reset()
	if err := cla.BuildPlaceholders(&ctx.builder, 0, 31); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
	ctx.Reset()
}

// cpu: QEMU Virtual CPU version 2.5+		2		10   	 8922171	       130.8 ns/op	     150 B/op	       0 allocs/op
// cpu: QEMU Virtual CPU version 2.5+		2		30   	 8632498	       482.9 ns/op	     474 B/op	       0 allocs/op
// cpu: QEMU Virtual CPU version 2.5+		2		31   	 7859523	       203.5 ns/op	     521 B/op	       0 allocs/op
func BenchmarkMySQLClause_BuildPlaceholders(b *testing.B) {
	cla := NewMySQLClause()
	ctx := NewContext()

	b.Run("10", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			if err := cla.BuildPlaceholders(&ctx.builder, 0, 10); err != nil {
				b.Error(err)
			}
		}
	})

	ctx.Reset()
	b.Run("30", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			if err := cla.BuildPlaceholders(&ctx.builder, 0, 30); err != nil {
				b.Error(err)
			}
		}
	})

	ctx.Reset()
	b.Run("31", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			if err := cla.BuildPlaceholders(&ctx.builder, 0, 31); err != nil {
				b.Error(err)
			}
		}
	})
}

func TestMySQLClause_BuildInsertSQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildInsertSQL(ctx, "users", false, "id", "name", "age", "sex", "email", "addr"); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())

	ctx.Reset()
	if err := cla.BuildInsertSQL(ctx, "users", true, "id", "name", "age", "sex", "email", "addr"); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())

	ctx.Reset()
	if err := cla.BuildInsertSQL(ctx, "users", true); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())

	fmt.Println()
	ctx.Reset()
	if err := cla.BuildInsertSQL(ctx, "users", false, "id", "name", "age", "sex", "email", "addr"); err != nil {
		t.Error(err)
	}
	if err := cla.BuildValuesSQL(ctx, 0, []any{"uid123", "wx", 18, "boy", "xxx@xxx.com", "wx"}); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

// cpu: QEMU Virtual CPU version 2.5+		2   	10058260	       118.6 ns/op	      96 B/op	       2 allocs/op
func BenchmarkMySQLClause_BuildInsertSQL(b *testing.B) {
	cla := NewMySQLClause()
	ctx := NewContext()

	b.ReportAllocs()

	for b.Loop() {
		if err := cla.BuildInsertSQL(ctx, "users", false, "id", "name", "age", "sex", "email", "addr"); err != nil {
			b.Error(err)
		}
		ctx.Reset()
	}
}

func TestMySQLClause_BuildReplaceSQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildReplaceSQL(ctx, "users", "id", "name", "age", "sex", "email", "addr"); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

// cpu: QEMU Virtual CPU version 2.5+		2   	10614832	       111.4 ns/op	      88 B/op	       2 allocs/op
func BenchmarkMySQLClause_BuildReplaceSQL(b *testing.B) {
	cla := NewMySQLClause()
	ctx := NewContext()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if err := cla.BuildReplaceSQL(ctx, "users", "id", "name", "age", "sex", "email", "addr"); err != nil {
			b.Error(err)
		}
		ctx.Reset()
	}
}

func TestMySQLClause_BuildValuesSQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildValuesSQL(ctx, 0,
		[]any{"id", "name", "age", "sex", "email", "addr"},
		[]any{"id", "name", "age", "sex", "email", "addr"},
		[]any{"id", "name", "age", "sex", "email", "addr"},
	); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

// cpu: QEMU Virtual CPU version 2.5+		2		1   	18184258	        63.16 ns/op	      32 B/op	       1 allocs/op
// cpu: QEMU Virtual CPU version 2.5+		2		5   	 6992611	       170.5 ns/op	     128 B/op	       1 allocs/op
// cpu: QEMU Virtual CPU version 2.5+		2		10   	 4154055	       288.6 ns/op	     256 B/op	       1 allocs/op
func BenchmarkMySQLClause_BuildValuesSQL(b *testing.B) {
	cla := NewMySQLClause()
	ctx := NewContext()

	b.Run("VALUES 1", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			if err := cla.BuildValuesSQL(ctx, 0,
				[]any{"id", "name", "age", "sex", "email", "addr"},
			); err != nil {
				b.Error(err)
			}
			ctx.Reset()
		}
	})

	b.Run("VALUES 5", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			if err := cla.BuildValuesSQL(ctx, 0,
				[]any{"id", "name", "age", "sex", "email", "addr"},
				[]any{"id", "name", "age", "sex", "email", "addr"},
				[]any{"id", "name", "age", "sex", "email", "addr"},
				[]any{"id", "name", "age", "sex", "email", "addr"},
				[]any{"id", "name", "age", "sex", "email", "addr"},
			); err != nil {
				b.Error(err)
			}
			ctx.Reset()
		}
	})

	b.Run("VALUES 10", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			if err := cla.BuildValuesSQL(ctx, 0,
				[]any{"id", "name", "age", "sex", "email", "addr"},
				[]any{"id", "name", "age", "sex", "email", "addr"},
				[]any{"id", "name", "age", "sex", "email", "addr"},
				[]any{"id", "name", "age", "sex", "email", "addr"},
				[]any{"id", "name", "age", "sex", "email", "addr"},
				[]any{"id", "name", "age", "sex", "email", "addr"},
				[]any{"id", "name", "age", "sex", "email", "addr"},
				[]any{"id", "name", "age", "sex", "email", "addr"},
				[]any{"id", "name", "age", "sex", "email", "addr"},
				[]any{"id", "name", "age", "sex", "email", "addr"},
			); err != nil {
				b.Error(err)
			}
			ctx.Reset()
		}
	})
}

func TestMySQLClause_BuildDuplicateSQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildDuplicateSQL(ctx, "name", "age", "sex", "email", "addr"); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

// cpu: QEMU Virtual CPU version 2.5+		2		1   	 5218136	       241.2 ns/op	     232 B/op	       4 allocs/op
// cpu: QEMU Virtual CPU version 2.5+		2		5   	 5218136	       241.2 ns/op	     232 B/op	       4 allocs/op
func BenchmarkMySQLClause_BuildDuplicateSQL(b *testing.B) {
	cla := NewMySQLClause()
	ctx := NewContext()

	b.Run("DUPLICATE 1", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			if err := cla.BuildDuplicateSQL(ctx, "name", "age", "sex", "email", "addr"); err != nil {
				b.Error(err)
			}
			ctx.Reset()
		}
	})

	b.Run("DUPLICATE 5", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			if err := cla.BuildDuplicateSQL(ctx, "name", "age", "sex", "email", "addr"); err != nil {
				b.Error(err)
			}
			ctx.Reset()
		}
	})
}

func TestMySQLClause_BuildUpdateSQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildUpdateSQL(ctx, "users"); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

// cpu: QEMU Virtual CPU version 2.5+		2   	 22182805	        51.28 ns/op	      24 B/op	       1 allocs/op
func BenchmarkMySQLClause_BuildUpdateSQL(b *testing.B) {
	cla := NewMySQLClause()
	ctx := NewContext()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if err := cla.BuildUpdateSQL(ctx, "users"); err != nil {
			b.Error(err)
		}
		ctx.Reset()
	}
}

func TestMySQLClause_BuildSetSQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildSetSQL(ctx, 0, map[string]any{
		"id":    "00000",
		"name":  "wx",
		"age":   18,
		"sex":   1,
		"email": "xxx@xxx.xxx",
		"addr":  "cq",
	}); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

// cpu: QEMU Virtual CPU version 2.5+		2   	 1   	 7851944	       149.6 ns/op	      48 B/op	       2 allocs/op
// cpu: QEMU Virtual CPU version 2.5+		2   	 5   	 3401403	       350.7 ns/op	     112 B/op	       3 allocs/op
// cpu: QEMU Virtual CPU version 2.5+		2   	 10   	 1670168	       862.8 ns/op	     496 B/op	       5 allocs/op
func BenchmarkMySQLClause_BuildSetSQL(b *testing.B) {
	cla := NewMySQLClause()
	ctx := NewContext()

	b.Run("SET 1", func(b *testing.B) {
		data := map[string]any{
			"email": "xxx@xxx.xxx",
		}

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			if err := cla.BuildSetSQL(ctx, 0, data); err != nil {
				b.Error(err)
			}
			ctx.Reset()
		}
	})

	b.Run("SET 5", func(b *testing.B) {
		data := map[string]any{
			"id":    "00000",
			"name":  "wx",
			"age":   18,
			"sex":   1,
			"email": "xxx@xxx.xxx",
		}

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			if err := cla.BuildSetSQL(ctx, 0, data); err != nil {
				b.Error(err)
			}
			ctx.Reset()
		}
	})

	b.Run("SET 10", func(b *testing.B) {
		data := map[string]any{
			"id":         "00000",
			"name":       "wx",
			"age":        18,
			"sex":        1,
			"email":      "xxx@xxx.xxx",
			"addr":       "cq",
			"group":      "admin",
			"is_role":    true,
			"updated_at": time.Now(),
			"updated_by": "wx",
		}

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			if err := cla.BuildSetSQL(ctx, 0, data); err != nil {
				b.Error(err)
			}
			ctx.Reset()
		}
	})
}

func TestMySQLClause_BuildDeleteSQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildDeleteSQL(ctx, "users"); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

// cpu: QEMU Virtual CPU version 2.5+		2   	 22035072	        52.19 ns/op	      24 B/op	       1 allocs/op
func BenchmarkMySQLClause_BuildDeleteSQL(b *testing.B) {
	cla := NewMySQLClause()
	ctx := NewContext()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if err := cla.BuildDeleteSQL(ctx, "users"); err != nil {
			b.Error(err)
		}
		ctx.Reset()
	}
}

func TestMySQLClause_BuildSelectSQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildSelectSQL(ctx, "id", "name", "age", "sex", "email", "addr"); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

// cpu: QEMU Virtual CPU version 2.5+		2   	 1   	 20368015	        55.99 ns/op	      16 B/op	       1 allocs/op
// cpu: QEMU Virtual CPU version 2.5+		2   	 1   	 19937287	        56.80 ns/op	      16 B/op	       1 allocs/op
// cpu: QEMU Virtual CPU version 2.5+		2   	 1   	 18268414	        62.85 ns/op	      16 B/op	       1 allocs/op
func BenchmarkMySQLClause_BuildSelectSQL(b *testing.B) {
	cla := NewMySQLClause()
	ctx := NewContext()

	b.Run("SELECT 1", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			if err := cla.BuildSelectSQL(ctx, "id"); err != nil {
				b.Error(err)
			}
			ctx.Reset()
		}
	})

	b.Run("SELECT 5", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			if err := cla.BuildSelectSQL(ctx, "id", "name", "age", "sex", "email"); err != nil {
				b.Error(err)
			}
			ctx.Reset()
		}
	})

	b.Run("SELECT 10", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			if err := cla.BuildSelectSQL(ctx, "id", "name", "age", "sex", "email", "addr", "group", "is_role", "updated_at", "updated_by"); err != nil {
				b.Error(err)
			}
			ctx.Reset()
		}
	})
}

func TestMySQLClause_BuildCountSQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildCountSQL(ctx); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())

	ctx.Reset()
	if err := cla.BuildCountSQL(ctx, "id"); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())

	ctx.Reset()
	if err := cla.BuildCountSQL(ctx, "DISTINCT id"); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

// cpu: QEMU Virtual CPU version 2.5+		2   	 31314392	        36.89 ns/op	      24 B/op	       1 allocs/op
func BenchmarkMySQLClause_BuildCountSQL(b *testing.B) {
	cla := NewMySQLClause()
	ctx := NewContext()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if err := cla.BuildCountSQL(ctx); err != nil {
			b.Error(err)
		}
		ctx.Reset()
	}
}

func TestMySQLClause_BuildWhereSQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildWhereSQL(ctx); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

// cpu: QEMU Virtual CPU version 2.5+		2   	 44189452	        26.73 ns/op	      16 B/op	       1 allocs/op
func BenchmarkMySQLClause_BuildWhereSQL(b *testing.B) {
	cla := NewMySQLClause()
	ctx := NewContext()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if err := cla.BuildWhereSQL(ctx); err != nil {
			b.Error(err)
		}
		ctx.Reset()
	}
}

func TestMySQLClause_BuildGroupBySQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildGroupBySQL(ctx, "id", "name", "age", "sex", "email", "addr"); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

// cpu: QEMU Virtual CPU version 2.5+		2   	 4515496	       261.1 ns/op	     112 B/op	       3 allocs/op
func BenchmarkMySQLClause_BuildGroupBySQL(b *testing.B) {
	cla := NewMySQLClause()
	ctx := NewContext()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if err := cla.BuildGroupBySQL(ctx, "id", "name", "age", "sex", "email", "addr"); err != nil {
			b.Error(err)
		}
		ctx.Reset()
	}
}

func TestMySQLClause_BuildHavingSQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildHavingSQL(ctx); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

func TestMySQLClause_BuildOrderBySQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildOrderBySQL(ctx, "id", "name", "age", "sex", "email", "addr"); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

func TestMySQLClause_BuildLimitSQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildLimitSQL(ctx, 100); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

func TestMySQLClause_BuildOffsetSQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildOffsetSQL(ctx, 100); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

func TestMySQLClause_BuildJSONExtractSQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildJSONExtractSQL(ctx, "users", "id"); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

// cpu: QEMU Virtual CPU version 2.5+		2   	 12330522	        94.07 ns/op	      40 B/op	       2 allocs/op
func BenchmarkMySQLClause_BuildJSONExtractSQL(b *testing.B) {
	cla := NewMySQLClause()
	ctx := NewContext()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if err := cla.BuildJSONExtractSQL(ctx, "users", "id"); err != nil {
			b.Error(err)
		}
		ctx.Reset()
	}
}

func TestMySQLClause_BuildJSONContainsSQL(t *testing.T) {
	cla := NewMySQLClause()
	ctx := NewContext()

	if err := cla.BuildJSONContainsSQL(ctx, "users", "id", "uid1234657890"); err != nil {
		t.Error(err)
	}
	fmt.Println(ctx.GetResult())
}

//go:noinline
func O1(args ...string) {
	O2(args...)
}

func O2(args ...string) {
	for range args {
	}
}

/*
BenchmarkO1/1_args
BenchmarkO1/1_args-2         	827553584	         1.452 ns/op	       0 B/op	       0 allocs/op
BenchmarkO1/5_args
BenchmarkO1/5_args-2         	202031132	         5.968 ns/op	       0 B/op	       0 allocs/op
BenchmarkO1/10_args
BenchmarkO1/10_args-2        	213833544	         5.604 ns/op	       0 B/op	       0 allocs/op
*/
func BenchmarkO1(b *testing.B) {
	b.Run("1 args", func(b *testing.B) {
		args := []string{"id"}

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			O1(args...)
		}
	})

	b.Run("5 args", func(b *testing.B) {
		args := []string{"id", "name", "age", "sex", "phone"}

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			O1(args...)
		}
	})

	b.Run("10 args", func(b *testing.B) {
		args := []string{"id", "name", "age", "sex", "phone", "role", "group", "email", "address", "status"}

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			O1(args...)
		}
	})
}

//go:noinline
func O11(args []string) {
	O22(args)
}

func O22(args []string) {
	for range args {
	}
}

/*
BenchmarkO11/1_args
BenchmarkO11/1_args-2         	825562615	         1.455 ns/op	       0 B/op	       0 allocs/op
BenchmarkO11/5_args
BenchmarkO11/5_args-2         	201331604	         5.907 ns/op	       0 B/op	       0 allocs/op
BenchmarkO11/10_args
BenchmarkO11/10_args-2        	213401635	         5.597 ns/op	       0 B/op	       0 allocs/op
*/
func BenchmarkO11(b *testing.B) {
	b.Run("1 args", func(b *testing.B) {
		args := []string{"id"}

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			O11(args)
		}
	})

	b.Run("5 args", func(b *testing.B) {
		args := []string{"id", "name", "age", "sex", "phone"}

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			O11(args)
		}
	})

	b.Run("10 args", func(b *testing.B) {
		args := []string{"id", "name", "age", "sex", "phone", "role", "group", "email", "address", "status"}

		b.ReportAllocs()
		b.ResetTimer()

		for b.Loop() {
			O11(args)
		}
	})
}
