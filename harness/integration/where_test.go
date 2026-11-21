package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"gitee.com/leminewx/wxorm"
	"gitee.com/leminewx/wxorm/harness/assetpath"
	"gitee.com/leminewx/wxorm/statement/where"
)

// seedAccount 对应 harness/testdata/accounts.json 中的一行种子数据
type seedAccount struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Age   int    `json:"age"`
}

// seedAccounts 读取 harness/testdata/accounts.json 并写入种子数据
func seedAccounts(t *testing.T, engine *wxorm.Engine) {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(assetpath.GetDir(), "accounts.json"))
	if err != nil {
		t.Fatalf("读取种子数据失败: %v", err)
	}

	var seeds []seedAccount
	if err = json.Unmarshal(content, &seeds); err != nil {
		t.Fatalf("解析种子数据失败: %v", err)
	}

	rows := make([]any, 0, len(seeds))
	for _, seed := range seeds {
		rows = append(rows, &Account{Name: seed.Name, Email: seed.Email, Age: seed.Age})
	}

	sess := engine.NewSession(&Account{})
	defer func() { _ = sess.Close() }()

	if _, err = sess.INSERT(false, rows...); err != nil {
		t.Fatalf("写入种子数据失败: %v", err)
	}
}

// TestWhereConditions 验证常用条件构造函数在三方言下的语义
func TestWhereConditions(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("account"), &Account{})
		seedAccounts(t, engine)

		cases := []struct {
			name string
			cond where.Condition
			want int64
		}{
			{"EQ", where.EQ("name", "bob"), 1},
			{"NE", where.NE("name", "bob"), 4},
			{"GT", where.GT("age", 20), 2},
			{"GTE", where.GTE("age", 20), 4},
			{"LT", where.LT("age", 20), 1},
			{"LTE", where.LTE("age", 20), 3},
			{"IN", where.IN("age", 18, 30), 2},
			{"NOTIN", where.NOTIN("age", 18, 30), 3},
			{"LIKE", where.LIKE("name", "bob%"), 2},
			{"NOTLIKE", where.NOTLIKE("name", "bob%"), 3},
			{"BETWEEN", where.BETWEEN("age", 20, 30), 3},
			{"NOTBETWEEN", where.NOTBETWEEN("age", 20, 30), 2},
			{"RAW", where.RAW("age >= ?", 30), 2},
			{"AND", where.AND(where.GTE("age", 20), where.LT("age", 40)), 3},
			{"OR", where.OR(where.EQ("age", 18), where.EQ("age", 40)), 2},
			{"NOT", where.NOT(where.EQ("age", 20)), 3},
			{
				"混合",
				where.AND(
					where.OR(where.EQ("age", 18), where.EQ("age", 40)),
					where.LIKE("name", "%a%"),
				),
				2,
			},
		}

		for _, item := range cases {
			got := mustCount(t, engine, &Account{}, item.cond)
			if got != item.want {
				t.Fatalf("%s 条件结果不符: got %d, want %d", item.name, got, item.want)
			}
		}

		// payload 未赋值，应写入 NULL
		if got := mustCount(t, engine, &Account{}, where.ISNULL("payload")); got != 5 {
			t.Fatalf("ISNULL 结果不符: got %d, want 5", got)
		}

		if got := mustCount(t, engine, &Account{}, where.ISNOTNULL("payload")); got != 0 {
			t.Fatalf("ISNOTNULL 结果不符: got %d, want 0", got)
		}
	})
}

// TestGroupHaving 验证 GROUP BY 与 HAVING 的组合查询
func TestGroupHaving(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("account"), &Account{})
		seedAccounts(t, engine)

		sess := engine.NewSession(&Account{})
		defer func() { _ = sess.Close() }()

		var rows []Account
		if _, err := sess.
			COLUMNS("age").
			GROUP("age").
			HAVING(where.GT("age", 18)).
			ORDER("age").
			SELECT(&rows); err != nil {
			t.Fatalf("分组查询失败: %v", err)
		}

		if len(rows) != 3 || rows[0].Age != 20 || rows[1].Age != 30 || rows[2].Age != 40 {
			t.Fatalf("分组查询结果不符: %+v", rows)
		}

		// 未在 COLUMNS 中的字段应保持零值
		for _, row := range rows {
			if row.Name != "" || row.Email != "" {
				t.Fatalf("未查询的字段应为零值: %+v", row)
			}
		}
	})
}
