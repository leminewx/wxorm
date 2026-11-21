package result

import "time"

type batchResult struct {
	totalRows    int64
	lastInsertID int64
	cost         time.Duration
	sql          string
	args         []any
}

func NewBatchResult(totalRows, lastInsertID int64, cost time.Duration, sql string, args ...any) Result {
	return &batchResult{
		totalRows:    totalRows,
		lastInsertID: lastInsertID,
		cost:         cost,
		sql:          sql,
		args:         copyArgs(args),
	}
}

func (own *batchResult) LastInsertId() (int64, error) { return own.lastInsertID, nil }

func (own *batchResult) RowsAffected() (int64, error) { return own.totalRows, nil }

func (own *batchResult) GetExecutionCost() time.Duration { return own.cost }

func (own *batchResult) GetExecutionSQL() string { return own.sql }

func (own *batchResult) GetExecutionArgs() []any { return own.args }
