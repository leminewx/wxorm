package integration

import (
	"testing"

	"gitee.com/leminewx/wxorm"
	"gitee.com/leminewx/wxorm/naming"
	"gitee.com/leminewx/wxorm/statement/where"
)

// JoinedAuthor 是被关联的作者模型
type JoinedAuthor struct {
	ID   int64  `orm:"column(id) pk auto"`
	Name string `orm:"column(name) notnull"`
}

func (*JoinedAuthor) GetTableName() string { return tableName("join_author") }

// JoinedBook 通过 join 标签声明 N:1 关联字段
//
// 联表字段只参与查询投影：建表时不会出现 author_name 列，
// INSERT/UPDATE 也不会写入它。
type JoinedBook struct {
	ID         int64   `orm:"column(id) pk auto"`
	Title      string  `orm:"column(title) notnull"`
	AuthorID   int64   `orm:"column(author_id) index default(0)"`
	AuthorName *string `orm:"join(author.name)"`
}

func (*JoinedBook) GetTableName() string { return tableName("join_book") }

// createJoinTables 建表并写入种子数据：两本有作者的图书 + 一本找不到作者的图书
func createJoinTables(t *testing.T, engine *wxorm.Engine) {
	t.Helper()

	createTable(t, engine, tableName("join_author"), &JoinedAuthor{})
	createTable(t, engine, tableName("join_book"), &JoinedBook{})

	for _, name := range []string{"张三", "李四"} {
		sess := engine.NewSession(&JoinedAuthor{})
		if _, err := sess.INSERT(false, &JoinedAuthor{Name: name}); err != nil {
			t.Fatalf("插入作者 %s 失败: %v", name, err)
		}
	}

	// 主键自增的起始值由方言决定，按名称反查，避免假设 ID
	var authors []JoinedAuthor
	sess := engine.NewSession(&JoinedAuthor{})
	if _, err := sess.SELECT(&authors); err != nil {
		t.Fatalf("查询作者失败: %v", err)
	}

	ids := make(map[string]int64, len(authors))
	for _, author := range authors {
		ids[author.Name] = author.ID
	}

	books := []*JoinedBook{
		{Title: "Go 实战", AuthorID: ids["张三"]},
		{Title: "SQL 之美", AuthorID: ids["李四"]},
		{Title: "无人认领", AuthorID: 9999},
	}
	for _, book := range books {
		sess := engine.NewSession(&JoinedBook{})
		if _, err := sess.INSERT(false, book); err != nil {
			t.Fatalf("插入图书 %s 失败: %v", book.Title, err)
		}
	}
}

// TestJoin_MapAndNull 验证三方言下的结果映射与左连接 NULL 语义
func TestJoin_MapAndNull(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createJoinTables(t, engine)

		// 内连接：联表列以 author_name 返回，回填到指针字段
		sess := engine.NewSession(&JoinedBook{})
		sess.INNER_JOIN(tableName("join_author"), "author",
			where.EQ_COLUMN(tableName("join_book")+".author_id", "author.id"))
		sess.WHERE(where.EQ("title", "Go 实战"))

		var book JoinedBook
		res, err := sess.SELECT(&book)
		if err != nil {
			t.Fatalf("内连接查询失败: %v\n%s", err, summarize(res))
		}
		if book.Title != "Go 实战" || book.AuthorID == 0 {
			t.Errorf("主表列映射错误: %#v", book)
		}
		if book.AuthorName == nil || *book.AuthorName != "张三" {
			t.Errorf("联表列映射错误: %#v", book.AuthorName)
		}

		// 左连接：没有匹配作者时保留主表行，联表字段为 NULL
		sess = engine.NewSession(&JoinedBook{})
		sess.LEFT_JOIN(tableName("join_author"), "author",
			where.EQ_COLUMN(tableName("join_book")+".author_id", "author.id"))
		sess.WHERE(where.EQ("title", "无人认领"))

		var orphan JoinedBook
		res, err = sess.SELECT(&orphan)
		if err != nil {
			t.Fatalf("左连接查询失败: %v\n%s", err, summarize(res))
		}
		if orphan.Title != "无人认领" || orphan.AuthorID != 9999 {
			t.Errorf("左连接主表列映射错误: %#v", orphan)
		}
		if orphan.AuthorName != nil {
			t.Errorf("无匹配作者时应为 NULL，实际 %q", *orphan.AuthorName)
		}

		// 多行 + 分页 + 指定列：未请求的列保持零值
		sess = engine.NewSession(&JoinedBook{})
		sess.LEFT_JOIN(tableName("join_author"), "author",
			where.EQ_COLUMN(tableName("join_book")+".author_id", "author.id"))
		sess.COLUMNS("id", "title", "author_name").
			ORDER(tableName("join_book") + ".id").
			LIMIT(3)

		var rows []JoinedBook
		res, err = sess.SELECT(&rows)
		if err != nil {
			t.Fatalf("左连接分页查询失败: %v\n%s", err, summarize(res))
		}
		if len(rows) != 3 {
			t.Fatalf("行数 = %d, want 3: %#v", len(rows), rows)
		}
		if rows[0].Title != "Go 实战" || rows[0].AuthorName == nil || *rows[0].AuthorName != "张三" {
			t.Errorf("第一行 = %#v", rows[0])
		}
		if rows[2].AuthorName != nil {
			t.Errorf("第三行联表列应为 NULL，实际 %q", *rows[2].AuthorName)
		}
		if rows[0].AuthorID != 0 {
			t.Errorf("未请求的列应保持零值，实际 %d", rows[0].AuthorID)
		}
	})
}

// TestJoin_CountExist 验证联表统计去重与存在性判断
func TestJoin_CountExist(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		createJoinTables(t, engine)

		on := where.EQ_COLUMN(tableName("join_book")+".author_id", "author.id")
		table := tableName("join_author")

		// 左连接统计必须按主表主键去重：3 本书（含无作者那本）
		sess := engine.NewSession(&JoinedBook{})
		sess.LEFT_JOIN(table, "author", on)
		count, res, err := sess.COUNT()
		if err != nil {
			t.Fatalf("左连接统计失败: %v\n%s", err, summarize(res))
		}
		if count != 3 {
			t.Errorf("左连接统计 = %d, want 3", count)
		}

		// 内连接统计：无作者的图书被排除
		sess = engine.NewSession(&JoinedBook{})
		sess.INNER_JOIN(table, "author", on)
		count, res, err = sess.COUNT()
		if err != nil {
			t.Fatalf("内连接统计失败: %v\n%s", err, summarize(res))
		}
		if count != 2 {
			t.Errorf("内连接统计 = %d, want 2", count)
		}

		// 关联表条件参与 WHERE：李四有一本书，王五没有
		sess = engine.NewSession(&JoinedBook{})
		sess.INNER_JOIN(table, "author", on).WHERE(where.EQ("author.name", "李四"))
		exist, err := sess.Exist()
		if err != nil {
			t.Fatalf("存在性判断失败: %v", err)
		}
		if !exist {
			t.Error("李四应存在关联图书")
		}

		sess = engine.NewSession(&JoinedBook{})
		sess.INNER_JOIN(table, "author", on).WHERE(where.EQ("author.name", "王五"))
		exist, err = sess.Exist()
		if err != nil {
			t.Fatalf("存在性判断失败: %v", err)
		}
		if exist {
			t.Error("王五不应存在关联图书")
		}
	})
}

// TestJoin_NamingPrefix 验证关联表同样套用命名策略，别名不受影响
func TestJoin_NamingPrefix(t *testing.T) {
	forEachDialect(t, func(t *testing.T, tc target, engine *wxorm.Engine) {
		strategy := naming.Strategy{Prefix: "it_"}

		createTableWithNaming(t, engine, &JoinedAuthor{}, strategy)
		createTableWithNaming(t, engine, &JoinedBook{}, strategy)

		authorSess := engine.NewSession(&JoinedAuthor{}).SetNaming(strategy)
		if _, err := authorSess.INSERT(false, &JoinedAuthor{Name: "张三"}); err != nil {
			t.Fatalf("插入作者失败: %v", err)
		}

		var authors []JoinedAuthor
		if _, err := authorSess.SELECT(&authors); err != nil {
			t.Fatalf("查询作者失败: %v", err)
		}

		bookSess := engine.NewSession(&JoinedBook{}).SetNaming(strategy)
		if _, err := bookSess.INSERT(false, &JoinedBook{Title: "Go 实战", AuthorID: authors[0].ID}); err != nil {
			t.Fatalf("插入图书失败: %v", err)
		}

		// 逻辑表名交给命名策略解析；别名 "author" 必须保持原样
		sess := engine.NewSession(&JoinedBook{}).SetNaming(strategy)
		sess.INNER_JOIN(tableName("join_author"), "author",
			where.EQ_COLUMN(sess.GetTableName()+".author_id", "author.id"))

		var book JoinedBook
		res, err := sess.SELECT(&book)
		if err != nil {
			t.Fatalf("带前缀的联表查询失败: %v\n%s", err, summarize(res))
		}
		if book.AuthorName == nil || *book.AuthorName != "张三" {
			t.Errorf("联表列映射错误: %#v", book.AuthorName)
		}
	})
}

// createTableWithNaming 按命名策略创建物理表，并在用例结束后清理
func createTableWithNaming(t *testing.T, engine *wxorm.Engine, model any, strategy naming.Strategy) {
	t.Helper()

	sess := engine.NewSession(model).SetNaming(strategy)
	physical := sess.GetQualifiedTableName()

	dropTable(t, engine, physical)
	if err := sess.CreateTable(); err != nil {
		t.Fatalf("创建数据表 %s 失败: %v", physical, err)
	}

	t.Cleanup(func() { dropTable(t, engine, physical) })
}
