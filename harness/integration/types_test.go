package integration

import (
	"bytes"
	"testing"
	"time"

	"gitee.com/leminewx/wxorm"
)

// TypeRow 覆盖三方言都支持的基础 Go 类型
//
// uint64 使用 MaxInt64 作为测试值，SQLite 的 INTEGER 为 8 字节有符号，
// 超过该范围属于已知限制（UNSIGNED BIGINT 溢出），不在本用例断言范围内。
type TypeRow struct {
	ID   int64     `orm:"column(id) pk auto"`
	I8   int8      `orm:"column(i8)"`
	I16  int16     `orm:"column(i16)"`
	I32  int32     `orm:"column(i32)"`
	I64  int64     `orm:"column(i64)"`
	U8   uint8     `orm:"column(u8)"`
	U16  uint16    `orm:"column(u16)"`
	U32  uint32    `orm:"column(u32)"`
	U64  uint64    `orm:"column(u64)"`
	F32  float32   `orm:"column(f32)"`
	F64  float64   `orm:"column(f64)"`
	Flag bool      `orm:"column(flag)"`
	Text string    `orm:"column(text)"`
	Blob []byte    `orm:"column(blob) null"`
	At   time.Time `orm:"column(at)"`
	// OptAt 验证可空时间列：nil 写 NULL，非 nil 往返时区与时间值
	OptAt *time.Time `orm:"column(opt_at) null"`
}

// GetTableName 返回类型测试表名
func (*TypeRow) GetTableName() string {
	return tableName("types")
}

// TestTypes 验证整数、浮点、布尔、字符串、二进制与时间的往返映射
func TestTypes(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("types"), &TypeRow{})

		at := time.Date(2026, 11, 20, 12, 34, 56, 0, time.UTC)
		want := &TypeRow{
			I8:    -8,
			I16:   -1600,
			I32:   -320000,
			I64:   -6400000000,
			U8:    200,
			U16:   60000,
			U32:   4000000000,
			U64:   9223372036854775807,
			F32:   1.5,
			F64:   2.25,
			Flag:  true,
			Text:  "wxorm-测试-🙂",
			Blob:  []byte{0x00, 0x0a, 0xff, 0x10},
			At:    at,
			OptAt: &at,
		}

		sess := engine.NewSession(&TypeRow{})
		if _, err := sess.INSERT(false, want); err != nil {
			t.Fatalf("插入类型数据失败: %v", err)
		}

		var got TypeRow
		if _, err := sess.FindFirst(&got); err != nil {
			t.Fatalf("查询类型数据失败: %v", err)
		}

		if got.I8 != want.I8 || got.I16 != want.I16 || got.I32 != want.I32 || got.I64 != want.I64 {
			t.Fatalf("有符号整型不符: %+v", got)
		}

		if got.U8 != want.U8 || got.U16 != want.U16 || got.U32 != want.U32 || got.U64 != want.U64 {
			t.Fatalf("无符号整型不符: %+v", got)
		}

		if got.F32 != want.F32 || got.F64 != want.F64 {
			t.Fatalf("浮点不符: %+v", got)
		}

		if got.Flag != want.Flag {
			t.Fatalf("布尔不符: %+v", got)
		}

		if got.Text != want.Text {
			t.Fatalf("字符串不符: got %q, want %q", got.Text, want.Text)
		}

		if !bytes.Equal(got.Blob, want.Blob) {
			t.Fatalf("二进制不符: got %v, want %v", got.Blob, want.Blob)
		}

		// MySQL 的 DATETIME 默认不保留小数秒，这里只比较到秒
		if !got.At.Equal(want.At) {
			t.Fatalf("时间不符: got %v, want %v", got.At, want.At)
		}

		if got.OptAt == nil || !got.OptAt.Equal(want.At) {
			t.Fatalf("可空时间不符: got %v, want %v", got.OptAt, want.At)
		}

		// 零值写回：bool 为 false、字符串为空、Blob 与可空时间为 NULL
		zero := &TypeRow{U8: 1, At: at}
		if _, err := sess.INSERT(false, zero); err != nil {
			t.Fatalf("插入零值数据失败: %v", err)
		}

		var gotZero []TypeRow
		if _, err := sess.ORDER("id").SELECT(&gotZero); err != nil {
			t.Fatalf("查询零值数据失败: %v", err)
		}

		if len(gotZero) != 2 || gotZero[1].Flag || gotZero[1].Text != "" || gotZero[1].Blob != nil || gotZero[1].OptAt != nil {
			t.Fatalf("零值写回不符: %+v", gotZero)
		}
	})
}
