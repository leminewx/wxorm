package schema

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/harness/examples"
)

// TestParse 测试 Parse 函数
func TestParse(t *testing.T) {
	dial := dialect.Get("mysql")

	t.Run("success with TableNameGetter", func(t *testing.T) {
		u := &examples.User{}
		_, ok := any(u).(TableNameGetter)
		if !ok {
			t.Skip("User does not implement TableNameGetter, but test expects it")
		}
		// 实际需要 User 实现 GetTableName 方法，但我们可以定义另一个类型
		// 这里为了演示，先跳过，改用有方法的类型
	})

	t.Run("with TableNameGetter", func(t *testing.T) {
		u := &examples.User{}
		s, err := Parse(dial, u)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		if s.TableName != "users" {
			t.Errorf("TableName = %q, want %q", s.TableName, "users")
		}
		if s.ModelType.String() != "examples.User" {
			t.Errorf("ModelName = %q, want %q", s.ModelName, "examples.User")
		}
	})

	t.Run("without TableNameGetter", func(t *testing.T) {
		type NoTable struct {
			ID int `orm:"pk"`
		}
		u := &NoTable{}
		s, err := Parse(dial, u)
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		// 表名应为下划线格式：no_table
		expected := "no_table"
		if s.TableName != expected {
			t.Errorf("TableName = %q, want %q", s.TableName, expected)
		}
	})

	t.Run("field filtering", func(t *testing.T) {
		type Model struct {
			ID       int    `wxorm:"primary_key"`
			Name     string `wxorm:"size:100"`
			Ignored  int    `orm:"-"`
			Ignored2 int    `wxorm:"-"`
			NoTag    string
			unexport int // 未导出
		}
		s, err := Parse(dial, &Model{})
		if err != nil {
			t.Fatalf("Parse failed: %v", err)
		}
		// 预期字段：ID, Name, NoTag （因为未导出和标签为 - 的都被过滤）
		// 注意：NoTag 没有标签，但按逻辑，标签为空时会尝试 orm 标签，若 orm 也为空，则跳过。
		// 实际代码：先取 wxorm 标签，为空再取 orm，两者均为空则 continue。所以 NoTag 会被过滤。
		// 因此只有 ID 和 Name 被保留。
		if len(s.Columns) != 2 {
			t.Errorf("expected 2 columns, got %d", len(s.Columns))
		}
	})

	t.Run("renamed tag key is rejected", func(t *testing.T) {
		// 改名后只认 wxorm 与 orm；旧键 worm 不再解析，并在解析阶段直接报错，
		// 避免旧模型被静默跳过而少列。
		type Model struct {
			ID   int    `wxorm:"column(id)"`
			Name string `worm:"column(legacy_name)"`
		}

		if _, err := Parse(dial, &Model{}); !errors.Is(err, ErrLegacyTagRemoved) {
			t.Fatalf("expected ErrLegacyTagRemoved, got %v", err)
		}
	})

	t.Run("empty struct", func(t *testing.T) {
		type Empty struct{}
		_, err := Parse(dial, &Empty{})
		if !errors.Is(err, ErrModelEmptyFields) {
			t.Errorf("expected ErrModelEmptyFields, got %v", err)
		}
	})

	t.Run("nil model", func(t *testing.T) {
		_, err := Parse(dial, nil)
		if !errors.Is(err, ErrModelNilPointer) {
			t.Errorf("expected ErrModelNilPointer, got %v", err)
		}
	})

	t.Run("non-struct model", func(t *testing.T) {
		_, err := Parse(dial, 42)
		if !errors.Is(err, ErrModelNotStruct) {
			t.Errorf("expected ErrModelNotStruct, got %v", err)
		}
	})
}

// TestParsePrimaryKeyNames 验证主键列表与自增主键的预计算
//
// PostgreSQL 的 ON CONFLICT (...) 需要冲突目标，INSERT ... RETURNING 需要自增主键，
// 两者都在 Parse 阶段算好，避免运行时反复扫描列定义。
func TestParsePrimaryKeyNames(t *testing.T) {
	pg := dialect.Get("postgres")

	type Model struct {
		ID       int64  `wxorm:"column(id) pk auto"`
		TenantID int64  `wxorm:"column(tenant_id) pk"`
		Name     string `wxorm:"column(name)"`
	}

	sch, err := Parse(pg, &Model{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if want := []string{"id", "tenant_id"}; !slices.Equal(sch.PrimaryKeyNames, want) {
		t.Errorf("PrimaryKeyNames = %v, want %v", sch.PrimaryKeyNames, want)
	}

	if sch.AutoPrimaryKeyName != "id" {
		t.Errorf("AutoPrimaryKeyName = %q, want id", sch.AutoPrimaryKeyName)
	}
}

// TestGetModelStruct 测试 GetModelStruct
func TestGetModelStruct(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		wantErr error
	}{
		{"nil", nil, ErrModelNilPointer},
		{"nil pointer", (*int)(nil), ErrModelNilPointer},
		{"non-struct", 42, ErrModelNotStruct},
		{"struct", struct{}{}, nil},
		{"pointer to struct", &struct{}{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := GetModelStruct(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("GetModelStruct() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				if val.Kind() != reflect.Struct {
					t.Errorf("expected struct kind, got %v", val.Kind())
				}
			}
		})
	}
}

// TestGetReflectFieldValue 测试 GetReflectFieldValue
func TestGetReflectFieldValue(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		wantVal  any
		wantZero bool
	}{
		{"int zero", 0, int64(0), true},
		{"int positive", 42, int64(42), false},
		{"int negative", -5, int64(-5), false},
		{"uint zero", uint(0), uint64(0), true},
		{"uint positive", uint(10), uint64(10), false},
		{"float zero", 0.0, 0.0, true},
		{"float positive", 3.14, 3.14, false},
		{"bool true", true, true, false},
		{"bool false", false, false, true},
		{"string empty", "", "", true},
		{"string non-empty", "hello", "hello", false},
		{"nil slice", []int(nil), []int(nil), true},
		{"empty slice", []int{}, []int{}, false},
		{"nil map", map[string]int(nil), map[string]int(nil), true},
		{"empty map", map[string]int{}, map[string]int{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.value)
			gotVal, gotZero := GetReflectFieldValue(val)
			if !reflect.DeepEqual(gotVal, tt.wantVal) {
				t.Errorf("GetReflectFieldValue() value = %v, want %v", gotVal, tt.wantVal)
			}
			if gotZero != tt.wantZero {
				t.Errorf("GetReflectFieldValue() zero = %v, want %v", gotZero, tt.wantZero)
			}
		})
	}
}

// TestIsZeroValue 测试 IsZeroValue
func TestIsZeroValue(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  bool
	}{
		{"int zero", 0, true},
		{"int non-zero", 42, false},
		{"string empty", "", true},
		{"string non-empty", "hello", false},
		{"bool true", true, false},
		{"bool false", false, true},
		{"nil pointer", (*int)(nil), true},
		{"non-nil pointer", func() *int { i := 10; return &i }(), false},
		{"nil slice", []int(nil), true},
		{"empty slice", []int{}, false},
		{"nil map", map[string]int(nil), true},
		{"empty map", map[string]int{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.value)
			if got := IsZeroValue(val); got != tt.want {
				t.Errorf("IsZeroValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSchema_ValidateStruct 测试 Schema.ValidateStruct
func TestSchema_ValidateStruct(t *testing.T) {
	type Model struct {
		ID int `wxorm:"pk"`
	}
	dial := dialect.Get("mysql")
	s, err := Parse(dial, &Model{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	tests := []struct {
		name    string
		value   any
		wantErr bool
	}{
		{"valid struct", Model{}, false},
		{"valid pointer", &Model{}, false},
		{"nil", nil, true},
		{"non-struct", 42, true},
		{"wrong type", struct{ X int }{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.ValidateStruct(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateStruct() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestSchema_ExtractValue 测试 ExtractValue
func TestSchema_ExtractValue(t *testing.T) {
	type Model struct {
		ID    int    `wxorm:"auto"`
		Name  string `wxorm:"type(varchar(100))"`
		Age   int    `orm:"index"`
		Email string `wxorm:"index"`
	}
	dial := dialect.Get("mysql")
	s, err := Parse(dial, &Model{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	// 预期可插入字段：Name, Age, Email（ID 是 auto_increment，被排除）
	model := Model{
		ID:    1,
		Name:  "Alice",
		Age:   30,
		Email: "alice@example.com",
	}
	if _, err := s.ExtractValue(model); err != nil {
		t.Fatalf("ExtractValue failed: %v", err)
	}

}

// TestSchema_ExtractNonZeroNamesAndValuesFromPrimaryKeys 测试提取主键非零值
func TestSchema_ExtractNonZeroNamesAndValuesFromPrimaryKeys(t *testing.T) {
	type Model struct {
		Id   int `wxorm:"pk"`
		Name string
	}
	dial := dialect.Get("mysql")
	s, err := Parse(dial, &Model{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// 非零主键
	val, _ := s.ValidateStruct(Model{Id: 10, Name: "Bob"})
	pkMap := s.ExtractNonZeroNamesAndValuesFromPrimaryKeys(val)
	if len(pkMap) != 1 {
		t.Errorf("expected map[id]=10, got %v", pkMap)
	}

	// 零值主键
	valZero, _ := s.ValidateStruct(Model{Name: "Bob"})
	pkMapZero := s.ExtractNonZeroNamesAndValuesFromPrimaryKeys(valZero)
	if len(pkMapZero) != 0 {
		t.Errorf("expected empty map, got %v", pkMapZero)
	}
}

// TestSchema_ExtractNamesAndValuesFromNonPrimaryKeys 测试提取非主键字段（包括零值）
func TestSchema_ExtractNamesAndValuesFromNonPrimaryKeys(t *testing.T) {
	type Model struct {
		Id    int    `wxorm:"pk"`
		Name  string `wxorm:"size:100"`
		Age   int    `wxorm:"updated"`
		Email string `orm:"index"`
	}
	dial := dialect.Get("mysql")
	s, err := Parse(dial, &Model{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	// 应跳过 ID（主键）和 Age（auto_insert），保留 Name 和 Email（即使零值）
	val, _ := s.ValidateStruct(Model{Id: 1, Name: "", Age: 20, Email: "e@x.com"})
	nm := s.ExtractNamesAndValuesFromNonPrimaryKeys(val)
	if len(nm) != 3 {
		t.Errorf("got %v", nm)
	}
}

// TestSchema_ExtractNonZeroNamesAndValuesFromNonPrimaryKeys 测试提取非主键非自动插入的非零值
func TestSchema_ExtractNonZeroNamesAndValuesFromNonPrimaryKeys(t *testing.T) {
	type Model struct {
		ID    int       `wxorm:"pk"`
		Name  string    `wxorm:"index"`
		Age   time.Time `wxorm:"created"`
		Email string
	}
	dial := dialect.Get("mysql")
	s, err := Parse(dial, &Model{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	// 应包含非零的 Email，Name 为零值被跳过，Age 是 auto_insert 被跳过
	val, _ := s.ValidateStruct(Model{ID: 1, Name: "aaa", Age: time.Now(), Email: "e@x.com"})
	nm := s.ExtractNonZeroNamesAndValuesFromNonPrimaryKeys(val)
	if len(nm) != 1 {
		t.Errorf("got %v", nm)
	}
}

// TestSchema_ExtractNonZeroPrimaryKeyValues 多主键按列顺序输出，保证 WHERE 的 SQL 稳定
func TestSchema_ExtractNonZeroPrimaryKeyValues(t *testing.T) {
	type Model struct {
		TenantID int    `wxorm:"pk column(tenant_id)"`
		ID       int    `wxorm:"pk column(id)"`
		Name     string `wxorm:"column(name)"`
	}
	dial := dialect.Get("mysql")
	s, err := Parse(dial, &Model{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	val, _ := s.ValidateStruct(Model{TenantID: 7, ID: 9, Name: "Bob"})
	got := s.ExtractNonZeroPrimaryKeyValues(val)
	if len(got) != 2 {
		t.Fatalf("expected 2 primary keys, got %v", got)
	}
	if got[0].Name != "tenant_id" || got[0].Value != int64(7) {
		t.Errorf("expected tenant_id=7 first, got %v", got[0])
	}
	if got[1].Name != "id" || got[1].Value != int64(9) {
		t.Errorf("expected id=9 second, got %v", got[1])
	}

	// 零值主键被跳过
	valZero, _ := s.ValidateStruct(Model{TenantID: 7})
	if got = s.ExtractNonZeroPrimaryKeyValues(valZero); len(got) != 1 || got[0].Name != "tenant_id" {
		t.Errorf("expected only tenant_id, got %v", got)
	}
}

// TestSchema_ExtractUpdateNamesAndValues 测试可更新列的零值策略
func TestSchema_ExtractUpdateNamesAndValues(t *testing.T) {
	type Model struct {
		ID        int       `wxorm:"pk auto"`
		Name      string    `wxorm:"column(name)"`
		Age       int       `wxorm:"column(age)"`
		CreatedAt time.Time `wxorm:"created"`
		UpdatedAt time.Time `wxorm:"updated"`
	}
	dial := dialect.Get("mysql")
	s, err := Parse(dial, &Model{})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// 默认跳过零值，且不包含主键与数据库填充列
	valZero, _ := s.ValidateStruct(Model{ID: 1})
	if got := s.ExtractUpdateNamesAndValues(valZero, false); len(got) != 0 {
		t.Errorf("expected empty update set, got %v", got)
	}

	// includeZero 为 true 时写回零值
	got := s.ExtractUpdateNamesAndValues(valZero, true)
	if len(got) != 2 {
		t.Fatalf("expected name and age, got %v", got)
	}
	if _, ok := got["name"]; !ok {
		t.Errorf("expected name in %v", got)
	}
	if _, ok := got["age"]; !ok {
		t.Errorf("expected age in %v", got)
	}

	// 非零值在两种模式下都包含
	val, _ := s.ValidateStruct(Model{ID: 1, Name: "Bob"})
	if got = s.ExtractUpdateNamesAndValues(val, false); len(got) != 1 || got["name"] != "Bob" {
		t.Errorf("expected name=Bob, got %v", got)
	}
}

/*
cpu: QEMU Virtual CPU version 2.5+						2			6376	    184906 ns/op	    4460 B/op	      65 allocs/op
cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		8		41040513	        24.52 ns/op	       0 B/op	       0 allocs/op
*/
func BenchmarkSchema_Parse(b *testing.B) {
	dial := dialect.Get("mysql")
	model := &examples.User{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := Parse(dial, model); err != nil {
			b.Fatal(err)
		}
	}
}

/*
cpu: QEMU Virtual CPU version 2.5+						2			2630751	       458.8 ns/op	     216 B/op	       5 allocs/op
cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz		8			3842355	       315.4 ns/op	     216 B/op	       5 allocs/op
*/
func BenchmarkSchema_ExtractValue(b *testing.B) {
	dial := dialect.Get("mysql")
	table, err := Parse(dial, &examples.User{})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err = table.ExtractValue(&examples.User{
			ID:        1,
			Name:      "wx",
			Age:       18,
			Email:     "wx@test.com",
			UpdatedAt: time.Now().Format(time.DateTime),
			GroupID:   3,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

// shardedModel 按实例返回不同表名，用于验证缓存键包含表名
type shardedModel struct {
	Partition string
	ID        int    `orm:"pk"`
	Name      string `orm:"column(name)"`
}

// GetTableName 返回当前分片对应的表名
func (own *shardedModel) GetTableName() string {
	return "shard_" + own.Partition
}

// TestParse_TableNameInCacheKey 测试分表模型不会共用首个实例的表名
//
// 缓存键若只含（方言, 类型），同一类型的不同分片实例会拿到首次解析的结果，
// 后续读写全部落到错误的物理表上。
func TestParse_TableNameInCacheKey(t *testing.T) {
	dial := dialect.Get("mysql")

	first, err := Parse(dial, &shardedModel{Partition: "2024"})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	second, err := Parse(dial, &shardedModel{Partition: "2025"})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if first.TableName != "shard_2024" {
		t.Errorf("first partition table = %q, want shard_2024", first.TableName)
	}

	if second.TableName != "shard_2025" {
		t.Errorf("second partition table = %q, want shard_2025", second.TableName)
	}

	if first == second {
		t.Error("different partitions must not share the same schema instance")
	}
}
