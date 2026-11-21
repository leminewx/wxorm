package clause

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

type mysql struct {
	quotedCache sync.Map

	fromCache        sync.Map
	insertCache      sync.Map
	valuesCache      sync.Map
	deleteCache      sync.Map
	updateCache      sync.Map
	selectCache      sync.Map
	replaceCache     sync.Map
	duplicateCache   sync.Map
	jsonExtractCache sync.Map
	jsonContainCache sync.Map
}

var _ MySQLClause = (*mysql)(nil)

func NewMySQLClause() MySQLClause {
	return &mysql{
		quotedCache:      sync.Map{},
		fromCache:        sync.Map{},
		insertCache:      sync.Map{},
		valuesCache:      sync.Map{},
		updateCache:      sync.Map{},
		deleteCache:      sync.Map{},
		selectCache:      sync.Map{},
		replaceCache:     sync.Map{},
		duplicateCache:   sync.Map{},
		jsonExtractCache: sync.Map{},
		jsonContainCache: sync.Map{},
	}
}

// GetLikeOperator 获取 LIKE 操作符
func (own *mysql) GetLikeOperator() string {
	return "LIKE"
}

// GetPlaceholder 获取占位符
func (own *mysql) GetPlaceholder(_ int) string {
	return "?"
}

/*
QuoteIdentifier 转义标识符

字段名可能带有去重或排序修饰（如 DISTINCT id、id ASC），修饰词需要原样保留；
其余内容按点号拆段逐段转义，因此形如 column name 的字段名会得到 `column name`。
*/
func (own *mysql) QuoteIdentifier(name string) string {
	// 检查缓存
	if quoted, ok := own.quotedCache.Load(name); ok {
		return quoted.(string)
	}

	// 拆出去重前缀与排序后缀
	prefix, body, suffix := splitIdentifierModifiers(name)

	// 计算
	parts := strings.Split(body, ".")
	for i, part := range parts {
		parts[i] = "`" + strings.ReplaceAll(part, "`", "``") + "`"
	}
	sql := prefix + strings.Join(parts, ".") + suffix

	// 缓存
	own.quotedCache.Store(name, sql)
	return sql
}

// splitIdentifierModifiers 拆出标识符的去重前缀与排序后缀。
func splitIdentifierModifiers(name string) (prefix, body, suffix string) {
	const distinct = "DISTINCT "

	body = strings.TrimSpace(name)
	if len(body) > len(distinct) && strings.EqualFold(body[:len(distinct)], distinct) {
		prefix = distinct
		body = strings.TrimSpace(body[len(distinct):])
	}

	for _, modifier := range []string{"ASC", "DESC"} {
		if len(body) <= len(modifier)+1 {
			continue
		}

		tail := body[len(body)-len(modifier)-1:]
		if tail[0] != ' ' || !strings.EqualFold(tail[1:], modifier) {
			continue
		}

		suffix = " " + modifier
		body = strings.TrimSpace(body[:len(body)-len(modifier)-1])
		break
	}

	return prefix, body, suffix
}

const (
	_PLACEHOLDERS_02 = "?, ?"
	_PLACEHOLDERS_03 = "?, ?, ?"
	_PLACEHOLDERS_04 = "?, ?, ?, ?"
	_PLACEHOLDERS_05 = "?, ?, ?, ?, ?"
	_PLACEHOLDERS_06 = "?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_07 = "?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_08 = "?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_09 = "?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_10 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_11 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_12 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_13 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_14 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_15 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_16 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_17 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_18 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_19 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_20 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_21 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_22 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_23 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_24 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_25 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_26 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_27 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_28 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_29 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
	_PLACEHOLDERS_30 = "?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?"
)

// BuildPlaceholders 构造占位符组成的字符串
func (own *mysql) BuildPlaceholders(builder *strings.Builder, _, num int) error {
	switch num {
	case 1:
		builder.WriteByte('?')
	case 2:
		builder.WriteString(_PLACEHOLDERS_02)
	case 3:
		builder.WriteString(_PLACEHOLDERS_03)
	case 4:
		builder.WriteString(_PLACEHOLDERS_04)
	case 5:
		builder.WriteString(_PLACEHOLDERS_05)
	case 6:
		builder.WriteString(_PLACEHOLDERS_06)
	case 7:
		builder.WriteString(_PLACEHOLDERS_07)
	case 8:
		builder.WriteString(_PLACEHOLDERS_08)
	case 9:
		builder.WriteString(_PLACEHOLDERS_09)
	case 10:
		builder.WriteString(_PLACEHOLDERS_10)
	case 11:
		builder.WriteString(_PLACEHOLDERS_11)
	case 12:
		builder.WriteString(_PLACEHOLDERS_12)
	case 13:
		builder.WriteString(_PLACEHOLDERS_13)
	case 14:
		builder.WriteString(_PLACEHOLDERS_14)
	case 15:
		builder.WriteString(_PLACEHOLDERS_15)
	case 16:
		builder.WriteString(_PLACEHOLDERS_16)
	case 17:
		builder.WriteString(_PLACEHOLDERS_17)
	case 18:
		builder.WriteString(_PLACEHOLDERS_18)
	case 19:
		builder.WriteString(_PLACEHOLDERS_19)
	case 20:
		builder.WriteString(_PLACEHOLDERS_20)
	case 21:
		builder.WriteString(_PLACEHOLDERS_21)
	case 22:
		builder.WriteString(_PLACEHOLDERS_22)
	case 23:
		builder.WriteString(_PLACEHOLDERS_23)
	case 24:
		builder.WriteString(_PLACEHOLDERS_24)
	case 25:
		builder.WriteString(_PLACEHOLDERS_25)
	case 26:
		builder.WriteString(_PLACEHOLDERS_26)
	case 27:
		builder.WriteString(_PLACEHOLDERS_27)
	case 28:
		builder.WriteString(_PLACEHOLDERS_28)
	case 29:
		builder.WriteString(_PLACEHOLDERS_29)
	case 30:
		builder.WriteString(_PLACEHOLDERS_30)
	default:
		builder.WriteString(_PLACEHOLDERS_30)
		for i := 0; i < num-30; i++ {
			builder.WriteString(", ?")
		}
	}

	return nil
}

// BuildInsertSQL 构造 INSERT 子句
func (own *mysql) BuildInsertSQL(ctx *Context, table string, ignore bool, columns ...string) (err error) {
	// 缓存键必须包含字段列表，否则同一张表的其他字段组合会命中错误的 SQL
	key := table + "\x1f" + strconv.FormatBool(ignore) + "\x1f" + strings.Join(columns, "\x1f")
	if sql, ok := own.insertCache.Load(key); ok {
		return ctx.WriteString(sql.(string))
	}

	var builder strings.Builder
	builder.Grow(128)

	// 写入 INSERT [IGNORE] INTO
	if ignore {
		builder.WriteString(_SQL_INSERT_IGNORE_INTO)
	} else {
		builder.WriteString(_SQL_INSERT_INTO)
	}

	// 写入数据表名
	builder.WriteString(own.QuoteIdentifier(table))

	// 插入指定字段
	if len(columns) > 0 {
		builder.WriteString(" (")
		own.joinColumns(&builder, columns...)
		builder.WriteByte(')')
	}

	sql := builder.String()
	own.insertCache.Store(key, sql)
	return ctx.WriteString(sql)
}

// BuildReplaceSQL 构造 REPLACE 子句
func (own *mysql) BuildReplaceSQL(ctx *Context, table string, columns ...string) error {
	key := table + "1"
	if len(columns) == 0 {
		key = table + "0"
	}
	if sql, ok := own.replaceCache.Load(key); ok {
		return ctx.WriteString(sql.(string))
	}

	var builder strings.Builder
	builder.Grow(128)

	// 写入 REPLACE INTO
	builder.WriteString(_SQL_REPLACE_INTO)

	// 写入数据表名
	builder.WriteString(own.QuoteIdentifier(table))

	// 插入指定字段
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
func (own *mysql) BuildValuesSQL(ctx *Context, _ int, values ...[]any) error {
	if len(values) == 0 {
		return ErrValuesIsEmpty
	}

	rows := len(values)
	cols := len(values[0])
	key := uint64(rows)<<32 | uint64(cols)
	if sql, ok := own.valuesCache.Load(key); ok {
		for _, value := range values {
			ctx.AddArgs(value...)
		}

		return ctx.WriteString(sql.(string))
	}

	var builder strings.Builder
	builder.Grow(rows * (cols*3 + 8))

	// 写入 VALUES
	builder.WriteString(_SQL_VALUES)

	// 写入数据
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

// BuildDuplicateSQL 构造 ON DUPLICATE KEY UPDATE 子句（用于插入数据，唯一键冲突时的处理）
func (own *mysql) BuildDuplicateSQL(ctx *Context, columns ...string) error {
	// 用不可见分隔符拼接列名，避免 (a, bc) 与 (ab, c) 生成相同的缓存键
	key := strings.Join(columns, "\x1f")
	if sql, ok := own.duplicateCache.Load(key); ok {
		return ctx.WriteString(sql.(string))
	}

	var builder strings.Builder
	builder.Grow(len(columns) * 32)

	// 写入 ON DUPLICATE KEY UPDATE
	builder.WriteString(_SQL_DUPLICATE_UPDATE)

	// 写入更新字段
	for i, column := range columns {
		if i > 0 {
			builder.WriteString(_SQL_COMMA_AND_LINE_BREAK_AND_4SPACE)
		}

		column = own.QuoteIdentifier(column)
		builder.WriteString(column)
		builder.WriteString(_SQL_VALUES_WHEN_DUPLICATE_UPDATE)
		builder.WriteString(column)
		builder.WriteByte(')')
	}

	sql := builder.String()
	own.duplicateCache.Store(key, sql)
	return ctx.WriteString(sql)
}

// BuildUpdateSQL 构造 UPDATE 子句
func (own *mysql) BuildUpdateSQL(ctx *Context, table string) error {
	// 查缓存
	if cache, ok := own.updateCache.Load(table); ok {
		return ctx.WriteString(cache.(string))
	}

	// 生成 DELETE 子句，并放入缓存
	sql := _SQL_UPDATE + own.QuoteIdentifier(table)
	own.updateCache.Store(table, sql)
	return ctx.WriteString(sql)
}

// BuildSetSQL 构造 SET 子句
func (own *mysql) BuildSetSQL(ctx *Context, _ int, values map[string]any) error {
	// 写入 SET
	_ = ctx.WriteString(_SQL_SET)

	// 按字段名排序，保证同一份数据生成的 SQL 文本稳定（便于预编译缓存与日志对比）
	columns := sortMapKeys(values)

	// 赋值 `column1` = ?, `column2` = ?, ...
	for i, column := range columns {
		if i > 0 {
			_ = ctx.WriteString(", ")
		}

		_ = ctx.WriteString(own.QuoteIdentifier(column))
		_ = ctx.WriteString(" = ?")
		ctx.AddArgs(values[column])
	}

	return nil
}

// BuildDeleteSQL 构造 DELETE 子句
func (own *mysql) BuildDeleteSQL(ctx *Context, table string) error {
	// 查缓存
	if cache, ok := own.deleteCache.Load(table); ok {
		return ctx.WriteString(cache.(string))
	}

	// 生成 DELETE 子句，并放入缓存
	sql := _SQL_DELETE_FROM + own.QuoteIdentifier(table)
	own.deleteCache.Store(table, sql)
	return ctx.WriteString(sql)
}

// BuildSelectSQL 构造 SELECT 子句
func (own *mysql) BuildSelectSQL(ctx *Context, table string, columns ...string) error {
	// 缓存键必须包含字段列表，同一张表的不同字段集不能共用一条 SQL
	key := table + "\x1f" + strings.Join(columns, "\x1f")
	if sql, ok := own.selectCache.Load(key); ok {
		return ctx.WriteString(sql.(string))
	}

	var builder strings.Builder
	builder.Grow(len(columns) * 16)

	// 写入 SELECT
	builder.WriteString(_SQL_SELECT)

	// 写入字段列表
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
func (own *mysql) BuildCountSQL(ctx *Context, column ...string) error {
	// 写入 SELECT
	_ = ctx.WriteString(_SQL_SELECT_COUNT)

	// 写入统计字段 或 *
	if len(column) == 0 {
		_ = ctx.WriteByte('*')
	} else {
		_ = ctx.WriteString(own.QuoteIdentifier(column[0]))
	}

	return ctx.WriteByte(')')
}

// BuildFromSQL 构造 FROM 子句
func (own *mysql) BuildFromSQL(ctx *Context, table string) error {
	// 查缓存
	if cache, ok := own.fromCache.Load(table); ok {
		return ctx.WriteString(cache.(string))
	}

	// 生成 FROM 子句，并放入缓存
	sql := _SQL_FROM + own.QuoteIdentifier(table)
	own.fromCache.Store(table, sql)
	return ctx.WriteString(sql)
}

// BuildJoinSQL 构造 JOIN 子句
//
// MySQL 不支持 FULL JOIN，其余类型与标准 SQL 一致。
func (own *mysql) BuildJoinSQL(ctx *Context, kind JoinKind, table, alias string) error {
	if err := CheckJoinKind(kind); err != nil {
		return err
	}

	if !own.SupportsJoinKind(kind) {
		return NewUnsupportedJoinError(kind)
	}

	return BuildJoinClause(ctx, own, kind, table, alias)
}

// SupportsJoinKind 判断 MySQL 是否支持该 JOIN 类型
func (own *mysql) SupportsJoinKind(kind JoinKind) bool {
	switch kind {
	case JOIN_INNER, JOIN_LEFT, JOIN_RIGHT, JOIN_CROSS:
		return true
	default:
		return false
	}
}

// BuildWhereSQL 构造 WHERE 子句
func (own *mysql) BuildWhereSQL(ctx *Context) error {
	return ctx.WriteString(_SQL_WHERE)
}

// BuildGroupBySQL 构造 GROUP BY 子句
func (own *mysql) BuildGroupBySQL(ctx *Context, columns ...string) error {
	// 写入 GROUP BY
	_ = ctx.WriteString(_SQL_GROUP_BY)
	// 写入字段列表
	own.joinColumns(&ctx.builder, columns...)
	return nil
}

// BuildHavingSQL 构造 HAVING 子句
func (own *mysql) BuildHavingSQL(ctx *Context) error {
	return ctx.WriteString(_SQL_HAVING)
}

// BuildOrderBySQL 构造 ORDER BY 子句
func (own *mysql) BuildOrderBySQL(ctx *Context, columns ...string) error {
	// 写入 ORDER BY
	_ = ctx.WriteString(_SQL_ORDER_BY)
	// 写入字段列表
	own.joinColumns(&ctx.builder, columns...)
	return nil
}

// BuildLimitSQL 构造 LIMIT 子句
//
// 不缓存结果：LIMIT 的取值来自分页参数，可能的取值无界，
// 以 int 为键的缓存会随分页深度持续增长，而拼一个整数远比一次 map 查找便宜。
func (own *mysql) BuildLimitSQL(ctx *Context, limit int) error {
	ctx.MarkLimit()

	if err := ctx.WriteString(_SQL_LIMIT); err != nil {
		return err
	}

	return ctx.WriteInt(limit)
}

// BuildOffsetSQL 构造 OFFSET 子句
//
// 同样不缓存，理由与 BuildLimitSQL 一致。
func (own *mysql) BuildOffsetSQL(ctx *Context, offset int) error {
	if err := ctx.WriteString(_SQL_OFFSET); err != nil {
		return err
	}

	return ctx.WriteInt(offset)
}

/*
BuildJSONExtractSQL 构造检查 JSON 类型字段是否存在特定路径的值的查询语句

例如，创建数据库，包含一个json字段：

	CREATE TABLE users (
		id INT PRIMARY KEY,
		name VARCHAR(50),
		metadata JSON  -- 存储JSON数据
	);

metadata 字段内容示例：
  - {"settings": {"theme": "dark", "notifications": true}, "tags": ["golang", "backend", "database"]}

查询 settings.theme 为 dark 的用户：
  - sql: JSON_EXTRACT(`metadata`, '$.settings.theme') = ?

查询数组中的元素：
  - sql: JSON_EXTRACT(`metadata`, '$.tags[0]') = ?
*/
func (own *mysql) BuildJSONExtractSQL(ctx *Context, column, path string) error {
	key := column + "\x1f" + path
	if cache, ok := own.jsonExtractCache.Load(key); ok {
		return ctx.WriteString(cache.(string))
	}

	sql := "JSON_EXTRACT(" + own.QuoteIdentifier(column) + ", " + quoteJSONPath(path) + ")"
	own.jsonExtractCache.Store(key, sql)
	return ctx.WriteString(sql)
}

/*
BuildJSONContainsSQL  构造检查 JSON 数据的数组是否包含某个值的查询语句

metadata 字段内容示例：
  - {"settings": {"theme": "dark", "notifications": true}, "tags": ["golang", "backend", "database"]}

检查 json 对象中的某个数组是否包含特定值：
  - sql: JSON_CONTAINS(metadata->'$.tags', '"golang"');
*/
func (own *mysql) BuildJSONContainsSQL(ctx *Context, column, path string, value any) error {
	key := column + "\x1f" + path
	if cache, ok := own.jsonContainCache.Load(key); ok {
		_ = ctx.WriteString(cache.(string))
		ctx.AddArgs(value)
		return nil
	}

	sql := "JSON_CONTAINS(" + own.QuoteIdentifier(column) + ", ?, " + quoteJSONPath(path) + ")"
	own.jsonContainCache.Store(key, sql)
	_ = ctx.WriteString(sql)
	ctx.AddArgs(value)
	return nil
}

// normalizeJSONPath 补全 JSON 路径前缀；空路径表示根节点
func normalizeJSONPath(path string) string {
	if path == "" {
		return "$"
	}

	if strings.HasPrefix(path, "$") {
		return path
	}

	return "$." + path
}

// quoteJSONPath 把 JSON 路径包成 SQL 字符串字面量
// 路径来自调用方输入，必须转义单引号，否则可以通过 ' 闭合字面量拼接任意 SQL
func quoteJSONPath(path string) string {
	return "'" + strings.ReplaceAll(normalizeJSONPath(path), "'", "''") + "'"
}

// fullTextModes 列出 MySQL 允许的全文检索模式，避免把调用方传入的 mode 原样拼进 SQL
var fullTextModes = map[string]string{
	"":                         "",
	"IN NATURAL LANGUAGE MODE": "IN NATURAL LANGUAGE MODE",
	"IN NATURAL LANGUAGE MODE WITH QUERY EXPANSION": "IN NATURAL LANGUAGE MODE WITH QUERY EXPANSION",
	"IN BOOLEAN MODE":      "IN BOOLEAN MODE",
	"WITH QUERY EXPANSION": "WITH QUERY EXPANSION",
}

// normalizeFullTextMode 校验并归一化全文检索模式
func normalizeFullTextMode(mode string) (string, error) {
	normalized := strings.Join(strings.Fields(strings.ToUpper(mode)), " ")
	if value, ok := fullTextModes[normalized]; ok {
		return value, nil
	}

	return "", fmt.Errorf("%w: %q", ErrInvalidFullTextMode, mode)
}

// BuildFullTextSearchSQL 构造全文搜索的查询语句
func (own *mysql) BuildFullTextSearchSQL(ctx *Context, columns []string, term, mode string) error {
	mode, err := normalizeFullTextMode(mode)
	if err != nil {
		return err
	}

	_ = ctx.WriteString("MATCH(")
	for i, column := range columns {
		if i > 0 {
			_ = ctx.WriteString(", ")
		}

		_ = ctx.WriteString(own.QuoteIdentifier(column))
	}

	_ = ctx.WriteString(") AGAINST(")
	if err := ctx.WritePlaceholderWithArg(own, term); err != nil {
		return err
	}

	if mode != "" {
		_ = ctx.WriteString(" ")
		_ = ctx.WriteString(mode)
	}

	return ctx.WriteByte(')')
}

func (own *mysql) joinColumns(builder *strings.Builder, columns ...string) {
	for i := 0; i < len(columns); i++ {
		if i > 0 {
			builder.WriteString(", ")
		}

		builder.WriteString(own.QuoteIdentifier(columns[i]))
	}
}
