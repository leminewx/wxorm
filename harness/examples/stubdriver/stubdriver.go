// Package stubdriver 提供 database/sql 的「脚本化行」驱动，用于验证结果映射。
//
// 它不解析任何 SQL，也不维护表数据：每次查询按入队顺序返回预置的列名与行，
// 没有脚本时直接报错，避免测试拿到静默的空结果。典型用途是构造真库与
// harness/examples/fakedriver 都不容易造出的边界：
//
//   - 结果列名带表前缀（`author`.`name`）或点号（author.name）
//   - SQL NULL 到指针字段的映射
//   - COLUMNS 只返回部分列时其余字段保持零值
//   - 返回模型不存在的列
//
// 该包只被测试引用，不参与运行时代码。
package stubdriver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync"
)

// DriverName 是注册到 database/sql 的驱动名
const DriverName = "wxorm-stub"

var (
	// ErrNoScript 表示查询时脚本队列为空
	ErrNoScript = errors.New("stubdriver: no scripted result")
	// ErrRowWidth 表示预置行的列数与列名数量不一致
	ErrRowWidth = errors.New("stubdriver: row width does not match columns")

	registerOnce sync.Once

	// scriptMu 保护脚本队列与调用记录；驱动可能被并发使用
	scriptMu  sync.Mutex
	script    []response
	calls     []string
	argCounts []int
)

// response 是一次脚本化响应
type response struct {
	exec         bool
	columns      []string
	rows         [][]driver.Value
	err          error
	rowsAffected int64
	lastInsertID int64
}

// Register 注册驱动（可重复调用，同名的重复注册会被忽略）
func Register() {
	registerOnce.Do(func() {
		sql.Register(DriverName, &stubDriver{})
	})
}

// Reset 清空脚本队列与调用记录
func Reset() {
	scriptMu.Lock()
	defer scriptMu.Unlock()

	script = nil
	calls = nil
	argCounts = nil
}

// Push 追加一次查询响应：列名与若干行；行内的 nil 表示 SQL NULL
func Push(columns []string, rows ...[]driver.Value) {
	scriptMu.Lock()
	defer scriptMu.Unlock()

	script = append(script, response{columns: columns, rows: rows})
}

// PushError 追加一次查询失败响应
func PushError(err error) {
	scriptMu.Lock()
	defer scriptMu.Unlock()

	script = append(script, response{err: err})
}

// PushExec 追加一次写语句响应
func PushExec(rowsAffected, lastInsertID int64) {
	scriptMu.Lock()
	defer scriptMu.Unlock()

	script = append(script, response{
		exec:         true,
		rowsAffected: rowsAffected,
		lastInsertID: lastInsertID,
	})
}

// Calls 返回已执行的 SQL 文本（按调用顺序）
func Calls() []string {
	scriptMu.Lock()
	defer scriptMu.Unlock()

	out := make([]string, len(calls))
	copy(out, calls)
	return out
}

// ArgCounts 返回每次调用的参数个数（与 Calls 一一对应）
func ArgCounts() []int {
	scriptMu.Lock()
	defer scriptMu.Unlock()

	out := make([]int, len(argCounts))
	copy(out, argCounts)
	return out
}

// take 弹出队首响应，并记录本次 SQL 与参数个数
func take(query string, args int, exec bool) (response, error) {
	scriptMu.Lock()
	defer scriptMu.Unlock()

	calls = append(calls, query)
	argCounts = append(argCounts, args)
	if len(script) == 0 {
		return response{}, ErrNoScript
	}

	resp := script[0]
	script = script[1:]
	if resp.exec != exec {
		return response{}, fmt.Errorf("stubdriver: scripted %s response used for %s call", kindName(resp.exec), kindName(exec))
	}

	return resp, nil
}

func kindName(exec bool) string {
	if exec {
		return "exec"
	}

	return "query"
}

// ---------------------------------------------------------------------------
// database/sql/driver 实现
// ---------------------------------------------------------------------------

type stubDriver struct{}

func (own *stubDriver) Open(string) (driver.Conn, error) {
	return stubConn{}, nil
}

type stubConn struct{}

func (own stubConn) Prepare(query string) (driver.Stmt, error) {
	return &stubStmt{query: query}, nil
}

func (own stubConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	return &stubStmt{query: query}, nil
}

func (own stubConn) Close() error { return nil }

func (own stubConn) Begin() (driver.Tx, error) { return stubTx{}, nil }

// BeginTx 让 database/sql 在开启事务时走上下文版本
func (own stubConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return stubTx{}, nil
}

func (own stubConn) Ping(context.Context) error { return nil }

func (own stubConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	resp, err := take(query, len(args), false)
	if err != nil {
		return nil, err
	}

	if resp.err != nil {
		return nil, resp.err
	}

	return &stubRows{columns: resp.columns, rows: resp.rows}, nil
}

func (own stubConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	resp, err := take(query, len(args), true)
	if err != nil {
		return nil, err
	}

	if resp.err != nil {
		return nil, resp.err
	}

	return stubResult{rowsAffected: resp.rowsAffected, lastInsertID: resp.lastInsertID}, nil
}

type stubStmt struct {
	query string
}

func (own *stubStmt) Close() error  { return nil }
func (own *stubStmt) NumInput() int { return -1 }

func (own *stubStmt) Exec(args []driver.Value) (driver.Result, error) {
	resp, err := take(own.query, len(args), true)
	if err != nil {
		return nil, err
	}

	if resp.err != nil {
		return nil, resp.err
	}

	return stubResult{rowsAffected: resp.rowsAffected, lastInsertID: resp.lastInsertID}, nil
}

func (own *stubStmt) Query(args []driver.Value) (driver.Rows, error) {
	resp, err := take(own.query, len(args), false)
	if err != nil {
		return nil, err
	}

	if resp.err != nil {
		return nil, resp.err
	}

	return &stubRows{columns: resp.columns, rows: resp.rows}, nil
}

func (own *stubStmt) CheckNamedValue(*driver.NamedValue) error { return nil }

type stubTx struct{}

func (stubTx) Commit() error   { return nil }
func (stubTx) Rollback() error { return nil }

type stubResult struct {
	rowsAffected int64
	lastInsertID int64
}

func (own stubResult) LastInsertId() (int64, error) { return own.lastInsertID, nil }
func (own stubResult) RowsAffected() (int64, error) { return own.rowsAffected, nil }

type stubRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (own *stubRows) Columns() []string { return own.columns }

func (own *stubRows) Close() error { return nil }

func (own *stubRows) Next(dest []driver.Value) error {
	if own.index >= len(own.rows) {
		return io.EOF
	}

	row := own.rows[own.index]
	own.index++

	if len(row) != len(own.columns) {
		return fmt.Errorf("%w: got %d values, want %d", ErrRowWidth, len(row), len(own.columns))
	}

	copy(dest, row)
	return nil
}
