package datatype

import (
	"math/big"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestConvertGoDatatypeToSQLiteDatatype(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected Datatype
	}{
		{"int", 1, _INTEGER_TO_SQLITE},
		{"int8", int8(1), _INTEGER_TO_SQLITE},
		{"int16", int16(1), _INTEGER_TO_SQLITE},
		{"int32", int32(1), _INTEGER_TO_SQLITE},
		{"int64", int64(1), _INTEGER_TO_SQLITE},
		{"uint", uint(1), _INTEGER_TO_SQLITE},
		{"uint8", uint8(1), _INTEGER_TO_SQLITE},
		{"uint16", uint16(1), _INTEGER_TO_SQLITE},
		{"uint32", uint32(1), _INTEGER_TO_SQLITE},
		{"uint64", uint64(1), _INTEGER_TO_SQLITE},
		{"float32", float32(1), _REAL_TO_SQLITE},
		{"float64", float64(1), _REAL_TO_SQLITE},
		{"big.Float", big.Float{}, _BIG_TO_SQLITE},
		{"big.Int", big.Int{}, _BIG_TO_SQLITE},
		{"bool", true, _INTEGER_TO_SQLITE},
		{"string", "wxorm", _TEXT_TO_SQLITE},
		{"[]byte", []byte("wxorm"), _BLOB_TO_SQLITE},
		{"time.Time", time.Now(), _DATETIME_TO_SQLITE},
		{"time.Duration", time.Second, _INTEGER_TO_SQLITE},
		{"net.IP", net.IPv4(127, 0, 0, 1), _TEXT_TO_SQLITE},
		{"json slice", []int{1, 2}, _TEXT_TO_SQLITE},
		{"json map", map[string]any{"a": 1}, _TEXT_TO_SQLITE},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := convertSQLiteValue(tt.value); got != tt.expected {
				t.Errorf("ConvertGoDatatypeToSQLiteDatatype(%T) = %q, want %q", tt.value, got, tt.expected)
			}
		})
	}
}

// rune 与 byte 不能退化成 TEXT：它们是 int32/uint8，退化成文本会让数值比较与索引失效
func TestConvertGoDatatypeToSQLiteRune(t *testing.T) {
	if got := convertSQLiteValue(rune('a')); got != _INTEGER_TO_SQLITE {
		t.Errorf("rune 应映射为 %q，实际 %q", _INTEGER_TO_SQLITE, got)
	}

	if got := convertSQLiteValue(byte('a')); got != _INTEGER_TO_SQLITE {
		t.Errorf("byte 应映射为 %q，实际 %q", _INTEGER_TO_SQLITE, got)
	}
}

func TestConvertGoDatatypeToSQLitePointerAndNil(t *testing.T) {
	value := "wxorm"
	if got := convertSQLiteValue(&value); got != _TEXT_TO_SQLITE {
		t.Errorf("*string 应映射为 %q，实际 %q", _TEXT_TO_SQLITE, got)
	}

	// 完全没有类型信息（nil reflect.Type）时降级为 TEXT，不能 panic
	if got := ConvertGoDatatypeToSQLiteDatatype(nil); got != _TEXT_TO_SQLITE {
		t.Errorf("nil 类型应降级为 %q，实际 %q", _TEXT_TO_SQLITE, got)
	}
}

// convertSQLiteValue 统一测试入口，避免每处都写 reflect.TypeFor
func convertSQLiteValue(value any) Datatype {
	return ConvertGoDatatypeToSQLiteDatatype(reflect.TypeOf(value))
}
