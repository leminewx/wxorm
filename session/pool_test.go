package session

import (
	"context"
	"testing"
	"time"

	"gitee.com/leminewx/wxorm/harness/examples"
)

/*
TestSession_Reset 验证重置只清空会话级可变状态

数据库、方言与预编译缓存属于引擎级依赖，池化复用后必须保留，
否则每次取用都会丢失连接与缓存。
*/
func TestSession_Reset(t *testing.T) {
	sess := setupGroupSession(t)
	defer func() { _ = sess.db.Close() }()

	// 记录引擎级依赖，重置后应与原值一致
	db, dial, stmts := sess.db, sess.dialect, sess.stmts

	sess.SetDebug(true)
	sess.SetTimeout(time.Second)
	sess.SetSlowThreshold(time.Second)
	sess.SetContext(context.Background())
	sess.UseModel(&examples.Group{})
	sess.COLUMNS("id")
	sess.Reset()

	if sess.id != 0 || sess.debug || sess.debugColor {
		t.Fatalf("会话级标记未清空: id=%d debug=%v color=%v", sess.id, sess.debug, sess.debugColor)
	}

	if sess.timeout != 0 || sess.slowThreshold != 0 {
		t.Fatalf("超时配置未清空: timeout=%v slow=%v", sess.timeout, sess.slowThreshold)
	}

	if sess.ctx != nil || sess.model != nil || sess.schema != nil || len(sess.columns) != 0 {
		t.Fatalf("模型与上下文引用未释放: ctx=%v model=%v schema=%v columns=%d",
			sess.ctx, sess.model, sess.schema, len(sess.columns))
	}

	if got := sess.GetTableName(); got != "" {
		t.Fatalf("重置后表名应为空，实际 %q", got)
	}

	if got := sess.GetQualifiedTableName(); got != "" {
		t.Fatalf("重置后全限定表名应为空，实际 %q", got)
	}

	// 上下文回落为 background，避免复用后仍带着上一个请求的取消信号
	if sess.GetContext() == nil {
		t.Fatal("重置后上下文不应为 nil")
	}

	if sess.db != db || sess.dialect != dial || sess.stmts != stmts {
		t.Fatal("重置不应丢弃数据库、方言或预编译缓存")
	}
}

// TestSession_CloseReleases 验证 Close 会清空状态并归还，重复关闭无副作用
func TestSession_CloseReleases(t *testing.T) {
	sess := setupGroupSession(t)
	defer func() { _ = sess.db.Close() }()

	released := 0
	sess.SetReleaser(func(got *Session) {
		if got != sess {
			t.Errorf("归还的会话不是当前实例: %p != %p", got, sess)
		}

		released++
	})

	sess.SetDebug(true)
	sess.UseModel(&examples.Group{})

	if err := sess.Close(); err != nil {
		t.Fatalf("关闭会话失败: %v", err)
	}

	if released != 1 {
		t.Fatalf("Close 应归还一次，实际 %d 次", released)
	}

	// 归还前已重置，池中会话不应残留上一个使用者的模型与表名
	if sess.GetTableName() != "" || sess.model != nil || sess.tx != nil {
		t.Fatalf("归还会话未重置: table=%q model=%v tx=%v", sess.GetTableName(), sess.model, sess.tx)
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("重复关闭不应报错: %v", err)
	}

	if released != 1 {
		t.Fatalf("重复关闭不应再次归还，实际 %d 次", released)
	}
}
