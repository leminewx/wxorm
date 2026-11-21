package datatype

import (
	"math/big"
	"net"
	"reflect"
	"time"
)

/*
SQLite 采用「动态类型 + 类型亲和性（type affinity）」，只有五种存储类别：
NULL、INTEGER、REAL、TEXT、BLOB。因此这里不做 MySQL 那样的宽度细分，
而是按亲和性收敛，避免声明出一堆 SQLite 不认识的具体类型。

几处刻意选择：
  - 所有整型（含有符号、无符号）统一用 INTEGER。SQLite 的 INTEGER 是 8 字节有符号，
    uint64 超过 MaxInt64 的值会溢出，这类字段建议改用 string 存十进制文本；
  - time.Time 用 DATETIME 而非 TEXT。驱动（mattn/go-sqlite3、modernc.org/sqlite）
    只有在列声明为 DATE/DATETIME/TIMESTAMP 时才会把结果解析回 time.Time；
  - bool 用 INTEGER（0/1），SQLite 没有原生布尔类型；
  - JSON 用 TEXT，配合 json_extract / json_each 等 JSON1 函数使用；
  - big.Int / big.Float 用 TEXT 而不是 NUMERIC：SQLite 的 NUMERIC 亲和性会把超出
    INTEGER/REAL 范围的值转成 REAL，大整数与高精度小数会静默丢精度；框架在写入/读取
    两端以十进制文本桥接，TEXT 是唯一能无损保存的选择（这类列在 SQLite 上无法直接做
    数值大小比较与聚合）；
  - big.Rat 不在支持列表内（schema.Parse 阶段拒绝）；
*/

const (
	_INTEGER_TO_SQLITE  Datatype = "INTEGER"  // 所有整型（含 time.Duration）
	_REAL_TO_SQLITE     Datatype = "REAL"     // float32、float64
	_TEXT_TO_SQLITE     Datatype = "TEXT"     // string、JSON、net.IP
	_BLOB_TO_SQLITE     Datatype = "BLOB"     // []byte
	_DATETIME_TO_SQLITE Datatype = "DATETIME" // time.Time
	_BIG_TO_SQLITE      Datatype = "TEXT"     // *big.Int、*big.Float（十进制文本，避免 NUMERIC 亲和性转 REAL 丢精度）
)

// ConvertGoDatatypeToSQLiteDatatype 将 Go 中的数据类型转换为 SQLite 数据库中的数据类型
func ConvertGoDatatypeToSQLiteDatatype(value reflect.Type) Datatype {
	// 处理指针类型，获取其指向的实际类型
	for value != nil && value.Kind() == reflect.Ptr {
		value = value.Elem()
	}

	if value == nil {
		return _TEXT_TO_SQLITE
	}

	// 首先检查具名类型
	switch value {
	case reflect.TypeFor[time.Time]():
		return _DATETIME_TO_SQLITE
	case reflect.TypeFor[time.Duration]():
		return _INTEGER_TO_SQLITE
	case reflect.TypeFor[big.Float](), reflect.TypeFor[big.Int](), reflect.TypeFor[big.Rat]():
		return _BIG_TO_SQLITE
	case reflect.TypeFor[[]byte]():
		return _BLOB_TO_SQLITE
	case reflect.TypeFor[net.IP]():
		return _TEXT_TO_SQLITE
	}

	// 然后检查 reflect.Kind
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return _INTEGER_TO_SQLITE
	case reflect.Float32, reflect.Float64:
		return _REAL_TO_SQLITE
	case reflect.Bool:
		return _INTEGER_TO_SQLITE
	case reflect.String:
		return _TEXT_TO_SQLITE
	case reflect.Slice:
		if value.Elem().Kind() == reflect.Uint8 {
			return _BLOB_TO_SQLITE
		}
		return _TEXT_TO_SQLITE
	case reflect.Map, reflect.Struct, reflect.Array:
		return _TEXT_TO_SQLITE
	default:
		return _TEXT_TO_SQLITE
	}
}
