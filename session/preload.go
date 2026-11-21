package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"gitee.com/leminewx/wxorm/schema"
	"gitee.com/leminewx/wxorm/statement"
	"gitee.com/leminewx/wxorm/statement/where"
)

var (
	// ErrPreloadUnsupported 表示写语句不支持 PRELOAD
	ErrPreloadUnsupported = errors.New("wxorm: PRELOAD is only supported by SELECT and FindFirst")
	// ErrPreloadColumnNotFound 表示关联列在主表或目标表中不存在
	ErrPreloadColumnNotFound = errors.New("wxorm: preload relation column does not exist")
	// ErrPreloadRequiresSingleKey 表示主表或目标表不是单列主键
	ErrPreloadRequiresSingleKey = errors.New("wxorm: PRELOAD requires single-column primary keys")
	// ErrPreloadKeyNotSelected 表示 COLUMNS 没有包含预加载需要的分组键
	ErrPreloadKeyNotSelected = errors.New("wxorm: preload key column is not included in COLUMNS")
)

// maxPreloadBatchParams 是单条子查询允许的最大参数个数，与 SQLite 的绑定变量上限对齐
const maxPreloadBatchParams = 30000

/*
PRELOAD 声明本次查询要预加载的关联字段（传 Go 字段名）

不传参数表示加载模型声明的全部关联；只对 SELECT / FindFirst 生效，
COUNT / Exist 会丢弃该声明，写语句返回 ErrPreloadUnsupported。
与 JOIN 一样「取走即归零」：一次查询结束后不影响下一次调用。
*/
func (own *Session) PRELOAD(fields ...string) *Session {
	own.resetPreloads()
	own.preloadSet = true

	if len(fields) == 0 {
		own.preloadAll = true
		return own
	}

	own.preloads = append(own.preloads, fields...)
	return own
}

// takePreloads 取出本次查询的预加载声明并清空会话状态
func (own *Session) takePreloads() (bool, []string, error) {
	all, fields, err := own.preloadAll, own.preloads, own.preloadErr
	own.resetPreloads()
	return all, fields, err
}

// resetPreloads 清空预加载状态
func (own *Session) resetPreloads() {
	own.preloadSet = false
	own.preloadAll = false
	own.preloads = nil
	own.preloadErr = nil
}

// ensureNoPreload 写语句的守卫：存在 PRELOAD 时拒绝执行
func (own *Session) ensureNoPreload() error {
	if !own.preloadSet && len(own.preloads) == 0 {
		return nil
	}

	own.resetPreloads()
	return ErrPreloadUnsupported
}

// loadPreloads 在父结果扫描完成后加载关联对象
//
// parents 是父结果元素（结构体值，可寻址）；requested 是 COLUMNS 指定的列名，
// nil 表示查询了全部列。
func (own *Session) loadPreloads(parents []reflect.Value, requested []string) error {
	all, fields, err := own.takePreloads()
	if err != nil {
		return err
	}

	if !all && len(fields) == 0 {
		return nil
	}

	parentSch, err := own.GetSchema()
	if err != nil {
		return err
	}

	var names []string
	if !all {
		names = fields
	}

	relations, err := parentSch.SelectRelations(names...)
	if err != nil {
		return err
	}

	if len(relations) == 0 || len(parents) == 0 {
		return nil
	}

	// 整次预加载共用一个上下文，子查询在 loadRelation 内部消费完毕，
	// 因此可以放心在函数返回时取消
	ctx, cancel := own.execContext()
	defer cancel()

	for _, relation := range relations {
		if err := own.loadRelation(ctx, parents, parentSch, relation, requested); err != nil {
			return err
		}
	}

	return nil
}

// loadRelation 加载单个关联：解析目标 Schema、收集键、分批查询并分配
func (own *Session) loadRelation(ctx context.Context, parents []reflect.Value, parentSch *schema.Schema, relation schema.Relation, requested []string) error {
	targetSch, err := schema.Parse(own.dialect, reflect.New(relation.TargetType).Interface())
	if err != nil {
		return fmt.Errorf("wxorm: failed to parse preload target %s: %w", relation.TargetTable, err)
	}

	if len(targetSch.PrimaryKeyNames) != 1 || len(parentSch.PrimaryKeyNames) != 1 {
		return fmt.Errorf("%w: %s", ErrPreloadRequiresSingleKey, relation.FieldName)
	}

	// belongs_to：本表关联列 → 目标主键；has_many：目标关联列 → 本表主键
	parentKeyColumn := relation.Column
	matchColumn := targetSch.PrimaryKeyNames[0]
	if relation.Kind == schema.RELATION_HAS_MANY {
		parentKeyColumn = parentSch.PrimaryKeyNames[0]
		matchColumn = relation.Column

		if _, ok := targetSch.ColumnIndexesByName[matchColumn]; !ok {
			return fmt.Errorf("%w: %s.%s", ErrPreloadColumnNotFound, relation.TargetTable, matchColumn)
		}
	}

	parentKeyIndex, ok := parentSch.ColumnIndexesByName[parentKeyColumn]
	if !ok {
		return fmt.Errorf("%w: %s.%s", ErrPreloadColumnNotFound, parentSch.TableName, parentKeyColumn)
	}

	// COLUMNS 未取分组键时不能用零值当键，必须显式报错
	if !isPreloadKeySelected(requested, parentKeyColumn) {
		return fmt.Errorf("%w: %s", ErrPreloadKeyNotSelected, parentKeyColumn)
	}

	keys := collectPreloadKeys(parents, parentKeyIndex)
	if len(keys) == 0 {
		return nil
	}

	grouped, err := own.queryPreloadValues(ctx, targetSch, matchColumn, keys)
	if err != nil {
		return err
	}

	return assignPreloadValues(parents, relation, parentKeyIndex, grouped)
}

// preloadKey 保存父键的原始值与归一化键
type preloadKey struct {
	value any
	key   string
}

// collectPreloadKeys 收集父结果的分组键，跳过零值并去重
func collectPreloadKeys(parents []reflect.Value, index int) []preloadKey {
	keys := make([]preloadKey, 0, len(parents))
	seen := make(map[string]struct{}, len(parents))

	for _, parent := range parents {
		value := derefPreloadValue(parent.Field(index))
		if !value.IsValid() {
			continue
		}

		raw := value.Interface()
		if isZeroPreloadKey(raw) {
			continue
		}

		key := stringKey(raw)
		if key == "" {
			continue
		}

		if _, ok := seen[key]; ok {
			continue
		}

		seen[key] = struct{}{}
		keys = append(keys, preloadKey{value: raw, key: key})
	}

	return keys
}

// queryPreloadValues 按参数上限分批查询子行，并按匹配列的值分组
func (own *Session) queryPreloadValues(ctx context.Context, targetSch *schema.Schema, matchColumn string, keys []preloadKey) (map[string][]reflect.Value, error) {
	grouped := make(map[string][]reflect.Value, len(keys))

	for start := 0; start < len(keys); start += maxPreloadBatchParams {
		end := min(start+maxPreloadBatchParams, len(keys))

		values := make([]any, end-start)
		for i, key := range keys[start:end] {
			values[i] = key.value
		}

		query, args, err := own.buildPreloadQuery(targetSch, matchColumn, values)
		if err != nil {
			return nil, err
		}

		rows, _, err := own.queryPrepared(ctx, query, args...)
		if err != nil {
			return nil, err
		}

		if err := own.scanPreloadRows(rows, targetSch, matchColumn, grouped); err != nil {
			_ = rows.Close()
			return nil, err
		}

		if err := rows.Close(); err != nil {
			return nil, err
		}
	}

	return grouped, nil
}

// buildPreloadQuery 构建子查询：SELECT 目标表全部列 WHERE 关联列 IN (...) ORDER BY 主键
func (own *Session) buildPreloadQuery(targetSch *schema.Schema, matchColumn string, values []any) (string, []any, error) {
	physical := own.naming.GetTableName(targetSch.TableName)
	qualified := own.naming.Qualify(physical)

	stmt := statement.NewStatement(targetSch.TableName)
	stmt.AddClause(statement.SELECT(targetSch.TableName, targetSch.ColumnNames...))
	stmt.AddClause(statement.FROM(qualified))
	stmt.AddClause(statement.WHERE(where.IN(matchColumn, values...)))
	stmt.AddClause(statement.ORDER(targetSch.PrimaryKeyNames...))

	return stmt.BuildSQL(own.dialect,
		statement.NAME_SELECT,
		statement.NAME_FROM,
		statement.NAME_WHERE,
		statement.NAME_ORDER,
	)
}

// scanPreloadRows 扫描子查询结果；列顺序与目标 Schema 的 ColumnNames 一致
func (own *Session) scanPreloadRows(rows *sql.Rows, targetSch *schema.Schema, matchColumn string, grouped map[string][]reflect.Value) error {
	matchIndex, ok := targetSch.ColumnIndexesByName[matchColumn]
	if !ok {
		return fmt.Errorf("%w: %s.%s", ErrPreloadColumnNotFound, targetSch.TableName, matchColumn)
	}

	scanFields := targetSch.ColumnIndexesInModel
	scanArgs := make([]any, len(scanFields))

	// 关联模型含大数字段时同样需要文本桥接，普通模型上 bigScan 为 nil
	bigScan := newBigScanner(targetSch, scanFields)
	if bigScan != nil {
		bigScan.Bind(scanArgs)
	}

	for rows.Next() {
		value := reflect.New(targetSch.ModelType).Elem()
		for i, idx := range scanFields {
			if bigScan.Covers(i) {
				continue
			}

			scanArgs[i] = value.Field(idx).Addr().Interface()
		}

		if err := rows.Scan(scanArgs...); err != nil {
			return fmt.Errorf("wxorm: failed to scan preload rows: %w", err)
		}

		if bigScan != nil {
			if err := bigScan.Assign(value); err != nil {
				return err
			}
		}

		keyValue := derefPreloadValue(value.Field(matchIndex))
		if !keyValue.IsValid() {
			continue
		}

		key := stringKey(keyValue.Interface())
		if key == "" {
			continue
		}

		grouped[key] = append(grouped[key], value)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("wxorm: failed to iterate preload rows: %w", err)
	}

	return nil
}

// assignPreloadValues 把分组后的子行分配给父结果
func assignPreloadValues(parents []reflect.Value, relation schema.Relation, parentKeyIndex int, grouped map[string][]reflect.Value) error {
	for _, parent := range parents {
		field := parent.Field(relation.FieldIndex)
		if !field.CanSet() {
			continue
		}

		key := ""
		if keyValue := derefPreloadValue(parent.Field(parentKeyIndex)); keyValue.IsValid() {
			key = stringKey(keyValue.Interface())
		}

		matches := grouped[key]
		switch relation.Kind {
		case schema.RELATION_BELONGS_TO:
			// 无匹配时保持 nil/零值，避免伪造关联对象
			if len(matches) == 0 {
				continue
			}

			if field.Kind() == reflect.Ptr {
				field.Set(matches[0].Addr())
			} else {
				field.Set(matches[0])
			}

		case schema.RELATION_HAS_MANY:
			// 无匹配时返回空切片而不是 nil，便于 JSON 序列化为 []
			elemType := field.Type().Elem()
			slice := reflect.MakeSlice(field.Type(), 0, len(matches))
			for _, match := range matches {
				if elemType.Kind() == reflect.Ptr {
					slice = reflect.Append(slice, match.Addr())
					continue
				}

				slice = reflect.Append(slice, match)
			}

			field.Set(slice)
		}
	}

	return nil
}

// isPreloadKeySelected 判断分组键是否包含在 COLUMNS 中
func isPreloadKeySelected(requested []string, column string) bool {
	if requested == nil {
		return true
	}

	for _, name := range requested {
		if normalizeColumnName(name) == column {
			return true
		}
	}

	return false
}

// derefPreloadValue 逐层解引用指针与接口，nil 返回零值
func derefPreloadValue(value reflect.Value) reflect.Value {
	for value.IsValid() {
		switch value.Kind() {
		case reflect.Ptr, reflect.Interface:
			if value.IsNil() {
				return reflect.Value{}
			}

			value = value.Elem()
		default:
			return value
		}
	}

	return value
}

// isZeroPreloadKey 判断键值是否应跳过（nil、空字符串、数值零等）
func isZeroPreloadKey(value any) bool {
	if value == nil {
		return true
	}

	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.String, reflect.Slice, reflect.Map:
		return reflected.Len() == 0
	default:
		return reflected.IsZero()
	}
}

// stringKey 把键值归一化为字符串，保证父子两侧使用同一套比较规则
func stringKey(value any) string {
	switch tv := value.(type) {
	case nil:
		return ""
	case string:
		return tv
	case []byte:
		return string(tv)
	case int:
		return strconv.FormatInt(int64(tv), 10)
	case int8, int16, int32, int64:
		return strconv.FormatInt(reflect.ValueOf(value).Int(), 10)
	case uint, uint8, uint16, uint32, uint64:
		return strconv.FormatUint(reflect.ValueOf(value).Uint(), 10)
	case float32, float64:
		return strconv.FormatFloat(reflect.ValueOf(value).Float(), 'g', -1, 64)
	case bool:
		return strconv.FormatBool(tv)
	case time.Time:
		return tv.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(value)
	}
}
