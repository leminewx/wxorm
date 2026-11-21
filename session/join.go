package session

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/statement"
	"gitee.com/leminewx/wxorm/statement/where"
)

var (
	// ErrJoinUnsupported 表示写语句不支持 JOIN
	ErrJoinUnsupported = errors.New("wxorm: JOIN is only supported by SELECT, FindFirst, COUNT and Exist")
	// ErrJoinTableRequired 表示 JOIN 没有指定关联表
	ErrJoinTableRequired = errors.New("wxorm: join table is required")
	// ErrJoinAliasRequired 表示 JOIN 没有指定别名
	ErrJoinAliasRequired = errors.New(`wxorm: join alias is required, eg: LEFT_JOIN("authors", "author", on)`)
	// ErrJoinAliasInvalid 表示 JOIN 别名含有会改变 SQL 结构的字符
	ErrJoinAliasInvalid = errors.New("wxorm: join alias contains invalid characters")
	// ErrJoinTableInvalid 表示 JOIN 表名含有会改变 SQL 结构的字符
	ErrJoinTableInvalid = errors.New("wxorm: join table contains invalid characters")
	// ErrJoinRequiresSinglePK 表示联表统计要求主表只有单列主键
	ErrJoinRequiresSinglePK = errors.New("wxorm: JOIN + COUNT requires a single-column primary key")
	// ErrJoinCountColumnUnsupported 表示联表统计不接受列名参数
	ErrJoinCountColumnUnsupported = errors.New("wxorm: JOIN + COUNT does not accept a column list")
)

/*
JOIN 添加一个联表子句

- kind: JOIN 类型（INNER/LEFT/RIGHT/FULL/CROSS）
- table: 关联表（逻辑表名，按会话的命名策略解析为物理表名）
- alias: 关联表别名，必须显式给出；主表用物理表名限定，所以别名不能与主表同名
- on: ON 条件，CROSS JOIN 必须为空，其余类型必须提供

只对 SELECT / FindFirst / COUNT / Exist 生效，写语句会返回 ErrJoinUnsupported。
*/
func (own *Session) JOIN(kind clause.JoinKind, table, alias string, on where.Condition) *Session {
	if own.joinErr != nil {
		return own
	}

	if own.namingErr != nil {
		own.joinErr = own.namingErr
		return own
	}

	if err := validateJoinTarget(kind, table, alias, on); err != nil {
		own.joinErr = err
		return own
	}

	// 关联表与主表共用命名策略：前缀、后缀与 Schema 都要生效，别名不参与改写
	physical := own.naming.GetTableName(table)
	own.joins = append(own.joins, statement.Join{
		Kind:  kind,
		Table: own.naming.Qualify(physical),
		Alias: alias,
		On:    on,
	})

	return own
}

// INNER_JOIN 添加内连接
func (own *Session) INNER_JOIN(table, alias string, on where.Condition) *Session {
	return own.JOIN(clause.JOIN_INNER, table, alias, on)
}

// LEFT_JOIN 添加左外连接
func (own *Session) LEFT_JOIN(table, alias string, on where.Condition) *Session {
	return own.JOIN(clause.JOIN_LEFT, table, alias, on)
}

// takeJoins 取出本次查询的 JOIN 列表并清空会话状态
//
// 与 columns/limit 一样「取走即归零」：查询结束后不会影响下一次调用，
// 也不会泄漏到写语句。
func (own *Session) takeJoins() ([]statement.Join, error) {
	joins, err := own.joins, own.joinErr
	own.joins, own.joinErr = nil, nil
	return joins, err
}

// resetJoins 清空 JOIN 状态
func (own *Session) resetJoins() {
	own.joins = nil
	own.joinErr = nil
}

// ensureNoJoin 写语句的守卫：存在 JOIN 时拒绝执行
func (own *Session) ensureNoJoin() error {
	if own.joinErr != nil {
		err := own.joinErr
		own.resetJoins()
		return err
	}

	if len(own.joins) > 0 {
		own.resetJoins()
		return ErrJoinUnsupported
	}

	return nil
}

// validateJoinTarget 校验 JOIN 的类型、表名、别名与 ON 条件组合
func validateJoinTarget(kind clause.JoinKind, table, alias string, on where.Condition) error {
	if err := clause.CheckJoinKind(kind); err != nil {
		return err
	}

	if table == "" {
		return ErrJoinTableRequired
	}

	if hasInvalidSegment(table) {
		return fmt.Errorf("%w: %q", ErrJoinTableInvalid, table)
	}

	if err := validateJoinAlias(alias); err != nil {
		return err
	}

	if kind == clause.JOIN_CROSS {
		if on != nil {
			return statement.ErrJoinConditionNotAllowed
		}
		return nil
	}

	if on == nil {
		return statement.ErrJoinConditionRequired
	}

	return nil
}

// validateJoinAlias 校验别名可用于标识符
func validateJoinAlias(alias string) error {
	if alias == "" {
		return ErrJoinAliasRequired
	}

	if strings.Contains(alias, ".") || hasInvalidSegment(alias) {
		return fmt.Errorf("%w: %q", ErrJoinAliasInvalid, alias)
	}

	return nil
}

// hasInvalidSegment 判断标识符是否含有会改变 SQL 结构的字符
func hasInvalidSegment(name string) bool {
	return strings.ContainsAny(name, "\"'`\\;") || strings.IndexFunc(name, unicode.IsSpace) >= 0 ||
		strings.IndexFunc(name, unicode.IsControl) >= 0
}
