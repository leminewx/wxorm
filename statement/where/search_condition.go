package where

import (
	"fmt"

	"gitee.com/leminewx/wxorm/dialect/clause"
)

// combine 把当前条件与其它条件按 AND/OR 组合
func combine(current Condition, operator Operator, conditions ...Condition) Condition {
	valid := GetValidConditions(conditions)
	switch len(valid) {
	case 0:
		return current
	case 1:
		return &listCondition{
			operator:   operator,
			conditions: append([]Condition{current}, valid[0]),
		}
	default:
		return &listCondition{
			operator: operator,
			conditions: append([]Condition{current}, &groupCondition{
				operator:   operator,
				conditions: valid,
			}),
		}
	}
}

/*
JSON_CONTAINS 判断 JSON 字段指定路径是否包含某个值

	MySQL:      JSON_CONTAINS(JSON_EXTRACT(`column`, '$.a.b'), value)
	PostgreSQL: ("column" #> '{a,b}') @> value::jsonb

路径统一使用 MySQL 风格（$.a.b），由方言自行转换；
方言未实现 clause.JSONQuerier 时返回 clause.ErrUnsupportedFeature。
*/
func JSON_CONTAINS(column, path string, value any) Condition {
	return &jsonCondition{
		column: column,
		path:   path,
		value:  value,
	}
}

type jsonCondition struct {
	column  string
	path    string
	value   any
	negated bool
}

var _ Condition = (*jsonCondition)(nil)

func (own *jsonCondition) AND(conditions ...Condition) Condition {
	return combine(own, _AND, conditions...)
}

func (own *jsonCondition) OR(conditions ...Condition) Condition {
	return combine(own, _OR, conditions...)
}

func (own *jsonCondition) NOT() Condition {
	return &jsonCondition{
		column:  own.column,
		path:    own.path,
		value:   own.value,
		negated: !own.negated,
	}
}

func (own *jsonCondition) Build(ctx *clause.Context, cla clause.Clause) error {
	if own.IsEmpty() {
		return nil
	}

	querier, ok := cla.(clause.JSONQuerier)
	if !ok {
		return fmt.Errorf("%w: JSON_CONTAINS", clause.ErrUnsupportedFeature)
	}

	if own.negated {
		_ = ctx.WriteString("NOT (")
	}

	if err := querier.BuildJSONContainsSQL(ctx, own.column, own.path, own.value); err != nil {
		ctx.Reset()
		return err
	}

	if own.negated {
		_ = ctx.WriteByte(')')
	}

	return nil
}

func (own *jsonCondition) Clone() Condition { return own.NOT().NOT() }

func (own *jsonCondition) IsEmpty() bool { return own.column == "" || own.path == "" }

func (own *jsonCondition) GetOperator() Operator { return _NONE }

func (own *jsonCondition) GetChildren() []Condition { return []Condition{own} }

/*
FULLTEXT 全文检索

	MySQL:      MATCH (`a`, `b`) AGAINST (query)
	PostgreSQL: to_tsvector('simple', coalesce("a",'') || ' ' || coalesce("b",'')) @@ plainto_tsquery(query)

mode 与 MySQL 保持一致：留空表示自然语言模式，传 "IN BOOLEAN MODE" 表示布尔模式；
PostgreSQL 把布尔模式映射为 websearch_to_tsquery，其余模式返回 clause.ErrUnsupportedFeature。
*/
func FULLTEXT(columns []string, query string, mode ...string) Condition {
	searchMode := ""
	if len(mode) > 0 {
		searchMode = mode[0]
	}

	return &fullTextCondition{
		columns: columns,
		query:   query,
		mode:    searchMode,
	}
}

type fullTextCondition struct {
	columns []string
	query   string
	mode    string
	negated bool
}

var _ Condition = (*fullTextCondition)(nil)

func (own *fullTextCondition) AND(conditions ...Condition) Condition {
	return combine(own, _AND, conditions...)
}

func (own *fullTextCondition) OR(conditions ...Condition) Condition {
	return combine(own, _OR, conditions...)
}

func (own *fullTextCondition) NOT() Condition {
	return &fullTextCondition{
		columns: own.columns,
		query:   own.query,
		mode:    own.mode,
		negated: !own.negated,
	}
}

func (own *fullTextCondition) Build(ctx *clause.Context, cla clause.Clause) error {
	if own.IsEmpty() {
		return nil
	}

	searcher, ok := cla.(clause.FullTextSearcher)
	if !ok {
		return fmt.Errorf("%w: FULLTEXT", clause.ErrUnsupportedFeature)
	}

	if own.negated {
		_ = ctx.WriteString("NOT (")
	}

	if err := searcher.BuildFullTextSearchSQL(ctx, own.columns, own.query, own.mode); err != nil {
		ctx.Reset()
		return err
	}

	if own.negated {
		_ = ctx.WriteByte(')')
	}

	return nil
}

func (own *fullTextCondition) Clone() Condition { return own.NOT().NOT() }

func (own *fullTextCondition) IsEmpty() bool { return len(own.columns) == 0 || own.query == "" }

func (own *fullTextCondition) GetOperator() Operator { return _NONE }

func (own *fullTextCondition) GetChildren() []Condition { return []Condition{own} }
