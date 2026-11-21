package column

import (
	"testing"

	"gitee.com/leminewx/wxorm/dialect/datatype"
)

// 测试基础字段解析（名称、数据类型）
func TestNewColumn_Basic(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		wantCol  string
		wantType datatype.Datatype
	}{
		{
			name:     "解析字段名和类型",
			tag:      "column(user_id),type(int unsigned)",
			wantCol:  "user_id",
			wantType: "INT UNSIGNED",
		},
		{
			name:     "类型包含括号（如varchar(30)）",
			tag:      "column(name),type(varchar(30))",
			wantCol:  "name",
			wantType: "VARCHAR(30)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col, err := newColumn(tt.tag)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			assertEqual(t, col.Column, tt.wantCol, "字段名不匹配")
			assertEqual(t, col.Datatype, tt.wantType, "数据类型不匹配")
		})
	}
}

// 测试主键、自增、空值约束
func TestNewColumn_Constraints(t *testing.T) {
	tests := []struct {
		name         string
		tag          string
		wantPK       bool
		wantAuto     bool
		wantNullable string // "NOT NULL" / "NULL" / ""（未设置）
	}{
		{
			name:         "主键+自增+非空",
			tag:          "pk,auto,notnull",
			wantPK:       true,
			wantAuto:     true,
			wantNullable: "NOT NULL",
		},
		{
			name:         "可空",
			tag:          "null",
			wantPK:       false,
			wantAuto:     false,
			wantNullable: "NULL",
		},
		{
			name:     "未设置空值约束",
			tag:      "pk",
			wantPK:   true,
			wantAuto: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col, err := newColumn(tt.tag)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			assertEqual(t, col.PrimaryKey, tt.wantPK, "主键标识不匹配")
			assertEqual(t, col.AutoIncrement, tt.wantAuto, "自增标识不匹配")

			if tt.wantNullable != "" {
				assertTrue(t, col.Nullable.Has, "预期设置了空值约束")
				assertEqual(t, col.Nullable.Value, tt.wantNullable, "空值约束值不匹配")
			} else {
				assertFalse(t, col.Nullable.Has, "不应设置空值约束")
			}
		})
	}
}

// 测试默认值、创建/更新时间
func TestNewColumn_DefaultAndTime(t *testing.T) {
	tests := []struct {
		name        string
		tag         string
		wantDefault string
		wantCreated bool
		wantUpdated bool
	}{
		{
			name:        "默认值为字符串",
			tag:         "default('default_name')",
			wantDefault: "'default_name'",
		},
		{
			name:        "默认值为数字",
			tag:         "default(0)",
			wantDefault: "0",
		},
		{
			name:        "创建时间",
			tag:         "created",
			wantCreated: true,
		},
		{
			name:        "更新时间",
			tag:         "updated",
			wantUpdated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col, err := newColumn(tt.tag)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}

			if tt.wantDefault != "" {
				assertTrue(t, col.Default.Has, "预期设置了默认值")
				assertEqual(t, col.Default.Value, tt.wantDefault, "默认值不匹配")
			}

			assertEqual(t, col.Created, tt.wantCreated, "创建时间标识不匹配")
			assertEqual(t, col.Updated, tt.wantUpdated, "更新时间标识不匹配")
		})
	}
}

// 测试索引和唯一约束
func TestNewColumn_IndexAndUnique(t *testing.T) {
	tests := []struct {
		name       string
		tag        string
		wantIndex  Metadata
		wantUnique Metadata
	}{
		{
			name:      "匿名索引",
			tag:       "index",
			wantIndex: Metadata{Has: true, Value: ""},
		},
		{
			name:      "命名索引",
			tag:       "index(idx_name)",
			wantIndex: Metadata{Has: true, Value: "idx_name"},
		},
		{
			name:       "匿名唯一约束",
			tag:        "unique",
			wantUnique: Metadata{Has: true, Value: ""},
		},
		{
			name:       "命名唯一约束",
			tag:        "unique(unique_email)",
			wantUnique: Metadata{Has: true, Value: "unique_email"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col, err := newColumn(tt.tag)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}

			assertEqual(t, col.Index.Has, tt.wantIndex.Has, "索引存在性不匹配")
			assertEqual(t, col.Index.Value, tt.wantIndex.Value, "索引值不匹配")

			assertEqual(t, col.Unique.Has, tt.wantUnique.Has, "唯一约束存在性不匹配")
			assertEqual(t, col.Unique.Value, tt.wantUnique.Value, "唯一约束值不匹配")
		})
	}
}

// 测试外键解析
func TestNewColumn_ForeignKey(t *testing.T) {
	tests := []struct {
		name       string
		tag        string
		wantTable  string
		wantColumn string
		wantName   string
		wantOUC    bool // OnUpdateCascade
		wantODC    bool // OnDeleteCascade
		wantNoFK   bool // 是否预期无外键（解析失败）
	}{
		{
			name:       "完整外键（含名称和级联）",
			tag:        "fk(group(id), 'fk_user_group', ouc, odc)",
			wantTable:  "group",
			wantColumn: "id",
			wantName:   "fk_user_group",
			wantOUC:    true,
			wantODC:    true,
		},
		{
			name:       "无名称和级联的外键",
			tag:        "fk(role(id))",
			wantTable:  "role",
			wantColumn: "id",
			wantName:   "",
			wantOUC:    false,
			wantODC:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col, err := newColumn(tt.tag)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}

			assertNotNil(t, col.ForeignKey, "预期有外键")
			assertEqual(t, col.ForeignKey.Table, tt.wantTable, "外键关联表不匹配")
			assertEqual(t, col.ForeignKey.Column, tt.wantColumn, "外键关联列不匹配")
			assertEqual(t, col.ForeignKey.Name, tt.wantName, "外键名称不匹配")
			assertEqual(t, col.ForeignKey.OnUpdateCascade, tt.wantOUC, "级联更新标识不匹配")
			assertEqual(t, col.ForeignKey.OnDeleteCascade, tt.wantODC, "级联删除标识不匹配")
		})
	}
}

// 测试注释、字符集、排序规则
func TestNewColumn_CommentAndCharset(t *testing.T) {
	tests := []struct {
		name        string
		tag         string
		wantComment string
		wantCharset string
		wantCollate string
	}{
		{
			name:        "普通注释",
			tag:         "comment('用户年龄')",
			wantComment: "'用户年龄'",
		},
		{
			name:        "含单引号的注释（需转义）",
			tag:         "comment('user's age')", // 原始标签含单引号
			wantComment: "'user''s age'",         // 预期转义为两个单引号
		},
		{
			name:        "字符集和排序规则",
			tag:         "charset(utf8mb4),collate(utf8mb4_general_ci)",
			wantCharset: "utf8mb4",
			wantCollate: "utf8mb4_general_ci",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col, err := newColumn(tt.tag)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}

			assertEqual(t, col.Comment, tt.wantComment, "注释内容不匹配")
			assertEqual(t, col.Charset, tt.wantCharset, "字符集不匹配")
			assertEqual(t, col.Collate, tt.wantCollate, "排序规则不匹配")
		})
	}
}

// 测试复合标签（多属性组合）
func TestNewColumn_Composite(t *testing.T) {
	tag := "column(email),type(varchar(255)),unique,default(''),notnull,comment('邮箱地址'),created"
	col, err := newColumn(tag)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	assertEqual(t, col.Column, "email", "字段名不匹配")
	assertEqual(t, string(col.Datatype), "VARCHAR(255)", "数据类型不匹配")
	assertTrue(t, col.Unique.Has, "应设置唯一约束")
	assertTrue(t, col.Default.Has, "应设置默认值")
	assertEqual(t, col.Default.Value, "''", "默认值不匹配")
	assertEqual(t, col.Nullable.Value, "NOT NULL", "空值约束不匹配")
	assertEqual(t, col.Comment, "'邮箱地址'", "注释不匹配")
	assertTrue(t, col.Created, "应设置创建时间标识")
}

// 辅助断言函数，简化测试代码
func assertEqual(t *testing.T, got, want interface{}, msg string) {
	t.Helper()
	if got == want {
		return
	}

	t.Errorf("%s: got %v, want %v", msg, got, want)
}

func assertTrue(t *testing.T, value bool, msg string) {
	t.Helper()
	if value {
		return
	}

	t.Errorf("%s: got false, want true", msg)
}

func assertFalse(t *testing.T, value bool, msg string) {
	t.Helper()
	if !value {
		return
	}

	t.Errorf("%s: got true, want false", msg)
}

func assertNotNil(t *testing.T, value any, msg string) {
	t.Helper()
	if value != nil {
		return
	}

	t.Errorf("%s: got nil, want non-nil", msg)
}
