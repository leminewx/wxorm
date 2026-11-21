package session

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/schema"
	"gitee.com/leminewx/wxorm/statement"
)

var (
	// ErrJoinAliasNotJoined 表示模型声明了联表字段，但本次查询没有对应的 JOIN 别名
	ErrJoinAliasNotJoined = errors.New("wxorm: join alias is not joined in this query")
	// ErrJoinColumnNotNilable 表示外连接下联表字段必须可空
	ErrJoinColumnNotNilable = errors.New("wxorm: join column must be nullable for OUTER JOIN")
)

// buildJoinSelectColumns 生成联表查询的投影列
//
// 主表列统一用物理表名限定，避免关联表存在同名列时产生歧义；
// 联表列固定使用 <alias>_<column> 作为结果列别名，扫描阶段按该别名回填模型字段。
func buildJoinSelectColumns(own *Session, sch *schema.Schema, joins []statement.Join, requested []string) ([]statement.SelectColumn, error) {
	aliases := make(map[string]struct{}, len(joins))
	nullable := false
	for _, join := range joins {
		aliases[join.Alias] = struct{}{}

		switch join.Kind {
		case clause.JOIN_LEFT, clause.JOIN_RIGHT, clause.JOIN_FULL:
			nullable = true
		}
	}

	// 模型声明的联表字段必须都能在本次 JOIN 中找到对应别名，否则说明模型与查询不匹配
	for _, joinColumn := range sch.JoinColumns {
		if _, ok := aliases[joinColumn.Alias]; !ok {
			return nil, fmt.Errorf("%w: %q", ErrJoinAliasNotJoined, joinColumn.Alias)
		}
	}

	// 未指定列：投影主表全部列 + 模型声明的全部联表列
	if len(requested) == 0 {
		columns := make([]statement.SelectColumn, 0, len(sch.ColumnNames)+len(sch.JoinColumns))
		for _, name := range sch.ColumnNames {
			columns = append(columns, statement.SelectColumn{Table: own.table, Name: name})
		}

		for _, joinColumn := range sch.JoinColumns {
			if nullable {
				if err := requireNilableJoinColumn(sch, joinColumn); err != nil {
					return nil, err
				}
			}

			columns = append(columns, statement.SelectColumn{
				Table: joinColumn.Alias,
				Name:  joinColumn.Name,
				Alias: joinColumn.As,
			})
		}

		return columns, nil
	}

	// 指定列：名称可以是主表列名，也可以是联表字段的结果列别名
	columns := make([]statement.SelectColumn, 0, len(requested))
	for _, name := range requested {
		normalized := normalizeColumnName(name)

		if _, ok := sch.ColumnIndexesByName[normalized]; ok {
			columns = append(columns, statement.SelectColumn{Table: own.table, Name: normalized})
			continue
		}

		joinColumn, ok := sch.GetJoinColumnByName(normalized)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrSelectColumnNotFound, name)
		}

		if nullable {
			if err := requireNilableJoinColumn(sch, joinColumn); err != nil {
				return nil, err
			}
		}

		columns = append(columns, statement.SelectColumn{
			Table: joinColumn.Alias,
			Name:  joinColumn.Name,
			Alias: joinColumn.As,
		})
	}

	return columns, nil
}

// requireNilableJoinColumn 外连接下联表字段必须能承接 NULL
func requireNilableJoinColumn(sch *schema.Schema, joinColumn schema.JoinColumn) error {
	field := sch.ModelType.Field(joinColumn.Index)
	if isNilableType(field.Type) {
		return nil
	}

	return fmt.Errorf("%w: field %s (%s)", ErrJoinColumnNotNilable, field.Name, joinColumn.As)
}

// isNilableType 判断类型能否承接 NULL（指针、接口、容器或实现了 sql.Scanner）
func isNilableType(fieldType reflect.Type) bool {
	switch fieldType.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map:
		return true
	}

	scannerType := reflect.TypeOf((*sql.Scanner)(nil)).Elem()
	return reflect.PointerTo(fieldType).Implements(scannerType)
}
