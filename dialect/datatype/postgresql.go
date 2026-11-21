package datatype

import (
	"math/big"
	"net"
	"reflect"
	"strconv"
	"time"
)

/*
PostgreSQL 的整型只有 SMALLINT(2B)、INTEGER(4B)、BIGINT(8B) 三档，
没有 MySQL 的 UNSIGNED 修饰，因此无符号类型只能用更大的有符号类型承载，
超过 BIGINT 上限的 uint64 退化为 NUMERIC(20)，避免隐式溢出。

与 MySQL 的一处刻意差异：Go 的 byte/rune 分别等价于 uint8/int32，
运行时无法与普通 uint8/int32 区分，这里按 Kind 统一映射，
不走“rune → CHAR(1)”的特判（那会让所有 int32 字段都变成单字符列）。
*/

const (
	_INT8_TO_POSTGRES  Datatype = "SMALLINT" // int8
	_INT16_TO_POSTGRES Datatype = "SMALLINT" // int16
	_INT32_TO_POSTGRES Datatype = "INTEGER"  // int32
	_INT64_TO_POSTGRES Datatype = "BIGINT"   // int64

	_INT_TO_POSTGRES_32 Datatype = "INTEGER" // int（32 位平台）
	_INT_TO_POSTGRES_64 Datatype = "BIGINT"  // int（64 位平台）

	_UINT8_TO_POSTGRES  Datatype = "SMALLINT"    // uint8
	_UINT16_TO_POSTGRES Datatype = "INTEGER"     // uint16
	_UINT32_TO_POSTGRES Datatype = "BIGINT"      // uint32
	_UINT64_TO_POSTGRES Datatype = "NUMERIC(20)" // uint64

	_UINT_TO_POSTGRES_32 Datatype = "BIGINT"      // uint（32 位平台）
	_UINT_TO_POSTGRES_64 Datatype = "NUMERIC(20)" // uint（64 位平台）

	_FLOAT32_TO_POSTGRES   Datatype = "REAL"             // float32
	_FLOAT64_TO_POSTGRES   Datatype = "DOUBLE PRECISION" // float64
	_BIG_FLOAT_TO_POSTGRES Datatype = "NUMERIC"          // *big.Int、*big.Float（十进制文本写入，NUMERIC 无精度上限）

	_STRING_TO_POSTGRES    Datatype = "VARCHAR(255)" // string
	_INTERFACE_TO_POSTGRES Datatype = "JSONB"        // interface{}

	_TIME_TO_POSTGRES     Datatype = "TIMESTAMPTZ" // time.Time（带时区，避免时区漂移）
	_DURATION_TO_POSTGRES Datatype = "INTERVAL"    // time.Duration

	_BOOL_TO_POSTGRES  Datatype = "BOOLEAN" // bool
	_BYTES_TO_POSTGRES Datatype = "BYTEA"   // []byte
	_IP_TO_POSTGRES    Datatype = "INET"    // net.IP
	_JSON_TO_POSTGRES  Datatype = "JSONB"   // []any、map、struct、array
)

// ConvertGoDatatypeToPostgresDatatype 将 Go 中的数据类型转换为 PostgreSQL 数据库中的数据类型
func ConvertGoDatatypeToPostgresDatatype(value reflect.Type) Datatype {
	// 处理指针类型，获取其指向的实际类型
	for value != nil && value.Kind() == reflect.Ptr {
		value = value.Elem()
	}

	if value == nil {
		return _INTERFACE_TO_POSTGRES
	}

	// 首先检查具名类型
	switch value {
	case reflect.TypeFor[time.Time]():
		return _TIME_TO_POSTGRES
	case reflect.TypeFor[time.Duration]():
		return _DURATION_TO_POSTGRES
	case reflect.TypeFor[big.Float](), reflect.TypeFor[big.Rat](), reflect.TypeFor[big.Int]():
		return _BIG_FLOAT_TO_POSTGRES
	case reflect.TypeFor[[]byte]():
		return _BYTES_TO_POSTGRES
	case reflect.TypeFor[net.IP]():
		return _IP_TO_POSTGRES
	}

	// 然后检查 reflect.Kind
	switch value.Kind() {
	case reflect.Int:
		if strconv.IntSize == 64 {
			return _INT_TO_POSTGRES_64
		}
		return _INT_TO_POSTGRES_32
	case reflect.Int8:
		return _INT8_TO_POSTGRES
	case reflect.Int16:
		return _INT16_TO_POSTGRES
	case reflect.Int32:
		return _INT32_TO_POSTGRES
	case reflect.Int64:
		return _INT64_TO_POSTGRES
	case reflect.Uint:
		if strconv.IntSize == 64 {
			return _UINT_TO_POSTGRES_64
		}
		return _UINT_TO_POSTGRES_32
	case reflect.Uint8:
		return _UINT8_TO_POSTGRES
	case reflect.Uint16:
		return _UINT16_TO_POSTGRES
	case reflect.Uint32:
		return _UINT32_TO_POSTGRES
	case reflect.Uint64:
		return _UINT64_TO_POSTGRES
	case reflect.Float32:
		return _FLOAT32_TO_POSTGRES
	case reflect.Float64:
		return _FLOAT64_TO_POSTGRES
	case reflect.Bool:
		return _BOOL_TO_POSTGRES
	case reflect.String:
		return _STRING_TO_POSTGRES
	case reflect.Slice:
		if value.Elem().Kind() == reflect.Uint8 {
			return _BYTES_TO_POSTGRES
		}
		return _JSON_TO_POSTGRES
	case reflect.Map, reflect.Struct, reflect.Array:
		return _JSON_TO_POSTGRES
	default:
		return _INTERFACE_TO_POSTGRES
	}
}
