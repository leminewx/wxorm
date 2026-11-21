package session

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/harness/examples"
	"gitee.com/leminewx/wxorm/harness/examples/stubdriver"
	"gitee.com/leminewx/wxorm/schema"
	"gitee.com/leminewx/wxorm/statement/where"
)

type preloadAuthor struct {
	ID   int64  `wxorm:"column(id) pk auto"`
	Name string `wxorm:"column(name) notnull"`
}

func (*preloadAuthor) GetTableName() string { return "session_preload_authors" }

type preloadComment struct {
	ID        int64  `wxorm:"column(id) pk auto"`
	ArticleID int64  `wxorm:"column(article_id) index"`
	Content   string `wxorm:"column(content)"`
}

func (*preloadComment) GetTableName() string { return "session_preload_comments" }

type preloadArticle struct {
	ID       int64            `wxorm:"column(id) pk auto"`
	Title    string           `wxorm:"column(title) notnull"`
	AuthorID int64            `wxorm:"column(author_id)"`
	Author   *preloadAuthor   `wxorm:"preload(author_id)"`
	Comments []preloadComment `wxorm:"preload(article_id)"`
}

func (*preloadArticle) GetTableName() string { return "session_preload_articles" }

// TestSession_Preload belongs_to 与 has_many 在内存驱动上端到端验证
func TestSession_Preload(t *testing.T) {
	articles, authors, comments := setupPreloadFixture(t)

	zhang := insertPreloadAuthor(t, authors, "张三")
	li := insertPreloadAuthor(t, authors, "李四")

	first := insertPreloadArticle(t, articles, "第一篇", zhang)
	second := insertPreloadArticle(t, articles, "第二篇", li)
	insertPreloadArticle(t, articles, "无人认领", 9999)

	insertPreloadComment(t, comments, first, "评论一")
	insertPreloadComment(t, comments, first, "评论二")

	var got []preloadArticle
	if _, err := articles.PRELOAD("Author", "Comments").ORDER("id").SELECT(&got); err != nil {
		t.Fatalf("select with preload failed: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("rows = %d, want 3", len(got))
	}

	if got[0].Author == nil || got[0].Author.Name != "张三" {
		t.Fatalf("first author = %+v, want 张三", got[0].Author)
	}
	if got[1].Author == nil || got[1].Author.Name != "李四" {
		t.Fatalf("second author = %+v, want 李四", got[1].Author)
	}
	if got[2].Author != nil {
		t.Fatalf("unmatched author = %+v, want nil", got[2].Author)
	}

	if len(got[0].Comments) != 2 || got[0].Comments[0].Content != "评论一" {
		t.Fatalf("first comments = %+v", got[0].Comments)
	}
	if got[1].Comments == nil {
		t.Fatal("empty comments must be an empty slice, not nil")
	}
	if len(got[1].Comments) != 0 {
		t.Fatalf("second comments = %+v, want empty", got[1].Comments)
	}
	if second == 0 {
		t.Fatal("second article id is zero")
	}

	// 未请求的关联不应被加载
	var onlyAuthor []preloadArticle
	if _, err := articles.PRELOAD("Author").ORDER("id").LIMIT(1).SELECT(&onlyAuthor); err != nil {
		t.Fatalf("select only author failed: %v", err)
	}
	if onlyAuthor[0].Comments != nil {
		t.Fatalf("comments = %+v, want nil when not requested", onlyAuthor[0].Comments)
	}
}

// 单行查询同样支持预加载
func TestSession_PreloadFindFirst(t *testing.T) {
	articles, authors, comments := setupPreloadFixture(t)

	zhang := insertPreloadAuthor(t, authors, "张三")
	articleID := insertPreloadArticle(t, articles, "第一篇", zhang)
	insertPreloadComment(t, comments, articleID, "评论一")

	var got preloadArticle
	if _, err := articles.PRELOAD("Author", "Comments").WHERE(where.EQ("id", articleID)).FindFirst(&got); err != nil {
		t.Fatalf("find first with preload failed: %v", err)
	}

	if got.Author == nil || got.Author.Name != "张三" {
		t.Fatalf("author = %+v", got.Author)
	}
	if len(got.Comments) != 1 || got.Comments[0].Content != "评论一" {
		t.Fatalf("comments = %+v", got.Comments)
	}
}

// 子查询的 SQL 形态与参数个数必须稳定
func TestSession_PreloadSQL(t *testing.T) {
	dial := dialect.Get("mysql")
	sch, err := schema.Parse(dial, &preloadArticle{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	db, err := sql.Open(stubdriver.DriverName, "preload-sql")
	if err != nil {
		t.Fatalf("open stub failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	sess := NewSession(1, "stub", db, dial, &sync.Map{})
	sess.SetSchema(sch)

	stubdriver.Reset()
	stubdriver.Push([]string{"id", "title", "author_id"}, []driver.Value{int64(1), "A1", int64(7)})
	stubdriver.Push([]string{"id", "name"}, []driver.Value{int64(7), "张三"})

	var got []preloadArticle
	if _, err := sess.PRELOAD("Author").SELECT(&got); err != nil {
		t.Fatalf("select failed: %v", err)
	}

	calls := stubdriver.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2\n%v", len(calls), calls)
	}

	query := calls[1]
	for _, fragment := range []string{"`id` IN (?)", "ORDER BY", "session_preload_authors"} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("subquery missing %q:\n%s", fragment, query)
		}
	}
}

// 预加载的校验与写语句守卫
func TestSession_PreloadValidation(t *testing.T) {
	dial := dialect.Get("mysql")
	sch, err := schema.Parse(dial, &preloadArticle{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	db, err := sql.Open(stubdriver.DriverName, "preload-validation")
	if err != nil {
		t.Fatalf("open stub failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	newSession := func() *Session {
		sess := NewSession(1, "stub", db, dial, &sync.Map{})
		sess.SetSchema(sch)
		return sess
	}

	t.Run("未知字段", func(t *testing.T) {
		sess := newSession()
		stubdriver.Reset()
		stubdriver.Push([]string{"id", "title", "author_id"}, []driver.Value{int64(1), "A1", int64(7)})

		var got []preloadArticle
		if _, err := sess.PRELOAD("Missing").SELECT(&got); !errors.Is(err, schema.ErrPreloadUnknownField) {
			t.Fatalf("err = %v, want ErrPreloadUnknownField", err)
		}
	})

	t.Run("COLUMNS 缺少分组键", func(t *testing.T) {
		sess := newSession()
		stubdriver.Reset()
		stubdriver.Push([]string{"title"}, []driver.Value{"A1"})

		var got []preloadArticle
		if _, err := sess.COLUMNS("title").PRELOAD("Author").SELECT(&got); !errors.Is(err, ErrPreloadKeyNotSelected) {
			t.Fatalf("err = %v, want ErrPreloadKeyNotSelected", err)
		}
	})

	t.Run("写语句拒绝", func(t *testing.T) {
		sess := newSession()
		if _, err := sess.PRELOAD("Author").INSERT(false, &preloadArticle{Title: "x"}); !errors.Is(err, ErrPreloadUnsupported) {
			t.Fatalf("err = %v, want ErrPreloadUnsupported", err)
		}
		if sess.preloadSet {
			t.Fatal("preload state must be cleared after the write guard")
		}
	})

	t.Run("COUNT 丢弃声明", func(t *testing.T) {
		sess := newSession()
		stubdriver.Reset()
		stubdriver.Push([]string{"count"}, []driver.Value{int64(0)})

		if _, _, err := sess.PRELOAD("Author").COUNT(); err != nil {
			t.Fatalf("count failed: %v", err)
		}
		if sess.preloadSet {
			t.Fatal("preload state must be cleared by COUNT")
		}
	})
}

// 预加载状态必须在一次查询后归零
func TestSession_PreloadLifecycle(t *testing.T) {
	dial := dialect.Get("mysql")
	sch, err := schema.Parse(dial, &preloadArticle{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	db, err := sql.Open(stubdriver.DriverName, "preload-lifecycle")
	if err != nil {
		t.Fatalf("open stub failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	sess := NewSession(1, "stub", db, dial, &sync.Map{})
	sess.SetSchema(sch)

	stubdriver.Reset()
	stubdriver.Push([]string{"id", "title", "author_id"}, []driver.Value{int64(1), "A1", int64(7)})
	stubdriver.Push([]string{"id", "name"}, []driver.Value{int64(7), "张三"})

	var got []preloadArticle
	if _, err := sess.PRELOAD("Author").SELECT(&got); err != nil {
		t.Fatalf("first select failed: %v", err)
	}

	stubdriver.Push([]string{"id", "title", "author_id"}, []driver.Value{int64(2), "A2", int64(8)})
	if _, err := sess.SELECT(&got); err != nil {
		t.Fatalf("second select failed: %v", err)
	}

	calls := stubdriver.Calls()
	if len(calls) != 3 {
		t.Fatalf("calls = %d, want 3 (second select must not preload)\n%v", len(calls), calls)
	}

	// Reset 后同样不能残留
	sess.PRELOAD("Author")
	sess.Reset()
	if sess.preloadSet || sess.preloadAll || len(sess.preloads) != 0 {
		t.Fatal("reset must clear preload state")
	}
}

// ---------------------------------------------------------------------------
// 测试数据准备
// ---------------------------------------------------------------------------

// setupPreloadFixture 建好三张表并返回各自的会话
func setupPreloadFixture(t *testing.T) (articles, authors, comments *Session) {
	t.Helper()

	db, err := examples.SetupTestDB()
	if err != nil {
		t.Fatalf("connect fake database failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dial := dialect.Get("mysql")
	authors = createPreloadTable(t, db, dial, &preloadAuthor{})
	comments = createPreloadTable(t, db, dial, &preloadComment{})
	articles = createPreloadTable(t, db, dial, &preloadArticle{})

	return articles, authors, comments
}

// createPreloadTable 解析模型并重建数据表
func createPreloadTable(t *testing.T, db *sql.DB, dial dialect.Dialect, model any) *Session {
	t.Helper()

	sch, err := schema.Parse(dial, model)
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	sess := NewSession(1, examples.DBName, db, dial, &sync.Map{})
	sess.SetSchema(sch)

	_ = sess.DeleteTable()
	if err := sess.CreateTable(); err != nil {
		t.Fatalf("create table %s failed: %v", sch.TableName, err)
	}

	t.Cleanup(func() { _ = sess.DeleteTable() })
	return sess
}

func insertPreloadAuthor(t *testing.T, sess *Session, name string) int64 {
	t.Helper()

	res, err := sess.INSERT(false, &preloadAuthor{Name: name})
	if err != nil {
		t.Fatalf("insert author failed: %v", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id failed: %v", err)
	}

	return id
}

func insertPreloadArticle(t *testing.T, sess *Session, title string, authorID int64) int64 {
	t.Helper()

	res, err := sess.INSERT(false, &preloadArticle{Title: title, AuthorID: authorID})
	if err != nil {
		t.Fatalf("insert article failed: %v", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id failed: %v", err)
	}

	return id
}

func insertPreloadComment(t *testing.T, sess *Session, articleID int64, content string) {
	t.Helper()

	if _, err := sess.INSERT(false, &preloadComment{ArticleID: articleID, Content: content}); err != nil {
		t.Fatalf("insert comment failed: %v", err)
	}
}

// 超过单批参数上限时必须拆成多条子查询
func TestSession_PreloadBatching(t *testing.T) {
	dial := dialect.Get("mysql")
	sch, err := schema.Parse(dial, &preloadArticle{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	db, err := sql.Open(stubdriver.DriverName, "preload-batch")
	if err != nil {
		t.Fatalf("open stub failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	sess := NewSession(1, "stub", db, dial, &sync.Map{})
	sess.SetSchema(sch)

	stubdriver.Reset()
	stubdriver.Push([]string{"id", "name"})
	stubdriver.Push([]string{"id", "name"})

	keyIndex := sch.ColumnIndexesByName["author_id"]
	parents := make([]reflect.Value, 0, maxPreloadBatchParams+1)
	for i := 0; i <= maxPreloadBatchParams; i++ {
		value := reflect.New(reflect.TypeOf(preloadArticle{})).Elem()
		value.Field(keyIndex).SetInt(int64(i + 1))
		parents = append(parents, value)
	}

	sess.PRELOAD("Author")
	if err := sess.loadPreloads(parents, nil); err != nil {
		t.Fatalf("load preloads failed: %v", err)
	}

	counts := stubdriver.ArgCounts()
	if len(counts) != 2 {
		t.Fatalf("queries = %d, want 2", len(counts))
	}
	if counts[0] != maxPreloadBatchParams || counts[1] != 1 {
		t.Fatalf("args = %v, want [%d 1]", counts, maxPreloadBatchParams)
	}
}

// 目标表为复合主键时必须在执行前报错
func TestSession_PreloadTargetErrors(t *testing.T) {
	type compositeTarget struct {
		ID1 int64 `wxorm:"column(id1) pk"`
		ID2 int64 `wxorm:"column(id2) pk"`
	}

	type compositeModel struct {
		ID     int64            `wxorm:"column(id) pk auto"`
		Target *compositeTarget `wxorm:"preload(target_id)"`
	}

	type missingColumnModel struct {
		ID     int64          `wxorm:"column(id) pk auto"`
		Author *preloadAuthor `wxorm:"preload(not_exists)"`
	}

	cases := []struct {
		name  string
		model any
		field string
		want  error
	}{
		{"复合主键", &compositeModel{}, "Target", ErrPreloadRequiresSingleKey},
		{"关联列不存在", &missingColumnModel{}, "Author", ErrPreloadColumnNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dial := dialect.Get("mysql")
			sch, err := schema.Parse(dial, tc.model)
			if err != nil {
				t.Fatalf("parse schema failed: %v", err)
			}

			sess := NewSession(1, "stub", nil, dial, nil)
			sess.SetSchema(sch)

			parent := reflect.New(sch.ModelType).Elem()
			parent.Field(0).SetInt(1)

			sess.PRELOAD(tc.field)
			if err := sess.loadPreloads([]reflect.Value{parent}, nil); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// 父子键的字符串归一必须一致
func TestStringKey(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"int", 7, "7"},
		{"int64", int64(7), "7"},
		{"uint", uint(7), "7"},
		{"float64", float64(7), "7"},
		{"string", "7", "7"},
		{"bytes", []byte("7"), "7"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stringKey(tc.value); got != tc.want {
				t.Fatalf("stringKey(%v) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

// 目标模型自身解析失败时必须透传包装错误，不返回半成品结果
func TestSession_PreloadTargetParseError(t *testing.T) {
	// 目标模型带一个非法 preload 标签，解析时必然失败
	type badTarget struct {
		ID int64 `wxorm:"column(id) pk auto"`
		X  int64 `wxorm:"preload()"`
	}

	type parentModel struct {
		ID     int64      `wxorm:"column(id) pk auto"`
		Target *badTarget `wxorm:"preload(target_id)"`
	}

	dial := dialect.Get("mysql")
	sch, err := schema.Parse(dial, &parentModel{})
	if err != nil {
		t.Fatalf("parse parent schema failed: %v", err)
	}

	sess := NewSession(1, "stub", nil, dial, nil)
	sess.SetSchema(sch)

	parent := reflect.New(sch.ModelType).Elem()
	parent.Field(0).SetInt(1)

	sess.PRELOAD("Target")
	err = sess.loadPreloads([]reflect.Value{parent}, nil)
	if err == nil || !strings.Contains(err.Error(), "failed to parse preload target") {
		t.Fatalf("err = %v, want 目标解析失败的包装错误", err)
	}
	if !errors.Is(err, schema.ErrInvalidPreloadTag) {
		t.Fatalf("err = %v, want 包装 schema.ErrInvalidPreloadTag", err)
	}
}

// 父结果为空时不应发出子查询
func TestSession_PreloadEmptyParents(t *testing.T) {
	dial := dialect.Get("mysql")
	sch, err := schema.Parse(dial, &preloadArticle{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	db, err := sql.Open(stubdriver.DriverName, "preload-empty")
	if err != nil {
		t.Fatalf("open stub failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	sess := NewSession(1, "stub", db, dial, &sync.Map{})
	sess.SetSchema(sch)

	stubdriver.Reset()
	sess.PRELOAD("Author", "Comments")
	if err := sess.loadPreloads(nil, nil); err != nil {
		t.Fatalf("load preloads failed: %v", err)
	}

	if calls := stubdriver.Calls(); len(calls) != 0 {
		t.Fatalf("空父集合不应发子查询，实际 %d 条: %v", len(calls), calls)
	}
}

// 多个父对象共享同一键时都应挂载成功，且键去重后只查一次
func TestSession_PreloadSharedKey(t *testing.T) {
	dial := dialect.Get("mysql")
	sch, err := schema.Parse(dial, &preloadArticle{})
	if err != nil {
		t.Fatalf("parse schema failed: %v", err)
	}

	db, err := sql.Open(stubdriver.DriverName, "preload-shared")
	if err != nil {
		t.Fatalf("open stub failed: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	sess := NewSession(1, "stub", db, dial, &sync.Map{})
	sess.SetSchema(sch)

	stubdriver.Reset()
	stubdriver.Push([]string{"id", "name"}, []driver.Value{int64(1), "张三"})

	keyIndex := sch.ColumnIndexesByName["author_id"]
	parents := make([]reflect.Value, 0, 2)
	for i := 0; i < 2; i++ {
		value := reflect.New(sch.ModelType).Elem()
		value.Field(keyIndex).SetInt(1)
		parents = append(parents, value)
	}

	sess.PRELOAD("Author")
	if err := sess.loadPreloads(parents, nil); err != nil {
		t.Fatalf("load preloads failed: %v", err)
	}

	if counts := stubdriver.ArgCounts(); len(counts) != 1 || counts[0] != 1 {
		t.Fatalf("共享键应只查一次且参数 1 个，实际 %v", counts)
	}

	for i, parent := range parents {
		author := parent.Addr().Interface().(*preloadArticle).Author
		if author == nil || author.Name != "张三" {
			t.Fatalf("第 %d 个父对象未挂载作者: %#v", i, author)
		}
	}
}

// 零值与 nil 键必须被跳过滤，避免用零值当外键误匹配
func TestIsZeroPreloadKey(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  bool
	}{
		{"nil", nil, true},
		{"zero-int64", int64(0), true},
		{"empty-string", "", true},
		{"empty-bytes", []byte{}, true},
		{"int64", int64(1), false},
		{"string", "x", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isZeroPreloadKey(tc.value); got != tc.want {
				t.Fatalf("isZeroPreloadKey(%v) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
