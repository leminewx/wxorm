/*
Package fakedriver 提供 database/sql 的内存驱动，用于在没有真实数据库时运行测试。

驱动注册为 mysql，实现了 wxorm 产生的 SQL 子集：

  - CREATE TABLE / DROP TABLE / ALTER TABLE ADD|DROP COLUMN / CREATE INDEX
  - INSERT [IGNORE] INTO ... VALUES (...), (...)
  - SELECT ... FROM ... [WHERE] [GROUP BY] [ORDER BY] [LIMIT] [OFFSET]
  - SELECT COUNT(*) / COUNT(DISTINCT col)
  - UPDATE ... SET ... WHERE ...
  - DELETE FROM ... WHERE ...
  - information_schema.tables 的表存在性与表属性查询
  - information_schema.columns 的列名查询、information_schema.statistics 的索引名查询
  - 事务（提交写回，回滚丢弃）与单列唯一约束

它是测试基础设施，不参与运行时代码：只在测试的 TestMain 中注册。
*/
package fakedriver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var registerOnce sync.Once

// Register 注册名为 mysql 的内存驱动（可重复调用）
func Register() {
	registerOnce.Do(func() {
		sql.Register("mysql", &fakeDriver{})
	})
}

// databaseFor 返回 DSN 对应的内存库，相同 DSN 共享同一份数据
func databaseFor(dsn string) *database {
	registry.Lock()
	defer registry.Unlock()

	db, ok := registry.dbs[dsn]
	if !ok {
		db = &database{tables: make(map[string]*table)}
		registry.dbs[dsn] = db
	}

	return db
}

var registry = struct {
	sync.Mutex
	dbs map[string]*database
}{dbs: make(map[string]*database)}

type fakeDriver struct{}

func (own *fakeDriver) Open(dsn string) (driver.Conn, error) {
	return &conn{db: databaseFor(dsn)}, nil
}

// ---------------------------------------------------------------------------
// 连接、语句、结果与事务
// ---------------------------------------------------------------------------

type conn struct {
	mu     sync.Mutex // 串行化同一连接上的操作，同时保护 txData
	db     *database
	txData *database
}

func (own *conn) Prepare(query string) (driver.Stmt, error) {
	return &stmt{conn: own, query: query}, nil
}

func (own *conn) Close() error { return nil }

func (own *conn) Begin() (driver.Tx, error) {
	return own.BeginTx(context.Background(), driver.TxOptions{})
}

func (own *conn) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	own.mu.Lock()
	defer own.mu.Unlock()

	// 事务期间所有读写都落在这份快照上，提交时写回，回滚时丢弃
	own.db.mu.Lock()
	own.txData = own.db.clone()
	own.db.mu.Unlock()

	return &tx{conn: own}, nil
}

// lockState 返回本次操作要访问的数据库状态以及释放锁的函数
//
// 加锁顺序固定为 conn.mu -> db.mu，避免与 Commit 交叉死锁。
func (own *conn) lockState() (*database, func()) {
	own.mu.Lock()
	if own.txData != nil {
		return own.txData, own.mu.Unlock
	}

	own.db.mu.Lock()
	return own.db, func() {
		own.db.mu.Unlock()
		own.mu.Unlock()
	}
}

func (own *conn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	state, unlock := own.lockState()
	defer unlock()

	return state.exec(query, namedValues(args))
}

func (own *conn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	state, unlock := own.lockState()
	defer unlock()

	return state.query(query, namedValues(args))
}

func (own *conn) CheckNamedValue(*driver.NamedValue) error { return nil }

func (own *conn) state() *database {
	if own.txData != nil {
		return own.txData
	}

	return own.db
}

type tx struct{ conn *conn }

func (own *tx) Commit() error {
	own.conn.mu.Lock()
	defer own.conn.mu.Unlock()

	if own.conn.txData == nil {
		return nil
	}

	own.conn.db.mu.Lock()
	defer own.conn.db.mu.Unlock()

	own.conn.db.replaceWith(own.conn.txData)
	own.conn.txData = nil
	return nil
}

func (own *tx) Rollback() error {
	own.conn.mu.Lock()
	defer own.conn.mu.Unlock()

	own.conn.txData = nil
	return nil
}

type stmt struct {
	conn  *conn
	query string
}

func (own *stmt) Close() error  { return nil }
func (own *stmt) NumInput() int { return -1 }

func (own *stmt) Exec(args []driver.Value) (driver.Result, error) {
	state, unlock := own.conn.lockState()
	defer unlock()

	return state.exec(own.query, args)
}

func (own *stmt) Query(args []driver.Value) (driver.Rows, error) {
	state, unlock := own.conn.lockState()
	defer unlock()

	return state.query(own.query, args)
}

type result struct {
	lastID   int64
	affected int64
}

func (own result) LastInsertId() (int64, error) { return own.lastID, nil }
func (own result) RowsAffected() (int64, error) { return own.affected, nil }

type rows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (own *rows) Columns() []string { return own.cols }
func (own *rows) Close() error      { return nil }

func (own *rows) Next(dest []driver.Value) error {
	if own.pos >= len(own.data) {
		return io.EOF
	}

	copy(dest, own.data[own.pos])
	own.pos++
	return nil
}

func namedValues(args []driver.NamedValue) []driver.Value {
	values := make([]driver.Value, len(args))
	for i, arg := range args {
		values[i] = arg.Value
	}

	return values
}

// ---------------------------------------------------------------------------
// 内存库与表
// ---------------------------------------------------------------------------

type database struct {
	mu     sync.Mutex
	tables map[string]*table
}

type table struct {
	columns  []string
	uniques  map[string]bool
	indexes  map[string]bool
	defaults map[string]columnDefault
	rows     [][]driver.Value
	auto     int
	nextID   int64
	meta     map[string]string
}

// columnDefault 描述字段的 DEFAULT 值
//
// 真实 MySQL 会在插入时填充默认值，内存驱动需要模拟这一行为，
// 否则 DEFAULT CURRENT_TIMESTAMP 的字段会一直为 NULL。
type columnDefault struct {
	timestamp bool         // DEFAULT CURRENT_TIMESTAMP
	value     driver.Value // 其余字面量默认值
}

func (own *database) clone() *database {
	cloned := &database{tables: make(map[string]*table, len(own.tables))}
	for name, tbl := range own.tables {
		copied := *tbl
		copied.columns = append([]string(nil), tbl.columns...)
		copied.meta = make(map[string]string, len(tbl.meta))
		for key, value := range tbl.meta {
			copied.meta[key] = value
		}

		copied.uniques = make(map[string]bool, len(tbl.uniques))
		for column := range tbl.uniques {
			copied.uniques[column] = true
		}

		copied.indexes = make(map[string]bool, len(tbl.indexes))
		for name := range tbl.indexes {
			copied.indexes[name] = true
		}

		copied.defaults = make(map[string]columnDefault, len(tbl.defaults))
		for column, value := range tbl.defaults {
			copied.defaults[column] = value
		}

		copied.rows = make([][]driver.Value, len(tbl.rows))
		for i, row := range tbl.rows {
			copied.rows[i] = append([]driver.Value(nil), row...)
		}

		cloned.tables[name] = &copied
	}

	return cloned
}

func (own *database) replaceWith(other *database) {
	own.tables = other.tables
}

func (own *database) exec(query string, args []driver.Value) (driver.Result, error) {
	text := normalizeSQL(query)

	switch {
	case strings.HasPrefix(text, "CREATE TABLE"):
		return own.createTable(text)
	case strings.HasPrefix(text, "DROP TABLE"):
		return own.dropTable(text)
	case strings.HasPrefix(text, "ALTER TABLE"):
		return own.alterTable(text)
	case strings.HasPrefix(text, "CREATE INDEX"):
		return own.createIndex(text)
	case strings.HasPrefix(text, "INSERT"):
		return own.insert(text, args)
	case strings.HasPrefix(text, "UPDATE"):
		return own.update(text, args)
	case strings.HasPrefix(text, "DELETE"):
		return own.delete(text, args)
	default:
		return nil, fmt.Errorf("fake: unsupported exec statement: %s", text)
	}
}

func (own *database) query(query string, args []driver.Value) (driver.Rows, error) {
	text := normalizeSQL(query)

	switch {
	case strings.Contains(strings.ToLower(text), "information_schema"):
		return own.queryInformationSchema(text, args)
	case strings.HasPrefix(text, "SELECT"):
		return own.selectRows(text, args)
	default:
		return nil, fmt.Errorf("fake: unsupported query statement: %s", text)
	}
}

// createIndex 记录索引名
//
// 内存驱动不维护真正的索引结构，只记录名字，供迁移时判断索引是否已存在。
func (own *database) createIndex(text string) (driver.Result, error) {
	name, tableName, err := parseCreateIndex(text)
	if err != nil {
		return nil, err
	}

	tbl, ok := own.tables[tableName]
	if !ok {
		return nil, fmt.Errorf("fake: table %s does not exist", tableName)
	}

	if tbl.indexes == nil {
		tbl.indexes = make(map[string]bool)
	}
	tbl.indexes[name] = true

	return driver.RowsAffected(0), nil
}

// parseCreateIndex 解析 CREATE INDEX <name> ON <table> (<columns>)
func parseCreateIndex(text string) (name, tableName string, err error) {
	const prefix = "CREATE INDEX "
	if !strings.HasPrefix(text, prefix) {
		return "", "", fmt.Errorf("fake: invalid create index statement: %s", text)
	}

	rest := text[len(prefix):]
	onIdx := strings.Index(strings.ToUpper(rest), " ON ")
	if onIdx < 0 {
		return "", "", fmt.Errorf("fake: invalid create index statement: %s", text)
	}

	name = trimIdent(strings.TrimSpace(rest[:onIdx]))
	tablePart := strings.TrimSpace(rest[onIdx+len(" ON "):])
	if open := strings.Index(tablePart, " ("); open >= 0 {
		tablePart = tablePart[:open]
	}

	tableName = trimIdent(tablePart)
	if idx := strings.LastIndexAny(tableName, ".`"); idx >= 0 {
		tableName = trimIdent(tableName[idx+1:])
	}

	return name, tableName, nil
}

func (own *database) createTable(text string) (driver.Result, error) {
	name, body, options, err := parseCreateTable(text)
	if err != nil {
		return nil, err
	}

	tbl := &table{
		uniques:  make(map[string]bool),
		indexes:  make(map[string]bool),
		defaults: make(map[string]columnDefault),
		auto:     -1,
		nextID:   1,
		meta:     parseTableMeta(options),
	}

	for _, definition := range splitTopLevel(body, ',') {
		definition = strings.TrimSpace(definition)
		if definition == "" || strings.HasPrefix(definition, "--") {
			continue
		}

		lower := strings.ToUpper(definition)
		if strings.HasPrefix(lower, "PRIMARY KEY") || strings.HasPrefix(lower, "INDEX") ||
			strings.HasPrefix(lower, "CONSTRAINT") || strings.HasPrefix(lower, "FOREIGN KEY") ||
			strings.HasPrefix(lower, "UNIQUE KEY") {
			continue
		}

		fields := strings.Fields(definition)
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "`") {
			continue
		}

		column := trimIdent(fields[0])
		if strings.Contains(lower, " AUTO_INCREMENT") {
			tbl.auto = len(tbl.columns)
		}
		if strings.Contains(lower, " UNIQUE") {
			tbl.uniques[column] = true
		}
		if current, ok := parseColumnDefault(definition); ok {
			tbl.defaults[column] = current
		}

		tbl.columns = append(tbl.columns, column)
	}

	own.tables[name] = tbl
	return result{}, nil
}

func (own *database) dropTable(text string) (driver.Result, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return nil, fmt.Errorf("fake: invalid drop statement: %s", text)
	}

	name := trimIdent(fields[len(fields)-1])
	delete(own.tables, name)
	return result{}, nil
}

func (own *database) alterTable(text string) (driver.Result, error) {
	fields := strings.Fields(text)
	if len(fields) < 4 {
		return nil, fmt.Errorf("fake: invalid alter statement: %s", text)
	}

	name := trimIdent(fields[2])
	tbl, ok := own.tables[name]
	if !ok {
		return nil, fmt.Errorf("fake: table %s does not exist", name)
	}

	upper := strings.ToUpper(text)
	switch {
	case strings.Contains(upper, " ADD COLUMN "):
		idx := strings.Index(upper, " ADD COLUMN ")
		definition := strings.TrimSpace(text[idx+len(" ADD COLUMN "):])
		if column := trimIdent(strings.Fields(definition)[0]); column != "" {
			tbl.columns = append(tbl.columns, column)
			for i := range tbl.rows {
				tbl.rows[i] = append(tbl.rows[i], nil)
			}
			if strings.Contains(strings.ToUpper(definition), "AUTO_INCREMENT") {
				tbl.auto = len(tbl.columns) - 1
			}
			if current, ok := parseColumnDefault(definition); ok {
				tbl.defaults[column] = current
			}
		}
	case strings.Contains(upper, " DROP COLUMN "):
		idx := strings.Index(upper, " DROP COLUMN ")
		column := trimIdent(strings.Fields(text[idx+len(" DROP COLUMN "):])[0])
		pos := tbl.columnIndex(column)
		if pos < 0 {
			return nil, fmt.Errorf("fake: column %s does not exist", column)
		}

		tbl.columns = append(tbl.columns[:pos], tbl.columns[pos+1:]...)
		for i := range tbl.rows {
			tbl.rows[i] = append(tbl.rows[i][:pos], tbl.rows[i][pos+1:]...)
		}
	default:
		return nil, fmt.Errorf("fake: unsupported alter statement: %s", text)
	}

	return result{}, nil
}

func (own *database) insert(text string, args []driver.Value) (driver.Result, error) {
	ignore := strings.HasPrefix(strings.ToUpper(text), "INSERT IGNORE")
	name, columns, err := parseInsert(text)
	if err != nil {
		return nil, err
	}

	tbl, ok := own.tables[name]
	if !ok {
		return nil, fmt.Errorf("fake: table %s does not exist", name)
	}

	if len(columns) == 0 {
		columns = append([]string(nil), tbl.columns...)
	}

	rowsCount := 0
	if len(columns) > 0 {
		rowsCount = len(args) / len(columns)
	}

	var firstID, inserted int64
	for row := 0; row < rowsCount; row++ {
		values := make([]driver.Value, len(tbl.columns))
		for i, column := range columns {
			pos := tbl.columnIndex(column)
			if pos >= 0 {
				values[pos] = args[row*len(columns)+i]
			}
		}

		// 填充未指定的 DEFAULT 值，与真实 MySQL 保持一致
		for i, column := range tbl.columns {
			if values[i] != nil {
				continue
			}

			current, ok := tbl.defaults[column]
			if !ok {
				continue
			}
			if current.timestamp {
				values[i] = time.Now()
				continue
			}

			values[i] = current.value
		}

		if tbl.auto >= 0 && (values[tbl.auto] == nil || isZeroNumber(values[tbl.auto])) {
			values[tbl.auto] = tbl.nextID
			tbl.nextID++
			if firstID == 0 {
				firstID = values[tbl.auto].(int64)
			}
		}

		if tbl.hasUniqueConflict(values) {
			if ignore {
				continue
			}

			return nil, fmt.Errorf("fake: duplicate entry for unique column in table %s", name)
		}

		tbl.rows = append(tbl.rows, values)
		inserted++
	}

	return result{lastID: firstID, affected: inserted}, nil
}

func (own *database) update(text string, args []driver.Value) (driver.Result, error) {
	name, sets, condition, err := parseUpdate(text)
	if err != nil {
		return nil, err
	}

	tbl, ok := own.tables[name]
	if !ok {
		return nil, fmt.Errorf("fake: table %s does not exist", name)
	}

	position := 0
	type assignment struct {
		index int
		value driver.Value
	}

	assignments := make([]assignment, 0, len(sets))
	for _, set := range sets {
		if position >= len(args) {
			return nil, fmt.Errorf("fake: not enough args for update: %s", text)
		}

		assignments = append(assignments, assignment{index: tbl.columnIndex(set), value: args[position]})
		position++
	}

	var affected int64
	for _, row := range tbl.rows {
		// 每行都从 SET 参数之后重新开始消费 WHERE 参数
		at := position
		ok, err := matchWhere(condition, tbl, row, args, &at)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}

		for _, item := range assignments {
			if item.index >= 0 {
				row[item.index] = item.value
			}
		}
		affected++
	}

	return result{affected: affected}, nil
}

func (own *database) delete(text string, args []driver.Value) (driver.Result, error) {
	name, condition, err := parseDelete(text)
	if err != nil {
		return nil, err
	}

	tbl, ok := own.tables[name]
	if !ok {
		return nil, fmt.Errorf("fake: table %s does not exist", name)
	}

	position := 0
	kept := make([][]driver.Value, 0, len(tbl.rows))
	var affected int64
	for _, row := range tbl.rows {
		at := position
		ok, err := matchWhere(condition, tbl, row, args, &at)
		if err != nil {
			return nil, err
		}
		if ok {
			affected++
			continue
		}

		kept = append(kept, row)
	}

	tbl.rows = kept
	return result{affected: affected}, nil
}

func (own *database) queryInformationSchema(text string, args []driver.Value) (driver.Rows, error) {
	tableName := informationSchemaTableName(text, args)

	// information_schema.columns：按列定义顺序返回列名
	if strings.Contains(strings.ToLower(text), "information_schema.columns") {
		return own.queryInformationSchemaColumns(text, tableName), nil
	}

	// information_schema.statistics：返回已创建的索引名
	if strings.Contains(strings.ToLower(text), "information_schema.statistics") {
		return own.queryInformationSchemaIndexes(tableName), nil
	}

	// 按 SELECT 列表返回对应的列，未请求的列不返回
	columns := []string{"table_name"}
	if fromIdx := indexKeyword(text, "FROM"); fromIdx > 0 {
		selection := strings.TrimSpace(text[len("SELECT"):fromIdx])
		if selection != "*" && selection != "" {
			columns = columns[:0]
			for _, item := range splitTopLevel(selection, ',') {
				columns = append(columns, trimIdent(strings.TrimSpace(item)))
			}
		}
	}

	tbl, exists := own.tables[tableName]
	if !exists {
		return &rows{cols: columns}, nil
	}

	values := make([]driver.Value, 0, len(columns))
	for _, column := range columns {
		switch strings.ToUpper(column) {
		case "TABLE_NAME":
			values = append(values, tableName)
		case "ENGINE":
			values = append(values, tbl.meta["ENGINE"])
		case "TABLE_COLLATION":
			values = append(values, tbl.meta["COLLATE"])
		case "TABLE_COMMENT":
			values = append(values, tbl.meta["COMMENT"])
		default:
			values = append(values, nil)
		}
	}

	return &rows{cols: columns, data: [][]driver.Value{values}}, nil
}

// informationSchemaTableName 从查询语句或参数中取出被查询的表名
//
// wxorm 把库名与表名都改成了占位符，表名不再出现于 SQL 文本：
// 两个条件时参数依次是 [namespace, table]，只有一个条件时就是 [table]。
// 手写 SQL 仍可能用字面量，因此先尝试正则，再回落到参数。
func informationSchemaTableName(text string, args []driver.Value) string {
	if match := tableNamePattern.FindStringSubmatch(text); len(match) == 2 {
		return strings.ReplaceAll(match[1], "''", "'")
	}

	if len(args) == 0 {
		return ""
	}

	if name, ok := args[len(args)-1].(string); ok {
		return name
	}

	return ""
}

// queryInformationSchemaColumns 返回 information_schema.columns 的查询结果
//
// 只需支持 wxorm 迁移使用的一种形式：按表名筛选并按 ordinal_position 返回 column_name。
func (own *database) queryInformationSchemaColumns(text, tableName string) *rows {
	columns := []string{"column_name"}
	if fromIdx := indexKeyword(text, "FROM"); fromIdx > 0 {
		selection := strings.TrimSpace(text[len("SELECT"):fromIdx])
		if selection != "*" && selection != "" {
			columns = columns[:0]
			for _, item := range splitTopLevel(selection, ',') {
				columns = append(columns, trimIdent(strings.TrimSpace(item)))
			}
		}
	}

	tbl, exists := own.tables[tableName]
	if !exists {
		return &rows{cols: columns}
	}

	data := make([][]driver.Value, 0, len(tbl.columns))
	for _, column := range tbl.columns {
		values := make([]driver.Value, 0, len(columns))
		for _, requested := range columns {
			if strings.EqualFold(requested, "column_name") {
				values = append(values, column)
				continue
			}

			values = append(values, nil)
		}

		data = append(data, values)
	}

	return &rows{cols: columns, data: data}
}

// queryInformationSchemaIndexes 返回 information_schema.statistics 的查询结果
//
// 只需支持 wxorm 迁移使用的一种形式：按表名筛选并返回 index_name。
func (own *database) queryInformationSchemaIndexes(tableName string) *rows {
	tbl, exists := own.tables[tableName]
	if !exists {
		return &rows{cols: []string{"index_name"}}
	}

	names := make([]string, 0, len(tbl.indexes))
	for name := range tbl.indexes {
		names = append(names, name)
	}
	sort.Strings(names)

	data := make([][]driver.Value, 0, len(names))
	for _, name := range names {
		data = append(data, []driver.Value{name})
	}

	return &rows{cols: []string{"index_name"}, data: data}
}

func (own *database) selectRows(text string, args []driver.Value) (driver.Rows, error) {
	selection, name, condition, groupBy, orderBy, limit, offset, err := parseSelect(text)
	if err != nil {
		return nil, err
	}

	tbl, ok := own.tables[name]
	if !ok {
		return nil, fmt.Errorf("fake: table %s does not exist", name)
	}

	matched := make([][]driver.Value, 0, len(tbl.rows))
	for _, row := range tbl.rows {
		at := 0
		ok, err := matchWhere(condition, tbl, row, args, &at)
		if err != nil {
			return nil, err
		}
		if ok {
			matched = append(matched, row)
		}
	}

	// 统计查询
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(selection)), "COUNT(") {
		count := int64(len(matched))
		if distinct := distinctColumn(selection); distinct != "" {
			seen := make(map[string]struct{}, len(matched))
			idx := tbl.columnIndex(distinct)
			for _, row := range matched {
				if idx < 0 {
					continue
				}

				seen[fmt.Sprint(row[idx])] = struct{}{}
			}
			count = int64(len(seen))
		}

		return &rows{cols: []string{"COUNT(*)"}, data: [][]driver.Value{{count}}}, nil
	}

	// 投影列
	projected := make([]int, 0, len(tbl.columns))
	columns := tbl.columns
	if selection != "*" {
		columns = nil
		for _, item := range splitTopLevel(selection, ',') {
			column := trimIdent(strings.TrimSpace(item))
			columns = append(columns, column)
			projected = append(projected, tbl.columnIndex(column))
		}
	} else {
		for i := range tbl.columns {
			projected = append(projected, i)
		}
	}

	// 分组：取每组第一行
	if groupBy != "" {
		seen := make(map[string]struct{}, len(matched))
		grouped := make([][]driver.Value, 0, len(matched))
		for _, row := range matched {
			key := ""
			for _, column := range splitTopLevel(groupBy, ',') {
				if idx := tbl.columnIndex(trimIdent(strings.TrimSpace(column))); idx >= 0 {
					key += fmt.Sprint(row[idx]) + "\x1f"
				}
			}

			if _, ok := seen[key]; ok {
				continue
			}

			seen[key] = struct{}{}
			grouped = append(grouped, row)
		}

		matched = grouped
	}

	// 排序
	if orderBy != "" {
		sortRows(tbl, matched, orderBy)
	}

	// 分页
	if offset > 0 {
		if offset >= len(matched) {
			matched = nil
		} else {
			matched = matched[offset:]
		}
	}
	if limit >= 0 && limit < len(matched) {
		matched = matched[:limit]
	}

	data := make([][]driver.Value, 0, len(matched))
	for _, row := range matched {
		item := make([]driver.Value, 0, len(projected))
		for _, idx := range projected {
			if idx >= 0 {
				item = append(item, row[idx])
			}
		}

		data = append(data, item)
	}

	return &rows{cols: columns, data: data}, nil
}

// ---------------------------------------------------------------------------
// 表级工具
// ---------------------------------------------------------------------------

func (own *table) columnIndex(name string) int {
	for i, column := range own.columns {
		if column == name {
			return i
		}
	}

	return -1
}

func (own *table) hasUniqueConflict(values []driver.Value) bool {
	for column := range own.uniques {
		idx := own.columnIndex(column)
		if idx < 0 || values[idx] == nil {
			continue
		}

		for _, row := range own.rows {
			if equalValue(row[idx], values[idx]) {
				return true
			}
		}
	}

	return false
}

func sortRows(tbl *table, data [][]driver.Value, orderBy string) {
	type item struct {
		index int
		desc  bool
	}

	items := make([]item, 0, 4)
	for _, part := range splitTopLevel(orderBy, ',') {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 0 {
			continue
		}

		current := item{index: tbl.columnIndex(trimIdent(fields[0]))}
		if len(fields) > 1 {
			current.desc = strings.EqualFold(fields[1], "DESC")
		}

		items = append(items, current)
	}

	sort.SliceStable(data, func(i, j int) bool {
		for _, current := range items {
			if current.index < 0 {
				continue
			}

			cmp := compareValues(data[i][current.index], data[j][current.index])
			if cmp == 0 {
				continue
			}
			if current.desc {
				return cmp > 0
			}

			return cmp < 0
		}

		return false
	})
}

// ---------------------------------------------------------------------------
// SQL 解析
// ---------------------------------------------------------------------------

var (
	enginePattern    = regexp.MustCompile(`ENGINE=(\w+)`)
	collatePattern   = regexp.MustCompile(`COLLATE=(\w+)`)
	commentPattern   = regexp.MustCompile(`COMMENT='((?:[^']|'')*)'`)
	tableNamePattern = regexp.MustCompile("(?i)TABLE_NAME`?\\s*=\\s*'((?:[^']|'')*)'")
)

func normalizeSQL(input string) string {
	var builder strings.Builder
	pendingSpace := false

	// 先剔除注释：wxorm 生成的多行 DDL 会在字段之间插入 "-- 文本" 注释，
	// 若不剔除，折叠空白后注释会与后面的字段拼接成同一条定义。
	inQuote := rune(0)
	for i := 0; i < len(input); i++ {
		char := rune(input[i])

		if inQuote != 0 {
			builder.WriteRune(char)
			if char == inQuote {
				inQuote = 0
			}
			continue
		}

		if char == '\'' || char == '"' || char == '`' {
			inQuote = char
			builder.WriteRune(char)
			continue
		}

		if char == '-' && i+1 < len(input) && input[i+1] == '-' {
			for i < len(input) && input[i] != '\n' {
				i++
			}
			builder.WriteByte(' ')
			continue
		}

		if char == '/' && i+1 < len(input) && input[i+1] == '*' {
			for i+1 < len(input) && !(input[i] == '*' && input[i+1] == '/') {
				i++
			}
			i++
			builder.WriteByte(' ')
			continue
		}

		builder.WriteRune(char)
	}

	stripped := builder.String()
	builder.Reset()

	for _, char := range strings.TrimSpace(stripped) {
		switch char {
		case ' ', '\n', '\r', '\t':
			pendingSpace = true
			continue
		}

		if pendingSpace && builder.Len() > 0 {
			builder.WriteByte(' ')
		}

		pendingSpace = false
		builder.WriteRune(char)
	}

	return strings.TrimSuffix(strings.TrimSpace(builder.String()), ";")
}

func parseCreateTable(text string) (name, body, options string, err error) {
	prefix := "CREATE TABLE "
	if !strings.HasPrefix(text, prefix) {
		return "", "", "", fmt.Errorf("fake: invalid create statement: %s", text)
	}

	rest := text[len(prefix):]
	open := strings.Index(rest, "(")
	close := strings.LastIndex(rest, ")")
	if open < 0 || close < open {
		return "", "", "", fmt.Errorf("fake: invalid create statement: %s", text)
	}

	name = trimIdent(strings.TrimSpace(rest[:open]))
	return name, rest[open+1 : close], rest[close+1:], nil
}

func parseTableMeta(options string) map[string]string {
	meta := make(map[string]string)
	if match := enginePattern.FindStringSubmatch(options); len(match) == 2 {
		meta["ENGINE"] = match[1]
	}
	if match := collatePattern.FindStringSubmatch(options); len(match) == 2 {
		meta["COLLATE"] = match[1]
	}

	matches := commentPattern.FindAllStringSubmatch(options, -1)
	if len(matches) > 0 {
		meta["COMMENT"] = strings.ReplaceAll(matches[len(matches)-1][1], "''", "'")
	}

	return meta
}

func parseInsert(text string) (string, []string, error) {
	rest := text
	if strings.HasPrefix(strings.ToUpper(rest), "INSERT IGNORE") {
		rest = strings.TrimSpace(rest[len("INSERT IGNORE"):])
	} else if strings.HasPrefix(strings.ToUpper(rest), "INSERT") {
		rest = strings.TrimSpace(rest[len("INSERT"):])
	}

	if !strings.HasPrefix(strings.ToUpper(rest), "INTO") {
		return "", nil, fmt.Errorf("fake: invalid insert statement: %s", text)
	}

	rest = strings.TrimSpace(rest[len("INTO"):])
	open := strings.Index(rest, "(")
	valuesIdx := strings.Index(strings.ToUpper(rest), " VALUES ")
	if valuesIdx < 0 || open < 0 || open > valuesIdx {
		return "", nil, fmt.Errorf("fake: invalid insert statement: %s", text)
	}

	name := trimIdent(strings.TrimSpace(rest[:open]))
	columnPart := rest[open+1 : strings.Index(rest, ")")]
	columns := make([]string, 0, 4)
	for _, column := range splitTopLevel(columnPart, ',') {
		if trimmed := trimIdent(strings.TrimSpace(column)); trimmed != "" {
			columns = append(columns, trimmed)
		}
	}

	return name, columns, nil
}

func parseUpdate(text string) (string, []string, string, error) {
	rest := strings.TrimSpace(text[len("UPDATE"):])
	setIdx := strings.Index(strings.ToUpper(rest), " SET ")
	if setIdx < 0 {
		return "", nil, "", fmt.Errorf("fake: invalid update statement: %s", text)
	}

	name := trimIdent(strings.TrimSpace(rest[:setIdx]))
	rest = rest[setIdx+len(" SET "):]

	whereIdx := strings.Index(strings.ToUpper(rest), " WHERE ")
	if whereIdx < 0 {
		return "", nil, "", fmt.Errorf("fake: update requires where clause: %s", text)
	}

	columns := make([]string, 0, 4)
	for _, item := range splitTopLevel(rest[:whereIdx], ',') {
		fields := strings.Fields(strings.TrimSpace(item))
		if len(fields) == 0 {
			continue
		}

		columns = append(columns, trimIdent(fields[0]))
	}

	return name, columns, rest[whereIdx+len(" WHERE "):], nil
}

func parseDelete(text string) (string, string, error) {
	rest := strings.TrimSpace(text[len("DELETE"):])
	if !strings.HasPrefix(strings.ToUpper(rest), "FROM") {
		return "", "", fmt.Errorf("fake: invalid delete statement: %s", text)
	}

	rest = strings.TrimSpace(rest[len("FROM"):])
	whereIdx := strings.Index(strings.ToUpper(rest), " WHERE ")
	if whereIdx < 0 {
		return "", "", fmt.Errorf("fake: delete requires where clause: %s", text)
	}

	return trimIdent(strings.TrimSpace(rest[:whereIdx])), rest[whereIdx+len(" WHERE "):], nil
}

func parseSelect(text string) (selection, name, condition, groupBy, orderBy string, limit, offset int, err error) {
	rest := strings.TrimSpace(text[len("SELECT"):])
	fromIdx := indexKeyword(rest, "FROM")
	if fromIdx < 0 {
		return "", "", "", "", "", -1, -1, fmt.Errorf("fake: invalid select statement: %s", text)
	}

	selection = strings.TrimSpace(rest[:fromIdx])
	sections := splitSelectClauses(rest[fromIdx+len("FROM"):])

	name = trimIdent(strings.TrimSpace(sections.table))
	condition = sections.values["WHERE"]
	groupBy = sections.values["GROUP BY"]
	orderBy = sections.values["ORDER BY"]
	limit = parseIntOr(sections.values["LIMIT"], -1)
	offset = parseIntOr(sections.values["OFFSET"], -1)
	if offset < 0 {
		offset = 0
	}

	return selection, name, condition, groupBy, orderBy, limit, offset, nil
}

// selectClauses 是 SELECT 中 FROM 之后可能出现的子句，按书写顺序列举
var selectClauses = []string{"WHERE", "GROUP BY", "ORDER BY", "LIMIT", "OFFSET"}

type selectSections struct {
	table  string
	values map[string]string
}

// splitSelectClauses 把 FROM 之后的部分切分为表名与各子句内容
func splitSelectClauses(tail string) selectSections {
	type hit struct {
		keyword string
		idx     int
	}

	hits := make([]hit, 0, len(selectClauses))
	for _, keyword := range selectClauses {
		if idx := indexKeyword(tail, keyword); idx >= 0 {
			hits = append(hits, hit{keyword: keyword, idx: idx})
		}
	}

	sort.Slice(hits, func(i, j int) bool { return hits[i].idx < hits[j].idx })

	sections := selectSections{values: make(map[string]string, len(hits))}
	sections.table = tail
	if len(hits) > 0 {
		sections.table = tail[:hits[0].idx]
	}

	for i, current := range hits {
		end := len(tail)
		if i+1 < len(hits) {
			end = hits[i+1].idx
		}

		sections.values[current.keyword] = strings.TrimSpace(tail[current.idx+len(current.keyword) : end])
	}

	return sections
}

// indexKeyword 查找以空格分隔的关键字出现位置（不区分大小写）
func indexKeyword(text, keyword string) int {
	upper := strings.ToUpper(text)
	needle := strings.ToUpper(keyword)

	offset := 0
	for {
		idx := strings.Index(upper[offset:], needle)
		if idx < 0 {
			return -1
		}

		start := offset + idx
		end := start + len(needle)
		beforeOK := start == 0 || upper[start-1] == ' '
		afterOK := end == len(upper) || upper[end] == ' '
		if beforeOK && afterOK {
			return start
		}

		offset = start + 1
	}
}

// parseColumnDefault 从字段定义中提取 DEFAULT 值
func parseColumnDefault(definition string) (columnDefault, bool) {
	idx := indexKeyword(definition, "DEFAULT")
	if idx < 0 {
		return columnDefault{}, false
	}

	rest := strings.TrimSpace(definition[idx+len("DEFAULT"):])
	if rest == "" {
		return columnDefault{}, false
	}

	if strings.HasPrefix(strings.ToUpper(rest), "CURRENT_TIMESTAMP") {
		return columnDefault{timestamp: true}, true
	}

	if rest[0] == '\'' {
		end := 1
		for end < len(rest) {
			if rest[end] == '\'' {
				// '' 是转义后的单引号，不是结束符
				if end+1 < len(rest) && rest[end+1] == '\'' {
					end += 2
					continue
				}

				return columnDefault{value: strings.ReplaceAll(rest[1:end], "''", "'")}, true
			}
			end++
		}

		return columnDefault{}, false
	}

	token := rest
	if space := strings.IndexByte(rest, ' '); space >= 0 {
		token = rest[:space]
	}

	if strings.EqualFold(token, "NULL") {
		return columnDefault{}, false
	}

	if number, err := strconv.ParseFloat(token, 64); err == nil {
		return columnDefault{value: number}, true
	}

	return columnDefault{}, false
}

func literalAfter(text, key string) string {
	lower := strings.ToLower(text)
	idx := strings.Index(lower, strings.ToLower(key))
	if idx < 0 {
		return ""
	}

	rest := text[idx+len(key):]
	open := strings.Index(rest, "'")
	if open < 0 {
		return ""
	}

	rest = rest[open+1:]
	if closeIdx := strings.Index(rest, "'"); closeIdx >= 0 {
		return strings.ReplaceAll(rest[:closeIdx], "''", "'")
	}

	return ""
}

func distinctColumn(selection string) string {
	upper := strings.ToUpper(selection)
	idx := strings.Index(upper, "DISTINCT")
	if idx < 0 {
		return ""
	}

	rest := selection[idx+len("DISTINCT"):]
	rest = strings.TrimSuffix(strings.TrimSpace(rest), ")")
	return trimIdent(strings.TrimSpace(rest))
}

func parseIntOr(input string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil {
		return fallback
	}

	return value
}

// splitTopLevel 按分隔符拆分，忽略括号与引号内的分隔符
func splitTopLevel(input string, sep rune) []string {
	var (
		parts   []string
		current strings.Builder
		depth   int
		quote   rune
	)

	for _, char := range input {
		switch {
		case quote != 0:
			current.WriteRune(char)
			if char == quote {
				quote = 0
			}
		case char == '\'' || char == '`' || char == '"':
			quote = char
			current.WriteRune(char)
		case char == '(':
			depth++
			current.WriteRune(char)
		case char == ')':
			depth--
			current.WriteRune(char)
		case char == sep && depth == 0:
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteRune(char)
		}
	}

	if trimmed := strings.TrimSpace(current.String()); trimmed != "" {
		parts = append(parts, trimmed)
	}

	return parts
}

func trimIdent(input string) string {
	return strings.Trim(strings.TrimSpace(input), "`")
}

// ---------------------------------------------------------------------------
// 条件求值
// ---------------------------------------------------------------------------

func matchWhere(condition string, tbl *table, row []driver.Value, args []driver.Value, position *int) (bool, error) {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return true, nil
	}

	condition = trimOuterParens(condition)
	if condition == "" {
		return true, nil
	}

	if parts := splitTopLevelKeyword(condition, " OR "); len(parts) > 1 {
		result := false
		for _, part := range parts {
			ok, err := matchWhere(part, tbl, row, args, position)
			if err != nil {
				return false, err
			}
			result = result || ok
		}

		return result, nil
	}

	if parts := splitTopLevelKeyword(condition, " AND "); len(parts) > 1 {
		result := true
		for _, part := range parts {
			ok, err := matchWhere(part, tbl, row, args, position)
			if err != nil {
				return false, err
			}
			result = result && ok
		}

		return result, nil
	}

	upper := strings.ToUpper(condition)
	if strings.HasPrefix(upper, "NOT ") {
		ok, err := matchWhere(condition[len("NOT "):], tbl, row, args, position)
		return !ok, err
	}

	if condition == "1=1" {
		return true, nil
	}
	if condition == "1=0" {
		return false, nil
	}

	return matchAtom(condition, tbl, row, args, position)
}

func matchAtom(condition string, tbl *table, row []driver.Value, args []driver.Value, position *int) (bool, error) {
	upper := strings.ToUpper(condition)

	// IS [NOT] NULL
	if idx := strings.Index(upper, " IS NULL"); idx > 0 {
		return valueOf(tbl, row, trimIdent(condition[:idx])) == nil, nil
	}
	if idx := strings.Index(upper, " IS NOT NULL"); idx > 0 {
		return valueOf(tbl, row, trimIdent(condition[:idx])) != nil, nil
	}

	// [NOT] IN (...)
	for _, keyword := range []string{" NOT IN ", " IN "} {
		if idx := strings.Index(upper, keyword); idx > 0 {
			negate := strings.Contains(keyword, "NOT")
			column := trimIdent(condition[:idx])
			open := strings.Index(condition[idx:], "(")
			closeIdx := strings.LastIndex(condition, ")")
			if open < 0 || closeIdx < idx {
				return false, fmt.Errorf("fake: invalid IN condition: %s", condition)
			}

			value := valueOf(tbl, row, column)
			matched := false
			for _, item := range splitTopLevel(condition[idx+open+1:closeIdx], ',') {
				if strings.TrimSpace(item) == "?" {
					candidate, err := nextArg(args, position)
					if err != nil {
						return false, err
					}

					matched = matched || equalValue(value, candidate)
				}
			}

			if negate {
				return !matched, nil
			}

			return matched, nil
		}
	}

	// LIKE / NOT LIKE
	for _, keyword := range []string{" NOT LIKE ", " LIKE "} {
		if idx := strings.Index(upper, keyword); idx > 0 {
			negate := strings.Contains(keyword, "NOT")
			column := trimIdent(condition[:idx])
			candidate, err := nextArg(args, position)
			if err != nil {
				return false, err
			}

			matched := likeMatch(fmt.Sprint(valueOf(tbl, row, column)), fmt.Sprint(candidate))
			if negate {
				return !matched, nil
			}

			return matched, nil
		}
	}

	// 比较运算
	for _, operator := range []string{"<>", "!=", ">=", "<=", "=", ">", "<"} {
		idx := strings.Index(condition, operator)
		if idx <= 0 {
			continue
		}

		column := trimIdent(condition[:idx])
		right := strings.TrimSpace(condition[idx+len(operator):])
		if right != "?" && right != "1" {
			continue
		}

		value := valueOf(tbl, row, column)
		var candidate driver.Value
		if right == "?" {
			var err error
			candidate, err = nextArg(args, position)
			if err != nil {
				return false, err
			}
		} else {
			candidate = int64(1)
		}

		cmp := compareValues(value, candidate)
		switch operator {
		case "=":
			return cmp == 0, nil
		case "<>", "!=":
			return cmp != 0, nil
		case ">":
			return cmp > 0, nil
		case ">=":
			return cmp >= 0, nil
		case "<":
			return cmp < 0, nil
		case "<=":
			return cmp <= 0, nil
		}
	}

	return false, fmt.Errorf("fake: unsupported condition: %s", condition)
}

// nextArg 取出下一个参数
func nextArg(args []driver.Value, position *int) (driver.Value, error) {
	if *position >= len(args) {
		return nil, fmt.Errorf("fake: not enough args")
	}

	value := args[*position]
	*position = *position + 1
	return value, nil
}

// valueOf 读取字段值，字段不存在时返回 nil
func valueOf(tbl *table, row []driver.Value, column string) driver.Value {
	idx := tbl.columnIndex(column)
	if idx < 0 || idx >= len(row) {
		return nil
	}

	return row[idx]
}

// splitTopLevelKeyword 按关键字拆分，关键字出现在括号或引号内时忽略
func splitTopLevelKeyword(input, keyword string) []string {
	var (
		parts   []string
		current strings.Builder
		depth   int
		quote   rune
	)

	runes := []rune(input)
	keywordRunes := []rune(keyword)

	for i := 0; i < len(runes); i++ {
		char := runes[i]
		switch {
		case quote != 0:
			current.WriteRune(char)
			if char == quote {
				quote = 0
			}
			continue
		case char == '\'' || char == '`' || char == '"':
			quote = char
			current.WriteRune(char)
			continue
		case char == '(':
			depth++
		case char == ')':
			depth--
		}

		if depth == 0 && i+len(keywordRunes) <= len(runes) && strings.EqualFold(string(runes[i:i+len(keywordRunes)]), keyword) {
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
			i += len(keywordRunes) - 1
			continue
		}

		current.WriteRune(char)
	}

	if trimmed := strings.TrimSpace(current.String()); trimmed != "" {
		parts = append(parts, trimmed)
	}

	return parts
}

func trimOuterParens(input string) string {
	for strings.HasPrefix(input, "(") && strings.HasSuffix(input, ")") {
		depth := 0
		balanced := true
		for i, char := range input {
			switch char {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 && i != len(input)-1 {
					balanced = false
				}
			}
		}

		if !balanced {
			break
		}

		input = strings.TrimSpace(input[1 : len(input)-1])
	}

	return input
}

// ---------------------------------------------------------------------------
// 值比较
// ---------------------------------------------------------------------------

func likeMatch(value, pattern string) bool {
	var builder strings.Builder
	builder.WriteString("(?is)^")
	for _, char := range pattern {
		switch char {
		case '%':
			builder.WriteString(".*")
		case '_':
			builder.WriteString(".")
		default:
			builder.WriteString(regexp.QuoteMeta(string(char)))
		}
	}
	builder.WriteString("$")

	matched, err := regexp.MatchString(builder.String(), value)
	return err == nil && matched
}

func equalValue(left, right driver.Value) bool {
	return compareValues(left, right) == 0
}

func compareValues(left, right driver.Value) int {
	leftNumber, leftOK := numberOf(left)
	rightNumber, rightOK := numberOf(right)
	if leftOK && rightOK {
		switch {
		case leftNumber < rightNumber:
			return -1
		case leftNumber > rightNumber:
			return 1
		default:
			return 0
		}
	}

	leftTime, leftIsTime := left.(time.Time)
	rightTime, rightIsTime := right.(time.Time)
	if leftIsTime && rightIsTime {
		switch {
		case leftTime.Before(rightTime):
			return -1
		case leftTime.After(rightTime):
			return 1
		default:
			return 0
		}
	}

	return strings.Compare(fmt.Sprint(left), fmt.Sprint(right))
}

func numberOf(value driver.Value) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case float32:
		return float64(typed), true
	case float64:
		return typed, true
	default:
		return 0, false
	}
}

func isZeroNumber(value driver.Value) bool {
	number, ok := numberOf(value)
	return ok && number == 0
}
