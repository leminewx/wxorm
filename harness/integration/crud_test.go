package integration

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"gitee.com/leminewx/wxorm"
	"gitee.com/leminewx/wxorm/session"
	"gitee.com/leminewx/wxorm/statement/where"
)

// TestDDL 验证建表、表存在性、模型解析与删表
func TestDDL(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		table := tableName("account")
		dropTable(t, engine, table)

		sess := engine.NewSession(&Account{})
		defer func() { _ = sess.Close() }()

		if exists, err := sess.ExistsTable(); err != nil {
			t.Fatalf("查询表存在性失败: %v", err)
		} else if exists {
			t.Fatal("删表后 ExistsTable 应为 false")
		}

		if err := sess.CreateTable(); err != nil {
			t.Fatalf("建表失败: %v", err)
		}

		if exists, err := sess.ExistsTable(); err != nil {
			t.Fatalf("查询表存在性失败: %v", err)
		} else if !exists {
			t.Fatal("建表后 ExistsTable 应为 true")
		}

		sch, err := sess.GetSchema()
		if err != nil {
			t.Fatalf("解析模型失败: %v", err)
		}

		want := []string{"id", "name", "email", "age", "balance", "active", "payload", "note", "created_at", "updated_at"}
		if len(sch.ColumnNames) != len(want) {
			t.Fatalf("模型列数不符: got %v, want %v", sch.ColumnNames, want)
		}

		for i, name := range want {
			if sch.ColumnNames[i] != name {
				t.Fatalf("第 %d 列不符: got %q, want %q（全量 %v）", i, sch.ColumnNames[i], name, sch.ColumnNames)
			}
		}

		if err := sess.DeleteTable(); err != nil {
			t.Fatalf("删表失败: %v", err)
		}

		if exists, err := sess.ExistsTable(); err != nil {
			t.Fatalf("查询表存在性失败: %v", err)
		} else if exists {
			t.Fatal("DeleteTable 后 ExistsTable 应为 false")
		}
	})
}

// TestInsertSelect 验证单条/多条插入、查询、分页、指定列与存在性判断
func TestInsertSelect(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		table := tableName("account")
		createTable(t, engine, table, &Account{})

		first := &Account{
			Name:    "alice",
			Email:   "alice@example.com",
			Age:     30,
			Balance: 12.5,
			Active:  true,
			Payload: []byte{0x00, 0x01, 0xff},
			Note:    "first",
		}

		sess := engine.NewSession(&Account{})
		res, err := sess.INSERT(false, first)
		if err != nil {
			t.Fatalf("插入单条失败: %v", err)
		}

		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("读取自增主键失败: %v", err)
		}

		if tc.Driver == "pgx" {
			// PostgreSQL 通过 INSERT ... RETURNING 回填模型主键
			if first.ID == 0 {
				t.Fatal("PostgreSQL 插入后应回填模型主键")
			}
			id = first.ID
		}

		if id == 0 {
			t.Fatal("插入后自增主键不应为 0")
		}

		// 多条插入走实时构建 INSERT 的分支
		others := []any{
			&Account{Name: "bob", Email: "bob@example.com", Age: 20, Balance: 1.25, Active: false, Note: "b"},
			&Account{Name: "carol", Email: "carol@example.com", Age: 40, Balance: 99.99, Active: true, Note: "c"},
		}
		if _, err := sess.INSERT(false, others...); err != nil {
			t.Fatalf("插入多条失败: %v", err)
		}

		// 全量查询：列顺序与模型一致，字段值应完整还原
		var accounts []Account
		if _, err := sess.ORDER("id").SELECT(&accounts); err != nil {
			t.Fatalf("查询全部失败: %v", err)
		}

		if len(accounts) != 3 {
			t.Fatalf("查询行数不符: got %d, want 3", len(accounts))
		}

		got := accounts[0]
		if got.ID != id || got.Name != first.Name || got.Email != first.Email || got.Age != first.Age {
			t.Fatalf("首行字段不符: %+v", got)
		}

		if got.Balance != first.Balance || got.Active != first.Active {
			t.Fatalf("浮点/布尔字段不符: %+v", got)
		}

		if !bytes.Equal(got.Payload, first.Payload) {
			t.Fatalf("二进制字段不符: got %v, want %v", got.Payload, first.Payload)
		}

		if got.Note != first.Note {
			t.Fatalf("字符串字段不符: got %q, want %q", got.Note, first.Note)
		}

		if got.CreatedAt.IsZero() || time.Since(got.CreatedAt).Abs() > 30*time.Minute {
			t.Fatalf("created 列未被数据库填充: %v（本地时间 %v）", got.CreatedAt, time.Now())
		}

		// 单条查询：命中
		var one Account
		if _, err := sess.WHERE(where.EQ("email", "bob@example.com")).FindFirst(&one); err != nil {
			t.Fatalf("FindFirst 失败: %v", err)
		}

		if one.Name != "bob" || one.Age != 20 || one.Active {
			t.Fatalf("FindFirst 结果不符: %+v", one)
		}

		// 单条查询：未命中应返回 ErrRecordNotFound
		var missing Account
		if _, err := sess.WHERE(where.EQ("email", "nobody@example.com")).FindFirst(&missing); !errors.Is(err, session.ErrRecordNotFound) {
			t.Fatalf("未命中应返回 ErrRecordNotFound, got %v", err)
		}

		// Exist
		exist, err := sess.WHERE(where.EQ("name", "carol")).Exist()
		if err != nil {
			t.Fatalf("Exist 失败: %v", err)
		}
		if !exist {
			t.Fatal("carol 应存在")
		}

		exist, err = sess.WHERE(where.EQ("name", "dave")).Exist()
		if err != nil {
			t.Fatalf("Exist 失败: %v", err)
		}
		if exist {
			t.Fatal("dave 不应存在")
		}

		// COUNT
		count, _, err := sess.COUNT()
		if err != nil {
			t.Fatalf("COUNT 失败: %v", err)
		}
		if count != 3 {
			t.Fatalf("COUNT 不符: got %d, want 3", count)
		}

		count, _, err = sess.WHERE(where.LIKE("name", "b%")).COUNT()
		if err != nil {
			t.Fatalf("条件 COUNT 失败: %v", err)
		}
		if count != 1 {
			t.Fatalf("条件 COUNT 不符: got %d, want 1", count)
		}

		// 分页：按 id 倒序取第二页
		var page []*Account
		if _, err := sess.ORDER("id DESC").LIMIT(1).OFFSET(1).SELECT(&page); err != nil {
			t.Fatalf("分页查询失败: %v", err)
		}
		if len(page) != 1 || page[0].Name != "bob" {
			t.Fatalf("分页结果不符: %+v", page)
		}

		// 只查指定列
		var partial []Account
		if _, err := sess.COLUMNS("id", "name").ORDER("id").SELECT(&partial); err != nil {
			t.Fatalf("指定列查询失败: %v", err)
		}
		if len(partial) != 3 || partial[0].Name != "alice" || partial[0].Email != "" {
			t.Fatalf("指定列查询结果不符: %+v", partial)
		}

		if _, err := sess.COLUMNS("id", "not_exist").SELECT(&partial); !errors.Is(err, session.ErrSelectColumnNotFound) {
			t.Fatalf("未知列应返回 ErrSelectColumnNotFound, got %v", err)
		}

		// 只写 OFFSET 应被拒绝
		if _, err := sess.OFFSET(1).SELECT(&partial); !errors.Is(err, session.ErrOffsetRequiresLimit) {
			t.Fatalf("单独 OFFSET 应返回 ErrOffsetRequiresLimit, got %v", err)
		}
	})
}

// TestUpdateDelete 验证结构化更新、map 更新与两种删除方式
func TestUpdateDelete(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		table := tableName("account")
		createTable(t, engine, table, &Account{})

		accounts := []any{
			&Account{Name: "alice", Email: "alice@example.com", Age: 30, Balance: 1, Active: true},
			&Account{Name: "bob", Email: "bob@example.com", Age: 20, Balance: 2, Active: true},
			&Account{Name: "carol", Email: "carol@example.com", Age: 40, Balance: 3, Active: false},
		}

		sess := engine.NewSession(&Account{})
		if _, err := sess.INSERT(false, accounts...); err != nil {
			t.Fatalf("准备数据失败: %v", err)
		}

		var rows []Account
		if _, err := sess.ORDER("id").SELECT(&rows); err != nil {
			t.Fatalf("查询准备数据失败: %v", err)
		}
		if len(rows) != 3 {
			t.Fatalf("准备数据行数不符: %d", len(rows))
		}

		// 结构化更新：只写非零字段，按主键定位
		update := rows[0]
		update.Name = "alice2"
		update.Age = 31
		if _, err := sess.UPDATE(&update); err != nil {
			t.Fatalf("结构化更新失败: %v", err)
		}

		var one Account
		if _, err := sess.WHERE(where.EQ("id", rows[0].ID)).FindFirst(&one); err != nil {
			t.Fatalf("验证更新失败: %v", err)
		}
		if one.Name != "alice2" || one.Age != 31 || one.Email != "alice@example.com" {
			t.Fatalf("结构化更新结果不符: %+v", one)
		}

		// 部分结构体：零值字段默认不写回，避免把未赋值的列清空
		partial := Account{ID: rows[0].ID, Name: "alice3"}
		if _, err := sess.UPDATE(&partial); err != nil {
			t.Fatalf("部分结构体更新失败: %v", err)
		}

		if _, err := sess.WHERE(where.EQ("id", rows[0].ID)).FindFirst(&one); err != nil {
			t.Fatalf("验证部分更新失败: %v", err)
		}
		if one.Name != "alice3" || one.Email != "alice@example.com" || one.Age != 31 {
			t.Fatalf("零值字段被意外写回: %+v", one)
		}

		// OVERWRITE：显式要求写回零值
		if _, err := sess.OVERWRITE().UPDATE(&Account{ID: rows[0].ID, Name: "alice4"}); err != nil {
			t.Fatalf("OVERWRITE 更新失败: %v", err)
		}

		if _, err := sess.WHERE(where.EQ("id", rows[0].ID)).FindFirst(&one); err != nil {
			t.Fatalf("验证 OVERWRITE 失败: %v", err)
		}
		if one.Name != "alice4" || one.Email != "" || one.Age != 0 {
			t.Fatalf("OVERWRITE 未写回零值: %+v", one)
		}

		// map 更新：必须显式 WHERE
		if _, err := sess.UPDATE(map[string]any{"balance": 88.8}); !errors.Is(err, session.ErrMissingWhere) {
			t.Fatalf("map 更新缺少 WHERE 应报错, got %v", err)
		}

		if _, err := sess.WHERE(where.EQ("name", "bob")).UPDATE(map[string]any{"balance": 88.8, "active": false}); err != nil {
			t.Fatalf("map 更新失败: %v", err)
		}

		if _, err := sess.WHERE(where.EQ("id", rows[1].ID)).FindFirst(&one); err != nil {
			t.Fatalf("验证 map 更新失败: %v", err)
		}
		if one.Balance != 88.8 || one.Active {
			t.Fatalf("map 更新结果不符: %+v", one)
		}

		// 结构化删除：按主键
		if _, err := sess.DELETE(&Account{ID: rows[0].ID}); err != nil {
			t.Fatalf("结构化删除失败: %v", err)
		}

		if got := mustCount(t, engine, &Account{}, nil); got != 2 {
			t.Fatalf("结构化删除后行数不符: got %d, want 2", got)
		}

		// 无条件删除应被拒绝
		if _, err := sess.DELETE(); !errors.Is(err, session.ErrMissingWhere) {
			t.Fatalf("无条件删除应返回 ErrMissingWhere, got %v", err)
		}

		// 条件删除
		if _, err := sess.WHERE(where.EQ("name", "carol")).DELETE(); err != nil {
			t.Fatalf("条件删除失败: %v", err)
		}

		if got := mustCount(t, engine, &Account{}, nil); got != 1 {
			t.Fatalf("条件删除后行数不符: got %d, want 1", got)
		}
	})
}
