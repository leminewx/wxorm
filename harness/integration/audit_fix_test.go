package integration

import (
	"errors"
	"math/big"
	"testing"

	"gitee.com/leminewx/wxorm"
	"gitee.com/leminewx/wxorm/schema"
	"gitee.com/leminewx/wxorm/session"
	"gitee.com/leminewx/wxorm/statement/where"
)

// BigAccount 用于验证 big.Int / big.Float 的写入与回读
//
// 值类型与指针类型同时覆盖：指针字段区分「有值」与「SQL NULL」。
type BigAccount struct {
	ID     int64      `orm:"column(id) pk auto"`
	Name   string     `orm:"column(name) notnull"`
	Amount *big.Int   `orm:"column(amount) null"`
	Rate   *big.Float `orm:"column(rate) null"`
	Total  big.Int    `orm:"column(total) null"`
}

// GetTableName 返回大数测试表名
func (*BigAccount) GetTableName() string {
	return tableName("big_account")
}

// TestBigNumber 验证 big.Int / big.Float 在三方言下的建表、写入与回读
func TestBigNumber(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		table := tableName("big_account")
		createTable(t, engine, table, &BigAccount{})
		defer dropTable(t, engine, table)

		amount, ok := new(big.Int).SetString("123456789012345678901234567890", 10)
		if !ok {
			t.Fatal("构造 big.Int 失败")
		}

		rate, _, err := big.ParseFloat("12345.678901234567890123456789", 10, 256, big.ToNearestEven)
		if err != nil {
			t.Fatalf("构造 big.Float 失败: %v", err)
		}

		total := *new(big.Int).SetInt64(-987654321098765432)

		sess := engine.NewSession(&BigAccount{})
		defer func() { _ = sess.Close() }()

		if _, err := sess.INSERT(false, &BigAccount{
			Name:   "alice",
			Amount: amount,
			Rate:   rate,
			Total:  total,
		}); err != nil {
			t.Fatalf("%s: 插入大数失败: %v", tc.Name, err)
		}

		var got BigAccount
		if _, err := sess.WHERE(where.EQ("name", "alice")).FindFirst(&got); err != nil {
			t.Fatalf("%s: 查询大数失败: %v", tc.Name, err)
		}

		if got.Amount == nil || got.Amount.Cmp(amount) != 0 {
			t.Fatalf("%s: big.Int 回读不一致: got %v, want %v", tc.Name, got.Amount, amount)
		}

		if got.Rate == nil || got.Rate.Text('f', 30) != rate.Text('f', 30) {
			t.Fatalf("%s: big.Float 回读不一致: got %v, want %v", tc.Name, got.Rate, rate)
		}

		if got.Total.Cmp(&total) != 0 {
			t.Fatalf("%s: 值类型 big.Int 回读不一致: got %v, want %v", tc.Name, got.Total.String(), total.String())
		}

		// 指针字段置 nil 时应写入 SQL NULL 并原样读回
		if _, err := sess.WHERE(where.EQ("name", "alice")).UPDATE(map[string]any{"amount": nil}); err != nil {
			t.Fatalf("%s: 清空大数列失败: %v", tc.Name, err)
		}

		var nulled BigAccount
		if _, err := sess.WHERE(where.EQ("name", "alice")).FindFirst(&nulled); err != nil {
			t.Fatalf("%s: 查询 NULL 大数失败: %v", tc.Name, err)
		}

		if nulled.Amount != nil {
			t.Fatalf("%s: SQL NULL 应回读为 nil，got %v", tc.Name, nulled.Amount)
		}
	})
}

// TestBigRatRejected 验证 big.Rat 在模型解析阶段就被拒绝
func TestBigRatRejected(t *testing.T) {
	type unsupported struct {
		ID   int64    `orm:"column(id) pk auto"`
		Rate *big.Rat `orm:"column(rate)"`
	}

	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		sess := engine.NewSession(&unsupported{})
		defer func() { _ = sess.Close() }()

		if _, err := sess.GetSchema(); !errors.Is(err, schema.ErrUnsupportedBigRat) {
			t.Fatalf("%s: big.Rat 应被拒绝，got %v", tc.Name, err)
		}
	})
}

// TestUpdateColumns 验证 UPDATE_COLUMNS 只写白名单列
func TestUpdateColumns(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		table := tableName("account")
		createTable(t, engine, table, &Account{})
		defer dropTable(t, engine, table)

		sess := engine.NewSession(&Account{})
		defer func() { _ = sess.Close() }()

		seed := &Account{Name: "alice", Email: "alice@example.com", Age: 30, Note: "keep"}
		res, err := sess.INSERT(false, seed)
		if err != nil {
			t.Fatalf("插入种子数据失败: %v", err)
		}

		// MySQL / SQLite 单条插入不回填模型主键，统一以返回值兜底
		id := seed.ID
		if id == 0 {
			if id, err = res.LastInsertId(); err != nil {
				t.Fatalf("读取自增主键失败: %v", err)
			}
		}

		// 其余列不赋值，白名单只允许 name 被写回
		if _, err := sess.UPDATE_COLUMNS("name").UPDATE(&Account{ID: id, Name: "alice2"}); err != nil {
			t.Fatalf("%s: 白名单更新失败: %v", tc.Name, err)
		}

		var got Account
		if _, err := sess.WHERE(where.EQ("id", id)).FindFirst(&got); err != nil {
			t.Fatalf("查询更新结果失败: %v", err)
		}

		if got.Name != "alice2" || got.Age != 30 || got.Note != "keep" {
			t.Fatalf("%s: 白名单更新污染了其他列: %+v", tc.Name, got)
		}

		// 白名单内的零值同样写回
		if _, err := sess.UPDATE_COLUMNS("note").UPDATE(&Account{ID: id}); err != nil {
			t.Fatalf("%s: 白名单零值更新失败: %v", tc.Name, err)
		}

		if _, err := sess.WHERE(where.EQ("id", id)).FindFirst(&got); err != nil {
			t.Fatalf("查询零值更新结果失败: %v", err)
		}

		if got.Note != "" || got.Name != "alice2" {
			t.Fatalf("%s: 白名单零值未写回或越界写入: %+v", tc.Name, got)
		}

		if _, err := sess.UPDATE_COLUMNS("missing").UPDATE(&Account{ID: id}); !errors.Is(err, schema.ErrUnknownUpdateColumn) {
			t.Fatalf("%s: 未知列应返回 ErrUnknownUpdateColumn, got %v", tc.Name, err)
		}

		if _, err := sess.UPDATE_COLUMNS().UPDATE(&Account{ID: id}); !errors.Is(err, session.ErrUpdateColumnsEmpty) {
			t.Fatalf("%s: 空白名单应返回 ErrUpdateColumnsEmpty, got %v", tc.Name, err)
		}
	})
}

// TestCountRejectsGroup 验证 COUNT 不会静默丢弃 GROUP BY
func TestCountRejectsGroup(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		table := tableName("account")
		createTable(t, engine, table, &Account{})
		defer dropTable(t, engine, table)

		sess := engine.NewSession(&Account{})
		defer func() { _ = sess.Close() }()

		if _, err := sess.INSERT(false,
			&Account{Name: "a", Email: "a@example.com", Age: 1},
			&Account{Name: "b", Email: "b@example.com", Age: 1},
		); err != nil {
			t.Fatalf("插入种子数据失败: %v", err)
		}

		if _, _, err := sess.GROUP("age").COUNT(); !errors.Is(err, session.ErrCountWithGroupUnsupported) {
			t.Fatalf("%s: 带 GROUP 的 COUNT 应被拒绝, got %v", tc.Name, err)
		}
	})
}

// TestMigrateKeepsDroppedColumns 验证 Migrate 默认不删列，显式选项才执行 DROP
func TestMigrateKeepsDroppedColumns(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		table := tableName("account")
		createTable(t, engine, table, &Account{})
		defer dropTable(t, engine, table)

		// 模拟历史遗留列：模型中没有，数据库中存在
		execSQL(t, engine, "ALTER TABLE "+table+" ADD COLUMN legacy_col INT")

		if err := engine.Migrate(&Account{}); err != nil {
			t.Fatalf("%s: 默认迁移失败: %v", tc.Name, err)
		}

		if !hasTableColumn(t, engine, &Account{}, "legacy_col") {
			t.Fatalf("%s: 默认迁移不应删除模型之外的列", tc.Name)
		}

		if err := engine.Migrate(&Account{}, wxorm.WithDropMissingColumns()); err != nil {
			t.Fatalf("%s: 显式删列迁移失败: %v", tc.Name, err)
		}

		if hasTableColumn(t, engine, &Account{}, "legacy_col") {
			t.Fatalf("%s: WithDropMissingColumns 应删除模型之外的列", tc.Name)
		}
	})
}

// hasTableColumn 判断表中是否存在指定列
func hasTableColumn(t testing.TB, engine *wxorm.Engine, model any, column string) bool {
	t.Helper()

	sess := engine.NewSession(model)
	defer func() { _ = sess.Close() }()

	columns, err := sess.GetTableColumns()
	if err != nil {
		t.Fatalf("读取表列失败: %v", err)
	}

	for _, name := range columns {
		if name == column {
			return true
		}
	}

	return false
}
