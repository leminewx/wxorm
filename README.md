# wxorm

![wxorm logo](docs/assets/logo.png)

`wxorm` 是一个构建在 Go 标准库 `database/sql` 之上的轻量级 ORM。它在保持
`*sql.DB` / `*sql.Tx` 完全兼容的前提下，以结构体标签描述表结构、约束与关联，
提供链式 SQL 构造、CRUD、批量写入、UPSERT、事务与上下文取消、N:1 JOIN、
关联预加载、表前缀与库限定、DDL 迁移及结构差异报告、预编译语句 LRU 缓存、
慢 SQL 日志与 `big.Int` / `big.Float` 大数支持等能力；内置 MySQL、PostgreSQL、
SQLite 三套方言适配，SQL 与占位符按方言生成，便于在多种数据库之间平滑切换。

- **方言**：MySQL、PostgreSQL、SQLite 均已端到端可用（CRUD、UPSERT、DDL、索引、主键回填）；
  各方言的差异与边界见 [已知限制](#已知限制)。
- **依赖**：仅标准库。数据库驱动由使用者自行引入。
- **测试**：内置内存驱动，`go test ./...` 不需要真实数据库即可跑通。
- **版本**：`v1.0.0.0`（正式发布，公开 API 已冻结），历史记录见 [RELEASE.md](RELEASE.md)。

## 安装

```bash
go get gitee.com/leminewx/wxorm
```

使用时需要自行引入对应的驱动（以 MySQL 为例）：

```go
import _ "github.com/go-sql-driver/mysql"
```

PostgreSQL 同理，引入 pgx 的 `database/sql` 适配层即可：

```go
import _ "github.com/jackc/pgx/v5/stdlib"
```

SQLite 二选一，两个驱动分别注册为 `sqlite` 与 `sqlite3`，对应同一个方言实例：

```go
import _ "modernc.org/sqlite"        // 纯 Go，无需 cgo，注册名 sqlite
// import _ "github.com/mattn/go-sqlite3" // 需要 cgo，注册名 sqlite3
```

## 快速开始

### 1. 定义模型

```go
// User 通过标签声明表结构；没有 type() 时由 Go 类型推断数据库类型
type User struct {
	ID        int64     `orm:"column(id) type(bigint) pk auto comment('用户ID')"`
	Name      string    `wxorm:"column(name) type(varchar(50)) notnull comment('用户名')"`
	Email     string    `wxorm:"column(email) type(varchar(100)) unique comment('邮箱')"`
	Age       int       `orm:"column(age) type(int) default(0) comment('年龄')"`
	GroupID   int64     `orm:"column(group_id) type(bigint) fk(groups(id),'fk_user_group')"`
	CreatedAt time.Time `wxorm:"created comment('创建时间')"`
}

// GetTableName 显式指定表名；不实现则按结构体名推断
func (own *User) GetTableName() string { return "users" }
```

### 2. 连接与建表

```go
engine, err := wxorm.NewEngine("mysql", "user:pass@tcp(127.0.0.1:3306)/demo?charset=utf8mb4")
if err != nil {
	return err
}
defer engine.Close()

// Migrate 幂等：表不存在则创建，存在则按模型补列/删列，并补建缺失索引
if err := engine.Migrate(&User{}); err != nil {
	return err
}
```

### 3. 增删改查

```go
sess := engine.NewSession(&User{})

// 插入：第一个参数为 true 时生成 INSERT IGNORE（SQLite 为 INSERT OR IGNORE）
res, err := sess.INSERT(false, &User{Name: "alice", Email: "a@example.com", Age: 20})
id, _ := res.LastInsertId()

// 查询：目标必须是指针，支持 *Struct、*[]Struct、*[]*Struct
var user User
if _, err := engine.NewSession(&User{}).WHERE(where.EQ("id", id)).SELECT(&user); err != nil {
	return err
}

// 单条：查不到返回 ErrRecordNotFound，与「查询失败」区分开
if _, err := engine.NewSession(&User{}).WHERE(where.EQ("id", id)).FindFirst(&user); err != nil {
	if errors.Is(err, session.ErrRecordNotFound) {
		// 这里处理「没有这条数据」，而不是当成错误上报
	}
	return err
}

// 存在性：生成 SELECT 1 ... LIMIT 1，比 COUNT 便宜得多
var exists bool
if exists, err = engine.NewSession(&User{}).WHERE(where.EQ("name", "alice")).Exist(); err != nil {
	return err
}

// 指定列：结果按返回的列名映射，没查的字段保持零值
var briefs []User
if _, err := engine.NewSession(&User{}).COLUMNS("id", "name").SELECT(&briefs); err != nil {
	return err
}

// 更新：支持 map 与 struct；struct 默认只写非零字段，零值字段保持原值
if _, err := engine.NewSession(&User{}).WHERE(where.EQ("id", id)).
	UPDATE(map[string]any{"age": 21}); err != nil {
	return err
}

// 只更新指定列：白名单内的列无论是否为零值都会写回，其余列保持不变
if _, err := engine.NewSession(&User{}).UPDATE_COLUMNS("name").
	UPDATE(&User{ID: id, Name: "alice"}); err != nil {
	return err
}

// 整行覆盖：OVERWRITE() 会把未赋值的字段写成零值，
// 只有确认传进来的结构体就是完整的一行时才用它
if _, err := engine.NewSession(&User{}).OVERWRITE().
	UPDATE(&User{ID: id, Name: "alice"}); err != nil {
	return err
}

// 统计
count, _, err := engine.NewSession(&User{}).WHERE(where.EQ("age", 21)).COUNT()

// 删除：不传参数时按主键删除，传入 struct 时按该结构体的主键删除
if _, err := engine.NewSession(&User{}).WHERE(where.EQ("id", id)).DELETE(); err != nil {
	return err
}
```

> `WHERE`、`LIMIT`、`ORDER`、`COLUMNS` 等阶段子句在**一次** CRUD 调用后会自动清理，
> 但同一个 `Session` 不能被多个 goroutine 并发使用。

> 同一张表可以对应多个模型（例如只含部分列的「精简模型」）。每个模型单独解析并缓存列集，
> `INSERT` 的预编译语句也按列集区分，不会出现参数串位；查询返回哪个模型，就只填该模型声明的字段。

### 4. 事务

```go
// 事务：回调返回错误或 panic 时回滚，panic 会被转换为 error 返回
_, err := engine.Transact(func(sess *session.Session) (any, error) {
	return sess.UseModel(&User{}).INSERT(false,
		&User{Name: "bob", Email: "b@example.com"},
		&User{Name: "carol", Email: "c@example.com"},
	)
})

// 绑定请求上下文：ctx 取消或超时会传入事务及其中每条 SQL，触发回滚
_, err = engine.TransactContext(ctx, func(sess *session.Session) (any, error) {
	return sess.UseModel(&User{}).INSERT(false, &User{Name: "dave"})
})

// 手动事务：Begin() 等价于 BeginTx(sess.GetContext(), nil)
if err := sess.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable}); err != nil {
	return err
}
if _, err := sess.Exec("UPDATE users SET age = age + 1"); err != nil {
	_ = sess.Rollback() // 回滚失败不覆盖原始错误
	return err
}
return sess.Commit()
```

重复 `Begin` 返回 `session.ErrTransactionAlreadyStarted`；未开启事务就 `Commit` / `Rollback`
返回 `session.ErrTransactionNotStartedYet`。

### 5. 条件构造

```go
where.EQ("status", 1).AND(where.IN("id", 1, 2, 3))
where.LIKE("name", "ali%").OR(where.ISNULL("deleted_at"))
where.BETWEEN("age", 18, 30).AND(where.NOTIN("role", "guest"))
where.RAW("created_at >= ?", time.Now().AddDate(0, 0, -7)) // PostgreSQL 下自动改写为 $n，SQLite 仍是 ?
where.WHEN(onlyActive, func() where.Condition { return where.EQ("active", 1) })

// 空集合不会生成非法的 IN ()：IN() 恒假（1 = 0），NOTIN() / NOT(IN()) 恒真（1 = 1）
where.IN("id") // 等价于 1 = 0

// JSON 与全文检索：MySQL → JSON_CONTAINS / MATCH...AGAINST，PostgreSQL → @> / to_tsvector，
// SQLite → json_each；全文检索在 SQLite 下不支持（需 FTS5 虚拟表）
where.JSON_CONTAINS("payload", "$.user.name", "wxorm")
where.FULLTEXT([]string{"title", "body"}, "wxorm orm")
```

> `RAW` 只改写引号与注释之外的 `?`：字符串字面量、引用标识符、注释与 PostgreSQL 美元引用
> 内的 `?` 原样保留。jsonb 的 `?` 运算符与占位符形态相同，无法词法区分，混用时请写成
> `jsonb_exists(data, 'k')` 或直接使用方言占位符。

> `COUNT` 不接受 `GROUP` / `HAVING`：存在分组子句时返回 `session.ErrCountWithGroupUnsupported`，
> 不会静默丢掉分组条件后统计全表。

### 6. UPSERT 与幂等写入

```go
// 冲突则忽略：MySQL 生成 INSERT IGNORE，SQLite 生成 INSERT OR IGNORE，
// PostgreSQL 生成 ON CONFLICT DO NOTHING
_, err := engine.NewSession(&User{}).INSERT(true, &User{Email: "a@example.com"})

// UPSERT：按模型主键冲突，只覆盖指定列（跨方言统一入口）
// MySQL → ON DUPLICATE KEY UPDATE；PostgreSQL / SQLite → ON CONFLICT (主键) DO UPDATE SET
// 注意：DUPLICATE 只有 MySQL 语义（冲突目标由唯一索引隐式确定），
// PostgreSQL 与 SQLite 下会返回 clause.ErrUnsupportedFeature
_, err = engine.NewSession(&User{}).UPSERT("name", "age").
	INSERT(false, &User{ID: 1, Name: "alice", Age: 21})

// 单条插入后可直接拿到数据库生成的主键：
// MySQL / SQLite 走 LastInsertId，PostgreSQL 走 INSERT ... RETURNING
res, err := engine.NewSession(&User{}).INSERT(false, &User{Name: "bob"})
if err == nil {
	id, _ := res.LastInsertId() // 各方言语义一致
}
```

### 7. PostgreSQL

```go
// DSN 支持 URL 与 keyword 两种写法，数据库名会自动解析；
// 省略库名时使用用户名同名库（遵循 libpq 的默认行为）
engine, err := wxorm.NewEngine("postgres", "postgres://user:pass@127.0.0.1:5432/demo?sslmode=disable")
if err != nil {
	return err
}

defer engine.Close()

// 建表：SERIAL/BIGSERIAL、GENERATED ... STORED、索引与 COMMENT ON 均会一并生成
if err := engine.Migrate(&User{}); err != nil {
	return err
}
```

`postgres` / `postgresql` / `pgx` 三个名字均可使用，指向同一个方言实例。

### 8. SQLite

```go
// 注册名 sqlite / sqlite3 指向同一个方言；DSN 是文件路径或 file: URI
// modernc.org/sqlite 用 _pragma 传 pragma，mattn/go-sqlite3 用 _foreign_keys=on
engine, err := wxorm.NewEngine("sqlite", "file:app.db?_pragma=foreign_keys(1)")
if err != nil {
	return err
}
defer engine.Close()

// 建表：pk auto 内联成 INTEGER PRIMARY KEY AUTOINCREMENT，索引在建表后逐条 CREATE INDEX；
// 内存库请用共享缓存，否则每条新连接都会拿到一个空库：
// wxorm.NewEngine("sqlite", "file::memory:?cache=shared")
if err := engine.Migrate(&User{}); err != nil {
	return err
}

// 读写与其它方言完全一致，插入后同样能拿到自增主键
res, err := engine.NewSession(&User{}).INSERT(false, &User{Name: "alice", Email: "a@example.com"})
```

SQLite 侧的边界：

- **外键默认不生效**：SQLite 的外键检查是连接级开关，需要在 DSN 里打开，或在每条连接上执行
  `PRAGMA foreign_keys = ON`；否则 `fk(...)` 只建约束、不做校验。
- **没有 `ON UPDATE`**：`updated` 标记不生效，更新时间由业务赋值或触发器维护。
- **没有表/列注释、没有全文检索**：`comment(...)` 会被忽略；`where.FULLTEXT` 返回
  `clause.ErrUnsupportedFeature`（需要 FTS5 虚拟表）。
- **`DUPLICATE` 不可用**：冲突目标必须显式，请用 `UPSERT`（生成 `ON CONFLICT (...) DO UPDATE SET`）。
- **`Migrate` 补列有约束**：SQLite 不允许给已有表新增带 `CURRENT_TIMESTAMP` 默认值的列，
  `created` 字段若在建表之后才补上，会报 `Cannot add a column with non-constant default`。
- **整型是 8 字节有符号**：`uint64` 超过 `MaxInt64` 会溢出，这类值建议改用字符串存十进制。

### 9. 表前缀与库限定

多租户、分库分表或环境隔离不必给每个模型手写 `GetTableName()`：给引擎或会话设置命名策略即可。
命名策略只影响物理表名，不参与 schema 缓存，所以同一个模型可以在不同引擎上使用不同前缀。

```go
// 全引擎生效：之后新建的会话都带前缀
engine.SetNaming(naming.Strategy{Prefix: "tenant1_"})
sess := engine.NewSession(&User{}) // users → tenant1_users

// 也可以只覆盖单个会话
sess.SetNaming(naming.Strategy{Schema: "tenant1", Prefix: "t_"})
sess.GetTableName()          // t_users
sess.GetQualifiedTableName() // tenant1.t_users
```

- `Schema` 是库（MySQL）或 Schema（PostgreSQL），SQLite 没有该层级、直接忽略；为空时由方言回落到
  `DATABASE()` / `current_schema()`，不会拿连接串里的库名去查元数据（PostgreSQL 的库与 schema 是两个层级）。
- 策略里的点号、引号、分隔符与空白会被拒绝（`naming.ErrInvalidSegment`），错误由 `GetSchema` 返回，
  不会静默退化成逻辑表名。
- INSERT 的预编译缓存键包含物理表名，同表不同前缀不会命中同一条语句。
- `CreateTable` / `DeleteTable` / `ExistsTable` / `Migrate` 都会使用物理表名与限定名。

### 10. 日志与慢 SQL

SQL 观测走可注入的 `Logger`，默认实现沿用标准库 `log` 的格式；调试模式记录全部语句，
非调试模式只上报达到阈值的慢 SQL：

```go
engine.
	SetLogger(myLogger).                       // 实现 LogQuery(ctx, wxorm.QueryEvent)
	SetSlowThreshold(200 * time.Millisecond).
	SetLogParamValues(true)                    // 慢 SQL 是否输出参数原值，默认关闭

// 也可以只覆盖单个会话
sess.SetSlowThreshold(50 * time.Millisecond).SetLogParamValues(false)

type myLogger struct{}

func (myLogger) LogQuery(ctx context.Context, event wxorm.QueryEvent) {
	// event.SessionID / Elapsed / Query / Args / Debug / Slow / Color
}
```

- `QueryEvent.Elapsed`：`Exec` 为完整执行耗时，`Query` 为拿到结果集的时间（不含调用方逐行 `Scan`）。
- 慢 SQL 默认脱敏：只输出参数个数，`QueryEvent.LogArgs` 为 `false`；确认日志环境安全后可用
  `SetLogParamValues(true)` 输出原值。调试模式（`SetDebug`）始终输出参数。
- 阈值传 `0` 或负数关闭慢 SQL 检测；不设置日志器时使用 `wxorm.NewDefaultLogger()`。
- 同一日志器会被多个会话并发调用，实现需自行保证并发安全。

### 11. 结构差异报告

`Migrate` 会自动补列并补建缺失索引。模型之外的列（字段被删除或改名）默认只报告不删除：
DDL 不可回滚，静默删列会销毁整列真实数据；确认要清理时显式传入 `wxorm.WithDropMissingColumns()`。
列名与类型比较不区分大小写，避免仅改大小写被判定成「删一列 + 加一列」。列类型差异无法保证
无损转换，只提示不执行。需要事先审计时可以调用只读的 `DiffTable`：

```go
diff, err := engine.DiffTable(&User{})
if err != nil {
	return err
}

diff.Exists          // 表是否存在
diff.AddedColumns    // 模型有、库里没有的列
diff.DroppedColumns  // 库里有、模型已删除的列
diff.ChangedColumns  // 类型不一致的列（[]wxorm.ColumnDiff）
diff.MissingIndexes  // 模型声明但库里缺失的索引（Migrate 会补建）
diff.IsEmpty()       // 上述差异是否全部为空
```

```go
engine.Migrate(&User{})                                 // 补列 + 补建缺失索引
engine.Migrate(&User{}, wxorm.WithDropMissingColumns()) // 额外删除模型之外的列
```

方言不提供数据字典读取能力时，类型与索引比对会被跳过，不会阻断迁移。

### 12. 查询缓存与批量写入

引擎默认创建容量 256 的查询语句缓存（`session.DefaultStatementCacheSize`），所有会话共享；
`SELECT` / `COUNT` / `Exist` / `UPDATE` / `DELETE` 与多行 `INSERT` 在非事务场景复用预编译语句：

```go
engine.SetStatementCacheSize(512) // 调整容量
engine.SetStatementCacheSize(0)   // 关闭缓存
```

- 事务内不复用：预编译语句绑定在 `*sql.DB` 上，进入事务会绕过缓存，避免提交/回滚失效。
- `CreateTable` / `DeleteTable` / `Migrate` 与 `Engine.Close` 会清空缓存；手工改表后可调用
  `sess.ClearStatementCache()` 主动清空（缓存由引擎共享，会影响同一引擎下的所有会话）。
- 缓存按 LRU 淘汰，防止动态 `WHERE` 组合把内存撑大；默认 256 条远低于 MySQL 的
  `max_prepared_stmt_count`（默认 16382）。
- 淘汰不立即关闭语句：缓存项带引用计数，正在执行的语句要等最后一个使用者归还后才 `Close`，
  并发下不会出现 `sql: statement is closed`。
- `SELECT` 会按 `LIMIT` 预分配结果集容量（上限 1024）；批量插入按方言占位符上限与列数自动分批
  （单批最多 1000 行，MySQL / PostgreSQL 60000 个占位符，SQLite 30000）。

```go
// 1000 行以内通常是一条 INSERT；超过上限自动拆批，并在需要时开启事务
sess.INSERT(false, rows...)
```

### 13. 启动标识

需要在启动日志里打印品牌标识时调用 `ShowLogo`，默认写到标准输出；
也可以接管输出（例如写入启动日志文件）：

```go
// 样式是进程级设置，默认宽幅艺术字；越界值会被忽略
wxorm.SetLogoStyle(wxorm.LOGO_STYLE_PIXEL)
if err := wxorm.ShowLogo(); err != nil { // 写 os.Stdout
	return err
}

// 需要接管输出时用 WriteLogo
if err := wxorm.WriteLogo(logFile); err != nil {
	return err
}
```

| 常量 | 风格 | 依赖字符 |
| --- | --- | --- |
| `LOGO_STYLE_CLASSIC` | 宽幅艺术字（默认） | 反斜杠、下划线、竖线 |
| `LOGO_STYLE_PIXEL` | 实心像素块 | `█` |
| `LOGO_STYLE_SHADOW` | 双线阴影 | `█`、`╔`、`║` |
| `LOGO_STYLE_ASCII` | 纯 ASCII 井号体 | 仅 ASCII |

`GetLogoStyle()` 读取当前样式，越界值会被忽略。样式用 `atomic.Int32` 存储，
可在启动协程中设置，并发调用 `ShowLogo` / `WriteLogo` 不产生数据竞争。

### 14. 联表查询（N:1 JOIN）

在模型上用 `join(别名.列名)` 声明关联字段，查询时用 `INNER_JOIN` / `LEFT_JOIN` / `JOIN` 指定关联表、别名与 ON 条件：

```go
type Article struct {
	ID         int64   `wxorm:"column(id) pk auto"`
	Title      string  `wxorm:"column(title) notnull"`
	AuthorID   int64   `wxorm:"column(author_id) index"`
	AuthorName *string `wxorm:"join(author.name)"` // 结果列名为 author_name
}

var articles []Article
_, err := sess.
	LEFT_JOIN("authors", "author", where.EQ_COLUMN("articles.author_id", "author.id")).
	WHERE(where.LIKE("author.name", "%张%")).
	ORDER("articles.id").
	LIMIT(20).
	SELECT(&articles)
```

生成的 SQL：

```sql
SELECT
    `articles`.`id`, `articles`.`title`, `articles`.`author_id`, `author`.`name` AS `author_name`
FROM `articles`
LEFT JOIN `authors` AS `author`
ON `articles`.`author_id` = `author`.`id`
WHERE `author`.`name` LIKE ?
ORDER BY `articles`.`id`
LIMIT 20;
```

约定：

- `join(...)` 字段只参与查询投影，不进建表列、`INSERT`、`UPDATE`，改模型不会影响已有表结构。
- 关联表名传**逻辑表名**，照常套用 naming 的前缀/后缀/Schema；别名不套命名策略，且必须显式、不含点号与空白。
- 结果列别名固定为 `<别名>_<列名>`（`author.name` → `author_name`），`COLUMNS` 也用它取值。
- 外连接（`LEFT` / `RIGHT` / `FULL`）下联表字段必须可空（指针、`interface`、`slice`、`map`，或实现 `sql.Scanner` 的类型），
  否则构建期返回 `session.ErrJoinColumnNotNilable`。
- `COUNT` 在 JOIN 下自动改为 `COUNT(DISTINCT 主表.单列主键)`，避免行数放大；复合/无主键返回 `session.ErrJoinRequiresSinglePK`。
- 写语句不支持 JOIN：存在 JOIN 时返回 `session.ErrJoinUnsupported` 并清空状态。
- 仅支持 N:1（一行主表对应一行关联表）；1:N 集合映射与嵌套对象请用 `PRELOAD`（见[关联预加载](#15-关联预加载preload)）。
- MySQL 不支持 `FULL JOIN`，返回 `clause.ErrUnsupportedFeature`。

### 15. 关联预加载（Preload）

1:N 集合与 N:1 嵌套对象用 `PRELOAD`，不与主查询拼 JOIN：先查父集合，再用一条
`WHERE 关联列 IN (...)` 把子集合捞回来，在内存里按分组键合并，分页与父行数不受子行影响。

```go
// Author / Comment 为独立的表模型，此处省略字段定义
type Article struct {
	ID       int64     `wxorm:"column(id) pk auto"`
	Title    string    `wxorm:"column(title) notnull"`
	AuthorID int64     `wxorm:"column(author_id)"`
	Author   *Author   `wxorm:"preload(author_id)"`  // 本表列 → 目标主键（belongs_to）
	Comments []Comment `wxorm:"preload(article_id)"` // 目标表列 → 本表主键（has_many）
}

var articles []Article
_, err := sess.PRELOAD("Author", "Comments").ORDER("id").SELECT(&articles)
```

约定：

- **方向由字段类型推导**：`*T` / `T` 为 belongs_to，`preload(...)` 里写本表的关联列；
  `[]T` / `[]*T` 为 has_many，写目标表里指向本表的列。目标表名优先用 `GetTableName()`，否则按元素类型转蛇形。
- `PRELOAD()` 不带参数表示加载模型声明的全部关联；只加载一层直接关联，不做递归。
- 子查询固定为「目标表全部列 + `WHERE 关联列 IN (...)` + `ORDER BY 目标主键`」；
  键去重后每批最多 30000 个占位符，超过自动拆成多条子查询，无 N+1。
- `*T` 无匹配时保持 `nil`；`[]T` 无匹配时得到**空切片**（非 nil），便于直接遍历与序列化。
- 声明是一次性的：`SELECT` / `FindFirst` 取走即归零；`COUNT` / `Exist` 静默丢弃声明；
  写语句带 `PRELOAD` 返回 `session.ErrPreloadUnsupported`。
- 与 `COLUMNS` 同用必须带上分组键（belongs_to 的关联列、has_many 的主键），
  否则返回 `session.ErrPreloadKeyNotSelected`，避免用零值当键把子集合挂错父对象。
- 子查询走当前会话的连接，事务内能读到未提交数据；但子查询不触发模型钩子。
- 目标表照常套用 naming 策略；关联键只支持单列主键，复合主键返回 `session.ErrPreloadRequiresSingleKey`。
- 可与 JOIN 同用：JOIN 负责扁平列投影，PRELOAD 负责集合与嵌套对象。

### 16. 大数支持（big.Int / big.Float）

`math/big` 的 `big.Int` / `big.Float` 可直接作为模型字段（值或指针），无需实现 `driver.Valuer`：

```go
type Ledger struct {
	ID     int64     `wxorm:"column(id) pk auto"`
	Amount *big.Int  `wxorm:"column(amount) null"` // 指针 nil ↔ SQL NULL
	Rate   big.Float `wxorm:"column(rate)"`
}

amount, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
_, err := engine.NewSession(&Ledger{}).INSERT(false, &Ledger{
	Amount: amount,
	Rate:   *big.NewFloat(0.0001),
})
```

约定：

- 写入把大数格式化成十进制文本（`big.Int` 用 `String()`，`big.Float` 用 `Text('f', 30)`）；
  读取先扫进 `sql.NullString`，再用 `SetString` / `ParseFloat(..., 256, ToNearestEven)` 回填。
- 默认类型映射：MySQL `DECIMAL(65,0)` / `DECIMAL(65,30)`，PostgreSQL `NUMERIC`，SQLite `TEXT`
  （SQLite 的 `NUMERIC` 亲和性会把超大数转成 `REAL` 丢精度，因此落文本）。
- `big.Float` 默认保留 30 位小数；需要更高精度时用 `type(...)` 显式声明列类型。
- `big.Int` 超过 65 位十进制会超出默认列宽，需自行放宽 `type(...)`（如 PostgreSQL `NUMERIC(200,0)`）。
- `big.Rat` **不支持**：模型解析阶段返回 `schema.ErrUnsupportedBigRat`。有理数无法无损映射到
  `NUMERIC` / `DECIMAL`，请改用 `big.Float`、字符串或自定义 `driver.Valuer` / `sql.Scanner` 类型。
- 只有声明了 big 字段的模型才走文本桥接，普通模型仍是直接扫描，无额外开销。
- big 字段的零值遵循 `UPDATE(struct)` 的默认策略：`big.Int` / `big.Float` 零值默认跳过，
  需要写入 0 时用 `UPDATE_COLUMNS("amount")` 或 `OVERWRITE()`。指针字段用 nil 表示 SQL NULL。

## API 速查

| 位置 | 方法 | 说明 |
| --- | --- | --- |
| `Engine` | `NewEngine` / `Close` / `GetStatus` | 建引擎、释放连接池、读连接池统计 |
| `Engine` | `Migrate(value any, options ...MigrateOption)` | 建表，或按模型补列并补建缺失索引；默认不删列，`WithDropMissingColumns()` 才删除模型之外的列；类型差异只提示 |
| `Engine` | `DiffTable(value any)` | 只读对比模型与表结构，返回 `*TableDiff` |
| `Engine` | `SetLogger` / `SetSlowThreshold` / `SetLogParamValues` | 注入 SQL 观测日志器、设置慢 SQL 阈值、控制慢 SQL 是否输出参数原值（默认隐藏） |
| `Engine` | `SetStatementCacheSize(size)` | 设置查询语句缓存容量；`0` 关闭，默认 256 |
| `Engine` | `Transact(txFunc)` / `TransactContext(ctx, txFunc)` | 事务包装，panic 时回滚并返回 `*PanicError`（保留栈与 `errors.Is` 链）；`ctx` 取消或超时触发回滚 |
| `Engine` | `NewSession(model ...any)` | 创建会话，可预设模型 |
| `Engine` | `SetNaming` / `GetNaming` | 设置表名命名策略（库/Schema、前缀、后缀） |
| `Session` | `UseModel` / `GetSchema` / `SetSchema` | 切换模型、读取表结构 |
| `Session` | `SetNaming` / `GetNaming` / `GetTableName` / `GetQualifiedTableName` | 单会话命名策略与物理表名 |
| `Session` | `INSERT` / `UPDATE` / `DELETE` / `SELECT` / `COUNT` | CRUD |
| `Session` | `OVERWRITE()` / `UPDATE_COLUMNS(...)` | `UPDATE(struct)` 的零值策略：默认跳过零值；`OVERWRITE` 把零值一并写回（整行覆盖）；`UPDATE_COLUMNS` 只写白名单列（零值也写回）；全零结构体返回 `ErrUpdateStructEmpty` |
| `Session` | `FindFirst` / `Exist` / `COLUMNS` | 查单条（未命中报错）/ 存在性判断 / 指定查询列 |
| `Session` | `DUPLICATE` / `UPSERT` | 冲突时更新：`DUPLICATE` 仅 MySQL 可用；`UPSERT` 冲突目标固定为模型主键（MySQL 用 `ON DUPLICATE KEY UPDATE`，PostgreSQL / SQLite 用 `ON CONFLICT ... DO UPDATE SET`） |
| `Session` | `FROM` / `WHERE` / `GROUP` / `HAVING` / `ORDER` / `LIMIT` / `OFFSET` | 阶段子句；`LIMIT(0)` 生成 `LIMIT 0`（返回 0 行），负数返回 `statement.ErrLimitNegative` |
| `Session` | `JOIN` / `INNER_JOIN` / `LEFT_JOIN` | 追加 N:1 联表子句；`ON` 条件用 `where.EQ_COLUMN` 做列对列比较 |
| `Session` | `PRELOAD(fields ...string)` | 声明本次查询要预加载的关联字段；空参加载全部声明（见[关联预加载](#15-关联预加载preload)） |
| `Session` | `CreateTable` / `DeleteTable` / `ExistsTable` | 表级操作；`ExistsTable` 返回 `(bool, error)`，查询失败不再当成“表不存在” |
| `Session` | `GetTableColumns` / `GetTableColumnTypes` / `GetTableIndexes` | 读取数据字典中的列名、列类型与索引名 |
| `Session` | `Exec` / `Query` / `QueryRow`（含 `*Context` 变体） | 逃生舱：执行原生 SQL，同样受会话超时约束 |
| `Session` | `ExecDDL` | 执行建表/改表/建索引等 DDL：不叠加会话查询超时，仍继承会话上下文 |
| `Session` | `SetContext` / `SetTimeout` | 传递请求上下文与超时；`SetTimeout(0)` 关闭默认超时 |
| `Session` | `SetLogger` / `SetSlowThreshold` / `SetLogParamValues` | 会话级日志器、慢 SQL 阈值与参数脱敏开关 |
| `Session` | `SetStatementCache(cache)` / `ClearStatementCache()` | 会话级查询缓存；`nil` 表示该会话不缓存；`ClearStatementCache` 清空引擎共享的缓存 |
| `Session` | `Begin` / `BeginTx` / `Commit` / `Rollback` | 手动事务；重复 `Begin` 返回 `ErrTransactionAlreadyStarted`，未开启事务就提交/回滚返回 `ErrTransactionNotStartedYet` |
| `Session` | `Close` / `Reset` | 回滚未提交事务并归还会话池；`Reset` 主动归零可复用状态 |
| `wxorm` | `ShowLogo` / `WriteLogo` | 按当前样式把启动标识写到标准输出 / 指定 writer |
| `wxorm` | `SetLogoStyle` / `GetLogoStyle` | 设置/读取启动标识样式（4 种，进程级） |

## 模型标签

基本标记（不区分大小写，空格分隔即可）：

| 标记 | 作用 |
| --- | --- |
| `pk` | 主键 |
| `auto` | 自增 |
| `notnull` / `null` | 是否允许为空 |
| `unique` | 唯一约束 |
| `index` | 普通索引 |
| `created` | 创建时间，由数据库填充 |
| `updated` | 更新时间，由数据库维护（PostgreSQL / SQLite 不支持，需业务赋值） |

带参数标记：

| 标记 | 作用 |
| --- | --- |
| `column(name)` | 指定列名，缺省按字段名转蛇形 |
| `type(varchar(64))` | 指定数据库类型，缺省由 Go 类型推断 |
| `default(0)` | 默认值 |
| `comment('说明')` | 注释，允许包含单引号 |
| `unique('uk_name')` / `index('idx_name')` | 命名约束或索引 |
| `fk(groups(id),'fk_user_group')` | 外键，第二个参数为约束名 |
| `generated(...)` | 生成列：MySQL 为 `VIRTUAL`，PostgreSQL 为 `STORED`（表达式原样保留） |
| `join(alias.column)` | 联表字段：只参与 SELECT 投影，不参与建表、INSERT、UPDATE（见[联表查询](#14-联表查询n1-join)） |
| `preload(column)` | 关联预加载字段：`*T` 为 belongs_to、`[]T` 为 has_many，不参与建表与读写（见[关联预加载](#15-关联预加载preload)） |
| `check(...)` / `charset(...)` / `collate(...)` / `format(...)` / `storage(...)` | 其余列属性（PostgreSQL 会忽略不支持的片段） |

标签写法同时支持 `wxorm:` 与 `orm:` 两种 key（优先 `wxorm`），值为 `-` 表示忽略该字段，
两种 key 都没有的字段不会参与建表与读写。改名前的 `worm:` 不再解析：解析模型时遇到旧键会直接返回
`schema.ErrLegacyTagRemoved`，旧模型需把 `worm:` 改为 `wxorm:`。

## 钩子

实现同名方法即生效，方法签名统一为 `func(*session.Session) error`：

| 方法 | 时机 |
| --- | --- |
| `CallBeforeInsert` / `CallAfterInsert` | 插入前后 |
| `CallBeforeUpdate` / `CallAfterUpdate` | 更新前后 |
| `CallBeforeDelete` / `CallAfterDelete` | 删除前后 |
| `CallBeforeSelect` / `CallAfterSelect` | 查询前后 |

```go
// 在插入前统一补默认值，避免每个调用点重复设置
func (own *User) CallBeforeInsert(sess *session.Session) error {
	if own.Name == "" {
		return errors.New("name is required")
	}
	return nil
}
```

## 测试

```bash
go build ./...
go vet ./...
go test ./... -count=1
```

测试不连接真实数据库：`main_test.go` 与 `session/main_test.go` 在 `TestMain` 中注册
`harness/examples/fakedriver`——一个 `database/sql/driver` 的内存实现，覆盖 CREATE/ALTER/DROP TABLE、
INSERT/UPDATE/DELETE/SELECT、`information_schema` 查询、事务提交与回滚、单列唯一约束。
它只服务于测试；生产代码不引用它。

测试资产统一放在 `harness/` 下（与 leego 的目录约定一致）：

| 目录 | 作用 |
| --- | --- |
| `harness/examples` | 测试模型与内存驱动 |
| `harness/assetpath` | 定位 `harness/testdata` |
| `harness/database` | 解析 `database.json` 并构造 DSN |
| `harness/testdata` | 连接配置与种子数据（SQLite 真库文件已忽略） |
| `harness/integration` | 真实数据库集成测试（独立子模块） |

### 真实数据库集成测试

`harness/integration/` 是独立子模块（有自己的 `go.mod` 与 `replace`），不影响根模块的零依赖约束。
它默认连接 `harness/testdata/database.json` 中配置的专用库 `wxorm_harness`，先创建真库再跑用例：

```powershell
cd harness/integration
go run ./cmd/preparedb              # 创建 MySQL / PostgreSQL / SQLite 真库，可重复执行
$env:WXORM_IT_STRICT = "1"           # 连不上时直接失败（CI 用）；不设置则跳过
go test ./... -count=1              # 三方言全量
go test ./... -run "^$" -bench . -benchtime=1s   # 性能基线
```

| 环境变量 | 默认值 / 作用 |
| --- | --- |
| `WXORM_IT_MYSQL_DSN` | `database.json` 的 `mysql` 段（本地默认指向 `127.0.0.1:3306/wxorm_harness`） |
| `WXORM_IT_PG_DSN` | `database.json` 的 `postgres` 段（本地默认指向 `127.0.0.1:5432/wxorm_harness`） |
| `WXORM_IT_SQLITE_FILE` | 覆盖 SQLite 文件；相对路径基于 `harness/testdata` |
| `WXORM_IT_DB` | 仅跑指定方言，如 `mysql` / `postgres` / `sqlite`（逗号分隔） |
| `WXORM_IT_STRICT` | 设为 `1` 时，连接失败从跳过改为测试失败 |

密码等连接参数只属于本地默认值，CI 应通过环境变量覆盖。SQLite 默认使用
`harness/testdata/sqlite/wxorm_harness.db`（该目录已在 `.gitignore` 中），DSN 已带
`busy_timeout` / `journal_mode(WAL)` / `foreign_keys(1)`；把 `sqlite.file` 置空可退回 `t.TempDir()` 隔离文件。

## 已知限制

- **MySQL、PostgreSQL、SQLite 三种方言均已端到端可用**。新方言只需实现 `clause.Clause`（通用子句），
  再按需追加能力接口（`Upserter`、`ReturningInserter`、`JSONQuerier` 等）；不支持的能力会返回
  `clause.ErrUnsupportedFeature`，不会静默生成坏 SQL。
- **`Migrate` 默认只增量、不删列**：自动补列并补建缺失索引；模型之外的列进入 `DroppedColumns`
  报告但不执行 `DROP COLUMN`，需要显式传 `wxorm.WithDropMissingColumns()`。列类型差异只输出日志
  并继续执行（无法判断数据能否无损转换）；需要事先拿到完整差异请用只读的 `DiffTable`。
- **`Session` 非并发安全**：`statement` 与模型状态是会话级可变的，请每个 goroutine 各自 `NewSession`。
  会话由引擎内部池化：`Close` 会回滚未提交事务并归零字段，重复 `Close` 无副作用，归还后不得继续使用。
- **`COLUMNS` 的列必须存在于模型**：结果按返回列名映射，未查询的字段保持零值；未知列直接返回错误。
- **单行 INSERT 的模型主键回填因方言而异**：PostgreSQL 走 `INSERT ... RETURNING` 直接回填模型字段；
  MySQL / SQLite 只提供 `Result.LastInsertId()`，模型字段保持零值，需要主键时请取结果值。
- **批量插入的主键回填因方言而异**：PostgreSQL / SQLite 用 `INSERT ... RETURNING` 逐行回填模型主键；
  MySQL 无法逐行取回生成的主键，模型主键仍为零值。`INSERT(true, ...)`（冲突忽略）与 `UPSERT` / `DUPLICATE`
  路径不回填，避免冲突行不返回导致行序错位。
- **`DUPLICATE` 仅 MySQL 可用**：PostgreSQL / SQLite 下返回 `clause.ErrUnsupportedFeature`，跨方言 UPSERT 请用 `UPSERT`；
  模型无主键时 `UPSERT` 返回 `statement.ErrConflictTargetRequired`。
- **PostgreSQL 与 SQLite 不支持 `ON UPDATE` 自动时间戳**：`created` / `updated` 列在 INSERT 时由
  `DEFAULT CURRENT_TIMESTAMP` 填值，但 UPDATE 不会自动刷新，需要触发器或业务赋值。
- **MySQL 严格模式不接受零值 `time.Time`**：可空时间用 `*time.Time`，非空时间列交给 `created` / `updated` 默认值。
- **SQLite 的自增主键必须是单列 `INTEGER PRIMARY KEY`**：`auto` 未标在 `pk` 上返回
  `column.ErrAutoIncrementRequiresPrimaryKey`，复合主键中出现 `auto` 返回 `dialect.ErrSQLiteCompositeAutoPrimaryKey`。
- **SQLite 的 `RIGHT` / `FULL JOIN` 依赖 3.39+**：方言无法静态判断版本，按支持处理；旧版本执行时由数据库报语法错。
- **SQLite 不支持表/列注释与全文检索**：`comment(...)` 被忽略，`where.FULLTEXT` 返回 `ErrUnsupportedFeature`；
  外键需要连接级 `PRAGMA foreign_keys = ON` 才会生效。
- **全文检索不建索引**：`FULLTEXT` 只生成匹配条件，GIN/倒排索引需要另行创建（SQLite 无此能力）。
- **`big.Int` / `big.Float` 可用，`big.Rat` 不可用**：big 字段按十进制文本桥接（SQLite 列类型为 `TEXT`），
  `big.Float` 默认保留 30 位小数；`big.Rat` 在模型解析阶段返回 `schema.ErrUnsupportedBigRat`（见「16. 大数支持」）。
- **`IN` 空集合与 `COUNT` 分组**：`where.IN()` 生成恒假的 `1 = 0`、`NOTIN()` 生成恒真的 `1 = 1`；
  `COUNT` 遇到 `GROUP` / `HAVING` 返回 `session.ErrCountWithGroupUnsupported`，不会静默丢弃分组。
- **默认超时可关闭也可收紧**：`SetTimeout(0)` 表示不限制，负数被忽略；
  `Exec` / `Query` / `QueryRow`（含 `*Context` 变体）均受会话超时约束。
- **DDL 不受会话查询超时约束**：`CreateTable` / `DeleteTable` / `Migrate` 与 `Session.ExecDDL`
  不会叠加默认超时（大表 `ALTER TABLE` 可能远超 10s，而 MySQL 的 DDL 不能回滚）；
  需要中断时通过 `Session.SetContext` 传入带截止时间的 context。
- **`time.Duration` 按纳秒存整数**：MySQL 为 `BIGINT`、SQLite 为 `INTEGER`，PostgreSQL 为 `INTERVAL`；
  旧版本 MySQL 建出的 `TIMESTAMP` 列需要手工改类型。
- **分表/多租户模型可复用同一类型**：实现了 `TableNameGetter` 的模型实例按自己的表名解析，
  schema 缓存键包含逻辑表名，不会把首次解析的表名固化到其它实例上。
- **查询缓存有界且仅在非事务场景生效**：默认 256 条、LRU 淘汰；事务内直接执行；执行 DDL 后整体清空。
  淘汰只标记退役，正在执行的语句在最后一个使用者归还后才 `Close`。长尾动态 SQL 较多时可调大
  `SetStatementCacheSize`，或传 `0` 关闭。
- **查询构造器支持单表、N:1 JOIN 与 `PRELOAD` 预加载**：`JOIN` / `INNER_JOIN` / `LEFT_JOIN` 负责扁平列投影
  （别名显式，结果列名为 `<别名>_<列名>`，`COUNT` 自动去重计数）；1:N 集合与嵌套对象用 `PRELOAD` 的两段式查询；
  不提供子查询与聚合结果映射，写语句不支持 JOIN。
  需要这些能力时用 `Session.Query` / `QueryRow` / `Exec`（含 `*Context` 变体）执行原生 SQL，
  再自行 `Scan` 映射；这些方法同样受会话超时约束。
- **内存驱动只覆盖测试用到的 SQL 子集**：不支持 `JOIN`、子查询、`BETWEEN`、`HAVING`；
  JOIN 的执行语义由 `harness/integration` 的真库用例覆盖。

## 文档

- [docs/analysis.md](docs/analysis.md)：分层架构分析、缺陷台账（含已修与未修）
- [docs/join-design.md](docs/join-design.md)：N:1 联表查询的设计、API 与验收清单
- [docs/preload-design.md](docs/preload-design.md)：关联预加载（Preload）与脚本化行驱动的设计、验收与真库结果
- [docs/roadmap.md](docs/roadmap.md)：分阶段更新与优化计划、验收标准
- [ISSUES.md](ISSUES.md)：问题清单与当前状态
- [RELEASE.md](RELEASE.md)：版本记录
