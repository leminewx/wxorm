package assetpath

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGetDir 验证数据目录定位到 harness/testdata
func TestGetDir(t *testing.T) {
	dir := GetDir()
	if !filepath.IsAbs(dir) {
		// 源码位置推断可用时应得到绝对路径；兜底路径只会在极端情况下出现
		t.Logf("GetDir 返回相对路径 %q（回退模式）", dir)
	}

	if _, err := os.Stat(filepath.Join(dir, "database.json")); err != nil {
		t.Fatalf("GetDir 未定位到数据库配置文件: %v", err)
	}
}

// TestResolve 验证正常相对路径与越界路径的处理
func TestResolve(t *testing.T) {
	if got := Resolve("database.json"); got == "" {
		t.Fatal("正常相对路径不应返回空")
	}

	for _, name := range []string{"", "..", "../go.mod", "a/../../go.mod"} {
		if got := Resolve(name); got != "" {
			t.Errorf("Resolve(%q) = %q, want 空字符串", name, got)
		}
	}
}
