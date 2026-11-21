package dialect

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/dialect/column"
	"gitee.com/leminewx/wxorm/dialect/datatype"
)

type (
	Dialect interface {
		clause.Clause

		// ParseColumn 解析数据表字段
		ParseColumn(tag string) (column.Column, error)
		// ConvertDatatype 转换数据类型
		ConvertDatatype(datatype reflect.Type) datatype.Datatype

		// GetDriverName 获取驱动名称
		GetDriverName() string
		// GetPrimaryKeyColumns 获取主键字段
		GetPrimaryKeyColumns(columns []column.Column) []column.Column
		// GetForeignKeyColumns 获取外键字段
		GetForeignKeyColumns(columns []column.Column) map[string][]column.Column
		// GetUnionIndexColumns 获取联合索引字段
		GetUnionIndexColumns(columns []column.Column) map[string][]column.Column
		// GetUnionUniqueColumns 获取联合索引字段
		GetUnionUniqueColumns(columns []column.Column) map[string][]column.Column

		// BuildTableQuerySQL 构造数据表存在性查询语句
		//
		// namespace 为库/Schema 名，空表示由方言使用当前命名空间
		// （MySQL 用 DATABASE()，PostgreSQL 用 current_schema()，SQLite 忽略）。
		BuildTableQuerySQL(ctx *clause.Context, namespace, tableName string) error
		// BuildTableColumnsQuerySQL 构造数据表列名查询语句（按定义顺序返回）
		BuildTableColumnsQuerySQL(ctx *clause.Context, namespace, tableName string) error
		// BuildTableCreateSQL 构造数据表创建语句
		BuildTableCreateSQL(ctx *clause.Context, tableName string, columns []column.Column, metadata ...TableMetadata) error
	}

	/*
		TableIndexer 方言需要单独创建索引时实现

		MySQL 的列定义不接受内联普通索引（KEY 在列定义里是主键同义词），
		PostgreSQL 与 SQLite 也不支持内联 INDEX，三者都必须建表后单独执行
		CREATE INDEX。会话通过本接口拿到待执行的语句列表。
	*/
	TableIndexer interface {
		BuildTableIndexSQLs(tableName string, columns []column.Column) []string
	}

	/*
		TableCommenter 方言需要通过独立语句写注释时实现

		PostgreSQL 没有内联表注释与列注释，只能用 COMMENT ON TABLE / COLUMN 单独执行。
	*/
	TableCommenter interface {
		BuildTableCommentSQLs(tableName string, columns []column.Column, metadata ...TableMetadata) []string
	}

	/*
		TableColumnTyper 方言能返回列类型时实现

		迁移时需要把模型声明的类型与数据库实际类型对比。方言各自读取数据字典：
		MySQL 用 information_schema.columns.column_type，PostgreSQL 用 pg_attribute + format_type，
		SQLite 用 pragma_table_info。查询结果按定义顺序返回（列名, 类型）两列。
	*/
	TableColumnTyper interface {
		BuildTableColumnTypesQuerySQL(ctx *clause.Context, namespace, tableName string) error
	}

	/*
		TableIndexReader 方言能列出已有索引时实现

		迁移时需要判断模型声明的索引是否已建立，避免重复执行 CREATE INDEX。
	*/
	TableIndexReader interface {
		BuildTableIndexesQuerySQL(ctx *clause.Context, namespace, tableName string) error
	}

	/*
		TableCaseInsensitive 由标识符不区分大小写的方言实现

		MySQL 在 Windows/macOS 默认 lower_case_table_names=1，SQLite 解析表名时忽略 ASCII 大小写，
		因此 information_schema/sqlite_master 返回的名字可能与模型声明的大小写不同。
		会话据此决定表存在性比对是否忽略大小写，避免误判为“表不存在”而重复建表。
	*/
	TableCaseInsensitive interface {
		IsTableCaseInsensitive() bool
	}

	// TableMetadata 数据库元数据
	TableMetadata struct {
		Engine  string // 数据库引擎
		Charset string // 数据库字符集
		Collate string // 数据库排序规则
		Comment string // 数据库注释
	}
)

// dialects 保存已注册的方言
//
// 内置方言在 init 中注册，但第三方方言可能在任何时候调用 Register；
// 普通 map 在注册与查询并发时会直接触发 fatal error（concurrent map read and map write），
// 因此改为带锁访问。
var (
	dialectsMu sync.RWMutex
	dialects   = map[string]Dialect{}
)

// ErrUnsupportedDriver 表示请求的数据库方言尚未注册
var ErrUnsupportedDriver = errors.New("wxorm: unsupported database driver")

// Register 注册指定的 Dialect
func Register(name string, dialect Dialect) {
	key := normalizeDriverName(name)

	dialectsMu.Lock()
	dialects[key] = dialect
	dialectsMu.Unlock()
}

// Get 获取指定的 Dialect，未注册时返回 nil
//
// 需要在未注册时拿到明确错误，请使用 Lookup。
func Get(name string) Dialect {
	dialectsMu.RLock()
	dial := dialects[normalizeDriverName(name)]
	dialectsMu.RUnlock()

	return dial
}

// normalizeDriverName 归一化方言名称，注册与查询必须使用同一规则
func normalizeDriverName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// Lookup 获取指定的 Dialect，未注册时返回 ErrUnsupportedDriver
//
// 与 Get 相比，Lookup 能避免调用方在方言缺失时拿到 nil 并在后续解引用时 panic。
func Lookup(name string) (Dialect, error) {
	if dial := Get(name); dial != nil {
		return dial, nil
	}

	return nil, fmt.Errorf("%w: %s", ErrUnsupportedDriver, name)
}
