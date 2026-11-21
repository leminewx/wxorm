package session

import (
	"fmt"
	"testing"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/harness/examples"
	"gitee.com/leminewx/wxorm/schema"
)

// TestFindBatchReturningColumns 验证批量回填只在支持 RETURNING 的方言上启用
func TestFindBatchReturningColumns(t *testing.T) {
	cases := []struct {
		driver   string
		expected bool
	}{
		// MySQL 无法逐行取回生成的主键
		{driver: "mysql", expected: false},
		// PostgreSQL 的 RETURNING 直接返回每行主键
		{driver: "postgres", expected: true},
		// SQLite 3.35+ 支持 RETURNING
		{driver: "sqlite", expected: true},
	}

	for _, tc := range cases {
		t.Run(tc.driver, func(t *testing.T) {
			dial := dialect.Get(tc.driver)
			sess := NewSession(1, examples.DBName, nil, dial, nil)

			sch, err := schema.Parse(dial, &examples.Group{})
			if err != nil {
				t.Fatalf("解析模型失败: %v", err)
			}

			columns := sess.findBatchReturningColumns(sch)
			if got := len(columns) > 0; got != tc.expected {
				t.Fatalf("批量回填能力 = %v, want %v（列: %v）", got, tc.expected, columns)
			}

			if tc.expected && columns[0] != sch.AutoPrimaryKeyName {
				t.Fatalf("回填列 = %q, want %q", columns[0], sch.AutoPrimaryKeyName)
			}
		})
	}

	// 模型没有自增主键时不应回填
	dial := dialect.Get("postgres")
	sess := NewSession(1, examples.DBName, nil, dial, nil)
	sch, err := schema.Parse(dial, &noAutoPrimaryModel{})
	if err != nil {
		t.Fatalf("解析模型失败: %v", err)
	}

	if columns := sess.findBatchReturningColumns(sch); len(columns) != 0 {
		t.Fatalf("非自增主键不应回填，实际列: %v", columns)
	}
}

// noAutoPrimaryModel 主键由调用方指定，不是自增列
type noAutoPrimaryModel struct {
	Code string `orm:"column(code) type(varchar(32)) pk"`
	Name string `wxorm:"column(name) type(varchar(50))"`
}

// GetTableName 返回测试用表名
func (own *noAutoPrimaryModel) GetTableName() string {
	return "no_auto_primary"
}

// TestInsertBatchSize 验证单批行数按方言占位符上限与列数计算
func TestInsertBatchSize(t *testing.T) {
	cases := []struct {
		driver  string
		columns int
		want    int
	}{
		{driver: "mysql", columns: 10, want: 1000},
		{driver: "postgres", columns: 10, want: 1000},
		{driver: "sqlite", columns: 10, want: 1000},
		// 宽表按占位符上限收缩
		{driver: "mysql", columns: 100, want: 600},
		{driver: "sqlite", columns: 100, want: 300},
		// 异常列数不能导致除零或零行批次
		{driver: "mysql", columns: 0, want: 1},
		{driver: "mysql", columns: 100000, want: 1},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s-%d", tc.driver, tc.columns), func(t *testing.T) {
			dial := dialect.Get(tc.driver)
			sess := NewSession(1, examples.DBName, nil, dial, nil)

			if got := sess.insertBatchSize(tc.columns); got != tc.want {
				t.Fatalf("insertBatchSize(%d) = %d, want %d", tc.columns, got, tc.want)
			}
		})
	}
}

// TestInsertLargeBatch 验证超过单批上限的数据会自动拆分成多条语句
func TestInsertLargeBatch(t *testing.T) {
	sess := setupGroupSession(t)
	defer teardownSession(sess)

	const total = 1200
	values := make([]any, total)
	for i := range values {
		values[i] = &examples.Group{
			Name:      fmt.Sprintf("group-%04d", i),
			CreatedBy: "batch",
		}
	}

	res, err := sess.INSERT(false, values...)
	if err != nil {
		t.Fatalf("大批量插入失败: %v", err)
	}

	if rows, err := res.RowsAffected(); err != nil || rows != total {
		t.Fatalf("受影响行数 = %d, err = %v, want %d", rows, err, total)
	}

	count, _, err := sess.COUNT()
	if err != nil || count != total {
		t.Fatalf("COUNT = %d, err = %v, want %d", count, err, total)
	}
}

// TestInsertMany_MySQLKeepsPrimaryKeyEmpty 验证 MySQL 批量插入保持不回填主键
func TestInsertMany_MySQLKeepsPrimaryKeyEmpty(t *testing.T) {
	sess := setupGroupSession(t)
	defer teardownSession(sess)

	values := []any{
		&examples.Group{Name: "batch1"},
		&examples.Group{Name: "batch2"},
	}

	res, err := sess.INSERT(false, values...)
	if err != nil {
		t.Fatalf("批量插入失败: %v", err)
	}

	if rows, err := res.RowsAffected(); err != nil || rows != 2 {
		t.Fatalf("受影响行数 = %d, err = %v, want 2", rows, err)
	}

	// MySQL 没有 RETURNING，主键保持零值，调用方需自行按唯一条件回查
	for i, value := range values {
		if id := value.(*examples.Group).ID; id != 0 {
			t.Fatalf("第 %d 行主键不应回填，实际 %d", i, id)
		}
	}
}
