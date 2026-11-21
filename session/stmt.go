package session

import (
	"container/list"
	"context"
	"database/sql"
	"sync"
	"time"
)

// DefaultStatementCacheSize 是查询语句缓存的默认容量
const DefaultStatementCacheSize = 256

// StatementCache 缓存查询类 SQL 的预编译语句
//
// 查询语句会随 WHERE/ORDER/LIMIT 的组合变化，键空间不可预估，因此不能像
// INSERT 那样使用无上限的 sync.Map；达到容量后按 LRU 淘汰最久未使用的语句。
// 缓存由引擎创建并共享，会话只持有引用，DDL 执行后需要整体清空。
//
// 淘汰时不立即关闭语句：使用方可能刚拿到指针尚未执行，立即 Close 会让它
// 收到与业务无关的 "sql: statement is closed"。这里改用引用计数 +
// 退役标记，最后一个使用者 Release 后才真正关闭。

type StatementCache struct {
	mu    sync.Mutex
	max   int
	items map[string]*list.Element // SQL → LRU 节点（值为 *cacheEntry）
	lru   *list.List               // 前端为最近使用，后端为待淘汰
}

// cacheEntry 是缓存节点保存的键值

type cacheEntry struct {
	query string
	stmt  *Stmt
}

// NewStatementCache 创建容量为 max 的语句缓存，max 小于 1 时按 1 处理
func NewStatementCache(max int) *StatementCache {
	if max < 1 {
		max = 1
	}

	return &StatementCache{
		max:   max,
		items: make(map[string]*list.Element, max),
		lru:   list.New(),
	}
}

/*
LoadOrPrepare 命中缓存时直接返回，未命中时调用 prepare 预编译并写入缓存

预编译发生在锁外，避免慢语句阻塞其他查询的命中；并发预编译同一 SQL 时
只保留先写入的语句，后到的语句立即关闭。

返回值已持有一个引用（acquire），调用方使用完毕后必须调用 Release()，
否则被淘汰的语句无法关闭，缓存容量会名存实亡。
*/
func (own *StatementCache) LoadOrPrepare(query string, prepare func() (*sql.Stmt, error)) (*Stmt, error) {
	own.mu.Lock()
	if elem, ok := own.items[query]; ok {
		entry := elem.Value.(*cacheEntry)
		if entry.stmt.acquire() {
			own.lru.MoveToFront(elem)
			own.mu.Unlock()
			return entry.stmt, nil
		}

		// 语句已退役：从索引中摘除，重新预编译
		own.lru.Remove(elem)
		delete(own.items, query)
	}
	own.mu.Unlock()

	raw, err := prepare()
	if err != nil {
		return nil, err
	}

	own.mu.Lock()
	defer own.mu.Unlock()

	// 预编译期间可能已有其他协程写入同一 SQL
	if elem, ok := own.items[query]; ok {
		entry := elem.Value.(*cacheEntry)
		if entry.stmt.acquire() {
			own.lru.MoveToFront(elem)
			_ = raw.Close()
			return entry.stmt, nil
		}

		own.lru.Remove(elem)
		delete(own.items, query)
	}

	stmt := &Stmt{Stmt: raw, query: query}
	stmt.acquire() // 调用方持有一个引用

	// 达到容量上限时淘汰最久未使用的一条
	if own.lru.Len() >= own.max {
		if oldest := own.lru.Back(); oldest != nil {
			entry := oldest.Value.(*cacheEntry)
			own.lru.Remove(oldest)
			delete(own.items, entry.query)
			entry.stmt.retire()
		}
	}

	own.items[query] = own.lru.PushFront(&cacheEntry{query: query, stmt: stmt})
	return stmt, nil
}

// Len 返回当前缓存的语句数量
func (own *StatementCache) Len() int {
	own.mu.Lock()
	defer own.mu.Unlock()

	return own.lru.Len()
}

// Clear 清空缓存：语句标记退役，没有使用方时立即关闭
func (own *StatementCache) Clear() {
	own.mu.Lock()
	entries := own.items
	own.items = make(map[string]*list.Element, own.max)
	own.lru.Init()
	own.mu.Unlock()

	for _, elem := range entries {
		elem.Value.(*cacheEntry).stmt.retire()
	}
}

// prepare 通过当前会话预编译 SQL 语句
func (own *Session) prepare(key, query string) (*Stmt, error) {
	return Prepare(own.db, own.stmts, key, query)
}

// Prepare 预编译 SQL 语句
func Prepare(db *sql.DB, stmts *sync.Map, key, query string) (*Stmt, error) {
	// 预编译
	srcStmt, err := db.Prepare(query)
	if err != nil {
		return nil, err
	}

	// 包装并缓存
	dstStmt := &Stmt{Stmt: srcStmt, query: query}
	if cache, ok := stmts.LoadOrStore(key, dstStmt); ok {
		_ = srcStmt.Close()
		return cache.(*Stmt), nil
	}

	return dstStmt, nil
}

// Stmt 包装 sql.Stmt，以适配本框架的 DEBUG 模式
//
// 除查询缓存外，INSERT 语句也用它包装，但只有缓存中的查询语句会走
// acquire/Release 引用计数，INSERT 语句的 refs 始终为 0。
type Stmt struct {
	*sql.Stmt // 继承 *sql.Stmt
	query     string

	mu      sync.Mutex
	refs    int  // 当前使用方数量
	retired bool // 已从缓存摘除，等待最后一个使用方释放
}

// acquire 增加一次使用引用；语句已退役时返回 false，调用方需重新预编译
func (own *Stmt) acquire() bool {
	own.mu.Lock()
	defer own.mu.Unlock()

	if own.retired {
		return false
	}

	own.refs++
	return true
}

// Release 释放 LoadOrPrepare 返回语句的引用
//
// 语句已退役且没有其他使用方时关闭底层语句；重复调用或未持有引用时无操作。
func (own *Stmt) Release() {
	own.mu.Lock()
	if own.refs == 0 {
		own.mu.Unlock()
		return
	}

	own.refs--
	last := own.refs == 0 && own.retired
	own.mu.Unlock()

	if last {
		_ = own.Stmt.Close()
	}
}

// retire 标记语句退役；没有使用方时立即关闭
func (own *Stmt) retire() {
	own.mu.Lock()
	if own.retired {
		own.mu.Unlock()
		return
	}

	own.retired = true
	last := own.refs == 0
	own.mu.Unlock()

	if last {
		_ = own.Stmt.Close()
	}
}

func (own *Stmt) Exec(id uint64, debug, debugColor bool, args ...any) (sql.Result, error) {
	own.logDebug(id, debug, debugColor, own.query, args...)
	return own.Stmt.Exec(args...)
}

func (own *Stmt) ExecContext(ctx context.Context, id uint64, debug, debugColor bool, args ...any) (sql.Result, error) {
	own.logDebug(id, debug, debugColor, own.query, args...)
	return own.Stmt.ExecContext(ctx, args...)
}

func (own *Stmt) Query(id uint64, debug, debugColor bool, args ...any) (*sql.Rows, error) {
	own.logDebug(id, debug, debugColor, own.query, args...)
	return own.Stmt.Query(args...)
}

func (own *Stmt) QueryContext(ctx context.Context, id uint64, debug, debugColor bool, args ...any) (*sql.Rows, error) {
	own.logDebug(id, debug, debugColor, own.query, args...)
	return own.Stmt.QueryContext(ctx, args...)
}

func (own *Stmt) QueryRow(id uint64, debug, debugColor bool, args ...any) *sql.Row {
	own.logDebug(id, debug, debugColor, own.query, args...)
	return own.Stmt.QueryRow(args...)
}

func (own *Stmt) QueryRowContext(ctx context.Context, id uint64, debug, debugColor bool, args ...any) *sql.Row {
	own.logDebug(id, debug, debugColor, own.query, args...)
	return own.Stmt.QueryRowContext(ctx, args...)
}

// preparedStmt 返回 query 对应的缓存语句；事务内或未启用缓存时返回 nil
//
// 非 nil 的返回值已持有一个引用，调用方必须在使用后调用 Release()。
func (own *Session) preparedStmt(query string) (*Stmt, error) {
	if own.tx != nil || own.stmtCache == nil {
		return nil, nil
	}

	return own.stmtCache.LoadOrPrepare(query, func() (*sql.Stmt, error) {
		return own.db.Prepare(query)
	})
}

// execPrepared 执行 SQL 并返回耗时：非事务场景优先复用预编译语句
func (own *Session) execPrepared(ctx context.Context, query string, args ...any) (sql.Result, time.Duration, error) {
	start := time.Now()
	stmt, err := own.preparedStmt(query)
	if err != nil {
		cost := time.Since(start)
		own.logQuery(cost, query, args...)
		return nil, cost, err
	}

	// 事务内或未启用缓存时直接执行
	if stmt == nil {
		res, err := own.GetDatabase().ExecContext(ctx, query, args...)
		cost := time.Since(start)
		own.logQuery(cost, query, args...)
		return res, cost, err
	}

	res, err := stmt.ExecContext(ctx, own.id, false, false, args...)
	stmt.Release()
	cost := time.Since(start)
	own.logQuery(cost, query, args...)
	return res, cost, err
}

// queryPrepared 执行查询并返回耗时：非事务场景优先复用预编译语句
func (own *Session) queryPrepared(ctx context.Context, query string, args ...any) (*sql.Rows, time.Duration, error) {
	start := time.Now()
	stmt, err := own.preparedStmt(query)
	if err != nil {
		cost := time.Since(start)
		own.logQuery(cost, query, args...)
		return nil, cost, err
	}

	// 事务内或未启用缓存时直接执行
	if stmt == nil {
		rows, err := own.GetDatabase().QueryContext(ctx, query, args...)
		cost := time.Since(start)
		own.logQuery(cost, query, args...)
		return rows, cost, err
	}

	rows, err := stmt.QueryContext(ctx, own.id, false, false, args...)
	stmt.Release()
	cost := time.Since(start)
	own.logQuery(cost, query, args...)
	return rows, cost, err
}

// queryRowPrepared 执行单行查询并返回发起耗时：非事务场景优先复用预编译语句
//
// 与 QueryRowContext 一致，Row 在调用方 Scan 时才真正执行，耗时仅覆盖发起查询。
func (own *Session) queryRowPrepared(ctx context.Context, query string, args ...any) (*sql.Row, time.Duration) {
	start := time.Now()
	stmt, err := own.preparedStmt(query)

	// 预编译失败时回退到普通查询，由 Scan 返回驱动错误
	if err != nil || stmt == nil {
		row := own.GetDatabase().QueryRowContext(ctx, query, args...)
		cost := time.Since(start)
		own.logQuery(cost, query, args...)
		return row, cost
	}

	row := stmt.QueryRowContext(ctx, own.id, false, false, args...)
	stmt.Release()
	cost := time.Since(start)
	own.logQuery(cost, query, args...)
	return row, cost
}

// logDebug 按需输出调试信息
//
// 保留 debug/debugColor 参数仅为兼容既有调用；会话内部已统一由 Logger 上报，
// 这里复用默认日志器，避免两套输出格式各自漂移。
func (own *Stmt) logDebug(id uint64, debug, debugColor bool, query string, args ...any) {
	if !debug {
		return
	}

	defaultLogger{}.logDebug(QueryEvent{
		SessionID: id,
		Query:     query,
		Args:      args,
		Debug:     true,
		Color:     debugColor,
	})
}
