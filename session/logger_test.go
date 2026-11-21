package session

import (
	"context"
	"sync"
	"testing"
	"time"
)

// recordingLogger 记录 SQL 观测事件，用于断言日志注入与慢 SQL 判定
type recordingLogger struct {
	mu     sync.Mutex
	events []QueryEvent
}

// LogQuery 记录事件（实现 Logger 接口）
func (own *recordingLogger) LogQuery(_ context.Context, event QueryEvent) {
	own.mu.Lock()
	own.events = append(own.events, event)
	own.mu.Unlock()
}

// getEvents 返回已记录事件的副本
func (own *recordingLogger) getEvents() []QueryEvent {
	own.mu.Lock()
	defer own.mu.Unlock()

	events := make([]QueryEvent, len(own.events))
	copy(events, own.events)
	return events
}

// TestSessionLogger_Debug 验证调试事件交给注入的日志器
func TestSessionLogger_Debug(t *testing.T) {
	sess := setupGroupSession(t)

	logger := &recordingLogger{}
	sess.SetLogger(logger)
	sess.SetDebug(false)

	if _, _, err := sess.COUNT(); err != nil {
		t.Fatalf("count failed: %v", err)
	}

	events := logger.getEvents()
	if len(events) != 1 {
		t.Fatalf("应收到 1 条日志事件，实际 %d", len(events))
	}

	if !events[0].Debug || events[0].Slow {
		t.Fatalf("事件应为调试而非慢 SQL，实际: %+v", events[0])
	}

	if events[0].Query == "" || events[0].SessionID == 0 {
		t.Fatalf("事件缺少 SQL 或会话ID: %+v", events[0])
	}
}

// TestSessionLogger_Slow 验证慢 SQL 判定：非调试模式下只上报达到阈值的语句
//
// 内存驱动执行耗时接近 0，无法在真实执行路径上稳定跨过阈值，
// 此处直接向 logQuery 传入耗时，覆盖判定逻辑与上报路径；
// 最后再用真实 COUNT 确认「未开调试且未达阈值」时不产生事件。
func TestSessionLogger_Slow(t *testing.T) {
	sess := setupGroupSession(t)

	logger := &recordingLogger{}
	sess.SetLogger(logger).SetSlowThreshold(time.Second)

	sess.logQuery(time.Second, "SELECT * FROM groups WHERE id = ?", 1)

	events := logger.getEvents()
	if len(events) != 1 {
		t.Fatalf("应收到 1 条慢 SQL 事件，实际 %d", len(events))
	}

	if !events[0].Slow || events[0].Debug {
		t.Fatalf("事件应为慢 SQL 而非调试，实际: %+v", events[0])
	}

	// 未达阈值不上报
	sess.logQuery(time.Second-time.Nanosecond, "SELECT 1")
	if events := logger.getEvents(); len(events) != 1 {
		t.Fatalf("未达阈值时不应上报，实际 %d 条", len(events))
	}

	// 阈值归零表示关闭检测
	sess.SetSlowThreshold(0)
	sess.logQuery(time.Hour, "SELECT 1")
	if events := logger.getEvents(); len(events) != 1 {
		t.Fatalf("关闭检测后不应上报，实际 %d 条", len(events))
	}

	// 真实执行路径：未开调试且未达阈值，不产生事件
	logger = &recordingLogger{}
	sess.SetLogger(logger)
	if _, _, err := sess.COUNT(); err != nil {
		t.Fatalf("count failed: %v", err)
	}

	if events := logger.getEvents(); len(events) != 0 {
		t.Fatalf("未开调试且未达阈值时不应上报，实际 %d 条", len(events))
	}
}

// TestSessionLogger_Default 验证 nil 日志器恢复默认实现
func TestSessionLogger_Default(t *testing.T) {
	sess := setupGroupSession(t)

	sess.SetLogger(nil)
	if sess.logger == nil {
		t.Fatal("SetLogger(nil) 应恢复默认日志器")
	}
}
