package session

import (
	"database/sql"
	"fmt"
	"reflect"

	"gitee.com/leminewx/wxorm/schema"
)

/*
bigScanner 负责大数字段（big.Int / big.Float）的扫描桥接

数据库返回的是十进制文本，而 database/sql 无法把文本直接写入 big.Int / big.Float
字段（未实现 sql.Scanner），因此这类列先扫描到 sql.NullString，再解析回模型字段。

模型不含大数字段时 newBigScanner 返回 nil，扫描路径保持原有写法，
不会为普通查询引入任何额外分支或分配。
*/

// bigScanColumn 描述一个需要文本桥接的结果列
type bigScanColumn struct {
	position int             // 在 scanArgs / scanFields 中的下标
	field    int             // 模型字段下标
	kind     schema.BigKind  // 桥接种类
	target   *sql.NullString // 复用的扫描目标
}

// bigScanner 复用一组桥接目标，避免每行重新分配
type bigScanner struct {
	columns []bigScanColumn
	covered []bool // 按 scanArgs 下标标记该位置是否由桥接器接管
}

// newBigScanner 按扫描列创建桥接器；模型不含大数字段时返回 nil
func newBigScanner(sch *schema.Schema, scanFields []int) *bigScanner {
	if sch == nil || !sch.HasBigFields {
		return nil
	}

	scanner := &bigScanner{covered: make([]bool, len(scanFields))}
	for position, fieldIndex := range scanFields {
		if fieldIndex < 0 || fieldIndex >= len(sch.BigFieldKinds) {
			continue
		}

		kind := sch.BigFieldKinds[fieldIndex]
		if kind == schema.BIG_NONE {
			continue
		}

		scanner.covered[position] = true
		scanner.columns = append(scanner.columns, bigScanColumn{
			position: position,
			field:    fieldIndex,
			kind:     kind,
			target:   new(sql.NullString),
		})
	}

	if len(scanner.columns) == 0 {
		return nil
	}

	return scanner
}

// Covers 判断某个扫描位置是否由桥接器接管
func (own *bigScanner) Covers(position int) bool {
	return own != nil && position < len(own.covered) && own.covered[position]
}

// Bind 为桥接列写入扫描目标；调用方只需负责其余列
func (own *bigScanner) Bind(scanArgs []any) {
	for i := range own.columns {
		column := &own.columns[i]
		scanArgs[column.position] = column.target
	}
}

// Assign 把扫描到的文本解析回模型字段
func (own *bigScanner) Assign(value reflect.Value) error {
	for i := range own.columns {
		column := &own.columns[i]
		if err := schema.ScanBigValue(value.Field(column.field), column.kind, column.target.String, column.target.Valid); err != nil {
			return fmt.Errorf("wxorm: failed to scan big number column %d: %w", column.position, err)
		}
	}

	return nil
}
