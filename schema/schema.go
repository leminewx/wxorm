package schema

import (
	"errors"
	"fmt"
	"go/ast"
	"reflect"
	"strings"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/dialect/column"
)

const (
	// _TAG1 是最早的标签键，保留兼容。
	_TAG1 = "orm"
	// _TAG2 是当前标签键，优先级最高。
	_TAG2 = "wxorm"
	// _TAG_LEGACY 是改名前的标签键，不再解析，仅用于检测未迁移的模型。
	_TAG_LEGACY = "worm"
)

var (
	ErrValueNil         = errors.New("wxorm: value is nil")
	ErrValueNotStruct   = errors.New("wxorm: value is not Struct or *Struct")
	ErrModelNotStruct   = errors.New("wxorm: model is not Struct or *Struct")
	ErrModelNilPointer  = errors.New("wxorm: model pointer is nil")
	ErrModelEmptyFields = errors.New("wxorm: model struct has no available field")
	ErrValuesEmpty      = errors.New("wxorm: values is empty")

	// ErrUnknownUpdateColumn 表示 UPDATE_COLUMNS 指定了不存在或不可更新的列
	ErrUnknownUpdateColumn = errors.New("wxorm: unknown update column")

	// ErrLegacyTagRemoved 模型仍使用改名前的标签键
	ErrLegacyTagRemoved = errors.New("wxorm: legacy tag key is no longer supported, rename it")
)

type TableNameGetter interface {
	GetTableName() string
}

// Schema 定义数据表的数据结构
//
// TableName 是模型声明的逻辑表名：前缀、后缀与库名由命名策略在会话层应用，
// 不写入本结构，否则同一模型在不同引擎下会共用被冻结的表名。
type Schema struct {
	TableName                 string                 // 数据表逻辑表名
	ModelName                 string                 // 数据模型名称
	ModelType                 reflect.Type           // 数据模型类型
	Fields                    []*reflect.StructField // 数据模型字段
	Columns                   []column.Column        // 数据表字段列表
	ColumnNames               []string               // 数据表字段名列表
	ColumnIndexesInModel      []int                  // 数据表字段索引列表（数据表字段在对应的数据模型中的索引）
	ColumnIndexesByName       map[string]int         // 数据表字段名到数据模型字段索引的映射
	JoinColumns               []JoinColumn           // 联表查询字段（不参与建表与读写）
	JoinColumnIndexesByName   map[string]int         // 联表结果列别名到数据模型字段索引的映射
	Relations                 []Relation             // 预加载关联（不参与建表、读写与 SELECT 投影）
	RelationIndexesByName     map[string]int         // 关联字段名到 Relations 下标的映射
	InsertableColumnNames     []string               // 数据表可插入字段名列表
	InsertableFieldIndexes    []int                  // 数据模型可插入字段索引列表
	InsertColumnsKey          string                 // 可插入列集的缓存键片段（不含表名）
	AutoPrimaryKeyName        string                 // 自增主键字段名（PostgreSQL 的 INSERT ... RETURNING 依赖它）
	PrimaryKeyNames           []string               // 主键字段名列表（PostgreSQL 的 ON CONFLICT 冲突目标依赖它）
	PrimaryKeyFields          []FieldIndex           // 主键列与模型字段的对应关系（按列顺序，WHERE 构建依赖它保证 SQL 稳定）
	UpdateFields              []FieldIndex           // 可更新列与模型字段的对应关系（非主键、非自动插入）
	UpdateColumnIndexesByName map[string]int         // 可更新列名 → UpdateFields 下标（UPDATE_COLUMNS 白名单使用）
	BigFieldKinds             []BigKind              // 模型字段下标 → 大数桥接种类（长度等于模型的字段数）
	HasBigFields              bool                   // 模型是否含需要在文本与大数之间桥接的字段
}

// FieldIndex 描述数据库列与数据模型字段的对应关系
type FieldIndex struct {
	Name  string // 列名
	Index int    // 数据模型中的字段索引
}

// FieldValue 描述列名与模型字段值的配对
type FieldValue struct {
	Name  string // 列名
	Value any    // 模型字段值
}

/*
Parse 解析 Go 结构体数据模型与数据表的对应关系
  - dial: 对应数据库的 dialect.Dialect
  - model: 数据表对应的数据模型，推荐使用 *Struct 格式
*/
func Parse(dial dialect.Dialect, model any) (*Schema, error) {
	// 获取数据模型
	modelValue, err := GetModelStruct(model)
	if err != nil {
		return nil, err
	}
	modelType := modelValue.Type()

	// 获取数据表名
	//
	// GetTableName 可能依赖实例状态（分表、多租户），因此表名必须参与缓存键：
	// 先算表名再查缓存，避免首个实例把表名冻结在同一类型的其它实例上。
	var tableName string
	if tab, ok := model.(TableNameGetter); ok {
		tableName = tab.GetTableName()
	} else {
		tableName = column.FormatCamelToSnake([]byte(modelType.Name()))
	}

	// 尝试从缓存中获取数据模型；表名为空时不缓存，避免把无效表名固化下来
	driverName := dial.GetDriverName()
	if tableName != "" {
		if tab, ok := GetFromCache(driverName, modelType, tableName); ok {
			return tab, nil
		}
	}

	// 初始化数据表
	fieldNum := modelValue.NumField()
	sch := &Schema{
		TableName:                 tableName,
		ModelName:                 modelType.Name(),
		ModelType:                 modelType,
		Columns:                   make([]column.Column, 0, fieldNum),
		ColumnNames:               make([]string, 0, fieldNum),
		ColumnIndexesInModel:      make([]int, 0, fieldNum),
		ColumnIndexesByName:       make(map[string]int, fieldNum),
		JoinColumnIndexesByName:   make(map[string]int, 2),
		RelationIndexesByName:     make(map[string]int, 2),
		InsertableColumnNames:     make([]string, 0, fieldNum),
		InsertableFieldIndexes:    make([]int, 0, fieldNum),
		UpdateColumnIndexesByName: make(map[string]int, fieldNum),
		BigFieldKinds:             make([]BigKind, fieldNum),
	}

	// 遍历数据模型的所有字段
	for i := range fieldNum {
		field := modelType.Field(i)

		// 过滤掉匿名和不可导出字段
		if field.Anonymous || !ast.IsExported(field.Name) {
			continue
		}

		// 无法无损桥接的大数类型在解析阶段直接拒绝，避免首次写入时才报错
		if err := EnsureBigSupported(field.Name, field.Type); err != nil {
			return nil, err
		}

		// 改名前的标签键已废弃：直接报错而不是静默跳过，
		// 否则旧模型会因少列而在后续 SQL 里报错或写丢数据。
		if _, ok := field.Tag.Lookup(_TAG_LEGACY); ok {
			return nil, fmt.Errorf("%w: field %s uses %q, use %q instead", ErrLegacyTagRemoved, field.Name, _TAG_LEGACY, _TAG2)
		}

		// 解析字段标签：wxorm 优先，其次更早的 orm
		tag := field.Tag.Get(_TAG2)
		switch tag {
		case "-":
			continue
		case "":
			if tag = field.Tag.Get(_TAG1); tag == "-" || tag == "" {
				continue
			}
		}

		// 联表字段：只进入查询投影，不进入建表与读写列集
		if joinColumn, ok, err := parseJoinTag(field.Name, tag); err != nil {
			return nil, err
		} else if ok {
			joinColumn.Index = i
			sch.JoinColumns = append(sch.JoinColumns, joinColumn)
			continue
		}

		// 预加载字段：只记录关联声明，不进入建表、读写与 SELECT 投影
		if relation, ok, err := parsePreloadTag(field.Name, tag, field.Type); err != nil {
			return nil, err
		} else if ok {
			relation.FieldIndex = i
			sch.Relations = append(sch.Relations, relation)
			continue
		}

		// 解析标签，生成数据字段
		col, err := dial.ParseColumn(tag)
		if err != nil {
			return nil, fmt.Errorf("wxorm: failed to parse column tag for field %s: %w", field.Name, err)
		}

		// 检查数据字段名
		if col.GetName() == "" {
			name := column.FormatCamelToSnake([]byte(field.Name))
			col.SetName(name)
		}

		// 检查数据字段类型
		if col.GetDatatype() == "" {
			col.SetDatatype(dial.ConvertDatatype(field.Type))
		}

		// 添加每一个数据表字段
		sch.Fields = append(sch.Fields, &field)
		sch.Columns = append(sch.Columns, col)
		sch.ColumnNames = append(sch.ColumnNames, col.GetName())
		sch.ColumnIndexesInModel = append(sch.ColumnIndexesInModel, i)
		sch.ColumnIndexesByName[col.GetName()] = i

		// 大数字段需要在参数与结果集两侧做文本桥接，提前记录种类供扫描路径使用
		if kind := GetBigKind(field.Type); kind != BIG_NONE {
			sch.BigFieldKinds[i] = kind
			sch.HasBigFields = true
		}

		// 添加可写入的数据表字段
		if !col.IsAutoInsert() {
			sch.InsertableColumnNames = append(sch.InsertableColumnNames, col.GetName())
			sch.InsertableFieldIndexes = append(sch.InsertableFieldIndexes, i)
		}
	}

	// 检查数据表字段
	if len(sch.Columns) == 0 {
		return nil, ErrModelEmptyFields
	}

	// 联表字段只面向查询，需校验结果列别名与主表列不冲突
	if err := sch.validateJoinColumns(); err != nil {
		return nil, err
	}

	// 预加载关联只面向 Preload，需校验同一字段不重复声明
	if err := sch.validatePreloadRelations(); err != nil {
		return nil, err
	}

	// 预计算主键字段：
	//   - 自增主键供 PostgreSQL 的 INSERT ... RETURNING 回填使用；
	//   - 主键列表供 PostgreSQL 的 ON CONFLICT (...) 冲突目标使用。
	// MySQL 的 LastInsertId 不需要这两项
	sch.PrimaryKeyNames = make([]string, 0, 2)
	sch.PrimaryKeyFields = make([]FieldIndex, 0, 2)
	sch.UpdateFields = make([]FieldIndex, 0, fieldNum)
	for colIdx, fieldIdx := range sch.ColumnIndexesInModel {
		col := sch.Columns[colIdx]
		if col.IsPrimaryKey() {
			sch.PrimaryKeyNames = append(sch.PrimaryKeyNames, col.GetName())
			sch.PrimaryKeyFields = append(sch.PrimaryKeyFields, FieldIndex{Name: col.GetName(), Index: fieldIdx})
			if sch.AutoPrimaryKeyName == "" && col.IsAutoInsert() {
				sch.AutoPrimaryKeyName = col.GetName()
			}

			continue
		}

		// 可更新列：排除主键与由数据库填充的列（auto_increment、created、updated ...）
		if !col.IsAutoInsert() {
			sch.UpdateColumnIndexesByName[col.GetName()] = len(sch.UpdateFields)
			sch.UpdateFields = append(sch.UpdateFields, FieldIndex{Name: col.GetName(), Index: fieldIdx})
		}
	}

	// 预计算插入语句的缓存键片段：可插入列集
	// 同一张表可能对应多个模型（字段集不同），缓存键必须包含字段集，否则会命中共用错误的预编译语句；
	// 表名由会话在物理表名确定后拼接，因此同一模型在不同前缀下不会共用语句。
	sch.InsertColumnsKey = strings.Join(sch.InsertableColumnNames, "\x1f")

	// 缓存数据模型；表名为空时跳过，下次解析重新计算
	if tableName != "" {
		PutToCache(driverName, modelType, tableName, sch)
	}

	return sch, nil
}

// GetInsertCacheKey 返回 INSERT 预编译语句的缓存键
func (own *Schema) GetInsertCacheKey(table string) string {
	return table + "\x1find:\x1f" + own.InsertColumnsKey
}

// GetInsertIgnoreCacheKey 返回 INSERT IGNORE 预编译语句的缓存键
func (own *Schema) GetInsertIgnoreCacheKey(table string) string {
	return table + "\x1find-ignore:\x1f" + own.InsertColumnsKey
}

// GetColumnByName 根据字段名获取数据字段
func (own *Schema) GetColumnByName(name string) (column.Column, error) {
	for _, col := range own.Columns {
		if col.GetName() == name {
			return col, nil
		}
	}

	return nil, fmt.Errorf("wxorm: column '%s' not found in table '%s'", name, own.TableName)
}

/*
ExtractValue 从结构体中提取单个数据
  - value：数据格式 Struct 或 *Struct
*/
func (own *Schema) ExtractValue(value any) ([]any, error) {
	val, err := own.ValidateStruct(value)
	if err != nil {
		return nil, err
	}

	dst := make([]any, len(own.InsertableFieldIndexes))
	for i, idx := range own.InsertableFieldIndexes {
		dst[i], _ = GetReflectFieldValue(val.Field(idx))
	}

	return dst, nil
}

// ValidateStruct 校验数据表结构体
func (own *Schema) ValidateStruct(value any) (reflect.Value, error) {
	if value == nil {
		return reflect.Value{}, ErrValueNil
	}

	val := reflect.ValueOf(value)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("wxorm: value must be struct or *struct, got %T", value)
	}

	if val.Type() != own.ModelType {
		return reflect.Value{}, fmt.Errorf("wxorm: value model '%s' is not match schema model '%s'", val.Type().Name(), own.ModelType)
	}

	return val, nil
}

// ExtractNonZeroPrimaryKeyValues 按列顺序提取非零主键值
//
// 返回切片而非 map：多主键的 WHERE 条件顺序稳定，预编译语句才能稳定命中缓存。
func (own *Schema) ExtractNonZeroPrimaryKeyValues(value reflect.Value) []FieldValue {
	values := make([]FieldValue, 0, len(own.PrimaryKeyFields))
	for _, field := range own.PrimaryKeyFields {
		if pkValue, zero := GetReflectFieldValue(value.Field(field.Index)); !zero {
			values = append(values, FieldValue{Name: field.Name, Value: pkValue})
		}
	}

	return values
}

// ExtractNonZeroNamesAndValuesFromPrimaryKeys 提取主键中的字段和数据（非零值）
func (own *Schema) ExtractNonZeroNamesAndValuesFromPrimaryKeys(value reflect.Value) map[string]any {
	primaryKeys := own.ExtractNonZeroPrimaryKeyValues(value)
	values := make(map[string]any, len(primaryKeys))
	for _, primaryKey := range primaryKeys {
		values[primaryKey.Name] = primaryKey.Value
	}

	return values
}

// ExtractUpdateNamesAndValues 提取可更新列的字段和数据
//
// includeZero 为 false 时跳过零值字段，只把非零字段写回；
// 需要把字段显式置零时传 true（对应 Session.OVERWRITE），或改用 UPDATE(map[string]any)。
func (own *Schema) ExtractUpdateNamesAndValues(value reflect.Value, includeZero bool) map[string]any {
	values := make(map[string]any, len(own.UpdateFields))
	for _, field := range own.UpdateFields {
		val, zero := GetReflectFieldValue(value.Field(field.Index))
		if zero && !includeZero {
			continue
		}

		values[field.Name] = val
	}

	return values
}

// ExtractUpdateNamesAndValuesOf 只提取白名单中的可更新列（无论是否为零值）
//
// 用于 UPDATE_COLUMNS：调用方显式列出要写入的列，零值同样写回，
// 未列出的列保持数据库中的原值。列名不在可更新列集合中时返回 ErrUnknownUpdateColumn。
func (own *Schema) ExtractUpdateNamesAndValuesOf(value reflect.Value, names []string) (map[string]any, error) {
	values := make(map[string]any, len(names))
	for _, name := range names {
		index, ok := own.UpdateColumnIndexesByName[name]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnknownUpdateColumn, name)
		}

		field := own.UpdateFields[index]
		val, _ := GetReflectFieldValue(value.Field(field.Index))
		values[field.Name] = val
	}

	return values, nil
}

// ExtractNamesAndValuesFromNonPrimaryKeys 提取非主键中的字段和数据（包括零值）
func (own *Schema) ExtractNamesAndValuesFromNonPrimaryKeys(value reflect.Value) map[string]any {
	return own.ExtractUpdateNamesAndValues(value, true)
}

// ExtractNonZeroNamesAndValuesFromNonPrimaryKeys 提取非主键中的字段和数据（非零值）
func (own *Schema) ExtractNonZeroNamesAndValuesFromNonPrimaryKeys(value reflect.Value) map[string]any {
	return own.ExtractUpdateNamesAndValues(value, false)
}

// GetModelStruct 获取数据模型的数据结构
func GetModelStruct(model any) (reflect.Value, error) {
	if model == nil {
		return reflect.Value{}, ErrModelNilPointer
	}

	// 如果数据模型是指针类型，则获取底层类型
	value := reflect.ValueOf(model)
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return reflect.Value{}, ErrModelNilPointer
		}

		value = value.Elem()
	}

	// 检查数据模型是否为 Go 结构体
	if value.Type().Kind() != reflect.Struct {
		return reflect.Value{}, ErrModelNotStruct
	}

	return value, nil
}

// GetReflectFieldValue 获取反射字段的值
func GetReflectFieldValue(field reflect.Value) (value any, zero bool) {
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value := field.Int()
		return value, value == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value := field.Uint()
		return value, value == 0
	case reflect.Float32, reflect.Float64:
		value := field.Float()
		return value, value == 0
	case reflect.Bool:
		value := field.Bool()
		return value, !value
	case reflect.String:
		value := field.String()
		return value, value == ""
	default:
		// 复杂类型使用 Interface()
		value := field.Interface()
		switch field.Kind() {
		case reflect.Ptr:
			if field.IsNil() {
				return nil, true
			}

			// 大数指针需要先格式化为文本，驱动不接受 struct 参数
			if kind := GetBigKind(field.Type()); kind != BIG_NONE {
				formatted, zero, _ := FormatBigValue(field, kind)
				return formatted, zero
			}

			return value, false
		case reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan:
			return value, field.IsNil()
		case reflect.Struct:
			// 大数值类型（big.Int / big.Float）同样要转成十进制文本
			if kind := GetBigKind(field.Type()); kind != BIG_NONE {
				formatted, zero, _ := FormatBigValue(field, kind)
				return formatted, zero
			}

			// IsZero 对 time.Time 等结构体逐字段比较，比 reflect.DeepEqual 快一个数量级
			return value, field.IsZero()
		default:
			return value, field.IsZero()
		}
	}
}

// IsZeroValue 判断是否为零值
func IsZeroValue(value reflect.Value) bool {
	if !value.IsValid() {
		return true
	}

	switch value.Kind() {
	case reflect.String:
		return value.String() == ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return value.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return value.Float() == 0
	case reflect.Bool:
		return !value.Bool()
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan:
		return value.IsNil()
	default:
		// 对于复杂类型，逐字段比较；比 DeepEqual 快且不分配内存
		return value.IsZero()
	}
}
