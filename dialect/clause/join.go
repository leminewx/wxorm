package clause

import "fmt"

// JoinKind 定义 JOIN 类型
type JoinKind string

const (
	JOIN_INNER JoinKind = "INNER"
	JOIN_LEFT  JoinKind = "LEFT"
	JOIN_RIGHT JoinKind = "RIGHT"
	JOIN_FULL  JoinKind = "FULL"
	JOIN_CROSS JoinKind = "CROSS"
)

// Keyword 返回 JOIN 关键字，未知类型返回空串
func (own JoinKind) Keyword() string {
	switch own {
	case JOIN_INNER, JOIN_LEFT, JOIN_RIGHT, JOIN_FULL, JOIN_CROSS:
		return string(own) + " JOIN"
	default:
		return ""
	}
}

// String 返回便于日志与错误信息展示的类型名
func (own JoinKind) String() string {
	if keyword := own.Keyword(); keyword != "" {
		return keyword
	}

	return string(own)
}

// CheckJoinKind 校验 JOIN 类型是否为已知类型
func CheckJoinKind(kind JoinKind) error {
	if kind.Keyword() == "" {
		return fmt.Errorf("%w: %q", ErrInvalidJoinKind, string(kind))
	}

	return nil
}

// BuildJoinClause 供各方言复用的 JOIN 子句写入逻辑
//
// 只写关键字、表名与别名，ON 条件交给 statement 层，
// 方言之间的差异集中在关键字支持范围上。
func BuildJoinClause(ctx *Context, cla Clause, kind JoinKind, table, alias string) error {
	if table == "" {
		return ErrJoinTableIsEmpty
	}
	if alias == "" {
		return ErrJoinAliasIsEmpty
	}

	keyword := kind.Keyword()
	if keyword == "" {
		return CheckJoinKind(kind)
	}

	if err := ctx.WriteString("\n" + keyword + "\n    "); err != nil {
		return err
	}

	if err := ctx.WriteString(cla.QuoteIdentifier(table)); err != nil {
		return err
	}

	if err := ctx.WriteString(" AS "); err != nil {
		return err
	}

	return ctx.WriteString(cla.QuoteIdentifier(alias))
}

// NewUnsupportedJoinError 把「方言不支持某种 JOIN」包成可被 errors.Is 识别的错误
func NewUnsupportedJoinError(kind JoinKind) error {
	return fmt.Errorf("%w: %s", ErrUnsupportedFeature, kind)
}
