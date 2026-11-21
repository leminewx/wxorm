package integration

import (
	"fmt"
	"testing"

	"gitee.com/leminewx/wxorm"
	"gitee.com/leminewx/wxorm/statement/where"
)

// seedRows 生成 n 条基准数据
func seedRows(n int) []any {
	rows := make([]any, n)
	for i := range rows {
		rows[i] = &Account{
			Name:  fmt.Sprintf("user-%d", i),
			Email: fmt.Sprintf("user-%d@example.com", i),
			Age:   i % 100,
		}
	}

	return rows
}

// BenchmarkInsertBatch 测量不同批量大小下 INSERT 的吞吐
func BenchmarkInsertBatch(b *testing.B) {
	for _, size := range []int{1, 20, 200} {
		size := size

		b.Run(fmt.Sprintf("rows=%d", size), func(b *testing.B) {
			forEachBenchDialect(b, func(b *testing.B, tc target, engine *wxorm.Engine) {
				// 表名由模型决定，这里必须传 wxorm_it_account 才能与 Account.GetTableName 一致
				createTable(b, engine, tableName("account"), &Account{})

				sess := engine.NewSession(&Account{})
				defer func() { _ = sess.Close() }()

				b.ReportAllocs()
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					rows := make([]any, size)
					for j := range rows {
						n := i*size + j
						rows[j] = &Account{
							Name:  fmt.Sprintf("user-%d", n),
							Email: fmt.Sprintf("user-%d@example.com", n),
							Age:   n % 100,
						}
					}

					if _, err := sess.INSERT(false, rows...); err != nil {
						b.Fatalf("批量插入失败: %v", err)
					}
				}
			})
		})
	}
}

// BenchmarkSelectPage 测量按主键顺序分页读取 100 行的耗时（每轮换一页）
func BenchmarkSelectPage(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, tc target, engine *wxorm.Engine) {
		createTable(b, engine, tableName("account"), &Account{})

		seeder := engine.NewSession(&Account{})
		if _, err := seeder.INSERT(false, seedRows(1000)...); err != nil {
			b.Fatalf("写入基准数据失败: %v", err)
		}

		_ = seeder.Close()

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			var rows []Account

			sess := engine.NewSession(&Account{})
			if _, err := sess.WHERE(where.GT("id", 0)).ORDER("id ASC").LIMIT(100).OFFSET((i % 10) * 100).SELECT(&rows); err != nil {
				_ = sess.Close()
				b.Fatalf("分页查询失败: %v", err)
			}

			_ = sess.Close()

			if len(rows) != 100 {
				b.Fatalf("分页行数 = %d, want 100", len(rows))
			}
		}
	})
}

// forEachBenchDialect 按方言跑基准子测试，连接失败的处理与测试保持一致
func forEachBenchDialect(b *testing.B, fn func(b *testing.B, tc target, engine *wxorm.Engine)) {
	b.Helper()

	for _, tc := range targets(b) {
		tc := tc

		b.Run(tc.Name, func(b *testing.B) {
			fn(b, tc, openEngine(b, tc))
		})
	}
}
