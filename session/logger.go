package session

import (
	"context"
	"log"
	"time"
)

const (
	_FORMAT_DEBUG_WITH_COLOR_SQL              = "[DEBUG] [wxorm] [sid-%d] Execute SQL statement:\n----- SQL Statement -----\n\033[32m%s\033[0m\n\n"
	_FORMAT_DEBUG_WITHOUT_COLOR_SQL           = "[DEBUG] [wxorm] [sid-%d] Execute SQL statement:\n----- SQL Statement -----\n%s\n\n"
	_FORMAT_DEBUG_WITH_ARGS_WITH_COLOR_SQL    = "[DEBUG] [wxorm] [sid-%d] Execute SQL statement:\n----- SQL Statement -----\n\033[32m%s\033[0m\n----- SQL Parameters -----\n%v\n\n"
	_FORMAT_DEBUG_WITH_ARGS_WITHOUT_COLOR_SQL = "[DEBUG] [wxorm] [sid-%d] Execute SQL statement:\n----- SQL Statement -----\n%s\n----- SQL Parameters -----\n%v\n\n"
	_FORMAT_SLOW_SQL                          = "[SLOW] [wxorm] [sid-%d] Slow SQL statement (%s):\n----- SQL Statement -----\n%s\n\n"
	_FORMAT_SLOW_WITH_ARGS_SQL                = "[SLOW] [wxorm] [sid-%d] Slow SQL statement (%s):\n----- SQL Statement -----\n%s\n----- SQL Parameters -----\n%v\n\n"
	_FORMAT_SLOW_WITH_HIDDEN_ARGS_SQL         = "[SLOW] [wxorm] [sid-%d] Slow SQL statement (%s):\n----- SQL Statement -----\n%s\n----- SQL Parameters -----\n%d parameter(s) hidden, call SetLogParamValues(true) to expose\n\n"
)

// QueryEvent 描述一次已执行 SQL 的观测数据
//
// Elapsed 为「发起执行 → 返回结果」的耗时：Exec 为完整执行时间，
// Query 为拿到结果集的时间，不含调用方逐行 Scan 的时间。
type QueryEvent struct {
	SessionID uint64        // 会话ID
	Elapsed   time.Duration // 执行耗时
	Query     string        // SQL 语句
	Args      []any         // SQL 参数
	Debug     bool          // 是否处于调试模式
	Slow      bool          // 是否达到慢 SQL 阈值
	LogArgs   bool          // 是否允许默认日志器输出参数原值（慢 SQL 默认只输出参数个数）
	Color     bool          // 调试输出是否着色（仅默认日志器使用）
}

// Logger 定义 SQL 观测日志的输出接口
//
// 同一日志器会被多个会话共用，实现需保证并发安全；
// 默认实现把 Debug / Slow 事件写入标准库 log，保持与历史版本一致的格式。
type Logger interface {
	LogQuery(ctx context.Context, event QueryEvent)
}

// defaultLogger 使用标准库 log 输出 SQL 观测事件
type defaultLogger struct{}

// NewDefaultLogger 创建默认日志器
func NewDefaultLogger() Logger {
	return defaultLogger{}
}

// LogQuery 输出调试或慢 SQL 日志
func (own defaultLogger) LogQuery(_ context.Context, event QueryEvent) {
	switch {
	case event.Slow && !event.Debug:
		if len(event.Args) == 0 {
			log.Printf(_FORMAT_SLOW_SQL, event.SessionID, event.Elapsed, event.Query)
			return
		}

		// 慢 SQL 日志常被集中采集，参数原值里的手机号、身份证、密码会长期留存，
		// 默认只输出个数，需要定位具体数据时由调用方显式开启 SetLogParamValues(true)
		if !event.LogArgs {
			log.Printf(_FORMAT_SLOW_WITH_HIDDEN_ARGS_SQL, event.SessionID, event.Elapsed, event.Query, len(event.Args))
			return
		}

		log.Printf(_FORMAT_SLOW_WITH_ARGS_SQL, event.SessionID, event.Elapsed, event.Query, event.Args)
	case !event.Debug:
		// 既不是调试模式也未达到慢 SQL 阈值，无需输出
	default:
		own.logDebug(event)
	}
}

// logDebug 按历史格式输出调试信息
func (own defaultLogger) logDebug(event QueryEvent) {
	argsLen := len(event.Args)
	switch {
	case argsLen > 0 && event.Color:
		log.Printf(_FORMAT_DEBUG_WITH_ARGS_WITH_COLOR_SQL, event.SessionID, event.Query, event.Args)
	case argsLen > 0 && !event.Color:
		log.Printf(_FORMAT_DEBUG_WITH_ARGS_WITHOUT_COLOR_SQL, event.SessionID, event.Query, event.Args)
	case argsLen == 0 && event.Color:
		log.Printf(_FORMAT_DEBUG_WITH_COLOR_SQL, event.SessionID, event.Query)
	default:
		log.Printf(_FORMAT_DEBUG_WITHOUT_COLOR_SQL, event.SessionID, event.Query)
	}
}
