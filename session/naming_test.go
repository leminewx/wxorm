package session

import (
	"errors"
	"sync"
	"testing"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/harness/examples"
	"gitee.com/leminewx/wxorm/naming"
	"gitee.com/leminewx/wxorm/schema"
)

// newNamingSession 新建带命名策略的会话（模型固定为 examples.Group）
//
// stmts 由调用方传入，便于验证「共享预编译缓存 + 不同前缀」不会互相串语句。
func newNamingSession(t *testing.T, stmts *sync.Map, strategy naming.Strategy) *Session {
	t.Helper()

	db, err := examples.SetupTestDB()
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}

	dial := dialect.Get("mysql")
	sch, err := schema.Parse(dial, &examples.Group{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	sess := NewSession(1, examples.DBName, db, dial, stmts)
	sess.SetNaming(strategy)
	sess.SetSchema(sch)
	return sess
}

// TestSessionNamingPrefix 验证前缀进入表名、建表与存在性判断
func TestSessionNamingPrefix(t *testing.T) {
	sess := newNamingSession(t, &sync.Map{}, naming.Strategy{Prefix: "tenant1_"})
	_ = sess.DeleteTable()
	if err := sess.CreateTable(); err != nil {
		t.Fatalf("CreateTable failed: %v", err)
	}
	defer func() { _ = sess.DeleteTable() }()

	if got := sess.GetTableName(); got != "tenant1_groups" {
		t.Errorf("GetTableName = %q, want %q", got, "tenant1_groups")
	}

	// 未指定 Schema 时不应拼出库名
	if got := sess.GetQualifiedTableName(); got != "tenant1_groups" {
		t.Errorf("GetQualifiedTableName = %q, want %q", got, "tenant1_groups")
	}

	exists, err := sess.ExistsTable()
	if err != nil {
		t.Fatalf("ExistsTable failed: %v", err)
	}

	if !exists {
		t.Fatal("带前缀的表应存在于数据库中")
	}

	// 换一个前缀就是另一张表，说明前缀真正落到了物理表名上
	other := newNamingSession(t, &sync.Map{}, naming.Strategy{Prefix: "tenant2_"})
	_ = other.DeleteTable()

	exists, err = other.ExistsTable()
	if err != nil {
		t.Fatalf("ExistsTable failed: %v", err)
	}

	if exists {
		t.Error("不同前缀应指向不同的物理表")
	}
}

// TestSessionNamingSchema 验证库名拼接只影响 SQL 中的限定名
func TestSessionNamingSchema(t *testing.T) {
	sess := newNamingSession(t, &sync.Map{}, naming.Strategy{Schema: "tenant1", Prefix: "t_"})

	if got := sess.GetTableName(); got != "t_groups" {
		t.Errorf("GetTableName = %q, want %q", got, "t_groups")
	}

	if got := sess.GetQualifiedTableName(); got != "tenant1.t_groups" {
		t.Errorf("GetQualifiedTableName = %q, want %q", got, "tenant1.t_groups")
	}
}

// TestSessionNamingInsertIsolation 验证共享预编译缓存时不同前缀互不串语句
func TestSessionNamingInsertIsolation(t *testing.T) {
	stmts := &sync.Map{}

	first := newNamingSession(t, stmts, naming.Strategy{Prefix: "tenant_a_"})
	second := newNamingSession(t, stmts, naming.Strategy{Prefix: "tenant_b_"})

	for _, sess := range []*Session{first, second} {
		_ = sess.DeleteTable()
		if err := sess.CreateTable(); err != nil {
			t.Fatalf("CreateTable(%s) failed: %v", sess.GetTableName(), err)
		}

		defer func(s *Session) { _ = s.DeleteTable() }(sess)
	}

	if _, err := first.INSERT(false, &examples.Group{Name: "a"}); err != nil {
		t.Fatalf("insert into tenant_a_ failed: %v", err)
	}

	if _, err := second.INSERT(false, &examples.Group{Name: "b"}); err != nil {
		t.Fatalf("insert into tenant_b_ failed: %v", err)
	}

	for _, tc := range []struct {
		sess *Session
		want string
	}{
		{first, "a"},
		{second, "b"},
	} {
		var groups []examples.Group
		if _, err := tc.sess.SELECT(&groups); err != nil {
			t.Fatalf("select from %s failed: %v", tc.sess.GetTableName(), err)
		}

		if len(groups) != 1 || groups[0].Name != tc.want {
			t.Fatalf("%s 应只有 1 行 %q，实际 %#v", tc.sess.GetTableName(), tc.want, groups)
		}
	}
}

// TestSessionNamingInvalid 验证非法命名策略不会被静默忽略
func TestSessionNamingInvalid(t *testing.T) {
	// 点号会把前缀变成多段标识符，必须拒绝
	sess := newNamingSession(t, &sync.Map{}, naming.Strategy{Prefix: "tenant.1"})

	if _, err := sess.GetSchema(); !errors.Is(err, naming.ErrInvalidSegment) {
		t.Errorf("GetSchema 应返回 ErrInvalidSegment，实际: %v", err)
	}

	if _, err := sess.ExistsTable(); !errors.Is(err, naming.ErrInvalidSegment) {
		t.Errorf("ExistsTable 应返回 ErrInvalidSegment，实际: %v", err)
	}

	if err := sess.CreateTable(); !errors.Is(err, naming.ErrInvalidSegment) {
		t.Errorf("CreateTable 应返回 ErrInvalidSegment，实际: %v", err)
	}
}

// TestSessionNamingZero 验证零值策略不改变既有行为
func TestSessionNamingZero(t *testing.T) {
	sess := newNamingSession(t, &sync.Map{}, naming.Strategy{})

	if got := sess.GetTableName(); got != "groups" {
		t.Errorf("GetTableName = %q, want %q", got, "groups")
	}

	if !sess.GetNaming().IsZero() {
		t.Error("零值策略应为 IsZero")
	}
}

// TestSessionNamespace 验证元数据查询的库名只来自命名策略
//
// 不能用连接串里的库名兜底：MySQL 的库就是 schema，而 PostgreSQL 的库与 schema
// 是两个层级，把库名当成 schema 会查不到任何表（真库回归曾因此漏判表已存在）。
func TestSessionNamespace(t *testing.T) {
	sess := newNamingSession(t, &sync.Map{}, naming.Strategy{})
	if got := sess.namespace(); got != "" {
		t.Errorf("零值策略的 namespace 应为空（交给方言回落），实际 %q", got)
	}

	sess.SetNaming(naming.Strategy{Schema: "tenant1"})
	if got := sess.namespace(); got != "tenant1" {
		t.Errorf("namespace = %q, want %q", got, "tenant1")
	}
}
