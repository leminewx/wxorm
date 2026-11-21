package column

import (
	"strings"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/dialect/datatype"
)

var _ Column = &PostgresColumn{}

/*
PostgresColumn 定义 PostgreSQL 数据字段的数据结构

	PostgreSQL 数据表创建推荐格式：

	CREATE TABLE "table_name" (
		"column_name" data_type [NOT NULL | NULL] [DEFAULT default_value]
		[GENERATED ALWAYS AS (expr) STORED] [UNIQUE] [CHECK (expr)]
		[REFERENCES other_table (other_column)]
	)

与 MySQL 的差异：
  - 标识符用双引号；
  - 自增用 SERIAL/BIGSERIAL（内部创建序列并绑定默认值），没有 AUTO_INCREMENT；
  - 没有 ON UPDATE CURRENT_TIMESTAMP，更新时间需要触发器维护；
  - 不支持内联 INDEX，索引由 dialect 在建表后单独创建；
  - 不支持内联列注释，注释由 COMMENT ON COLUMN 单独书写。
*/
type PostgresColumn struct {
	*BaseColumn
}

// NewPostgresColumn 新建一个 PostgreSQL 字段
func NewPostgresColumn(tag string) (*PostgresColumn, error) {
	col, err := newColumn(tag)
	if err != nil {
		return nil, err
	}

	return &PostgresColumn{col}, nil
}

// SetName 写入字段名
func (own *PostgresColumn) SetName(name string) {
	own.Column = FormatCamelToSnake([]byte(name))
}

// SetDatatype 写入数据类型
func (own *PostgresColumn) SetDatatype(datatype datatype.Datatype) {
	own.Datatype = datatype
}

// IsAutoInsert 是否自动插入
//
// created / updated 时间戳都由数据库生成：created 是插入时间，
// updated 在首次插入时应等于插入时间，因此两者都不参与 INSERT 参数绑定。
func (own *PostgresColumn) IsAutoInsert() bool {
	if !own.AutoIncrement && !own.Created && !own.Updated {
		return false
	}

	return own.AutoIncrement || isTimestampDatatype(own.Datatype)
}

// IsAutoUpdate 是否自动更新
//
// PostgreSQL 没有 ON UPDATE CURRENT_TIMESTAMP，自动更新时间需要触发器，
// 这里恒返回 false，避免生成非法 DDL。
func (own *PostgresColumn) IsAutoUpdate() bool {
	return false
}

// BuildCreateSQL 构造创建 PostgreSQL 数据表的字段信息
func (own *PostgresColumn) BuildCreateSQL(ctx *clause.Context) error {
	// 写入字段名 "column"
	_ = ctx.WriteByte('"')
	_ = ctx.WriteString(strings.ReplaceAll(own.Column, `"`, `""`))
	_ = ctx.WriteString(`" `)

	// 写入数据类型（自增整型用 SERIAL/BIGSERIAL 承载序列与默认值）
	_ = ctx.WriteString(own.buildPostgresDatatype())

	// 写入 NULL 或 NOT NULL
	if own.Nullable.Has {
		_ = ctx.WriteByte(' ')
		_ = ctx.WriteString(own.Nullable.Value)
	}

	// 写入默认值（SERIAL/BIGSERIAL 已带默认值，不能再写）
	switch {
	case own.AutoIncrement:
	case (own.Created || own.Updated) && isTimestampDatatype(own.Datatype):
		_ = ctx.WriteString(" DEFAULT CURRENT_TIMESTAMP")
	case own.Default.Has:
		_ = ctx.WriteString(" DEFAULT ")
		_ = ctx.WriteString(own.Default.Value)
	}

	// 写入生成列，PostgreSQL 只支持 STORED 存储方式
	if own.Generated.Has && own.Generated.Value != "" {
		_ = ctx.WriteString(" GENERATED ALWAYS AS (")
		_ = ctx.WriteString(own.Generated.Value)
		_ = ctx.WriteString(") STORED")
	}

	// 写入（非联合）唯一约束
	if own.Unique.Has && own.Unique.Value == "" {
		_ = ctx.WriteString(" UNIQUE")
	}

	// 写入检查约束
	if own.Check != "" {
		_ = ctx.WriteString(" CHECK (")
		_ = ctx.WriteString(own.Check)
		_ = ctx.WriteByte(')')
	}

	return nil
}

// buildPostgresDatatype 返回字段在 PostgreSQL 中使用的类型
func (own *PostgresColumn) buildPostgresDatatype() string {
	if !own.AutoIncrement {
		return string(own.Datatype)
	}

	// SERIAL 是 INTEGER + 序列，BIGSERIAL 是 BIGINT + 序列
	switch own.Datatype {
	case datatype.BIGINT, datatype.BIGSERIAL:
		return string(datatype.BIGSERIAL)
	default:
		return string(datatype.SERIAL)
	}
}

// isTimestampDatatype 判断是否为带时区/不带时区的时间戳类型
func isTimestampDatatype(data datatype.Datatype) bool {
	switch data {
	case datatype.TIMESTAMP, datatype.TIMESTAMPTZ, datatype.TIMESTAMPZ:
		return true
	default:
		return false
	}
}
