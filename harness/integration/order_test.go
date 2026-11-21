package integration

import (
	"testing"

	"gitee.com/leminewx/wxorm"
)

// TestOrderLimit 验证多列排序与分页语义
func TestOrderLimit(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("account"), &Account{})
		seedAccounts(t, engine)

		// age 降序；同龄时按 name 升序（bob < bobby），只取前 3 条
		var rows []Account
		sess := engine.NewSession(&Account{})
		res, err := sess.COLUMNS("name", "age").ORDER("age DESC", "name ASC").LIMIT(3).SELECT(&rows)
		if err != nil {
			t.Fatalf("排序查询失败: %v\n%s", err, summarize(res))
		}

		want := []struct {
			name string
			age  int
		}{
			{"dave", 40},
			{"carol", 30},
			{"bob", 20},
		}

		if len(rows) != len(want) {
			t.Fatalf("行数 = %d, want %d: %#v", len(rows), len(want), rows)
		}

		for i := range want {
			if rows[i].Name != want[i].name || rows[i].Age != want[i].age {
				t.Errorf("第 %d 行 = (%q, %d), want (%q, %d)", i, rows[i].Name, rows[i].Age, want[i].name, want[i].age)
			}
		}

		// LIMIT + OFFSET：按同一顺序跳过前 2 条
		var page []Account
		sess = engine.NewSession(&Account{})
		res, err = sess.COLUMNS("name").ORDER("age DESC", "name ASC").LIMIT(2).OFFSET(2).SELECT(&page)
		if err != nil {
			t.Fatalf("分页查询失败: %v\n%s", err, summarize(res))
		}

		if len(page) != 2 || page[0].Name != "bob" || page[1].Name != "bobby" {
			t.Fatalf("分页结果 = %#v, want [bob bobby]", page)
		}

		// 排序字段不必出现在返回列中
		var names []Account
		sess = engine.NewSession(&Account{})
		res, err = sess.COLUMNS("name").ORDER("age DESC").LIMIT(1).SELECT(&names)
		if err != nil {
			t.Fatalf("仅取名称失败: %v\n%s", err, summarize(res))
		}

		if len(names) != 1 || names[0].Name != "dave" || names[0].Age != 0 {
			t.Fatalf("仅查询 name 的结果 = %#v", names)
		}
	})
}

// TestUniqueViolation 验证唯一约束真实生效：重复值插入必须失败且不污染已有数据
func TestUniqueViolation(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("account"), &Account{})

		sess := engine.NewSession(&Account{})
		if _, err := sess.INSERT(false, &Account{Name: "alice", Email: "dup@example.com"}); err != nil {
			t.Fatalf("首次插入失败: %v", err)
		}

		if _, err := sess.INSERT(false, &Account{Name: "bob", Email: "dup@example.com"}); err == nil {
			t.Fatal("重复唯一值应插入失败")
		}

		if got := mustCount(t, engine, &Account{}, nil); got != 1 {
			t.Fatalf("失败插入后行数 = %d, want 1", got)
		}
	})
}
