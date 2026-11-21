package datatype

import (
	"fmt"
	"math/big"
	"reflect"
	"testing"
	"time"
)

func TestMySQL_ConvertGoDatatypeToMySQLDatatype(t *testing.T) {
	fmt.Println("int            ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(123)))
	fmt.Println("int8           ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(int8(123))))
	fmt.Println("int16          ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(int16(123))))
	fmt.Println("int32          ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(int32(123))))
	fmt.Println("int64          ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(int64(123))))

	fmt.Println("uint           ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(uint(123))))
	fmt.Println("uint8          ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(uint8(123))))
	fmt.Println("uint16         ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(uint16(123))))
	fmt.Println("uint32         ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(uint32(123))))
	fmt.Println("uint64         ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(uint64(123))))

	fmt.Println("float32        ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(1.23)))
	fmt.Println("float64        ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(float32(1.23))))
	fmt.Println("*big.Float      ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(big.NewFloat(1.23))))

	fmt.Println("bool           ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(true)))
	fmt.Println("bool           ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(false)))

	fmt.Println("rune           ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf('a')))
	fmt.Println("byte           ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(byte(1))))
	fmt.Println("string         ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf("username")))
	fmt.Println("[]byte         ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf([]byte("username"))))
	fmt.Println("[]string       ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf([]string{"a", "b", "c"})))
	fmt.Println("map[string]int ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(map[string]int{"a": 1, "b": 2, "c": 3})))

	fmt.Println("time.Time      ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(time.Now())))
	fmt.Println("time.Duration  ===>>>", ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(time.Second)))
}

// 测试 time.Duration 的映射与 Go 侧语义一致
//
// time.Duration 底层是 int64 纳秒，映射成 TIMESTAMP 会把时长当成时间点存，
// 写进去的值无法原样读回。
func TestMySQL_DurationMapping(t *testing.T) {
	if got := ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(time.Duration(0))); got != _DURATION_TO_MYSQL {
		t.Fatalf("unexpected duration datatype: %s", got)
	}

	if _DURATION_TO_MYSQL != "BIGINT" {
		t.Errorf("duration should be stored as BIGINT (int64 nanoseconds), got %s", _DURATION_TO_MYSQL)
	}
}

func BenchmarkConvertGoDatatypeToMySQLDatatype(b *testing.B) {
	b.Run("time.Time", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(time.Now()))

		}
	})

	b.Run("time.Duration", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(time.Second))

		}
	})

	b.Run("int", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(123))
		}
	})

	b.Run("uint", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(uint(123)))
		}
	})

	b.Run("float", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(1.23))
		}
	})

	b.Run("string", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf("username"))
		}
	})

	b.Run("bool", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(true))
		}
	})

	b.Run("[]byte", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf([]byte("username")))
		}
	})

	b.Run("[]string", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf([]string{"a", "b", "c"}))
		}
	})

	b.Run("map[string]int", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			ConvertGoDatatypeToMySQLDatatype(reflect.TypeOf(map[string]int{"a": 1, "b": 2, "c": 3}))
		}
	})
}
