package session

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/naming"
	"gitee.com/leminewx/wxorm/schema"
	"gitee.com/leminewx/wxorm/statement"
)

// Session 数据库的连接会话
type Session struct {
	id              uint64               // 会话ID
	debug           bool                 // 调试模式，输出一些打印
	debugColor      bool                 // 调式模式时，SQL 信息是否标记颜色
	dbname          string               // 数据库名称（仅记录连接信息，不参与元数据查询）
	timeout         time.Duration        // 执行 SQL 语句超时时间，0 表示不额外设置超时
	slowThreshold   time.Duration        // 慢 SQL 阈值，0 表示不检测
	ctx             context.Context      // 执行 SQL 语句使用的上下文
	model           any                  // 当前会话使用的数据模型
	modelErr        error                // 解析数据模型时的错误
	db              *sql.DB              // 数据库
	tx              *sql.Tx              // 数据库事务
	schema          *schema.Schema       // 数据表信息
	columns         []string             // 本次查询指定返回的列，空表示返回全部列
	joins           []statement.Join     // 本次查询的 JOIN 子句，查询结束后清空
	joinErr         error                // JOIN 参数的校验错误，在构建 SQL 时返回
	preloadSet      bool                 // 本次查询是否声明了 PRELOAD（区分「未声明」与「声明全部」）
	preloadAll      bool                 // PRELOAD 不传参数，加载全部关联
	preloads        []string             // PRELOAD 指定的模型字段名
	preloadErr      error                // PRELOAD 参数的校验错误
	limit           int                  // 本次查询的 LIMIT 值，用于预分配结果集容量
	overwrite       bool                 // UPDATE 结构体时是否写回零值字段（默认跳过）
	updateColumns   []string             // UPDATE_COLUMNS 白名单，nil 表示未指定，非 nil 空切片表示误用
	logArgs         bool                 // 慢 SQL 日志是否输出参数原值（默认隐藏）
	stmts           *sync.Map            // 缓存预编译的 SQL 语句
	stmtCache       *StatementCache      // 查询类 SQL 的预编译缓存，nil 表示不缓存
	statement       *statement.Statement // 用于构建 SQL 语句
	dialect         dialect.Dialect      // 数据库驱动接口，如：MySQL/PostgresSQL/SQLite等
	logger          Logger               // SQL 观测日志器
	naming          naming.Strategy      // 表名命名策略（库名/前缀/后缀）
	namingErr       error                // 命名策略校验错误
	table           string               // 物理表名（含前后缀，不含库名）
	tableSQL        string               // 物理表名（含库名），SQL 中使用
	insertKey       string               // INSERT 预编译缓存键
	insertIgnoreKey string               // INSERT IGNORE 预编译缓存键
	pooled          bool                 // 是否已归还会话池
	release         func(*Session)       // 归还回调，nil 表示不参与池化
}

// NewSession 新建一个连接会话（默认查询超时时间10s）
func NewSession(id uint64, dbname string, db *sql.DB, dial dialect.Dialect, stmts *sync.Map) *Session {
	// 允许多个会话共享预编译缓存，也允许传入 nil 表示不共享
	if stmts == nil {
		stmts = &sync.Map{}
	}

	return &Session{
		id:        id,
		dbname:    dbname,
		timeout:   time.Second * 10,
		ctx:       context.Background(),
		db:        db,
		stmts:     stmts,
		statement: statement.NewStatement(""),
		dialect:   dial,
		logger:    NewDefaultLogger(),
	}
}

// SetDebug 开启调试模式
func (own *Session) SetDebug(color bool) {
	own.debug = true
	own.debugColor = color
}

// SetTimeout 设置执行 SQL 语句的超时时间
//
// 传入 0 表示不额外设置超时（仍受 SetContext 传入的 deadline 约束），
// 适合短连接、本地库或希望自行控制超时的场景；负数忽略。
func (own *Session) SetTimeout(timeout time.Duration) {
	if timeout >= 0 {
		own.timeout = timeout
	}
}

// SetStatementCache 设置查询类 SQL 的预编译缓存（由引擎注入）
//
// 传入 nil 关闭查询缓存；缓存由引擎创建并共享，会话重置时保留。
func (own *Session) SetStatementCache(cache *StatementCache) *Session {
	own.stmtCache = cache
	return own
}

// ClearStatementCache 关闭并清空本会话使用的查询语句缓存
//
// 缓存由引擎创建并共享，本方法会影响同一引擎下的所有会话。
// 执行 DDL 或手工改表后调用；会话未启用缓存时无操作。
func (own *Session) ClearStatementCache() {
	if own.stmtCache != nil {
		own.stmtCache.Clear()
	}
}

// SetLogger 设置 SQL 观测日志器
//
// 传入 nil 恢复默认日志器；同一日志器会被该引擎创建的所有会话共用，需保证并发安全。
func (own *Session) SetLogger(logger Logger) *Session {
	if logger == nil {
		logger = NewDefaultLogger()
	}

	own.logger = logger
	return own
}

// SetSlowThreshold 设置慢 SQL 阈值
//
// 执行耗时达到阈值时交给日志器的慢 SQL 事件；传入 0 或负数表示关闭检测。
func (own *Session) SetSlowThreshold(threshold time.Duration) *Session {
	own.slowThreshold = threshold
	return own
}

// SetLogParamValues 控制慢 SQL 日志是否输出参数原值
//
// 默认关闭：慢 SQL 日志常被采集到集中式日志系统，参数中的手机号、身份证、
// 密码等会长期留存。需要定位具体数据时再显式开启。
// 调试模式不受本开关影响，始终输出参数。
func (own *Session) SetLogParamValues(enabled bool) *Session {
	own.logArgs = enabled
	return own
}

// execContext 返回本次数据库调用使用的上下文与取消函数
//
// 未设置超时时直接复用会话上下文，避免每次调用都创建计时器和额外的取消函数。
func (own *Session) execContext() (context.Context, context.CancelFunc) {
	ctx := own.GetContext()
	if own.timeout <= 0 {
		return ctx, func() {}
	}

	return context.WithTimeout(ctx, own.timeout)
}

// GetID 获取会话ID
func (own *Session) GetID() uint64 {
	return own.id
}

// SetContext 设置会话上下文
//
// 上下文会传递给每次数据库调用，用于传递请求级取消、超时与链路追踪信息；
// 传入 nil 时保持原有上下文不变。
func (own *Session) SetContext(ctx context.Context) *Session {
	if ctx != nil {
		own.ctx = ctx
	}

	return own
}

// GetContext 获取会话上下文
func (own *Session) GetContext() context.Context {
	if own.ctx == nil {
		return context.Background()
	}

	return own.ctx
}

var (
	_ BaseDatabase = (*sql.DB)(nil)
	_ BaseDatabase = (*sql.Tx)(nil)
)

// BaseDatabase 定义数据库操作的基本接口
type BaseDatabase interface {
	Exec(sql string, args ...any) (sql.Result, error)
	ExecContext(ctx context.Context, sql string, args ...any) (sql.Result, error)
	Query(sql string, args ...any) (*sql.Rows, error)
	QueryContext(ctx context.Context, sql string, args ...any) (*sql.Rows, error)
	QueryRow(sql string, args ...any) *sql.Row
	QueryRowContext(ctx context.Context, sql string, args ...any) *sql.Row
}

// GetDatabase 返回 *sql.DB 或 *sql.TX
func (own *Session) GetDatabase() BaseDatabase {
	if own.tx != nil {
		return own.tx
	}

	return own.db
}

// Exec 执行 SQL 语句
//
// 与 ExecContext 一样受会话上下文与超时约束；需要自行控制超时的场景请使用 ExecContext。
func (own *Session) Exec(sqlStr string, args ...any) (sql.Result, error) {
	ctx, cancel := own.execContext()
	defer cancel()

	return own.ExecContext(ctx, sqlStr, args...)
}

// ExecContext 执行带上下文的 SQL 语句
func (own *Session) ExecContext(ctx context.Context, sqlStr string, args ...any) (sql.Result, error) {
	start := time.Now()
	res, err := own.GetDatabase().ExecContext(ctx, sqlStr, args...)
	own.logQuery(time.Since(start), sqlStr, args...)

	return res, err
}

// ExecDDL 执行 DDL 语句（建表、改表、建索引、删表等）
//
// DDL 不叠加会话的查询超时：大表上的 ALTER TABLE 可能远超默认的 10s，
// 而 MySQL 的 DDL 不能回滚，超时中断会留下半成品表结构。
// 语句仍然继承会话上下文，需要中断控制时通过 SetContext 设置截止时间。
func (own *Session) ExecDDL(sqlStr string, args ...any) (sql.Result, error) {
	return own.ExecContext(own.GetContext(), sqlStr, args...)
}

// Query 执行 SQL 查询语句，返回多条结果
//
// 返回的 Rows 由调用方消费，生命周期不受本方法控制，因此不能再叠加会话超时的
// 计时器：无法在消费完成后 cancel 会泄漏 timer 与 context 节点，而提前 cancel 又会
// 让后续 Next() 直接报 context canceled。
// 需要单次查询超时时使用 QueryContext 并自行 defer cancel。
func (own *Session) Query(sqlStr string, args ...any) (*sql.Rows, error) {
	return own.QueryContext(own.GetContext(), sqlStr, args...)
}

// QueryContext 执行带上下文的 SQL 查询语句，返回多条结果
func (own *Session) QueryContext(ctx context.Context, sqlStr string, args ...any) (*sql.Rows, error) {
	start := time.Now()
	rows, err := own.GetDatabase().QueryContext(ctx, sqlStr, args...)
	own.logQuery(time.Since(start), sqlStr, args...)

	return rows, err
}

// QueryRow 执行 SQL 查询语句，返回单条结果
//
// 同 Query：Row 在调用方 Scan 时才真正执行，无法在返回前取消上下文，
// 因此不再叠加会话超时；需要单次超时时使用 QueryRowContext。
func (own *Session) QueryRow(sqlStr string, args ...any) *sql.Row {
	return own.QueryRowContext(own.GetContext(), sqlStr, args...)
}

// QueryRowContext 执行带上下文的 SQL 查询语句，返回单条结果
//
// Row 在调用方 Scan 时才真正执行，因此日志中的耗时仅为「发起查询」的耗时。
func (own *Session) QueryRowContext(ctx context.Context, sqlStr string, args ...any) *sql.Row {
	start := time.Now()
	row := own.GetDatabase().QueryRowContext(ctx, sqlStr, args...)
	own.logQuery(time.Since(start), sqlStr, args...)

	return row
}

// SetID 设置会话ID
//
// 会话从池中复用时由引擎重新分配，保证日志中的 sid 不被重复使用。
func (own *Session) SetID(id uint64) {
	own.id = id
}

// SetReleaser 设置会话归还回调（由引擎在取出会话时注入）
//
// 回调非空时会话参与池化：Close 会重置状态并归还，此后调用方不得再使用该会话。
func (own *Session) SetReleaser(release func(*Session)) {
	own.release = release
	own.pooled = false
}

/*
Reset 将会话恢复到可复用状态

清空模型、事务、语句与上下文等可变状态并释放其引用，
保留数据库连接、方言、预编译缓存等引擎级依赖。
会话池归还前会自动调用，手工复用时也可主动调用。
*/
func (own *Session) Reset() {
	own.id = 0
	own.debug = false
	own.debugColor = false
	own.timeout = 0
	own.slowThreshold = 0
	own.ctx = nil
	own.model = nil
	own.modelErr = nil
	own.tx = nil
	own.schema = nil
	own.columns = nil
	own.joins = nil
	own.joinErr = nil
	own.preloadSet = false
	own.preloadAll = false
	own.preloads = nil
	own.preloadErr = nil
	own.limit = 0
	own.overwrite = false
	own.updateColumns = nil
	own.naming = naming.Strategy{}
	own.namingErr = nil
	own.table = ""
	own.tableSQL = ""
	own.insertKey = ""
	own.insertIgnoreKey = ""
	own.release = nil

	if own.statement != nil {
		own.statement.Reset()
	}
}

// Close 关闭会话：回滚未提交事务，并将会话归还引擎的会话池
//
// 归还会清空会话状态，调用方不得再使用该会话；重复调用无副作用。
func (own *Session) Close() error {
	// 已归还的对象可能被其他调用方借走，重复关闭必须是无操作
	if own.pooled {
		return nil
	}

	var err error
	if own.tx != nil {
		err = own.tx.Rollback()
	}

	release := own.release
	own.Reset()
	if release != nil {
		own.pooled = true
		release(own)
	}

	return err
}

// logQuery 把本次执行交给日志器：仅调试模式上报全部语句，非调试模式只上报慢 SQL
func (own *Session) logQuery(elapsed time.Duration, query string, args ...any) {
	slow := own.slowThreshold > 0 && elapsed >= own.slowThreshold
	if !own.debug && !slow {
		return
	}

	logger := own.logger
	if logger == nil {
		logger = NewDefaultLogger()
	}

	logger.LogQuery(own.GetContext(), QueryEvent{
		SessionID: own.id,
		Elapsed:   elapsed,
		Query:     query,
		Args:      args,
		Debug:     own.debug,
		Slow:      slow,
		LogArgs:   own.logArgs,
		Color:     own.debugColor,
	})
}
