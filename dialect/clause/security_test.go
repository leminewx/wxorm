package clause

import (
	"errors"
	"testing"
)

// 测试 JSON 路径中的单引号被转义，避免闭合 SQL 字符串字面量
func TestMySQLBuildJSONExtractSQL_EscapePath(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	if err := mysql.BuildJSONExtractSQL(ctx, "metadata", "a') OR 1=1 --"); err != nil {
		t.Fatalf("build json extract failed: %v", err)
	}

	sql, _ := ctx.GetResult()
	want := "JSON_EXTRACT(`metadata`, '$.a'') OR 1=1 --')"
	if sql != want {
		t.Errorf("unexpected sql:\n got: %s\nwant: %s", sql, want)
	}
}

// 测试 JSON_CONTAINS 的路径同样被转义
func TestMySQLBuildJSONContainsSQL_EscapePath(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	if err := mysql.BuildJSONContainsSQL(ctx, "metadata", "tags'x", "golang"); err != nil {
		t.Fatalf("build json contains failed: %v", err)
	}

	sql, _ := ctx.GetResult()
	want := "JSON_CONTAINS(`metadata`, ?, '$.tags''x')"
	if sql != want {
		t.Errorf("unexpected sql:\n got: %s\nwant: %s", sql, want)
	}
}

// 测试非法的全文检索模式被拒绝，而不是原样拼进 SQL
func TestMySQLBuildFullTextSearchSQL_InvalidMode(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	if err := mysql.BuildFullTextSearchSQL(ctx, []string{"content"}, "term", "IN BOOLEAN MODE) OR 1=1 --"); !errors.Is(err, ErrInvalidFullTextMode) {
		t.Errorf("expected ErrInvalidFullTextMode, got %v", err)
	}
}

// 测试合法模式忽略大小写与多余空白
func TestMySQLBuildFullTextSearchSQL_NormalizeMode(t *testing.T) {
	mysql := NewMySQLClause()
	ctx := NewContext()

	if err := mysql.BuildFullTextSearchSQL(ctx, []string{"content"}, "term", " in   boolean mode "); err != nil {
		t.Fatalf("build full text search failed: %v", err)
	}

	sql, args := ctx.GetResult()
	want := "MATCH(`content`) AGAINST(? IN BOOLEAN MODE)"
	if sql != want {
		t.Errorf("unexpected sql:\n got: %s\nwant: %s", sql, want)
	}

	if len(args) != 1 || args[0] != "term" {
		t.Errorf("unexpected args: %v", args)
	}
}
