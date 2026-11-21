/*
Package naming 定义数据表命名策略

命名策略把数据模型声明的「逻辑表名」解析为数据库中的「物理表名」：
  - Schema 是库（MySQL）或 Schema（PostgreSQL），SQLite 忽略；
  - Prefix / Suffix 追加在逻辑表名两侧，用于多租户、分库分表或环境隔离。

策略只影响表名，不参与 schema 缓存：同一模型可以在不同引擎上使用不同前缀，
缓存键仍是「方言 + 模型类型」，前缀在会话建立时计算。
*/
package naming

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// ErrInvalidSegment 表示命名策略的某一段包含会改变标识符结构的字符
var ErrInvalidSegment = errors.New("wxorm: invalid naming segment")

/*
Strategy 定义表名命名策略

零值表示不改变逻辑表名。Schema 为空时使用数据库的当前命名空间
（MySQL 的 DATABASE()、PostgreSQL 的 current_schema()）。
*/
type Strategy struct {
	Schema string // 库/Schema 名，可为空
	Prefix string // 表名前缀
	Suffix string // 表名后缀
}

// IsZero 判断策略是否为空（不改变逻辑表名）
func (own Strategy) IsZero() bool {
	return own.Schema == "" && own.Prefix == "" && own.Suffix == ""
}

// GetTableName 返回逻辑表名对应的物理表名（含前后缀，不含 Schema）
func (own Strategy) GetTableName(logical string) string {
	return own.Prefix + logical + own.Suffix
}

// Qualify 在物理表名前拼接 Schema，Schema 为空时原样返回
func (own Strategy) Qualify(table string) string {
	if own.Schema == "" || table == "" {
		return table
	}

	return own.Schema + "." + table
}

/*
Validate 校验各段是否可安全用于标识符

Prefix/Suffix/Schema 由开发者配置，但仍拒绝点号、引号、分隔符与空白：
点号会把前缀变成多段标识符，引号与分隔符则让物理表名脱离预期。
*/
func (own Strategy) Validate() error {
	segments := []struct {
		name  string
		value string
	}{
		{"schema", own.Schema},
		{"prefix", own.Prefix},
		{"suffix", own.Suffix},
	}

	for _, segment := range segments {
		if err := validateSegment(segment.name, segment.value); err != nil {
			return err
		}
	}

	return nil
}

// validateSegment 拒绝会改变标识符结构的字符
func validateSegment(name, value string) error {
	for _, r := range value {
		switch {
		case r == '.' || r == '"' || r == '`' || r == '\'' || r == '\\' || r == ';' || r == 0:
			return fmt.Errorf("%w: %s contains %q", ErrInvalidSegment, name, r)
		case unicode.IsSpace(r):
			return fmt.Errorf("%w: %s contains whitespace", ErrInvalidSegment, name)
		}
	}

	return nil
}

// String 返回便于日志输出的策略描述
func (own Strategy) String() string {
	if own.IsZero() {
		return "naming(none)"
	}

	parts := make([]string, 0, 3)
	if own.Schema != "" {
		parts = append(parts, "schema="+own.Schema)
	}
	if own.Prefix != "" {
		parts = append(parts, "prefix="+own.Prefix)
	}
	if own.Suffix != "" {
		parts = append(parts, "suffix="+own.Suffix)
	}

	return "naming(" + strings.Join(parts, ", ") + ")"
}
