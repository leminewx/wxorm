# WxORM 联表查询设计（N:1 JOIN）

本文档定义 WxORM 联表能力的边界、API 草案、实现分层与验收清单。
落地顺序为 P1 → P4，每一阶段都必须保持现有 14 个包的测试与三方言子句快照全绿。

> 状态：P1–P4 已全部落地（v0.13.0）。实现与本文档的差异只出现在细节补充上，已回写到对应章节。

## 1. 目标与非目标

### 目标

1. 用**一条 SQL** 完成「主表 + 关联表」的过滤、排序与分页（典型场景：文章列表按作者名筛选）。
2. 结果映射沿用现有 `SELECT` 语义：扫描到调用方传入的结构体（切片或单条）。
3. 三方言（MySQL / PostgreSQL / SQLite）行为一致；方言不支持的能力在构建期报错，而不是让数据库报语法错。
4. 不破坏既有 API：`Clause` 基础接口不新增方法，JOIN 走可选能力接口。

### 非目标（首版明确不做）

| 不做 | 原因 |
|---|---|
| 1:N 集合映射（一行父、多行子聚合成切片） | 需要行分组与去重，属于独立的结果形态设计；已由 v0.14.0 的 `Preload` 覆盖 |
| 关联预加载 `Preload` | 与 JOIN 互补，另行设计；已于 v0.14.0 落地（见第 8 节） |
| 子查询、CTE、`LATERAL` | 与 JOIN 无直接关系，收益低于成本 |
| JOIN 下的 `UPDATE` / `DELETE` | 各数据库语法差异大（MySQL 多表更新、PG `USING`），风险高 |
| 自动加索引、执行计划优化 | 属于迁移/运维范畴 |
| `SELECT *` | 列名冲突在方言间行为不一致，必须显式列 |

## 2. 现状约束

| # | 代码事实 | 对联表的影响 |
|---|---|---|
| 1 | 子句存放在 `Statement.builds map[Name]ClauseBuildFunc`，发射顺序由 `buildSelect` 传给 `BuildSQL` 的名单决定 | `NAME_JOIN` 常量已存在，插入点固定为 `FROM` 之后、`WHERE` 之前，ON 参数自然排在 WHERE 参数之前 |
| 2 | 方言 = 基础 `Clause`（18 个通用方法）+ 可选能力接口 + `ErrUnsupportedFeature` | JOIN 用可选接口 `Joiner`，第三方方言不被破坏 |
| 3 | 三方言的 `BuildSelectSQL` 带 `selectCache`，键为 `table + columns` | JOIN 的 SELECT 列集不走该缓存（直接写投影），避免缓存键无法表达别名 |
| 4 | `scanFieldsOf` 用 `rows.Columns()` 映射模型字段，未声明列报 `ErrSelectColumnNotFound` | JOIN 结果必须走列名映射；`ColumnIndexesInModel` 快路径仅限单表 |
| 5 | `COLUMNS` 只校验模型列名 | 需扩展为「主表列名或 join 别名列」 |
| 6 | `SELECT` / `FindFirst` 用 `defer statement.Reset()`；`columns`、`limit` 在 `buildSelect` 取走后归零 | `joins` 必须同生命周期，且不得泄漏到写语句 |
| 7 | 查询统一走 `queryPrepared` + StatementCache（SQL 文本为键） | JOIN 查询自动进入预编译缓存，该层零改动 |

## 3. API 草案

### 3.1 方言层（`dialect/clause`）

- `type JoinKind string`，常量：`JOIN_INNER` / `JOIN_LEFT` / `JOIN_RIGHT` / `JOIN_FULL` / `JOIN_CROSS`。
- 可选接口：

  `Joiner interface { BuildJoinSQL(ctx *Context, kind JoinKind, table, alias string) error; SupportsJoinKind(kind JoinKind) bool }`

- 职责划分：方言只写「关键字 + 表名 + 别名」（`\nLEFT JOIN\n    \`authors\` AS \`author\``），ON 条件由 `statement` 层拼接。
- 能力矩阵：

  | 方言 | INNER | LEFT | RIGHT | FULL | CROSS |
  |---|---|---|---|---|---|
  | MySQL | 支持 | 支持 | 支持 | ✗（返回 `ErrUnsupportedFeature`） | 支持 |
  | PostgreSQL | 支持 | 支持 | 支持 | 支持 | 支持 |
  | SQLite | 支持 | 支持 | 3.39+ | 3.39+ | 支持 |

  SQLite 无法静态判断版本，按支持处理并在 README 标注最低版本要求。

### 3.2 子句层（`statement`）

- `type SelectColumn struct { Table, Name, Alias string }`：`Alias` 为空表示不加 `AS`。
- `SELECT_COLUMNS(columns ...SelectColumn)`：显式写出投影列，主表列与 join 列统一走 `QuoteIdentifier`。
- `type Join struct { Kind clause.JoinKind; Table, Alias string; On where.Condition }`。
- `JOIN(joins ...Join)`：单个构建函数遍历全部 join，避免 `AddClause` 同名覆盖。
- `COUNT_DISTINCT(column string)`：写 `SELECT COUNT(DISTINCT col)`，供联表统计去重使用。
- 校验：非 CROSS 的 join 必须带 `On`（`ErrJoinConditionRequired`）；CROSS 不允许带 `On`（`ErrJoinConditionNotAllowed`）。

### 3.3 条件层（`statement/where`）

- ON 条件复用 `where.Condition`，新增列对列比较：

  `EQ_COLUMN(left, right string) Condition` → `left = right`（右侧写标识符，不占位符）。

- 其他比较仍可用 `EQ/NE/GT/GTE/LT/LTE`，右侧只支持「值」；列对列的扩展按需追加。

### 3.4 会话层（`session`）

- 入口：

  `JOIN(kind clause.JoinKind, table, alias string, on where.Condition) *Session`
  `INNER_JOIN(table, alias string, on where.Condition) *Session`
  `LEFT_JOIN(table, alias string, on where.Condition) *Session`

- 表名处理：`table` 是**逻辑表名**（模型 `GetTableName` 的返回值），`naming` 的 `Prefix/Suffix` 与 `Schema` 由 `JOIN` 统一套用；别名不套命名策略，且必须显式给出。
- 状态：`Session.joins []statement.Join`，在 `buildSelect` / `COUNT` / `Exist` 中「取走并清空」，错误先存 `joinErr`。
- 写语句拦截：`INSERT` / `UPDATE` / `DELETE` 发现 `joins` 非空时返回 `ErrJoinUnsupported`，并清空 joins。
- 别名校验：非空、不含 `.`、引号、空白；非法时记录 `joinErr`，在构建期返回。
- 错误一览：`ErrJoinUnsupported`、`ErrJoinTableRequired`、`ErrJoinAliasRequired`、`ErrJoinAliasInvalid`、`ErrJoinRequiresSinglePK`、`ErrJoinCountColumnUnsupported`、`ErrJoinAliasNotJoined`、`ErrJoinColumnNotNilable`（均可用 `errors.Is` 判断）。

### 3.5 模型层（`schema`）

- 标签语法：`wxorm:"join(<alias>.<column>)"`，例如：

  ```go
  type Article struct {
      ID         int     `wxorm:"primary_key"`
      Title      string  `wxorm:"size:200"`
      AuthorID   int     `wxorm:"index"`
      AuthorName *string `wxorm:"join(author.name)"`
  }
  ```

- 解析结果单独存放在：

  `type JoinColumn struct { Alias, Name, As string; Index int }`
  `Schema.JoinColumns []JoinColumn`
  `Schema.JoinColumnIndexesByName map[string]int`

- 关键约束：
  1. join 字段**不进入** `Columns` / `ColumnNames` / `Insertable*` / `PrimaryKey*` / `UpdateFields`，保证建表与写语句完全不受影响；
  2. 结果列别名固定为 `<alias>_<column>`（`author.name` → `author_name`）；
  3. 与主表列或其它 join 别名冲突时，解析阶段返回 `ErrJoinColumnConflict`；
  4. 标签格式非法返回 `ErrInvalidJoinTag`。

- NULL 语义：`LEFT` / `FULL` join 存在时，join 字段必须可空（指针、`interface`、`slice`、`map`，或实现 `sql.Scanner` 的类型），否则在构建查询时返回 `ErrJoinColumnNotNilable`。

## 4. SQL 形态

```sql
SELECT
    `articles`.`id`, `articles`.`title`, `articles`.`author_id`, `author`.`name` AS `author_name`
FROM
    `articles`
LEFT JOIN
    `authors` AS `author`
ON
    `articles`.`author_id` = `author`.`id`
WHERE
    `author`.`name` LIKE ?
ORDER BY
    `articles`.`id` DESC
LIMIT
    20;
```

要点：

- 主表列用物理表名限定（`articles.id`），避免 join 表同名列造成歧义；
- join 列必须带 `AS`，结果列名即 `<alias>_<column>`；
- `ORDER BY` / `GROUP BY` 的列需要调用方自行限定（`author.name`），`QuoteIdentifier` 支持点号分段；
- 占位符：ON 在 WHERE 之前发射，`?` / `$n` 顺序天然正确。

## 5. 语义约定

| 场景 | 约定 |
|---|---|
| `SELECT` / `FindFirst` | 支持 JOIN；`FindFirst` 隐含 `LIMIT 1`，同样生效 |
| `COLUMNS` | 列名可以是主表列名，也可以是联表结果列别名（`author_name`）；未知列返回 `ErrSelectColumnNotFound` |
| `COUNT` | JOIN 存在时改为 `COUNT(DISTINCT 主表单列主键)`；不接受列参数（`ErrJoinCountColumnUnsupported`）；模型无主键或复合主键返回 `ErrJoinRequiresSinglePK` |
| `Exist` | 支持 JOIN（`SELECT 1 FROM t JOIN ... LIMIT 1`） |
| `GROUP` / `HAVING` | 可用，列需自行限定；聚合结果不映射到模型字段（沿用现状限制） |
| `LIMIT` / `OFFSET` | 按 N:1 语义安全；1:N 未实现，文档警示 |
| 写语句 | 一律拒绝 |
| 钩子 | `BeforeSelect` / `AfterSelect` 语义不变 |
| 事务 / 超时 / 日志 / `Result` | 复用 `queryPrepared`，零改动 |
| 状态清理 | joins 随查询消费归零；错误路径与 `Reset` / `Close`（池化归还）同样清空 |

## 6. 分阶段实施

### P1 子句骨架（已完成）

- `dialect/clause`：`JoinKind`、`Joiner`、三个方言实现与能力矩阵。
- `statement`：`Join`、`JOIN`、`SELECT_COLUMNS`、`EQ_COLUMN`。
- 测试：三方言 SQL 快照、非法 kind、MySQL FULL 拒绝、CROSS/ON 组合校验、参数顺序。

### P2 会话入口（已完成）

- `Session.JOIN` / `INNER_JOIN` / `LEFT_JOIN`、别名与表名校验、`joins` 生命周期、写语句拦截。
- `buildSelect` 接入 JOIN 子句（此时结果映射仍只支持单表，先保证 SQL 正确）。
- 测试：joins 串联不泄漏、写语句拒绝、别名非法报错、SQL 文本断言。

### P3 结果映射（已完成）

- `schema`：`join(...)` 标签解析、`JoinColumns`、冲突检测。
- `session`：JOIN 时的投影列与扫描计划、`COLUMNS` 扩展、NULL 可空校验。
- 测试：join 列命中、未声明列报错、LEFT JOIN 全 NULL、`COLUMNS` 与 JOIN 组合、冲突标签报错。

### P4 语义收敛与文档（已完成）

- `COUNT` / `Exist` 支持 JOIN；`COUNT` 主键约束。
- README、RELEASE、`docs/analysis.md` 更新，移除「不提供 JOIN」的限制描述。
- `harness/integration` 三方言真库回归：N:1、LEFT NULL、分页、naming 前后缀、`-race`。

## 7. 验收清单

### P1

- [x] `mysql` / `postgres` / `sqlite` 均实现 `clause.Joiner`，编译期断言通过。
- [x] `BuildJoinSQL` 输出 `\nLEFT JOIN\n    \`authors\` AS \`author\``，别名与表名均被正确引用。
- [x] MySQL 的 `JOIN_FULL` 返回 `ErrUnsupportedFeature`；其余方言全部 kind 通过。
- [x] `statement.JOIN` 在 `FROM` 之后、`WHERE` 之前发射；多个 join 顺序稳定。
- [x] ON 带参 + WHERE 带参时，参数顺序与占位符编号正确（PG 的 `$n` 连续）。
- [x] 非 CROSS 缺 ON、CROSS 带 ON 均返回既定错误。
- [x] `EQ_COLUMN` 生成 `"a"."c" = "b"."c"`，不产生参数。
- [x] 现有子句快照测试全部保持原样通过。

### P2

- [x] `INNER_JOIN` / `LEFT_JOIN` 生成的 SQL 与 `JOIN(JOIN_LEFT, ...)` 完全一致。
- [x] 连续两次查询，第二次 SQL 不包含第一次的 JOIN。
- [x] `INSERT` / `UPDATE` / `DELETE` 在存在 joins 时返回 `ErrJoinUnsupported`，且 joins 被清空。
- [x] 空别名、含点号/引号/空白的别名在构建期报错。
- [x] join 表名正确套用 naming 的前后缀与 schema。
- [x] `-race` 无数据竞争。

### P3

- [x] `join(author.name)` 字段不参与建表、INSERT、UPDATE。
- [x] 查询结果列 `author_name` 正确映射到模型字段；LEFT JOIN 无匹配时为 `nil`（指针）。
- [x] 未声明的返回列仍返回 `ErrSelectColumnNotFound`。
- [x] `COLUMNS("id", "author_name")` 只查这两列，其余字段保持零值。
- [x] LEFT/FULL join 下非可空 join 字段返回 `ErrJoinColumnNotNilable`。
- [x] `join(author.name)` 与主表列/其它 join 别名冲突时返回 `ErrJoinColumnConflict`。
- [x] 非法标签（缺列名、多个点号、空别名）返回 `ErrInvalidJoinTag`。
- [x] 单表查询路径（无 join）的分配与 SQL 文本与改造前一致。

### P4

- [x] JOIN + `COUNT` 生成 `COUNT(DISTINCT 主表.主键)`；无主键/复合主键返回 `ErrJoinRequiresSinglePK`。
- [x] JOIN + `Exist` 正确返回布尔值。
- [x] 三方言真库：N:1 内连接、LEFT JOIN 空行、分页、naming 前后缀全部通过。
- [x] README / RELEASE / analysis 不再声明「不提供 JOIN」。
- [x] `gofmt -l .` 空、`go vet ./...` 干净、`go test ./... -count=1` 全绿、`go test -race ./...` 通过。

### 验收用例分布

| 层 | 文件 | 覆盖内容 |
|---|---|---|
| 方言 | `dialect/clause/join_test.go` | 三方言 SQL 快照、能力矩阵、kind/表名/别名校验 |
| 子句 | `statement/join_test.go`、`statement/where/column_test.go` | 投影顺序、多 JOIN、参数顺序、`EQ_COLUMN`、`COUNT_DISTINCT` |
| 模型 | `schema/join_test.go` | 标签解析、列集隔离、冲突与非法格式 |
| 会话 | `session/join_test.go` | SQL 文本与子句顺序、naming、生命周期、写语句拦截、NULL 与统计约束 |
| 真库 | `harness/integration/join_test.go` | N:1 映射、LEFT NULL、`COLUMNS`、分页、`COUNT`/`Exist`、naming 前后缀 |

说明：根模块的内存驱动（`harness/examples/fakedriver`）仍未实现 JOIN，
因此会话层单测只验证 SQL 构建与错误路径；真正的执行语义由 `harness/integration`
负责（默认跑 SQLite，MySQL/PostgreSQL 在配置了 DSN 或 `WXORM_IT_STRICT=1` 时参与）。

## 8. 后续方向（不在本轮）

- ✅ `Preload`（v0.14.0 已落地）：父查询 + `IN` 批量子查询 + 内存合并，解决 1:N 与「模型嵌套」场景；
  与 JOIN 互补，可与 JOIN 同时使用。实际形态、验收清单与三方言结果见 [preload-design.md](preload-design.md)。
- 1:N JOIN 结果分组：需要定义分组键（主表主键）与子切片类型，属于结果形态层面的设计。
- `Scan` 到 `map[string]any` / 标量：报表类查询的通用出口。
- JOIN 下 `UPDATE` / `DELETE`：按方言分别实现，暂不进入公共 API。