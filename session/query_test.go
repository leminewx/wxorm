package session

import (
	"errors"
	"strings"
	"testing"

	"gitee.com/leminewx/wxorm/harness/examples"
	"gitee.com/leminewx/wxorm/statement"
	"gitee.com/leminewx/wxorm/statement/where"
)

// groupNameOnly 与 examples.Group 映射同一张表，但只映射部分列
// 用于验证同一张表的两个模型不会共用同一条预编译语句
type groupNameOnly struct {
	ID   int64  `orm:"column(id) pk auto"`
	Name string `orm:"column(name) notnull"`
}

func (own *groupNameOnly) GetTableName() string { return "groups" }

// TestSession_First 测试 First 命中与未命中的语义
func TestSession_First(t *testing.T) {
	sess := setupGroupSession(t)
	defer teardownSession(sess)

	id := testInsertGroup(sess, "研发部", "admin")

	// 命中
	var found examples.Group
	if _, err := sess.WHERE(where.EQ("id", id)).FindFirst(&found); err != nil {
		t.Fatalf("first failed: %v", err)
	}
	if found.ID != id || found.Name != "研发部" {
		t.Errorf("expected (%d, 研发部), got (%d, %s)", id, found.ID, found.Name)
	}

	// 未命中：必须返回 ErrRecordNotFound，而不是零值 + nil error
	var missing examples.Group
	if _, err := sess.WHERE(where.EQ("id", id+1000)).FindFirst(&missing); !errors.Is(err, ErrRecordNotFound) {
		t.Errorf("expected ErrRecordNotFound, got %v", err)
	}

	// 非指针参数
	if _, err := sess.FindFirst(examples.Group{}); !errors.Is(err, ErrSelectValuesNotPointer) {
		t.Errorf("expected ErrSelectValuesNotPointer, got %v", err)
	}
}

// TestSession_First_DirectScan 验证单行直扫：支持指定列、未查询列归零、LIMIT 固定为 1
func TestSession_First_DirectScan(t *testing.T) {
	sess := setupGroupSession(t)
	defer teardownSession(sess)

	id := testInsertGroup(sess, "研发部", "admin")

	// 预置旧值：单行查询应整体覆盖，未查询的列回到零值
	group := examples.Group{CreatedBy: "stale", Name: "stale"}
	res, err := sess.COLUMNS("id", "name").WHERE(where.EQ("id", id)).FindFirst(&group)
	if err != nil {
		t.Fatalf("find first with columns failed: %v", err)
	}
	if group.ID != id || group.Name != "研发部" {
		t.Errorf("expected (%d, 研发部), got (%d, %s)", id, group.ID, group.Name)
	}
	if group.CreatedBy != "" {
		t.Errorf("expected created_by to be cleared, got %q", group.CreatedBy)
	}
	if sql := strings.Join(strings.Fields(res.GetExecutionSQL()), " "); !strings.Contains(strings.ToUpper(sql), "LIMIT 1") {
		t.Errorf("expected LIMIT 1 in sql: %s", sql)
	}

	// 调用方设置的 LIMIT 不影响单行查询，也不会残留到下一条语句
	var limited examples.Group
	if _, err := sess.LIMIT(10).WHERE(where.EQ("id", id)).FindFirst(&limited); err != nil {
		t.Fatalf("find first with limit failed: %v", err)
	}
	if limited.ID != id {
		t.Errorf("expected id %d, got %d", id, limited.ID)
	}
	if sess.limit != 0 {
		t.Errorf("expected limit reset after query, got %d", sess.limit)
	}
}

// TestSession_LimitNegative 验证负数 LIMIT 在构建 SQL 阶段报错并向上传递
//
// 负数不能退化成「不带 LIMIT」：分页参数算错时会静默查全表。
func TestSession_LimitNegative(t *testing.T) {
	sess := setupGroupSession(t)
	defer teardownSession(sess)

	testInsertGroup(sess, "研发部", "admin")

	var groups []examples.Group
	res, err := sess.LIMIT(-1).SELECT(&groups)
	if !errors.Is(err, statement.ErrLimitNegative) {
		t.Fatalf("err = %v, want statement.ErrLimitNegative", err)
	}
	if res != nil {
		t.Fatalf("res = %v, want nil on build error", res)
	}
}

// TestSession_Exist 测试存在性判断
func TestSession_Exist(t *testing.T) {
	sess := setupGroupSession(t)
	defer teardownSession(sess)

	testInsertGroup(sess, "研发部", "admin")

	exist, err := sess.WHERE(where.EQ("name", "研发部")).Exist()
	if err != nil {
		t.Fatalf("exist failed: %v", err)
	}
	if !exist {
		t.Error("expected record exists")
	}

	exist, err = sess.WHERE(where.EQ("name", "不存在的部门")).Exist()
	if err != nil {
		t.Fatalf("exist failed: %v", err)
	}
	if exist {
		t.Error("expected record not exists")
	}
}

// TestSession_Columns 测试只查询指定列
func TestSession_Columns(t *testing.T) {
	sess := setupGroupSession(t)
	defer teardownSession(sess)

	id := testInsertGroup(sess, "研发部", "admin")

	var group examples.Group
	res, err := sess.COLUMNS("id", "name").WHERE(where.EQ("id", id)).SELECT(&group)
	if err != nil {
		t.Fatalf("select columns failed: %v", err)
	}
	if group.ID != id || group.Name != "研发部" {
		t.Errorf("expected (%d, 研发部), got (%d, %s)", id, group.ID, group.Name)
	}
	if group.CreatedBy != "" {
		t.Errorf("expected created_by to stay zero, got %q", group.CreatedBy)
	}

	if got := res.GetExecutionSQL(); got == "" || !strings.Contains(got, "`name`") {
		t.Errorf("unexpected sql: %s", got)
	}

	// 列名不在模型中：必须报错，而不是让字段静默错位
	if _, err := sess.COLUMNS("not_exists").SELECT(&group); !errors.Is(err, ErrSelectColumnNotFound) {
		t.Errorf("expected ErrSelectColumnNotFound, got %v", err)
	}
}

// TestSession_InsertCacheIsolation 测试同一张表的两个模型不会共用预编译语句
func TestSession_InsertCacheIsolation(t *testing.T) {
	sess := setupGroupSession(t)
	defer teardownSession(sess)

	// 先用完整模型建立 INSERT 预编译语句（5 个可插入列）
	full := &examples.Group{Name: "研发部", CreatedBy: "admin"}
	if _, err := sess.INSERT(false, full); err != nil {
		t.Fatalf("insert full model failed: %v", err)
	}

	// 切换到同表的窄模型（2 个可插入列），复用同一个预编译缓存
	sess.UseModel(&groupNameOnly{})
	narrow := &groupNameOnly{Name: "市场部"}
	if _, err := sess.INSERT(false, narrow); err != nil {
		t.Fatalf("insert narrow model failed: %v", err)
	}

	// 窄模型读取，确认写入的是它自己的列集
	var got groupNameOnly
	if _, err := sess.WHERE(where.EQ("name", "市场部")).FindFirst(&got); err != nil {
		t.Fatalf("select narrow model failed: %v", err)
	}
	if got.Name != "市场部" {
		t.Errorf("expected 市场部, got %s", got.Name)
	}
}

// TestSession_SelectPrealloc 验证按 LIMIT 预分配容量时保留调用方已有元素
func TestSession_SelectPrealloc(t *testing.T) {
	sess := setupGroupSession(t)
	defer teardownSession(sess)

	for _, name := range []string{"研发部", "市场部", "行政部"} {
		testInsertGroup(sess, name, "admin")
	}

	// 预置一个元素：查询结果应追加在其后，而不是覆盖或丢弃
	dest := []examples.Group{{Name: "调用方预置"}}
	if _, err := sess.ORDER("id").LIMIT(2).SELECT(&dest); err != nil {
		t.Fatalf("查询失败: %v", err)
	}

	if len(dest) != 3 {
		t.Fatalf("结果行数 = %d, want 3（保留 1 行预置 + 追加 2 行）", len(dest))
	}

	if dest[0].Name != "调用方预置" {
		t.Fatalf("预置元素丢失: %q", dest[0].Name)
	}

	if dest[1].Name != "研发部" || dest[2].Name != "市场部" {
		t.Fatalf("追加顺序不符: %q, %q", dest[1].Name, dest[2].Name)
	}
}

// TestSession_SetTimeout_Disabled 测试关闭默认超时后仍可正常执行
func TestSession_SetTimeout_Disabled(t *testing.T) {
	sess := setupGroupSession(t)
	defer teardownSession(sess)

	sess.SetTimeout(0)
	if sess.timeout != 0 {
		t.Errorf("expected timeout to be disabled, got %v", sess.timeout)
	}

	id := testInsertGroup(sess, "研发部", "admin")
	var group examples.Group
	if _, err := sess.WHERE(where.EQ("id", id)).FindFirst(&group); err != nil {
		t.Fatalf("query with disabled timeout failed: %v", err)
	}
}
