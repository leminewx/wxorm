package schema

import (
	"reflect"
	"sync"
)

// cacheEntry 定义 Schema 缓存的键
//
// 键由方言名称、模型类型与逻辑表名共同组成：
//   - 同一模型在不同方言下的字段定义并不相同；
//   - 函数内定义的局部类型可能同名（如两个包的测试中都有 type Model struct），
//     只有 reflect.Type 才能唯一标识一个类型；
//   - 实现了 TableNameGetter 的模型可以按实例返回不同表名（分表、多租户），
//     表名不参与键会让首个实例把表名冻结在同一类型的其它实例上。
type cacheEntry struct {
	driver string
	typ    reflect.Type
	table  string
}

var schemaCache = &sync.Map{}

// GetFromCache 从缓存中获取 Schema
func GetFromCache(driver string, typ reflect.Type, table string) (*Schema, bool) {
	if sch, ok := schemaCache.Load(cacheEntry{driver: driver, typ: typ, table: table}); ok {
		return sch.(*Schema), true
	}

	return nil, false
}

// PutToCache 缓存 Schema
func PutToCache(driver string, typ reflect.Type, table string, sch *Schema) {
	schemaCache.Store(cacheEntry{driver: driver, typ: typ, table: table}, sch)
}
