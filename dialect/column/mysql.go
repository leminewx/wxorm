package column

import (
	"strings"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/dialect/datatype"
)

var _ Column = &MySQLColumn{}

/*
MySQLColumn 定义 MySQL 数据字段的数据结构

	MySQL 数据表创建推荐格式：

	CREATE TABLE `table_name` (
		`column_name` data_type [NOT NULL | NULL] [DEFAULT default_value]
		[AUTO_INCREMENT] [UNIQUE [KEY]] [PRIMARY KEY]
		[COMMENT 'string']
		[COLLATE collation_name]
		[COLUMN_FORMAT {FIXED|DYNAMIC|DEFAULT}]
		[STORAGE {DISK|MEMORY}]
		[REFERENCES other_table (other_column)]
	)

普通索引不在此列表内：MySQL 的列定义不支持内联 INDEX，
单列与复合索引由方言层在建表后通过 CREATE INDEX 创建。
*/
type MySQLColumn struct {
	*BaseColumn
}

// NewMySQLColumn 新建一个 MySQL 字段
func NewMySQLColumn(tag string) (*MySQLColumn, error) {
	col, err := newColumn(tag)
	if err != nil {
		return nil, err
	}

	return &MySQLColumn{col}, nil
}

// SetName 写入字段名
func (own *MySQLColumn) SetName(name string) {
	own.Column = FormatCamelToSnake([]byte(name))
}

// SetDatatype 写入数据类型
func (own *MySQLColumn) SetDatatype(datatype datatype.Datatype) {
	own.Datatype = datatype
}

// IsAutoInsert 是否自动插入
//
// created / updated 时间戳都由数据库生成：created 是插入时间，
// updated 在首次插入时应等于插入时间，因此两者都不参与 INSERT 参数绑定。
func (own *MySQLColumn) IsAutoInsert() bool {
	if !own.AutoIncrement && !own.Created && !own.Updated {
		return false
	}

	return own.AutoIncrement || own.Datatype == datatype.DATETIME || own.Datatype == datatype.TIMESTAMP
}

// IsAutoUpdate 是否自动更新
func (own *MySQLColumn) IsAutoUpdate() bool {
	return own.Updated && (own.Datatype == datatype.DATETIME || own.Datatype == datatype.TIMESTAMP)
}

// BuildCreateSQL 构造创建 MySQL 数据表的字段信息
func (own *MySQLColumn) BuildCreateSQL(ctx *clause.Context) error {
	// 写入字段名 `column`，内部反引号成对转义，与 QuoteIdentifier 保持一致
	_ = ctx.WriteByte('`')
	_ = ctx.WriteString(strings.ReplaceAll(own.Column, "`", "``"))
	_ = ctx.WriteString("` ")

	// 写入数据类型
	_ = ctx.WriteString(string(own.Datatype))

	// 写入 NULL 或 NOT NULL
	if own.Nullable.Has {
		_ = ctx.WriteByte(' ')
		_ = ctx.WriteString(own.Nullable.Value)
	}

	// 写入默认值
	if own.Default.Has || own.Created || own.Updated {
		if (own.Created || own.Updated) && (own.Datatype == "DATETIME" || own.Datatype == "TIMESTAMP") {
			_ = ctx.WriteString(" DEFAULT CURRENT_TIMESTAMP")
		} else {
			_ = ctx.WriteString(" DEFAULT ")
			_ = ctx.WriteString(own.Default.Value)
		}
	}

	// 自动更新
	if own.Updated && (own.Datatype == "DATETIME" || own.Datatype == "TIMESTAMP") {
		_ = ctx.WriteString(" ON UPDATE CURRENT_TIMESTAMP")
	}

	// 写入生成列（默认 VIRTUAL，显式写 STORED 才是物理存储）
	if own.Generated.Has && own.Generated.Value != "" {
		_ = ctx.WriteString(" GENERATED ALWAYS AS (")
		_ = ctx.WriteString(own.Generated.Value)
		_ = ctx.WriteString(") VIRTUAL")
	}

	// 写入主键自增信息（仅支持 INT、INTEGER、BIGINT 与他们的无符号类型支持自增）
	if own.AutoIncrement {
		_ = ctx.WriteString(" AUTO_INCREMENT")
	}

	// 写入（非联合）唯一信息
	//
	// MySQL 的列定义语法不接受内联 INDEX（单列普通索引在列定义里的 KEY 是主键同义词），
	// 单列与复合索引统一由方言层在建表后通过 CREATE INDEX 创建。
	if own.Unique.Has && own.Unique.Value == "" {
		_ = ctx.WriteString(" UNIQUE")
	}

	// 写入字段描述信息
	if own.Comment != "" {
		_ = ctx.WriteString(" COMMENT ")
		_ = ctx.WriteString(own.Comment)
	}

	// 写入字符集
	if own.Charset != "" {
		_ = ctx.WriteString(" CHARACTER SET ")
		_ = ctx.WriteString(own.Charset)
	}

	// 写入排序规则
	if own.Collate != "" {
		_ = ctx.WriteString(" COLLATE ")
		_ = ctx.WriteString(own.Collate)
	}

	// 写入格式信息
	if own.Format != "" {
		_ = ctx.WriteString(" COLUMN_FORMAT ")
		_ = ctx.WriteString(own.Format)
	}

	// 写入存储信息
	if own.Storage != "" {
		_ = ctx.WriteString(" STORAGE ")
		_ = ctx.WriteString(own.Storage)
	}

	return nil
}
