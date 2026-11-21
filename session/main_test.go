package session

import (
	"os"
	"testing"

	"gitee.com/leminewx/wxorm/harness/examples/fakedriver"
	"gitee.com/leminewx/wxorm/harness/examples/stubdriver"
)

// TestMain 注册内存驱动与脚本化行驱动，使本包测试不再依赖真实 MySQL 服务
func TestMain(m *testing.M) {
	fakedriver.Register()
	stubdriver.Register()
	os.Exit(m.Run())
}
