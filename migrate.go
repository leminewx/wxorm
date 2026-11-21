package wxorm

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/schema"
	"gitee.com/leminewx/wxorm/session"
)

// ColumnDiff 描述单个列的类型差异
type ColumnDiff struct {
	Name     string // 列名
	Expected string // 模型声明的类型
	Actual   string // 数据库实际类型
}

// String 便于日志输出
func (own ColumnDiff) String() string {
	return fmt.Sprintf("%s: want %s, have %s", own.Name, own.Expected, own.Actual)
}

/*
TableDiff 描述模型与数据库表结构之间的差异

Migrate 会自动处理列增删与缺失索引；列类型差异只做提示，
避免在无法判断数据能否无损转换时擅自执行 ALTER COLUMN TYPE。
*/
type TableDiff struct {
	Table          string       // 物理表名（含库/Schema 名）
	Exists         bool         // 表是否已存在
	AddedColumns   []string     // 模型新增、数据库缺失的列
	DroppedColumns []string     // 模型已移除、数据库仍存在的列
	ChangedColumns []ColumnDiff // 类型不一致的列
	MissingIndexes []string     // 模型声明、数据库缺失的索引
}

// IsEmpty 判断表结构与模型是否一致
func (own *TableDiff) IsEmpty() bool {
	if own == nil {
		return true
	}

	return own.Exists && len(own.AddedColumns) == 0 && len(own.DroppedColumns) == 0 &&
		len(own.ChangedColumns) == 0 && len(own.MissingIndexes) == 0
}

// MigrateOption 配置 Migrate 的迁移行为
type MigrateOption func(options *migrateOptions)

// migrateOptions 保存 Migrate 的可选行为
//
// 默认值全部为「不执行破坏性操作」：迁移只做可逆或可重建的变更。
type migrateOptions struct {
	dropMissingColumns bool
}

// WithDropMissingColumns 允许 Migrate 删除数据库中模型已移除的列
//
// 默认不删列：字段被删除、改名或仅改变大小写时，数据库中的对应列会被
// 报告为 DroppedColumns，但不会执行 DROP COLUMN。DDL 不可回滚，
// 静默删列等于在无人确认的情况下销毁整列真实数据。
func WithDropMissingColumns() MigrateOption {
	return func(options *migrateOptions) {
		options.dropMissingColumns = true
	}
}

/*
DiffTable 对比模型与数据库表结构，返回差异报告（只读，不执行 DDL）

表尚未创建时报告 Exists 为 false；列类型比较前会归一化方言差异
（整数显示宽度、character varying 等别名），但仍可能存在无法消除的写法差异，
因此类型差异只用于提示。
*/
func (own *Engine) DiffTable(model any) (*TableDiff, error) {
	if model == nil {
		return nil, nil
	}

	sess := own.NewSession(model)
	defer func() { _ = sess.Close() }()

	return own.diffTable(sess)
}

// diffTable 用已绑定模型的会话生成差异报告
func (own *Engine) diffTable(sess *session.Session) (*TableDiff, error) {
	diff := &TableDiff{Table: sess.GetQualifiedTableName()}

	// 查询失败时不能当成“表不存在”，否则会重复建表
	exists, err := sess.ExistsTable()
	if err != nil {
		return nil, err
	}

	diff.Exists = exists
	if !exists {
		return diff, nil
	}

	sch, err := sess.GetSchema()
	if err != nil {
		return nil, err
	}

	// 从数据字典读取列名，不用 SELECT *：pgx 会缓存执行计划，
	// 列集变化后再次执行会报 cached plan must not change result type
	columns, err := sess.GetTableColumns()
	if err != nil {
		return nil, fmt.Errorf("wxorm: failed to describe table %s: %w", diff.Table, err)
	}

	diff.AddedColumns = diffStrings(sch.ColumnNames, columns)
	diff.DroppedColumns = diffStrings(columns, sch.ColumnNames)

	if diff.ChangedColumns, err = own.diffColumnTypes(sess, sch); err != nil {
		return nil, err
	}

	if diff.MissingIndexes, err = own.diffIndexes(sess, sch); err != nil {
		return nil, err
	}

	return diff, nil
}

// diffColumnTypes 对比模型声明与数据库实际的列类型
func (own *Engine) diffColumnTypes(sess *session.Session, sch *schema.Schema) ([]ColumnDiff, error) {
	columns, err := sess.GetTableColumnTypes()
	if errors.Is(err, session.ErrDialectCannotReadColumns) {
		// 方言不提供数据字典读取能力：跳过类型比对，不阻断迁移
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("wxorm: failed to read column types of table %s: %w", sess.GetQualifiedTableName(), err)
	}

	actual := make(map[string]string, len(columns))
	for _, col := range columns {
		// 列名按大小写不敏感对齐，避免方言对标识符的折叠差异导致漏比类型
		actual[strings.ToLower(col.Name)] = normalizeDataType(col.Type)
	}

	var diffs []ColumnDiff
	for _, col := range sch.Columns {
		current, ok := actual[strings.ToLower(col.GetName())]
		if !ok {
			// 缺失列已由 AddedColumns 报告
			continue
		}

		expected := normalizeDataType(string(col.GetDatatype()))
		if expected == "" || current == "" || expected == current {
			continue
		}

		diffs = append(diffs, ColumnDiff{Name: col.GetName(), Expected: string(col.GetDatatype()), Actual: current})
	}

	return diffs, nil
}

// diffIndexes 对比模型声明与数据库已存在的索引名
func (own *Engine) diffIndexes(sess *session.Session, sch *schema.Schema) ([]string, error) {
	indexer, ok := own.dialect.(dialect.TableIndexer)
	if !ok {
		return nil, nil
	}

	stmts := indexer.BuildTableIndexSQLs(sess.GetQualifiedTableName(), sch.Columns)
	if len(stmts) == 0 {
		return nil, nil
	}

	existing, err := sess.GetTableIndexes()
	if errors.Is(err, session.ErrDialectCannotReadIndexes) {
		// 无法枚举已有索引时宁可跳过，也不重复创建同名索引
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("wxorm: failed to read indexes of table %s: %w", sess.GetQualifiedTableName(), err)
	}

	have := make(map[string]bool, len(existing))
	for _, name := range existing {
		have[strings.ToLower(name)] = true
	}

	var missing []string
	for _, stmt := range stmts {
		name := parseIndexName(stmt)
		if name == "" || have[strings.ToLower(name)] {
			continue
		}

		missing = append(missing, name)
	}

	return missing, nil
}

// createMissingIndexes 执行模型声明但数据库缺失的索引
func (own *Engine) createMissingIndexes(sess *session.Session, sch *schema.Schema, missing []string) error {
	if len(missing) == 0 {
		return nil
	}

	indexer, ok := own.dialect.(dialect.TableIndexer)
	if !ok {
		return nil
	}

	pending := make(map[string]bool, len(missing))
	for _, name := range missing {
		pending[name] = true
	}

	for _, stmt := range indexer.BuildTableIndexSQLs(sess.GetQualifiedTableName(), sch.Columns) {
		if !pending[parseIndexName(stmt)] {
			continue
		}

		if _, err := sess.ExecDDL(stmt); err != nil {
			return fmt.Errorf("wxorm: failed to create index: %w", err)
		}
	}

	return nil
}

// parseIndexName 从 CREATE INDEX 语句中提取索引名
//
// 语句由方言的 BuildTableIndexSQLs 生成，格式固定为
// CREATE INDEX <name> ON <table> (<columns>)，名字可能带引号。
func parseIndexName(stmt string) string {
	const prefix = "CREATE INDEX "
	if !strings.HasPrefix(stmt, prefix) {
		return ""
	}

	name := stmt[len(prefix):]
	if idx := strings.Index(name, " ON "); idx >= 0 {
		name = name[:idx]
	}

	return strings.TrimSpace(strings.Trim(name, "`\""))
}

// intDisplayWidth 匹配带显示宽度的整数类型（MySQL 旧版本写法）
var intDisplayWidth = regexp.MustCompile(`^(tinyint|smallint|mediumint|int|integer|bigint)\(\d+\)(\s+unsigned)?$`)

// normalizeDataType 归一化方言之间的类型写法差异
//
// PostgreSQL 的 format_type 返回 character varying(50) 这类完整名称，
// MySQL 旧版本会返回 int(11)，这里统一成模型声明的常见写法再比较。
func normalizeDataType(dataType string) string {
	value := strings.ToLower(strings.Join(strings.Fields(dataType), " "))
	if value == "" {
		return ""
	}

	value = strings.ReplaceAll(value, "character varying", "varchar")
	value = strings.ReplaceAll(value, "timestamp without time zone", "timestamp")
	value = strings.ReplaceAll(value, "timestamp with time zone", "timestamptz")
	value = strings.ReplaceAll(value, "double precision", "double")
	value = strings.ReplaceAll(value, "boolean", "bool")
	value = strings.ReplaceAll(value, "integer", "int")
	value = intDisplayWidth.ReplaceAllString(value, "$1$2")

	return strings.TrimSpace(value)
}

// logUnhandledDiff 提示无法自动处理的差异（不阻断迁移）
func logUnhandledDiff(diff *TableDiff) {
	if diff == nil {
		return
	}

	if len(diff.ChangedColumns) > 0 {
		log.Printf("wxorm: table %s has %d column type mismatch(es), skipped: %v", diff.Table, len(diff.ChangedColumns), diff.ChangedColumns)
	}
}
