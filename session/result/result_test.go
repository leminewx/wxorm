package result

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

// stubResult 实现 sql.Result，用于验证结果包装的委托行为
type stubResult struct {
	lastID   int64
	affected int64
}

func (own *stubResult) LastInsertId() (int64, error) { return own.lastID, nil }

func (own *stubResult) RowsAffected() (int64, error) { return own.affected, nil }

var _ sql.Result = (*stubResult)(nil)

// 测试 NewResult 的委托与参数拷贝
func TestNewResult(t *testing.T) {
	args := []any{1, "name"}
	res := NewResult(&stubResult{lastID: 7, affected: 1}, 3*time.Millisecond, "INSERT INTO t", args...)

	if id, err := res.LastInsertId(); err != nil || id != 7 {
		t.Errorf("expected last insert id 7, got %d, err: %v", id, err)
	}

	if rows, err := res.RowsAffected(); err != nil || rows != 1 {
		t.Errorf("expected 1 row affected, got %d, err: %v", rows, err)
	}

	if got := res.GetExecutionSQL(); got != "INSERT INTO t" {
		t.Errorf("unexpected sql: %s", got)
	}

	if got := res.GetExecutionCost(); got != 3*time.Millisecond {
		t.Errorf("unexpected cost: %v", got)
	}

	// 修改调用方传入的切片，不应影响已保存的参数
	args[0] = 100
	got := res.GetExecutionArgs()
	if len(got) != 2 || got[0] != 1 || got[1] != "name" {
		t.Errorf("execution args should be copied, got %v", got)
	}
}

// 测试 SelectResult 的零值语义
func TestNewSelectResult(t *testing.T) {
	res := NewSelectResult(time.Millisecond, "SELECT 1")

	if id, err := res.LastInsertId(); err != nil || id != 0 {
		t.Errorf("select result should not have last insert id, got %d, err: %v", id, err)
	}

	if rows, err := res.RowsAffected(); err != nil || rows != 0 {
		t.Errorf("select result should not have affected rows, got %d, err: %v", rows, err)
	}

	if got := res.GetExecutionSQL(); got != "SELECT 1" {
		t.Errorf("unexpected sql: %s", got)
	}

	if got := res.GetExecutionArgs(); got != nil {
		t.Errorf("expected nil args, got %v", got)
	}
}

// 测试批量结果的行数与自增 ID 汇总
func TestNewBatchResult(t *testing.T) {
	args := []any{"a"}
	res := NewBatchResult(250, 128, time.Second, "", args...)

	if rows, err := res.RowsAffected(); err != nil || rows != 250 {
		t.Errorf("expected 250 rows, got %d, err: %v", rows, err)
	}

	if id, err := res.LastInsertId(); err != nil || id != 128 {
		t.Errorf("expected last insert id 128, got %d, err: %v", id, err)
	}

	args[0] = "b"
	if got := res.GetExecutionArgs(); len(got) != 1 || got[0] != "a" {
		t.Errorf("execution args should be copied, got %v", got)
	}
}

// 测试错误路径的结果对象不 panic
//
// 执行失败时会话返回 nil 结果，若调用方仍访问 LastInsertId / RowsAffected，
// 必须拿到明确错误而不是空指针 panic。
func TestResult_NilUnderlying(t *testing.T) {
	res := NewResult(nil, time.Millisecond, "DELETE FROM missing")

	if _, err := res.LastInsertId(); !errors.Is(err, ErrNoExecutionResult) {
		t.Errorf("LastInsertId should report ErrNoExecutionResult, got %v", err)
	}

	if _, err := res.RowsAffected(); !errors.Is(err, ErrNoExecutionResult) {
		t.Errorf("RowsAffected should report ErrNoExecutionResult, got %v", err)
	}

	if got := res.GetExecutionSQL(); got != "DELETE FROM missing" {
		t.Errorf("unexpected sql: %s", got)
	}
}

// 测试空参数不产生额外分配
func TestCopyArgs_Empty(t *testing.T) {
	if got := copyArgs(nil); got != nil {
		t.Errorf("expected nil for empty args, got %v", got)
	}

	if got := copyArgs([]any{}); got != nil {
		t.Errorf("expected nil for zero-length args, got %v", got)
	}
}
