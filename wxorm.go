package wxorm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/naming"
	"gitee.com/leminewx/wxorm/session"
)

// 版本信息，供启动标识与诊断输出使用
//
// version 自 v1.0.0.0 起为正式发布版：公开 API 冻结，后续修复只做新增、不做破坏性变更。
var (
	year    = "2026"
	version = "v1.0.0.0"
	release = "2026-11-22"
)

// Logger 是 session.Logger 的别名，便于调用方直接在根包实现
type Logger = session.Logger

// QueryEvent 是 session.QueryEvent 的别名
type QueryEvent = session.QueryEvent

// Engine ORM 引擎
type Engine struct {
	// mu 保护下列可变配置：setter 与 NewSession 在不同 goroutine 调用时不加锁会产生数据竞争。
	// 连接与方言在创建后不再变化，不需要保护。
	mu             sync.RWMutex
	debug          bool
	debugColor     bool
	dbname         string
	sessionID      uint64
	sessionTimeout time.Duration
	slowThreshold  time.Duration
	logArgs        bool // 慢 SQL 日志是否输出参数原值（默认隐藏）
	naming         naming.Strategy
	logger         Logger
	db             *sql.DB
	stmts          *sync.Map
	stmtCache      *session.StatementCache // 查询类 SQL 的预编译缓存
	pool           *sync.Pool
	dialect        dialect.Dialect
}

// NewEngine 创建一个 ORM 引擎（默认会话查询超时10s）
func NewEngine(driverName, dbSource string) (*Engine, error) {
	// 先确认方言已注册，避免建好连接后才发现不支持
	dial, err := dialect.Lookup(driverName)
	if err != nil {
		return nil, err
	}

	// 解析数据库信息
	dbName, err := extractDBName(driverName, dbSource)
	if err != nil {
		return nil, err
	}

	// 打开数据库连接
	db, err := sql.Open(driverName, dbSource)
	if err != nil {
		return nil, err
	}

	// 检查数据库连接是否活跃；失败时释放连接池，避免遗留后台连接
	if err = db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	// 预编译缓存与连接池共用同一实例，保证池中会话取出的预编译语句与引擎一致
	stmts := &sync.Map{}

	engine := &Engine{
		dbname:         dbName,
		sessionTimeout: time.Second * 10,
		logger:         session.NewDefaultLogger(),
		db:             db,
		stmts:          stmts,
		stmtCache:      session.NewStatementCache(session.DefaultStatementCacheSize),
		dialect:        dial,
	}

	// 空闲会话池：会话归还前已重置，取用时只需重新下发会话级配置
	engine.pool = &sync.Pool{New: func() any {
		return session.NewSession(0, dbName, db, dial, stmts)
	}}

	return engine, nil
}

// extractDBName 从数据源中提取数据库名
func extractDBName(driverName, dbSource string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(driverName)) {
	case "mysql":
		// root:root@tcp(127.0.0.1:3306)/test?charset=utf8mb4&parseTime=True&loc=Local
		// 取最后一个 / 之后、? 之前的部分，避免空库名被当成合法的数据库名称
		idx := strings.LastIndex(dbSource, "/")
		if idx < 0 {
			return "", errors.New("wxorm: invalid MySQL data source")
		}

		dbPart := strings.Split(dbSource[idx+1:], "?")[0]
		if dbPart == "" {
			return "", errors.New("wxorm: MySQL data source has empty database name")
		}

		return dbPart, nil
	case "postgres", "postgresql", "pgx":
		return extractPostgresDBName(dbSource)
	case "sqlite", "sqlite3":
		return extractSQLiteDBName(dbSource)
	default:
		return "", fmt.Errorf("wxorm: unsupported database driver: %s", driverName)
	}
}

/*
extractPostgresDBName 从 PostgreSQL 数据源中提取数据库名

支持两种 DSN 形式：
  - URL：postgres://user:pass@host:5432/dbname?sslmode=disable
  - 关键字：host=127.0.0.1 port=5432 user=postgres dbname=wxorm sslmode=disable

两处都允许省略库名：PostgreSQL 会退化为使用用户名同名的库。
*/
func extractPostgresDBName(dbSource string) (string, error) {
	source := strings.TrimSpace(dbSource)
	if source == "" {
		return "", errors.New("wxorm: PostgreSQL data source is empty")
	}

	// URL 形式
	if strings.HasPrefix(source, "postgres://") || strings.HasPrefix(source, "postgresql://") {
		parsed, err := url.Parse(source)
		if err != nil {
			return "", fmt.Errorf("wxorm: invalid PostgreSQL data source: %w", err)
		}

		return strings.TrimPrefix(parsed.Path, "/"), nil
	}

	// 关键字形式，dbname 可以出现在任意位置，且可能被引号包裹
	for _, field := range strings.Fields(source) {
		key, value, ok := strings.Cut(field, "=")
		if !ok || !strings.EqualFold(key, "dbname") {
			continue
		}

		return strings.Trim(value, `'"`), nil
	}

	return "", nil
}

/*
extractSQLiteDBName 从 SQLite 数据源中提取库名（文件名）

支持的形式：
  - 普通文件：data/app.db
  - URI：file:app.db?cache=shared&mode=rwc
  - 内存库：:memory: 或 file::memory:?cache=shared

SQLite 没有「库」的层级，一个文件就是一个数据库，这里取文件名只用于日志与诊断；
建表与存在性查询不会用到它。
*/
func extractSQLiteDBName(dbSource string) (string, error) {
	source := strings.TrimSpace(dbSource)
	if source == "" {
		return "", errors.New("wxorm: SQLite data source is empty")
	}

	source = strings.TrimPrefix(source, "file:")
	if idx := strings.IndexByte(source, '?'); idx >= 0 {
		source = source[:idx]
	}

	if source == "" {
		return "", errors.New("wxorm: SQLite data source has empty database name")
	}

	// 同时兼容 Windows 与 Unix 的分隔符
	if idx := strings.LastIndexAny(source, `/\`); idx >= 0 {
		source = source[idx+1:]
	}

	return source, nil
}

// SetDebug 开启调试模式
func (own *Engine) SetDebug(color bool) {
	own.mu.Lock()
	defer own.mu.Unlock()

	own.debug = true
	own.debugColor = color
}

// SetSessionTimeout 设置执行 SQL 语句的超时时间
//
// 0 表示不设置默认超时（仍可配合 Session.SetContext 控制），负数忽略。
func (own *Engine) SetSessionTimeout(timeout time.Duration) {
	if timeout < 0 {
		return
	}

	own.mu.Lock()
	defer own.mu.Unlock()

	own.sessionTimeout = timeout
}

// SetLogger 设置 SQL 观测日志器
//
// 传入 nil 恢复默认日志器；同一日志器会被该引擎创建的所有会话共用，需保证并发安全。
func (own *Engine) SetLogger(logger Logger) *Engine {
	if logger == nil {
		logger = session.NewDefaultLogger()
	}

	own.mu.Lock()
	defer own.mu.Unlock()

	own.logger = logger
	return own
}

// SetSlowThreshold 设置慢 SQL 阈值
//
// 执行耗时达到阈值时交给日志器的慢查询事件；传入 0 或负数表示关闭检测。
func (own *Engine) SetSlowThreshold(threshold time.Duration) *Engine {
	own.mu.Lock()
	defer own.mu.Unlock()

	own.slowThreshold = threshold
	return own
}

// SetLogParamValues 控制慢 SQL 日志是否输出参数原值
//
// 默认关闭：参数中的手机号、身份证、密码等敏感信息不应随慢 SQL 日志长期留存。
// 调试模式（SetDebug）不受本开关影响，始终输出参数。
func (own *Engine) SetLogParamValues(enabled bool) *Engine {
	own.mu.Lock()
	defer own.mu.Unlock()

	own.logArgs = enabled
	return own
}

/*
SetStatementCacheSize 设置查询类 SQL 的预编译缓存容量

传入 0 或负数关闭查询缓存；默认容量为 session.DefaultStatementCacheSize。
缓存由引擎创建并被所有会话共享，应在开始使用引擎前设置：
运行中替换后，已取出的会话仍持有旧缓存指针。
*/
func (own *Engine) SetStatementCacheSize(size int) *Engine {
	own.mu.Lock()
	defer own.mu.Unlock()

	if own.stmtCache != nil {
		own.stmtCache.Clear()
	}

	if size <= 0 {
		own.stmtCache = nil
		return own
	}

	own.stmtCache = session.NewStatementCache(size)
	return own
}

/*
SetNaming 设置表名命名策略（库/Schema、前缀、后缀）

策略在下发到会话时生效，可以让同一个模型在不同引擎上使用不同前缀；
策略不合法时不会立即报错，由会话的 GetSchema 返回，避免在初始化期吞错。
*/
func (own *Engine) SetNaming(strategy naming.Strategy) *Engine {
	own.mu.Lock()
	defer own.mu.Unlock()

	own.naming = strategy
	return own
}

// GetNaming 获取表名命名策略
func (own *Engine) GetNaming() naming.Strategy {
	own.mu.RLock()
	defer own.mu.RUnlock()

	return own.naming
}

// SetMaxIdleConnections 设置空闲连接池中的最大连接数
func (own *Engine) SetMaxIdleConnections(num int) {
	own.db.SetMaxIdleConns(num)
}

// SetMaxOpenConnections 设置数据库的最大打开连接数
func (own *Engine) SetMaxOpenConnections(num int) {
	own.db.SetMaxOpenConns(num)
}

// SetConnMaxIdleTime 设置连接处于空闲状态的最长时间
func (own *Engine) SetConnMaxIdleTime(duration time.Duration) {
	own.db.SetConnMaxIdleTime(duration)
}

// SetConnMaxLifetime 设置可以重复使用连接的最长时间
func (own *Engine) SetConnMaxLifetime(duration time.Duration) {
	own.db.SetConnMaxLifetime(duration)
}

// GetStatus 获取数据库状态
func (own *Engine) GetStatus() sql.DBStats {
	return own.db.Stats()
}

// NewSession 新建一个会话
func (own *Engine) NewSession(model ...any) *session.Session {
	// 生成 Session ID（原子自增，溢出后回绕到 1）
	id := atomic.AddUint64(&own.sessionID, 1)
	if id == 0 {
		id = atomic.AddUint64(&own.sessionID, 1)
	}

	// 创建 Session：优先复用池中已重置的会话
	sess := own.pool.Get().(*session.Session)
	sess.SetID(id)
	sess.SetReleaser(own.recycleSession)

	// 一次性取配置快照：避免逐字段读取时与 setter 交错，也避免持有读锁创建会话
	own.mu.RLock()
	timeout, logger := own.sessionTimeout, own.logger
	slowThreshold, logArgs := own.slowThreshold, own.logArgs
	stmtCache, strategy := own.stmtCache, own.naming
	debug, debugColor := own.debug, own.debugColor
	own.mu.RUnlock()

	sess.SetTimeout(timeout)
	sess.SetLogger(logger).SetSlowThreshold(slowThreshold).SetLogParamValues(logArgs)
	sess.SetStatementCache(stmtCache)

	// 先下发命名策略，再解析模型：物理表名在 SetSchema 时计算，两者顺序可交换，
	// 但先设策略可以避免先算一次逻辑表名再覆盖
	sess.SetNaming(strategy)

	// 设置模型
	if len(model) > 0 && model[0] != nil {
		sess.UseModel(model[0])
	}

	// 调试模式
	if debug {
		sess.SetDebug(debugColor)
	}

	return sess
}

// recycleSession 把已重置的会话放回池中
func (own *Engine) recycleSession(sess *session.Session) {
	own.pool.Put(sess)
}

// TxFunc 定义事务的回调函数
type TxFunc func(sess *session.Session) (any, error)

/*
PanicError 保留事务回调中 panic 的原始值与调用栈

用 fmt.Errorf("%v", p) 转换会把 panic 变成普通错误：类型、栈与 errors.Is 链全部丢失，
线上只能看到一行 "runtime error: ..."，无法定位到具体代码。
*/
type PanicError struct {
	Value any    // panic 的原始值
	Stack []byte // panic 发生时的调用栈
}

// Error 实现 error 接口
func (own *PanicError) Error() string {
	return fmt.Sprintf("wxorm: transaction panicked: %v", own.Value)
}

// Unwrap 让 errors.Is/errors.As 能穿透到 panic 中的原始错误
func (own *PanicError) Unwrap() error {
	err, _ := own.Value.(error)
	return err
}

// Transact 执行事务中的 SQL 语句，若没有发生错误，则自动提交
func (own *Engine) Transact(txFunc TxFunc) (any, error) {
	return own.TransactContext(context.Background(), txFunc)
}

// TransactContext 在给定上下文中执行事务，若没有发生错误，则自动提交
//
// 上下文会传递给事务及其中每条 SQL：客户端断开或请求超时会触发回滚，
// 不会出现「调用方已放弃、事务仍在后台占用连接」的情况。
func (own *Engine) TransactContext(ctx context.Context, txFunc TxFunc) (res any, err error) {
	if txFunc == nil {
		return nil, nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// 新建会话，并开始事务（会话池化后必须归还，否则无法复用）
	sess := own.NewSession()
	sess.SetContext(ctx)
	defer func() { _ = sess.Close() }()

	if err := sess.BeginTx(ctx, nil); err != nil {
		return nil, err
	}

	// 回滚或提交事务
	defer func() {
		if p := recover(); p != nil {
			// 调用异常，回滚事务，并保留 panic 值与栈
			_ = sess.Rollback()
			err = &PanicError{Value: p, Stack: debug.Stack()}
		} else if err != nil {
			// 执行失败，回滚事务
			_ = sess.Rollback()
		} else {
			// 提交事务
			err = sess.Commit()
		}
	}()

	// 执行事务
	return txFunc(sess)
}

// Prepare 预编译 SQL 语句
func (own *Engine) Prepare(key, query string) error {
	_, err := session.Prepare(own.db, own.stmts, key, query)
	return err
}

/*
Migrate 迁移数据表：列增补、缺失索引，并按选项处理已移除的列
  - ALTER TABLE <表名> ADD COLUMN <字段名> <数据类型>;
  - ALTER TABLE <表名> DROP COLUMN <字段名>;（仅在传入 WithDropMissingColumns 时执行）
  - CREATE INDEX <索引名> ON <表名> (<字段名>);

默认不删列：模型字段被删除、改名或仅改变大小写时，数据库中的对应列会出现在
DroppedColumns 中，但不会执行 DROP COLUMN——DDL 不可回滚，静默删列等于销毁真实数据。
确认要清理这些列时显式传入 WithDropMissingColumns()。

列类型不一致只记录日志并跳过（详情见 DiffTable），因无法判断数据能否无损转换；
注意：MySQL 的 DDL 会隐式提交事务，因此迁移不使用事务。
*/
func (own *Engine) Migrate(value any, options ...MigrateOption) error {
	if value == nil {
		return nil
	}

	settings := migrateOptions{}
	for _, option := range options {
		if option != nil {
			option(&settings)
		}
	}

	// DDL 会使缓存的执行计划失效，迁移结束后统一清空
	defer own.clearStatementCache()

	sess := own.NewSession(value)
	defer func() { _ = sess.Close() }()

	// 先对比结构：差异既用于执行 DDL，也用于提示无法自动处理的变化
	diff, err := own.diffTable(sess)
	if err != nil {
		return err
	}

	if !diff.Exists {
		return sess.CreateTable()
	}

	table, err := sess.GetSchema()
	if err != nil {
		return err
	}

	if len(diff.AddedColumns) > 0 || len(diff.DroppedColumns) > 0 || len(diff.MissingIndexes) > 0 {
		own.logDebug(sess.GetID(), "Migrate table '%s': added cols %v, deleted cols %v, missing indexes %v",
			diff.Table, diff.AddedColumns, diff.DroppedColumns, diff.MissingIndexes)
	}

	// 类型不一致无法保证无损转换，只提示不执行
	logUnhandledDiff(diff)

	// 添加字段：复用列定义，保证迁移后的默认值、注释与非空约束与建表一致
	for _, name := range diff.AddedColumns {
		col, err := table.GetColumnByName(name)
		if err != nil {
			return fmt.Errorf("wxorm: failed to read column %s: %w", name, err)
		}

		ctx := clause.NewContext()
		if err = col.BuildCreateSQL(ctx); err != nil {
			return fmt.Errorf("wxorm: failed to build column %s: %w", name, err)
		}

		definition, _ := ctx.GetResult()
		sqlStr := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;", own.dialect.QuoteIdentifier(sess.GetQualifiedTableName()), definition)
		if _, err = sess.ExecDDL(sqlStr); err != nil {
			return fmt.Errorf("wxorm: failed to add column %s: %w", name, err)
		}
	}

	// 删除字段：默认只报告不执行，避免模型变更变成不可回滚的数据销毁
	if len(diff.DroppedColumns) > 0 {
		if !settings.dropMissingColumns {
			log.Printf("wxorm: table %s has %d column(s) missing in model, skipped dropping: %v (pass WithDropMissingColumns() to drop them)",
				diff.Table, len(diff.DroppedColumns), diff.DroppedColumns)
		} else {
			// 逐列 DROP COLUMN，避免重建整表丢失主键、索引与自增值
			for _, name := range diff.DroppedColumns {
				sqlStr := fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s;", own.dialect.QuoteIdentifier(sess.GetQualifiedTableName()), own.dialect.QuoteIdentifier(name))
				if _, err = sess.ExecDDL(sqlStr); err != nil {
					return fmt.Errorf("wxorm: failed to drop column %s: %w", name, err)
				}
			}
		}
	}

	// 补建模型声明但数据库中缺失的索引
	return own.createMissingIndexes(sess, table, diff.MissingIndexes)
}

// diffStrings 返回 a - b，列名按大小写不敏感比较
//
// 各方言对未加引号标识符的折叠规则不同（MySQL 列名不区分大小写，PostgreSQL 会折叠为小写），
// 若按精确字符串比较，模型里改一个字母的大小写就会被判成「删一列 + 加一列」，
// 配合 DROP COLUMN 会直接丢数据。这里统一按小写匹配，输出仍保留数据库中的原始列名。
func diffStrings(a []string, b []string) (diff []string) {
	mapB := make(map[string]bool, len(b))
	for _, v := range b {
		mapB[strings.ToLower(v)] = true
	}

	for _, v := range a {
		if _, ok := mapB[strings.ToLower(v)]; !ok {
			diff = append(diff, v)
		}
	}

	return
}

// Close 关闭引擎
//
// 关闭后引擎不可再使用：重复调用是幂等的，新建的会话会因数据库已关闭而报错。
func (own *Engine) Close() error {
	own.mu.RLock()
	stmtCache := own.stmtCache
	own.mu.RUnlock()

	// 关闭查询语句缓存
	if stmtCache != nil {
		stmtCache.Clear()
	}

	// 关闭并移除所有预编译语句：只关闭不删除会让关闭后的引擎继续交出已关闭的语句
	if own.stmts != nil {
		own.stmts.Range(func(key, value any) bool {
			_ = value.(*session.Stmt).Close()
			own.stmts.Delete(key)
			return true
		})
	}

	// 关闭数据库
	if own.db != nil {
		return own.db.Close()
	}

	return nil
}

// clearStatementCache 清空查询类语句缓存
//
// 缓存指针可能在运行期被 SetStatementCacheSize 替换，读写都必须经过锁。
func (own *Engine) clearStatementCache() {
	own.mu.RLock()
	cache := own.stmtCache
	own.mu.RUnlock()

	if cache != nil {
		cache.Clear()
	}
}

func (own *Engine) logDebug(sessionID uint64, msg string, args ...any) {
	own.mu.RLock()
	debug := own.debug
	own.mu.RUnlock()

	if !debug {
		return
	}

	msg = fmt.Sprintf("[DEBUG] [wxorm] [sid-%d] %s", sessionID, msg)
	if len(args) == 0 {
		log.Println(msg)
	} else {
		log.Printf(msg, args...)
	}
}
