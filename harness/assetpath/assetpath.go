/*
Package assetpath 定位 harness/testdata 目录，供测试与示例复用。

go test 以包目录为工作目录，go run 以调用目录为工作目录，两者并不一致，
因此先按源码位置推断，再沿工作目录逐级向上查找 harness/testdata。
*/
package assetpath

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// _FALLBACK_DIR 为所有候选都不可用时的兜底相对路径。
const _FALLBACK_DIR = "harness/testdata"

// GetDir 返回测试数据目录。
//
// 查找顺序：源码位置推断 → 工作目录逐级向上查找 harness/testdata 或 testdata；
// 全部不可用时返回兜底路径，由调用方在读取文件时报错。
func GetDir() string {
	if _, file, _, ok := runtime.Caller(0); ok {
		guessed := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "testdata"))
		if isDir(guessed) {
			return guessed
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		return _FALLBACK_DIR
	}

	for dir := wd; ; dir = filepath.Dir(dir) {
		for _, candidate := range []string{
			filepath.Join(dir, "harness", "testdata"),
			filepath.Join(dir, "testdata"),
		} {
			if isDir(candidate) {
				return candidate
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}

	return _FALLBACK_DIR
}

// Resolve 把相对路径映射到测试数据目录内。
//
// 空路径与越界路径（含 ".."）返回空字符串，避免调用方读到目录外的文件。
func Resolve(relative string) string {
	root := GetDir()
	target := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(strings.TrimSpace(relative), "/")))

	inner, err := filepath.Rel(root, target)
	if err != nil || inner == "." || inner == ".." || strings.HasPrefix(inner, ".."+string(filepath.Separator)) {
		return ""
	}

	return target
}

// isDir 判断路径是否存在且为目录。
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
