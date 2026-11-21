package session

import (
	"testing"

	"gitee.com/leminewx/wxorm/harness/examples"
	"gitee.com/leminewx/wxorm/statement/where"
)

// benchRows 基准测试使用的数据行数
const benchRows = 200

// prepareBenchGroups 建表并写入固定行数的数据
func prepareBenchGroups(b *testing.B) *Session {
	b.Helper()

	sess := setupGroupSession(b)
	values := make([]any, benchRows)
	for i := 0; i < benchRows; i++ {
		values[i] = &examples.Group{Name: "bench", CreatedBy: "tester"}
	}

	if _, err := sess.INSERT(false, values...); err != nil {
		b.Fatalf("prepare benchmark data failed: %v", err)
	}

	return sess
}

// BenchmarkSessionSelectRows 全表查询 200 行
func BenchmarkSessionSelectRows(b *testing.B) {
	sess := prepareBenchGroups(b)
	defer teardownSession(sess)

	b.ReportAllocs()

	for b.Loop() {
		var groups []examples.Group
		if _, err := sess.SELECT(&groups); err != nil {
			b.Fatalf("select failed: %v", err)
		}

		if len(groups) < benchRows {
			b.Fatalf("expected at least %d rows, got %d", benchRows, len(groups))
		}
	}
}

// BenchmarkSessionSelectColumns 只查询两列，验证列映射开销
func BenchmarkSessionSelectColumns(b *testing.B) {
	sess := prepareBenchGroups(b)
	defer teardownSession(sess)

	b.ReportAllocs()

	for b.Loop() {
		var groups []examples.Group
		if _, err := sess.COLUMNS("id", "name").SELECT(&groups); err != nil {
			b.Fatalf("select failed: %v", err)
		}

		if len(groups) < benchRows {
			b.Fatalf("expected at least %d rows, got %d", benchRows, len(groups))
		}
	}
}

// BenchmarkSessionFirst 查询单条数据
func BenchmarkSessionFirst(b *testing.B) {
	sess := prepareBenchGroups(b)
	defer teardownSession(sess)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var group examples.Group
		if _, err := sess.WHERE(where.EQ("id", 1)).FindFirst(&group); err != nil {
			b.Fatalf("first failed: %v", err)
		}
	}
}

// BenchmarkSessionExist 存在性判断
func BenchmarkSessionExist(b *testing.B) {
	sess := prepareBenchGroups(b)
	defer teardownSession(sess)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := sess.WHERE(where.EQ("name", "bench")).Exist(); err != nil {
			b.Fatalf("exist failed: %v", err)
		}
	}
}

// BenchmarkSessionInsertOne 单条插入（预编译路径）
func BenchmarkSessionInsertOne(b *testing.B) {
	sess := setupGroupSession(b)
	defer teardownSession(sess)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := sess.INSERT(false, &examples.Group{Name: "bench", CreatedBy: "tester"}); err != nil {
			b.Fatalf("insert failed: %v", err)
		}
	}
}

// BenchmarkSessionUpdateStruct 按主键更新结构体
func BenchmarkSessionUpdateStruct(b *testing.B) {
	sess := prepareBenchGroups(b)
	defer teardownSession(sess)

	id := testInsertGroup(sess, "update-bench", "tester")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		group := &examples.Group{ID: id, Name: "update-bench", CreatedBy: "tester"}
		if _, err := sess.UPDATE(group); err != nil {
			b.Fatalf("update failed: %v", err)
		}
	}
}

// prepareCachedBenchGroups 建表、写入数据并启用查询语句缓存
//
// 注意：本文件的所有 *Cached 基准跑在 harness/examples/fakedriver 上，
// 该驱动的 Prepare 只记录 SQL、Stmt.Exec 仍完整解析语句，
// 因此这里的数字不体现真实数据库复用预编译语句的收益，只作回归监控；
// 真实收益以 harness/integration 的三方言基准为准（见 docs/analysis.md 12.7 / 12.8）。
func prepareCachedBenchGroups(b *testing.B) (*Session, *StatementCache) {
	b.Helper()

	sess := prepareBenchGroups(b)
	cache := NewStatementCache(DefaultStatementCacheSize)
	sess.SetStatementCache(cache)
	return sess, cache
}

// BenchmarkSessionSelectRowsCached 全表查询 200 行（命中预编译缓存）
func BenchmarkSessionSelectRowsCached(b *testing.B) {
	sess, cache := prepareCachedBenchGroups(b)
	defer teardownSession(sess)
	defer cache.Clear()

	b.ReportAllocs()

	for b.Loop() {
		var groups []examples.Group
		if _, err := sess.SELECT(&groups); err != nil {
			b.Fatalf("select failed: %v", err)
		}

		if len(groups) < benchRows {
			b.Fatalf("expected at least %d rows, got %d", benchRows, len(groups))
		}
	}
}

// BenchmarkSessionFirstCached 查询单条数据（命中预编译缓存）
func BenchmarkSessionFirstCached(b *testing.B) {
	sess, cache := prepareCachedBenchGroups(b)
	defer teardownSession(sess)
	defer cache.Clear()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		var group examples.Group
		if _, err := sess.WHERE(where.EQ("id", 1)).FindFirst(&group); err != nil {
			b.Fatalf("first failed: %v", err)
		}
	}
}

// BenchmarkSessionUpdateStructCached 按主键更新结构体（命中预编译缓存）
func BenchmarkSessionUpdateStructCached(b *testing.B) {
	sess, cache := prepareCachedBenchGroups(b)
	defer teardownSession(sess)
	defer cache.Clear()

	id := testInsertGroup(sess, "update-bench", "tester")
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		group := &examples.Group{ID: id, Name: "update-bench", CreatedBy: "tester"}
		if _, err := sess.UPDATE(group); err != nil {
			b.Fatalf("update failed: %v", err)
		}
	}
}
