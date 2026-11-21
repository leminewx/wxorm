# P5 设计：Preload 关联预加载 + 脚本化行驱动

> 状态：P5.1–P5.4 已落地（v0.14.0），并已在 SQLite / MySQL / PostgreSQL 三方言上完成真库验证。
> 前置：v0.13.0 已落地 N:1 JOIN，本文补齐 1:N 与「模型嵌套」场景。
> 关联文档：[join-design.md](join-design.md)。

## 1. 背景与定位

JOIN 解决的是「一行主表 + 若干列来自关联表」的扁平投影，适合 N:1。
1:N（一个父对象挂一个子集合）走 JOIN 会带来三个难以自洽的问题：

| 问题 | 说明 |
|---|---|
| 分页语义被破坏 | `LIMIT` 作用在连接后的行上，父行数不可控 |
| 父数据重复传输 | 一个父对象 N 个子行，父列被复制 N 份 |
| 结果形态分叉 | 现有扫描是「一行 → 一个结构体」，集合需要分组键与去重 |

Preload 把「父集合」和「子集合」拆成两次查询，在内存里合并：

- 父查询完全复用现有 `SELECT`，分页、排序、`COLUMNS` 语义不变；
- 子查询是 `WHERE 关联列 IN (?, ?, ...)`，按占位符上限分批；
- 合并复杂度 O(父行数 + 子行数)，没有 N+1。

因此 **Preload 是 1:N 的推荐解法，不在 JOIN 上继续扩张**；`join-design.md` 第 8 节列出的
「1:N JOIN 结果分组」本轮不做。

## 2. 非目标

| 不做 | 原因 |
|---|---|
| 多对多（中间表）关联 | 需要 join 表元数据，属于关联模型的独立设计 |
| 复合键关联 | 语义与错误提示成本高，首版只支持单列键 |
| 子查询形式的 Preload（`IN (SELECT ...)`） | 参数与分批策略完全不同，收益低 |
| 子查询的排序 / 分页 / 过滤条件 | 首版固定按目标主键排序，避免「每个父对象 N 条」的变体 |
| Preload 下嵌套递归（A→B→C） | 首版只加载一层直接关联，见 6.2 |
| JOIN 下的 `UPDATE` / `DELETE` | 三方言语法与语义差异大，保持 `ErrJoinUnsupported` |

## 3. API 草案

### 3.1 模型标签

沿用 `join(alias.column)` 的「标签声明 + 解析期校验」风格，新增 `preload(<column>)`。
**关联方向由 Go 字段类型决定，不需要额外关键字：**

```go
type Article struct {
    ID         int64      `wxorm:"column(id) pk auto"`
    Title      string     `wxorm:"column(title) notnull"`
    AuthorID   int64      `wxorm:"column(author_id) index"`
    Status     string     `wxorm:"column(status) default('draft')"`

    // N:1（本表列 → 目标主键），column 是本表列名
    Author     *Author    `wxorm:"preload(author_id)"`

    // 1:N（目标表列 → 本表主键），column 是目标表列名
    Comments   []Comment  `wxorm:"preload(article_id)"`
}

type Author struct {
    ID   int64  `wxorm:"column(id) pk auto"`
    Name string `wxorm:"column(name) notnull"`
}

type Comment struct {
    ID        int64  `wxorm:"column(id) pk auto"`
    ArticleID int64  `wxorm:"column(article_id) index"`
    Content   string `wxorm:"column(content)"`
}
```

字段类型与语义的对应关系：

| Go 字段类型 | 关系 | `<column>` 的含义 | 匹配规则 |
|---|---|---|---|
| `*T` / `T` | `RELATION_BELONGS_TO` | 主表列名 | 主表列 = 目标表主键 |
| `[]T` / `[]*T` | `RELATION_HAS_MANY` | 目标表列名 | 目标表列 = 主表主键 |

约束：

- 目标类型 `T` 必须是结构体，且通过 `schema.Parse` 可解析（实现 `GetTableName()` 或按蛇形名）；
- 关联列不能是 `join(...)` 字段，也不能是目标表不存在的列；
- 主表与目标表的主键都必须是单列（首版）；
- 同一字段不能同时声明 `join(...)` 与 `preload(...)`；
- Preload 字段不进入 `Columns` / `ColumnNames` / `Insertable*` / `PrimaryKey*` / `UpdateFields`，
  因此建表、INSERT、UPDATE、Migrate 完全不受影响（与 JOIN 字段同一处理方式）。

### 3.2 会话方法

```go
// PRELOAD 声明本次查询要预加载的关联字段（传 Go 字段名）
sess.PRELOAD("Author", "Comments").WHERE(where.EQ("status", "published")).SELECT(&articles)

// 不传参数：加载模型声明的全部关联
sess.PRELOAD().SELECT(&articles)

// FindFirst 同样支持
sess.PRELOAD("Comments").WHERE(where.EQ("id", 1)).FindFirst(&article)
```

约定：

1. 必须显式调用 `PRELOAD`，**解析到标签不等于会查询**：避免「只想改一列却触发 5 条 SQL」的隐式行为；
2. 字段名使用 Go 字段名（`Author`），解析期按 `Schema.RelationIndexesByName` 校验，未知字段返回 `ErrPreloadUnknownField`；
3. 与 `JOIN`、`COLUMNS`、`WHERE`、`ORDER`、`LIMIT` 可同时使用，但 `COLUMNS` 必须包含分组键（见 5.3）；
4. 与 `JOIN` 一样「取走即归零」：一次查询结束后清空，不影响下一次查询与写语句；
5. `COUNT` / `Exist` 不返回模型，`PRELOAD` 被静默丢弃（不发起子查询）；
6. 写语句（INSERT / UPDATE / DELETE）带 `PRELOAD` 返回 `ErrPreloadUnsupported`，与 JOIN 的守卫一致。

## 4. 执行流程

### 4.1 主流程

```text
SELECT/FindFirst
  ├─ buildSelect()                     // 现有逻辑，返回 requestedNames 供 Preload 校验
  ├─ 执行父查询
  ├─ 扫描到 dest（现有逻辑）
  └─ loadPreloads(dest, requestedNames)
        ├─ takePreloads()              // 取出并清空 PRELOAD 状态，校验错误在此暴露
        ├─ 解析父 Schema 的 Relations（按字段名过滤、去重）
        ├─ 收集分组键（去重、跳过零值）
        ├─ 对每个 relation：
        │    ├─ schema.Parse(targetType)         // 目标 Schema，走现有缓存
        │    ├─ 校验键列（单列主键、关联列存在）
        │    ├─ 分批构建并执行子查询（独立 Statement，不碰 own.statement）
        │    ├─ 扫描子行到 []*T
        │    └─ 分配到父元素
        └─ 返回（任一子查询失败则整体失败）
```

### 4.2 关键实现点

- **子查询使用独立 `statement.NewStatement("")`**：`own.statement` 在父查询构建时已消费，
  且其 `Reset` 由 `SELECT` 的 `defer` 负责；Preload 不能复用，否则会污染状态与 `stmtCache` 键。
- **SQL 走现有 `queryPrepared`**：自动获得事务（`own.tx`）、超时、日志、慢 SQL、预编译缓存；
  整次 Preload 共用一个 `ctx`（`own.queryContext()`），避免每批重新计时。
- **不触发模型钩子**：子查询只做扫描，不调用 `CallBeforeSelect` / `CallAfterSelect`，
  避免父模型钩子被执行 N 次造成副作用。父查询钩子保持原语义。
- **键的归一化**：父键来自 Go 字段值，子键来自数据库扫描值，类型可能不同（`int64` vs `int`）。
  统一走 `stringKey(any) string`：有符号/无符号整数按十进制、浮点按 `'g'`、字符串原样、
  `[]byte` 转字符串、`time.Time` 用 RFC3339Nano；`nil` 视为「无键」直接跳过。
  子查询结果按目标列值生成同一个 `stringKey`，两侧必须共用该函数。

### 4.3 生成的 SQL

父查询保持现状（示例）：

```sql
SELECT
    `articles`.`id`, `articles`.`title`, `articles`.`author_id`
FROM
    `articles`
WHERE
    `articles`.`status` = ?;
```

`preload(author_id)`：

```sql
SELECT
    `authors`.`id`, `authors`.`name`
FROM
    `authors`
WHERE
    `id` IN (?, ?, ?)
ORDER BY
    `id`;
```

`preload(article_id)`：

```sql
SELECT
    `comments`.`id`, `comments`.`article_id`, `comments`.`content`
FROM
    `comments`
WHERE
    `article_id` IN (?, ?, ?)
ORDER BY
    `id`;
```

要点：

- 子查询显式列出目标表全部列，顺序等于 `Schema.ColumnNames`，因此扫描可以走
  `ColumnIndexesInModel` 快路径，不需要 `rows.Columns()` 映射；
- `ORDER BY` 目标表主键，保证子集合顺序稳定；用户无法覆盖（首版）；
- 关联列用裸列名（单表查询无歧义），表名仍套用会话命名策略 + `Qualify`。

### 4.4 分批

```go
// 与 SQLite 的绑定变量上限对齐，MySQL / PostgreSQL 更宽松，统一取安全值
const maxPreloadBatchParams = 30000
```

`preloadBatchSize()` 返回 `maxPreloadBatchParams`，按 `len(keys)` 切片分批；
单批失败即整体失败，不重试。父键为空（空结果集或全为零值）时不发子查询。

### 4.5 结果形态

| 关系 | 无匹配时 | 有匹配时 |
|---|---|---|
| `*T` | 保持 `nil` | 指向目标对象 |
| `T` | 保持零值 | 复制目标对象 |
| `[]T` / `[]*T` | **空切片**（非 nil） | 按目标主键升序 |

空切片而非 nil 的理由：JSON 序列化为 `[]` 而不是 `null`，前端无需特判。

## 5. 语义边界

### 5.1 与 JOIN 共存

```go
// 允许：JOIN 提供扁平列，Preload 提供集合
sess.LEFT_JOIN("authors", "author", where.EQ_COLUMN("articles.author_id", "author.id")).
    PRELOAD("Comments").
    WHERE(where.EQ("status", "published")).
    SELECT(&articles)
```

要求：`join(...)` 字段与 `preload(...)` 字段不能指向同一个目标列；`Preload` 只读取
`Schema.Relations`，不参与 JOIN 的列投影。

### 5.2 事务与上下文

- 父查询与所有子查询必须在同一事务内（`own.tx` 非 nil 时由 `queryPrepared` 自动使用该事务），
  保证父集合与子集合来自同一快照；
- 整次 Preload 共用一个 `ctx`，任一子查询超时即整体返回错误。

### 5.3 `COLUMNS` 交互

指定 `COLUMNS` 后父结果只包含部分列，分组键可能缺失：

- `belongs_to` 需要本表关联列；
- `has_many` 需要本表主键列。

`buildSelect` 增加返回 `requestedNames []string`（`nil` 表示全部列），`loadPreloads` 校验
每个 relation 的键列是否在列集内；缺失时返回 `ErrPreloadKeyNotSelected`，
而不是在内存里用零值当键静默错配。

### 5.4 与 `Session` 状态机

| 时机 | 行为 |
|---|---|
| `PRELOAD(...)` | 只记录字段名，不解析 Schema、不发 SQL |
| `SELECT` / `FindFirst` | 父查询扫描完成后执行，结束后状态归零 |
| `COUNT` / `Exist` | 静默丢弃 |
| 写语句 | `ensureNoPreload()` 返回 `ErrPreloadUnsupported` 并清空 |
| `Reset()` / `Close()` | 清空 `preloadSet` / `preloadAll` / `preloads` / `preloadErr`（池化归还不得泄漏） |

## 6. Schema 层

### 6.1 新增类型

```go
type RelationKind uint8

const (
    RELATION_BELONGS_TO RelationKind = iota + 1 // 本表列 → 目标主键
    RELATION_HAS_MANY                            // 目标表列 → 本表主键
)

type Relation struct {
    FieldName   string       // 模型字段名（PRELOAD 的参数）
    FieldIndex  int          // 模型字段索引
    Kind        RelationKind
    Column      string       // 关联列（按 Kind 解释）
    TargetType  reflect.Type // 元素类型（已去掉指针与切片）
    TargetTable string       // 目标表逻辑表名
}
```

`Schema` 增加：

```go
Relations             []Relation       // 关联声明（不参与建表与读写）
RelationIndexesByName map[string]int   // 字段名 → Relations 下标
```

### 6.2 解析规则

在 `Parse` 的主循环里，`preload(` 的判断放在 `parseJoinTag` 之后、`dial.ParseColumn` 之前：

```go
if rel, ok, err := parsePreloadTag(field.Name, tag); err != nil {
    return nil, err
} else if ok {
    rel.FieldIndex = i
    ...
    continue
}
```

`parsePreloadTag` 负责：

1. 语法：`preload(<column>)`，允许前后空白，列名不能为空、不能含空白/引号/分号/点号；
2. 字段类型：`T` / `*T` / `[]T` / `[]*T`，`T` 必须是结构体；切片元素不能是切片；
3. 目标表名：`reflect.New(T)` 断言 `TableNameGetter`，否则按 `FormatCamelToSnake`（与 `Parse` 一致）；
4. 冲突：同一字段重复声明、与 `join(...)` 同时声明、与普通列字段索引重复，均报错。

**目标 Schema 的解析延迟到首次 `loadPreloads`**：`Parse` 阶段只记录 `TargetType` 与
`TargetTable`，避免模型互相引用时在解析期递归成环；目标 Schema 由 `schema.Parse` 的现有缓存
按 `TargetType` 复用。

### 6.3 校验时机

| 校验 | 时机 | 错误 |
|---|---|---|
| 标签语法 / 字段类型 / 重复声明 | `schema.Parse` | `ErrInvalidPreloadTag` / `ErrPreloadFieldType` / `ErrPreloadRelationConflict` |
| 目标模型可解析 | 首次 Preload | 透传 `schema.Parse` 错误 |
| 单列主键 | 首次 Preload | `ErrPreloadRequiresSingleKey` |
| 关联列存在 | 首次 Preload | `ErrPreloadColumnNotFound` |
| `PRELOAD` 字段名未知 | 查询前 | `ErrPreloadUnknownField` |

## 7. 脚本化行驱动（stub driver）

### 7.1 要解决的问题

JOIN 无法在 fakedriver 上执行，只能验证 SQL 与错误路径；但**结果映射**（列名归一化、
NULL 回填、部分列零值）恰恰是最容易出错、又最难在真库上构造的部分。完整扩写 fakedriver
去实现 JOIN 等于第二套 SQL 引擎，语义漂移风险大于收益。

替代方案：一个**不解析 SQL** 的只读驱动，按调用顺序返回预置列与行。

### 7.2 设计

位置：`harness/examples/stubdriver`，与 `fakedriver` 平级，只被测试引用。

```go
package stubdriver

// Register 注册名为 wxorm-stub 的驱动（可重复调用）
func Register()

// Reset 清空脚本队列与调用记录
func Reset()

// Push 追加一次查询响应：列名 + 若干行
// 行内的 nil 表示 SQL NULL；其余值交给 database/sql 的 convertAssign
func Push(columns []string, rows ...[]driver.Value)

// PushError 追加一次「查询失败」响应
func PushError(err error)

// PushExec 追加一次写语句响应（RowsAffected / LastInsertId）
func PushExec(rowsAffected, lastInsertID int64)

// Calls 返回已执行的 SQL 文本（按顺序）
func Calls() []string

// ArgCounts 返回每次调用的参数个数（与 Calls 一一对应，用于验证分批）
func ArgCounts() []int
```

行为：

- `QueryContext` 弹出队首响应；队列为空返回 `stubdriver: no scripted result`，
  让测试显式失败而不是拿到静默空结果；
- 不解析 SQL、不校验参数，只记录文本，避免「小型 SQL 引擎」的维护成本；
- 实现 `driver.Conn` / `driver.Stmt` / `driver.Rows` / `driver.Tx` / `driver.Pinger`，
  以兼容 `queryPrepared` 的预编译路径与 `Transact`；
- 全局状态由 `sync.Mutex` 保护，`Register` 用 `sync.Once`（`sql.Register` 重名会 panic）；
- 与 `fakedriver` 的 `mysql` 驱动名不冲突，`TestMain` 里两者可同时注册。

### 7.3 覆盖的测试点

| 场景 | 断言 |
|---|---|
| JOIN 结果列 `author_name`（驱动返回的 `AS` 别名） | 映射到 `AuthorName` |
| 主表列名带一个表前缀 `scan_mapping.id` | 按最后一个点切分后匹配 `id` |
| 列名与模型字段的驼峰/蛇形差异 | 按蛇形归一后匹配 |
| NULL 子行 | `*string` 为 `nil`，非指针字段报扫描错误 |
| `COLUMNS("id")` 只返回 1 列 | 其余字段为零值，不报错 |
| 返回模型不存在的列 | `ErrSelectColumnNotFound` |
| 列顺序与模型字段顺序不同 | 按列名回填，不错位 |
| 子查询返回空行 | `has_many` 得到空切片（非 nil） |
| 子查询参数个数 | `ArgCounts` 断言分批边界（30000 + 1） |

注意：真库驱动返回的是 `AS` 后的别名（如 `author_name`），不是带引号的 `\`author\`.\`name\`` 表达式；
`normalizeColumnName` 也只按最后一个点切分、不剥引号，所以设计与测试均以内核实际收到的列名为准。

fakedriver 负责「能真跑的路径」（Preload 的父查询 + `IN` 子查询都受支持，可端到端验证），
stubdriver 负责「真库不好构造的边界」。

## 8. 实施阶段

### P5.1 测试基建：stub driver（已完成）

- 新增 `harness/examples/stubdriver`（约 150 行）；
- `session/main_test.go` 注册两个驱动；
- 新增 `session/scan_test.go`：用 stub driver 覆盖 7.3 的映射场景；
- 不触碰生产代码，可独立合入。

### P5.2 schema 层：关联声明（已完成）

- 新增 `schema/preload.go`（`Relation`、`parsePreloadTag`、校验）；
- `Schema` 增加 `Relations` / `RelationIndexesByName`；
- 新增 `schema/preload_test.go`：合法声明、字段类型错误、重复声明、与 `join` 冲突、
  关联列非法、目标表名推断；
- 此时 Preload 字段已不影响建表与读写（列集隔离）。

### P5.3 会话层：belongs_to（已完成）

- `Session` 增加 `preloadSet` / `preloadAll` / `preloads` / `preloadErr`；
- 实现 `PRELOAD`、`takePreloads`、`ensureNoPreload`、`Reset` 清理；
- 实现 `loadPreloads`（单 relation）、`stringKey`、分批子查询、赋值；
- `buildSelect` 返回 `requestedNames`，并加入键列校验；
- 新增 `session/preload_test.go`：SQL 形态、状态生命周期、`COLUMNS` 缺键、
  未知字段、写语句守卫、fakedriver 端到端（建表 → 插入 → Preload → 断言）；
- 新增 `TestSession_PreloadLifecycle`：`Reset` 后不残留。

### P5.4 会话层：has_many + 收尾（已完成）

- 支持 `[]T` / `[]*T`，空集合返回空切片；
- 新增 `harness/integration/preload_test.go`：三方言真库，覆盖空集合、
  命名策略前后缀、与 JOIN 共存、事务内预加载；
- 更新 `README.md`（新增「关联预加载」小节、API 速查、标签表、已知限制）、
  `RELEASE.md`（v0.14.0）、`docs/analysis.md`（功能矩阵 1:N 一栏）、
  `docs/join-design.md` 第 8 节（指向本文）。

## 9. 验收清单

### schema 层

- [x] `preload(author_id)` 解析为 `BELONGS_TO`，`TargetType` / `TargetTable` 正确
- [x] `preload(article_id)` 解析为 `HAS_MANY`
- [x] 重复声明（同 Kind + 目标表 + 列）→ `ErrPreloadRelationConflict`
- [x] `preload()` / `preload(a b)` / `preload(a.b)` / 缺右括号 → `ErrInvalidPreloadTag`
- [x] 切片元素为切片、目标为基本类型 → `ErrPreloadFieldType`
- [x] Preload 字段不出现在 `ColumnNames` / `Insertable*` / `PrimaryKey*` / `UpdateFields`
- [x] 目标表名：实现 `GetTableName()` 优先，否则蛇形名

### 会话层

- [x] `PRELOAD("Author")` 生成的子查询含 `WHERE \`id\` IN (?, ?, ?)` 与 `ORDER BY \`id\``
- [x] 父结果为空时不发子查询（0 条额外 SQL）
- [x] 键数超过 `maxPreloadBatchParams` 时按批拆分，参数顺序正确
- [x] `*T` 无匹配保持 `nil`；`[]T` 无匹配得到空切片
- [x] 多个父对象共享同一键时都挂载，且键去重后只查一次
- [x] `COLUMNS` 未包含键列 → `ErrPreloadKeyNotSelected`
- [x] `PRELOAD("Unknown")` → `ErrPreloadUnknownField`
- [x] 写语句带 `PRELOAD` → `ErrPreloadUnsupported`，且状态被清空
- [x] 第二次查询不含子查询（状态已归零）
- [x] `Reset()` / `Close()` 后无残留
- [x] 目标表为复合主键 → `ErrPreloadRequiresSingleKey`
- [x] 关联列不存在 → `ErrPreloadColumnNotFound`
- [x] 目标 Schema 解析失败时错误透传（`failed to parse preload target` 包装并保留 `errors.Is` 链）

### 结果映射（stub driver）

- [x] `author_name`（驱动 `AS` 后的别名）→ `AuthorName`
- [x] 主表列名带表前缀（`scan_mapping.id`）→ `id`
- [x] SQL NULL → `*string` 为 `nil`
- [x] 部分列查询时未返回字段为零值
- [x] 未知列 → `ErrSelectColumnNotFound`
- [x] 列顺序与字段顺序不一致时按列名回填

### 真库（harness/integration）

- [x] SQLite / MySQL / PostgreSQL 三方言 `belongs_to` 与 `has_many` 全绿
- [x] 零值 / nil 键不参与 `IN`（单测覆盖；真库以 9999 无匹配外键代替 NULL 列）
- [x] 命名策略前缀/后缀对目标表生效，关联列名不变
- [x] 与 JOIN 同时使用时，扁平列与集合字段都正确
- [x] 事务内 Preload 看到同一快照

## 10. 测试分布

| 层 | 文件 | 覆盖 |
|---|---|---|
| schema | `schema/preload_test.go` | 标签解析、类型校验、列集隔离 |
| session | `session/preload_test.go` | SQL 形态、状态机、错误路径、分批、零值键、共享键、fakedriver 端到端 |
| session | `session/scan_test.go` | stub driver 的结果映射边界（含 NULL → 指针） |
| integration | `harness/integration/preload_test.go` | 三方言执行语义、空集合、命名、JOIN 共存、事务内预加载 |

说明：Preload 的子查询是普通 `SELECT ... IN`，`fakedriver` 已支持，因此会话层单测
可以做端到端断言；stub driver 只补「真库与 fakedriver 都不好构造」的映射场景，
不承担 SQL 语义。两者与 integration 的分工在文档中固定，避免后续重复投入。

## 11. 风险与回退

| 风险 | 影响 | 缓解 |
|---|---|---|
| 会话状态泄漏（与 JOIN 同类） | 池化复用后继承 `PRELOAD`，产生意外查询 | `takePreloads` + `Reset`/`Close` 清理 + 生命周期测试 |
| 父子键类型不一致 | 静默错配（子集合为空或挂错父） | 统一 `stringKey`，覆盖 int/uint/float/string/[]byte 单测 |
| `COLUMNS` 漏键 | 用零值当键，全部挂到同一父对象 | 显式 `ErrPreloadKeyNotSelected` |
| 大集合内存放大 | 单个父对象挂数十万子行 | 首版文档标注；后续提供 `PRELOAD` 的 limit/分页 |
| 子查询数量增长 | 多个 relation → 多条 SQL | 每个 relation 固定一条（分批才增加），无 N+1 |
| 目标模型循环引用 | 解析期递归 | 目标 Schema 延迟解析 + 首版不递归 Preload |
| 钩子语义混淆 | 使用者以为子查询触发钩子 | 文档明确「子查询不触发模型钩子」 |

回退策略：所有改动为纯新增（新标签、新方法、新字段），不调用 `PRELOAD` 时与 v0.13.0
行为完全一致；若 P5.4 真库验证受阻，可只交付 P5.1–P5.3（belongs_to），
`has_many` 留在后续版本，不影响已发布 API。