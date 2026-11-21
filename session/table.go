package session

import (
	"database/sql"
	"errors"
	"strings"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/naming"
	"gitee.com/leminewx/wxorm/schema"
)

var (
	ErrModelNotSpecified        = errors.New("wxorm: data model is not be specified")
	ErrModelNoExportableFields  = errors.New("wxorm: data model has no exportable fields")
	ErrDialectCannotReadColumns = errors.New("wxorm: dialect cannot read table column types")
	ErrDialectCannotReadIndexes = errors.New("wxorm: dialect cannot read table indexes")
)

/*
UseModel 使用指定数据模型
  - model: 必须使用指针格式 *Struct
*/
func (own *Session) UseModel(model any) *Session {
	if model == nil {
		return own
	}

	// 记录模型实例，钩子调用以此为准，避免使用缓存中的首个实例
	own.model = model

	sch, err := schema.Parse(own.dialect, model)
	if err != nil {
		own.modelErr = err
		return own
	}

	own.modelErr = nil
	own.SetSchema(sch)
	return own
}

// SetSchema 设置数据表
func (own *Session) SetSchema(sch *schema.Schema) {
	if sch != nil {
		own.schema = sch
		own.resolveTableName()
	}
}

/*
SetNaming 设置表名命名策略（库/Schema、前缀、后缀）

策略在会话层应用，schema 缓存仍按「方言 + 模型类型」共享：
同一模型可以在不同引擎上配置不同前缀，而不会互相污染表名。
策略校验失败时只记录错误，由 GetSchema 统一返回，调用方无需逐处检查。
*/
func (own *Session) SetNaming(strategy naming.Strategy) *Session {
	if err := strategy.Validate(); err != nil {
		own.namingErr = err
		return own
	}

	own.namingErr = nil
	own.naming = strategy
	own.resolveTableName()
	return own
}

// GetNaming 获取表名命名策略
func (own *Session) GetNaming() naming.Strategy {
	return own.naming
}

// GetTableName 返回物理表名（含命名策略的前后缀，不含库/Schema 名）
func (own *Session) GetTableName() string {
	return own.table
}

// GetQualifiedTableName 返回物理表名（含库/Schema 名），用于原生 SQL 拼接
func (own *Session) GetQualifiedTableName() string {
	return own.tableSQL
}

/*
resolveTableName 依据命名策略计算物理表名与预编译缓存键

逻辑表名来自 schema 缓存，物理表名只能保存在会话上：
若写回 schema，同一模型在不同引擎下会共用被冻结的表名。
插入语句缓存键必须包含物理表名，否则不同前缀的表会命中同一条预编译语句。
*/
func (own *Session) resolveTableName() {
	if own.schema == nil {
		return
	}

	own.table = own.naming.GetTableName(own.schema.TableName)
	own.tableSQL = own.naming.Qualify(own.table)
	own.insertKey = own.schema.GetInsertCacheKey(own.table)
	own.insertIgnoreKey = own.schema.GetInsertIgnoreCacheKey(own.table)
	own.statement.SetTable(own.tableSQL)
}

/*
namespace 返回元数据查询使用的库/Schema 名

只返回命名策略显式指定的 Schema：为空时交给方言回落（MySQL 用 DATABASE()、
PostgreSQL 用 current_schema()、SQLite 忽略）。不能用连接串里的库名兜底：
MySQL 的库就是 schema，而 PostgreSQL 的库（catalog）与 schema 是两个层级，
把库名当成 schema 会查不到任何表。
*/
func (own *Session) namespace() string {
	return own.naming.Schema
}

// CreateTable 创建数据表
func (own *Session) CreateTable(metadata ...dialect.TableMetadata) error {
	// DDL 会使缓存的执行计划失效，无论成功与否都清空一次
	defer own.ClearStatementCache()

	table, err := own.GetSchema()
	if err != nil {
		return err
	}

	ctx := clause.NewContext()
	if err := own.dialect.BuildTableCreateSQL(ctx, own.tableSQL, table.Columns, metadata...); err != nil {
		return err
	}

	sql, args := ctx.GetResult()
	if _, err = own.ExecDDL(sql, args...); err != nil {
		return err
	}

	// MySQL / PostgreSQL / SQLite 都不支持列内联普通索引，建表后逐条创建
	if indexer, ok := own.dialect.(dialect.TableIndexer); ok {
		for _, stmt := range indexer.BuildTableIndexSQLs(own.tableSQL, table.Columns) {
			if _, err = own.ExecDDL(stmt); err != nil {
				return err
			}
		}
	}

	// 不支持内联注释的方言，用 COMMENT ON 单独写入
	if commenter, ok := own.dialect.(dialect.TableCommenter); ok {
		for _, stmt := range commenter.BuildTableCommentSQLs(own.tableSQL, table.Columns, metadata...) {
			if _, err = own.ExecDDL(stmt); err != nil {
				return err
			}
		}
	}

	return nil
}

// DeleteTable 删除数据表
func (own *Session) DeleteTable() error {
	// DDL 会使缓存的执行计划失效，无论成功与否都清空一次
	defer own.ClearStatementCache()

	if _, err := own.GetSchema(); err != nil {
		return err
	}

	_, err := own.ExecDDL("DROP TABLE IF EXISTS " + own.dialect.QuoteIdentifier(own.tableSQL) + ";")
	return err
}

/*
ExistsTable 检查数据表是否存在

查询失败不再伪装成「表不存在」：调用方（如 Migrate）需要区分这两者，
否则连接、权限或表名错误都会被当成缺表，进而重复执行建表语句。
*/
func (own *Session) ExistsTable() (bool, error) {
	if _, err := own.GetSchema(); err != nil {
		return false, err
	}

	// 生成查询语句
	ctx := clause.NewContext()
	if err := own.dialect.BuildTableQuerySQL(ctx, own.namespace(), own.table); err != nil {
		return false, err
	}
	sqlStr, args := ctx.GetResult()

	// 执行查询：没有结果行是正常结果，表示表不存在
	var name string
	if err := own.QueryRow(sqlStr, args...).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}

		return false, err
	}

	// 标识符大小写不敏感的方言（MySQL lower_case_table_names=1、SQLite）
	// 返回的名字可能与模型声明不同，直接比较会误判为缺表并重复建表
	if insensitive, ok := own.dialect.(dialect.TableCaseInsensitive); ok && insensitive.IsTableCaseInsensitive() {
		return strings.EqualFold(name, own.table), nil
	}

	return name == own.table, nil
}

// GetTableColumns 返回数据表在数据库中的列名（按定义顺序）
//
// 走 information_schema / pragma_table_info 读取，而不是 SELECT *：
// pgx 会缓存 SELECT * 的执行计划，列集变化后再次执行会报
// cached plan must not change result type，迁移流程会直接失败。
func (own *Session) GetTableColumns() ([]string, error) {
	if _, err := own.GetSchema(); err != nil {
		return nil, err
	}

	ctx := clause.NewContext()
	if err := own.dialect.BuildTableColumnsQuerySQL(ctx, own.namespace(), own.table); err != nil {
		return nil, err
	}
	query, args := ctx.GetResult()

	rows, err := own.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var columns []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}

		columns = append(columns, name)
	}

	return columns, rows.Err()
}

// TableColumnType 描述数据库中的一列及其类型
//
// Type 保留数据字典返回的原始写法（如 varchar(50)、bigint unsigned），
// 迁移比对前由调用方自行归一化。
type TableColumnType struct {
	Name string // 列名
	Type string // 列类型
}

// GetTableColumnTypes 返回数据表各列的名称与类型（按定义顺序）
func (own *Session) GetTableColumnTypes() ([]TableColumnType, error) {
	if _, err := own.GetSchema(); err != nil {
		return nil, err
	}

	typer, ok := own.dialect.(dialect.TableColumnTyper)
	if !ok {
		return nil, ErrDialectCannotReadColumns
	}

	ctx := clause.NewContext()
	if err := typer.BuildTableColumnTypesQuerySQL(ctx, own.namespace(), own.table); err != nil {
		return nil, err
	}
	query, args := ctx.GetResult()

	rows, err := own.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var columns []TableColumnType
	for rows.Next() {
		var col TableColumnType
		var dataType sql.NullString
		if err = rows.Scan(&col.Name, &dataType); err != nil {
			return nil, err
		}

		col.Type = dataType.String
		columns = append(columns, col)
	}

	return columns, rows.Err()
}

// GetTableIndexes 返回数据表已存在的索引名
func (own *Session) GetTableIndexes() ([]string, error) {
	if _, err := own.GetSchema(); err != nil {
		return nil, err
	}

	reader, ok := own.dialect.(dialect.TableIndexReader)
	if !ok {
		return nil, ErrDialectCannotReadIndexes
	}

	ctx := clause.NewContext()
	if err := reader.BuildTableIndexesQuerySQL(ctx, own.namespace(), own.table); err != nil {
		return nil, err
	}
	query, args := ctx.GetResult()

	rows, err := own.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var indexes []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}

		indexes = append(indexes, name)
	}

	return indexes, rows.Err()
}

// GetSchema 获取数据表
func (own *Session) GetSchema() (*schema.Schema, error) {
	// 命名策略不合法时优先返回真实原因
	if own.namingErr != nil {
		return nil, own.namingErr
	}

	// 模型解析失败时优先返回真实原因
	if own.modelErr != nil {
		return nil, own.modelErr
	}

	if own.schema == nil {
		return nil, ErrModelNotSpecified
	}

	if len(own.schema.Columns) == 0 {
		return nil, ErrModelNoExportableFields
	}

	return own.schema, nil
}
