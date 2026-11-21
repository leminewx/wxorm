package schema

import (
	"errors"
	"slices"
	"testing"

	"gitee.com/leminewx/wxorm/dialect"
)

// joinedModel 是联表字段的最小模型
type joinedModel struct {
	ID         int64   `wxorm:"column(id) pk auto"`
	Title      string  `wxorm:"column(title) notnull"`
	AuthorID   int64   `wxorm:"column(author_id)"`
	AuthorName *string `wxorm:"join(author.name)"`
}

func (*joinedModel) GetTableName() string { return "joined_models" }

// joinConflictModel 的主表列与联表结果列别名同名
type joinConflictModel struct {
	ID         int64   `wxorm:"column(id) pk auto"`
	AuthorName string  `wxorm:"column(author_name)"`
	Note       *string `wxorm:"join(author.name)"`
}

func (*joinConflictModel) GetTableName() string { return "join_conflict_models" }

// joinDuplicateModel 两次声明了同一个联表字段
type joinDuplicateModel struct {
	ID         int64   `wxorm:"column(id) pk auto"`
	AuthorName *string `wxorm:"join(author.name)"`
	AuthorMail *string `wxorm:"join(author.name)"`
}

func (*joinDuplicateModel) GetTableName() string { return "join_duplicate_models" }

// TestParseJoinColumn 验证联表字段只进入 JoinColumns，不参与建表与读写列集
func TestParseJoinColumn(t *testing.T) {
	sch, err := Parse(dialect.Get("mysql"), &joinedModel{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(sch.JoinColumns) != 1 {
		t.Fatalf("JoinColumns = %d, want 1", len(sch.JoinColumns))
	}

	joinColumn := sch.JoinColumns[0]
	if joinColumn.Alias != "author" || joinColumn.Name != "name" || joinColumn.As != "author_name" {
		t.Errorf("unexpected join column: %+v", joinColumn)
	}

	// 字段索引必须指向模型中的 AuthorName，扫描阶段据此回填
	if sch.ModelType.Field(joinColumn.Index).Name != "AuthorName" {
		t.Errorf("Index = %d, want AuthorName", joinColumn.Index)
	}

	// 联表字段不能出现在建表列、主键、INSERT 与 UPDATE 列集里
	if slices.Contains(sch.ColumnNames, "author_name") || slices.Contains(sch.ColumnNames, "name") {
		t.Errorf("ColumnNames should not contain join column: %v", sch.ColumnNames)
	}
	if slices.Contains(sch.InsertableColumnNames, "author_name") {
		t.Errorf("InsertableColumnNames should not contain join column: %v", sch.InsertableColumnNames)
	}
	if slices.Contains(sch.PrimaryKeyNames, "author_name") {
		t.Errorf("PrimaryKeyNames should not contain join column: %v", sch.PrimaryKeyNames)
	}
	for _, field := range sch.UpdateFields {
		if field.Name == "author_name" {
			t.Errorf("UpdateFields should not contain join column: %v", sch.UpdateFields)
		}
	}

	// 结果列别名索引用于扫描阶段按列名回填
	if idx, ok := sch.JoinColumnIndexesByName["author_name"]; !ok || idx != joinColumn.Index {
		t.Errorf("JoinColumnIndexesByName[author_name] = %d, %v", idx, ok)
	}

	if got, ok := sch.GetJoinColumnByName("author_name"); !ok || got.As != "author_name" {
		t.Errorf("GetJoinColumnByName(author_name) = %+v, %v", got, ok)
	}
	if _, ok := sch.GetJoinColumnByName("author.name"); ok {
		t.Error("GetJoinColumnByName should only match the result alias")
	}
}

// TestParseJoinColumnErrors 验证非法标签与冲突在解析期报错
func TestParseJoinColumnErrors(t *testing.T) {
	dial := dialect.Get("mysql")

	t.Run("missing column", func(t *testing.T) {
		type Model struct {
			ID   int64   `wxorm:"column(id) pk auto"`
			Name *string `wxorm:"join(author)"`
		}

		if _, err := Parse(dial, &Model{}); !errors.Is(err, ErrInvalidJoinTag) {
			t.Fatalf("expected ErrInvalidJoinTag, got %v", err)
		}
	})

	t.Run("missing alias", func(t *testing.T) {
		type Model struct {
			ID   int64   `wxorm:"column(id) pk auto"`
			Name *string `wxorm:"join(.name)"`
		}

		if _, err := Parse(dial, &Model{}); !errors.Is(err, ErrInvalidJoinTag) {
			t.Fatalf("expected ErrInvalidJoinTag, got %v", err)
		}
	})

	t.Run("whitespace in segment", func(t *testing.T) {
		type Model struct {
			ID   int64   `wxorm:"column(id) pk auto"`
			Name *string `wxorm:"join(author.na me)"`
		}

		if _, err := Parse(dial, &Model{}); !errors.Is(err, ErrInvalidJoinTag) {
			t.Fatalf("expected ErrInvalidJoinTag, got %v", err)
		}
	})

	t.Run("missing closing parenthesis", func(t *testing.T) {
		type Model struct {
			ID   int64   `wxorm:"column(id) pk auto"`
			Name *string `wxorm:"join(author.name"`
		}

		if _, err := Parse(dial, &Model{}); !errors.Is(err, ErrInvalidJoinTag) {
			t.Fatalf("expected ErrInvalidJoinTag, got %v", err)
		}
	})

	t.Run("conflict with table column", func(t *testing.T) {
		if _, err := Parse(dial, &joinConflictModel{}); !errors.Is(err, ErrJoinColumnConflict) {
			t.Fatalf("expected ErrJoinColumnConflict, got %v", err)
		}
	})

	t.Run("duplicate join column", func(t *testing.T) {
		if _, err := Parse(dial, &joinDuplicateModel{}); !errors.Is(err, ErrJoinColumnConflict) {
			t.Fatalf("expected ErrJoinColumnConflict, got %v", err)
		}
	})
}
