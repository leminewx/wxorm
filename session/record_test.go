package session

import (
	"errors"
	"fmt"
	"strconv"
	"testing"

	"gitee.com/leminewx/wxorm/harness/examples"
	"gitee.com/leminewx/wxorm/statement/where"
)

// 测试单条插入
func TestGroupInsertOne(t *testing.T) {
	sess := setupGroupSession(t)
	sess.SetDebug(false)
	defer teardownSession(sess)

	group := &examples.Group{
		Name:      "研发部",
		CreatedBy: "admin",
	}
	result, err := sess.INSERT(false, group)
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}
	if id, _ := result.LastInsertId(); id == 0 {
		t.Error("last insert id is 0")
	}

	// 验证写入数据
	var count int
	row := sess.QueryRow("SELECT COUNT(*) FROM `groups` WHERE `name` = ?", "研发部")
	if err := row.Scan(&count); err != nil || count != 1 {
		t.Errorf("expected 1 record, got %d, err: %v", count, err)
	}
}

// 测试多条插入
func TestGroupInsertMany(t *testing.T) {
	sess := setupGroupSession(t)
	sess.SetDebug(false)
	defer teardownSession(sess)

	groups := []any{
		&examples.Group{Name: "市场部", CreatedBy: "alice"},
		&examples.Group{Name: "技术部", CreatedBy: "bob"},
	}
	result, err := sess.INSERT(false, groups...)
	if err != nil {
		t.Fatalf("insert many failed: %v", err)
	}
	if rows, _ := result.RowsAffected(); rows != 2 {
		t.Errorf("expected 2 rows affected, got %d", rows)
	}
}

// 测试批量插入（超过100条，触发事务批次）
func TestGroupInsertBatches(t *testing.T) {
	sess := setupGroupSession(t)
	sess.SetDebug(false)
	defer teardownSession(sess)

	total := 250
	groups := make([]any, total)
	for i := 0; i < total; i++ {
		groups[i] = &examples.Group{
			Name:      "Group-" + strconv.Itoa(i),
			CreatedBy: "tester",
		}
	}
	result, err := sess.INSERT(false, groups...)
	if err != nil {
		t.Fatalf("batch insert failed: %v", err)
	}
	if rows, _ := result.RowsAffected(); rows != int64(total) {
		t.Errorf("expected %d rows affected, got %d", total, rows)
	}
}

func TestGroupUpdate_Map(t *testing.T) {
	sess := setupGroupSession(t)
	sess.SetDebug(false)
	defer teardownSession(sess)

	// 插入一条数据
	id := testInsertGroup(sess, "研发部", "admin")

	// 正常 map 更新
	result, err := sess.WHERE(where.EQ("id", id)).UPDATE(map[string]any{
		"name":       "技术部",
		"created_by": "alice",
	})
	if err != nil {
		t.Fatalf("map update failed: %v", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		t.Errorf("expected 1 row affected, got %d", affected)
	}

	// 验证数据
	var name, createdBy string
	row := sess.db.QueryRow("SELECT `name`, `created_by` FROM `groups` WHERE `id` = ?", id)
	if err := row.Scan(&name, &createdBy); err != nil {
		t.Fatal(err)
	}
	if name != "技术部" || createdBy != "alice" {
		t.Errorf("expect (技术部, alice), got (%s, %s)", name, createdBy)
	}

	// 空 map
	if _, err = sess.WHERE(where.EQ("id", id)).UPDATE(map[string]any{}); !errors.Is(err, ErrUpdateMapEmpty) {
		t.Errorf("expected ErrUpdateMapEmpty, got %v", err)
	}
}

func TestGroupUpdate_Struct_AutoWhere(t *testing.T) {
	sess := setupGroupSession(t)
	sess.SetDebug(false)
	defer teardownSession(sess)

	// 插入两条数据，ID 自增
	id1 := testInsertGroup(sess, "市场部", "bob")

	// 正常全量更新（自动使用主键 WHERE）
	updateGroup := &examples.Group{
		ID:        id1,
		Name:      "市场运营部",
		CreatedBy: "bob_updated",
		// CreatedAt 不设置，应为空字符串，但此字段是自动写入，不会参与更新
	}
	result, err := sess.UPDATE(updateGroup)
	if err != nil {
		t.Fatalf("struct update failed: %v", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		t.Errorf("expected 1 row affected, got %d", affected)
	}

	// 验证 name 和 created_by 已更新
	var name, createdBy string
	row := sess.db.QueryRow("SELECT `name`, `created_by` FROM `groups` WHERE `id` = ?", id1)
	if err := row.Scan(&name, &createdBy); err != nil {
		t.Fatal(err)
	}
	if name != "市场运营部" || createdBy != "bob_updated" {
		t.Errorf("expect (市场运营部, bob_updated), got (%s, %s)", name, createdBy)
	}

	// 验证 CreatedAt 未被更新（仍为原始时间，仅检查不为空且格式正确即可）
	var createdAt string
	row = sess.db.QueryRow("SELECT `created_at` FROM `groups` WHERE `id` = ?", id1)
	if err := row.Scan(&createdAt); err != nil {
		t.Fatal(err)
	}
	if createdAt == "" {
		t.Error("created_at should not be empty")
	}

	// 主键除外，其余值全为零值：默认不产生 UPDATE，避免把未赋值的字段写回零值
	zeroGroup := &examples.Group{
		ID: id1,
		// Name: "", CreatedBy: "" 均为零值
	}
	if _, err = sess.UPDATE(zeroGroup); !errors.Is(err, ErrUpdateStructEmpty) {
		t.Fatalf("expected ErrUpdateStructEmpty, got %v", err)
	}

	// 显式要求包含零值时才会写回：created_by 被置空
	result, err = sess.OVERWRITE().UPDATE(&examples.Group{ID: id1, Name: "市场运营部"})
	if err != nil {
		t.Fatalf("include zero update failed: %v", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		t.Errorf("expected 1 row affected, got %d", affected)
	}
	row = sess.db.QueryRow("SELECT `created_by` FROM `groups` WHERE `id` = ?", id1)
	if err := row.Scan(&createdBy); err != nil {
		t.Fatal(err)
	}
	if createdBy != "" {
		t.Errorf("expected created_by cleared to empty, got %q", createdBy)
	}

	// 主键为零值，且未手动 WHERE
	invalidGroup := &examples.Group{ID: 0, Name: "invalid"}
	if _, err = sess.UPDATE(invalidGroup); !errors.Is(err, ErrTablePKValuesZero) {
		t.Errorf("expected ErrUpdateTablePKValuesZero, got %v", err)
	}
}

func TestGroupUpdate_Struct_ManualWhere(t *testing.T) {
	sess := setupGroupSession(t)
	sess.SetDebug(false)
	defer teardownSession(sess)

	_ = testInsertGroup(sess, "财务部", "dave")
	_ = testInsertGroup(sess, "财务部", "eve") // 同名部门

	// 手动 WHERE：更新所有 name='财务部' 的记录
	updateGroup := &examples.Group{
		ID:   0, // ID 零值，但不会被用于 WHERE，因为手动条件优先
		Name: "财务与审计部",
	}
	result, err := sess.WHERE(where.EQ("name", "财务部")).UPDATE(updateGroup)
	if err != nil {
		t.Fatalf("update with manual WHERE failed: %v", err)
	}
	if affected, _ := result.RowsAffected(); affected != 2 {
		t.Errorf("expected 2 rows affected, got %d", affected)
	}

	// 验证两条记录的名称都已更新
	var count int
	row := sess.db.QueryRow("SELECT COUNT(*) FROM `groups` WHERE `name` = ?", "财务与审计部")
	if err := row.Scan(&count); err != nil || count != 2 {
		t.Errorf("expected 2 records with new name, got %d", count)
	}
}

func TestGroupUpdate_NoPK(t *testing.T) {
	sess := setupFilterSession(t)
	sess.SetDebug(false)
	defer teardownSession(sess)

	// 插入一条数据
	if _, err := sess.INSERT(false, &examples.Filter{
		ExportedField:   1,
		UnexportedField: true,
	}); err != nil {
		t.Fatal(err)
	}

	// 尝试通过结构体更新（无主键，未手动 WHERE）
	model := &examples.Filter{
		ExportedField:   2,
		UnexportedField: false,
		NoTagField:      "update",
	}
	if _, err := sess.UPDATE(model); !errors.Is(err, ErrTableNoPK) {
		t.Errorf("expected ErrUpdateTableNoPK, got %v", err)
	}

	// 手动 WHERE 可以更新
	result, err := sess.WHERE(where.EQ("exported", 1)).UPDATE(model)
	if err != nil {
		t.Fatalf("update with manual WHERE failed: %v", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		t.Errorf("expected 1 row affected, got %d", affected)
	}
}

func TestGroupUpdate_TypeMismatch(t *testing.T) {
	sess := setupGroupSession(t)
	defer teardownSession(sess)

	// 传入错误类型的结构体
	if _, err := sess.UPDATE(&examples.Filter{
		ExportedField:   2,
		UnexportedField: false,
		NoTagField:      "update",
	}); err == nil {
		t.Error("expected type mismatch error, got nil")
	}
}

/*
cpu: QEMU Virtual CPU version 2.5+		2		Insert-1         	 999	   2213624 ns/op	     871 B/op	      21 allocs/op
cpu: QEMU Virtual CPU version 2.5+		2		Insert-10        	 828	   2054773 ns/op	    5004 B/op	      81 allocs/op
cpu: QEMU Virtual CPU version 2.5+		2		Insert-100       	 266	   5763426 ns/op	   43022 B/op	     598 allocs/op
cpu: QEMU Virtual CPU version 2.5+		2		Insert-1000      	 100	  23913322 ns/op	  414692 B/op	    5957 allocs/op

cpu: Intel(R) Core(TM) i5-14600KF		20      Insert-1	   	       7	 153157229 ns/op	     873 B/op	      18 allocs/op
cpu: Intel(R) Core(TM) i5-14600KF		20      Insert-10	  	       7	 153171700 ns/op	    5006 B/op	      70 allocs/op
cpu: Intel(R) Core(TM) i5-14600KF		20      Insert-100	 	       3	 361745567 ns/op	   43066 B/op	     533 allocs/op
cpu: Intel(R) Core(TM) i5-14600KF		20      Insert-1000	      	   2	 566058000 ns/op	  412072 B/op	    5954 allocs/op
*/
func BenchmarkGroupInsert(b *testing.B) {
	sess := setupGroupSession(b)
	defer teardownSession(sess)

	// 单条插入（预编译路径）
	b.Run("Insert-1", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			group := &examples.Group{Name: "Bench" + strconv.Itoa(i), CreatedBy: "bench"}
			if _, err := sess.INSERT(false, group); err != nil {
				b.Fatal(err)
			}
		}
	})

	// 小批量（10条，动态SQL）
	b.Run("Insert-10", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			groups := make([]any, 10)
			for j := 0; j < 10; j++ {
				groups[j] = &examples.Group{Name: "Batch10_" + strconv.Itoa(i) + "_" + strconv.Itoa(j), CreatedBy: "bench"}
			}

			if _, err := sess.INSERT(false, groups...); err != nil {
				b.Fatal(err)
			}
		}
	})

	// 批量（100条，进入 insertMany）
	b.Run("Insert-100", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			groups := make([]any, 100)
			for j := 0; j < 100; j++ {
				groups[j] = &examples.Group{Name: "Batch100_" + strconv.Itoa(i) + "_" + strconv.Itoa(j), CreatedBy: "bench"}
			}

			if _, err := sess.INSERT(false, groups...); err != nil {
				b.Fatal(err)
			}
		}
	})

	// 大批量（1000条，触发批次事务）
	b.Run("Insert-1000", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			groups := make([]any, 1000)
			for j := 0; j < 1000; j++ {
				groups[j] = &examples.Group{Name: "Batch1k_" + strconv.Itoa(i) + "_" + strconv.Itoa(j), CreatedBy: "bench"}
			}

			if _, err := sess.INSERT(false, groups...); err != nil {
				b.Fatal(err)
			}
		}
	})
}

/*
cpu: Intel(R) Core(TM) i5-14600KF		 8			1138	    951850 ns/op	    1492 B/op	      23 allocs/op
*/
func BenchmarkGroupUpdate_Map(b *testing.B) {
	sess := setupGroupSession(b)
	defer teardownSession(sess)

	id := testInsertGroup(sess, "bench-map", "tester")

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := sess.WHERE(where.EQ("id", id)).UPDATE(map[string]any{
			"name":       "map-updated",
			"created_by": "bench",
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

/*
cpu: Intel(R) Core(TM) i5-14600KF		 8			1117	    962779 ns/op	    1652 B/op	      27 allocs/op
*/
func BenchmarkGroupUpdate_Struct_Single(b *testing.B) {
	sess := setupGroupSession(b)
	defer teardownSession(sess)

	id := testInsertGroup(sess, "bench-struct", "tester")
	group := &examples.Group{
		ID:        id,
		Name:      "struct-updated",
		CreatedBy: "bench-struct",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := sess.UPDATE(group)
		if err != nil {
			b.Fatal(err)
		}
	}
}

/*
对比手动 WHERE 与自动 WHERE 的性能差异

cpu: Intel(R) Core(TM) i5-14600KF		 8			1340	    875372 ns/op	    1655 B/op	      27 allocs/op
*/
func BenchmarkGroupUpdate_Struct_ManualWhere(b *testing.B) {
	sess := setupGroupSession(b)
	defer teardownSession(sess)

	id := testInsertGroup(sess, "bench-manual", "tester")
	group := &examples.Group{
		ID:        id, // 这里 ID 实际不会被用于 WHERE，因为手动条件覆盖
		Name:      "manual-where",
		CreatedBy: "bench",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := sess.WHERE(where.EQ("id", id)).UPDATE(group)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// 批量更新模拟：循环更新多条记录
func BenchmarkGroupUpdate_Batch(b *testing.B) {
	sess := setupGroupSession(b)
	defer teardownSession(sess)

	// 预先插入100条记录
	ids := make([]int64, 100)
	for i := 0; i < 100; i++ {
		ids[i] = testInsertGroup(sess, fmt.Sprintf("batch-%d", i), "batch")
	}

	b.Run("Batch-1", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for n := 0; n < b.N; n++ {
			for i := 0; i < 1; i++ {
				group := &examples.Group{ID: ids[i], Name: "batch1-updated"}
				_, _ = sess.UPDATE(group)
			}
		}
	})

	b.Run("Batch-10", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for n := 0; n < b.N; n++ {
			for i := 0; i < 10; i++ {
				group := &examples.Group{ID: ids[i], Name: "batch10-updated"}
				_, _ = sess.UPDATE(group)
			}
		}
	})

	b.Run("Batch-100", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for n := 0; n < b.N; n++ {
			for i := 0; i < 100; i++ {
				group := &examples.Group{ID: ids[i], Name: "batch100-updated"}
				_, _ = sess.UPDATE(group)
			}
		}
	})
}
