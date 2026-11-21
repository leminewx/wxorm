package integration

import (
	"fmt"
	"sync"
	"testing"

	"gitee.com/leminewx/wxorm"
	"gitee.com/leminewx/wxorm/statement/where"
)

// MigrateV1 迁移前模型：只有 id 与 name
type MigrateV1 struct {
	ID   int64  `orm:"column(id) pk auto"`
	Name string `orm:"column(name)"`
}

// GetTableName 返回迁移测试表名
func (*MigrateV1) GetTableName() string { return tableName("migrate") }

// MigrateV2 迁移后模型：新增 age（非空默认 0）
type MigrateV2 struct {
	ID   int64  `orm:"column(id) pk auto"`
	Name string `orm:"column(name)"`
	Age  int    `orm:"column(age) notnull default(0)"`
}

// GetTableName 返回迁移测试表名
func (*MigrateV2) GetTableName() string { return tableName("migrate") }

// MigrateV3 再次迁移：删除 age
type MigrateV3 struct {
	ID   int64  `orm:"column(id) pk auto"`
	Name string `orm:"column(name)"`
}

// GetTableName 返回迁移测试表名
func (*MigrateV3) GetTableName() string { return tableName("migrate") }

// TestMigrate 验证建表、新增列、删除列与幂等性
func TestMigrate(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		table := tableName("migrate")
		dropTable(t, engine, table)

		// 表不存在时应直接建表
		if err := engine.Migrate(&MigrateV1{}); err != nil {
			t.Fatalf("首次迁移（建表）失败: %v", err)
		}

		sess := engine.NewSession(&MigrateV1{})
		if exists, err := sess.ExistsTable(); err != nil {
			t.Fatalf("查询表存在性失败: %v", err)
		} else if !exists {
			t.Fatal("Migrate 应创建数据表")
		}

		if _, err := sess.INSERT(false,
			&MigrateV1{Name: "m1"},
			&MigrateV1{Name: "m2"},
		); err != nil {
			t.Fatalf("写入迁移前数据失败: %v", err)
		}

		// 新增列：已有行应拿到默认值 0
		if err := engine.Migrate(&MigrateV2{}); err != nil {
			t.Fatalf("新增列迁移失败: %v", err)
		}

		var v2Rows []MigrateV2
		sessV2 := engine.NewSession(&MigrateV2{})
		if _, err := sessV2.ORDER("id").SELECT(&v2Rows); err != nil {
			t.Fatalf("迁移后查询失败: %v", err)
		}

		if len(v2Rows) != 2 || v2Rows[0].Age != 0 || v2Rows[1].Age != 0 {
			t.Fatalf("新增列默认值不符: %+v", v2Rows)
		}

		if _, err := sessV2.INSERT(false, &MigrateV2{Name: "m3", Age: 7}); err != nil {
			t.Fatalf("新增列写入失败: %v", err)
		}

		// 默认迁移不删列：仅报告模型之外的列，避免模型变更静默销毁数据
		if err := engine.Migrate(&MigrateV3{}); err != nil {
			t.Fatalf("默认迁移失败: %v", err)
		}

		if columns := tableColumns(t, engine, &MigrateV3{}); !containsColumn(columns, "age") {
			t.Fatalf("默认迁移不应删除 age 列，当前列: %v", columns)
		}

		// 显式选项才执行 DROP COLUMN：列被移除后仍能按新模型查询
		if err := engine.Migrate(&MigrateV3{}, wxorm.WithDropMissingColumns()); err != nil {
			t.Fatalf("删除列迁移失败: %v", err)
		}

		if columns := tableColumns(t, engine, &MigrateV3{}); containsColumn(columns, "age") {
			t.Fatalf("age 列应已被删除，当前列: %v", columns)
		}

		var v3Rows []MigrateV3
		if _, err := engine.NewSession(&MigrateV3{}).SELECT(&v3Rows); err != nil {
			t.Fatalf("删除列后查询失败: %v", err)
		}

		if len(v3Rows) != 3 {
			t.Fatalf("删除列后行数不符: got %d, want 3", len(v3Rows))
		}

		// 幂等：结构一致时不应报错，也不应产生变更
		if err := engine.Migrate(&MigrateV3{}); err != nil {
			t.Fatalf("重复迁移失败: %v", err)
		}
	})
}

// tableColumns 读取数据表当前的列名
func tableColumns(t testing.TB, engine *wxorm.Engine, model any) []string {
	t.Helper()

	sess := engine.NewSession(model)
	defer func() { _ = sess.Close() }()

	columns, err := sess.GetTableColumns()
	if err != nil {
		t.Fatalf("读取表结构失败: %v", err)
	}

	return columns
}

// containsColumn 判断列名是否存在于列表中
func containsColumn(columns []string, name string) bool {
	for _, column := range columns {
		if column == name {
			return true
		}
	}

	return false
}

// MigrateDiffBase 迁移差异测试：基础列
type MigrateDiffBase struct {
	ID   int64  `orm:"column(id) pk auto"`
	Name string `orm:"column(name)"`
}

// GetTableName 返回迁移差异测试表名
func (*MigrateDiffBase) GetTableName() string { return tableName("migrate_diff") }

// MigrateDiffNext 迁移差异测试：新增带索引的列
type MigrateDiffNext struct {
	ID    int64  `orm:"column(id) pk auto"`
	Name  string `orm:"column(name)"`
	Email string `orm:"column(email) index"`
}

// GetTableName 返回迁移差异测试表名
func (*MigrateDiffNext) GetTableName() string { return tableName("migrate_diff") }

// TestMigrateDiffReport 验证差异报告在两轮迁移前后的内容
func TestMigrateDiffReport(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		table := tableName("migrate_diff")
		dropTable(t, engine, table)

		if err := engine.Migrate(&MigrateDiffBase{}); err != nil {
			t.Fatalf("基础建表失败: %v", err)
		}

		// 只读对比：应报告新增列与缺失索引，但不执行任何 DDL
		diff, err := engine.DiffTable(&MigrateDiffNext{})
		if err != nil {
			t.Fatalf("DiffTable 失败: %v", err)
		}

		if !diff.Exists {
			t.Fatal("表应视为已存在")
		}

		if len(diff.AddedColumns) != 1 || diff.AddedColumns[0] != "email" {
			t.Fatalf("AddedColumns = %v, want [email]", diff.AddedColumns)
		}

		wantIndex := table + "_email_idx"
		if len(diff.MissingIndexes) != 1 || diff.MissingIndexes[0] != wantIndex {
			t.Fatalf("MissingIndexes = %v, want [%s]", diff.MissingIndexes, wantIndex)
		}

		if diff.IsEmpty() {
			t.Fatal("存在差异时 IsEmpty 应为 false")
		}

		// 执行迁移后，差异应清空且索引真实存在
		if err = engine.Migrate(&MigrateDiffNext{}); err != nil {
			t.Fatalf("迁移失败: %v", err)
		}

		diff, err = engine.DiffTable(&MigrateDiffNext{})
		if err != nil {
			t.Fatalf("迁移后 DiffTable 失败: %v", err)
		}

		if !diff.IsEmpty() {
			t.Fatalf("迁移后应无差异，实际: %+v", diff)
		}

		sess := engine.NewSession(&MigrateDiffNext{})
		defer func() { _ = sess.Close() }()

		indexes, err := sess.GetTableIndexes()
		if err != nil {
			t.Fatalf("读取索引失败: %v", err)
		}

		found := false
		for _, name := range indexes {
			if name == wantIndex {
				found = true
				break
			}
		}

		if !found {
			t.Fatalf("索引 %s 不存在，实际: %v", wantIndex, indexes)
		}
	})
}

// TestBatchInsertReturnsPrimaryKeys 验证批量插入在 PG/SQLite 下回填自增主键
func TestBatchInsertReturnsPrimaryKeys(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("account"), &Account{})

		rows := []*Account{
			{Name: "pk-1", Email: "pk-1@example.com"},
			{Name: "pk-2", Email: "pk-2@example.com"},
			{Name: "pk-3", Email: "pk-3@example.com"},
		}

		sess := engine.NewSession(&Account{})
		defer func() { _ = sess.Close() }()

		res, err := sess.INSERT(false, rows[0], rows[1], rows[2])
		if err != nil {
			t.Fatalf("批量插入失败: %v", err)
		}

		// MySQL 无法逐行取回生成的主键，保持零值
		if tc.Name == "mysql" {
			for i, row := range rows {
				if row.ID != 0 {
					t.Fatalf("MySQL 批量插入不应回填主键，第 %d 行 = %d", i, row.ID)
				}
			}

			return
		}

		for i, row := range rows {
			if row.ID == 0 {
				t.Fatalf("%s 批量插入应回填主键，第 %d 行为 0\n%s", tc.Name, i, summarize(res))
			}

			if i > 0 && row.ID <= rows[i-1].ID {
				t.Fatalf("%s 主键应按行序递增: %d <= %d", tc.Name, row.ID, rows[i-1].ID)
			}
		}
	})
}

// TestBatchInsert 验证不同规模下的单条、多行与分批插入
func TestBatchInsert(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("account"), &Account{})

		sizes := []int{1, 2, 99, 150, 1200}
		var total int64

		for _, size := range sizes {
			rows := make([]any, size)
			for i := 0; i < size; i++ {
				rows[i] = &Account{
					Name:  fmt.Sprintf("batch-%d-%d", size, i),
					Email: fmt.Sprintf("batch-%d-%d@example.com", size, i),
					Age:   i % 50,
				}
			}

			sess := engine.NewSession(&Account{})
			res, err := sess.INSERT(false, rows...)
			if err != nil {
				t.Fatalf("批量插入 %d 条失败: %v", size, err)
			}

			affected, err := res.RowsAffected()
			if err != nil {
				t.Fatalf("读取影响行数失败: %v", err)
			}

			if affected != int64(size) {
				t.Fatalf("批量插入 %d 条影响行数不符: got %d", size, affected)
			}

			total += int64(size)
			if got := mustCount(t, engine, &Account{}, nil); got != total {
				t.Fatalf("批量插入后总数不符: got %d, want %d", got, total)
			}
		}
	})
}

// TestPreparedStatementCache 验证查询/统计/更新/删除在真库上复用预编译语句
func TestPreparedStatementCache(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("account"), &Account{})

		sess := engine.NewSession(&Account{})
		defer func() { _ = sess.Close() }()

		account := &Account{Name: "prepared", Email: "prepared@example.com", Age: 18, Active: true}
		res, err := sess.INSERT(false, account)
		if err != nil {
			t.Fatalf("插入失败: %v", err)
		}

		id := account.ID
		if id == 0 {
			if id, err = res.LastInsertId(); err != nil || id == 0 {
				t.Fatalf("读取自增主键失败: id=%d err=%v", id, err)
			}
		}

		// 同一 SQL 连续执行多次，命中缓存不能改变结果
		for i := 0; i < 3; i++ {
			var got Account
			if _, err := sess.WHERE(where.EQ("id", id)).FindFirst(&got); err != nil {
				t.Fatalf("第 %d 次查询失败: %v", i+1, err)
			}

			if got.Name != "prepared" || got.Email != "prepared@example.com" {
				t.Fatalf("第 %d 次查询结果不符: %+v", i+1, got)
			}
		}

		if count, _, err := sess.WHERE(where.EQ("id", id)).COUNT(); err != nil || count != 1 {
			t.Fatalf("COUNT = %d, err = %v, want 1", count, err)
		}

		if ok, err := sess.WHERE(where.EQ("id", id)).Exist(); err != nil || !ok {
			t.Fatalf("Exist = %v, err = %v, want true", ok, err)
		}

		var got Account
		if _, err := sess.WHERE(where.EQ("id", id)).FindFirst(&got); err != nil {
			t.Fatalf("更新前查询失败: %v", err)
		}

		got.Note = "updated"
		if _, err := sess.UPDATE(&got); err != nil {
			t.Fatalf("更新失败: %v", err)
		}

		var updated Account
		if _, err := sess.WHERE(where.EQ("id", id)).FindFirst(&updated); err != nil {
			t.Fatalf("更新后查询失败: %v", err)
		}

		if updated.Note != "updated" {
			t.Fatalf("更新未生效: note=%q", updated.Note)
		}

		if _, err := sess.DELETE(&updated); err != nil {
			t.Fatalf("删除失败: %v", err)
		}

		if count := mustCount(t, engine, &Account{}, nil); count != 0 {
			t.Fatalf("删除后行数 = %d, want 0", count)
		}
	})
}

// TestBatchInsertLarge 验证超过单批上限时按方言分批写入
func TestBatchInsertLarge(t *testing.T) {
	const total = 1200

	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("account"), &Account{})

		sess := engine.NewSession(&Account{})
		defer func() { _ = sess.Close() }()

		values := make([]any, total)
		for i := range values {
			values[i] = &Account{
				Name:  fmt.Sprintf("batch-%04d", i),
				Email: fmt.Sprintf("batch-%04d@example.com", i),
				Age:   i % 100,
			}
		}

		res, err := sess.INSERT(false, values...)
		if err != nil {
			t.Fatalf("大批量插入失败: %v", err)
		}

		if rows, err := res.RowsAffected(); err != nil || rows != total {
			t.Fatalf("受影响行数 = %d, err = %v, want %d", rows, err, total)
		}

		if count := mustCount(t, engine, &Account{}, nil); count != total {
			t.Fatalf("插入后行数 = %d, want %d", count, total)
		}

		// 支持 RETURNING 的方言应在分批写入中逐行回填主键
		if tc.Driver != "mysql" {
			for i, value := range values {
				if value.(*Account).ID == 0 {
					t.Fatalf("第 %d 行主键未回填", i)
				}
			}
		}
	})
}

// TestConcurrentSessions 验证多 goroutine 各自持有会话时的并发写入
func TestConcurrentSessions(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("account"), &Account{})

		const (
			workers = 8
			perWork = 25
		)

		var wg sync.WaitGroup
		errCh := make(chan error, workers)

		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()

				sess := engine.NewSession(&Account{})
				defer func() { _ = sess.Close() }()

				for i := 0; i < perWork; i++ {
					if _, err := sess.INSERT(false, &Account{
						Name:  fmt.Sprintf("worker-%d-%d", worker, i),
						Email: fmt.Sprintf("worker-%d-%d@example.com", worker, i),
					}); err != nil {
						errCh <- fmt.Errorf("worker %d 第 %d 条插入失败: %w", worker, i, err)
						return
					}
				}
			}(w)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Fatal(err)
		}

		want := int64(workers * perWork)
		if got := mustCount(t, engine, &Account{}, nil); got != want {
			t.Fatalf("并发写入行数不符: got %d, want %d", got, want)
		}
	})
}

// TestSessionCloseRollback 验证未提交事务在 Close 时自动回滚
func TestSessionCloseRollback(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("account"), &Account{})

		sess := engine.NewSession(&Account{})
		if err := sess.Begin(); err != nil {
			t.Fatalf("Begin 失败: %v", err)
		}

		if _, err := sess.INSERT(false, &Account{Name: "ghost", Email: "ghost@example.com"}); err != nil {
			t.Fatalf("事务内插入失败: %v", err)
		}

		if err := sess.Close(); err != nil {
			t.Fatalf("关闭会话失败: %v", err)
		}

		if got := mustCount(t, engine, &Account{}, nil); got != 0 {
			t.Fatalf("Close 应回滚未提交事务, 当前行数 %d", got)
		}
	})
}
