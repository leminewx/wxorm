package schema

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"gitee.com/leminewx/wxorm/dialect/column"
)

var (
	// ErrInvalidPreloadTag 表示预加载字段的标签格式不合法
	ErrInvalidPreloadTag = errors.New("wxorm: invalid preload tag, want preload(<column>)")
	// ErrPreloadFieldType 表示预加载字段的类型无法表达关联关系
	ErrPreloadFieldType = errors.New("wxorm: preload field must be struct, *struct, []struct or []*struct")
	// ErrPreloadRelationConflict 表示同一字段重复声明了预加载关系
	ErrPreloadRelationConflict = errors.New("wxorm: preload relation is declared more than once")
	// ErrPreloadUnknownField 表示 PRELOAD 指定了模型未声明的关联字段
	ErrPreloadUnknownField = errors.New("wxorm: preload field is not declared in model")
)

// RelationKind 描述预加载的关联方向
type RelationKind uint8

const (
	// RELATION_BELONGS_TO 表示本表列指向目标表主键（N:1）
	RELATION_BELONGS_TO RelationKind = iota + 1
	// RELATION_HAS_MANY 表示目标表列指向本表主键（1:N）
	RELATION_HAS_MANY
)

// String 返回关联方向的可读名称，便于错误信息与调式输出
func (own RelationKind) String() string {
	switch own {
	case RELATION_BELONGS_TO:
		return "belongs_to"
	case RELATION_HAS_MANY:
		return "has_many"
	default:
		return "unknown"
	}
}

// Relation 描述一个预加载关联
//
// 这类字段只参与 Preload 的子查询，不参与建表、INSERT 与 UPDATE，
// 也不出现在 SELECT 投影中（与 JoinColumn 的隔离方式一致）。
type Relation struct {
	FieldName   string       // 模型字段名，PRELOAD 的参数
	FieldIndex  int          // 模型字段索引
	Kind        RelationKind // 关联方向
	Column      string       // 关联列：belongs_to 是本表列，has_many 是目标表列
	TargetType  reflect.Type // 目标模型类型（已去掉指针与切片）
	TargetTable string       // 目标表逻辑表名
}

// parsePreloadTag 解析 preload(<column>) 标签
//
// 关联方向由字段类型决定：单值（T / *T）为本表列指向目标主键，
// 切片（[]T / []*T）为目标表列指向本表主键。
func parsePreloadTag(fieldName, tag string, fieldType reflect.Type) (Relation, bool, error) {
	inner, ok := strings.CutPrefix(tag, "preload(")
	if !ok {
		return Relation{}, false, nil
	}

	if !strings.HasSuffix(inner, ")") {
		return Relation{}, true, fmt.Errorf("%w: field %s", ErrInvalidPreloadTag, fieldName)
	}

	inner = strings.TrimSpace(strings.TrimSuffix(inner, ")"))
	if inner == "" || !isValidTagSegment(inner) {
		return Relation{}, true, fmt.Errorf("%w: field %s contains an invalid column", ErrInvalidPreloadTag, fieldName)
	}

	relation := Relation{FieldName: fieldName, Column: inner}
	switch fieldType.Kind() {
	case reflect.Struct:
		relation.Kind = RELATION_BELONGS_TO
		relation.TargetType = fieldType

	case reflect.Ptr:
		if fieldType.Elem().Kind() != reflect.Struct {
			return Relation{}, true, fmt.Errorf("%w: field %s", ErrPreloadFieldType, fieldName)
		}

		relation.Kind = RELATION_BELONGS_TO
		relation.TargetType = fieldType.Elem()

	case reflect.Slice:
		elem := fieldType.Elem()
		if elem.Kind() == reflect.Ptr {
			elem = elem.Elem()
		}

		if elem.Kind() != reflect.Struct {
			return Relation{}, true, fmt.Errorf("%w: field %s", ErrPreloadFieldType, fieldName)
		}

		relation.Kind = RELATION_HAS_MANY
		relation.TargetType = elem

	default:
		return Relation{}, true, fmt.Errorf("%w: field %s", ErrPreloadFieldType, fieldName)
	}

	relation.TargetTable = preloadTableName(relation.TargetType)
	return relation, true, nil
}

// preloadTableName 与 Parse 使用同一套表名规则：优先 GetTableName，否则蛇形名
func preloadTableName(modelType reflect.Type) string {
	if tab, ok := reflect.New(modelType).Interface().(TableNameGetter); ok {
		return tab.GetTableName()
	}

	return column.FormatCamelToSnake([]byte(modelType.Name()))
}

// validatePreloadRelations 校验重复关联并建立字段名索引
//
// 同一目标表 + 同一关联列 + 同一方向视为重复声明：会生成完全相同的子查询，
// 且两个字段得到同一份数据，属于模型错误。
func (own *Schema) validatePreloadRelations() error {
	seen := make(map[string]struct{}, len(own.Relations))
	for i, relation := range own.Relations {
		key := fmt.Sprintf("%d|%s|%s", relation.Kind, relation.TargetTable, relation.Column)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: field %s (%s %s.%s)", ErrPreloadRelationConflict, relation.FieldName, relation.Kind, relation.TargetTable, relation.Column)
		}

		seen[key] = struct{}{}
		own.RelationIndexesByName[relation.FieldName] = i
	}

	return nil
}

// GetRelationByName 按模型字段名查找预加载关联
func (own *Schema) GetRelationByName(fieldName string) (Relation, bool) {
	idx, ok := own.RelationIndexesByName[fieldName]
	if !ok {
		return Relation{}, false
	}

	return own.Relations[idx], true
}

/*
SelectRelations 返回本次需要预加载的关联

fields 为空表示模型声明的全部关联；否则按字段名过滤，未声明的字段返回
ErrPreloadUnknownField。返回顺序与声明顺序一致，便于生成稳定的 SQL 序列。
*/
func (own *Schema) SelectRelations(fields ...string) ([]Relation, error) {
	if len(fields) == 0 {
		return own.Relations, nil
	}

	relations := make([]Relation, 0, len(fields))
	for _, name := range fields {
		relation, ok := own.GetRelationByName(name)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrPreloadUnknownField, name)
		}

		relations = append(relations, relation)
	}

	return relations, nil
}
