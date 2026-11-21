package schema

import (
	"reflect"
	"testing"
	"time"

	"gitee.com/leminewx/wxorm/dialect"
)

// zeroModel 用于验证零值判断与列名索引
type zeroModel struct {
	ID   int       `orm:"column(id) pk auto"`
	At   time.Time `orm:"column(at)"`
	Text string    `orm:"column(text)"`
}

func (own *zeroModel) GetTableName() string { return "zero_models" }

// 测试结构体字段（time.Time）的零值判断不依赖 reflect.DeepEqual
func TestGetReflectFieldValue_Time(t *testing.T) {
	value := reflect.ValueOf(&zeroModel{}).Elem()

	got, zero := GetReflectFieldValue(value.Field(1))
	if !zero {
		t.Error("expected zero time to be treated as zero value")
	}

	if _, ok := got.(time.Time); !ok {
		t.Errorf("expected time.Time value, got %T", got)
	}

	value.Field(1).Set(reflect.ValueOf(time.Now()))
	if _, zero = GetReflectFieldValue(value.Field(1)); zero {
		t.Error("expected non-zero time to be treated as non-zero value")
	}
}

// 测试零值字段不会进入更新集合
func TestExtractNonZeroNamesAndValuesFromNonPrimaryKeys(t *testing.T) {
	sch, err := Parse(dialect.Get("mysql"), &zeroModel{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	values := sch.ExtractNonZeroNamesAndValuesFromNonPrimaryKeys(reflect.ValueOf(&zeroModel{}).Elem())
	if _, ok := values["at"]; ok {
		t.Error("zero time should be skipped")
	}
	if _, ok := values["text"]; ok {
		t.Error("empty string should be skipped")
	}

	now := time.Now()
	values = sch.ExtractNonZeroNamesAndValuesFromNonPrimaryKeys(reflect.ValueOf(&zeroModel{At: now, Text: "x"}).Elem())
	if _, ok := values["at"]; !ok {
		t.Error("non-zero time should be included")
	}
	if _, ok := values["text"]; !ok {
		t.Error("non-empty string should be included")
	}
}

// 测试列名到模型字段索引的预计算映射
func TestColumnIndexesByName(t *testing.T) {
	sch, err := Parse(dialect.Get("mysql"), &zeroModel{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	for i, name := range sch.ColumnNames {
		idx, ok := sch.ColumnIndexesByName[name]
		if !ok {
			t.Fatalf("column %s not found in map", name)
		}

		if idx != sch.ColumnIndexesInModel[i] {
			t.Errorf("column %s maps to %d, want %d", name, idx, sch.ColumnIndexesInModel[i])
		}
	}
}

// 测试插入语句缓存键：列集与物理表名都必须参与
func TestInsertCacheKey(t *testing.T) {
	dial := dialect.Get("mysql")

	full, err := Parse(dial, &zeroModel{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	narrow, err := Parse(dial, &struct {
		ID int `orm:"column(id) pk auto"`
	}{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	// 不同列集不能共用同一条预编译语句
	if full.GetInsertCacheKey("users") == narrow.GetInsertCacheKey("users") {
		t.Error("insert cache keys of different column sets must not collide")
	}

	// 普通插入与 INSERT IGNORE 是两条不同语句
	if full.GetInsertCacheKey("users") == full.GetInsertIgnoreCacheKey("users") {
		t.Error("ignore insert must not share key with normal insert")
	}

	// 同一模型配上不同前缀（不同物理表名）时也不能共用语句
	if full.GetInsertCacheKey("tenant1_users") == full.GetInsertCacheKey("tenant2_users") {
		t.Error("insert cache keys of different physical tables must not collide")
	}
}

// 基准测试：结构体字段取值与零值判断
func BenchmarkGetReflectFieldValue(b *testing.B) {
	value := reflect.ValueOf(&zeroModel{At: time.Now(), Text: "x"}).Elem()
	field := value.Field(1)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = GetReflectFieldValue(field)
	}
}

// 基准测试：主键提取（切片输出，按列顺序稳定）
func BenchmarkSchema_ExtractPrimaryKey(b *testing.B) {
	type model struct {
		TenantID int    `wxorm:"pk column(tenant_id)"`
		ID       int    `wxorm:"pk column(id)"`
		Name     string `wxorm:"column(name)"`
	}

	sch, err := Parse(dialect.Get("mysql"), &model{})
	if err != nil {
		b.Fatal(err)
	}

	value := reflect.ValueOf(&model{TenantID: 7, ID: 9, Name: "Bob"}).Elem()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if values := sch.ExtractNonZeroPrimaryKeyValues(value); len(values) != 2 {
			b.Fatalf("got %v", values)
		}
	}
}
