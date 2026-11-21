package session

import (
	"database/sql"
	"sync"
	"testing"

	"gitee.com/leminewx/wxorm/harness/examples"
	"gitee.com/leminewx/wxorm/statement/where"
)

// TestStatementCache_LoadOrPrepare 验证缓存命中、容量淘汰与清空
func TestStatementCache_LoadOrPrepare(t *testing.T) {
	db, err := examples.SetupTestDB()
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	cache := NewStatementCache(2)
	prepare := func(query string) func() (*sql.Stmt, error) {
		return func() (*sql.Stmt, error) {
			return db.Prepare(query)
		}
	}

	q1 := "SELECT 1"
	first, err := cache.LoadOrPrepare(q1, prepare(q1))
	if err != nil {
		t.Fatalf("预编译失败: %v", err)
	}

	again, err := cache.LoadOrPrepare(q1, prepare(q1))
	if err != nil {
		t.Fatalf("缓存命中失败: %v", err)
	}

	if first != again {
		t.Fatal("相同 SQL 应复用同一条预编译语句")
	}

	if got := cache.Len(); got != 1 {
		t.Fatalf("首次加载后缓存数量 = %d, want 1", got)
	}

	// 达到容量后按 LRU 淘汰最久未使用的语句
	for _, query := range []string{"SELECT 2", "SELECT 3"} {
		if _, err := cache.LoadOrPrepare(query, prepare(query)); err != nil {
			t.Fatalf("预编译 %q 失败: %v", query, err)
		}
	}

	if got := cache.Len(); got != 2 {
		t.Fatalf("容量上限为 2，实际缓存 %d 条", got)
	}

	cache.mu.Lock()
	_, exists := cache.items[q1]
	cache.mu.Unlock()
	if exists {
		t.Fatal("超出容量后应淘汰最旧的语句")
	}

	cache.Clear()
	if got := cache.Len(); got != 0 {
		t.Fatalf("清空后缓存数量 = %d, want 0", got)
	}
}

// TestStatementCache_ConcurrentLoad 验证并发加载同一 SQL 只保留一条语句
func TestStatementCache_ConcurrentLoad(t *testing.T) {
	db, err := examples.SetupTestDB()
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	const workers = 16
	query := "SELECT 1"
	cache := NewStatementCache(8)

	stmts := make([]*Stmt, workers)
	errs := make([]error, workers)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			stmts[idx], errs[idx] = cache.LoadOrPrepare(query, func() (*sql.Stmt, error) {
				return db.Prepare(query)
			})
		}(i)
	}
	wg.Wait()

	for i := range stmts {
		if errs[i] != nil {
			t.Fatalf("第 %d 个协程预编译失败: %v", i, errs[i])
		}

		if stmts[i] != stmts[0] {
			t.Fatalf("并发加载同一 SQL 应返回同一条语句，第 %d 个不同", i)
		}
	}

	if got := cache.Len(); got != 1 {
		t.Fatalf("并发加载后缓存数量 = %d, want 1", got)
	}

	cache.Clear()
}

// TestSession_PreparedQueryCache 验证查询、统计、更新与删除都会写入并复用语句缓存
func TestSession_PreparedQueryCache(t *testing.T) {
	sess := setupGroupSession(t)
	defer teardownSession(sess)

	cache := NewStatementCache(8)
	sess.SetStatementCache(cache)

	id := testInsertGroup(sess, "研发部", "admin")

	var group examples.Group
	if _, err := sess.WHERE(where.EQ("id", id)).FindFirst(&group); err != nil {
		t.Fatalf("首次查询失败: %v", err)
	}

	afterSelect := cache.Len()
	if afterSelect == 0 {
		t.Fatal("查询后应写入语句缓存")
	}

	// 相同 SQL 再次查询不应新增缓存条目
	if _, err := sess.WHERE(where.EQ("id", id)).FindFirst(&group); err != nil {
		t.Fatalf("第二次查询失败: %v", err)
	}

	if got := cache.Len(); got != afterSelect {
		t.Fatalf("相同 SQL 不应新增缓存条目: got %d, want %d", got, afterSelect)
	}

	// COUNT / Exist / UPDATE / DELETE 均走同一缓存
	if _, _, err := sess.WHERE(where.EQ("id", id)).COUNT(); err != nil {
		t.Fatalf("COUNT 失败: %v", err)
	}

	if ok, err := sess.WHERE(where.EQ("id", id)).Exist(); err != nil || !ok {
		t.Fatalf("Exist 失败: exist=%v err=%v", ok, err)
	}

	group.Name = "市场部"
	if _, err := sess.UPDATE(&group); err != nil {
		t.Fatalf("UPDATE 失败: %v", err)
	}

	var updated examples.Group
	if _, err := sess.WHERE(where.EQ("id", id)).FindFirst(&updated); err != nil {
		t.Fatalf("更新后查询失败: %v", err)
	}

	if updated.Name != "市场部" {
		t.Fatalf("更新未生效: got %q, want 市场部", updated.Name)
	}

	if _, err := sess.DELETE(&updated); err != nil {
		t.Fatalf("DELETE 失败: %v", err)
	}

	if ok, err := sess.WHERE(where.EQ("id", id)).Exist(); err != nil || ok {
		t.Fatalf("删除后记录不应存在: exist=%v err=%v", ok, err)
	}
}

// TestSession_PreparedCacheBypassedInTx 验证事务内不复用绑定在 *sql.DB 上的语句
func TestSession_PreparedCacheBypassedInTx(t *testing.T) {
	sess := setupGroupSession(t)
	defer teardownSession(sess)

	cache := NewStatementCache(8)
	sess.SetStatementCache(cache)

	id := testInsertGroup(sess, "研发部", "admin")

	if err := sess.Begin(); err != nil {
		t.Fatalf("开启事务失败: %v", err)
	}

	var group examples.Group
	if _, err := sess.WHERE(where.EQ("id", id)).FindFirst(&group); err != nil {
		_ = sess.Rollback()
		t.Fatalf("事务内查询失败: %v", err)
	}

	if got := cache.Len(); got != 0 {
		_ = sess.Rollback()
		t.Fatalf("事务内不应写入查询缓存，实际 %d 条", got)
	}

	if err := sess.Rollback(); err != nil {
		t.Fatalf("回滚失败: %v", err)
	}
}

// TestSession_DDLClearsCache 验证建表与删表会清空共享的查询缓存
func TestSession_DDLClearsCache(t *testing.T) {
	sess := setupGroupSession(t)
	defer func() { _ = sess.db.Close() }()

	cache := NewStatementCache(8)
	sess.SetStatementCache(cache)

	id := testInsertGroup(sess, "研发部", "admin")

	var group examples.Group
	if _, err := sess.WHERE(where.EQ("id", id)).FindFirst(&group); err != nil {
		t.Fatalf("查询失败: %v", err)
	}

	if cache.Len() == 0 {
		t.Fatal("查询后应写入语句缓存")
	}

	if err := sess.DeleteTable(); err != nil {
		t.Fatalf("删表失败: %v", err)
	}

	if got := cache.Len(); got != 0 {
		t.Fatalf("DDL 后应清空缓存，实际 %d 条", got)
	}

	// 恢复测试表，供 teardownSession 清理
	if err := sess.CreateTable(); err != nil {
		t.Fatalf("重建测试表失败: %v", err)
	}

	_ = sess.Close()
}

// TestStatementCache_RetireWaitsForRelease 验证清空缓存不会关闭仍在使用的语句
func TestStatementCache_RetireWaitsForRelease(t *testing.T) {
	db, err := examples.SetupTestDB()
	if err != nil {
		t.Fatalf("连接测试库失败: %v", err)
	}
	defer func() { _ = db.Close() }()

	cache := NewStatementCache(4)

	// 假驱动要求 SELECT 带表名，先建一张临时表
	const table = "stmt_cache_retire"
	if _, err := db.Exec("CREATE TABLE `" + table + "` (`id` INT AUTO_INCREMENT, PRIMARY KEY (`id`))"); err != nil {
		t.Fatalf("创建测试表失败: %v", err)
	}
	defer func() { _, _ = db.Exec("DROP TABLE `" + table + "`") }()

	query := "SELECT id FROM " + table

	stmt, err := cache.LoadOrPrepare(query, func() (*sql.Stmt, error) {
		return db.Prepare(query)
	})
	if err != nil {
		t.Fatalf("预编译失败: %v", err)
	}

	// 模拟两个并发查询同时持有同一条语句
	stmt.acquire()

	cache.Clear()
	if got := cache.Len(); got != 0 {
		t.Fatalf("清空后缓存数量 = %d, want 0", got)
	}

	rows, err := stmt.Query(0, false, false)
	if err != nil {
		t.Fatalf("退役中的语句应仍可执行: %v", err)
	}
	_ = rows.Close()

	stmt.Release()
	rows, err = stmt.Query(0, false, false)
	if err != nil {
		t.Fatalf("仍有使用者时不应关闭语句: %v", err)
	}
	_ = rows.Close()

	// 最后一个引用归还后才真正关闭
	stmt.Release()
	if _, err := stmt.Query(0, false, false); err == nil {
		t.Fatal("引用归零后语句应被关闭")
	}

	// 退役语句不会命中缓存，重新加载应得到新语句
	fresh, err := cache.LoadOrPrepare(query, func() (*sql.Stmt, error) {
		return db.Prepare(query)
	})
	if err != nil {
		t.Fatalf("重新预编译失败: %v", err)
	}
	if fresh == stmt {
		t.Fatal("退役语句不应再次从缓存返回")
	}
	fresh.Release()
}
