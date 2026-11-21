package schema

import (
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strings"
)

/*
大数类型的读写桥接

database/sql 的参数转换只接受基础类型与 driver.Valuer，*big.Int / *big.Float
解引用后是 struct，直接传给驱动会报 "unsupported type"，读取侧同样缺少 sql.Scanner。
因此框架在两端各做一次转换：

  - 写入：把大数格式化为十进制文本（数据库的 DECIMAL / NUMERIC / TEXT 都能接受字面量）；
  - 读取：把数据库返回的文本解析回大数，再写回模型字段。

big.Rat 是有理数，写成 NUMERIC 只能取有限小数（有损），写成文本又会失去数值语义，
因此解析阶段直接拒绝，避免"建表成功、写入静默失真"。

只有声明了这类字段的模型才会启用桥接，其余模型的扫描路径保持零额外分支。
*/

var (
	// ErrUnsupportedBigRat 表示模型使用了无法无损映射到数据库数值列的有理数类型
	ErrUnsupportedBigRat = errors.New("wxorm: big.Rat is not supported, use big.Float, string or a custom driver.Valuer/sql.Scanner type")

	// ErrBigValueMalformed 表示数据库返回的文本无法解析成大数
	ErrBigValueMalformed = errors.New("wxorm: malformed numeric text for big number")
)

// BigKind 描述一个大数字段需要在文本与 Go 大数之间桥接的种类
type BigKind uint8

const (
	BIG_NONE  BigKind = iota // 普通字段，无需桥接
	BIG_INT                  // *big.Int / big.Int
	BIG_FLOAT                // *big.Float / big.Float
)

// bigFloatPrecision 是 big.Float 写入数据库时固定保留的小数位
//
// 与 MySQL 的 DECIMAL(65,30) 对齐；PostgreSQL 的 NUMERIC 与 SQLite 的 TEXT
// 同样接受这一文本形式，跨方言读写保持一致。
const bigFloatPrecision = 30

// GetBigKind 返回字段类型（含指针）对应的大数桥接种类
func GetBigKind(typ reflect.Type) BigKind {
	for typ != nil && typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	if typ == nil {
		return BIG_NONE
	}

	switch typ {
	case reflect.TypeFor[big.Int]():
		return BIG_INT
	case reflect.TypeFor[big.Float]():
		return BIG_FLOAT
	default:
		return BIG_NONE
	}
}

// EnsureBigSupported 在解析模型阶段拒绝无法无损桥接的大数类型
//
// big.Rat 的分数形式无法写入 NUMERIC/DECIMAL，改写成小数又会丢精度，
// 在这里直接失败比等到首次写入时报错更安全。
func EnsureBigSupported(fieldName string, typ reflect.Type) error {
	for typ != nil && typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	if typ == reflect.TypeFor[big.Rat]() {
		return fmt.Errorf("%w (field %s)", ErrUnsupportedBigRat, fieldName)
	}

	return nil
}

// FormatBigValue 把大数字段格式化为可写入数据库的文本
//
// 第三个返回值表示该字段是否为 SQL NULL（仅指针为 nil 时为 true）；
// 第二个返回值沿用零值语义，供 UPDATE 默认跳过零值字段使用。
func FormatBigValue(field reflect.Value, kind BigKind) (value any, zero bool, isNull bool) {
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			return nil, true, true
		}

		field = field.Elem()
	}

	switch kind {
	case BIG_INT:
		value := field.Interface().(big.Int)
		if value.Sign() == 0 {
			return "0", true, false
		}

		return value.String(), false, false

	case BIG_FLOAT:
		value := field.Interface().(big.Float)
		if value.Sign() == 0 {
			return "0", true, false
		}

		return value.Text('f', bigFloatPrecision), false, false

	default:
		return field.Interface(), false, false
	}
}

// ScanBigValue 把数据库返回的文本写回大数字段
//
// valid 为 false 表示该列是 SQL NULL：指针字段置 nil，值字段清零。
func ScanBigValue(field reflect.Value, kind BigKind, text string, valid bool) error {
	if !valid {
		field.Set(reflect.Zero(field.Type()))
		return nil
	}

	text = strings.TrimSpace(text)
	if text == "" || strings.EqualFold(text, "null") {
		field.Set(reflect.Zero(field.Type()))
		return nil
	}

	switch kind {
	case BIG_INT:
		value, ok := new(big.Int).SetString(text, 10)
		if !ok {
			return fmt.Errorf("%w: %q cannot be parsed as big.Int", ErrBigValueMalformed, text)
		}

		return setBigNumber(field, reflect.ValueOf(*value))

	case BIG_FLOAT:
		value, _, err := big.ParseFloat(text, 10, 256, big.ToNearestEven)
		if err != nil {
			return fmt.Errorf("%w: %q cannot be parsed as big.Float: %v", ErrBigValueMalformed, text, err)
		}

		return setBigNumber(field, reflect.ValueOf(*value))

	default:
		return nil
	}
}

// setBigNumber 把解析后的大数写入字段，必要时先为指针字段分配实例
func setBigNumber(field reflect.Value, value reflect.Value) error {
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			if !field.CanSet() {
				return fmt.Errorf("wxorm: big number field %s is not settable", field.Type())
			}

			field.Set(reflect.New(field.Type().Elem()))
		}

		field = field.Elem()
	}

	if !field.CanSet() {
		return fmt.Errorf("wxorm: big number field %s is not settable", field.Type())
	}

	field.Set(value)
	return nil
}
