package session

import (
	"errors"
	"strings"
	"testing"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/harness/examples"
	"gitee.com/leminewx/wxorm/naming"
	"gitee.com/leminewx/wxorm/schema"
	"gitee.com/leminewx/wxorm/statement"
	"gitee.com/leminewx/wxorm/statement/where"
)

// articleWithAuthor 用联表字段扩展示例模型：join 字段只参与查询投影
type articleWithAuthor struct {
	ID         int64   `wxorm:"column(id) pk auto"`
	Title      string  `wxorm:"column(title) notnull"`
	AuthorID   int64   `wxorm:"column(author_id)"`
	AuthorName *string `wxorm:"join(author.name)"`
}

func (*articleWithAuthor) GetTableName() string { return "articles" }

// articleWithStrictAuthor 的联表字段不可空，用于验证外连接的 NULL 校验
type articleWithStrictAuthor struct {
	ID         int64  `wxorm:"column(id) pk auto"`
	AuthorName string `wxorm:"join(author.name)"`
}

func (*articleWithStrictAuthor) GetTableName() string { return "strict_articles" }

// joinedCompositeModel 是复合主键的联表模型，用于验证联表统计的主键约束
type joinedCompositeModel struct {
	ID         int64   `wxorm:"column(id) pk auto"`
	TenantID   int64   `wxorm:"column(tenant_id) pk"`
	AuthorName *string `wxorm:"join(author.name)"`
}

func (*joinedCompositeModel) GetTableName() string { return "composite_articles" }

// newJoinTestSession 基于模型创建不连数据库的会话，只用于构建 SQL 与校验参数
func newJoinTestSession(t *testing.T, model any) *Session {
	t.Helper()

	dial := dialect.Get("mysql")
	sch, err := schema.Parse(dial, model)
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	sess := NewSession(0, "", nil, dial, nil)
	sess.SetSchema(sch)
	return sess
}

// buildJoinSelect 构建 SELECT 并清理语句状态，便于断言纯文本结果
func buildJoinSelect(t *testing.T, sess *Session) (string, []any, bool) {
	t.Helper()

	query, args, _, requested, _, err := sess.buildSelect()
	sess.statement.Reset()
	if err != nil {
		t.Fatalf("build select failed: %v", err)
	}

	return query, args, requested
}

// TestSession_JoinSQL 验证联表 SQL 的投影、子句顺序与参数顺序
func TestSession_JoinSQL(t *testing.T) {
	sess := newJoinTestSession(t, &articleWithAuthor{})
	sess.LEFT_JOIN("authors", "author", where.EQ_COLUMN("articles.author_id", "author.id"))
	sess.WHERE(where.EQ("author.name", "张三"))

	query, args, requested := buildJoinSelect(t, sess)

	if !requested {
		t.Error("联表查询必须按返回列名映射结果（requested = true）")
	}

	// 主表列用物理表名限定，联表列固定别名为 <alias>_<column>
	wants := []string{
		"SELECT\n    `articles`.`id`, `articles`.`title`, `articles`.`author_id`",
		"`author`.`name` AS `author_name`",
		"\nFROM\n    `articles`",
		"\nLEFT JOIN\n    `authors` AS `author`",
		"\nON\n    `articles`.`author_id` = `author`.`id`",
		"\nWHERE\n    `author`.`name` = ?",
	}
	for _, want := range wants {
		if !strings.Contains(query, want) {
			t.Errorf("SQL 缺少片段 %q：\n%s", want, query)
		}
	}

	// 顺序必须是 FROM → JOIN → ON → WHERE，参数才会落在正确位置
	from := strings.Index(query, "\nFROM\n")
	join := strings.Index(query, "\nLEFT JOIN\n")
	on := strings.Index(query, "\nON\n")
	filter := strings.Index(query, "\nWHERE\n")
	if !(from >= 0 && from < join && join < on && on < filter) {
		t.Errorf("子句顺序错误 FROM=%d JOIN=%d ON=%d WHERE=%d：\n%s", from, join, on, filter, query)
	}

	if len(args) != 1 || args[0] != "张三" {
		t.Errorf("args = %v, want [张三]", args)
	}

	// JOIN 与 columns/limit 同样「取走即归零」
	if len(sess.joins) != 0 || sess.joinErr != nil {
		t.Errorf("JOIN 状态未清空：joins=%v err=%v", sess.joins, sess.joinErr)
	}
}

// TestSession_JoinNaming 验证关联表同样套用命名策略，别名不参与改写
func TestSession_JoinNaming(t *testing.T) {
	sess := newJoinTestSession(t, &examples.Group{})
	sess.SetNaming(naming.Strategy{Prefix: "t_", Suffix: "_v2"})
	sess.INNER_JOIN("users", "u", where.EQ_COLUMN("t_groups_v2.id", "u.group_id"))

	query, _, _ := buildJoinSelect(t, sess)

	for _, want := range []string{
		"\nFROM\n    `t_groups_v2`",
		"\nINNER JOIN\n    `t_users_v2` AS `u`",
	} {
		if !strings.Contains(query, want) {
			t.Errorf("SQL 缺少片段 %q：\n%s", want, query)
		}
	}
	if strings.Contains(query, "`t_u`") {
		t.Errorf("别名不应套用命名策略：\n%s", query)
	}
}

// TestSession_JoinValidation 验证 JOIN 参数在构建 SQL 时报错，且不残留下一次查询
func TestSession_JoinValidation(t *testing.T) {
	on := where.EQ_COLUMN("groups.id", "u.group_id")

	cases := []struct {
		name string
		join func(*Session)
		want error
	}{
		{"empty table", func(s *Session) { s.JOIN(clause.JOIN_INNER, "", "u", on) }, ErrJoinTableRequired},
		{"empty alias", func(s *Session) { s.JOIN(clause.JOIN_INNER, "users", "", on) }, ErrJoinAliasRequired},
		{"dotted alias", func(s *Session) { s.JOIN(clause.JOIN_INNER, "users", "u.x", on) }, ErrJoinAliasInvalid},
		{"missing on", func(s *Session) { s.INNER_JOIN("users", "u", nil) }, statement.ErrJoinConditionRequired},
		{"cross with on", func(s *Session) { s.JOIN(clause.JOIN_CROSS, "users", "u", on) }, statement.ErrJoinConditionNotAllowed},
		{"unknown kind", func(s *Session) { s.JOIN(clause.JoinKind("OUTER"), "users", "u", on) }, clause.ErrInvalidJoinKind},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			sess := newJoinTestSession(t, &examples.Group{})
			tt.join(sess)

			if _, _, _, _, _, err := sess.buildSelect(); !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}

			// 校验失败后状态已清空，下一次查询不受影响
			sess.statement.Reset()
			if _, _, _, _, _, err := sess.buildSelect(); err != nil {
				t.Errorf("JOIN 错误泄漏到下一次查询：%v", err)
			}
			sess.statement.Reset()
		})
	}
}

// TestSession_JoinUnsupportedKind 验证方言不支持的类型在构建期报错
func TestSession_JoinUnsupportedKind(t *testing.T) {
	sess := newJoinTestSession(t, &examples.Group{})
	sess.JOIN(clause.JOIN_FULL, "users", "u", where.EQ_COLUMN("groups.id", "u.group_id"))

	_, _, _, _, _, err := sess.buildSelect()
	sess.statement.Reset()
	if !errors.Is(err, clause.ErrUnsupportedFeature) {
		t.Fatalf("expected ErrUnsupportedFeature, got %v", err)
	}
}

// TestSession_JoinWriteGuard 验证写语句拒绝执行并清空 JOIN
func TestSession_JoinWriteGuard(t *testing.T) {
	on := where.EQ_COLUMN("articles.author_id", "author.id")

	cases := []struct {
		name string
		exec func(*Session) error
	}{
		{"insert", func(s *Session) error {
			_, err := s.INSERT(false, &articleWithAuthor{Title: "Go 实战"})
			return err
		}},
		{"update", func(s *Session) error {
			_, err := s.UPDATE(map[string]any{"title": "Go 实战"})
			return err
		}},
		{"delete", func(s *Session) error {
			_, err := s.DELETE()
			return err
		}},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			sess := newJoinTestSession(t, &articleWithAuthor{})
			sess.LEFT_JOIN("authors", "author", on)

			if err := tt.exec(sess); !errors.Is(err, ErrJoinUnsupported) {
				t.Fatalf("expected ErrJoinUnsupported, got %v", err)
			}

			if len(sess.joins) != 0 || sess.joinErr != nil {
				t.Errorf("拒绝后 JOIN 状态未清空：joins=%v err=%v", sess.joins, sess.joinErr)
			}
		})
	}
}

// TestSession_JoinLifecycle 验证 JOIN 只对当前查询生效
func TestSession_JoinLifecycle(t *testing.T) {
	sess := newJoinTestSession(t, &examples.Group{})
	sess.LEFT_JOIN("users", "u", where.EQ_COLUMN("groups.id", "u.group_id"))

	if query, _, _ := buildJoinSelect(t, sess); !strings.Contains(query, "LEFT JOIN") {
		t.Fatalf("第一次查询应包含 JOIN：\n%s", query)
	}

	query, _, _ := buildJoinSelect(t, sess)
	if strings.Contains(strings.ToUpper(query), "JOIN") {
		t.Errorf("JOIN 泄漏到第二次查询：\n%s", query)
	}
}

// TestSession_JoinReset 验证 Reset（会话池归还路径）会清空 JOIN 与校验错误
func TestSession_JoinReset(t *testing.T) {
	sess := newJoinTestSession(t, &examples.Group{})
	sess.LEFT_JOIN("users", "u", where.EQ_COLUMN("groups.id", "u.group_id"))
	sess.Reset()

	if len(sess.joins) != 0 || sess.joinErr != nil {
		t.Fatalf("Reset 未清空 JOIN 状态：joins=%v err=%v", sess.joins, sess.joinErr)
	}

	// 模拟池化归还后再借出：新模型不应带上上一个使用者的 JOIN
	sch, err := schema.Parse(dialect.Get("mysql"), &examples.Group{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}
	sess.SetSchema(sch)

	query, _, _ := buildJoinSelect(t, sess)
	if strings.Contains(strings.ToUpper(query), "JOIN") {
		t.Errorf("JOIN 泄漏到复用后的会话：\n%s", query)
	}
}

// TestSession_JoinColumns 验证 COLUMNS 可同时指定主表列与联表结果列
func TestSession_JoinColumns(t *testing.T) {
	sess := newJoinTestSession(t, &articleWithAuthor{})
	sess.COLUMNS("id", "author_name")
	sess.INNER_JOIN("authors", "author", where.EQ_COLUMN("articles.author_id", "author.id"))

	query, _, requested := buildJoinSelect(t, sess)
	if !requested {
		t.Error("指定列后 requested 应为 true")
	}

	if want := "SELECT\n    `articles`.`id`, `author`.`name` AS `author_name`\n"; !strings.Contains(query, want) {
		t.Errorf("SQL 缺少片段 %q：\n%s", want, query)
	}

	// 未知列必须报错，而不是静默少列
	sess = newJoinTestSession(t, &articleWithAuthor{})
	sess.COLUMNS("author_email")
	sess.INNER_JOIN("authors", "author", where.EQ_COLUMN("articles.author_id", "author.id"))
	if _, _, _, _, _, err := sess.buildSelect(); !errors.Is(err, ErrSelectColumnNotFound) {
		t.Fatalf("expected ErrSelectColumnNotFound, got %v", err)
	}
	sess.statement.Reset()
}

// TestSession_JoinNullable 验证外连接要求联表字段可空，内连接不限制
func TestSession_JoinNullable(t *testing.T) {
	on := where.EQ_COLUMN("strict_articles.id", "author.id")

	sess := newJoinTestSession(t, &articleWithStrictAuthor{})
	sess.LEFT_JOIN("authors", "author", on)
	if _, _, _, _, _, err := sess.buildSelect(); !errors.Is(err, ErrJoinColumnNotNilable) {
		t.Fatalf("expected ErrJoinColumnNotNilable, got %v", err)
	}
	sess.statement.Reset()

	sess = newJoinTestSession(t, &articleWithStrictAuthor{})
	sess.INNER_JOIN("authors", "author", on)
	if _, _, _, _, _, err := sess.buildSelect(); err != nil {
		t.Fatalf("INNER JOIN 不应限制可空性：%v", err)
	}
	sess.statement.Reset()
}

// TestSession_JoinAliasNotJoined 验证模型声明的联表别名必须在本次查询中出现
func TestSession_JoinAliasNotJoined(t *testing.T) {
	sess := newJoinTestSession(t, &articleWithAuthor{})
	sess.INNER_JOIN("authors", "a", where.EQ_COLUMN("articles.author_id", "a.id"))

	_, _, _, _, _, err := sess.buildSelect()
	sess.statement.Reset()
	if !errors.Is(err, ErrJoinAliasNotJoined) {
		t.Fatalf("expected ErrJoinAliasNotJoined, got %v", err)
	}
}

// TestSession_JoinCountGuards 验证联表统计的列参数与主键约束
func TestSession_JoinCountGuards(t *testing.T) {
	sess := newJoinTestSession(t, &articleWithAuthor{})
	sess.LEFT_JOIN("authors", "author", where.EQ_COLUMN("articles.author_id", "author.id"))
	if _, _, err := sess.COUNT("id"); !errors.Is(err, ErrJoinCountColumnUnsupported) {
		t.Fatalf("expected ErrJoinCountColumnUnsupported, got %v", err)
	}

	sess = newJoinTestSession(t, &joinedCompositeModel{})
	sess.LEFT_JOIN("authors", "author", where.EQ_COLUMN("composite_articles.id", "author.id"))
	if _, _, err := sess.COUNT(); !errors.Is(err, ErrJoinRequiresSinglePK) {
		t.Fatalf("expected ErrJoinRequiresSinglePK, got %v", err)
	}
}
