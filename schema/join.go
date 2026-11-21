package schema

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

var (
	// ErrInvalidJoinTag 表示联表字段的标签格式不合法
	ErrInvalidJoinTag = errors.New("wxorm: invalid join tag, want join(<alias>.<column>)")
	// ErrJoinColumnConflict 表示联表字段的结果列别名与主表列或其它联表字段冲突
	ErrJoinColumnConflict = errors.New("wxorm: join column alias conflicts")
)

// JoinColumn 描述一个联表查询字段
//
// 这类字段只参与 SELECT 投影，不参与建表、INSERT 与 UPDATE。
type JoinColumn struct {
	Alias string // 联表别名，对应 JOIN 的别名
	Name  string // 关联表的列名
	As    string // 结果列别名，固定为 <alias>_<column>
	Index int    // 数据模型中的字段索引
}

// parseJoinTag 解析 join(<alias>.<column>) 标签
func parseJoinTag(fieldName, tag string) (JoinColumn, bool, error) {
	inner, ok := strings.CutPrefix(tag, "join(")
	if !ok {
		return JoinColumn{}, false, nil
	}

	if !strings.HasSuffix(inner, ")") {
		return JoinColumn{}, true, fmt.Errorf("%w: field %s", ErrInvalidJoinTag, fieldName)
	}

	inner = strings.TrimSpace(strings.TrimSuffix(inner, ")"))
	alias, column, found := strings.Cut(inner, ".")
	if !found || alias == "" || column == "" || strings.Contains(column, ".") {
		return JoinColumn{}, true, fmt.Errorf("%w: field %s", ErrInvalidJoinTag, fieldName)
	}

	if !isValidTagSegment(alias) || !isValidTagSegment(column) {
		return JoinColumn{}, true, fmt.Errorf("%w: field %s contains invalid characters", ErrInvalidJoinTag, fieldName)
	}

	return JoinColumn{
		Alias: alias,
		Name:  column,
		As:    alias + "_" + column,
	}, true, nil
}

// isValidTagSegment 判断段名是否可用作标识符的一部分
func isValidTagSegment(segment string) bool {
	return !strings.ContainsAny(segment, "\"'`\\;.") && strings.IndexFunc(segment, unicode.IsSpace) < 0
}

// validateJoinColumns 校验联表字段并建立结果列别名索引
func (own *Schema) validateJoinColumns() error {
	seen := make(map[string]struct{}, len(own.JoinColumns))
	for _, joinColumn := range own.JoinColumns {
		if _, ok := own.ColumnIndexesByName[joinColumn.As]; ok {
			return fmt.Errorf("%w: %q conflicts with a table column", ErrJoinColumnConflict, joinColumn.As)
		}

		if _, ok := seen[joinColumn.As]; ok {
			return fmt.Errorf("%w: %q is declared more than once", ErrJoinColumnConflict, joinColumn.As)
		}

		seen[joinColumn.As] = struct{}{}
		own.JoinColumnIndexesByName[joinColumn.As] = joinColumn.Index
	}

	return nil
}

// GetJoinColumnByName 按结果列别名查找联表字段
func (own *Schema) GetJoinColumnByName(name string) (JoinColumn, bool) {
	idx, ok := own.JoinColumnIndexesByName[name]
	if !ok {
		return JoinColumn{}, false
	}

	for _, joinColumn := range own.JoinColumns {
		if joinColumn.Index == idx {
			return joinColumn, true
		}
	}

	return JoinColumn{}, false
}
