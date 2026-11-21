package column

import (
	"errors"
	"strings"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/dialect/datatype"
)

var _ Column = &SQLiteColumn{}

// ErrAutoIncrementRequiresPrimaryKey 表示 SQLite 的自增标记没有落在主键上
//
// SQLite 只允许「INTEGER PRIMARY KEY AUTOINCREMENT」这一种写法，
// 没有主键的 AUTOINCREMENT 生成出来就是非法 DDL，因此在构建阶段直接报错。
var ErrAutoIncrementRequiresPrimaryKey = errors.New("wxorm: SQLite requires AUTOINCREMENT to be declared on a primary key")

/*
SQLiteColumn 定义 SQLite 数据字段的数据结构

	SQLite 数据表创建推荐格式：

	CREATE TABLE "table_name" (
		"column_name" data_type [PRIMARY KEY [AUTOINCREMENT]] [NOT NULL | NULL]
		[DEFAULT default_value] [GENERATED ALWAYS AS (expr) VIRTUAL]
		[UNIQUE] [CHECK (expr)] [REFERENCES other_table (other_column)]
	)

与 MySQL 的差异：
  - 自增必须是 INTEGER 类型且内联 PRIMARY KEY，否则 SQLite 不会把它当作 rowid 别名；
  - 没有 ON UPDATE CURRENT_TIMESTAMP，更新时间只能由业务或触发器维护；
  - 不支持内联 INDEX，索引由 dialect 在建表后单独创建；
  - 不支持列注释（COMMENT）、字符集（CHARACTER SET）与 COLLATE 之外的列选项。
*/
type SQLiteColumn struct {
	*BaseColumn
}

// NewSQLiteColumn 新建一个 SQLite 字段
func NewSQLiteColumn(tag string) (*SQLiteColumn, error) {
	col, err := newColumn(tag)
	if err != nil {
		return nil, err
	}

	return &SQLiteColumn{col}, nil
}

// SetName 写入字段名
func (own *SQLiteColumn) SetName(name string) {
	own.Column = FormatCamelToSnake([]byte(name))
}

// SetDatatype 写入数据类型
func (own *SQLiteColumn) SetDatatype(datatype datatype.Datatype) {
	own.Datatype = datatype
}

// IsAutoInsert 是否自动插入
//
// created / updated 时间戳都由数据库生成：created 是插入时间，
// updated 在首次插入时应等于插入时间，因此两者都不参与 INSERT 参数绑定。
func (own *SQLiteColumn) IsAutoInsert() bool {
	if !own.AutoIncrement && !own.Created && !own.Updated {
		return false
	}

	return own.AutoIncrement || own.Datatype == datatype.DATETIME
}

// IsAutoUpdate 是否自动更新
//
// SQLite 没有 ON UPDATE CURRENT_TIMESTAMP，自动更新时间需要触发器，
// 这里恒返回 false，避免生成非法 DDL。
func (own *SQLiteColumn) IsAutoUpdate() bool {
	return false
}

// IsInlinePrimaryKey 判断该列是否已在列定义中内联声明主键
//
// SQLite 的自增主键必须写成「INTEGER PRIMARY KEY AUTOINCREMENT」，
// dialect 需要据此跳过表级 PRIMARY KEY 约束，否则会出现两个主键声明。
func (own *SQLiteColumn) IsInlinePrimaryKey() bool {
	return own.AutoIncrement && own.PrimaryKey
}

// BuildCreateSQL 构造创建 SQLite 数据表的字段信息
func (own *SQLiteColumn) BuildCreateSQL(ctx *clause.Context) error {
	// 自增必须落在主键上，否则 SQLite 不接受 AUTOINCREMENT
	if own.AutoIncrement && !own.PrimaryKey {
		return ErrAutoIncrementRequiresPrimaryKey
	}

	// 写入字段名 "column"
	_ = ctx.WriteByte('"')
	_ = ctx.WriteString(strings.ReplaceAll(own.Column, `"`, `""`))
	_ = ctx.WriteString(`" `)

	// 自增主键必须用 INTEGER，AUTOINCREMENT 才会生效（rowid 别名）
	if own.IsInlinePrimaryKey() {
		_ = ctx.WriteString("INTEGER PRIMARY KEY AUTOINCREMENT")
	} else {
		_ = ctx.WriteString(string(own.Datatype))
	}

	// 写入 NULL 或 NOT NULL（自增主键隐含非空）
	if own.Nullable.Has && !own.IsInlinePrimaryKey() {
		_ = ctx.WriteByte(' ')
		_ = ctx.WriteString(own.Nullable.Value)
	}

	// 写入默认值（自增列不能再写默认值）
	switch {
	case own.IsInlinePrimaryKey():
	case (own.Created || own.Updated) && own.Datatype == datatype.DATETIME:
		_ = ctx.WriteString(" DEFAULT CURRENT_TIMESTAMP")
	case own.Default.Has:
		_ = ctx.WriteString(" DEFAULT ")
		_ = ctx.WriteString(own.Default.Value)
	}

	// 写入生成列，SQLite 支持 VIRTUAL 与 STORED，默认 VIRTUAL 成本更低
	if own.Generated.Has && own.Generated.Value != "" {
		_ = ctx.WriteString(" GENERATED ALWAYS AS (")
		_ = ctx.WriteString(own.Generated.Value)
		_ = ctx.WriteString(") VIRTUAL")
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

	// 索引：SQLite 不支持内联 INDEX，由 dialect 建表后单独创建
	return nil
}
