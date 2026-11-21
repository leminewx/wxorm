package clause

import (
	"errors"
	"strings"
)

var (
	// ErrValuesIsEmpty 表示构建 VALUES 子句时没有传入任何行
	ErrValuesIsEmpty = errors.New("wxorm: values is empty")
	// ErrInvalidFullTextMode 表示全文检索模式不在方言允许的语法范围内
	ErrInvalidFullTextMode = errors.New("wxorm: invalid full text search mode")
	// ErrUnsupportedFeature 表示当前方言不支持该能力
	// 通过能力接口（Replacer、Upserter、JSONQuerier 等）识别，缺失即返回本错误
	ErrUnsupportedFeature = errors.New("wxorm: feature is not supported by dialect")
	// ErrJoinTableIsEmpty 表示 JOIN 子句没有指定关联表
	ErrJoinTableIsEmpty = errors.New("wxorm: join table is empty")
	// ErrJoinAliasIsEmpty 表示 JOIN 子句没有指定别名
	ErrJoinAliasIsEmpty = errors.New("wxorm: join alias is empty")
	// ErrInvalidJoinKind 表示 JOIN 类型不在方言支持的范围内
	ErrInvalidJoinKind = errors.New("wxorm: invalid join kind")
)

/*
Clause 定义所有方言都必须实现的通用子句能力

方言之间的差异大体分两类：
  - **语法差异**（占位符、引号、分页、UPSERT）：通过本接口表达；
  - **能力差异**（REPLACE、JSON、全文检索）：通过下方可选接口表达。

新增方言时只需实现本接口，再按需追加能力接口，不必为了“像 MySQL”
而实现一堆用不上的方法。
*/
type Clause interface {
	// QuoteIdentifier 转义标识符
	QuoteIdentifier(column string) string

	// GetLikeOperator 获取 LIKE 操作符
	GetLikeOperator() string
	// GetPlaceholder 获取占位符
	GetPlaceholder(argIndex int) string

	// BuildPlaceholders 构造占位符组成的字符串
	BuildPlaceholders(builder *strings.Builder, argStartIndex, num int) error
	// BuildInsertSQL 构造 INSERT 子句
	BuildInsertSQL(ctx *Context, table string, ignore bool, columns ...string) error
	// BuildValuesSQL 构造 VALUES 子句
	BuildValuesSQL(ctx *Context, argStartIndex int, values ...[]any) error
	// BuildUpdateSQL 构造 UPDATE 子句
	BuildUpdateSQL(ctx *Context, table string) error
	// BuildSetSQL 构造 SET 子句
	BuildSetSQL(ctx *Context, argStartIndex int, values map[string]any) error
	// BuildDeleteSQL 构造 DELETE 子句
	BuildDeleteSQL(ctx *Context, table string) error
	// BuildSelectSQL 构造 SELECT 子句
	BuildSelectSQL(ctx *Context, table string, columns ...string) error
	// BuildCountSQL 构造 COUNT 子句
	BuildCountSQL(ctx *Context, column ...string) error
	// BuildFromSQL 构造 FROM 子句
	BuildFromSQL(ctx *Context, table string) error
	// BuildWhereSQL 构造 WHERE 子句
	BuildWhereSQL(ctx *Context) error
	// BuildGroupBySQL 构造 GROUP BY 子句
	BuildGroupBySQL(ctx *Context, columns ...string) error
	// BuildHavingSQL 构造 HAVING 子句
	BuildHavingSQL(ctx *Context) error
	// BuildOrderBySQL 构造 ORDER BY 子句
	BuildOrderBySQL(ctx *Context, columns ...string) error
	// BuildLimitSQL 构造 LIMIT 子句
	BuildLimitSQL(ctx *Context, limit int) error
	// BuildOffsetSQL 构造 OFFSET 子句
	BuildOffsetSQL(ctx *Context, offset int) error
}

/*
可选能力接口

调用方（statement / where）通过类型断言检测方言是否具备某项能力，
不具备时返回 ErrUnsupportedFeature，而不是生成一段语法错误的 SQL。
*/
type (
	// Replacer 支持 REPLACE INTO（MySQL 系）
	Replacer interface {
		BuildReplaceSQL(ctx *Context, table string, columns ...string) error
	}

	// Upserter 支持 UPSERT：MySQL 为 ON DUPLICATE KEY UPDATE，PostgreSQL 为 ON CONFLICT DO UPDATE
	//
	// 语义约定：columns 既作为「更新列」也作为「冲突目标」的简写，
	// 适用于 MySQL 这类由唯一索引自动确定冲突目标的方言。
	// 需要区分两者的方言（PostgreSQL）请额外实现 ConflictUpserter。
	Upserter interface {
		BuildDuplicateSQL(ctx *Context, columns ...string) error
	}

	// ConflictUpserter 支持「冲突目标与更新列分离」的 UPSERT（PostgreSQL）
	//
	// 调用方（session）用模型主键作为 conflict，用显式指定的列作为 update；
	// conflict 为空时返回错误，不静默降级为 DO NOTHING。
	ConflictUpserter interface {
		BuildUpsertSQL(ctx *Context, conflict, update []string) error
	}

	// IgnoreInserter 支持 INSERT 的冲突忽略后缀
	// MySQL 在 INSERT 关键字后内联 IGNORE，PostgreSQL 必须在 VALUES 之后追加 ON CONFLICT DO NOTHING
	IgnoreInserter interface {
		BuildInsertIgnoreSQL(ctx *Context) error
	}

	// ReturningInserter 支持插入后回填数据库生成的值（PostgreSQL 的 RETURNING）
	ReturningInserter interface {
		BuildInsertReturningSQL(ctx *Context, columns ...string) error
	}

	// BatchReturningInserter 支持在多行 INSERT 之后追加 RETURNING（PostgreSQL/SQLite）
	//
	// SQLite 的单条插入用 LastInsertId 回填就够用，只有批量插入需要 RETURNING 逐行取回生成值，
	// 因此单独声明能力，不并入 ReturningInserter。
	BatchReturningInserter interface {
		BuildInsertBatchReturningSQL(ctx *Context, columns ...string) error
	}

	// JSONQuerier 支持 JSON 路径查询
	JSONQuerier interface {
		BuildJSONExtractSQL(ctx *Context, column, path string) error
		BuildJSONContainsSQL(ctx *Context, column, path string, value any) error
	}

	// FullTextSearcher 支持全文检索
	FullTextSearcher interface {
		BuildFullTextSearchSQL(ctx *Context, columns []string, query string, mode string) error
	}

	// Joiner 支持 JOIN 子句
	//
	// BuildJoinSQL 只负责关键字、表名与别名，ON 条件由 statement 层拼接，
	// 这样各方言的差异只集中在关键字支持范围上。
	Joiner interface {
		BuildJoinSQL(ctx *Context, kind JoinKind, table, alias string) error
		SupportsJoinKind(kind JoinKind) bool
	}
)

/*
MySQLClause 是 MySQL 系方言的完整能力集合

保留这个聚合接口，是为了让 MySQL 的测试与调用方仍能直接使用全部方法；
新方言不建议照抄，按需实现即可。
*/
type MySQLClause interface {
	Clause
	Replacer
	Upserter
	JSONQuerier
	FullTextSearcher
	Joiner
}

const (
	_SQL_COMMA_AND_LINE_BREAK_AND_4SPACE = ",\n    "

	_SQL_INSERT_INTO                  = "INSERT INTO\n    "
	_SQL_INSERT_IGNORE_INTO           = "INSERT IGNORE INTO\n    "
	_SQL_REPLACE_INTO                 = "REPLACE INTO\n    "
	_SQL_VALUES                       = "\nVALUES\n    "
	_SQL_DUPLICATE_UPDATE             = "\nON DUPLICATE KEY UPDATE\n    "
	_SQL_VALUES_WHEN_DUPLICATE_UPDATE = " = VALUES("
	_SQL_UPDATE                       = "UPDATE\n    "
	_SQL_SET                          = "\nSET\n    "
	_SQL_DELETE_FROM                  = "DELETE FROM\n    "
	_SQL_SELECT                       = "SELECT\n    "
	_SQL_SELECT_COUNT                 = "SELECT\n    COUNT("
	_SQL_FROM                         = "\nFROM\n    "
	_SQL_WHERE                        = "\nWHERE\n    "
	_SQL_GROUP_BY                     = "\nGROUP BY\n    "
	_SQL_HAVING                       = "\nHAVING\n    "
	_SQL_ORDER_BY                     = "\nORDER BY\n    "
	_SQL_LIMIT                        = "\nLIMIT\n    "
	_SQL_OFFSET                       = "\nOFFSET\n    "

	SQL_RECURSIVE                  = "WITH RECURSIVE "
	SQL_RECURSIVE_AS               = " AS (\n    "
	SQL_RECURSIVE_UNION_LINE_BREAK = "\n    UNION ALL\n    "
)
