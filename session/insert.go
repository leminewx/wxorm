package session

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/schema"
	"gitee.com/leminewx/wxorm/session/result"
	"gitee.com/leminewx/wxorm/statement"
)

// insertClauseNames 固定 INSERT 的子句顺序
//
// PostgreSQL 的 ON CONFLICT 必须写在 VALUES 之后，RETURNING 必须写在最后；
// 未添加的子句会被 BuildSQL 跳过，因此这里可以一次性声明全部候选子句。
var insertClauseNames = []statement.Name{
	statement.NAME_INSERT,
	statement.NAME_VALUES,
	statement.NAME_IGNORE,
	statement.NAME_RETURNING,
}

// findReturningColumns 判断是否需要 INSERT ... RETURNING 回填自增主键
//
// 只有方言实现了 clause.ReturningInserter（如 PostgreSQL）且模型定义了自增主键时才启用；
// MySQL 通过 sql.Result.LastInsertId 取回主键，不需要额外子句。
func (own *Session) findReturningColumns(sch *schema.Schema) []string {
	if sch == nil || sch.AutoPrimaryKeyName == "" {
		return nil
	}

	if _, ok := own.dialect.(clause.ReturningInserter); !ok {
		return nil
	}

	return []string{sch.AutoPrimaryKeyName}
}

// insertReturning 执行带 RETURNING 的 INSERT，并把主键回填到结果与模型
func (own *Session) insertReturning(names []statement.Name, model any, pkName string) (result.Result, error) {
	query, args, err := own.statement.BuildSQL(own.dialect, names...)
	own.statement.Reset()
	if err != nil {
		return nil, err
	}

	ctx, cancel := own.execContext()
	defer cancel()

	start := time.Now()
	var raw any
	row, _ := own.queryRowPrepared(ctx, query, args...)
	scanErr := row.Scan(&raw)
	cost := time.Since(start)

	switch {
	case scanErr == nil:
	case errors.Is(scanErr, sql.ErrNoRows):
		// INSERT ... ON CONFLICT DO NOTHING 命中冲突时不会返回任何行
		return result.NewInsertResult(0, 0, cost, query, args...), nil
	default:
		return nil, scanErr
	}

	// 回填模型的主键字段，调用方插入后可以直接使用 value.ID
	setModelPrimaryKey(model, own.schema, pkName, raw)

	return result.NewInsertResult(convertToInt64(raw), 1, cost, query, args...), nil
}

// findBatchReturningColumns 判断批量插入是否需要 RETURNING 回填自增主键
//
// 只有方言实现了 clause.BatchReturningInserter（PostgreSQL/SQLite）且模型定义了自增主键时才启用；
// MySQL 无法逐行取回生成的主键，批量插入保持不回填。
func (own *Session) findBatchReturningColumns(sch *schema.Schema) []string {
	if sch == nil || sch.AutoPrimaryKeyName == "" {
		return nil
	}

	if _, ok := own.dialect.(clause.BatchReturningInserter); !ok {
		return nil
	}

	return []string{sch.AutoPrimaryKeyName}
}

// insertReturningMany 执行带 RETURNING 的多行 INSERT，并按行序回填主键
func (own *Session) insertReturningMany(names []statement.Name, values []any, pkName string) (result.Result, error) {
	query, args, err := own.statement.BuildSQL(own.dialect, names...)
	own.statement.Reset()
	if err != nil {
		return nil, err
	}

	ctx, cancel := own.execContext()
	defer cancel()

	start := time.Now()
	rows, _, err := own.queryPrepared(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	// RETURNING 的行序与 VALUES 的行序一致，可以按下标回填
	ids := make([]int64, 0, len(values))
	for rows.Next() {
		var raw any
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}

		ids = append(ids, convertToInt64(raw))
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	cost := time.Since(start)

	// RETURNING 的行序在 PostgreSQL / SQLite 的实现中与 VALUES 的行序一致，
	// 但“行序一致”不是 SQL 标准保证：先用行数校验兜底，
	// 不一致时宁可报错也不要把主键回填到错误的模型上
	if len(ids) != len(values) {
		return nil, fmt.Errorf("%w: wrote %d row(s), returned %d", ErrBatchReturningMismatch, len(values), len(ids))
	}

	for i, id := range ids {
		setModelPrimaryKey(values[i], own.schema, pkName, id)
	}

	var lastInsertID int64
	if len(ids) > 0 {
		lastInsertID = ids[len(ids)-1]
	}

	return result.NewBatchResult(int64(len(ids)), lastInsertID, cost, query, args...), nil
}

// setModelPrimaryKey 把数据库生成的主键写回模型指针字段
func setModelPrimaryKey(model any, sch *schema.Schema, columnName string, raw any) {
	if model == nil || sch == nil {
		return
	}

	index, ok := sch.ColumnIndexesByName[columnName]
	if !ok {
		return
	}

	value := reflect.ValueOf(model)
	for value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return
		}

		value = value.Elem()
	}

	if value.Kind() != reflect.Struct || index >= value.NumField() {
		return
	}

	field := value.Field(index)
	if !field.CanSet() {
		return
	}

	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		field.SetInt(convertToInt64(raw))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if id := convertToInt64(raw); id > 0 {
			field.SetUint(uint64(id))
		}
	case reflect.String:
		field.SetString(convertToText(raw))
	}
}

// convertToInt64 把驱动返回的主键值转换为 int64
func convertToInt64(raw any) int64 {
	switch value := raw.(type) {
	case int64:
		return value
	case int32:
		return int64(value)
	case int:
		return int64(value)
	case uint64:
		return int64(value)
	case float64:
		return int64(value)
	case []byte:
		id, _ := strconv.ParseInt(string(value), 10, 64)
		return id
	case string:
		id, _ := strconv.ParseInt(value, 10, 64)
		return id
	default:
		return 0
	}
}

// convertToText 把驱动返回的文本主键转换为 string
func convertToText(raw any) string {
	switch value := raw.(type) {
	case []byte:
		return string(value)
	case string:
		return value
	default:
		return ""
	}
}
