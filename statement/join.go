package statement

import (
	"errors"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/statement/where"
)

var (
	// ErrJoinConditionRequired 表示非 CROSS JOIN 缺少 ON 条件
	ErrJoinConditionRequired = errors.New("wxorm: join requires an ON condition")
	// ErrJoinConditionNotAllowed 表示 CROSS JOIN 不允许携带 ON 条件
	ErrJoinConditionNotAllowed = errors.New("wxorm: cross join does not accept an ON condition")
)

// SelectColumn 描述联表查询的一个投影列
type SelectColumn struct {
	Table string // 表名或别名，空表示不限定
	Name  string // 列名
	Alias string // 结果列别名，空表示不取别名
}

// SELECT_COLUMNS 显式写出投影列
//
// 单表查询仍使用 SELECT 子句以复用方言缓存；联表查询的列集带有别名，
// 无法用「表名 + 列名列表」表达，因此单独提供本构造器。
func SELECT_COLUMNS(columns ...SelectColumn) (Name, ClauseBuildFunc) {
	return NAME_SELECT, func(ctx *clause.Context, cla clause.Clause) error {
		if len(columns) == 0 {
			return ErrColumnsIsEmpty
		}

		if err := ctx.WriteString("SELECT\n    "); err != nil {
			return err
		}

		for i, column := range columns {
			if column.Name == "" {
				return ErrColumnsIsEmpty
			}

			if i > 0 {
				if err := ctx.WriteString(", "); err != nil {
					return err
				}
			}

			name := column.Name
			if column.Table != "" {
				name = column.Table + "." + column.Name
			}

			if err := ctx.WriteString(cla.QuoteIdentifier(name)); err != nil {
				return err
			}

			if column.Alias == "" {
				continue
			}

			if err := ctx.WriteString(" AS "); err != nil {
				return err
			}
			if err := ctx.WriteString(cla.QuoteIdentifier(column.Alias)); err != nil {
				return err
			}
		}

		return nil
	}
}

// Join 描述一个 JOIN 子句
type Join struct {
	Kind  clause.JoinKind // JOIN 类型
	Table string          // 关联表（已是物理表名）
	Alias string          // 关联表别名，必须显式给出
	On    where.Condition // ON 条件，CROSS JOIN 为空
}

// JOIN 构造 JOIN 子句
//
// 单个构建函数内遍历全部 join：Statement 的子句以 Name 为键存放，
// 多次 AddClause(NAME_JOIN, ...) 会互相覆盖。
func JOIN(joins ...Join) (Name, ClauseBuildFunc) {
	return NAME_JOIN, func(ctx *clause.Context, cla clause.Clause) error {
		if len(joins) == 0 {
			return nil
		}

		joiner, ok := cla.(clause.Joiner)
		if !ok {
			return newUnsupportedFeatureError("JOIN")
		}

		for _, join := range joins {
			if join.Kind == clause.JOIN_CROSS {
				if join.On != nil {
					return ErrJoinConditionNotAllowed
				}
			} else if join.On == nil {
				return ErrJoinConditionRequired
			}

			if err := joiner.BuildJoinSQL(ctx, join.Kind, join.Table, join.Alias); err != nil {
				return err
			}

			if join.On == nil {
				continue
			}

			if err := ctx.WriteString("\nON\n    "); err != nil {
				return err
			}
			if err := join.On.Build(ctx, cla); err != nil {
				return err
			}
		}

		return nil
	}
}

// COUNT_DISTINCT 构造 COUNT(DISTINCT column) 子句
//
// 联表查询会放大行数，直接 COUNT(*) 得到的是连接后的行数，
// 统计主表实体必须先去重。
func COUNT_DISTINCT(column string) (Name, ClauseBuildFunc) {
	return NAME_SELECT, func(ctx *clause.Context, cla clause.Clause) error {
		if column == "" {
			return ErrColumnsIsEmpty
		}

		if err := ctx.WriteString("SELECT\n    COUNT(DISTINCT "); err != nil {
			return err
		}
		if err := ctx.WriteString(cla.QuoteIdentifier(column)); err != nil {
			return err
		}

		return ctx.WriteByte(')')
	}
}
