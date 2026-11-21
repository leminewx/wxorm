package dialect

import (
	"reflect"
	"strings"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/dialect/column"
	"gitee.com/leminewx/wxorm/dialect/datatype"
)

const (
	// Postgres 是 PostgreSQL 方言的默认注册名
	Postgres = "postgres"
	// Postgresql 是 PostgreSQL 方言的别名，兼容常见写法
	Postgresql = "postgresql"
	// Pgx 是 pgx 驱动的注册名，用它创建的引擎同样映射到 PostgreSQL 方言
	Pgx = "pgx"
)

func init() {
	dial := NewPostgresDialect()
	Register(Postgres, dial)
	Register(Postgresql, dial)
	Register(Pgx, dial)
}

/*
postgres 定义 PostgreSQL 方言

SQL 文本全部由 clause.PostgresClause 负责，本类型只处理“方言边界”：
字段解析、类型映射、表结构查询与建表语句。
*/
type postgres struct {
	clause.PostgresClause
}

var (
	_ Dialect          = (*postgres)(nil)
	_ TableIndexer     = (*postgres)(nil)
	_ TableCommenter   = (*postgres)(nil)
	_ TableColumnTyper = (*postgres)(nil)
	_ TableIndexReader = (*postgres)(nil)
)

// NewPostgresDialect 创建 PostgreSQL 数据库适配器
func NewPostgresDialect() Dialect {
	return &postgres{
		PostgresClause: clause.NewPostgresClause(),
	}
}

// ParseColumn 解析数据表字段
func (own *postgres) ParseColumn(tag string) (column.Column, error) {
	return column.NewPostgresColumn(tag)
}

// ConvertDatatype 转换 Go 结构体数据类型为 PostgreSQL 对应的数据类型
func (own *postgres) ConvertDatatype(data reflect.Type) datatype.Datatype {
	return datatype.ConvertGoDatatypeToPostgresDatatype(data)
}

// GetDriverName 获取驱动名称
//
// 方言名（postgres/postgresql/pgx）可以有多个别名，
// 但数据模型缓存按驱动名分桶，这里固定返回 postgres，让三个别名共用同一份缓存。
func (own *postgres) GetDriverName() string {
	return Postgres
}

// GetClause 获取处理子句相关的接口
func (own *postgres) GetClause() clause.Clause {
	return own.PostgresClause
}

// GetPrimaryKeyColumns 获取主键字段
func (own *postgres) GetPrimaryKeyColumns(columns []column.Column) []column.Column {
	return filterPrimaryKeyColumns(columns)
}

// GetForeignKeyColumns 获取外键字段
func (own *postgres) GetForeignKeyColumns(columns []column.Column) map[string][]column.Column {
	return groupForeignKeyColumns(columns)
}

// GetUnionIndexColumns 获取联合索引字段
func (own *postgres) GetUnionIndexColumns(columns []column.Column) map[string][]column.Column {
	return groupMetadataColumns(columns, column.Column.GetIndex)
}

// GetUnionUniqueColumns 获取联合唯一字段
func (own *postgres) GetUnionUniqueColumns(columns []column.Column) map[string][]column.Column {
	return groupMetadataColumns(columns, column.Column.GetUnique)
}

/*
BuildTableQuerySQL 构造数据表存在性查询语句

PostgreSQL 的表名在库（catalog）内按 schema 隔离。namespace 为空时取 current_schema()，
因此通过 PGOPTIONS/search_path 切换 schema 后无需改代码；显式指定时按该 schema 过滤。
库（catalog）无需过滤：一个连接只能访问当前库。
*/
func (own *postgres) BuildTableQuerySQL(ctx *clause.Context, namespace, tableName string) error {
	if tableName == "" {
		return ErrTableNameIsEmpty
	}

	_ = ctx.WriteString("SELECT\n    table_name\nFROM\n    information_schema.tables\nWHERE\n    table_schema = ")
	if err := own.writeNamespace(ctx, namespace); err != nil {
		return err
	}

	_ = ctx.WriteString(" AND table_name = ")
	return ctx.WritePlaceholderWithArg(own, tableName)
}

// writeNamespace 写入 schema 过滤表达式，未指定时取 current_schema()
func (own *postgres) writeNamespace(ctx *clause.Context, namespace string) error {
	if namespace == "" {
		return ctx.WriteString("current_schema()")
	}

	return ctx.WritePlaceholderWithArg(own, namespace)
}

/*
BuildTableColumnsQuerySQL 构造数据表列名查询语句

迁移时不能靠 SELECT * 读列名：pgx 会缓存该语句的执行计划，
列集变化后再次执行会报 cached plan must not change result type。
*/
func (own *postgres) BuildTableColumnsQuerySQL(ctx *clause.Context, namespace, tableName string) error {
	if tableName == "" {
		return ErrTableNameIsEmpty
	}

	_ = ctx.WriteString("SELECT\n    column_name\nFROM\n    information_schema.columns\nWHERE\n    table_schema = ")
	if err := own.writeNamespace(ctx, namespace); err != nil {
		return err
	}

	_ = ctx.WriteString(" AND table_name = ")
	if err := ctx.WritePlaceholderWithArg(own, tableName); err != nil {
		return err
	}

	return ctx.WriteString("\nORDER BY\n    ordinal_position")
}

/*
BuildTableColumnTypesQuerySQL 构造数据表列名与类型查询语句

format_type 会把 regtype 还原成带修饰的类型（如 character varying(50)、numeric(10,2)），
比 information_schema.data_type 更接近模型声明的写法，可直接用于迁移比对。
*/
func (own *postgres) BuildTableColumnTypesQuerySQL(ctx *clause.Context, namespace, tableName string) error {
	if tableName == "" {
		return ErrTableNameIsEmpty
	}

	qualified := own.QuoteIdentifier(tableName)
	if namespace != "" {
		qualified = own.QuoteIdentifier(namespace) + "." + qualified
	}

	_ = ctx.WriteString("SELECT\n    attname,\n    format_type(atttypid, atttypmod)\nFROM\n    pg_attribute\nWHERE\n    attrelid = ")
	if err := ctx.WritePlaceholderWithArg(own, qualified); err != nil {
		return err
	}

	return ctx.WriteString("::regclass AND attnum > 0 AND NOT attisdropped\nORDER BY\n    attnum")
}

/*
BuildTableIndexesQuerySQL 构造数据表索引名查询语句

pg_indexes 会包含主键索引（*_pkey）；比对只用于判断模型声明的索引是否缺失，
多余项不会影响结论，因此不在 SQL 层过滤。
*/
func (own *postgres) BuildTableIndexesQuerySQL(ctx *clause.Context, namespace, tableName string) error {
	if tableName == "" {
		return ErrTableNameIsEmpty
	}

	_ = ctx.WriteString("SELECT\n    indexname\nFROM\n    pg_indexes\nWHERE\n    schemaname = ")
	if err := own.writeNamespace(ctx, namespace); err != nil {
		return err
	}

	_ = ctx.WriteString(" AND tablename = ")
	if err := ctx.WritePlaceholderWithArg(own, tableName); err != nil {
		return err
	}

	return ctx.WriteString("\nORDER BY\n    indexname")
}

/*
BuildTableCreateSQL 构造数据表创建语句

PostgreSQL 没有 ENGINE/CHARSET/COLLATE 这些表级选项，
索引与注释也无法内联，需要建表后单独执行（见 BuildTableIndexSQLs / BuildTableCommentSQLs）。
*/
func (own *postgres) BuildTableCreateSQL(ctx *clause.Context, tableName string, columns []column.Column, _ ...TableMetadata) error {
	if tableName == "" {
		return ErrTableNameIsEmpty
	}

	// 表定义开始
	_ = ctx.WriteString("CREATE TABLE ")
	_ = ctx.WriteString(own.QuoteIdentifier(tableName))
	_ = ctx.WriteString(" (\n")

	// 定义数据表字段、主键、联合唯一与外键约束
	generators := []func(ctx *clause.Context, columns []column.Column) error{
		own.buildColumns,     // 处理数据字段
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
// PostgreSQL 不支持在 CREATE TABLE 内联 INDEX，
// 这里返回建表后需要逐条执行的 CREATE INDEX 语句。
func (own *postgres) BuildTableIndexSQLs(tableName string, columns []column.Column) []string {
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

// BuildTableCommentSQLs 构造注释语句
func (own *postgres) BuildTableCommentSQLs(tableName string, columns []column.Column, metadata ...TableMetadata) []string {
	if tableName == "" {
		return nil
	}

	var stmts []string
	if len(metadata) > 0 && metadata[0].Comment != "" {
		stmts = append(stmts, "COMMENT ON TABLE "+own.QuoteIdentifier(tableName)+" IS "+quoteLiteral(metadata[0].Comment)+";")
	}

	for _, col := range columns {
		commenter, ok := col.(interface{ GetComment() string })
		if !ok {
			continue
		}

		// Comment 在解析标签时已带外层引号并完成转义
		if comment := commenter.GetComment(); comment != "" {
			stmts = append(stmts, "COMMENT ON COLUMN "+own.QuoteIdentifier(tableName)+"."+own.QuoteIdentifier(col.GetName())+" IS "+comment+";")
		}
	}

	return stmts
}

// buildColumns 构造数据表字段信息
func (own *postgres) buildColumns(ctx *clause.Context, columns []column.Column) error {
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
func (own *postgres) buildPrimaryKeys(ctx *clause.Context, columns []column.Column) error {
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
func (own *postgres) buildUnionUniques(ctx *clause.Context, columns []column.Column) error {
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
func (own *postgres) buildForeignKeys(ctx *clause.Context, columns []column.Column) error {
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

// quoteLiteral 把字符串包成 PostgreSQL 的单引号字面量
func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
