package column

import (
	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/dialect/datatype"
)

type (
	// Column 定义数据表字段的接口类型
	Column interface {
		// GetName 获取字段名
		GetName() string
		// GetIndex 获取索引信息
		GetIndex() Metadata
		// GetUnique 获取唯一信息
		GetUnique() Metadata
		// GetDatatype 获取数据类型
		GetDatatype() datatype.Datatype
		// GetForeignKey 获取外键信息
		GetForeignKey() *ForeignKey

		// SetName 手动设置字段名
		SetName(name string)
		// SetDatatype 手动设置数据类型
		SetDatatype(datatype datatype.Datatype)

		// IsAutoInsert 是否自动插入
		IsAutoInsert() bool
		// IsAutoUpdate 是否自动更新
		IsAutoUpdate() bool
		// IsPrimaryKey 是否为主键
		IsPrimaryKey() bool

		// BuildCreateSQL 构造创建数据表的字段信息
		BuildCreateSQL(ctx *clause.Context) error
	}

	// Metadata 定义数据表列的元数据的数据结构
	Metadata struct {
		Has   bool
		Value string
	}

	// ForeignKey 定义数据表外键的数据结构
	ForeignKey struct {
		Name            string // 外键名称
		Table           string // 外键关联的表
		Column          string // 外键关联的列
		OnUpdateCascade bool   // 是否级联更新
		OnDeleteCascade bool   // 是否级联删除
	}
)

type BaseColumn struct {
	AutoIncrement bool              // 是否自增（仅适用于 MySQL）
	PrimaryKey    bool              // 是否主键
	Created       bool              // 创建时自动插入（仅用于 MySQL 的 TIMESTAMP, DATETIME(≥5.6.5) 或 PostgresSQL 的 TIMESTAMP, TIMESTAMPTZ）
	Updated       bool              // 更新时自动插入（仅用于 MySQL 的 TIMESTAMP, DATETIME(≥5.6.5)）
	Column        string            // 数据字段
	Datatype      datatype.Datatype // 数据类型
	Check         string            // 数据类型约束（适用于 SQLite 和 PostgresSQL）
	Charset       string            // 字符集（仅适用于 MySQL，定义字符的编码方式，如：utf8mb4、latin1）
	Collate       string            // 排序规则（主要用于指定字符串的比较和排序规则，控制字符数据的排序顺序、比较规则和大小写敏感性，如：en_US.utf8）
	Format        string            // 列格式（仅适用于 MySQL 的 NDB 引擎）
	Storage       string            // 列存储方式（主要适用于 MySQL 的 InnoDB 引擎，特别针对大对象类型的列，如：TEXT、BLOB）
	Comment       string            // 字段描述
	Default       Metadata          // 默认值
	Nullable      Metadata          // 是否可为空
	Generated     Metadata          // 自增信息（仅适用于 PostgresSQL）
	Index         Metadata          // 索引信息
	Unique        Metadata          // 唯一信息
	ForeignKey    *ForeignKey       // 外键信息
}

// mewColumn 新建一个数据的底层表字段（MySQL/SQLite/PostgresSQL）
func newColumn(tag string) (*BaseColumn, error) {
	parser := &Parser{}
	return parser.Parse(tag)
}

// GetName 获取字段名
func (own *BaseColumn) GetName() string {
	return own.Column
}

// GetIndex 获取索引信息
func (own *BaseColumn) GetIndex() Metadata {
	return own.Index
}

// GetUnique 获取唯一信息
func (own *BaseColumn) GetUnique() Metadata {
	return own.Unique
}

// GetDatatype 获取数据类型
func (own *BaseColumn) GetDatatype() datatype.Datatype {
	return own.Datatype
}

// GetForeignKey 获取外键
func (own *BaseColumn) GetForeignKey() *ForeignKey {
	return own.ForeignKey
}

// GetComment 获取字段注释
//
// 注释在解析标签时已完成单引号转义并保留外层引号，可直接写入 DDL。
func (own *BaseColumn) GetComment() string {
	return own.Comment
}

// IsPrimaryKey 是否为主键
func (own *BaseColumn) IsPrimaryKey() bool {
	return own.PrimaryKey
}

// IsAutoIncrement 是否自增
//
// 不在 Column 接口里，避免破坏外部自定义实现；
// 方言层通过可选接口断言探测（SQLite 需要据此决定主键是否内联）。
func (own *BaseColumn) IsAutoIncrement() bool {
	return own.AutoIncrement
}

/*
FormatCamelToSnake 将驼峰格式转换为下划线格式

eg:
  - FormatCamelToSnake ===> format_camel_to_snake
*/
func FormatCamelToSnake(str []byte) string {
	buf := make([]byte, 0, len(str)*2)
	for i, r := range str {
		if 65 <= r && r < 65+26 {
			if i > 0 {
				buf = append(buf, '_')
			}

			buf = append(buf, r+32)
		} else {
			buf = append(buf, r)
		}
	}

	return string(buf)
}
