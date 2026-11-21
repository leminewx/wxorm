package result

import "time"

type selectResult struct {
	cost time.Duration
	sql  string
	args []any
}

func NewSelectResult(cost time.Duration, sql string, args ...any) Result {
	return &selectResult{
		cost: cost,
		sql:  sql,
		args: copyArgs(args),
	}
}

func (own *selectResult) LastInsertId() (int64, error) { return 0, nil }

func (own *selectResult) RowsAffected() (int64, error) { return 0, nil }

func (own *selectResult) GetExecutionCost() time.Duration { return own.cost }

func (own *selectResult) GetExecutionSQL() string { return own.sql }

func (own *selectResult) GetExecutionArgs() []any { return own.args }
