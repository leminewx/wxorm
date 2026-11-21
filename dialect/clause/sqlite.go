package clause

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
)

/*
SQLiteClause SQLite 方言的完整能力集合

与 MySQL 的相同点：占位符是 ?、INSERT OR IGNORE 可以内联、支持 JSON1 函数；
不同点：
  - 标识符用双引号（SQLite 也接受反引号，但双引号是标准写法）；
  - 冲突更新写 ON CONFLICT (...) DO UPDATE SET，冲突目标必须显式给出；
  - 自增靠 INTEGER PRIMARY KEY AUTOINCREMENT，回填主键用 LastInsertId；
  - 没有 REPLACE 的等价语法问题（REPLACE INTO 是 INSERT OR REPLACE 的别名）。

本接口刻意不包含两项能力：
  - Upserter：MySQL 的「冲突目标 = 更新列」在 SQLite 同样不成立，DUPLICATE 返回不支持；
  - FullTextSearcher：SQLite 的全文检索依赖 FTS5 虚拟表，无法用单条子句表达。
*/
type SQLiteClause interface {
	Clause
	Replacer
	ConflictUpserter
	JSONQuerier
	BatchReturningInserter
	Joiner
}

type sqlite struct {
	quotedCache   sync.Map
	fromCache     sync.Map
	insertCache   sync.Map
	valuesCache   sync.Map
	deleteCache   sync.Map
	updateCache   sync.Map
	selectCache   sync.Map
	replaceCache  sync.Map
	conflictCache sync.Map
	jsonCache     sync.Map
}

var _ SQLiteClause = (*sqlite)(nil)

// NewSQLiteClause 创建 SQLite 子句构建器
func NewSQLiteClause() SQLiteClause {
	return &sqlite{
		quotedCache:   sync.Map{},
		fromCache:     sync.Map{},
		insertCache:   sync.Map{},
		valuesCache:   sync.Map{},
		deleteCache:   sync.Map{},
		updateCache:   sync.Map{},
		selectCache:   sync.Map{},
		replaceCache:  sync.Map{},
		conflictCache: sync.Map{},
		jsonCache:     sync.Map{},
	}
}

// GetLikeOperator 获取 LIKE 操作符
//
// SQLite 的 LIKE 默认对 ASCII 大小写不敏感，不需要 PostgreSQL 那样的 ILIKE。
func (own *sqlite) GetLikeOperator() string { return "LIKE" }

// GetPlaceholder 获取占位符
func (own *sqlite) GetPlaceholder(_ int) string { return "?" }

// QuoteIdentifier 转义标识符
//
// 与 MySQL 保持一致：DISTINCT 前缀与 ASC/DESC 后缀原样保留，
// 其余按点号拆段逐段加双引号，内部双引号成对转义。
func (own *sqlite) QuoteIdentifier(name string) string {
	if quoted, ok := own.quotedCache.Load(name); ok {
		return quoted.(string)
	}

	prefix, body, suffix := splitIdentifierModifiers(name)
	parts := strings.Split(body, ".")
	for i, part := range parts {
		parts[i] = `"` + strings.ReplaceAll(part, `"`, `""`) + `"`
	}

	sql := prefix + strings.Join(parts, ".") + suffix
	own.quotedCache.Store(name, sql)
	return sql
}

// BuildPlaceholders 构造占位符组成的字符串
//
// SQLite 的 ? 与参数出现顺序绑定，argStartIndex 不参与生成。
func (own *sqlite) BuildPlaceholders(builder *strings.Builder, _ int, num int) error {
	for i := 0; i < num; i++ {
		if i > 0 {
			builder.WriteString(", ")
		}

		builder.WriteByte('?')
	}

	return nil
}

// BuildInsertSQL 构造 INSERT 子句
//
// SQLite 支持 INSERT OR IGNORE，冲突忽略可以内联在 INSERT 关键字之后。
func (own *sqlite) BuildInsertSQL(ctx *Context, table string, ignore bool, columns ...string) error {
	key := table + "\x1f" + strconv.FormatBool(ignore) + "\x1f" + strings.Join(columns, "\x1f")
	if sql, ok := own.insertCache.Load(key); ok {
		return ctx.WriteString(sql.(string))
	}

	var builder strings.Builder
	builder.Grow(128)

	if ignore {
		builder.WriteString("INSERT OR IGNORE INTO\n    ")
	} else {
		builder.WriteString(_SQL_INSERT_INTO)
	}

	builder.WriteString(own.QuoteIdentifier(table))

	if len(columns) > 0 {
		builder.WriteString(" (")
		own.joinColumns(&builder, columns...)
		builder.WriteByte(')')
	}

	sql := builder.String()
	own.insertCache.Store(key, sql)
	return ctx.WriteString(sql)
}

// BuildInsertBatchReturningSQL 追加 RETURNING 子句，用于批量插入后回填生成值
//
// SQLite 3.35 起支持 RETURNING；单条插入仍走 LastInsertId，只有批量回填依赖本方法。
func (own *sqlite) BuildInsertBatchReturningSQL(ctx *Context, columns ...string) error {
	if len(columns) == 0 {
		return ErrValuesIsEmpty
	}

	var builder strings.Builder
	builder.Grow(64)
	builder.WriteString("\nRETURNING ")
	own.joinColumns(&builder, columns...)

	return ctx.WriteString(builder.String())
}

// BuildReplaceSQL 构造 REPLACE 子句
//
// REPLACE INTO 是 INSERT OR REPLACE INTO 的别名，语义与 MySQL 的 REPLACE 一致。
func (own *sqlite) BuildReplaceSQL(ctx *Context, table string, columns ...string) error {
	key := table + "\x1f" + strings.Join(columns, "\x1f")
	if sql, ok := own.replaceCache.Load(key); ok {
		return ctx.WriteString(sql.(string))
	}

	var builder strings.Builder
	builder.Grow(128)
	builder.WriteString(_SQL_REPLACE_INTO)
	builder.WriteString(own.QuoteIdentifier(table))

	if len(columns) > 0 {
		builder.WriteString(" (")
		own.joinColumns(&builder, columns...)
		builder.WriteByte(')')
	}

	sql := builder.String()
	own.replaceCache.Store(key, sql)
	return ctx.WriteString(sql)
}

// BuildValuesSQL 构造 VALUES 子句
func (own *sqlite) BuildValuesSQL(ctx *Context, _ int, values ...[]any) error {
	if len(values) == 0 {
		return ErrValuesIsEmpty
	}

	rows, cols := len(values), len(values[0])
	key := uint64(rows)<<32 | uint64(cols)
	if sql, ok := own.valuesCache.Load(key); ok {
		for _, value := range values {
			ctx.AddArgs(value...)
		}

		return ctx.WriteString(sql.(string))
	}

	var builder strings.Builder
	builder.Grow(rows * (cols*2 + 8))
	builder.WriteString(_SQL_VALUES)

	for i, value := range values {
		if i > 0 {
			builder.WriteString(_SQL_COMMA_AND_LINE_BREAK_AND_4SPACE)
		}

		builder.WriteByte('(')
		_ = own.BuildPlaceholders(&builder, 0, len(value))
		builder.WriteByte(')')

		ctx.AddArgs(value...)
	}

	sql := builder.String()
	own.valuesCache.Store(key, sql)
	return ctx.WriteString(sql)
}

/*
BuildUpsertSQL 构造「冲突目标与更新列分离」的 UPSERT 子句

example: BuildUpsertSQL(ctx, []string{"id"}, []string{"name", "age"}) 生成

	ON CONFLICT ("id") DO UPDATE SET
	    "name" = excluded."name",
	    "age" = excluded."age"

SQLite 的 DO UPDATE 必须带冲突目标，因此 conflict 为空时直接报错，
不降级为 DO NOTHING——否则调用方会误以为数据被更新。
*/
func (own *sqlite) BuildUpsertSQL(ctx *Context, conflict, update []string) error {
	if len(conflict) == 0 || len(update) == 0 {
		return ErrValuesIsEmpty
	}

	key := strings.Join(conflict, "\x1f") + "\x1e" + strings.Join(update, "\x1f")
	if sql, ok := own.conflictCache.Load(key); ok {
		return ctx.WriteString(sql.(string))
	}

	var builder strings.Builder
	builder.Grow(64 + (len(conflict)+len(update))*24)
	builder.WriteString("\nON CONFLICT (")
	own.joinColumns(&builder, conflict...)
	builder.WriteString(") DO UPDATE SET\n    ")

	for i, column := range update {
		if i > 0 {
			builder.WriteString(",\n    ")
		}

		quoted := own.QuoteIdentifier(column)
		builder.WriteString(quoted)
		builder.WriteString(" = excluded.")
		builder.WriteString(quoted)
	}

	sql := builder.String()
	own.conflictCache.Store(key, sql)
	return ctx.WriteString(sql)
}

// BuildUpdateSQL 构造 UPDATE 子句
func (own *sqlite) BuildUpdateSQL(ctx *Context, table string) error {
	if sql, ok := own.updateCache.Load(table); ok {
		return ctx.WriteString(sql.(string))
	}

	sql := _SQL_UPDATE + own.QuoteIdentifier(table)
	own.updateCache.Store(table, sql)
	return ctx.WriteString(sql)
}

// BuildSetSQL 构造 SET 子句
//
// 通过 WritePlaceholderWithArg 写入占位符，列名排序保证同一份数据生成的 SQL 稳定。
func (own *sqlite) BuildSetSQL(ctx *Context, _ int, values map[string]any) error {
	if err := ctx.WriteString(_SQL_SET); err != nil {
		return err
	}

	for i, column := range slices.Sorted(maps.Keys(values)) {
		if i > 0 {
			if err := ctx.WriteString(", "); err != nil {
				return err
			}
		}

		if err := ctx.WriteString(own.QuoteIdentifier(column)); err != nil {
			return err
		}
		if err := ctx.WriteString(" = "); err != nil {
			return err
		}
		if err := ctx.WritePlaceholderWithArg(own, values[column]); err != nil {
			return err
		}
	}

	return nil
}

// BuildDeleteSQL 构造 DELETE 子句
func (own *sqlite) BuildDeleteSQL(ctx *Context, table string) error {
	if sql, ok := own.deleteCache.Load(table); ok {
		return ctx.WriteString(sql.(string))
	}

	sql := _SQL_DELETE_FROM + own.QuoteIdentifier(table)
	own.deleteCache.Store(table, sql)
	return ctx.WriteString(sql)
}

// BuildSelectSQL 构造 SELECT 子句
func (own *sqlite) BuildSelectSQL(ctx *Context, table string, columns ...string) error {
	key := table + "\x1f" + strings.Join(columns, "\x1f")
	if sql, ok := own.selectCache.Load(key); ok {
		return ctx.WriteString(sql.(string))
	}

	var builder strings.Builder
	builder.Grow(16 + len(columns)*16)
	builder.WriteString(_SQL_SELECT)

	if len(columns) == 0 {
		builder.WriteByte('*')
	} else {
		own.joinColumns(&builder, columns...)
	}

	sql := builder.String()
	own.selectCache.Store(key, sql)
	return ctx.WriteString(sql)
}

// BuildCountSQL 构造 COUNT 子句
func (own *sqlite) BuildCountSQL(ctx *Context, column ...string) error {
	if err := ctx.WriteString(_SQL_SELECT_COUNT); err != nil {
		return err
	}

	if len(column) == 0 {
		if err := ctx.WriteByte('*'); err != nil {
			return err
		}
	} else if err := ctx.WriteString(own.QuoteIdentifier(column[0])); err != nil {
		return err
	}

	return ctx.WriteByte(')')
}

// BuildFromSQL 构造 FROM 子句
func (own *sqlite) BuildFromSQL(ctx *Context, table string) error {
	if sql, ok := own.fromCache.Load(table); ok {
		return ctx.WriteString(sql.(string))
	}

	sql := _SQL_FROM + own.QuoteIdentifier(table)
	own.fromCache.Store(table, sql)
	return ctx.WriteString(sql)
}

// BuildJoinSQL 构造 JOIN 子句
//
// SQLite 3.39 起支持 RIGHT/FULL JOIN；无法静态判断版本，按支持处理。
func (own *sqlite) BuildJoinSQL(ctx *Context, kind JoinKind, table, alias string) error {
	if err := CheckJoinKind(kind); err != nil {
		return err
	}

	if !own.SupportsJoinKind(kind) {
		return NewUnsupportedJoinError(kind)
	}

	return BuildJoinClause(ctx, own, kind, table, alias)
}

// SupportsJoinKind 判断 SQLite 是否支持该 JOIN 类型
func (own *sqlite) SupportsJoinKind(kind JoinKind) bool {
	switch kind {
	case JOIN_INNER, JOIN_LEFT, JOIN_RIGHT, JOIN_FULL, JOIN_CROSS:
		return true
	default:
		return false
	}
}

// BuildWhereSQL 构造 WHERE 子句
func (own *sqlite) BuildWhereSQL(ctx *Context) error { return ctx.WriteString(_SQL_WHERE) }

// BuildGroupBySQL 构造 GROUP BY 子句
func (own *sqlite) BuildGroupBySQL(ctx *Context, columns ...string) error {
	if err := ctx.WriteString(_SQL_GROUP_BY); err != nil {
		return err
	}

	return own.joinColumnsContext(ctx, columns...)
}

// BuildHavingSQL 构造 HAVING 子句
func (own *sqlite) BuildHavingSQL(ctx *Context) error { return ctx.WriteString(_SQL_HAVING) }

// BuildOrderBySQL 构造 ORDER BY 子句
func (own *sqlite) BuildOrderBySQL(ctx *Context, columns ...string) error {
	if err := ctx.WriteString(_SQL_ORDER_BY); err != nil {
		return err
	}

	return own.joinColumnsContext(ctx, columns...)
}

// BuildLimitSQL 构造 LIMIT 子句
//
// 不缓存结果：LIMIT 的取值来自分页参数，可能的取值无界，
// 以 int 为键的缓存会随分页深度持续增长。
func (own *sqlite) BuildLimitSQL(ctx *Context, limit int) error {
	ctx.MarkLimit()

	if err := ctx.WriteString(_SQL_LIMIT); err != nil {
		return err
	}

	return ctx.WriteInt(limit)
}

// BuildOffsetSQL 构造 OFFSET 子句
//
// SQLite 的 OFFSET 只能跟在 LIMIT 之后，会话层已保证单独使用 OFFSET 会报错。
// 同样不缓存，理由与 BuildLimitSQL 一致。
func (own *sqlite) BuildOffsetSQL(ctx *Context, offset int) error {
	if err := ctx.WriteString(_SQL_OFFSET); err != nil {
		return err
	}

	return ctx.WriteInt(offset)
}

/*
BuildJSONExtractSQL 构造 JSON 路径取值

SQLite 的 JSON1 扩展与 MySQL 的路径语法一致（$.a.b、$.tags[0]），
因此这里不需要像 PostgreSQL 那样转换路径，只需转义单引号。
*/
func (own *sqlite) BuildJSONExtractSQL(ctx *Context, column, path string) error {
	key := column + "\x1f" + path
	if sql, ok := own.jsonCache.Load(key); ok {
		return ctx.WriteString(sql.(string))
	}

	sql := "json_extract(" + own.QuoteIdentifier(column) + ", " + quoteJSONPath(path) + ")"
	own.jsonCache.Store(key, sql)
	return ctx.WriteString(sql)
}

/*
BuildJSONContainsSQL 构造 JSON 数组包含判断

MySQL 用 JSON_CONTAINS(col, ?, '$.tags') 表达「数组里有这个值」；
SQLite 没有 JSON_CONTAINS，用 json_each 展开数组后判断是否存在等值元素：

	EXISTS (SELECT 1 FROM json_each("col", '$.tags') WHERE json_each.value = ?)
*/
func (own *sqlite) BuildJSONContainsSQL(ctx *Context, column, path string, value any) error {
	prefix := "EXISTS (SELECT 1 FROM json_each(" + own.QuoteIdentifier(column) + ", " + quoteJSONPath(path) + ") WHERE json_each.value = "
	if err := ctx.WriteString(prefix); err != nil {
		return err
	}

	if err := ctx.WritePlaceholderWithArg(own, value); err != nil {
		return err
	}

	return ctx.WriteByte(')')
}

func (own *sqlite) joinColumns(builder *strings.Builder, columns ...string) {
	for i, column := range columns {
		if i > 0 {
			builder.WriteString(", ")
		}

		builder.WriteString(own.QuoteIdentifier(column))
	}
}

func (own *sqlite) joinColumnsContext(ctx *Context, columns ...string) error {
	for i, column := range columns {
		if i > 0 {
			if err := ctx.WriteString(", "); err != nil {
				return err
			}
		}

		if err := ctx.WriteString(own.QuoteIdentifier(column)); err != nil {
			return err
		}
	}

	return nil
}
