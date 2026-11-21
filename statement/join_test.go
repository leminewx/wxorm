package statement

import (
	"errors"
	"reflect"
	"testing"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/statement/where"
)

// TestSELECTCOLUMNS 校验联表投影列的构造与别名
func TestSELECTCOLUMNS(t *testing.T) {
	tests := []struct {
		name    string
		columns []SelectColumn
		cla     clause.Clause
		want    string
		wantErr error
	}{
		{
			name: "mysql 带别名",
			columns: []SelectColumn{
				{Table: "articles", Name: "id"},
				{Table: "author", Name: "name", Alias: "author_name"},
			},
			cla:  clause.NewMySQLClause(),
			want: "SELECT\n    `articles`.`id`, `author`.`name` AS `author_name`",
		},
		{
			name: "postgres 不限定表",
			columns: []SelectColumn{
				{Name: "id"},
				{Name: "name", Alias: "author_name"},
			},
			cla:  clause.NewPostgresClause(),
			want: "SELECT\n    \"id\", \"name\" AS \"author_name\"",
		},
		{
			name:    "空列集",
			columns: nil,
			cla:     clause.NewMySQLClause(),
			wantErr: ErrColumnsIsEmpty,
		},
		{
			name:    "列名为空",
			columns: []SelectColumn{{Table: "articles"}},
			cla:     clause.NewMySQLClause(),
			wantErr: ErrColumnsIsEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, build := SELECT_COLUMNS(tt.columns...)
			ctx := clause.NewContext()
			err := build(ctx, tt.cla)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}

			if tt.wantErr != nil {
				return
			}

			if sql, _ := ctx.GetResult(); sql != tt.want {
				t.Errorf("SQL = %q, want %q", sql, tt.want)
			}
		})
	}
}

// TestJOINClauseOrder 校验 JOIN 在 FROM 之后、WHERE 之前发射
func TestJOINClauseOrder(t *testing.T) {
	stmt := NewStatement("articles")
	stmt.AddClause(SELECT_COLUMNS(
		SelectColumn{Table: "articles", Name: "id"},
		SelectColumn{Table: "author", Name: "name", Alias: "author_name"},
	))
	stmt.AddClause(FROM("articles"))
	stmt.AddClause(JOIN(Join{
		Kind:  clause.JOIN_LEFT,
		Table: "authors",
		Alias: "author",
		On:    where.EQ_COLUMN("articles.author_id", "author.id"),
	}))
	stmt.AddClause(WHERE(where.LIKE("author.name", "Ada%")))

	query, args, err := stmt.BuildSQL(clause.NewMySQLClause(),
		NAME_SELECT, NAME_FROM, NAME_JOIN, NAME_WHERE)
	if err != nil {
		t.Fatalf("BuildSQL error: %v", err)
	}

	want := "SELECT\n    `articles`.`id`, `author`.`name` AS `author_name`" +
		"\nFROM\n    `articles`" +
		"\nLEFT JOIN\n    `authors` AS `author`" +
		"\nON\n    `articles`.`author_id` = `author`.`id`" +
		"\nWHERE\n    `author`.`name` LIKE ?" +
		";"
	if query != want {
		t.Errorf("SQL = %q, want %q", query, want)
	}

	if !reflect.DeepEqual(args, []any{"Ada%"}) {
		t.Errorf("args = %v, want [Ada%%]", args)
	}
}

// TestJOINParameterOrder 校验 ON 的参数排在 WHERE 之前，且占位符编号连续
func TestJOINParameterOrder(t *testing.T) {
	stmt := NewStatement("articles")
	stmt.AddClause(SELECT_COLUMNS(SelectColumn{Table: "articles", Name: "id"}))
	stmt.AddClause(FROM("articles"))
	stmt.AddClause(JOIN(Join{
		Kind:  clause.JOIN_INNER,
		Table: "authors",
		Alias: "author",
		On: where.AND(
			where.EQ_COLUMN("articles.author_id", "author.id"),
			where.EQ("author.status", 1),
		),
	}))
	stmt.AddClause(WHERE(where.LIKE("author.name", "Ada%")))

	query, args, err := stmt.BuildSQL(clause.NewPostgresClause(),
		NAME_SELECT, NAME_FROM, NAME_JOIN, NAME_WHERE)
	if err != nil {
		t.Fatalf("BuildSQL error: %v", err)
	}

	want := "SELECT\n    \"articles\".\"id\"" +
		"\nFROM\n    \"articles\"" +
		"\nINNER JOIN\n    \"authors\" AS \"author\"" +
		"\nON\n    (\"articles\".\"author_id\" = \"author\".\"id\" AND \"author\".\"status\" = $1)" +
		"\nWHERE\n    \"author\".\"name\" ILIKE $2" +
		";"
	if query != want {
		t.Errorf("SQL = %q, want %q", query, want)
	}

	if !reflect.DeepEqual(args, []any{1, "Ada%"}) {
		t.Errorf("args = %v, want [1 Ada%%]", args)
	}
}

// TestJOINConditionValidation 校验 ON 条件与 JOIN 类型的组合约束
func TestJOINConditionValidation(t *testing.T) {
	cla := clause.NewMySQLClause()

	tests := []struct {
		name    string
		joins   []Join
		wantErr error
	}{
		{
			name:    "非 CROSS 缺少 ON",
			joins:   []Join{{Kind: clause.JOIN_LEFT, Table: "authors", Alias: "author"}},
			wantErr: ErrJoinConditionRequired,
		},
		{
			name: "CROSS 携带 ON",
			joins: []Join{{
				Kind:  clause.JOIN_CROSS,
				Table: "authors",
				Alias: "author",
				On:    where.EQ_COLUMN("a.id", "b.id"),
			}},
			wantErr: ErrJoinConditionNotAllowed,
		},
		{
			name: "MySQL 不支持 FULL",
			joins: []Join{{
				Kind:  clause.JOIN_FULL,
				Table: "authors",
				Alias: "author",
				On:    where.EQ_COLUMN("a.id", "b.id"),
			}},
			wantErr: clause.ErrUnsupportedFeature,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, build := JOIN(tt.joins...)
			err := build(clause.NewContext(), cla)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}

	// 空 join 列表应是空操作
	_, build := JOIN()
	if err := build(clause.NewContext(), cla); err != nil {
		t.Errorf("空 JOIN 应无错误，实际: %v", err)
	}
}

// TestCOUNTDISTINCT 校验联表统计的去重 SQL
func TestCOUNTDISTINCT(t *testing.T) {
	_, build := COUNT_DISTINCT("articles.id")
	ctx := clause.NewContext()
	if err := build(ctx, clause.NewMySQLClause()); err != nil {
		t.Fatalf("COUNT_DISTINCT error: %v", err)
	}

	want := "SELECT\n    COUNT(DISTINCT `articles`.`id`)"
	if sql, _ := ctx.GetResult(); sql != want {
		t.Errorf("SQL = %q, want %q", sql, want)
	}

	_, build = COUNT_DISTINCT("")
	if err := build(clause.NewContext(), clause.NewMySQLClause()); !errors.Is(err, ErrColumnsIsEmpty) {
		t.Errorf("空列应返回 ErrColumnsIsEmpty，实际: %v", err)
	}
}
