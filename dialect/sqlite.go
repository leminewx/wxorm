package dialect

import (
	"errors"
	"reflect"
	"strings"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/dialect/column"
	"gitee.com/leminewx/wxorm/dialect/datatype"
)

const (
	// SQLite 是 SQLite 方言的默认注册名（modernc.org/sqlite 驱动的名字）
	SQLite = "sqlite"
	// SQLite3 是 SQLite 方言的别名（mattn/go-sqlite3 驱动的名字）
	SQLite3 = "sqlite3"
)

func init() {
	dial := NewSQLiteDialect()
	Register(SQLite, dial)
	Register(SQLite3, dial)
}

// ErrSQLiteCompositeAutoPrimaryKey 表示 SQLite 不支持「复合主键 + 自增列」
//
// AUTOINCREMENT 只能出现在单列的 INTEGER PRIMARY KEY 上，
// 与复合主键冲突时无法生成合法 DDL。
var ErrSQLiteCompositeAutoPrimaryKey = errors.New("wxorm: SQLite does not support AUTOINCREMENT in a composite primary key")

/*
sqlite 定义 SQLite 方言

SQL 文本全部由 clause.SQLiteClause 负责，本类型只处理“方言边界”：
字段解析、类型映射、表结构查询、建表语句与后置索引。
*/
type sqlite struct {
	clause.SQLiteClause
}

var (
	_ Dialect          = (*sqlite)(nil)
	_ TableIndexer     = (*sqlite)(nil)
	_ TableColumnTyper = (*sqlite)(nil)
	_ TableIndexReader = (*sqlite)(nil)
)

// NewSQLiteDialect 创建 SQLite 数据库适配器
func NewSQLiteDialect() Dialect {
	return &sqlite{
		SQLiteClause: clause.NewSQLiteClause(),
	}
}

// ParseColumn 解析数据表字段
func (own *sqlite) ParseColumn(tag string) (column.Column, error) {
	return column.NewSQLiteColumn(tag)
}

// ConvertDatatype 转换 Go 结构体数据类型为 SQLite 对应的数据类型
func (own *sqlite) ConvertDatatype(data reflect.Type) datatype.Datatype {
	return datatype.ConvertGoDatatypeToSQLiteDatatype(data)
}

// GetDriverName 获取驱动名称
//
// sqlite 与 sqlite3 两个别名共用同一份模型缓存，因此固定返回 sqlite。
func (own *sqlite) GetDriverName() string {
	return SQLite
}

// IsTableCaseInsensitive 标识 SQLite 的表名不区分大小写
func (own *sqlite) IsTableCaseInsensitive() bool {
	return true
}

// GetClause 获取处理子句相关的接口
func (own *sqlite) GetClause() clause.Clause {
	return own.SQLiteClause
}

// GetPrimaryKeyColumns 获取主键字段
func (own *sqlite) GetPrimaryKeyColumns(columns []column.Column) []column.Column {
	return filterPrimaryKeyColumns(columns)
}

// GetForeignKeyColumns 获取外键字段
func (own *sqlite) GetForeignKeyColumns(columns []column.Column) map[string][]column.Column {
	return groupForeignKeyColumns(columns)
}

// GetUnionIndexColumns 获取联合索引字段
func (own *sqlite) GetUnionIndexColumns(columns []column.Column) map[string][]column.Column {
	return groupMetadataColumns(columns, column.Column.GetIndex)
}

// GetUnionUniqueColumns 获取联合唯一字段
func (own *sqlite) GetUnionUniqueColumns(columns []column.Column) map[string][]column.Column {
	return groupMetadataColumns(columns, column.Column.GetUnique)
}

/*
BuildTableQuerySQL 构造数据表存在性查询语句

SQLite 没有 information_schema，表信息存放在 sqlite_master（别名 sqlite_schema）中。
namespace 在 SQLite 里没有对应概念（一个文件就是一个库），因此忽略；
如果需要查询 attached 数据库，请在原生 SQL 里写 "<schema>.sqlite_master"。
*/
func (own *sqlite) BuildTableQuerySQL(ctx *clause.Context, _, tableName string) error {
	if tableName == "" {
		return ErrTableNameIsEmpty
	}

	_ = ctx.WriteString("SELECT\n    name\nFROM\n    sqlite_master\nWHERE\n    type = 'table' AND name = ")
	return ctx.WritePlaceholderWithArg(own, tableName)
}

/*
BuildTableColumnsQuerySQL 构造数据表列名查询语句

SQLite 的表结构通过 pragma_table_info 虚表函数返回，
按 cid 排序即为列定义顺序；同样避免用 SELECT * 探测列名。
*/
func (own *sqlite) BuildTableColumnsQuerySQL(ctx *clause.Context, _, tableName string) error {
	if tableName == "" {
		return ErrTableNameIsEmpty
	}

	_ = ctx.WriteString("SELECT\n    name\nFROM\n    pragma_table_info(")
	if err := ctx.WritePlaceholderWithArg(own, tableName); err != nil {
		return err
	}

	return ctx.WriteString(")\nORDER BY\n    cid")
}

// BuildTableColumnTypesQuerySQL 构造数据表列名与类型查询语句
func (own *sqlite) BuildTableColumnTypesQuerySQL(ctx *clause.Context, _, tableName string) error {
	if tableName == "" {
		return ErrTableNameIsEmpty
	}

	_ = ctx.WriteString("SELECT\n    name,\n    type\nFROM\n    pragma_table_info(")
	if err := ctx.WritePlaceholderWithArg(own, tableName); err != nil {
		return err
	}

	return ctx.WriteString(")\nORDER BY\n    cid")
}

// BuildTableIndexesQuerySQL 构造数据表索引名查询语句
//
// sqlite_autoindex_* 是唯一约束自动生成的索引，不属于模型声明，这里一并排除。
func (own *sqlite) BuildTableIndexesQuerySQL(ctx *clause.Context, _, tableName string) error {
	if tableName == "" {
		return ErrTableNameIsEmpty
	}

	_ = ctx.WriteString("SELECT\n    name\nFROM\n    sqlite_master\nWHERE\n    type = 'index' AND name NOT LIKE 'sqlite_%' AND tbl_name = ")
	return ctx.WritePlaceholderWithArg(own, tableName)
}

/*
BuildTableCreateSQL 构造数据表创建语句

SQLite 没有 ENGINE/CHARSET/COLLATE 这些表级选项，索引也无法内联，
需要建表后单独执行（见 BuildTableIndexSQLs）；表与列的注释则完全不支持。
*/
func (own *sqlite) BuildTableCreateSQL(ctx *clause.Context, tableName string, columns []column.Column, _ ...TableMetadata) error {
	if tableName == "" {
		return ErrTableNameIsEmpty
	}

	// 自增主键只能内联在单列上，复合主键里出现自增列直接报错，避免生成非法 DDL
	if findInlinePrimaryKeyColumn(columns) != nil && len(own.GetPrimaryKeyColumns(columns)) > 1 {
		return ErrSQLiteCompositeAutoPrimaryKey
	}

	// 表定义开始
	_ = ctx.WriteString("CREATE TABLE ")
	_ = ctx.WriteString(own.QuoteIdentifier(tableName))
	_ = ctx.WriteString(" (\n")

	// 定义数据表字段、主键、联合唯一与外键约束
	generators := []func(ctx *clause.Context, columns []column.Column) error{
		own.buildColumns,     // 处理数据字段（自增主键在这里内联）
		own.buildPrimaryKeys, // 处理主键约束
		own.buildUnionUniques,
		own.buildForeignKeys, // 处理外键约束
	}

	for _, generator := range generators {
		if err := generator(ctx, columns); err != nil {
			return err
		}
	}

	_ = ctx.WriteString("\n)")

	// 表定义结束
	return ctx.WriteString(";")
}

// BuildTableIndexSQLs 构造索引创建语句
//
// SQLite 不支持在 CREATE TABLE 内联 INDEX，
// 这里返回建表后需要逐条执行的 CREATE INDEX 语句。
func (own *sqlite) BuildTableIndexSQLs(tableName string, columns []column.Column) []string {
	if tableName == "" {
		return nil
	}

	quotedTable := own.QuoteIdentifier(tableName)
	base := indexNameBase(tableName)
	stmts := make([]string, 0, len(columns))

	// 未命名的 index 标记：单列索引，名字按 表名_字段名_idx 生成
	for _, col := range columns {
		index := col.GetIndex()
		if !index.Has || index.Value != "" {
			continue
		}

		name := base + "_" + col.GetName() + "_idx"
		stmts = append(stmts, "CREATE INDEX "+own.QuoteIdentifier(name)+" ON "+quotedTable+" ("+own.QuoteIdentifier(col.GetName())+");")
	}

	// 命名的 index 标记：同名多列组成复合索引
	groups := groupMetadataColumns(columns, column.Column.GetIndex)
	for _, name := range sortKeys(groups) {
		quoted := make([]string, 0, len(groups[name]))
		for _, col := range groups[name] {
			quoted = append(quoted, own.QuoteIdentifier(col.GetName()))
		}

		stmts = append(stmts, "CREATE INDEX "+own.QuoteIdentifier(name)+" ON "+quotedTable+" ("+strings.Join(quoted, ", ")+");")
	}

	return stmts
}

// buildColumns 构造数据表字段信息
func (own *sqlite) buildColumns(ctx *clause.Context, columns []column.Column) error {
	_ = ctx.WriteString("    -- 命名字段\n")
	for i, col := range columns {
		if i > 0 {
			_ = ctx.WriteString(",\n")
		}

		_ = ctx.WriteString("    ")
		if err := col.BuildCreateSQL(ctx); err != nil {
			return err
		}
	}

	return nil
}

// buildPrimaryKeys 构造数据表主键信息
//
// 自增主键已在列定义里内联为 INTEGER PRIMARY KEY AUTOINCREMENT，
// 这里必须跳过，否则 SQLite 会因为两个主键声明而报错。
func (own *sqlite) buildPrimaryKeys(ctx *clause.Context, columns []column.Column) error {
	if findInlinePrimaryKeyColumn(columns) != nil {
		return nil
	}

	pkColumns := own.GetPrimaryKeyColumns(columns)
	if len(pkColumns) == 0 {
		return nil
	}

	_ = ctx.WriteString(",\n\n    -- 命名主键\n    PRIMARY KEY (")
	for i, col := range pkColumns {
		if i > 0 {
			_ = ctx.WriteString(", ")
		}

		_ = ctx.WriteString(own.QuoteIdentifier(col.GetName()))
	}

	return ctx.WriteByte(')')
}

// buildUnionUniques 构造联合唯一信息
func (own *sqlite) buildUnionUniques(ctx *clause.Context, columns []column.Column) error {
	var noted bool
	groups := own.GetUnionUniqueColumns(columns)
	for _, name := range sortKeys(groups) {
		group := groups[name]
		if noted {
			_ = ctx.WriteString(",\n")
		}
		if !noted {
			_ = ctx.WriteString(",\n\n    -- 命名唯一约束\n")
			noted = true
		}

		_ = ctx.WriteString("    CONSTRAINT ")
		_ = ctx.WriteString(own.QuoteIdentifier(name))
		_ = ctx.WriteString(" UNIQUE (")

		for i, col := range group {
			if i > 0 {
				_ = ctx.WriteString(", ")
			}

			_ = ctx.WriteString(own.QuoteIdentifier(col.GetName()))
		}

		_ = ctx.WriteByte(')')
	}

	return nil
}

// buildForeignKeys 构造数据表外键信息
//
// SQLite 支持外键语法，但默认不强制检查，需要在连接上执行 PRAGMA foreign_keys = ON。
func (own *sqlite) buildForeignKeys(ctx *clause.Context, columns []column.Column) error {
	var noted, hasName bool
	groups := own.GetForeignKeyColumns(columns)
	for _, ft := range sortKeys(groups) {
		group := groups[ft]
		if noted {
			_ = ctx.WriteString(",\n")
		}
		if !noted {
			_ = ctx.WriteString(",\n\n    -- 命名外键约束\n")
			noted = true
		}

		// 写入 CONSTRAINT "fk_name"
		for _, col := range group {
			fk := col.GetForeignKey()
			if fk.Name != "" {
				_ = ctx.WriteString("    CONSTRAINT ")
				_ = ctx.WriteString(own.QuoteIdentifier(fk.Name))
				_ = ctx.WriteByte('\n')
				hasName = true
				break
			}
		}

		// 写入 FOREIGN KEY ("column1", ...)
		var odc, ouc bool

		if hasName {
			_ = ctx.WriteString("        FOREIGN KEY (")
		} else {
			_ = ctx.WriteString("    FOREIGN KEY (")
		}

		for i, col := range group {
			if i > 0 {
				_ = ctx.WriteString(", ")
			}

			_ = ctx.WriteString(own.QuoteIdentifier(col.GetName()))

			fk := col.GetForeignKey()
			if fk.OnDeleteCascade {
				odc = true
			}
			if fk.OnUpdateCascade {
				ouc = true
			}
		}

		// 写入 REFERENCES "other_table" ("other_column1", ...)
		if hasName {
			_ = ctx.WriteString(")\n        REFERENCES ")
		} else {
			_ = ctx.WriteString(")\n    REFERENCES ")
		}

		_ = ctx.WriteString(own.QuoteIdentifier(ft))
		_ = ctx.WriteString(" (")
		for i, col := range group {
			if i > 0 {
				_ = ctx.WriteString(", ")
			}

			_ = ctx.WriteString(own.QuoteIdentifier(col.GetForeignKey().Column))
		}
		_ = ctx.WriteString(")")

		// 写入 ON UPDATE CASCADE 或 ON DELETE CASCADE
		if odc {
			if hasName {
				_ = ctx.WriteString("\n        ON DELETE CASCADE")
			} else {
				_ = ctx.WriteString("\n    ON DELETE CASCADE")
			}
		}
		if ouc {
			if hasName {
				_ = ctx.WriteString("\n        ON UPDATE CASCADE")
			} else {
				_ = ctx.WriteString("\n    ON UPDATE CASCADE")
			}
		}

		// 重置命名外键约束
		hasName = false
	}

	return nil
}

// autoIncrementer 由需要知道「列是否自增」的方言实现（见 column.BaseColumn）
type autoIncrementer interface {
	IsAutoIncrement() bool
}

// findInlinePrimaryKeyColumn 返回已在列定义中内联声明主键的列
//
// 只有 SQLite 需要这个判断：它的自增主键必须写成 INTEGER PRIMARY KEY AUTOINCREMENT。
func findInlinePrimaryKeyColumn(columns []column.Column) column.Column {
	for _, col := range columns {
		if !col.IsPrimaryKey() {
			continue
		}

		if auto, ok := col.(autoIncrementer); ok && auto.IsAutoIncrement() {
			return col
		}
	}

	return nil
}
