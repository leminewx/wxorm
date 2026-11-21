package session

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/schema"
	"gitee.com/leminewx/wxorm/session/result"
	"gitee.com/leminewx/wxorm/statement"
	"gitee.com/leminewx/wxorm/statement/where"
)

var (
	ErrTableNoPK                         = errors.New("wxorm: table no primary key")
	ErrTablePKValuesZero                 = errors.New("wxorm: values of primary keys are zero")
	ErrMissingWhere                      = errors.New("wxorm: requires WHERE clause, use .WHERE() first")
	ErrOffsetRequiresLimit               = errors.New("wxorm: OFFSET requires LIMIT clause, use .LIMIT() first")
	ErrInsertValuesEmpty                 = errors.New("wxorm: insert values are empty")
	ErrBatchReturningMismatch            = errors.New("wxorm: batch INSERT RETURNING row count mismatch")
	ErrUpdateValuesNil                   = errors.New("wxorm: update values are nil")
	ErrUpdateMapEmpty                    = errors.New("wxorm: update map is empty")
	ErrUpdateStructEmpty                 = errors.New("wxorm: update struct has no non-zero field, use OVERWRITE(), UPDATE_COLUMNS(...) or UPDATE(map)")
	ErrUpdateColumnsEmpty                = errors.New("wxorm: UPDATE_COLUMNS requires at least one column")
	ErrSelectValueNil                    = errors.New("wxorm: select values are nil")
	ErrSelectValuesNotPointer            = errors.New("wxorm: select values must be pointer, eg: *Struct, *[]Struct, *[]*Struct")
	ErrSelectDestinationValueInvalid     = errors.New("wxorm: select destination value is invalid")
	ErrSelectDestinationValueNotSettable = errors.New("wxorm: select destination value is not settable")
	ErrSelectDestinationSliceNotSettable = errors.New("wxorm: select destination slice is not settable")
	ErrRecordNotFound                    = errors.New("wxorm: record not found")
	ErrSelectColumnNotFound              = errors.New("wxorm: select column not found in model")

	// ErrCountWithGroupUnsupported COUNT 无法表达 GROUP BY / HAVING 的语义
	//
	// 带分组时每个分组各返回一行，而 COUNT 只返回单个数值；静默丢掉分组会得到
	// 与调用方预期不同的数字，因此显式拒绝，请改用 SELECT 读取分组统计。
	ErrCountWithGroupUnsupported = errors.New("wxorm: COUNT does not support GROUP BY / HAVING, use SELECT instead")
)

// -------------------------------------------------------------------------------------------------------------------------------------------
// INSERT 插入数据
// -------------------------------------------------------------------------------------------------------------------------------------------

/*
INSERT 插入单条或多条数据到数据表
  - ignore: 插入语句 INSERT IGNORE INTO ...
  - values: 支持格式 ...Struct 或 ...*Struct
*/
func (own *Session) INSERT(ignore bool, values ...any) (result.Result, error) {
	defer own.statement.Reset()

	// 写语句不支持 JOIN 与 PRELOAD，存在时直接拒绝并清空状态
	if err := own.ensureNoJoin(); err != nil {
		return nil, err
	}

	if err := own.ensureNoPreload(); err != nil {
		return nil, err
	}

	// 获取数据表信息
	sch, err := own.GetSchema()
	if err != nil {
		return nil, err
	}

	// 根据数量动态选择插入策略：单条复用按模型键缓存的语句，多条按方言占位符上限分批
	rows := len(values)
	switch {
	case rows == 0:
		return nil, ErrInsertValuesEmpty
	case rows == 1 && !own.statement.Has(statement.NAME_DUPLICATE):
		return own.insertOne(sch, ignore, values[0])
	default:
		batchSize := own.insertBatchSize(len(sch.InsertableColumnNames))
		if rows <= batchSize {
			return own.insertMany(sch, ignore, values)
		}

		return own.insertBatches(sch, ignore, values, batchSize)
	}
}

const (
	// maxInsertBatchRows 单条 INSERT 语句最多包含的行数
	maxInsertBatchRows = 1000
	// maxInsertBatchParams MySQL/PostgreSQL 单条语句的占位符上限（PostgreSQL 为 65535）
	maxInsertBatchParams = 60000
	// maxInsertBatchParamsSQLite SQLite 3.32+ 的绑定变量上限为 32766
	maxInsertBatchParamsSQLite = 30000
)

// insertBatchSize 按方言占位符上限与列数计算单批最大行数
func (own *Session) insertBatchSize(columns int) int {
	if columns <= 0 {
		return 1
	}

	maxParams := maxInsertBatchParams
	switch strings.ToLower(own.dialect.GetDriverName()) {
	case "sqlite", "sqlite3":
		maxParams = maxInsertBatchParamsSQLite
	}

	batch := maxParams / columns
	if batch > maxInsertBatchRows {
		return maxInsertBatchRows
	}

	if batch < 1 {
		return 1
	}

	return batch
}

// insertOne 插入单条数据
func (own *Session) insertOne(schema *schema.Schema, ignore bool, value any) (res result.Result, err error) {
	// 调用 CallBeforeInsert 方法
	if err = own.CallMethod(_HOOK_BEFORE_INSERT, value); err != nil {
		return nil, err
	}

	// 提取对象数据
	values, err := schema.ExtractValue(value)
	if err != nil {
		return nil, err
	}

	// 获取预编译语句缓存键
	// 键包含物理表名与可插入字段集：不同前缀的表、同表不同模型都不能共用一条预编译语句
	key := own.insertKey
	if ignore {
		key = own.insertIgnoreKey
	}

	// 方言是否需要 INSERT ... RETURNING 回填自增主键
	returning := own.findReturningColumns(schema)

	// 预编译语句绑定在 *sql.DB 上，事务内复用会绕过事务，因此只在非事务场景使用缓存；
	// 带 RETURNING 的语句要逐行读取返回值，不能走 Exec 路径
	if own.tx == nil && len(returning) == 0 {
		if stmt, ok := own.stmts.Load(key); ok {
			ctx, cancel := own.execContext()
			defer cancel()

			// 执行预编译的 INSERT 语句
			stm := stmt.(*Stmt)
			start := time.Now()
			r, err := stm.ExecContext(ctx, own.id, false, false, values...)
			cost := time.Since(start)
			own.logQuery(cost, stm.query, values...)
			if err != nil {
				return nil, err
			}

			res = result.NewResult(r, cost, stm.query, values...)

			// 调用 CallAfterInsert 方法
			return res, own.CallMethod(_HOOK_AFTER_INSERT, value)
		}
	}

	// 添加 INSERT 子句
	own.statement.AddClause(statement.INSERT(own.tableSQL, ignore, schema.InsertableColumnNames...)).
		AddClause(statement.VALUES(values))

	// PostgreSQL 的 IGNORE 是 VALUES 之后的 ON CONFLICT DO NOTHING，需要独立子句；
	// MySQL 已在 INSERT 内联 IGNORE，该子句为空操作
	if ignore {
		own.statement.AddClause(statement.INSERT_IGNORE())
	}

	// 执行 INSERT 语句：方言支持 RETURNING 时改走查询路径，否则复用预编译语句
	if len(returning) > 0 {
		own.statement.AddClause(statement.RETURNING(returning...))
		if res, err = own.insertReturning(insertClauseNames, value, returning[0]); err != nil {
			return res, err
		}

		// RETURNING 路径同样需要触发后置钩子，与普通 Exec 路径保持一致
		return res, own.CallMethod(_HOOK_AFTER_INSERT, value)
	}

	// 构建并执行 INSERT 语句（事务内不复用预编译语句）
	if res, err = own.buildAndExecContext(own.tx == nil, key, insertClauseNames...); err != nil {
		return
	}

	// 调用 CallAfterInsert 方法
	return res, own.CallMethod(_HOOK_AFTER_INSERT, value)
}

// insertMany 插入多条数据（实时构建 INSERT 语句）
func (own *Session) insertMany(schema *schema.Schema, ignore bool, values []any) (res result.Result, err error) {
	// 数据类型未实现插入钩子时不进入钩子分支，省去每行的接口装箱
	valueType := reflect.TypeOf(values[0])
	hasBeforeInsert := valueType != nil && valueType.Implements(hookBeforeInsertType)
	hasAfterInsert := valueType != nil && valueType.Implements(hookAfterInsertType)

	// 提取对象数据
	recordValues := make([][]any, len(values))
	for i, value := range values {
		// 调用 CallBeforeInsert 方法
		if hasBeforeInsert {
			if err = own.CallMethod(_HOOK_BEFORE_INSERT, value); err != nil {
				return nil, err
			}
		}

		// 提取数据
		vals, err := schema.ExtractValue(value)
		if err != nil {
			return nil, err
		}

		recordValues[i] = vals
	}

	// 添加 INSERT 子句
	own.statement.AddClause(statement.INSERT(own.tableSQL, ignore, schema.InsertableColumnNames...)).
		AddClause(statement.VALUES(recordValues...))

	// PostgreSQL 的 IGNORE 是 VALUES 之后的 ON CONFLICT DO NOTHING；
	// 与 DUPLICATE（ON CONFLICT DO UPDATE）互斥，同时存在时以 DUPLICATE 为准
	if ignore && !own.statement.Has(statement.NAME_DUPLICATE) {
		own.statement.AddClause(statement.INSERT_IGNORE())
	}

	// PostgreSQL/SQLite 支持批量 RETURNING：一次取回全部生成主键并按行序回填；
	// 冲突忽略与冲突更新的行不保证返回，行序会与模型错位，因此这两条路径不回填
	if !ignore && !own.statement.Has(statement.NAME_DUPLICATE) {
		if returning := own.findBatchReturningColumns(schema); len(returning) > 0 {
			own.statement.AddClause(statement.RETURNING(returning...))
			if res, err = own.insertReturningMany(insertClauseNames, values, returning[0]); err != nil {
				return res, err
			}

			// RETURNING 路径同样需要触发后置钩子，与普通 Exec 路径保持一致
			if hasAfterInsert {
				for _, value := range values {
					if err = own.CallMethod(_HOOK_AFTER_INSERT, value); err != nil {
						return res, err
					}
				}
			}

			return res, nil
		}
	}

	// 构建并执行 INSERT 语句（非事务场景复用查询语句缓存）
	if res, err = own.buildAndExecContext(true, "", statement.NAME_INSERT, statement.NAME_VALUES, statement.NAME_IGNORE, statement.NAME_DUPLICATE); err != nil {
		return res, err
	}

	// 调用 CallAfterInsert 方法
	if hasAfterInsert {
		for _, value := range values {
			if err = own.CallMethod(_HOOK_AFTER_INSERT, value); err != nil {
				return res, err
			}
		}
	}

	return res, nil
}

// insertBatches 批量插入数据（实时构建 INSERT 语句）
func (own *Session) insertBatches(schema *schema.Schema, ignore bool, values []any, batchSize int) (res result.Result, err error) {
	var lastError error
	var totalRows, lastInsertID int64

	// 如果不在事务中，开启事务
	if own.tx == nil {
		if err := own.Begin(); err != nil {
			return nil, err
		}

		defer func() {
			if p := recover(); p != nil {
				_ = own.Rollback() // 调用异常，回滚事务
				err = fmt.Errorf("%v", p)
			} else if lastError != nil {
				_ = own.Rollback() // 执行失败，回滚事务
			} else {
				err = own.Commit() // 执行成功，提交事务
			}
		}()
	} else {
		// 已经在事务中，不需要额外的提交/回滚
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("%v", p)
			}
		}()
	}

	// 分批处理
	start := time.Now()
	for i := 0; i < len(values); i += batchSize {
		batch := values[i:min(i+batchSize, len(values))]

		// 批量插入数据
		res, err := own.insertMany(schema, ignore, batch)
		if err != nil {
			lastError = err
			return res, err
		}

		// 统计插入信息
		if id, err := res.LastInsertId(); err == nil && id != 0 {
			lastInsertID = id
		}
		if rows, err := res.RowsAffected(); err == nil {
			totalRows += rows
		}
	}

	return result.NewBatchResult(totalRows, lastInsertID, time.Since(start), ""), nil
}

// -------------------------------------------------------------------------------------------------------------------------------------------
// UPDATE 更新数据
// -------------------------------------------------------------------------------------------------------------------------------------------

/*
UPDATE 更新数据
  - values: 支持格式 map[[string]]any、struct、*struct
  - 注意1：若传入结构体且没有手动设置 WHERE 子句，则内部使用主键自动构建 WHERE 子句，此时，数据模型必须定义主键字段，且模型实例中必须设置主键字段的值
  - 注意2：map[[string]]any 更新不支持钩子函数，也不自动构建 WHERE 子句，请自行调用 WHERE 方法
*/
func (own *Session) UPDATE(values any) (result.Result, error) {
	if err := own.ensureNoJoin(); err != nil {
		return nil, err
	}

	if err := own.ensureNoPreload(); err != nil {
		return nil, err
	}

	if values == nil {
		return nil, ErrUpdateValuesNil
	}

	if mapValues, ok := values.(map[string]any); ok {
		return own.updateByMap(mapValues)
	}

	return own.updateByStruct(values)
}

// updateByMap 根据 map 映射更新数据
func (own *Session) updateByMap(values map[string]any) (result.Result, error) {
	defer own.statement.Reset()

	if len(values) == 0 {
		return nil, ErrUpdateMapEmpty
	}

	if _, err := own.GetSchema(); err != nil {
		return nil, err
	}

	// 更新数据必须确保有 WHERE 条件，否则直接全表操作（高风险操作）
	if !own.statement.Has(statement.NAME_WHERE) {
		return nil, ErrMissingWhere
	}

	// 添加 UPDATE 子句
	own.statement.AddClause(statement.UPDATE(own.tableSQL)).AddClause(statement.SET(values))

	// 构建并执行 UPDATE 语句
	return own.buildAndExecContext(true, "", statement.NAME_UPDATE, statement.NAME_SET, statement.NAME_WHERE)
}

// updateByStruct 根据主键更新结构体中的非零字段
//
// 默认跳过零值字段；需要把字段显式置零时用 OVERWRITE()，
// 只更新指定列时用 UPDATE_COLUMNS(...)，也可以改用 UPDATE(map[string]any)。
func (own *Session) updateByStruct(value any) (res result.Result, err error) {
	defer own.statement.Reset()

	// 标志只对当前语句生效，提前取走避免泄漏到下一条语句
	overwrite := own.overwrite
	own.overwrite = false

	updateColumns := own.updateColumns
	own.updateColumns = nil

	if _, err := own.GetSchema(); err != nil {
		return nil, err
	}

	val, err := own.schema.ValidateStruct(value)
	if err != nil {
		return nil, err
	}

	if err = own.CallMethod(_HOOK_BEFORE_UPDATE, value); err != nil {
		return nil, err
	}

	setValues := make(map[string]any, 0)
	if updateColumns != nil {
		if len(updateColumns) == 0 {
			return nil, ErrUpdateColumnsEmpty
		}

		// 白名单列显式写入，零值也写回；列名非法时在提取阶段报错
		setValues, err = own.schema.ExtractUpdateNamesAndValuesOf(val, updateColumns)
		if err != nil {
			return nil, err
		}
	} else {
		setValues = own.schema.ExtractUpdateNamesAndValues(val, overwrite)
	}

	if len(setValues) == 0 {
		return nil, ErrUpdateStructEmpty
	}

	// 没有主键值时仍允许使用手动 WHERE，两者都没有则报错
	if err = own.ensureWhereClause(own.schema.ExtractNonZeroPrimaryKeyValues(val)); err != nil {
		return nil, err
	}

	own.statement.AddClause(statement.UPDATE(own.tableSQL)).AddClause(statement.SET(setValues))
	if res, err = own.buildAndExecContext(true, "", statement.NAME_UPDATE, statement.NAME_SET, statement.NAME_WHERE); err != nil {
		return res, err
	}

	return res, own.CallMethod(_HOOK_AFTER_UPDATE, value)
}

// -------------------------------------------------------------------------------------------------------------------------------------------
// DELETE 删除数据
// -------------------------------------------------------------------------------------------------------------------------------------------

/*
DELETE 删除数据
  - value: 支持格式 map[[string]]any、struct、*struct
  - 注意：若使用 struct 或 *struct 参数时，若没有 WHERE 条件，则主动使用主键作为 WHERE 条件，若主键均为零值，则返回错误
*/
func (own *Session) DELETE(value ...any) (result.Result, error) {
	if err := own.ensureNoJoin(); err != nil {
		return nil, err
	}

	if err := own.ensureNoPreload(); err != nil {
		return nil, err
	}

	if len(value) == 0 {
		return own.deleteByWhere()
	}

	return own.deleteByStruct(value[0])
}

func (own *Session) deleteByWhere() (res result.Result, err error) {
	defer own.statement.Reset()

	if _, err := own.GetSchema(); err != nil {
		return nil, err
	}

	// 删除数据必须确保有 WHERE 条件，否则会直接删表（高风险操作）
	if !own.statement.Has(statement.NAME_WHERE) {
		return nil, ErrMissingWhere
	}

	// 添加 DELETE 子句
	own.statement.AddClause(statement.DELETE(own.tableSQL))

	// 构建并执行 DELETE 语句
	return own.buildAndExecContext(true, "", statement.NAME_DELETE, statement.NAME_WHERE)
}

func (own *Session) deleteByStruct(value any) (res result.Result, err error) {
	defer own.statement.Reset()

	// 检查 schema 是否存在
	if _, err := own.GetSchema(); err != nil {
		return nil, err
	}

	// 校验数据
	val, err := own.schema.ValidateStruct(value)
	if err != nil {
		return nil, err
	}

	// 调用前置钩子
	if err := own.CallMethod(_HOOK_BEFORE_DELETE, value); err != nil {
		return nil, err
	}

	// 提取主键字段用于构建 WHERE 条件
	if err = own.ensureWhereClause(own.schema.ExtractNonZeroPrimaryKeyValues(val)); err != nil {
		return nil, err
	}

	// 添加 DELETE 子句
	own.statement.AddClause(statement.DELETE(own.tableSQL))

	// 构建并执行 DELETE 语句
	if res, err = own.buildAndExecContext(true, "", statement.NAME_DELETE, statement.NAME_WHERE); err != nil {
		return
	}

	// 调用后置钩子
	return res, own.CallMethod(_HOOK_AFTER_DELETE, value)
}

// ensureWhereClause 如果没有 WHERE 子句，则添加主键条件作为 WHERE 子句
//
// primaryKeys 按列顺序排列，保证同一条更新/删除语句生成稳定的 SQL 文本，
// 预编译缓存才能跨调用命中。
func (own *Session) ensureWhereClause(primaryKeys []schema.FieldValue) error {
	// 已添加 WHERE 子句
	if own.statement.Has(statement.NAME_WHERE) {
		return nil
	}

	// 没有主键值：区分“模型没有主键”与“主键值为零值”
	if len(primaryKeys) == 0 {
		if len(own.schema.PrimaryKeyFields) == 0 {
			return ErrTableNoPK
		}

		return ErrTablePKValuesZero
	}

	// 使用主键构建 WHERE 条件
	cond := where.RAW("1=1")
	for _, primaryKey := range primaryKeys {
		cond = cond.AND(where.EQ(primaryKey.Name, primaryKey.Value))
	}

	// 添加 WHERE 子句
	own.statement.AddClause(statement.WHERE(cond))
	return nil
}

// buildAndExecContext 编译 SQL 语句，并执执行
func (own *Session) buildAndExecContext(prepare bool, stmtKey string, names ...statement.Name) (result.Result, error) {
	// 构建 SQL 语句
	query, args, err := own.statement.BuildSQL(own.dialect, names...)
	own.statement.Reset() // 无论是否构建成功，都需要重置
	if err != nil {
		return nil, err
	}

	// 不缓存预编译语句
	if !prepare {
		ctx, cancel := own.execContext()
		defer cancel()

		// 执行 SQL 语句，并返回结果
		start := time.Now()
		res, err := own.ExecContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}

		return result.NewResult(res, time.Since(start), query, args...), nil
	}

	// 未指定缓存键时按 SQL 复用查询语句缓存
	if stmtKey == "" {
		ctx, cancel := own.execContext()
		defer cancel()

		res, cost, err := own.execPrepared(ctx, query, args...)
		if err != nil {
			return nil, err
		}

		return result.NewResult(res, cost, query, args...), nil
	}

	// 获取预编译语句（内部自动缓存）
	stmt, err := own.prepare(stmtKey, query)
	if err != nil {
		return nil, err
	}

	// 使用会话上下文（未设置超时时不额外创建计时器）
	ctx, cancel := own.execContext()
	defer cancel()

	// 执行 SQL 语句，并返回结果
	start := time.Now()
	res, err := stmt.ExecContext(ctx, own.id, false, false, args...)
	cost := time.Since(start)
	own.logQuery(cost, stmt.query, args...)
	if err != nil {
		return nil, err
	}

	return result.NewResult(res, cost, stmt.query, args...), nil
}

/*
SELECT 查询一条或多条数据
  - values: 必须是指针，支持 *Struct 或 *[]Struct 或 *[]*Struct
*/
func (own *Session) SELECT(values any) (res result.Result, err error) {
	defer own.statement.Reset()

	if values == nil {
		return nil, ErrSelectValueNil
	}

	// 校验数据模型，避免钩子与列信息读取空模型
	if _, err := own.GetSchema(); err != nil {
		return nil, err
	}

	// 调用 CallBeforeSelect 方法
	if err := own.CallMethod(_HOOK_BEFORE_SELECT, nil); err != nil {
		return nil, err
	}

	// 检查 values 是否为指针类型：*Struct 或 *[]Struct 或 *[]*Struct
	originalValue := reflect.ValueOf(values)
	if originalValue.Kind() != reflect.Ptr {
		return nil, ErrSelectValuesNotPointer
	}

	// 获取 dest 的地址
	destValue := reflect.Indirect(originalValue)
	if !destValue.IsValid() {
		return nil, ErrSelectDestinationValueInvalid
	}

	// 处理不同数据类型
	destValueKind := destValue.Kind()
	switch destValueKind {
	// []Struct 或 []*Struct
	case reflect.Slice:
		elemType := destValue.Type().Elem()
		elemKind := elemType.Kind()
		switch elemKind {
		// Struct
		case reflect.Struct:
			return own.selectMore(destValue, elemType, elemKind)

		// *Struct
		case reflect.Ptr:
			if elemType.Elem().Kind() != reflect.Struct {
				return nil, ErrSelectValuesNotPointer
			}
			return own.selectMore(destValue, elemType.Elem(), elemKind)

		// 错误类型
		default:
			return nil, ErrSelectValuesNotPointer
		}

	// Struct
	case reflect.Struct:
		res, _, err := own.selectFirst(originalValue, destValue.Type())
		return res, err

	// 错误类型
	default:
		return nil, ErrSelectValuesNotPointer
	}
}

// selectFirst 直接扫描 SELECT 语句查询到的第一条数据，第二个返回值表示是否命中数据
//
// 单行查询不需要中转切片：直接构建 SELECT ... LIMIT 1 并扫描到目标结构体，
// 省掉一次切片分配与整结构体拷贝。
func (own *Session) selectFirst(destValue reflect.Value, destType reflect.Type) (res result.Result, found bool, err error) {
	elem := destValue.Elem()
	if !elem.CanSet() {
		return nil, false, ErrSelectDestinationValueNotSettable
	}

	// 单行查询不预分配结果集，清掉可能残留的 LIMIT 值
	own.limit = 0
	own.statement.AddClause(statement.LIMIT(1))

	// 构建 SELECT 语句
	query, args, sch, requested, requestedNames, err := own.buildSelect()
	if err != nil {
		return nil, false, err
	}

	// 使用会话上下文（未设置超时时不额外创建计时器）
	ctx, cancel := own.execContext()
	defer cancel()

	// 执行 SQL 语句
	rows, cost, err := own.queryPrepared(ctx, query, args...)
	res = result.NewSelectResult(cost, query, args...)
	if err != nil {
		return res, false, err
	}
	defer func() {
		if errClose := rows.Close(); errClose != nil && err == nil {
			err = errClose
		}
	}()

	// 计算返回列对应的模型字段索引：未指定列时直接复用预计算的索引，避免额外的列名匹配
	scanFields := sch.ColumnIndexesInModel
	if requested {
		if scanFields, err = scanFieldsOf(sch, rows); err != nil {
			return res, false, err
		}
	}

	// 没有数据：保持与切片路径一致，返回结果对象 + found=false
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return res, false, fmt.Errorf("wxorm: failed to iterative rows when selecting: %w", err)
		}

		// 未命中数据时不加载关联，同时清空状态避免泄漏到下一次查询
		own.resetPreloads()
		return res, false, nil
	}

	// 先归零再扫描：未查询的列保持零值，避免残留调用方预置的旧数据
	elem.Set(reflect.Zero(destType))

	scanArgs := make([]any, len(scanFields))
	for i, idx := range scanFields {
		scanArgs[i] = elem.Field(idx).Addr().Interface()
	}

	// 大数字段先扫到文本再解析回字段，普通模型上 bigScan 为 nil
	bigScan := newBigScanner(sch, scanFields)
	if bigScan != nil {
		bigScan.Bind(scanArgs)
	}

	if err = rows.Scan(scanArgs...); err != nil {
		return res, false, fmt.Errorf("wxorm: failed to scann rows when selecting: %w", err)
	}

	if bigScan != nil {
		if err = bigScan.Assign(elem); err != nil {
			return res, false, err
		}
	}

	// 先释放结果集，再执行钩子与预加载：两者都可能发起新的查询，
	// rows 未关闭会一直占用连接——单连接池下直接自锁，默认池下一条查询要占两条连接
	if err = rows.Close(); err != nil {
		return res, false, err
	}

	// 调用 CallAfterSelect 方法
	if reflect.PointerTo(destType).Implements(hookAfterSelectType) {
		if err = own.CallMethod(_HOOK_AFTER_SELECT, elem.Addr().Interface()); err != nil {
			return res, false, fmt.Errorf("wxorm: failed to hook after selecting: %w", err)
		}
	}

	// 单行结果同样可以预加载关联
	if err = own.loadPreloads([]reflect.Value{elem}, requestedNames); err != nil {
		return res, false, err
	}

	return res, true, nil
}

/*
FindFirst 查询第一条数据

未查询到数据时返回 ErrRecordNotFound，可配合 errors.Is 判断；
需要区分「没有数据」与「查询失败」时优先使用本方法，而不是 SELECT。
*/
func (own *Session) FindFirst(dest any) (result.Result, error) {
	defer own.statement.Reset()

	if dest == nil {
		return nil, ErrSelectValueNil
	}

	if _, err := own.GetSchema(); err != nil {
		return nil, err
	}

	originalValue := reflect.ValueOf(dest)
	if originalValue.Kind() != reflect.Ptr || originalValue.IsNil() {
		return nil, ErrSelectValuesNotPointer
	}

	destValue := reflect.Indirect(originalValue)
	if !destValue.IsValid() || destValue.Kind() != reflect.Struct {
		return nil, ErrSelectDestinationValueInvalid
	}

	if err := own.CallMethod(_HOOK_BEFORE_SELECT, nil); err != nil {
		return nil, err
	}

	res, found, err := own.selectFirst(originalValue, destValue.Type())
	if err != nil {
		return res, err
	}

	if !found {
		return res, ErrRecordNotFound
	}

	return res, nil
}

/*
Exist 判断是否存在满足条件的记录

只取 1 条记录，不会像 COUNT 那样统计全部行；存在返回 true。
*/
func (own *Session) Exist() (bool, error) {
	defer own.statement.Reset()

	// 不返回模型，PRELOAD 没有承载对象，直接丢弃声明
	own.resetPreloads()

	// 取出并清空 JOIN 状态
	joins, err := own.takeJoins()
	if err != nil {
		return false, err
	}

	if _, err := own.GetSchema(); err != nil {
		return false, err
	}

	// 写入 SELECT 1 FROM ... LIMIT 1
	own.statement.AddClause(statement.EXISTS(own.tableSQL)).AddClause(statement.LIMIT(1))
	if len(joins) > 0 {
		own.statement.AddClause(statement.JOIN(joins...))
	}

	query, args, err := own.statement.BuildSQL(own.dialect, statement.NAME_EXISTS, statement.NAME_JOIN, statement.NAME_WHERE, statement.NAME_LIMIT)
	if err != nil {
		return false, err
	}

	ctx, cancel := own.execContext()
	defer cancel()

	rows, _, err := own.queryPrepared(ctx, query, args...)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		return true, rows.Err()
	}

	return false, rows.Err()
}

// buildSelect 构建 SELECT 语句，返回 SQL、参数、表结构、是否指定了查询列与列名列表
func (own *Session) buildSelect() (query string, args []any, sch *schema.Schema, requested bool, requestedNames []string, err error) {
	// JOIN 与 columns/limit 一样「取走即归零」：
	// 校验错误在这里暴露，同时清空状态，避免泄漏到下一次查询
	joins, err := own.takeJoins()
	if err != nil {
		return "", nil, nil, false, nil, err
	}

	// 获取数据表信息
	if sch, err = own.GetSchema(); err != nil {
		return "", nil, nil, false, nil, err
	}

	// 单独使用 OFFSET 在 MySQL 中不合法，必须配合 LIMIT
	if own.statement.Has(statement.NAME_OFFSET) && !own.statement.Has(statement.NAME_LIMIT) {
		return "", nil, nil, false, nil, ErrOffsetRequiresLimit
	}

	// 未指定列时返回全部列；指定列时只查询这些列，并按返回的列名映射到模型字段
	requestedNames = own.columns
	requested = len(requestedNames) > 0
	own.columns = nil

	// 联表查询：显式投影主表列与联表列，主表列用物理表名限定避免重名歧义
	if len(joins) > 0 {
		selectColumns, err := buildJoinSelectColumns(own, sch, joins, requestedNames)
		if err != nil {
			return "", nil, nil, false, nil, err
		}

		own.statement.AddClause(statement.SELECT_COLUMNS(selectColumns...))
		if !own.statement.Has(statement.NAME_FROM) {
			own.statement.AddClause(statement.FROM(own.tableSQL))
		}

		// 联表结果必须按返回列名逐个映射，不能走 ColumnIndexesInModel 快路径
		requested = true
	} else {
		columns := sch.ColumnNames
		if requested {
			// 先在本地校验列名，避免把未知列发给数据库后才拿到方言错误
			for _, name := range requestedNames {
				if _, ok := sch.ColumnIndexesByName[normalizeColumnName(name)]; !ok {
					return "", nil, nil, false, nil, fmt.Errorf("%w: %q", ErrSelectColumnNotFound, name)
				}
			}

			columns = requestedNames
		}

		// 写入 SELECT 子句和 FROM 子句
		if own.statement.Has(statement.NAME_FROM) {
			own.statement.AddClause(statement.SELECT(own.statement.GetTable(), columns...))
		} else {
			own.statement.AddClause(statement.SELECT(own.tableSQL, columns...))
			own.statement.AddClause(statement.FROM(own.tableSQL))
		}
	}

	// 写入 JOIN 子句：位置固定在 FROM 之后、WHERE 之前
	if len(joins) > 0 {
		own.statement.AddClause(statement.JOIN(joins...))
	}

	// 构建完整的 SQL 语句
	query, args, err = own.statement.BuildSQL(own.dialect,
		statement.NAME_RECURSIVE,
		statement.NAME_SELECT,
		statement.NAME_FROM,
		statement.NAME_JOIN,
		statement.NAME_WHERE,
		statement.NAME_GROUP,
		statement.NAME_HAVING,
		statement.NAME_ORDER,
		statement.NAME_LIMIT,
		statement.NAME_OFFSET,
	)
	if err != nil {
		return "", nil, nil, false, nil, err
	}

	return query, args, sch, requested, requestedNames, nil
}

// selectMore 获取 SELECT 语句查询到的全部数据
func (own *Session) selectMore(destValue reflect.Value, destType reflect.Type, destKind reflect.Kind) (res result.Result, err error) {
	// 检查目标是否可设置
	if !destValue.CanSet() {
		return nil, ErrSelectDestinationSliceNotSettable
	}

	// LIMIT 只用于本条语句的结果集预分配，取走后归零，避免影响下一次查询
	limit := own.limit
	own.limit = 0

	// 构建 SELECT 语句
	query, args, sch, requested, requestedNames, err := own.buildSelect()
	if err != nil {
		return nil, err
	}

	// 使用会话上下文（未设置超时时不额外创建计时器）
	ctx, cancel := own.execContext()
	defer cancel()

	// 执行 SQL 语句
	rows, cost, err := own.queryPrepared(ctx, query, args...)
	res = result.NewSelectResult(cost, query, args...)
	if err != nil {
		return res, err
	}
	defer func() {
		if errClose := rows.Close(); errClose != nil && err == nil {
			err = errClose
		}
	}()

	// 计算返回列对应的模型字段索引：未指定列时直接复用预计算的索引，避免额外的列名匹配
	scanFields := sch.ColumnIndexesInModel
	if requested {
		if scanFields, err = scanFieldsOf(sch, rows); err != nil {
			return res, err
		}
	}

	// 复用扫描参数切片，避免每行重新分配
	scanArgs := make([]any, len(scanFields))

	// 大数字段的扫描目标固定，只需绑定一次；普通模型上 bigScan 为 nil
	bigScan := newBigScanner(sch, scanFields)
	if bigScan != nil {
		bigScan.Bind(scanArgs)
	}

	// 目标类型未实现 CallAfterSelect 时跳过每行的接口装箱
	hasAfterSelect := reflect.PointerTo(destType).Implements(hookAfterSelectType)

	// 按 LIMIT 预估结果集容量，减少逐行扩容
	own.reserveSelect(destValue, limit)

	// 收集扫描后的父结果，供 Preload 分配关联对象
	parents := make([]reflect.Value, 0, 16)

	// 从返回值中提取并封装数据
	for rows.Next() {
		// 结构体切片直接扩容后扫描到新元素，避免临时实例与整结构体拷贝
		if destKind == reflect.Struct {
			destValue.Set(reflect.Append(destValue, reflect.Zero(destType)))
			value := destValue.Index(destValue.Len() - 1)

			// 准备扫描参数
			for i, idx := range scanFields {
				if bigScan.Covers(i) {
					continue
				}

				scanArgs[i] = value.Field(idx).Addr().Interface()
			}

			// 扫描行数据
			if err := rows.Scan(scanArgs...); err != nil {
				return res, fmt.Errorf("wxorm: failed to scann rows when selecting: %w", err)
			}

			if bigScan != nil {
				if err := bigScan.Assign(value); err != nil {
					return res, err
				}
			}

			parents = append(parents, value)
			continue
		}

		// 指针切片：每行仍需独立实例，扫描后追加指针
		value := reflect.New(destType).Elem()

		// 准备扫描参数
		for i, idx := range scanFields {
			if bigScan.Covers(i) {
				continue
			}

			scanArgs[i] = value.Field(idx).Addr().Interface()
		}

		// 扫描行数据
		if err := rows.Scan(scanArgs...); err != nil {
			return res, fmt.Errorf("wxorm: failed to scann rows when selecting: %w", err)
		}

		if bigScan != nil {
			if err := bigScan.Assign(value); err != nil {
				return res, err
			}
		}

		destValue.Set(reflect.Append(destValue, value.Addr()))
		parents = append(parents, value)
	}

	// 检查行迭代过程中的错误
	if err := rows.Err(); err != nil {
		return res, fmt.Errorf("wxorm: failed to iterative rows when selecting: %w", err)
	}

	// 先释放结果集，再执行钩子与预加载：两者都可能发起新的查询
	if err = rows.Close(); err != nil {
		return res, err
	}

	// 钩子在结果集释放后统一执行：逐行执行会与后续查询争抢连接
	if hasAfterSelect {
		for _, parent := range parents {
			if err = own.CallMethod(_HOOK_AFTER_SELECT, parent.Addr().Interface()); err != nil {
				return res, fmt.Errorf("wxorm: failed to hook after selecting: %w", err)
			}
		}
	}

	// 扫描完成后加载关联对象；任一子查询失败则整体失败
	if err := own.loadPreloads(parents, requestedNames); err != nil {
		return res, err
	}

	return res, nil
}

const (
	// defaultSelectCapacity 无 LIMIT 时结果集的起步容量
	defaultSelectCapacity = 16
	// maxSelectCapacity 按 LIMIT 预分配的容量上限，避免超大 LIMIT 直接占用大量内存
	maxSelectCapacity = 1024
)

// reserveSelect 按 LIMIT 预估结果集容量，并保留调用方已有的元素
func (own *Session) reserveSelect(dest reflect.Value, limit int) {
	capacity := limit
	if capacity <= 0 {
		capacity = defaultSelectCapacity
	} else if capacity > maxSelectCapacity {
		capacity = maxSelectCapacity
	}

	if dest.Cap() >= capacity {
		return
	}

	// 只分配容量，不改变长度，避免丢弃调用方传入的已有元素
	grown := reflect.MakeSlice(dest.Type(), dest.Len(), capacity)
	reflect.Copy(grown, dest)
	dest.Set(grown)
}

// scanFieldsOf 将查询返回的列名映射为模型字段索引
//
// 支持 COLUMNS("id", "name") 的列名，也支持 "表名.列名" 与 SELECT 中带修饰的形式；
// 联表查询的结果列（如 author_name）同样在此映射。
// 出现模型里不存在的列时返回错误，避免字段静默错位。
func scanFieldsOf(sch *schema.Schema, rows *sql.Rows) ([]int, error) {
	names, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("wxorm: failed to read columns when selecting: %w", err)
	}

	fields := make([]int, len(names))
	for i, name := range names {
		normalized := normalizeColumnName(name)
		idx, ok := sch.ColumnIndexesByName[normalized]
		if !ok {
			idx, ok = sch.JoinColumnIndexesByName[normalized]
		}
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrSelectColumnNotFound, name)
		}

		fields[i] = idx
	}

	return fields, nil
}

// normalizeColumnName 去掉返回列名的表名前缀与首尾空白
func normalizeColumnName(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}

	return strings.TrimSpace(name)
}

// COUNT 根据条件统计数据
func (own *Session) COUNT(column ...string) (int64, result.Result, error) {
	defer own.statement.Reset()

	// 带分组的统计不是单个数值，静默忽略 GROUP/HAVING 会返回错误的总数
	if own.statement.Has(statement.NAME_GROUP) || own.statement.Has(statement.NAME_HAVING) {
		return 0, nil, ErrCountWithGroupUnsupported
	}

	// 不返回模型，PRELOAD 没有承载对象，直接丢弃声明
	own.resetPreloads()

	// 取出并清空 JOIN 状态
	joins, err := own.takeJoins()
	if err != nil {
		return 0, nil, err
	}

	// 获取数据表信息
	sch, err := own.GetSchema()
	if err != nil {
		return 0, nil, err
	}

	// 联表统计会放大行数，必须对主表主键去重；复合主键无法用 COUNT(DISTINCT) 表达
	if len(joins) > 0 {
		if len(column) > 0 {
			return 0, nil, ErrJoinCountColumnUnsupported
		}

		if len(sch.PrimaryKeyNames) != 1 {
			return 0, nil, ErrJoinRequiresSinglePK
		}

		own.statement.AddClause(statement.COUNT_DISTINCT(own.table + "." + sch.PrimaryKeyNames[0]))
		if !own.statement.Has(statement.NAME_FROM) {
			own.statement.AddClause(statement.FROM(own.tableSQL))
		}
		own.statement.AddClause(statement.JOIN(joins...))
	} else {
		// 写入 SELECT 语句
		own.statement.AddClause(statement.COUNT(column...))

		// 写入 FROM 子句
		if ok := own.statement.Has(statement.NAME_FROM); !ok {
			own.statement.AddClause(statement.FROM(own.tableSQL))
		}
	}

	// 构建 SQL 语句
	query, args, err := own.statement.BuildSQL(own.dialect,
		statement.NAME_SELECT,
		statement.NAME_FROM,
		statement.NAME_JOIN,
		statement.NAME_WHERE,
	)
	if err != nil {
		return 0, nil, err
	}

	// 使用会话上下文（未设置超时时不额外创建计时器）
	ctx, cancel := own.execContext()
	defer cancel()

	// 执行 SQL 语句
	row, cost := own.queryRowPrepared(ctx, query, args...)
	res := result.NewSelectResult(cost, query, args...)

	// 读取统计结果
	var count int64
	return count, res, row.Scan(&count)
}

// -------------------------------------------------------------------------------------------------------------------------------------------
// SQL 子句
// -------------------------------------------------------------------------------------------------------------------------------------------

// // RECURSIVE 写入 WITH RECURSIVE 递归子句
// func (own *Session) RECURSIVE(cteTable, initSelectSql, cteSelectSql string, args ...any) *Session {
// 	own.statement.AddClause(clause.RECURSIVE(cteTable, initSelectSql, cteSelectSql, args...))
// 	return own
// }

// DUPLICATE 执行 INSERT 语句时，主键或唯一索引冲突时的更新操作（仅 MySQL 系）
//
// columns 是「冲突时需要更新的列」，冲突目标由数据库根据唯一索引隐式确定。
// PostgreSQL 必须显式指定冲突目标，调用本方法会在执行时返回
// clause.ErrUnsupportedFeature，跨方言场景请改用 UPSERT。
func (own *Session) DUPLICATE(columns ...string) *Session {
	own.statement.AddClause(statement.DUPLICATE(columns...))
	return own
}

// UPSERT 执行 INSERT 语句时，按模型主键冲突更新指定列
//
// 冲突目标固定为模型主键，因此同一份调用在 MySQL 与 PostgreSQL 下语义一致：
// MySQL 生成 ON DUPLICATE KEY UPDATE，PostgreSQL 生成 ON CONFLICT (主键) DO UPDATE SET。
// 模型无主键且方言需要显式冲突目标时，执行阶段返回 statement.ErrConflictTargetRequired。
func (own *Session) UPSERT(update ...string) *Session {
	if _, ok := own.dialect.(clause.ConflictUpserter); ok {
		var conflict []string
		if sch, err := own.GetSchema(); err == nil {
			conflict = sch.PrimaryKeyNames
		}

		own.statement.AddClause(statement.UPSERT(conflict, update...))
		return own
	}

	own.statement.AddClause(statement.DUPLICATE(update...))
	return own
}

// OVERWRITE 让 UPDATE(struct) 把零值字段一并写回
//
// 默认只更新非零字段，与「读取-修改-写回」的使用方式一致；
// 本方法表示「结构体就是完整的一行」，未赋值的列会被写成零值，
// 因此容易误清数据——只想改某些列时用 UPDATE_COLUMNS 或 UPDATE(map[string]any)。
// 标志只对紧接着的一条 UPDATE 生效。
func (own *Session) OVERWRITE() *Session {
	own.overwrite = true
	return own
}

// UPDATE_COLUMNS 限定 UPDATE(struct) 只写回指定列
//
// 列名必须是模型的可更新列（非主键、非自动插入列），否则返回 schema.ErrUnknownUpdateColumn。
// 与 OVERWRITE 不同，白名单中的列无论是否为零值都会写回，未列出的列保持数据库中的原值。
// 白名单只对紧接着的一条 UPDATE 生效。
func (own *Session) UPDATE_COLUMNS(columns ...string) *Session {
	own.updateColumns = append(make([]string, 0, len(columns)), columns...)
	return own
}

// FROM 写入 FROM 子句
func (own *Session) FROM(table string) *Session {
	own.statement.SetTable(table)
	own.statement.AddClause(statement.FROM(table))
	return own
}

// WHERE 写入 WHERE 子句
func (own *Session) WHERE(condition where.Condition) *Session {
	own.statement.AddClause(statement.WHERE(condition))
	return own
}

// GROUP 写入 GROUP BY 子句
func (own *Session) GROUP(columns ...string) *Session {
	own.statement.AddClause(statement.GROUP(columns...))
	return own
}

// HAVING 写入 HAVING 子句
func (own *Session) HAVING(condition where.Condition) *Session {
	own.statement.AddClause(statement.HAVING(condition))
	return own
}

// ORDER 写入 ORDER BY 子句
func (own *Session) ORDER(columns ...string) *Session {
	own.statement.AddClause(statement.ORDER(columns...))
	return own
}

/*
COLUMNS 指定本次查询返回的列

不指定时返回模型映射的全部列；指定后只查询这些列，未查询的字段保持零值。
列名需与模型列名一致，查询结果会按返回的列名映射回结构体字段。
*/
func (own *Session) COLUMNS(columns ...string) *Session {
	if len(columns) == 0 {
		own.columns = nil
		return own
	}

	own.columns = append(own.columns[:0], columns...)
	return own
}

// LIMIT 写入 LIMIT 子句
//
// 0 是有效值（生成 LIMIT 0，不返回任何行）；负数在构建 SQL 时返回
// statement.ErrLimitNegative，避免分页参数算错时静默查全表。
func (own *Session) LIMIT(limit int) *Session {
	own.limit = limit
	own.statement.AddClause(statement.LIMIT(limit))
	return own
}

// OFFSET 写入 OFFSET 子句
func (own *Session) OFFSET(offset int) *Session {
	own.statement.AddClause(statement.OFFSET(offset))
	return own
}
