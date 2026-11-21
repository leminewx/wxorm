/*
Package integration 用真实数据库验证 wxorm 的 SQL 生成、结果映射与事务语义。

该包位于独立模块（harness/integration/go.mod），因为根模块坚持零第三方依赖，
而真实驱动不可避免要引入第三方库。根模块的单元测试继续使用
harness/examples/fakedriver，不依赖任何外部服务。

连接信息优先读环境变量，未设置时读 harness/testdata/database.json：

	WXORM_IT_MYSQL_DSN    MySQL DSN
	WXORM_IT_PG_DSN       PostgreSQL DSN
	WXORM_IT_SQLITE_FILE  SQLite 数据文件；相对路径基于 harness/testdata
	WXORM_IT_DB           仅运行指定方言，可选 mysql / postgres / sqlite（逗号分隔）

连接不上时默认跳过该方言（便于无数据库的环境开发），设置 WXORM_IT_STRICT=1
可把连接失败改为测试失败，用于 CI。
*/
package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitee.com/leminewx/wxorm"
	"gitee.com/leminewx/wxorm/harness/database"
	"gitee.com/leminewx/wxorm/statement/where"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// target 描述一个待验证的数据库连接
type target struct {
	// Name 是子测试名，也是 WXORM_IT_DB 的过滤值
	Name string
	// Driver 是注册到 database/sql 的驱动名，与 wxorm 的方言名一致
	Driver string
	// DSN 是数据源；SQLite 默认使用独立临时文件，也可指向 testdata 下的真库
	DSN string
}

// dsnOf 读取环境变量，未设置时使用配置值
func dsnOf(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	return fallback
}

// targets 返回本次需要验证的全部目标
func targets(t testing.TB) []target {
	t.Helper()

	config, err := database.Load()
	if err != nil {
		t.Fatalf("读取测试数据失败: %v", err)
	}
	all := []target{
		{
			Name:   "mysql",
			Driver: "mysql",
			DSN:    dsnOf("WXORM_IT_MYSQL_DSN", database.BuildMySQLDSN(config.MySQL)),
		},
		{
			Name:   "postgres",
			Driver: "pgx",
			DSN:    dsnOf("WXORM_IT_PG_DSN", database.BuildPostgresDSN(config.Postgres)),
		},
		{
			Name:   "sqlite",
			Driver: "sqlite",
			DSN:    sqliteDSN(t, config),
		},
	}

	filter := strings.TrimSpace(os.Getenv("WXORM_IT_DB"))
	if filter == "" {
		return all
	}

	wanted := make(map[string]bool)
	for _, name := range strings.Split(filter, ",") {
		wanted[strings.ToLower(strings.TrimSpace(name))] = true
	}

	var picked []target
	for _, tc := range all {
		if wanted[tc.Name] {
			picked = append(picked, tc)
		}
	}

	if len(picked) == 0 {
		t.Fatalf("WXORM_IT_DB=%q 没有匹配任何方言", filter)
	}

	return picked
}

// sqliteDSN 生成 SQLite DSN；配置了 testdata 下的真库时直接使用该文件
func sqliteDSN(t testing.TB, config database.Config) string {
	t.Helper()

	if override := strings.TrimSpace(os.Getenv("WXORM_IT_SQLITE_FILE")); override != "" {
		config.SQLite.File = override
	}

	file := database.ResolveSQLite(config)
	if file == "" {
		file = filepath.Join(t.TempDir(), "wxorm_it.db")
	}

	return "file:" + filepath.ToSlash(file) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
}

// openEngine 建立引擎；连接失败时按 WXORM_IT_STRICT 决定跳过还是失败
func openEngine(t testing.TB, tc target) *wxorm.Engine {
	t.Helper()

	engine, err := wxorm.NewEngine(tc.Driver, tc.DSN)
	if err != nil {
		if strings.TrimSpace(os.Getenv("WXORM_IT_STRICT")) == "1" {
			t.Fatalf("连接 %s 失败: %v", tc.Name, err)
		}

		t.Skipf("跳过 %s：%v（设置 WXORM_IT_STRICT=1 可改为失败）", tc.Name, err)
	}

	engine.SetSessionTimeout(15 * time.Second)
	t.Cleanup(func() { _ = engine.Close() })
	return engine
}

// forEachDialect 按方言拉起子测试，每个子测试独立引擎
func forEachDialect(t *testing.T, fn func(t *testing.T, tc target, engine *wxorm.Engine)) {
	t.Helper()

	for _, tc := range targets(t) {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			engine := openEngine(t, tc)
			fn(t, tc, engine)
		})
	}
}

// dropTable 删除数据表，忽略不存在的情况
func dropTable(t testing.TB, engine *wxorm.Engine, table string) {
	t.Helper()

	sess := engine.NewSession()
	defer func() { _ = sess.Close() }()

	if _, err := sess.Exec("DROP TABLE IF EXISTS " + table); err != nil {
		t.Fatalf("删除数据表 %s 失败: %v", table, err)
	}
}

// execSQL 执行辅助 SQL（建索引、插入种子数据等）
func execSQL(t testing.TB, engine *wxorm.Engine, query string, args ...any) {
	t.Helper()

	sess := engine.NewSession()
	defer func() { _ = sess.Close() }()

	if _, err := sess.Exec(query, args...); err != nil {
		t.Fatalf("执行 SQL 失败: %v\nSQL: %s", err, query)
	}
}

// createTable 先删表再建表，保证每次用例从干净状态开始
func createTable(t testing.TB, engine *wxorm.Engine, table string, model any) {
	t.Helper()

	dropTable(t, engine, table)

	sess := engine.NewSession(model)
	defer func() { _ = sess.Close() }()

	if err := sess.CreateTable(); err != nil {
		t.Fatalf("创建数据表 %s 失败: %v", table, err)
	}
}

// mustCount 统计满足条件的行数；cond 为 nil 时统计全表
func mustCount(t testing.TB, engine *wxorm.Engine, model any, cond where.Condition) int64 {
	t.Helper()

	sess := engine.NewSession(model)
	defer func() { _ = sess.Close() }()

	if cond != nil {
		sess.WHERE(cond)
	}

	count, _, err := sess.COUNT()
	if err != nil {
		t.Fatalf("统计行数失败: %v", err)
	}

	return count
}

// tableName 生成带前缀的数据表名，避免与业务表冲突
func tableName(suffix string) string {
	return "wxorm_it_" + suffix
}

// summarize 在失败信息里附带执行过的 SQL，便于定位方言差异
func summarize(res interface {
	GetExecutionSQL() string
	GetExecutionArgs() []any
}) string {
	if res == nil {
		return "<nil>"
	}

	return fmt.Sprintf("SQL: %s\nARGS: %v", res.GetExecutionSQL(), res.GetExecutionArgs())
}
