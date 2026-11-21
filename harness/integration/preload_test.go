package integration

import (
	"fmt"
	"testing"

	"gitee.com/leminewx/wxorm"
	"gitee.com/leminewx/wxorm/naming"
	"gitee.com/leminewx/wxorm/session"
	"gitee.com/leminewx/wxorm/statement/where"
)

// PreloadAuthor 是预加载的目标作者模型
type PreloadAuthor struct {
	ID   int64  `orm:"column(id) pk auto"`
	Name string `orm:"column(name) notnull"`
}

func (*PreloadAuthor) GetTableName() string { return tableName("preload_author") }

// PreloadComment 通过 book_id 反向挂到 PreloadBook 上
type PreloadComment struct {
	ID      int64  `orm:"column(id) pk auto"`
	BookID  int64  `orm:"column(book_id) index default(0)"`
	Content string `orm:"column(content) notnull"`
}

func (*PreloadComment) GetTableName() string { return tableName("preload_comment") }

// PreloadBook 同时声明 N:1（Author）与 1:N（Comments）关联
type PreloadBook struct {
	ID       int64            `orm:"column(id) pk auto"`
	Title    string           `orm:"column(title) notnull"`
	AuthorID int64            `orm:"column(author_id) index default(0)"`
	Author   *PreloadAuthor   `orm:"preload(author_id)"`
	Comments []PreloadComment `orm:"preload(book_id)"`
}

func (*PreloadBook) GetTableName() string { return tableName("preload_book") }

// PreloadBookJoin 用于验证 JOIN 与 PRELOAD 共存：扁平列 + 集合字段
type PreloadBookJoin struct {
	ID         int64            `orm:"column(id) pk auto"`
	Title      string           `orm:"column(title) notnull"`
	AuthorID   int64            `orm:"column(author_id) index default(0)"`
	AuthorName *string          `orm:"join(author.name)"`
	Comments   []PreloadComment `orm:"preload(book_id)"`
}

func (*PreloadBookJoin) GetTableName() string { return tableName("preload_book") }

// createPreloadTables 建表并写入种子数据
//
// 数据形态与内存驱动用例一致：两本有作者的书 + 一本无人认领的书，
// 只有第一本书有评论，第二本书用于验证空集合。
func createPreloadTables(t *testing.T, engine *wxorm.Engine) {
	t.Helper()

	createTable(t, engine, tableName("preload_author"), &PreloadAuthor{})
	createTable(t, engine, tableName("preload_comment"), &PreloadComment{})
	createTable(t, engine, tableName("preload_book"), &PreloadBook{})

	for _, name := range []string{"张三", "李四"} {
		sess := engine.NewSession(&PreloadAuthor{})
		if _, err := sess.INSERT(false, &PreloadAuthor{Name: name}); err != nil {
			t.Fatalf("插入作者 %s 失败: %v", name, err)
		}
	}

	// 主键起始值由方言决定，按名称反查
	var authors []PreloadAuthor
	sess := engine.NewSession(&PreloadAuthor{})
	if _, err := sess.SELECT(&authors); err != nil {
		t.Fatalf("查询作者失败: %v", err)
	}

	ids := make(map[string]int64, len(authors))
	for _, author := range authors {
		ids[author.Name] = author.ID
	}

	for _, book := range []*PreloadBook{
		{Title: "Go 实战", AuthorID: ids["张三"]},
		{Title: "SQL 之美", AuthorID: ids["李四"]},
		{Title: "无人认领", AuthorID: 9999},
	} {
		sess := engine.NewSession(&PreloadBook{})
		if _, err := sess.INSERT(false, book); err != nil {
			t.Fatalf("插入图书 %s 失败: %v", book.Title, err)
		}
	}

	var books []PreloadBook
	sess = engine.NewSession(&PreloadBook{})
	if _, err := sess.SELECT(&books); err != nil {
		t.Fatalf("查询图书失败: %v", err)
	}

	bookIDs := make(map[string]int64, len(books))
	for _, book := range books {
		bookIDs[book.Title] = book.ID
	}

	for _, content := range []string{"评论一", "评论二"} {
		sess := engine.NewSession(&PreloadComment{})
		if _, err := sess.INSERT(false, &PreloadComment{BookID: bookIDs["Go 实战"], Content: content}); err != nil {
			t.Fatalf("插入评论 %s 失败: %v", content, err)
		}
	}
}

// TestPreload_BelongsToAndHasMany 三方言验证两种关联方向与空集合语义
func TestPreload_BelongsToAndHasMany(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createPreloadTables(t, engine)

		sess := engine.NewSession(&PreloadBook{})
		sess.PRELOAD("Author", "Comments").ORDER("id")

		var books []PreloadBook
		res, err := sess.SELECT(&books)
		if err != nil {
			t.Fatalf("预加载查询失败: %v\n%s", err, summarize(res))
		}

		if len(books) != 3 {
			t.Fatalf("行数 = %d, want 3: %#v", len(books), books)
		}

		if books[0].Author == nil || books[0].Author.Name != "张三" {
			t.Errorf("第一本书作者 = %#v", books[0].Author)
		}
		if books[1].Author == nil || books[1].Author.Name != "李四" {
			t.Errorf("第二本书作者 = %#v", books[1].Author)
		}
		if books[2].Author != nil {
			t.Errorf("无匹配作者时应为 nil，实际 %#v", books[2].Author)
		}

		if len(books[0].Comments) != 2 {
			t.Errorf("第一本书评论数 = %d, want 2", len(books[0].Comments))
		} else if books[0].Comments[0].Content != "评论一" {
			t.Errorf("评论顺序错误: %#v", books[0].Comments)
		}
		if books[1].Comments == nil {
			t.Error("无评论时应返回空切片而不是 nil")
		}
		if len(books[1].Comments) != 0 {
			t.Errorf("第二本书评论 = %#v, want empty", books[1].Comments)
		}

		// 单行查询
		sess = engine.NewSession(&PreloadBook{})
		sess.PRELOAD().WHERE(where.EQ("title", "Go 实战"))

		var book PreloadBook
		res, err = sess.FindFirst(&book)
		if err != nil {
			t.Fatalf("单行预加载失败: %v\n%s", err, summarize(res))
		}
		if book.Author == nil || book.Author.Name != "张三" {
			t.Errorf("单行查询作者 = %#v", book.Author)
		}
		if len(book.Comments) != 2 {
			t.Errorf("单行查询评论数 = %d, want 2", len(book.Comments))
		}
	})
}

// TestPreload_PartialColumns 验证 COLUMNS 与预加载共存，未请求列保持零值
func TestPreload_PartialColumns(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createPreloadTables(t, engine)

		sess := engine.NewSession(&PreloadBook{})
		sess.COLUMNS("id", "author_id").PRELOAD("Author").ORDER("id")

		var books []PreloadBook
		res, err := sess.SELECT(&books)
		if err != nil {
			t.Fatalf("部分列预加载失败: %v\n%s", err, summarize(res))
		}

		if len(books) != 3 {
			t.Fatalf("行数 = %d, want 3", len(books))
		}
		if books[0].Title != "" {
			t.Errorf("未请求的 title 应为零值，实际 %q", books[0].Title)
		}
		if books[0].Author == nil || books[0].Author.Name != "张三" {
			t.Errorf("分组键缺失导致关联丢失: %#v", books[0].Author)
		}
	})
}

// TestPreload_Naming 验证目标表同样套用命名策略
func TestPreload_Naming(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		strategy := naming.Strategy{Prefix: "it_"}

		createTableWithNaming(t, engine, &PreloadAuthor{}, strategy)
		createTableWithNaming(t, engine, &PreloadComment{}, strategy)
		createTableWithNaming(t, engine, &PreloadBook{}, strategy)

		authorSess := engine.NewSession(&PreloadAuthor{}).SetNaming(strategy)
		if _, err := authorSess.INSERT(false, &PreloadAuthor{Name: "张三"}); err != nil {
			t.Fatalf("插入作者失败: %v", err)
		}

		var authors []PreloadAuthor
		if _, err := authorSess.SELECT(&authors); err != nil {
			t.Fatalf("查询作者失败: %v", err)
		}

		bookSess := engine.NewSession(&PreloadBook{}).SetNaming(strategy)
		if _, err := bookSess.INSERT(false, &PreloadBook{Title: "Go 实战", AuthorID: authors[0].ID}); err != nil {
			t.Fatalf("插入图书失败: %v", err)
		}

		sess := engine.NewSession(&PreloadBook{}).SetNaming(strategy)
		sess.PRELOAD("Author")

		var books []PreloadBook
		res, err := sess.SELECT(&books)
		if err != nil {
			t.Fatalf("带前缀的预加载失败: %v\n%s", err, summarize(res))
		}
		if len(books) != 1 || books[0].Author == nil || books[0].Author.Name != "张三" {
			t.Errorf("带前缀的关联结果错误: %#v", books)
		}
	})
}

// TestPreload_WithJoin 验证同一次查询里 JOIN 扁平列与 PRELOAD 集合互不干扰
func TestPreload_WithJoin(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createPreloadTables(t, engine)

		sess := engine.NewSession(&PreloadBookJoin{})
		sess.INNER_JOIN(tableName("preload_author"), "author",
			where.EQ_COLUMN(tableName("preload_book")+".author_id", "author.id"))
		sess.PRELOAD("Comments").WHERE(where.EQ("title", "Go 实战"))

		var book PreloadBookJoin
		res, err := sess.SELECT(&book)
		if err != nil {
			t.Fatalf("JOIN + PRELOAD 失败: %v\n%s", err, summarize(res))
		}

		if book.AuthorName == nil || *book.AuthorName != "张三" {
			t.Errorf("联表列 = %#v", book.AuthorName)
		}
		if len(book.Comments) != 2 {
			t.Errorf("集合字段 = %#v, want 2 条", book.Comments)
		}
	})
}

// TestPreload_InTransaction 验证事务内未提交的写入也能被 Preload 读取
func TestPreload_InTransaction(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createTable(t, engine, tableName("preload_author"), &PreloadAuthor{})
		createTable(t, engine, tableName("preload_comment"), &PreloadComment{})
		createTable(t, engine, tableName("preload_book"), &PreloadBook{})

		_, err := engine.Transact(func(sess *session.Session) (any, error) {
			// 事务内插入作者并回查主键（各方言自增起始值不同）
			authorSess := sess.UseModel(&PreloadAuthor{})
			if _, err := authorSess.INSERT(false, &PreloadAuthor{Name: "事务作者"}); err != nil {
				return nil, err
			}

			var authors []PreloadAuthor
			if _, err := authorSess.SELECT(&authors); err != nil {
				return nil, err
			}
			if len(authors) != 1 {
				return nil, fmt.Errorf("事务内作者数 = %d, want 1", len(authors))
			}

			// 插入父行与子行后立即 Preload，应看到本事务内的写入
			bookSess := sess.UseModel(&PreloadBook{})
			bookRes, err := bookSess.INSERT(false, &PreloadBook{Title: "事务图书", AuthorID: authors[0].ID})
			if err != nil {
				return nil, err
			}

			// INSERT 不回填模型主键，自增 ID 从结果取
			bookID, err := bookRes.LastInsertId()
			if err != nil {
				return nil, err
			}

			commentSess := sess.UseModel(&PreloadComment{})
			if _, err := commentSess.INSERT(false, &PreloadComment{BookID: bookID, Content: "事务评论"}); err != nil {
				return nil, err
			}

			var books []PreloadBook
			loadSess := sess.UseModel(&PreloadBook{})
			loadSess.PRELOAD("Author", "Comments")
			if _, err := loadSess.SELECT(&books); err != nil {
				return nil, err
			}
			if len(books) != 1 {
				return nil, fmt.Errorf("事务内图书数 = %d, want 1", len(books))
			}
			if books[0].Author == nil || books[0].Author.Name != "事务作者" {
				return nil, fmt.Errorf("事务内作者未预加载: %#v", books[0].Author)
			}
			if len(books[0].Comments) != 1 {
				return nil, fmt.Errorf("事务内评论数 = %d, want 1（插入 bookID=%d, 查询 book.ID=%d）",
					len(books[0].Comments), bookID, books[0].ID)
			}

			return nil, nil
		})
		if err != nil {
			t.Fatalf("事务内预加载失败: %v", err)
		}
	})
}
