package schema

import (
	"errors"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"gitee.com/leminewx/wxorm/dialect"
)

// TestGetBigKind 验证大数类型（值/指针）的识别
func TestGetBigKind(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
		want BigKind
	}{
		{"big.Int", reflect.TypeFor[big.Int](), BIG_INT},
		{"*big.Int", reflect.TypeFor[*big.Int](), BIG_INT},
		{"big.Float", reflect.TypeFor[big.Float](), BIG_FLOAT},
		{"*big.Float", reflect.TypeFor[*big.Float](), BIG_FLOAT},
		{"big.Rat", reflect.TypeFor[big.Rat](), BIG_NONE},
		{"string", reflect.TypeFor[string](), BIG_NONE},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := GetBigKind(tc.typ); got != tc.want {
				t.Fatalf("GetBigKind(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestEnsureBigSupported 验证 big.Rat 在解析阶段被拒绝
func TestEnsureBigSupported(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeFor[big.Rat](),
		reflect.TypeFor[*big.Rat](),
	} {
		err := EnsureBigSupported("rate", typ)
		if !errors.Is(err, ErrUnsupportedBigRat) {
			t.Fatalf("EnsureBigSupported(%s) = %v, want ErrUnsupportedBigRat", typ, err)
		}
		if !strings.Contains(err.Error(), "rate") {
			t.Fatalf("错误信息应包含字段名，got %v", err)
		}
	}

	for _, typ := range []reflect.Type{
		reflect.TypeFor[big.Int](),
		reflect.TypeFor[*big.Float](),
		reflect.TypeFor[string](),
	} {
		if err := EnsureBigSupported("amount", typ); err != nil {
			t.Fatalf("EnsureBigSupported(%s) 不应报错: %v", typ, err)
		}
	}
}

// TestFormatBigValue 验证大数写入格式化为十进制文本
func TestFormatBigValue(t *testing.T) {
	t.Run("big.Int", func(t *testing.T) {
		value, zero, isNull := FormatBigValue(reflect.ValueOf(*big.NewInt(-42)), BIG_INT)
		if value != "-42" || zero || isNull {
			t.Fatalf("got (%v, %v, %v), want (\"-42\", false, false)", value, zero, isNull)
		}

		value, zero, isNull = FormatBigValue(reflect.ValueOf(*new(big.Int)), BIG_INT)
		if value != "0" || !zero || isNull {
			t.Fatalf("零值 got (%v, %v, %v), want (\"0\", true, false)", value, zero, isNull)
		}
	})

	t.Run("指针", func(t *testing.T) {
		amount, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
		value, zero, isNull := FormatBigValue(reflect.ValueOf(amount), BIG_INT)
		if value != amount.String() || zero || isNull {
			t.Fatalf("got (%v, %v, %v), want (%q, false, false)", value, zero, isNull, amount.String())
		}

		// nil 指针表示 SQL NULL
		value, zero, isNull = FormatBigValue(reflect.ValueOf((*big.Int)(nil)), BIG_INT)
		if value != nil || !zero || !isNull {
			t.Fatalf("nil 指针 got (%v, %v, %v), want (nil, true, true)", value, zero, isNull)
		}
	})

	t.Run("big.Float", func(t *testing.T) {
		rate := big.NewFloat(1.5)
		value, zero, isNull := FormatBigValue(reflect.ValueOf(*rate), BIG_FLOAT)
		if value != rate.Text('f', bigFloatPrecision) || zero || isNull {
			t.Fatalf("got (%v, %v, %v), want (%q, false, false)", value, zero, isNull, rate.Text('f', bigFloatPrecision))
		}

		// 小数位固定对齐 DECIMAL(65,30)
		if text, _ := value.(string); !strings.Contains(text, ".") || len(text)-strings.Index(text, ".")-1 != bigFloatPrecision {
			t.Fatalf("小数位应为 %d，got %q", bigFloatPrecision, value)
		}
	})
}

// TestScanBigValue 验证数据库文本回填大数
func TestScanBigValue(t *testing.T) {
	t.Run("big.Int", func(t *testing.T) {
		var target *big.Int
		field := reflect.ValueOf(&target).Elem()

		const text = "123456789012345678901234567890"
		if err := ScanBigValue(field, BIG_INT, text, true); err != nil {
			t.Fatalf("扫描失败: %v", err)
		}
		if target == nil || target.String() != text {
			t.Fatalf("got %v, want %s", target, text)
		}

		// SQL NULL 与字面量 "null" 都回读为 nil
		for _, nullish := range []struct {
			text  string
			valid bool
		}{{"", false}, {"null", true}} {
			if err := ScanBigValue(field, BIG_INT, nullish.text, nullish.valid); err != nil {
				t.Fatalf("扫描 NULL 失败: %v", err)
			}
			if target != nil {
				t.Fatalf("NULL 应回读为 nil，got %v", target)
			}
		}

		if err := ScanBigValue(field, BIG_INT, "12x", true); !errors.Is(err, ErrBigValueMalformed) {
			t.Fatalf("非法文本应返回 ErrBigValueMalformed, got %v", err)
		}
	})

	t.Run("big.Float", func(t *testing.T) {
		var rate big.Float
		field := reflect.ValueOf(&rate).Elem()

		const text = "12345.678901234567890123456789"
		if err := ScanBigValue(field, BIG_FLOAT, text, true); err != nil {
			t.Fatalf("扫描失败: %v", err)
		}

		want, _, err := big.ParseFloat(text, 10, 256, big.ToNearestEven)
		if err != nil {
			t.Fatalf("构造期望值失败: %v", err)
		}
		if rate.Text('f', bigFloatPrecision) != want.Text('f', bigFloatPrecision) {
			t.Fatalf("got %s, want %s", rate.Text('f', bigFloatPrecision), want.Text('f', bigFloatPrecision))
		}

		if err := ScanBigValue(field, BIG_FLOAT, "abc", true); !errors.Is(err, ErrBigValueMalformed) {
			t.Fatalf("非法文本应返回 ErrBigValueMalformed, got %v", err)
		}
	})
}

// bigLedger 用于验证模型解析阶段的大数元数据
type bigLedger struct {
	ID     int64     `orm:"column(id) pk auto"`
	Name   string    `orm:"column(name)"`
	Amount *big.Int  `orm:"column(amount)"`
	Rate   big.Float `orm:"column(rate)"`
}

// GetTableName 返回大数测试表名
func (*bigLedger) GetTableName() string { return "big_ledger" }

// TestParseBigFields 验证 Parse 记录大数元数据
func TestParseBigFields(t *testing.T) {
	sch, err := Parse(dialect.Get("mysql"), &bigLedger{})
	if err != nil {
		t.Fatalf("解析模型失败: %v", err)
	}

	if !sch.HasBigFields {
		t.Fatal("含 big 字段的模型应标记 HasBigFields")
	}

	if len(sch.BigFieldKinds) != len(sch.Fields) {
		t.Fatalf("BigFieldKinds 长度 = %d, want %d", len(sch.BigFieldKinds), len(sch.Fields))
	}

	want := map[string]BigKind{"Amount": BIG_INT, "Rate": BIG_FLOAT}
	for i, field := range sch.Fields {
		if expected, ok := want[field.Name]; ok {
			if sch.BigFieldKinds[i] != expected {
				t.Fatalf("%s 的种类 = %v, want %v", field.Name, sch.BigFieldKinds[i], expected)
			}
			delete(want, field.Name)
		}
	}

	if len(want) != 0 {
		t.Fatalf("未找到大数字段: %v", want)
	}

	// 写入侧：大数值类型在 Extract 时被格式化为文本
	amount, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	model := &bigLedger{Name: "alice", Amount: amount, Rate: *big.NewFloat(1.5)}
	values := sch.ExtractUpdateNamesAndValues(reflect.ValueOf(model).Elem(), true)
	if values["amount"] != amount.String() {
		t.Fatalf("amount = %v, want %s", values["amount"], amount.String())
	}
	if text, ok := values["rate"].(string); !ok || text != big.NewFloat(1.5).Text('f', bigFloatPrecision) {
		t.Fatalf("rate = %v, want 文本形式", values["rate"])
	}
}

// TestParseBigRatRejected 验证含 big.Rat 的模型无法解析
func TestParseBigRatRejected(t *testing.T) {
	type unsupported struct {
		ID   int64    `orm:"column(id) pk auto"`
		Rate *big.Rat `orm:"column(rate)"`
	}

	if _, err := Parse(dialect.Get("mysql"), &unsupported{}); !errors.Is(err, ErrUnsupportedBigRat) {
		t.Fatalf("Parse 应返回 ErrUnsupportedBigRat, got %v", err)
	}
}
