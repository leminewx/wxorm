package result

import (
	"database/sql"
	"errors"
	"time"
)

var (
	// ErrNoExecutionResult 表示结果对象没有对应的底层执行结果
	//
	// 执行失败时调用方应当先处理 error；若仍调用 LastInsertId / RowsAffected，
	// 返回本错误而不是空指针 panic。
	ErrNoExecutionResult = errors.New("wxorm: execution has no result")
)

type Result interface {
	// Result inherits the sql.Result interface
	sql.Result

	// GetExecutionCost returns the execute cost
	GetExecutionCost() time.Duration
	// GetExecutionSQL returns the execute sql
	GetExecutionSQL() string
	// GetExecutionArgs returns to execute args
	GetExecutionArgs() []any
}

type result struct {
	res  sql.Result
	cost time.Duration
	sql  string
	args []any
}

func NewResult(res sql.Result, cost time.Duration, sql string, args ...any) Result {
	return &result{
		res:  res,
		cost: cost,
		sql:  sql,
		args: copyArgs(args),
	}
}

// copyArgs 复制参数切片，避免语句构建器复用底层数组后篡改已返回的执行参数
func copyArgs(args []any) []any {
	if len(args) == 0 {
		return nil
	}

	copied := make([]any, len(args))
	copy(copied, args)
	return copied
}

func (own *result) LastInsertId() (int64, error) {
	if own.res == nil {
		return 0, ErrNoExecutionResult
	}

	return own.res.LastInsertId()
}

func (own *result) RowsAffected() (int64, error) {
	if own.res == nil {
		return 0, ErrNoExecutionResult
	}

	return own.res.RowsAffected()
}

func (own *result) GetExecutionCost() time.Duration { return own.cost }

func (own *result) GetExecutionSQL() string { return own.sql }

func (own *result) GetExecutionArgs() []any { return own.args }
