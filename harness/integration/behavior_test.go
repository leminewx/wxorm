package integration

import (
	"errors"
	"testing"

	"gitee.com/leminewx/wxorm"
	"gitee.com/leminewx/wxorm/session"
	"gitee.com/leminewx/wxorm/statement/where"
)

// KV 使用非自增主键，用于验证跨方言 UPSERT 的冲突目标
type KV struct {
	K string `orm:"column(k) pk"`
	V string `orm:"column(v)"`
}

// GetTableName 返回键值表名
func (*KV) GetTableName() string {
	return tableName("kv")
}

// TestTransaction 验证事务提交、回滚与 panic 处理
func TestTransaction(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("account"), &Account{})

		// 提交：Transact 回调正常返回后数据可见
		if _, err := engine.Transact(func(sess *session.Session) (any, error) {
			sess.UseModel(&Account{})
			_, err := sess.INSERT(false,
				&Account{Name: "tx-commit-1", Email: "tx1@example.com"},
				&Account{Name: "tx-commit-2", Email: "tx2@example.com"},
			)
			return nil, err
		}); err != nil {
			t.Fatalf("事务提交失败: %v", err)
		}

		if got := mustCount(t, engine, &Account{}, nil); got != 2 {
			t.Fatalf("提交后行数不符: got %d, want 2", got)
		}

		// 回滚：回调返回错误时写入应被丢弃
		wantErr := errors.New("rollback on purpose")
		if _, err := engine.Transact(func(sess *session.Session) (any, error) {
			sess.UseModel(&Account{})
			if _, err := sess.INSERT(false, &Account{Name: "tx-rollback", Email: "tx3@example.com"}); err != nil {
				return nil, err
			}

			return nil, wantErr
		}); !errors.Is(err, wantErr) {
			t.Fatalf("事务回滚应返回原始错误, got %v", err)
		}

		if got := mustCount(t, engine, &Account{}, nil); got != 2 {
			t.Fatalf("回滚后行数不符: got %d, want 2", got)
		}

		// panic：应回滚并转换为错误，不向调用方抛出
		if _, err := engine.Transact(func(sess *session.Session) (any, error) {
			sess.UseModel(&Account{})
			if _, err := sess.INSERT(false, &Account{Name: "tx-panic", Email: "tx4@example.com"}); err != nil {
				return nil, err
			}

			panic("boom")
		}); err == nil {
			t.Fatal("panic 应转换为错误返回")
		}

		if got := mustCount(t, engine, &Account{}, nil); got != 2 {
			t.Fatalf("panic 回滚后行数不符: got %d, want 2", got)
		}

		// 显式 Begin/Rollback
		sess := engine.NewSession(&Account{})
		if err := sess.Begin(); err != nil {
			t.Fatalf("Begin 失败: %v", err)
		}

		if _, err := sess.INSERT(false, &Account{Name: "tx-explicit", Email: "tx5@example.com"}); err != nil {
			t.Fatalf("事务内插入失败: %v", err)
		}

		if err := sess.Rollback(); err != nil {
			t.Fatalf("Rollback 失败: %v", err)
		}

		if got := mustCount(t, engine, &Account{}, nil); got != 2 {
			t.Fatalf("显式回滚后行数不符: got %d, want 2", got)
		}
	})
}

// TestIgnore 验证冲突忽略在三方言下行为一致
func TestIgnore(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("account"), &Account{})

		sess := engine.NewSession(&Account{})
		if _, err := sess.INSERT(false, &Account{Name: "alice", Email: "alice@example.com"}); err != nil {
			t.Fatalf("首次插入失败: %v", err)
		}

		// 唯一键冲突：ignore=true 时不应报错
		res, err := sess.INSERT(true, &Account{Name: "alice-dup", Email: "alice@example.com"})
		if err != nil {
			t.Fatalf("冲突忽略插入失败: %v", err)
		}

		if rows, err := res.RowsAffected(); err != nil {
			t.Fatalf("读取影响行数失败: %v", err)
		} else if rows != 0 {
			t.Fatalf("冲突忽略应影响 0 行, got %d", rows)
		}

		if got := mustCount(t, engine, &Account{}, nil); got != 1 {
			t.Fatalf("冲突忽略后行数不符: got %d, want 1", got)
		}

		// 多条插入同样需要忽略冲突
		if _, err := sess.INSERT(true,
			&Account{Name: "alice-dup2", Email: "alice@example.com"},
			&Account{Name: "bob", Email: "bob@example.com"},
		); err != nil {
			t.Fatalf("多条忽略插入失败: %v", err)
		}

		if got := mustCount(t, engine, &Account{}, nil); got != 2 {
			t.Fatalf("多条忽略插入后行数不符: got %d, want 2", got)
		}
	})
}

// TestUpsert 验证跨方言 UPSERT 语义一致
func TestUpsert(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("kv"), &KV{})

		sess := engine.NewSession(&KV{})
		if _, err := sess.UPSERT("v").INSERT(false, &KV{K: "a", V: "1"}); err != nil {
			t.Fatalf("首次 UPSERT 失败: %v", err)
		}

		if _, err := sess.UPSERT("v").INSERT(false, &KV{K: "a", V: "2"}); err != nil {
			t.Fatalf("冲突 UPSERT 失败: %v", err)
		}

		var rows []KV
		if _, err := sess.ORDER("k").SELECT(&rows); err != nil {
			t.Fatalf("查询 UPSERT 结果失败: %v", err)
		}

		if len(rows) != 1 || rows[0].V != "2" {
			t.Fatalf("UPSERT 结果不符: %+v", rows)
		}
	})
}

// TestDuplicate 验证 MySQL 的 DUPLICATE 简写，其他方言应明确拒绝
func TestDuplicate(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("account"), &Account{})

		sess := engine.NewSession(&Account{})
		if _, err := sess.INSERT(false, &Account{Name: "alice", Email: "alice@example.com", Age: 1}); err != nil {
			t.Fatalf("首次插入失败: %v", err)
		}

		_, err := sess.DUPLICATE("name", "age").INSERT(false, &Account{Name: "alice2", Email: "alice@example.com", Age: 2})
		if tc.Driver == "mysql" {
			if err != nil {
				t.Fatalf("MySQL DUPLICATE 失败: %v", err)
			}

			var rows []Account
			if _, err := sess.SELECT(&rows); err != nil {
				t.Fatalf("查询失败: %v", err)
			}

			if len(rows) != 1 || rows[0].Name != "alice2" || rows[0].Age != 2 {
				t.Fatalf("DUPLICATE 更新结果不符: %+v", rows)
			}

			return
		}

		// PostgreSQL / SQLite 必须显式冲突目标，DUPLICATE 应返回不支持
		if err == nil {
			t.Fatalf("方言 %s 不应支持 DUPLICATE", tc.Driver)
		}
	})
}

// TestHooks 验证 8 个钩子的触发时机与次数
func TestHooks(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("hooked"), &Hooked{})

		row := &Hooked{Name: "h1"}
		sess := engine.NewSession(&Hooked{})
		if _, err := sess.INSERT(false, row); err != nil {
			t.Fatalf("插入失败: %v", err)
		}

		if row.BeforeInsert != 1 || row.AfterInsert != 1 {
			t.Fatalf("插入钩子次数不符: %+v", row)
		}

		if _, err := sess.INSERT(false,
			&Hooked{Name: "h2"},
			&Hooked{Name: "h3"},
		); err != nil {
			t.Fatalf("批量插入失败: %v", err)
		}

		var rows []*Hooked
		if _, err := sess.ORDER("id").SELECT(&rows); err != nil {
			t.Fatalf("查询失败: %v", err)
		}

		if len(rows) != 3 {
			t.Fatalf("查询行数不符: %d", len(rows))
		}

		// 每行查询都应触发后置钩子
		for i, item := range rows {
			if item.AfterSelect != 1 {
				t.Fatalf("第 %d 行 AfterSelect 次数不符: %+v", i, item)
			}
		}

		// 结构体更新与删除
		update := *rows[0]
		update.Name = "h1-renamed"
		if _, err := sess.UPDATE(&update); err != nil {
			t.Fatalf("更新失败: %v", err)
		}

		if update.BeforeUpdate != 1 || update.AfterUpdate != 1 {
			t.Fatalf("更新钩子次数不符: %+v", update)
		}

		if _, err := sess.DELETE(rows[2]); err != nil {
			t.Fatalf("删除失败: %v", err)
		}

		if rows[2].BeforeDelete != 1 || rows[2].AfterDelete != 1 {
			t.Fatalf("删除钩子次数不符: %+v", rows[2])
		}

		// 未被修改的行不应触发更新钩子
		if rows[1].BeforeUpdate != 0 || rows[1].AfterUpdate != 0 {
			t.Fatalf("未更新的行不应触发更新钩子: %+v", rows[1])
		}

		if got := mustCount(t, engine, &Hooked{}, nil); got != 2 {
			t.Fatalf("删除后行数不符: got %d, want 2", got)
		}

		// FindFirst 也应触发查询钩子
		one := &Hooked{}
		if _, err := sess.WHERE(where.EQ("name", "h2")).FindFirst(one); err != nil {
			t.Fatalf("FindFirst 失败: %v", err)
		}

		if one.AfterSelect != 1 {
			t.Fatalf("FindFirst 后置钩子次数不符: %+v", one)
		}
	})
}
