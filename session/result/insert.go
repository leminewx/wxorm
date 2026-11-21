package result

import "time"

var _ Result = (*insertResult)(nil)

/*
insertResult 描述 INSERT ... RETURNING 的执行结果

PostgreSQL 无法通过 sql.Result.LastInsertId 拿到自增主键，
主键只能来自 RETURNING 子句返回的行，因此需要一个自带主键值的结果类型。
*/
type insertResult struct {
	id   int64
	rows int64
	cost time.Duration
	sql  string
	args []any
}

// NewInsertResult 创建带 RETURNING 主键的插入结果
//   - id: 数据库生成的主键，未生成主键时为 0
//   - rows: 实际插入的行数，ON CONFLICT DO NOTHING 命中时为 0
func NewInsertResult(id, rows int64, cost time.Duration, sql string, args ...any) Result {
	return &insertResult{
		id:   id,
		rows: rows,
		cost: cost,
		sql:  sql,
		args: copyArgs(args),
	}
}

// LastInsertId 返回 RETURNING 得到的主键
func (own *insertResult) LastInsertId() (int64, error) { return own.id, nil }

// RowsAffected 返回插入的行数
func (own *insertResult) RowsAffected() (int64, error) { return own.rows, nil }

func (own *insertResult) GetExecutionCost() time.Duration { return own.cost }

func (own *insertResult) GetExecutionSQL() string { return own.sql }

func (own *insertResult) GetExecutionArgs() []any { return own.args }
