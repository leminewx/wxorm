package statement

import (
	"gitee.com/leminewx/wxorm/dialect/clause"
)

// Statement 定义 SQL 语句的数据结构
type Statement struct {
	table  string
	ctx    *clause.Context
	builds map[Name]ClauseBuildFunc
}

// NewStatement 创建一个 SQL 语句
func NewStatement(table string) *Statement {
	return &Statement{
		table:  table,
		ctx:    clause.NewContext(),
		builds: make(map[Name]ClauseBuildFunc),
	}
}

// Has 判断某个子句是否存在
func (own *Statement) Has(name Name) bool {
	_, ok := own.builds[name]
	return ok
}

// GetTable 获取数据表名
func (own *Statement) GetTable() string {
	return own.table
}

// SetTable 设置数据表名
func (own *Statement) SetTable(table string) *Statement {
	own.table = table
	return own
}

// AddClause 添加 SQL 子句
func (own *Statement) AddClause(name Name, build ClauseBuildFunc) *Statement {
	own.builds[name] = build
	return own
}

// BuildSQL 构建完整的 SQL 语句和参数列表
func (own *Statement) BuildSQL(cla clause.Clause, names ...Name) (string, []any, error) {
	for _, name := range names {
		// 获取构建函数
		build, ok := own.builds[name]
		if !ok || cla == nil {
			continue
		}

		// 构建 SQL 子句
		if err := build(own.ctx, cla); err != nil {
			return "", nil, err
		}
	}
	_ = own.ctx.WriteByte(';')

	sql, args := own.ctx.GetResult()
	return sql, args, nil
}

// Reset 重置 SQL 语句
func (own *Statement) Reset() {
	own.ctx.Reset()
	clear(own.builds)
}
