package datatype

import (
	"math/big"
	"reflect"
	"strconv"
	"time"
)

const (
	_INT8_TO_MYSQL  Datatype = "TINYINT"  // int8
	_INT16_TO_MYSQL Datatype = "SMALLINT" // int16
	_INT32_TO_MYSQL Datatype = "INT"      // int32
	_INT64_TO_MYSQL Datatype = "BIGINT"   // int64

	_UINT8_TO_MYSQL  Datatype = "TINYINT UNSIGNED"  // uint8
	_UINT16_TO_MYSQL Datatype = "SMALLINT UNSIGNED" // uint16
	_UINT32_TO_MYSQL Datatype = "INT UNSIGNED"      // uint32
	_UINT64_TO_MYSQL Datatype = "BIGINT UNSIGNED"   // uint64

	_FLOAT32_TO_MYSQL   Datatype = "FLOAT"          // float32
	_FLOAT64_TO_MYSQL   Datatype = "DOUBLE"         // float64
	_BIG_INT_TO_MYSQL   Datatype = "DECIMAL(65,0)"  // *big.Int（十进制文本写入）
	_BIG_FLOAT_TO_MYSQL Datatype = "DECIMAL(65,30)" // *big.Float（固定 30 位小数文本写入）

	_STRING_TO_MYSQL    Datatype = "VARCHAR(255)" // string（不定长字符串<=65535）
	_INTERFACE_TO_MYSQL Datatype = "LONGTEXT"     // interface（长文本）

	_TIME_TO_MYSQL     Datatype = "DATETIME" // time.Time（2006-01-02 15:04:05）
	_DURATION_TO_MYSQL Datatype = "BIGINT"   // time.Duration（Go 中就是 int64 纳秒，存时间点会丢语义且读不回）

	_BOOL_TO_MYSQL  Datatype = "TINYINT(1)" // bool
	_BYTES_TO_MYSQL Datatype = "BLOB"       // []byte
	_MAP_TO_MYSQL   Datatype = "JSON"       // map[string]int, map[string]string, ...
)

/*
ConvertGoDatatypeToMySQLDatatype 将 Go 中的数据类型转换为 MySQL 数据库中的数据类型

注意：Go 的 byte/rune 只是 uint8/int32 的别名，运行时无法区分，
因此这里统一按 Kind 映射（byte → TINYINT UNSIGNED，rune → INT），
不对 rune 单独走 CHAR(1)，否则所有 int32 字段都会被建成单字符列。
*/
func ConvertGoDatatypeToMySQLDatatype(value reflect.Type) Datatype {
	// 处理指针类型，获取其指向的实际类型
	for value != nil && value.Kind() == reflect.Ptr {
		value = value.Elem()
	}

	if value == nil {
		return _INTERFACE_TO_MYSQL
	}

	// 首先检查具名类型
	switch value {
	case reflect.TypeFor[time.Time]():
		return _TIME_TO_MYSQL
	case reflect.TypeFor[time.Duration]():
		return _DURATION_TO_MYSQL
	case reflect.TypeFor[big.Int]():
		return _BIG_INT_TO_MYSQL
	case reflect.TypeFor[big.Float]():
		return _BIG_FLOAT_TO_MYSQL
	case reflect.TypeFor[big.Rat]():
		// big.Rat 会在 schema.Parse 阶段被拒绝，这里保留映射仅用于诊断输出
		return _BIG_FLOAT_TO_MYSQL
	case reflect.TypeFor[[]byte]():
		return _BYTES_TO_MYSQL
	}

	// 然后检查 reflect.Kind
	switch value.Kind() {
	case reflect.Int:
		// 根据平台决定 int 大小
		if strconv.IntSize == 64 {
			return _INT64_TO_MYSQL
		}
		return _INT32_TO_MYSQL
	case reflect.Int8:
		return _INT8_TO_MYSQL
	case reflect.Int16:
		return _INT16_TO_MYSQL
	case reflect.Int32:
		return _INT32_TO_MYSQL
	case reflect.Int64:
		return _INT64_TO_MYSQL
	case reflect.Uint:
		// 根据平台决定 uint 大小
		if strconv.IntSize == 64 {
			return _UINT64_TO_MYSQL
		}
		return _UINT32_TO_MYSQL
	case reflect.Uint8:
		return _UINT8_TO_MYSQL
	case reflect.Uint16:
		return _UINT16_TO_MYSQL
	case reflect.Uint32:
		return _UINT32_TO_MYSQL
	case reflect.Uint64:
		return _UINT64_TO_MYSQL
	case reflect.Float32:
		return _FLOAT32_TO_MYSQL
	case reflect.Float64:
		return _FLOAT64_TO_MYSQL
	case reflect.Bool:
		return _BOOL_TO_MYSQL
	case reflect.String:
		return _STRING_TO_MYSQL
	case reflect.Slice:
		// 除了 []byte（转为 BLOB），其他切片类型也转为 JSON
		if value.Elem().Kind() == reflect.Uint8 {
			return _BYTES_TO_MYSQL
		}
		return _MAP_TO_MYSQL
	case reflect.Map, reflect.Struct, reflect.Array:
		return _MAP_TO_MYSQL
	case reflect.Interface:
		return _INTERFACE_TO_MYSQL
	default:
		// 无法映射的类型（chan、func、complex 等）退化为长文本，
		// 不在解析模型时 panic，避免一个异常字段拖垮整个服务启动
		return _INTERFACE_TO_MYSQL
	}
}
