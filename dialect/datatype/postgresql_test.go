package datatype

import (
	"math/big"
	"net"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestConvertGoDatatypeToPostgresDatatype(t *testing.T) {
	intDatatype := _INT_TO_POSTGRES_32
	uintDatatype := _UINT_TO_POSTGRES_32
	if strconv.IntSize == 64 {
		intDatatype = _INT_TO_POSTGRES_64
		uintDatatype = _UINT_TO_POSTGRES_64
	}

	tests := []struct {
		name     string
		value    any
		expected Datatype
	}{
		{"int", 1, intDatatype},
		{"int8", int8(1), _INT8_TO_POSTGRES},
		{"int16", int16(1), _INT16_TO_POSTGRES},
		{"int32", int32(1), _INT32_TO_POSTGRES},
		{"int64", int64(1), _INT64_TO_POSTGRES},
		{"uint", uint(1), uintDatatype},
		{"uint8", uint8(1), _UINT8_TO_POSTGRES},
		{"uint16", uint16(1), _UINT16_TO_POSTGRES},
		{"uint32", uint32(1), _UINT32_TO_POSTGRES},
		{"uint64", uint64(1), _UINT64_TO_POSTGRES},
		{"float32", float32(1), _FLOAT32_TO_POSTGRES},
		{"float64", float64(1), _FLOAT64_TO_POSTGRES},
		{"big.Float", big.Float{}, _BIG_FLOAT_TO_POSTGRES},
		{"bool", true, _BOOL_TO_POSTGRES},
		{"string", "wxorm", _STRING_TO_POSTGRES},
		{"[]byte", []byte("wxorm"), _BYTES_TO_POSTGRES},
		{"time.Time", time.Now(), _TIME_TO_POSTGRES},
		{"time.Duration", time.Second, _DURATION_TO_POSTGRES},
		{"net.IP", net.IPv4(127, 0, 0, 1), _IP_TO_POSTGRES},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := convertValue(tt.value); got != tt.expected {
				t.Errorf("ConvertGoDatatypeToPostgresDatatype(%T) = %q, want %q", tt.value, got, tt.expected)
			}
		})
	}
}

// int32（runes 的底层类型）必须映射为 INTEGER，
// 否则所有 int32 字段都会被建成 CHAR(1) 这样的单字符列
func TestConvertGoDatatypeToPostgresRuneIsInteger(t *testing.T) {
	if got := convertValue(rune('a')); got != _INT32_TO_POSTGRES {
		t.Errorf("rune 应映射为 %q，实际 %q", _INT32_TO_POSTGRES, got)
	}

	if got := convertValue(byte('a')); got != _UINT8_TO_POSTGRES {
		t.Errorf("byte 应映射为 %q，实际 %q", _UINT8_TO_POSTGRES, got)
	}
}

func TestConvertGoDatatypeToPostgresJSON(t *testing.T) {
	cases := []any{
		[]int{1, 2},
		[]string{"a"},
		map[string]any{"a": 1},
		struct{ Name string }{"wxorm"},
		[2]int{1, 2},
	}

	for _, value := range cases {
		if got := convertValue(value); got != _JSON_TO_POSTGRES {
			t.Errorf("%T 应映射为 %q，实际 %q", value, _JSON_TO_POSTGRES, got)
		}
	}
}

func TestConvertGoDatatypeToPostgresPointer(t *testing.T) {
	value := "wxorm"
	if got := convertValue(&value); got != _STRING_TO_POSTGRES {
		t.Errorf("*string 应映射为 %q，实际 %q", _STRING_TO_POSTGRES, got)
	}

	// 完全没有类型信息（nil reflect.Type）时降级为 JSONB，不能 panic
	if got := ConvertGoDatatypeToPostgresDatatype(nil); got != _INTERFACE_TO_POSTGRES {
		t.Errorf("nil 类型应降级为 %q，实际 %q", _INTERFACE_TO_POSTGRES, got)
	}
}

// convertValue 统一测试入口，避免每处都写 reflect.TypeFor
func convertValue(value any) Datatype {
	return ConvertGoDatatypeToPostgresDatatype(reflect.TypeOf(value))
}
