package session

import (
	"context"
	"database/sql"
	"errors"
	"log"
)

var (
	// ErrTransactionNotStartedYet 表示提交或回滚时没有进行中的事务
	ErrTransactionNotStartedYet = errors.New("wxorm: transaction is not started yet")

	// ErrTransactionAlreadyStarted 表示已有未结束的事务，不能重复开启
	//
	// 直接覆盖 tx 会让原事务失去提交/回滚入口，连接被长期占用直到进程退出。
	ErrTransactionAlreadyStarted = errors.New("wxorm: transaction already started")
)

var (
	_DEBUG_TRANSACTION_BEGIN    = "[DEBUG] [wxorm] [sid-%d] Begin transaction."
	_DEBUG_TRANSACTION_COMMIT   = "[DEBUG] [wxorm] [sid-%d] Commit transaction."
	_DEBUG_TRANSACTION_ROLLBACK = "[DEBUG] [wxorm] [sid-%d] Rollback transaction."
)

// Begin 开始事务
//
// 使用会话上下文；需要指定超时或隔离级别时用 BeginTx。
// 已有未结束的事务时返回 ErrTransactionAlreadyStarted。
func (own *Session) Begin() error {
	return own.BeginTx(own.GetContext(), nil)
}

// BeginTx 使用给定上下文与隔离级别开始事务
//
// ctx 为 nil 时回退到会话上下文；opts 为 nil 时使用驱动默认隔离级别。
// 事务进行中每条 SQL 都受该上下文约束，请求取消或超时会由驱动回滚。
func (own *Session) BeginTx(ctx context.Context, opts *sql.TxOptions) (err error) {
	if own.tx != nil {
		return ErrTransactionAlreadyStarted
	}

	if ctx == nil {
		ctx = own.GetContext()
	}

	if own.debug {
		log.Printf(_DEBUG_TRANSACTION_BEGIN, own.id)
	}

	if own.tx, err = own.db.BeginTx(ctx, opts); err != nil {
		own.tx = nil
	}

	return err
}

// Commit 提交事务
func (own *Session) Commit() (err error) {
	if own.tx == nil {
		return ErrTransactionNotStartedYet
	}

	if own.debug {
		log.Printf(_DEBUG_TRANSACTION_COMMIT, own.id)
	}

	if err = own.tx.Commit(); err == nil {
		own.tx = nil
	}

	return
}

// Rollback 回滚事务
func (own *Session) Rollback() (err error) {
	if own.tx == nil {
		return ErrTransactionNotStartedYet
	}

	if own.debug {
		log.Printf(_DEBUG_TRANSACTION_ROLLBACK, own.id)
	}

	if err = own.tx.Rollback(); err == nil {
		own.tx = nil
	}

	return
}
