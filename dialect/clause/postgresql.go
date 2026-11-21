package clause

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
)

/*
PostgresClause PostgreSQL 方言的完整能力集合

与 MySQL 的差异集中在四处，其余子句结构可以复用同一套语义：
  - 标识符用双引号，且内部双引号需成对转义；
  - 占位符是 $1、$2……，与参数**位置**绑定，因此带占位符的 SQL 不能只按
    “行数+列数”缓存，必须把起始参数序号也放进缓存键；
  - INSERT 冲突处理用 ON CONFLICT，且必须写在 VALUES 之后（IGNORE 不能内联）；
  - 自增主键靠 SERIAL/IDENTITY 生成，回填主键需要用 RETURNING。

注意：本接口**不包含 Upserter**。MySQL 的 DUPLICATE 语义是「更新列」，
冲突目标由唯一索引隐式决定；PostgreSQL 的 DO UPDATE 必须显式给出冲突目标，
两者无法用同一组参数表达。因此 DUPLICATE 在 PostgreSQL 下返回
ErrUnsupportedFeature，UPSERT 到 ConflictUpserter。
*/
type PostgresClause interface {
	Clause
	ConflictUpserter
	IgnoreInserter
	ReturningInserter
	BatchReturningInserter
	JSONQuerier
	FullTextSearcher
	Joiner
}

type postgres struct {
	quotedCache   sync.Map
	fromCache     sync.Map
	insertCache   sync.Map
	valuesCache   sync.Map
	deleteCache   sync.Map
	updateCache   sync.Map
	selectCache   sync.Map
	conflictCache sync.Map
	jsonCache     sync.Map
}

var _ PostgresClause = (*postgres)(nil)

// NewPostgresClause 创建 PostgreSQL 子句构建器
func NewPostgresClause() PostgresClause {
	return &postgres{
		quotedCache:   sync.Map{},
		fromCache:     sync.Map{},
		insertCache:   sync.Map{},
		valuesCache:   sync.Map{},
		deleteCache:   sync.Map{},
		updateCache:   sync.Map{},
		selectCache:   sync.Map{},
		conflictCache: sync.Map{},
		jsonCache:     sync.Map{},
	}
}

// GetLikeOperator 获取 LIKE 操作符（PostgreSQL 用 ILIKE 实现大小写不敏感匹配）
func (own *postgres) GetLikeOperator() string { return "ILIKE" }

// GetPlaceholder 获取占位符
func (own *postgres) GetPlaceholder(argIndex int) string {
	return "$" + strconv.Itoa(argIndex)
}

// QuoteIdentifier 转义标识符
//
// 与 MySQL 保持一致：DISTINCT 前缀与 ASC/DESC 后缀原样保留，
// 其余按点号拆段逐段加双引号。
func (own *postgres) QuoteIdentifier(name string) string {
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
// argStartIndex 是本次写入前的参数数量，因此第一个占位符编号为 argStartIndex+1。
func (own *postgres) BuildPlaceholders(builder *strings.Builder, argStartIndex, num int) error {
	for i := 1; i <= num; i++ {
		if i > 1 {
			builder.WriteString(", ")
		}

		builder.WriteByte('$')
		builder.WriteString(strconv.Itoa(argStartIndex + i))
	}

	return nil
}

/*
BuildInsertSQL 构造 INSERT 子句

ignore 参数在 PostgreSQL 中不内联：冲突忽略由 BuildInsertIgnoreSQL 在 VALUES 之后追加，
因此这里始终保持普通 INSERT，避免把 `INSERT IGNORE` 这种非法语法发给 PG。
*/
func (own *postgres) BuildInsertSQL(ctx *Context, table string, _ bool, columns ...string) error {
	key := table + "\x1f" + strings.Join(columns, "\x1f")
	if sql, ok := own.insertCache.Load(key); ok {
		return ctx.WriteString(sql.(string))
	}

	var builder strings.Builder
	builder.Grow(64 + len(columns)*16)

	builder.WriteString("INSERT INTO\n    ")
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

// BuildInsertIgnoreSQL 追加 ON CONFLICT DO NOTHING
func (own *postgres) BuildInsertIgnoreSQL(ctx *Context) error {
	return ctx.WriteString("\nON CONFLICT DO NOTHING")
}

// BuildInsertReturningSQL 追加 RETURNING 子句，用于回填自增主键
func (own *postgres) BuildInsertReturningSQL(ctx *Context, columns ...string) error {
	if len(columns) == 0 {
		return ErrValuesIsEmpty
	}

	if err := ctx.WriteString("\nRETURNING "); err != nil {
		return err
	}

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

// BuildInsertBatchReturningSQL 批量 INSERT 的 RETURNING 子句
//
// PostgreSQL 的单行与多行 RETURNING 语法一致，直接复用单条实现。
func (own *postgres) BuildInsertBatchReturningSQL(ctx *Context, columns ...string) error {
	return own.BuildInsertReturningSQL(ctx, columns...)
}

// valuesCacheKey VALUES 子句的缓存键
//
// PostgreSQL 的占位符与参数位置绑定，同样的行数/列数在不同起始序号下生成的 SQL 不同，
// 因此起始序号必须参与缓存键。
type valuesCacheKey struct {
	start int
	rows  int
	cols  int
}

// BuildValuesSQL 构造 VALUES 子句
func (own *postgres) BuildValuesSQL(ctx *Context, argStartIndex int, values ...[]any) error {
	if len(values) == 0 {
		return ErrValuesIsEmpty
	}

	rows, cols := len(values), len(values[0])
	key := valuesCacheKey{start: argStartIndex, rows: rows, cols: cols}
	if sql, ok := own.valuesCache.Load(key); ok {
		for _, value := range values {
			ctx.AddArgs(value...)
		}

		return ctx.WriteString(sql.(string))
	}

	var builder strings.Builder
	builder.Grow(rows * (cols*4 + 8))
	builder.WriteString("\nVALUES\n    ")

	index := argStartIndex
	for i, value := range values {
		if i > 0 {
			builder.WriteString(",\n    ")
		}

		builder.WriteByte('(')
		for j := range value {
			if j > 0 {
				builder.WriteString(", ")
			}

			index++
			builder.WriteByte('$')
			builder.WriteString(strconv.Itoa(index))
		}
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
	    "name" = EXCLUDED."name",
	    "age" = EXCLUDED."age"

PostgreSQL 的 DO UPDATE 必须带冲突目标，因此 conflict 为空时直接报错，
不降级为 DO NOTHING——否则调用方会误以为数据被更新。

本方法不实现 MySQL 语义的 BuildDuplicateSQL：DUPLICATE 在 PostgreSQL 下
应直接返回不支持，避免「冲突目标 = 更新列」这种跨方言不成立的假设。
*/
func (own *postgres) BuildUpsertSQL(ctx *Context, conflict, update []string) error {
	if len(conflict) == 0 || len(update) == 0 {
		return ErrValuesIsEmpty
	}

	return own.buildConflictUpdate(ctx, conflict, update)
}

// buildConflictUpdate 按「冲突目标 + 更新列」生成 ON CONFLICT 子句
func (own *postgres) buildConflictUpdate(ctx *Context, conflict, update []string) error {
	key := strings.Join(conflict, "\x1f") + "\x1e" + strings.Join(update, "\x1f")
	if sql, ok := own.conflictCache.Load(key); ok {
		return ctx.WriteString(sql.(string))
	}

	var builder strings.Builder
	builder.Grow(64 + (len(conflict)+len(update))*32)
	builder.WriteString("\nON CONFLICT (")
	own.joinColumns(&builder, conflict...)

	builder.WriteString(") DO UPDATE SET\n    ")
	for i, column := range update {
		if i > 0 {
			builder.WriteString(",\n    ")
		}

		quoted := own.QuoteIdentifier(column)
		builder.WriteString(quoted)
		builder.WriteString(" = EXCLUDED.")
		builder.WriteString(quoted)
	}

	sql := builder.String()
	own.conflictCache.Store(key, sql)
	return ctx.WriteString(sql)
}

// BuildUpdateSQL 构造 UPDATE 子句
func (own *postgres) BuildUpdateSQL(ctx *Context, table string) error {
	if sql, ok := own.updateCache.Load(table); ok {
		return ctx.WriteString(sql.(string))
	}

	sql := "UPDATE\n    " + own.QuoteIdentifier(table)
	own.updateCache.Store(table, sql)
	return ctx.WriteString(sql)
}

// BuildSetSQL 构造 SET 子句
//
// 通过 WritePlaceholderWithArg 写入占位符，保证 $n 与参数数量始终一致；
// 列名排序保证同一份数据生成的 SQL 文本稳定。
func (own *postgres) BuildSetSQL(ctx *Context, _ int, values map[string]any) error {
	if err := ctx.WriteString("\nSET\n    "); err != nil {
		return err
	}

	for i, column := range sortMapKeys(values) {
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
func (own *postgres) BuildDeleteSQL(ctx *Context, table string) error {
	if sql, ok := own.deleteCache.Load(table); ok {
		return ctx.WriteString(sql.(string))
	}

	sql := "DELETE FROM\n    " + own.QuoteIdentifier(table)
	own.deleteCache.Store(table, sql)
	return ctx.WriteString(sql)
}

// BuildSelectSQL 构造 SELECT 子句
func (own *postgres) BuildSelectSQL(ctx *Context, table string, columns ...string) error {
	key := table + "\x1f" + strings.Join(columns, "\x1f")
	if sql, ok := own.selectCache.Load(key); ok {
		return ctx.WriteString(sql.(string))
	}

	var builder strings.Builder
	builder.Grow(16 + len(columns)*16)
	builder.WriteString("SELECT\n    ")

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
func (own *postgres) BuildCountSQL(ctx *Context, column ...string) error {
	if err := ctx.WriteString("SELECT\n    COUNT("); err != nil {
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
func (own *postgres) BuildFromSQL(ctx *Context, table string) error {
	if sql, ok := own.fromCache.Load(table); ok {
		return ctx.WriteString(sql.(string))
	}

	sql := "\nFROM\n    " + own.QuoteIdentifier(table)
	own.fromCache.Store(table, sql)
	return ctx.WriteString(sql)
}

// BuildJoinSQL 构造 JOIN 子句
//
// PostgreSQL 完整支持标准 SQL 的 JOIN 类型。
func (own *postgres) BuildJoinSQL(ctx *Context, kind JoinKind, table, alias string) error {
	if err := CheckJoinKind(kind); err != nil {
		return err
	}

	if !own.SupportsJoinKind(kind) {
		return NewUnsupportedJoinError(kind)
	}

	return BuildJoinClause(ctx, own, kind, table, alias)
}

// SupportsJoinKind 判断 PostgreSQL 是否支持该 JOIN 类型
func (own *postgres) SupportsJoinKind(kind JoinKind) bool {
	switch kind {
	case JOIN_INNER, JOIN_LEFT, JOIN_RIGHT, JOIN_FULL, JOIN_CROSS:
		return true
	default:
		return false
	}
}

// BuildWhereSQL 构造 WHERE 子句
func (own *postgres) BuildWhereSQL(ctx *Context) error {
	return ctx.WriteString("\nWHERE\n    ")
}

// BuildGroupBySQL 构造 GROUP BY 子句
func (own *postgres) BuildGroupBySQL(ctx *Context, columns ...string) error {
	if err := ctx.WriteString("\nGROUP BY\n    "); err != nil {
		return err
	}

	return own.joinColumnsContext(ctx, columns...)
}

// BuildHavingSQL 构造 HAVING 子句
func (own *postgres) BuildHavingSQL(ctx *Context) error {
	return ctx.WriteString("\nHAVING\n    ")
}

// BuildOrderBySQL 构造 ORDER BY 子句
func (own *postgres) BuildOrderBySQL(ctx *Context, columns ...string) error {
	if err := ctx.WriteString("\nORDER BY\n    "); err != nil {
		return err
	}

	return own.joinColumnsContext(ctx, columns...)
}

// BuildLimitSQL 构造 LIMIT 子句
//
// 不缓存结果：LIMIT 的取值来自分页参数，可能的取值无界，
// 以 int 为键的缓存会随分页深度持续增长。
func (own *postgres) BuildLimitSQL(ctx *Context, limit int) error {
	ctx.MarkLimit()

	if err := ctx.WriteString("\nLIMIT\n    "); err != nil {
		return err
	}

	return ctx.WriteInt(limit)
}

// BuildOffsetSQL 构造 OFFSET 子句
//
// 同样不缓存，理由与 BuildLimitSQL 一致。
func (own *postgres) BuildOffsetSQL(ctx *Context, offset int) error {
	if err := ctx.WriteString("\nOFFSET\n    "); err != nil {
		return err
	}

	return ctx.WriteInt(offset)
}

/*
BuildJSONExtractSQL 构造 JSON 路径取值

PostgreSQL 的路径语法是 '{a,b}'，与 MySQL 的 '$.a.b' 不同，
这里把调用方传入的 MySQL 风格路径转换为 PostgreSQL 风格，保持 where 层 API 一致。
*/
func (own *postgres) BuildJSONExtractSQL(ctx *Context, column, path string) error {
	key := column + "\x1f" + path
	if sql, ok := own.jsonCache.Load(key); ok {
		return ctx.WriteString(sql.(string))
	}

	sql := "(" + own.QuoteIdentifier(column) + " #>> " + quotePostgresJSONPath(path) + ")"
	own.jsonCache.Store(key, sql)
	return ctx.WriteString(sql)
}

// BuildJSONContainsSQL 构造 JSON 包含判断
func (own *postgres) BuildJSONContainsSQL(ctx *Context, column, path string, value any) error {
	sql := "(" + own.QuoteIdentifier(column) + " #> " + quotePostgresJSONPath(path) + ") @> "
	if err := ctx.WriteString(sql); err != nil {
		return err
	}

	// 用 jsonb 转一次，避免驱动把 Go 值当成普通字符串参数
	if err := ctx.WritePlaceholderWithArg(own, value); err != nil {
		return err
	}

	return ctx.WriteString("::jsonb")
}

/*
BuildFullTextSearchSQL 构造全文检索

PostgreSQL 没有 MATCH ... AGAINST，用 to_tsvector @@ to_tsquery 表达：
  - 自然语言模式映射到 plainto_tsquery；
  - 布尔模式映射到 websearch_to_tsquery（PG 11+，语法更接近用户直觉）；
  - 其余 MySQL 模式没有等价语义，直接返回不支持，避免静默降级。
*/
func (own *postgres) BuildFullTextSearchSQL(ctx *Context, columns []string, term, mode string) error {
	if len(columns) == 0 {
		return ErrValuesIsEmpty
	}

	queryFunc, err := buildPostgresFullTextQueryFunc(mode)
	if err != nil {
		return err
	}

	if err := ctx.WriteString("to_tsvector('simple', "); err != nil {
		return err
	}

	for i, column := range columns {
		if i > 0 {
			if err := ctx.WriteString(" || ' ' || "); err != nil {
				return err
			}
		}

		if err := ctx.WriteString("coalesce("); err != nil {
			return err
		}
		if err := ctx.WriteString(own.QuoteIdentifier(column)); err != nil {
			return err
		}
		if err := ctx.WriteString(", '')"); err != nil {
			return err
		}
	}

	if err := ctx.WriteString(") @@ "); err != nil {
		return err
	}
	if err := ctx.WriteString(queryFunc); err != nil {
		return err
	}
	if err := ctx.WriteByte('('); err != nil {
		return err
	}
	if err := ctx.WritePlaceholderWithArg(own, term); err != nil {
		return err
	}

	return ctx.WriteByte(')')
}

// buildPostgresFullTextQueryFunc 把 MySQL 的检索模式映射到 PostgreSQL 的查询函数
func buildPostgresFullTextQueryFunc(mode string) (string, error) {
	switch strings.Join(strings.Fields(strings.ToUpper(mode)), " ") {
	case "", "IN NATURAL LANGUAGE MODE":
		return "plainto_tsquery", nil
	case "IN BOOLEAN MODE":
		return "websearch_to_tsquery", nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnsupportedFeature, mode)
	}
}

// quotePostgresJSONPath 把 MySQL 风格路径转成 PostgreSQL 的 '{a,b}' 形式并转义单引号
func quotePostgresJSONPath(path string) string {
	path = strings.TrimPrefix(strings.TrimSpace(path), "$")
	path = strings.TrimPrefix(path, ".")
	path = strings.TrimSuffix(path, "]")
	path = strings.ReplaceAll(path, "[", ",")
	path = strings.ReplaceAll(path, ".", ",")
	path = strings.ReplaceAll(path, ",,", ",")
	path = strings.ReplaceAll(path, "'", "''")

	return "'{" + path + "}'"
}

// sortMapKeys 返回 map 的有序键集合，保证 SET 子句的 SQL 文本稳定
func sortMapKeys(values map[string]any) []string {
	return slices.Sorted(maps.Keys(values))
}

func (own *postgres) joinColumns(builder *strings.Builder, columns ...string) {
	for i, column := range columns {
		if i > 0 {
			builder.WriteString(", ")
		}

		builder.WriteString(own.QuoteIdentifier(column))
	}
}

func (own *postgres) joinColumnsContext(ctx *Context, columns ...string) error {
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
