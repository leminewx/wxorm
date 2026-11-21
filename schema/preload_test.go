package schema

import (
	"errors"
	"reflect"
	"testing"

	"gitee.com/leminewx/wxorm/dialect"
)

type preloadAuthor struct {
	ID   int64  `wxorm:"column(id) pk auto"`
	Name string `wxorm:"column(name) notnull"`
}

func (*preloadAuthor) GetTableName() string { return "preload_authors" }

type preloadComment struct {
	ID        int64  `wxorm:"column(id) pk auto"`
	ArticleID int64  `wxorm:"column(article_id) index"`
	Content   string `wxorm:"column(content)"`
}

func (*preloadComment) GetTableName() string { return "preload_comments" }

type preloadArticle struct {
	ID       int64            `wxorm:"column(id) pk auto"`
	Title    string           `wxorm:"column(title) notnull"`
	AuthorID int64            `wxorm:"column(author_id)"`
	Author   *preloadAuthor   `wxorm:"preload(author_id)"`
	Comments []preloadComment `wxorm:"preload(article_id)"`
}

func (*preloadArticle) GetTableName() string { return "preload_articles" }

// 预加载字段必须被解析成关联声明，且不污染建表与读写列集
func TestParsePreloadRelations(t *testing.T) {
	sch, err := Parse(dialect.Get("mysql"), &preloadArticle{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	if len(sch.Relations) != 2 {
		t.Fatalf("relations = %d, want 2", len(sch.Relations))
	}

	author, ok := sch.GetRelationByName("Author")
	if !ok {
		t.Fatal("relation Author not found")
	}

	if author.Kind != RELATION_BELONGS_TO {
		t.Fatalf("Author kind = %s, want belongs_to", author.Kind)
	}
	if author.Column != "author_id" {
		t.Fatalf("Author column = %q, want author_id", author.Column)
	}
	if author.TargetType != reflect.TypeOf(preloadAuthor{}) {
		t.Fatalf("Author target = %v, want preloadAuthor", author.TargetType)
	}
	if author.TargetTable != "preload_authors" {
		t.Fatalf("Author target table = %q, want preload_authors", author.TargetTable)
	}
	if got := sch.ModelType.Field(author.FieldIndex).Name; got != "Author" {
		t.Fatalf("Author field index points to %q", got)
	}

	comments, ok := sch.GetRelationByName("Comments")
	if !ok {
		t.Fatal("relation Comments not found")
	}

	if comments.Kind != RELATION_HAS_MANY {
		t.Fatalf("Comments kind = %s, want has_many", comments.Kind)
	}
	if comments.Column != "article_id" {
		t.Fatalf("Comments column = %q, want article_id", comments.Column)
	}
	if comments.TargetTable != "preload_comments" {
		t.Fatalf("Comments target table = %q, want preload_comments", comments.TargetTable)
	}

	// 列集隔离：预加载字段不进入 SELECT、INSERT、UPDATE 与主键
	wantColumns := []string{"id", "title", "author_id"}
	if !reflect.DeepEqual(sch.ColumnNames, wantColumns) {
		t.Fatalf("columns = %v, want %v", sch.ColumnNames, wantColumns)
	}
	if !reflect.DeepEqual(sch.InsertableColumnNames, []string{"title", "author_id"}) {
		t.Fatalf("insertable = %v, want [title author_id]", sch.InsertableColumnNames)
	}
	if !reflect.DeepEqual(sch.PrimaryKeyNames, []string{"id"}) {
		t.Fatalf("primary keys = %v, want [id]", sch.PrimaryKeyNames)
	}
	if len(sch.UpdateFields) != 2 {
		t.Fatalf("update fields = %v, want 2", sch.UpdateFields)
	}

	all, err := sch.SelectRelations()
	if err != nil || len(all) != 2 {
		t.Fatalf("SelectRelations() = %v, %v", all, err)
	}

	only, err := sch.SelectRelations("Comments")
	if err != nil || len(only) != 1 || only[0].FieldName != "Comments" {
		t.Fatalf("SelectRelations(Comments) = %v, %v", only, err)
	}

	if _, err := sch.SelectRelations("Missing"); !errors.Is(err, ErrPreloadUnknownField) {
		t.Fatalf("err = %v, want ErrPreloadUnknownField", err)
	}
}

// 未实现 GetTableName 的目标模型按蛇形名推断表名
func TestParsePreloadTableNameFallback(t *testing.T) {
	type plainPreloadTarget struct {
		ID int64 `wxorm:"column(id) pk auto"`
	}

	type usesPlainTarget struct {
		ID     int64               `wxorm:"column(id) pk auto"`
		Target *plainPreloadTarget `wxorm:"preload(target_id)"`
	}

	sch, err := Parse(dialect.Get("mysql"), &usesPlainTarget{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	relation, ok := sch.GetRelationByName("Target")
	if !ok {
		t.Fatal("relation Target not found")
	}

	if relation.TargetTable != "plain_preload_target" {
		t.Fatalf("target table = %q, want plain_preload_target", relation.TargetTable)
	}
}

// 标签语法与字段类型的错误必须在解析期暴露
func TestParsePreloadErrors(t *testing.T) {
	cases := []struct {
		name  string
		model any
		want  error
	}{
		{
			name: "缺少关联列",
			model: func() any {
				type model struct {
					Author *preloadAuthor `wxorm:"preload()"`
				}
				return &model{}
			}(),
			want: ErrInvalidPreloadTag,
		},
		{
			name: "关联列含点号",
			model: func() any {
				type model struct {
					Author *preloadAuthor `wxorm:"preload(authors.id)"`
				}
				return &model{}
			}(),
			want: ErrInvalidPreloadTag,
		},
		{
			name: "关联列含空白",
			model: func() any {
				type model struct {
					Author *preloadAuthor `wxorm:"preload(author id)"`
				}
				return &model{}
			}(),
			want: ErrInvalidPreloadTag,
		},
		{
			name: "缺少右括号",
			model: func() any {
				type model struct {
					Author *preloadAuthor `wxorm:"preload(author_id"`
				}
				return &model{}
			}(),
			want: ErrInvalidPreloadTag,
		},
		{
			name: "标量字段",
			model: func() any {
				type model struct {
					AuthorID int64 `wxorm:"preload(author_id)"`
				}
				return &model{}
			}(),
			want: ErrPreloadFieldType,
		},
		{
			name: "标量切片",
			model: func() any {
				type model struct {
					IDs []int64 `wxorm:"preload(article_id)"`
				}
				return &model{}
			}(),
			want: ErrPreloadFieldType,
		},
		{
			name: "指向标量的指针",
			model: func() any {
				type model struct {
					AuthorID *int64 `wxorm:"preload(author_id)"`
				}
				return &model{}
			}(),
			want: ErrPreloadFieldType,
		},
		{
			name: "重复的关联声明",
			model: func() any {
				type model struct {
					ID         int64          `wxorm:"column(id) pk auto"`
					Author     *preloadAuthor `wxorm:"preload(author_id)"`
					AuthorCopy *preloadAuthor `wxorm:"preload(author_id)"`
				}
				return &model{}
			}(),
			want: ErrPreloadRelationConflict,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(dialect.Get("mysql"), tc.model); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
