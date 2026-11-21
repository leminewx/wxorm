package wxorm

import (
	"os"
	"testing"

	"gitee.com/leminewx/wxorm/harness/examples/fakedriver"
)

// TestMain 注册内存驱动，使根包测试不再依赖真实 MySQL 服务
func TestMain(m *testing.M) {
	fakedriver.Register()
	os.Exit(m.Run())
}
