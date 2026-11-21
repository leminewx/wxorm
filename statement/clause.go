package statement

import (
	"errors"
	"fmt"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/statement/where"
)

var (
	ErrColumnsIsEmpty   = errors.New("wxorm: columns is empty")
	ErrValuesIsEmpty    = errors.New("wxorm: values is empty")
	ErrConditionIsNil   = errors.New("wxorm: where condition is nil")
	ErrSetValuesIsEmpty = errors.New("wxorm: set values is empty")
	// ErrConflictTargetRequired 表示方言的 UPSERT 需要显式冲突目标（如 PostgreSQL 的 ON CONFLICT），
	// 但模型没有主键可供使用
	ErrConflictTargetRequired = errors.New("wxorm: upsert requires a conflict target, but the model has no primary key")
	// ErrLimitNegative 表示 LIMIT 传入了负数
	//
	// 负数无法表达行数上限，静默忽略会让调用方以为已经限制行数，因此直接报错。
	ErrLimitNegative = errors.New("wxorm: LIMIT must not be negative")
)

// newUnsupportedFeatureError 把“方言缺少某项能力”包成可被 errors.Is 识别的错误
func newUnsupportedFeatureError(feature string) error {
	return fmt.Errorf("%w: %s", clause.ErrUnsupportedFeature, feature)
}

// ClauseBuildFunc SQL 子句构建函数
type ClauseBuildFunc func(ctx *clause.Context, cla clause.Clause) error

// INSERT 子句
func INSERT(table string, ignore bool, columns ...string) (Name, ClauseBuildFunc) {
	return NAME_INSERT, func(ctx *clause.Context, cla clause.Clause) error {
		if table == "" {
			return dialect.ErrTableNameIsEmpty
		}
		if len(columns) == 0 {
			return ErrColumnsIsEmpty
		}

		return cla.BuildInsertSQL(ctx, table, ignore, columns...)
	}
}

// REPLACE 子句
//
// 只有实现 clause.Replacer 的方言（MySQL、SQLite）可用。
func REPLACE(table string, columns ...string) (Name, ClauseBuildFunc) {
	return NAME_REPLACE, func(ctx *clause.Context, cla clause.Clause) error {
		if table == "" {
			return dialect.ErrTableNameIsEmpty
		}
		if len(columns) == 0 {
			return ErrColumnsIsEmpty
		}

		replacer, ok := cla.(clause.Replacer)
		if !ok {
			return newUnsupportedFeatureError("REPLACE INTO")
		}

		return replacer.BuildReplaceSQL(ctx, table, columns...)
	}
}

// VALUES 子句
func VALUES(values ...[]any) (Name, ClauseBuildFunc) {
	return NAME_VALUES, func(ctx *clause.Context, cla clause.Clause) error {
		if len(values) == 0 {
			return ErrValuesIsEmpty
		}

		_, argStartIndex := ctx.GetSQLLengthAndArgNumber()
		return cla.BuildValuesSQL(ctx, argStartIndex, values...)
	}
}

// DUPLICATE 子句（仅 MySQL 系）
//
// 语义：columns 是「冲突时需要更新的列」，冲突目标由数据库根据唯一索引隐式确定。
// 该语义只在 MySQL 成立；PostgreSQL 的 ON CONFLICT DO UPDATE 必须显式指定冲突目标，
// 因此 PostgreSQL 下返回 ErrUnsupportedFeature，请改用 UPSERT。
func DUPLICATE(columns ...string) (Name, ClauseBuildFunc) {
	return NAME_DUPLICATE, func(ctx *clause.Context, cla clause.Clause) error {
		if len(columns) == 0 {
			return ErrColumnsIsEmpty
		}

		upserter, ok := cla.(clause.Upserter)
		if !ok {
			return newUnsupportedFeatureError("DUPLICATE (MySQL only), use UPSERT instead")
		}

		return upserter.BuildDuplicateSQL(ctx, columns...)
	}
}

// UPSERT 子句
//
// 冲突目标与更新列分开指定：conflict 通常是模型主键，update 是冲突时需要覆盖的列。
// 方言实现 clause.ConflictUpserter 时直接使用；否则退化为 DUPLICATE 的语义
// （适用于 MySQL：冲突目标由唯一索引自动确定）。
// conflict 为空且方言需要显式冲突目标时，返回 ErrConflictTargetRequired。
func UPSERT(conflict []string, update ...string) (Name, ClauseBuildFunc) {
	return NAME_DUPLICATE, func(ctx *clause.Context, cla clause.Clause) error {
		if len(update) == 0 {
			return ErrColumnsIsEmpty
		}

		if upserter, ok := cla.(clause.ConflictUpserter); ok {
			if len(conflict) == 0 {
				return ErrConflictTargetRequired
			}

			return upserter.BuildUpsertSQL(ctx, conflict, update)
		}

		upserter, ok := cla.(clause.Upserter)
		if !ok {
			return newUnsupportedFeatureError("UPSERT")
		}

		return upserter.BuildDuplicateSQL(ctx, update...)
	}
}

// INSERT_IGNORE 子句
//
// MySQL 把 IGNORE 内联在 INSERT 之后，无需额外子句；
// PostgreSQL 必须在 VALUES 之后追加 ON CONFLICT DO NOTHING，因此由本子句负责。
func INSERT_IGNORE() (Name, ClauseBuildFunc) {
	return NAME_IGNORE, func(ctx *clause.Context, cla clause.Clause) error {
		inserter, ok := cla.(clause.IgnoreInserter)
		if !ok {
			return nil
		}

		return inserter.BuildInsertIgnoreSQL(ctx)
	}
}

// RETURNING 子句
//
// 用于插入后回填数据库生成的值（PostgreSQL 的 INSERT ... RETURNING id）。
// 方言不支持时保持为空操作，由会话决定是否使用。
func RETURNING(columns ...string) (Name, ClauseBuildFunc) {
	return NAME_RETURNING, func(ctx *clause.Context, cla clause.Clause) error {
		if len(columns) == 0 {
			return ErrColumnsIsEmpty
		}

		// 单条与批量分开声明能力：SQLite 单条走 LastInsertId，只支持批量 RETURNING
		if returning, ok := cla.(clause.ReturningInserter); ok {
			return returning.BuildInsertReturningSQL(ctx, columns...)
		}

		if returning, ok := cla.(clause.BatchReturningInserter); ok {
			return returning.BuildInsertBatchReturningSQL(ctx, columns...)
		}

		return newUnsupportedFeatureError("INSERT ... RETURNING")
	}
}

// DELETE 子句
func DELETE(table string) (Name, ClauseBuildFunc) {
	return NAME_DELETE, func(ctx *clause.Context, cla clause.Clause) error {
		if table == "" {
			return dialect.ErrTableNameIsEmpty
		}

		return cla.BuildDeleteSQL(ctx, table)
	}
}

// WHERE 子句
func WHERE(condition where.Condition) (Name, ClauseBuildFunc) {
	return NAME_WHERE, func(ctx *clause.Context, cla clause.Clause) error {
		if condition == nil {
			return ErrConditionIsNil
		}

		if err := cla.BuildWhereSQL(ctx); err != nil {
			return err
		}

		return condition.Build(ctx, cla)
	}
}

// UPDATE 子句
func UPDATE(table string) (Name, ClauseBuildFunc) {
	return NAME_UPDATE, func(ctx *clause.Context, cla clause.Clause) error {
		if table == "" {
			return dialect.ErrTableNameIsEmpty
		}

		return cla.BuildUpdateSQL(ctx, table)
	}
}

// SET 子句
func SET(values map[string]any) (Name, ClauseBuildFunc) {
	return NAME_SET, func(ctx *clause.Context, cla clause.Clause) error {
		if len(values) == 0 {
			return ErrSetValuesIsEmpty
		}

		_, argStartIndex := ctx.GetSQLLengthAndArgNumber()
		return cla.BuildSetSQL(ctx, argStartIndex, values)
	}
}

// // RECURSIVE 子句
// func RECURSIVE(cteTable, initSelectSql, cteSelectSql string, args ...any) (Name, ClauseBuildFunc) {}
// 	return NAME_RECURSIVE, func(ctx *clause.Context, cla clause.Clause) error {}
// 		// 检查 SQL 语句是否生成失败
// 		if fail {
// 			return buffer, _args, false, true
// 		}

// 		// 检查 SQL 语句是否生成完成
// 		if over {
// 			return buffer, _args, true, false
// 		}

// 		if cteTable == "" || initSelectSql == "" || cteSelectSql == "" {
// 			return buffer, _args, false, true
// 		}

// 		// 写入 WITH RECURSIVE
// 		buffer.WriteString(SQL_RECURSIVE)
// 		// 写入临时表名
// 		buffer.WriteString(cteTable)
// 		// 写入 AS (
// 		buffer.WriteString(SQL_RECURSIVE_AS)
// 		// 写入初始查询语句
// 		buffer.WriteString(initSelectSql)
// 		// 写入 UNION ALLA
// 		buffer.WriteString(SQL_RECURSIVE_UNION_LINE_BREAK)
// 		// 写入递归查询语句
// 		buffer.WriteString(cteSelectSql)
// 		buffer.WriteString(SQL_LINE_BREAK_END)

// 		if len(args) > 0 {
// 			_args = append(_args, args...)
// 		}

// 		return nil
// 	{}
// {}

// SELECT 子句
func SELECT(table string, columns ...string) (Name, ClauseBuildFunc) {
	return NAME_SELECT, func(ctx *clause.Context, cla clause.Clause) error {
		return cla.BuildSelectSQL(ctx, table, columns...)
	}
}

// COUNT 子句
func COUNT(columns ...string) (Name, ClauseBuildFunc) {
	return NAME_SELECT, func(ctx *clause.Context, cla clause.Clause) error {
		return cla.BuildCountSQL(ctx, columns...)
	}
}

// EXISTS 子句（SELECT 1 FROM ...）用于判断是否存在满足条件的记录
func EXISTS(table string) (Name, ClauseBuildFunc) {
	return NAME_EXISTS, func(ctx *clause.Context, cla clause.Clause) error {
		if table == "" {
			return dialect.ErrTableNameIsEmpty
		}

		if err := ctx.WriteString("SELECT 1"); err != nil {
			return err
		}

		return cla.BuildFromSQL(ctx, table)
	}
}

// FROM 子句
func FROM(table string) (Name, ClauseBuildFunc) {
	return NAME_FROM, func(ctx *clause.Context, cla clause.Clause) error {
		if table == "" {
			return dialect.ErrTableNameIsEmpty
		}

		return cla.BuildFromSQL(ctx, table)
	}
}

// GROUP 子句
func GROUP(columns ...string) (Name, ClauseBuildFunc) {
	return NAME_GROUP, func(ctx *clause.Context, cla clause.Clause) error {
		if len(columns) == 0 {
			return nil
		}

		return cla.BuildGroupBySQL(ctx, columns...)
	}
}

// HAVING 子句
func HAVING(condition where.Condition) (Name, ClauseBuildFunc) {
	return NAME_HAVING, func(ctx *clause.Context, cla clause.Clause) error {
		if condition == nil {
			return ErrConditionIsNil
		}

		if err := cla.BuildHavingSQL(ctx); err != nil {
			return err
		}

		return condition.Build(ctx, cla)
	}
}

// ORDER 子句
func ORDER(columns ...string) (Name, ClauseBuildFunc) {
	return NAME_ORDER, func(ctx *clause.Context, cla clause.Clause) error {
		if len(columns) == 0 {
			return nil
		}

		return cla.BuildOrderBySQL(ctx, columns...)
	}
}

// LIMIT 子句
//
// 0 是有效值（SQL 的 LIMIT 0 表示不返回任何行），只有负数报错：
// 把 0 当成“未设置”会让分页参数算错时静默退化成全表查询。
func LIMIT(limit int) (Name, ClauseBuildFunc) {
	return NAME_LIMIT, func(ctx *clause.Context, cla clause.Clause) error {
		if limit < 0 {
			return ErrLimitNegative
		}

		return cla.BuildLimitSQL(ctx, limit)
	}
}

// OFFSET 子句
func OFFSET(offset int) (Name, ClauseBuildFunc) {
	return NAME_OFFSET, func(ctx *clause.Context, cla clause.Clause) error {
		if offset <= 0 {
			return nil
		}

		return cla.BuildOffsetSQL(ctx, offset)
	}
}
