package integration

import (
	"testing"
	"time"

	"gitee.com/leminewx/wxorm"
	"gitee.com/leminewx/wxorm/session"
)

// 本文件是第二轮系统性审计的回归用例，每项对应一个已修复缺陷。
// 用例只在 harness/integration（真库）运行：连接自锁、会话超时与分表
// 都依赖驱动的连接池与临时表语义，内存假驱动无法证明。

// fixHookModel 在 AFTER_SELECT 钩子里再发起一次查询
type fixHookModel struct {
	ID   int64  `orm:"column(id) pk auto"`
	Name string `orm:"column(name)"`
}

func (*fixHookModel) GetTableName() string { return tableName("fix_hook") }

// CallAfterSelect 用当前会话直接发起底层查询
//
// 修复前 rows 尚未关闭，单连接池下这条查询要等第二条连接，必然自锁。
func (*fixHookModel) CallAfterSelect(sess *session.Session) error {
	var one int
	return sess.QueryRow("SELECT 1").Scan(&one)
}

// TestFix_AfterSelectHookWithoutExtraConnection 验证 AFTER_SELECT 钩子不再占用连接
func TestFix_AfterSelectHookWithoutExtraConnection(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("fix_hook"), &fixHookModel{})

		sess := engine.NewSession(&fixHookModel{})
		if _, err := sess.INSERT(false, &fixHookModel{Name: "a"}); err != nil {
			t.Fatalf("插入种子数据失败: %v", err)
		}

		// 缩到 1 条连接：查询期间再发起查询必然等待第二条连接
		engine.SetMaxOpenConnections(1)
		engine.SetMaxIdleConnections(1)
		engine.SetSessionTimeout(3 * time.Second)
		defer engine.SetSessionTimeout(15 * time.Second)

		var row fixHookModel
		sess = engine.NewSession(&fixHookModel{})
		if _, err := sess.FindFirst(&row); err != nil {
			t.Fatalf("AFTER_SELECT 钩子内查询失败（连接自锁）: %v", err)
		}

		if row.Name != "a" {
			t.Errorf("unexpected row: %+v", row)
		}
	})
}

// TestFix_PreloadWithSingleConnection 验证单连接池下预加载不再自锁
//
// 修复前 FindFirst + PRELOAD 稳定在会话超时处失败（默认 10s）。
func TestFix_PreloadWithSingleConnection(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createPreloadTables(t, engine)

		engine.SetMaxOpenConnections(1)
		engine.SetMaxIdleConnections(1)
		engine.SetSessionTimeout(3 * time.Second)
		defer engine.SetSessionTimeout(15 * time.Second)

		sess := engine.NewSession(&PreloadBook{})
		sess.PRELOAD("Author").ORDER("id")

		var book PreloadBook
		if _, err := sess.FindFirst(&book); err != nil {
			t.Fatalf("单连接池下单行预加载失败: %v", err)
		}

		if book.Author == nil || book.Author.Name == "" {
			t.Errorf("预加载未挂载 author: %+v", book.Author)
		}

		sess = engine.NewSession(&PreloadBook{})
		sess.PRELOAD("Comments").ORDER("id")

		var books []PreloadBook
		if _, err := sess.SELECT(&books); err != nil {
			t.Fatalf("单连接池下切片预加载失败: %v", err)
		}

		if len(books) != 3 {
			t.Fatalf("期望 3 本图书，实际 %d", len(books))
		}
	})
}

// TestFix_LimitZero LIMIT(0) 必须真正返回零行，且能与 OFFSET 组合
func TestFix_LimitZero(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createPreloadTables(t, engine)

		sess := engine.NewSession(&PreloadBook{})

		var books []PreloadBook
		res, err := sess.LIMIT(0).SELECT(&books)
		if err != nil {
			t.Fatalf("LIMIT 0 应当合法: %v\n%s", err, summarize(res))
		}

		if len(books) != 0 {
			t.Errorf("LIMIT 0 应当返回 0 行，实际 %d 行", len(books))
		}

		sess = engine.NewSession(&PreloadBook{})

		books = nil
		if _, err = sess.LIMIT(0).OFFSET(1).SELECT(&books); err != nil {
			t.Fatalf("LIMIT 0 OFFSET 1 不应生成非法 SQL: %v", err)
		}

		if len(books) != 0 {
			t.Errorf("LIMIT 0 OFFSET 1 应当返回 0 行，实际 %d 行", len(books))
		}
	})
}

// fixGhostModel 指向一张刻意不存在的表
type fixGhostModel struct {
	ID   int64  `orm:"column(id) pk auto"`
	Name string `orm:"column(name)"`
}

func (*fixGhostModel) GetTableName() string { return tableName("fix_ghost") }

// TestFix_ErrorReturnsNilResult 执行失败时不再返回非 nil 的结果对象
func TestFix_ErrorReturnsNilResult(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		dropTable(t, engine, tableName("fix_ghost"))

		sess := engine.NewSession(&fixGhostModel{})
		res, err := sess.DELETE()
		if err == nil {
			t.Fatal("删除不存在的表应当报错")
		}

		if res != nil {
			t.Errorf("错误路径应返回 nil 结果，实际 %s", summarize(res))
		}
	})
}

// fixShardModel 按实例返回不同分表，用于验证 schema 缓存不会冻结表名
type fixShardModel struct {
	Partition string
	ID        int64  `orm:"column(id) pk auto"`
	Name      string `orm:"column(name)"`
}

func (own *fixShardModel) GetTableName() string {
	return tableName("fix_shard_" + own.Partition)
}

// TestFix_ShardTableNameNotFrozen 同一模型的两个分片必须落到各自的表
func TestFix_ShardTableNameNotFrozen(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("fix_shard_2024"), &fixShardModel{Partition: "2024"})
		createTable(t, engine, tableName("fix_shard_2025"), &fixShardModel{Partition: "2025"})

		for _, item := range []*fixShardModel{
			{Partition: "2024", Name: "a"},
			{Partition: "2025", Name: "b"},
		} {
			sess := engine.NewSession(item)
			if _, err := sess.INSERT(false, item); err != nil {
				t.Fatalf("写入分片 %s 失败: %v", item.Partition, err)
			}
		}

		var rows2024 []fixShardModel
		sess := engine.NewSession(&fixShardModel{Partition: "2024"})
		if _, err := sess.SELECT(&rows2024); err != nil {
			t.Fatalf("查询 2024 分片失败: %v", err)
		}

		if len(rows2024) != 1 || rows2024[0].Name != "a" {
			t.Errorf("2024 分片数据错误: %+v", rows2024)
		}

		var rows2025 []fixShardModel
		sess = engine.NewSession(&fixShardModel{Partition: "2025"})
		if _, err := sess.SELECT(&rows2025); err != nil {
			t.Fatalf("查询 2025 分片失败: %v", err)
		}

		if len(rows2025) != 1 || rows2025[0].Name != "b" {
			t.Errorf("2025 分片数据错误: %+v", rows2025)
		}
	})
}

// fixDDLModel 是迁移前的表结构
type fixDDLModel struct {
	ID   int64  `orm:"column(id) pk auto"`
	Name string `orm:"column(name)"`
}

func (*fixDDLModel) GetTableName() string { return tableName("fix_ddl") }

// fixDDLModelV2 比 fixDDLModel 多一列，用于触发 ADD COLUMN
type fixDDLModelV2 struct {
	ID   int64  `orm:"column(id) pk auto"`
	Name string `orm:"column(name)"`
	Age  int    `orm:"column(age)"`
}

func (*fixDDLModelV2) GetTableName() string { return tableName("fix_ddl") }

// TestFix_DDLIgnoresSessionTimeout 建表与迁移不能被查询超时打断
//
// 修复前 CreateTable / Migrate 走 Session.Exec，叠加了默认 10s 超时；
// MySQL 的 DDL 不能回滚，超时中断会留下半成品表结构。
func TestFix_DDLIgnoresSessionTimeout(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		dropTable(t, engine, tableName("fix_ddl"))

		// 1ns 超时：任何仍叠加会话超时的语句都会立刻失败
		engine.SetSessionTimeout(time.Nanosecond)
		defer engine.SetSessionTimeout(15 * time.Second)

		sess := engine.NewSession(&fixDDLModel{})
		if err := sess.CreateTable(); err != nil {
			t.Fatalf("建表不应受会话超时约束: %v", err)
		}

		if err := sess.Close(); err != nil {
			t.Fatalf("关闭会话失败: %v", err)
		}

		if err := engine.Migrate(&fixDDLModelV2{}); err != nil {
			t.Fatalf("迁移（ADD COLUMN）不应受会话超时约束: %v", err)
		}

		engine.SetSessionTimeout(15 * time.Second)

		sess = engine.NewSession(&fixDDLModelV2{})
		if _, err := sess.INSERT(false, &fixDDLModelV2{Name: "x", Age: 18}); err != nil {
			t.Fatalf("迁移后写入新增列失败: %v", err)
		}

		var row fixDDLModelV2
		sess = engine.NewSession(&fixDDLModelV2{})
		if _, err := sess.FindFirst(&row); err != nil {
			t.Fatalf("迁移后查询失败: %v", err)
		}

		if row.Age != 18 {
			t.Errorf("新增列未生效，age = %d", row.Age)
		}
	})
}
