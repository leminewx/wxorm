package dialect

import (
	"errors"
	"reflect"
	"strings"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/dialect/column"
	"gitee.com/leminewx/wxorm/dialect/datatype"
)

func init() {
	Register("mysql", NewMySQLDialect())
}

var (
	ErrDBNameIsEmpty    = errors.New("wxorm: database name is empty")
	ErrTableNameIsEmpty = errors.New("wxorm: table name is empty")
)

// MySQL 的索引与 PostgreSQL / SQLite 一样单独创建，保证同一份模型在三方言下行为一致
var _ TableIndexer = (*mysql)(nil)

// 迁移时支持列类型与已有索引的读取
var (
	_ TableColumnTyper = (*mysql)(nil)
	_ TableIndexReader = (*mysql)(nil)
)

// MySQL 定义 MySQL 数据表的数据结构
type mysql struct {
	// 使用 MySQL 的完整能力集合（含 REPLACE / UPSERT / JSON / 全文检索），
	// 这样 dialect.Dialect 的具体类型才能被会话断言为 clause.Replacer 等能力接口。
	clause.MySQLClause
}

// NewMySQLDialect 创建 MySQL 数据库适配器
func NewMySQLDialect() Dialect {
	return &mysql{
		MySQLClause: clause.NewMySQLClause(),
	}
}

// ParseColumn 解析数据表字段
func (own *mysql) ParseColumn(tag string) (column.Column, error) {
	return column.NewMySQLColumn(tag)
}

// ConvertDatatype 转换 Go 结构体数据类型为 MySQL 对应的数据类型
func (own *mysql) ConvertDatatype(data reflect.Type) datatype.Datatype {
	return datatype.ConvertGoDatatypeToMySQLDatatype(data)
}

// GetDriverName 获取驱动名称
func (own *mysql) GetDriverName() string {
	return "mysql"
}

// IsTableCaseInsensitive 标识 MySQL 的表名不区分大小写
//
// Windows/macOS 默认 lower_case_table_names=1，information_schema 返回的名字统一为小写，
// 与模型声明的大小写可能不同。
func (own *mysql) IsTableCaseInsensitive() bool {
	return true
}

// GetClause 获取处理子句相关的接口
func (own *mysql) GetClause() clause.Clause {
	return own.MySQLClause
}

// GetPrimaryKeyColumns 获取主键字段
func (own *mysql) GetPrimaryKeyColumns(columns []column.Column) []column.Column {
	return filterPrimaryKeyColumns(columns)
}

// GetForeignKeyColumns 获取外键字段
func (own *mysql) GetForeignKeyColumns(columns []column.Column) map[string][]column.Column {
	return groupForeignKeyColumns(columns)
}

// GetUnionIndexColumns 获取联合索引字段
func (own *mysql) GetUnionIndexColumns(columns []column.Column) map[string][]column.Column {
	return groupMetadataColumns(columns, column.Column.GetIndex)
}

// GetUnionUniqueColumns 获取联合唯一字段
func (own *mysql) GetUnionUniqueColumns(columns []column.Column) map[string][]column.Column {
	return groupMetadataColumns(columns, column.Column.GetUnique)
}

/*
BuildTableQuerySQL 构造数据表存在性查询语句

namespace 是数据表所属的库（MySQL 的 schema 即 database），为空时回落到当前连接的
DATABASE()：库名由连接决定，比从 DSN 中解析更可靠。
*/
func (own *mysql) BuildTableQuerySQL(ctx *clause.Context, namespace, tableName string) (err error) {
	if tableName == "" {
		return ErrTableNameIsEmpty
	}

	_ = ctx.WriteString("SELECT\n    `table_name`\nFROM\n    information_schema.tables\nWHERE\n    `table_schema` = ")
	if err = own.writeNamespace(ctx, namespace); err != nil {
		return err
	}

	_ = ctx.WriteString(" AND `table_name` = ")
	return ctx.WritePlaceholderWithArg(own, tableName)
}

// writeNamespace 写入库名过滤表达式，未指定时回落到 DATABASE()
func (own *mysql) writeNamespace(ctx *clause.Context, namespace string) error {
	if namespace == "" {
		return ctx.WriteString("DATABASE()")
	}

	return ctx.WritePlaceholderWithArg(own, namespace)
}

/*
BuildTableColumnsQuerySQL 构造数据表列名查询语句

迁移时不能靠 SELECT * 读列名：PostgreSQL（pgx）会缓存该语句的执行计划，
列集变化后再次执行会报 cached plan must not change result type。
*/
func (own *mysql) BuildTableColumnsQuerySQL(ctx *clause.Context, namespace, tableName string) (err error) {
	if tableName == "" {
		return ErrTableNameIsEmpty
	}

	_ = ctx.WriteString("SELECT\n    `column_name`\nFROM\n    information_schema.columns\nWHERE\n    `table_schema` = ")
	if err = own.writeNamespace(ctx, namespace); err != nil {
		return err
	}

	_ = ctx.WriteString(" AND `table_name` = ")
	if err = ctx.WritePlaceholderWithArg(own, tableName); err != nil {
		return err
	}

	return ctx.WriteString("\nORDER BY\n    `ordinal_position`")
}

// BuildTableColumnTypesQuerySQL 构造数据表列名与类型查询语句
//
// column_type 带长度与符号（如 varchar(50)、bigint unsigned），可直接与模型声明的类型比对。
func (own *mysql) BuildTableColumnTypesQuerySQL(ctx *clause.Context, namespace, tableName string) (err error) {
	if tableName == "" {
		return ErrTableNameIsEmpty
	}

	_ = ctx.WriteString("SELECT\n    `column_name`,\n    `column_type`\nFROM\n    information_schema.columns\nWHERE\n    `table_schema` = ")
	if err = own.writeNamespace(ctx, namespace); err != nil {
		return err
	}

	_ = ctx.WriteString(" AND `table_name` = ")
	if err = ctx.WritePlaceholderWithArg(own, tableName); err != nil {
		return err
	}

	return ctx.WriteString("\nORDER BY\n    `ordinal_position`")
}

// BuildTableIndexesQuerySQL 构造数据表索引名查询语句
//
// PRIMARY 是主键约束的索引名，不属于模型声明的索引，这里直接过滤。
func (own *mysql) BuildTableIndexesQuerySQL(ctx *clause.Context, namespace, tableName string) (err error) {
	if tableName == "" {
		return ErrTableNameIsEmpty
	}

	_ = ctx.WriteString("SELECT DISTINCT\n    `index_name`\nFROM\n    information_schema.statistics\nWHERE\n    `table_schema` = ")
	if err = own.writeNamespace(ctx, namespace); err != nil {
		return err
	}

	_ = ctx.WriteString(" AND `table_name` = ")
	if err = ctx.WritePlaceholderWithArg(own, tableName); err != nil {
		return err
	}

	return ctx.WriteString(" AND `index_name` <> 'PRIMARY'\nORDER BY\n    `index_name`")
}

// BuildTableCreateSQL 构造数据表创建语句
func (own *mysql) BuildTableCreateSQL(ctx *clause.Context, tableName string, columns []column.Column, metadata ...TableMetadata) (err error) {
	if tableName == "" {
		return ErrTableNameIsEmpty
	}

	if len(metadata) > 0 {
		return own.buildCreateSQL(ctx, tableName, &metadata[0], columns,
			own.buildColumns,      // 处理数据字段
			own.buildPrimaryKeys,  // 处理主键约束
			own.buildUnionUniques, // 处理唯一约束
			own.buildForeignKeys,  // 处理外键约束
		)
	}

	return own.buildCreateSQL(ctx, tableName, &TableMetadata{}, columns,
		own.buildColumns,      // 处理数据字段
		own.buildPrimaryKeys,  // 处理主键约束
		own.buildUnionUniques, // 处理唯一约束
		own.buildForeignKeys,  // 处理外键约束
	)
}

// buildCreateSQL 构造创建数据表的 SQL 语句
func (own *mysql) buildCreateSQL(ctx *clause.Context, tableName string, metadata *TableMetadata, columns []column.Column, generators ...func(ctx *clause.Context, columns []column.Column) error) error {
	if tableName == "" {
		return ErrTableNameIsEmpty
	}

	// 表定义开始
	_ = ctx.WriteString("CREATE TABLE ")
	_ = ctx.WriteString(own.QuoteIdentifier(tableName))
	_ = ctx.WriteString(" (\n")

	// 定义其他信息，如：数据表字段、主键、外键、联合唯一以及其他约束条件
	// 索引不在此处生成：命名与未命名的索引统一由 BuildTableIndexSQLs 在建表后创建，
	// 否则同名索引会因建表内联与 CREATE INDEX 重复而冲突。
	for _, generator := range generators {
		if err := generator(ctx, columns); err != nil {
			return err
		}
	}

	_ = ctx.WriteString("\n)")

	// 表定义结束
	if metadata.Engine != "" {
		_ = ctx.WriteString(" ENGINE=")
		_ = ctx.WriteString(metadata.Engine)
	}
	if metadata.Charset != "" {
		_ = ctx.WriteString(" DEFAULT CHARSET=")
		_ = ctx.WriteString(metadata.Charset)
	}
	if metadata.Collate != "" {
		_ = ctx.WriteString(" COLLATE=")
		_ = ctx.WriteString(metadata.Collate)
	}
	if metadata.Comment != "" {
		_ = ctx.WriteString(" COMMENT='")
		_ = ctx.WriteString(strings.ReplaceAll(metadata.Comment, "'", "''"))
		_ = ctx.WriteByte('\'')
	}

	return ctx.WriteString(";")
}

// BuildTableIndexSQLs 构造索引创建语句
//
// MySQL 的列定义无法内联普通索引，这里返回建表后需要逐条执行的 CREATE INDEX 语句，
// 与 PostgreSQL / SQLite 的处理方式保持一致。
func (own *mysql) BuildTableIndexSQLs(tableName string, columns []column.Column) []string {
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
func (own *mysql) buildColumns(ctx *clause.Context, columns []column.Column) error {
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
func (own *mysql) buildPrimaryKeys(ctx *clause.Context, columns []column.Column) error {
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
func (own *mysql) buildUnionUniques(ctx *clause.Context, columns []column.Column) error {
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
func (own *mysql) buildForeignKeys(ctx *clause.Context, columns []column.Column) error {
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

		// 写入 CONSTRAINT `fk_name`
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

		// 写入 FOREIGN KEY (`column1`, ...)
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

		// 写入 REFERENCES `other_table` (`other_column1`, ...)
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

// sortKeys 已移至 metadata.go，供各方言共用
