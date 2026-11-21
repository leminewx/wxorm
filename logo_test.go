package wxorm

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

// createLogoOutput 捕获一次 ShowLogo 的完整输出。
//
// ShowLogo 直接写 os.Stdout，因此替换标准输出后再恢复；
// 管道缓冲区远大于标识体积，不存在写阻塞风险。
func createLogoOutput(t *testing.T) string {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("创建管道失败: %v", err)
	}

	os.Stdout = writer
	showErr := ShowLogo()
	os.Stdout = original

	if err = writer.Close(); err != nil {
		t.Fatalf("关闭管道写入端失败: %v", err)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("读取标识输出失败: %v", err)
	}

	if err = reader.Close(); err != nil {
		t.Fatalf("关闭管道读取端失败: %v", err)
	}

	if showErr != nil {
		t.Fatalf("ShowLogo 执行失败: %v", showErr)
	}

	return string(data)
}

// TestShowLogo 验证启动标识包含应用名、版本与发布信息。
func TestShowLogo(t *testing.T) {
	original := GetLogoStyle()
	defer SetLogoStyle(original)

	output := createLogoOutput(t)
	for _, want := range []string{"WxORM", version, release, "Copyright"} {
		if !strings.Contains(output, want) {
			t.Fatalf("标识输出缺少 %q:\n%s", want, output)
		}
	}
}

// TestWriteLogoStyles 验证每种样式的图案互不相同，且都带版本信息。
func TestWriteLogoStyles(t *testing.T) {
	cases := []struct {
		name  string
		style LogoStyle
		art   string
	}{
		{"classic", LOGO_STYLE_CLASSIC, `_/\_/`},
		{"pixel", LOGO_STYLE_PIXEL, "███"},
		{"shadow", LOGO_STYLE_SHADOW, "╔"},
		{"ascii", LOGO_STYLE_ASCII, "#   #"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 样式是进程级状态，子测试串行执行并在结束后还原
			original := GetLogoStyle()
			defer SetLogoStyle(original)

			SetLogoStyle(tc.style)

			var buf bytes.Buffer
			if err := WriteLogo(&buf); err != nil {
				t.Fatalf("%s 样式写入失败: %v", tc.name, err)
			}

			output := buf.String()
			for _, want := range []string{tc.art, "WxORM", version, release} {
				if !strings.Contains(output, want) {
					t.Fatalf("%s 样式输出缺少 %q:\n%s", tc.name, want, output)
				}
			}
		})
	}
}

// TestWriteLogoRejectsNilWriter 验证空 writer 返回明确错误，而不是静默丢弃输出。
func TestWriteLogoRejectsNilWriter(t *testing.T) {
	if err := WriteLogo(nil); err == nil {
		t.Fatal("空 writer 未返回错误")
	}
}

// TestSetLogoStyleRejectsOutOfRange 验证越界样式被忽略且不影响当前设置。
func TestSetLogoStyleRejectsOutOfRange(t *testing.T) {
	original := GetLogoStyle()
	defer SetLogoStyle(original)

	SetLogoStyle(LOGO_STYLE_ASCII)
	SetLogoStyle(LOGO_STYLE_ASCII + 1)
	SetLogoStyle(-1)

	if got := GetLogoStyle(); got != LOGO_STYLE_ASCII {
		t.Fatalf("越界样式被接受: %v", got)
	}
}

// TestLogoConcurrentAccess 验证读写样式并发执行不产生数据竞争。
//
// 样式用 atomic.Int32 存储，写操作与 ShowLogo/WriteLogo 可以并行；
// 测试结束后恢复原样式，避免影响同包其他用例。
func TestLogoConcurrentAccess(t *testing.T) {
	original := GetLogoStyle()
	defer SetLogoStyle(original)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(style LogoStyle) {
			defer wg.Done()

			SetLogoStyle(style)
			var buf bytes.Buffer
			if err := WriteLogo(&buf); err != nil {
				t.Errorf("并发写入失败: %v", err)
			}
		}(LogoStyle(i % 4))
	}

	wg.Wait()
}
