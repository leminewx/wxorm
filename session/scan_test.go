package session

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"
	"testing"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/harness/examples/stubdriver"
	"gitee.com/leminewx/wxorm/schema"
)

// scanMappingModel 用于验证结果列名到模型字段的映射
type scanMappingModel struct {
	ID         int64   `wxorm:"column(id) pk auto"`
	Title      string  `wxorm:"column(title)"`
	AuthorName *string `wxorm:"join(author.name)"`
}

func (*scanMappingModel) GetTableName() string { return "scan_mapping" }

// scanFieldsOf 必须把联表结果列与带表前缀的主表列都映射回模型字段
func TestScanFieldsOfColumnMapping(t *testing.T) {
	dial := dialect.Get("mysql")
	sch, err := schema.Parse(dial, &scanMappingModel{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	db := openStubDB(t)

	cases := []struct {
		name    string
		columns []string
		want    []int
	}{
		{
			name:    "真实驱动返回的列名",
			columns: []string{"id", "title", "author_name"},
			want:    []int{0, 1, 2},
		},
		{
			name:    "主表列带表前缀",
			columns: []string{"scan_mapping.id", "scan_mapping.title", "author_name"},
			want:    []int{0, 1, 2},
		},
		{
			name:    "列顺序与字段顺序不同",
			columns: []string{"author_name", "title", "id"},
			want:    []int{2, 1, 0},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubdriver.Reset()
			stubdriver.Push(tc.columns)

			rows, err := db.Query("SELECT fixture")
			if err != nil {
				t.Fatalf("query stub failed: %v", err)
			}
			defer func() { _ = rows.Close() }()

			got, err := scanFieldsOf(sch, rows)
			if err != nil {
				t.Fatalf("scan fields failed: %v", err)
			}

			if len(got) != len(tc.want) {
				t.Fatalf("fields = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("fields = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// 返回模型不存在的列时必须报错，避免字段静默错位
func TestScanFieldsOfUnknownColumn(t *testing.T) {
	dial := dialect.Get("mysql")
	sch, err := schema.Parse(dial, &scanMappingModel{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	db := openStubDB(t)
	stubdriver.Reset()
	stubdriver.Push([]string{"id", "not_a_column"})

	rows, err := db.Query("SELECT fixture")
	if err != nil {
		t.Fatalf("query stub failed: %v", err)
	}
	defer func() { _ = rows.Close() }()

	if _, err := scanFieldsOf(sch, rows); !errors.Is(err, ErrSelectColumnNotFound) {
		t.Fatalf("err = %v, want ErrSelectColumnNotFound", err)
	}
}

// 脚本耗尽时必须显式失败，而不是返回空结果
func TestStubDriverEmptyScript(t *testing.T) {
	db := openStubDB(t)
	stubdriver.Reset()

	if _, err := db.Query("SELECT fixture"); !errors.Is(err, stubdriver.ErrNoScript) {
		t.Fatalf("err = %v, want ErrNoScript", err)
	}
}

// openStubDB 打开脚本化行驱动；驱动在 TestMain 中注册
func openStubDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open(stubdriver.DriverName, "stub")
	if err != nil {
		t.Fatalf("open stub driver failed: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
		stubdriver.Reset()
	})

	return db
}

// scanNullModel 用可空列验证 SQL NULL 到指针字段的映射
type scanNullModel struct {
	ID   int64   `wxorm:"column(id) pk auto"`
	Name *string `wxorm:"column(name)"`
}

func (*scanNullModel) GetTableName() string { return "scan_null" }

// SQL NULL 必须映射为指针字段的 nil，而不是空字符串
func TestSession_SelectNullColumn(t *testing.T) {
	dial := dialect.Get("mysql")
	sch, err := schema.Parse(dial, &scanNullModel{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	db := openStubDB(t)
	stubdriver.Reset()
	stubdriver.Push([]string{"id", "name"},
		[]driver.Value{int64(1), nil},
	)

	sess := NewSession(1, "stub", db, dial, &sync.Map{})
	sess.SetSchema(sch)

	var got []scanNullModel
	if _, err := sess.SELECT(&got); err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %d, want 1", len(got))
	}
	if got[0].Name != nil {
		t.Fatalf("NULL 列应映射为 nil，实际 %#v", got[0].Name)
	}
}
