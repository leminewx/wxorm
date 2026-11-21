# wxorm 架构分析与缺陷台账

本文是对 `wxorm` 项目的阶段性分析记录：先说清它「怎么运转」，再记录「哪里有问题、修了什么、还欠什么」。
所有结论都对应到具体包与符号，便于逐条核对。

- 统计口径：源码 39 个文件约 6300 行，测试 44 个文件约 7100 行，共 12 个包。
- Go 版本：`go.mod` 声明 `go 1.27.4`；除标准库外无第三方依赖。

---

## 一、分层结构

```
                 ┌──────────────────────────────────────────┐
   用户代码 ───► │ Engine（wxorm.go）                        │
                 │  连接池、Migrate、Transact、会话工厂       │
                 └───────────────┬──────────────────────────┘
                                 │ NewSession
                 ┌───────────────▼──────────────────────────┐
                 │ Session（session/）                       │
                 │  CRUD、阶段子句、Hook、事务、建删表        │
                 └───────┬──────────────────────┬───────────┘
                         │                      │
        ┌────────────────▼───────┐   ┌──────────▼───────────────┐
        │ Statement（statement/）│   │ Schema（schema/）         │
        │  子句集合 + 按名装配   │   │  反射解析模型 + 缓存       │
        │  where 条件（statement/where）                        │
        └────────────────┬───────┘   └──────────┬───────────────┘
                         │                      │
                 ┌───────▼──────────────────────▼───────────┐
                 │ Dialect（dialect/）                       │
                 │  MySQL DDL/DML 生成、标识符引用、类型映射  │
                 │  ├── clause/   子句 SQL 构造 + 占位符上下文 │
                 │  ├── column/   结构体标签解析为列定义       │
                 │  └── datatype/ Go 类型 ↔ 数据库类型         │
                 └──────────────────────────────────────────┘
```

职责边界比较清晰，是这套代码最大的优点：

| 层 | 只做一件事 | 反例（曾经存在的问题） |
| --- | --- | --- |
| `dialect` | 把「结构化的子句」翻译成某个方言的 SQL 文本 | 曾经在同一份 SQL 里既拼字面量又拼占位符，参数与文本对不上 |
| `session` | 组织一次数据库交互的时序（钩子、事务、超时、结果） | 曾经在事务内复用 `*sql.DB` 的预编译语句，导致语句逃逸事务 |
| `schema` | 把 Go 结构体解析为「表名 + 列定义」并缓存 | 曾经用 `Type.String()` 当缓存键，函数内同名局部类型互相污染 |
| `statement` | 只保管子句，不关心 SQL 长什么样 | 曾经在多次复用之间不清理，`WHERE` 泄漏进 `INSERT` |

## 二、一次查询的完整链路

以 `sess.WHERE(where.EQ("id", 1)).SELECT(&user)` 为例：

1. `WHERE` 把 `where.Condition` 存入 `Statement` 的 `map[Name]ClauseBuildFunc`。
2. `SELECT` 先 `GetSchema()` 拿到表名与列清单，再补齐 `SELECT` / `FROM` 子句。
3. `Statement.BuildSQL(dialect, NAME_SELECT, NAME_FROM, ...)` 按给定顺序折叠生成器。
4. 各生成器把 SQL 片段写入 `clause.Context`，参数一律走
   `Context.WritePlaceholderWithArg`，保证「占位符个数 == 参数个数」。
5. `Context.GetResult()` 返回 `(sql, args)`，由 `Session` 交给 `database/sql` 执行。
6. 结果包装成 `session/result` 里的实现，记录 SQL、参数与耗时。

链路本身很短，问题几乎都出在**缓存**与**状态清理**两处：wxorm 为了性能做了大量 `sync.Map` 缓存，
而缓存键一旦漏掉某个影响 SQL 文本的输入，就会静默命中错误的 SQL。

## 三、缺陷台账

优先级定义：

- **P0**：会产生错误结果、数据损坏或让测试无法运行。
- **P1**：边界条件下出错，或让行为不符合直觉。
- **P2**：可维护性、一致性、文档类问题。

### 3.1 P0（本轮已全部修复）

| # | 位置 | 问题 | 修复方式 |
| --- | --- | --- | --- |
| P0-1 | `dialect/clause/mysql.go` `QuoteIdentifier` | 名字含空格时**原样返回**，既打开注入面，又与「必须加反引号」的用例矛盾 | 拆出 `DISTINCT` 前缀与 `ASC/DESC` 后缀，其余按 `.` 分段逐段加反引号 |
| P0-2 | `dialect/clause/mysql.go` `BuildSelectSQL` | 缓存键只有表名，不同列集共用同一条 SQL（表现为所有 `SELECT` 都变成 `SELECT *`） | 缓存键改为 `表名 + 分隔符 + 列列表` |
| P0-3 | `dialect/clause/mysql.go` `BuildInsertSQL` / `BuildDuplicateSQL` | 缓存键分别漏掉列列表、用无分隔符的列名拼接，不同语句相互覆盖 | 键内统一使用 `\x1f` 作为列分隔符 |
| P0-4 | `dialect/clause/mysql.go` JSON 子句 | 缓存键 `column + path` 无分隔符；路径不合法（空、缺 `$.`） | 键加分隔符；新增 `normalizeJSONPath` 归一化路径 |
| P0-5 | `dialect/clause/mysql.go` `BuildValuesSQL` | `values` 为空时 `values[0]` 直接 panic | 返回 `ErrValuesIsEmpty` |
| P0-6 | `dialect/clause/mysql.go` `BuildFullTextSearchSQL` | 搜索词参数被 `_ string` 丢弃，`?` 没有对应参数（等于全表命中） | 参数改名 `term` 并写入 `WritePlaceholderWithArg` |
| P0-7 | `dialect/mysql.go` `buildForeignKeys` | `ON UPDATE` 分支误写成 `ON DELETE CASCADE` | 改为 `ON UPDATE CASCADE` |
| P0-8 | `dialect/column/parser.go` | `comment('unique')`、`check(x is not null)` 里的词被当成无参标记，误触发 `UNIQUE` / `NOT NULL` | 新增 `stripParamGroups`，先剔除带参数标记再解析基本标记 |
| P0-9 | `schema/cache.go` | 以 `modelType.String()` 为键，**函数内同名局部类型**命中同一条缓存（`schema.ValidateStruct` 失败的真正原因） | 键改为 `{driver, reflect.Type}`，方言也参与缓存维度 |
| P0-10 | `session/hooks.go` | 钩子取 `own.schema.Model`，模型切换后作用在旧模型上，且为 nil 时直接解引用 | 统一改为 `own.model` |
| P0-11 | `session/record.go` `insertOne` | 事务内复用 `*sql.DB` 的预编译语句，语句在事务外执行——**数据逃逸事务** | 仅在 `own.tx == nil` 时走预编译缓存 |
| P0-12 | `session/session.go` | `Session` 没有 `ctx`，`context.WithTimeout` 收到 nil 后 panic | 增加 `ctx` 字段与 `SetContext` / `GetContext` |
| P0-13 | `session/record.go` 全部构造器 | 未清理 `statement`，上一条 SQL 的 `WHERE` 会残留到下一条 `INSERT` | 每个导出方法 `defer own.statement.Reset()` |
| P0-14 | `session/result/*.go` | 结果对象直接持有调用方的 `args` 切片，下一次构建原地覆盖历史结果 | 新增 `copyArgs` 做防御性拷贝 |
| P0-15 | `wxorm.go` `Migrate` | 用 `Transact` 包 DDL（DDL 隐式提交，事务无意义）；删除字段用 `CREATE AS SELECT` + `DROP` + `RENAME` **重建整表** | 改为普通会话；删字段改为逐列 `ALTER TABLE ... DROP COLUMN` |
| P0-16 | `wxorm.go` `Migrate` | `Rows.Columns()` 的错误被忽略，失败后继续用空列集 | 判错并 `defer rows.Close()` |
| P0-17 | `wxorm.go` `extractDBName` | `Split("/")[1]` 在密码含 `/` 时错位，空库名也不报错 | 取最后一个 `/` 之后、`?` 之前的部分，空值报错 |
| P0-18 | `session/session.go` `New` | 调用方传 `nil` 语句缓存时，`stmts.Load` 空指针 | `nil` 时补一个空 `sync.Map` |

### 3.2 P1（本轮已修复）

| 位置 | 问题 | 修复方式 |
| --- | --- | --- |
| `dialect/clause/mysql.go` `BuildSetSQL` | `map` 遍历顺序不定，同一次调用的 SQL 文本不稳定 | 先 `slices.Sort` 列名再拼接 |
| `dialect/mysql.go` `buildUnionIndexes` / `buildUnionUniques` / `buildForeignKeys` | 同样是 `map` 遍历，DDL 文本每次不同 | 统一走 `sortKeys` 泛型助手排序 |
| `session/record.go` `updateByStruct` | 主键为零值时直接 `return nil, nil`，静默什么都没做 | 手动 `WHERE` 优先；两者都没有才报错 |
| `session/record.go` `ensureWhereClause` | 不区分「表无主键」与「主键值为零」 | 分别返回 `ErrTableNoPK` / `ErrTablePKValuesZero` |
| `session/record.go` `selectMore` | `OFFSET` 无 `LIMIT` 时生成 MySQL 非法 SQL | 提前返回 `ErrOffsetRequiresLimit` |
| `session/record.go` | `if own.statement.Reset(); err != nil` —— `Reset` 无返回值，判断恒假 | 改为正确的错误判断 |
| `dialect/*`、`schema/*` | 错误前缀不统一，混用 `wxorm` / `dialect` / `schema` 等前缀 | 统一为 `wxorm:` |
| `wxorm.go` | 手写自旋锁 `Engine.lock`，溢出会永不自增 | 改为纯 `atomic` 计数 |
| `wxorm.go` `SetConnMaxLifetime` | 内部调用无副作用的 `db.Stats()` | 删除 |
| `harness/examples/model.go` | 示例包 `import "testing"`，把测试依赖带进生产构建 | 改为 `SetupTestDB() (*sql.DB, error)` |
| 测试基建 | 所有测试依赖内网 MySQL（`10.254.249.16:3306`），离线必挂 | 新增内存驱动 `harness/examples/fakedriver` |

### 3.3 仍未处理（P1 / P2）

| 优先级 | 问题 | 说明 |
| --- | --- | --- |
| P1 | `Session` 不是并发安全的 | `statement`、`schema` 是会话级可变状态，一个会话不能被多个 goroutine 共用；文档需要显式声明 |
| P1 | `Migrate` 不做无损类型转换 | 已提供只读 `DiffTable`，`Migrate` 补建缺失索引、类型差异只提示；不再隐式删列（需要 `WithDropMissingColumns()`） |
| P1 | `Query` / `QueryRow` 不吃会话超时 | 直连版本不再叠加 timer（结果集未消费完无法 cancel 会泄漏），需要超时请用 `QueryContext` / `QueryRowContext` |
| P2 | 内存驱动只覆盖测试用到的 SQL 子集 | 不支持 `JOIN`、子查询、`BETWEEN`、`GROUP BY ... HAVING`，新增用例前需要先扩驱动；DSL 已支持 N:1 `JOIN`，执行语义由 `harness/integration` 真库用例覆盖 |
| P2 | 无 CI 配置 | 没有 `golangci-lint` / GitHub Actions / Gitee Go 流水线 |

原「PostgreSQL / SQLite 方言是半成品」一项已在第三轮（PostgreSQL）与第五轮（SQLite）解决，
当前三方言能力对照见第六节。

### 3.4 第二轮修复（正确性 / 安全 / 性能）

第二轮不再依赖「上一轮结论」，而是重读了 `statement`、`dialect/clause`、`schema`、`session`、`wxorm` 五层代码，
并用基准测试定位热点。共修 9 项，其中 3 项是真正会导致错误结果或安全问题的缺陷：

| # | 位置 | 问题 | 修复方式 |
| --- | --- | --- | --- |
| P0-19 | `session/record.go` `insertOne` | 预编译缓存键只有 `表名 + ignore 标志`。**同一张表对应两个模型**（列集不同）时共用同一条预编译语句，第二个模型的参数会按第一个模型的列顺序绑定——静默写错列 | 缓存键改为 `schema` 预计算的 `InsertCacheKey` / `InsertIgnoreCacheKey`，包含可插入列集；`schema` 层单独测试键不碰撞 |
| P0-20 | `dialect/clause/mysql.go` JSON 子句 | JSON 路径被直接拼进 SQL 字符串字面量，路径里的 `'` 可以闭合字面量并追加任意 SQL | 新增 `quoteJSONPath`，包单引号并转义内部单引号 |
| P0-21 | `dialect/clause/mysql.go` `BuildFullTextSearchSQL` | 检索模式（`IN BOOLEAN MODE` 等）原样拼接进 SQL，属于同一类注入面 | 新增 `normalizeFullTextMode` 白名单：去空白、转大写后匹配 5 种合法模式，其余返回 `ErrInvalidFullTextMode` |
| P1 | `wxorm.go` `NewEngine` | 先 `sql.Open`，再 `Ping`、再解析方言；`Ping` 或解析失败时直接 `return nil, err`，连接池未关闭 | 先 `dialect.Lookup` 校验方言；`Ping` 失败时 `db.Close()` 后再返回 |
| P1 | `wxorm.go` `Migrate` | `ADD COLUMN` 只写 `列名 类型`，丢掉 `NOT NULL` / `DEFAULT` / `COMMENT`，新增列与建表时的定义不一致 | 复用 `column.Column.BuildCreateSQL`，生成完整列定义 |
| P1 | `session/session.go` 超时 | 默认 10s 且 `SetTimeout` 只接受 `> 1s`，无法关闭；每条 SQL 都 `context.WithTimeout` 建一个 timer | `SetTimeout(0)` 表示不额外设超时；新增 `execContext()`，无超时时直接复用会话 ctx，返回空 cancel |
| P1 | `schema/schema.go` `GetReflectFieldValue` | 默认分支用 `reflect.DeepEqual` 判零值，`time.Time` 这种结构体每次都要深度比较 | 改用 `reflect.Value.IsZero`，语义一致、零分配；`IsZeroValue` 同步改为 `IsZero` 并补 `IsValid` 守卫 |
| P2 | `dialect/clause/clause.go` | `stringsBuilderPool` 取出的 builder 已被 `Reset`，容量早就丢了，池只带来同步开销 | 删除池，改为按需 `Grow` 预分配（`BuildValuesSQL` 按 `行数 × (列数×3+8)`） |
| P2 | `dialect/dialect.go` | `Get` 对未注册方言返回 `nil`，调用方解引用即 panic | 新增 `Lookup(name) (Dialect, error)`，返回 `ErrUnsupportedDriver`；`Register` / `Get` 对名字做 `TrimSpace + ToLower` 归一化 |

同时补齐了上一轮遗留的功能缺口与测试盲区：

- `Session.FindFirst(dest)`：查不到返回 `ErrRecordNotFound`，把「没数据」与「查询失败」分开。
- `Session.Exist()`：生成 `SELECT 1 FROM ... LIMIT 1`，不统计全表。
- `Session.COLUMNS(...)`：指定查询列，结果按返回的列名映射回模型字段；未知列返回 `ErrSelectColumnNotFound`。
- `schema.Schema` 新增 `ColumnIndexesByName` 与两个插入缓存键；`statement` 新增 `EXISTS` 子句。
- 补测试：`session/result`、`dialect`（原为空文件）、JSON/全文检索注入、零值判断、插入缓存隔离。
- 补基准：`session/bench_test.go`（SELECT / COLUMNS / First / Exist / INSERT / UPDATE）与 `BenchmarkGetReflectFieldValue`。

## 四、测试基建：为什么需要一个内存驱动

原测试直接 `sql.Open("mysql", DBSource)`，而 `go.mod` 里没有 MySQL 驱动，
DSN 又指向内网地址——这意味着**任何人都无法在本地跑通 `go test`**，46 个用例全红。

新增的 `harness/examples/fakedriver` 是一个 `database/sql/driver` 的内存实现，注册名同样是 `mysql`：

- **数据模型**：`database{tables}` → `table{columns, uniques, defaults, rows, auto, nextID, meta}`。
- **连接与事务**：每条连接持有 `txData` 快照；提交时整体写回，回滚时直接丢弃。
- **SQL 解析**：`normalizeSQL` 先剔除注释（wxorm 的 DDL 会在字段间插入 `-- 注释`），
  再折叠空白，然后按前缀分派到 CREATE / DROP / ALTER / INSERT / UPDATE / DELETE / SELECT /
  `information_schema`。
- **条件求值**：递归处理 `OR` / `AND` / `NOT` / 括号，原子条件支持比较、`IS [NOT] NULL`、
  `[NOT] IN`、`[NOT] LIKE`；参数按 SQL 中的 `?` 顺序消费。
- **语义保真点**：单列 `UNIQUE` 冲突必须报错（事务回滚用例依赖它）、
  `AUTO_INCREMENT` 回填并返回 `LastInsertId`、`DEFAULT CURRENT_TIMESTAMP` 在插入时填充、
  `information_schema.tables` 能区分「表存在」与「表不存在」。

它是纯粹的测试基础设施：只在 `TestMain` 里 `Register()`，不参与运行时代码。

## 五、性能：热点定位与基准

优化前先用基准把「ORM 自己的开销」和「驱动/网络的开销」分开。这一步很关键：
如果直接看 `session` 级基准，会误以为 ORM 很慢，实际大头在内存驱动解析 SQL。

### 5.1 基准分层

| 层级 | 基准 | 度量的是什么 |
| --- | --- | --- |
| 子句 | `BenchmarkMySQLClause_BuildInsertSQL` / `BuildSelectSQL` / `QuoteIdentifier` | 纯字符串装配，不含反射与驱动 |
| 语句 | `BenchmarkStatement_INSERT` | 子句折叠 + 占位符与参数配对 |
| 反射 | `BenchmarkSchema_ExtractValue` / `BenchmarkGetReflectFieldValue` | 结构体字段取值与零值判断 |
| 会话 | `BenchmarkSessionSelectRows` / `First` / `Exist` / `InsertOne` / `UpdateStruct` | 含驱动与结果扫描的端到端路径 |

### 5.2 优化前后的数据

`-benchmem -benchtime 500x`（会话级为准为多次运行的中位数量级，仅供横向比较）：

| 基准 | 优化前 | 优化后 | 说明 |
| --- | --- | --- | --- |
| `Statement_INSERT` | 423.5 ns / 602 B / 9 allocs | **347.4 ns / 579 B / 9 allocs** | −18%，去掉无效 builder 池 + 预分配 |
| `Session_Insert-1` | 3219 ns / 40 allocs | **2207 ns / 39 allocs** | −31%，不再建多余 timer，不再每行装箱 |
| `Session_Insert-10` | 7488 ns | **5338 ns** | −29% |
| `Session_Insert-100` | 1.10 ms | **0.79 ms** | −28% |
| `Session_Insert-1000` | 4.80 ms | **4.26 ms** | −11%，驱动行解析占比越来越大 |
| `Session_Update_Map` | 2728 ns | **2540 ns** | 略快 |
| `Session_Update_Struct` | 3777 ns | **3654 ns** | 略快 |
| `GetReflectFieldValue` | `reflect.DeepEqual` 分支 | **18.0 ns / 24 B / 1 alloc** | `time.Time` 上不再深度比较 |
| `QuoteIdentifier` | — | **15.2 ns / 0 alloc** | 标识符引用路径零分配 |
| `BuildInsertSQL` | — | **138.4 ns / 162 B / 3 allocs** | 新的参考基线 |
| `BuildSelectSQL` | — | **92.4 ns / 19 B / 2 allocs** | 新的参考基线 |

### 5.3 怎么读这些数字

- **会话级基准不要看 allocs。** `Insert-100` 的 13177 allocs ≈ 每行 132 次，其中绝大部分是内存驱动
  解析 SQL、按 `?` 切参数、造 `driver.Value`；ORM 自身只有每行 1–2 次反射取值。要看 ORM 开销，
  用 `Statement_*` 与 `Schema_*` 两列。
- **`Session_Insert-1000` 只快 11% 不代表优化无效。** 固定成本（拼 SQL、预编译查表、开事务）被行数摊薄，
  剩下的是驱动解析，改 ORM 动不到它。
- **优化方向要选“每条 SQL 一次”的地方。** 去 timer、去每行装箱、去无效池都属于这一类；
  而 `clause.Context` 内部的 `strings.Builder` 没有动——改成 `[]byte` 会牵动 40+ 处测试断言，
  相对一次数据库往返属于噪声，属于“宁可不动”的范围。

## 六、方言能力矩阵（MySQL / PostgreSQL / SQLite）

本轮之前 PostgreSQL 的四个文件是注释或空实现，等于「注册了但不能用」。本轮按
「通用接口 + 可选能力接口」重做，并在同一套接口上落地了 SQLite。当前能力对照如下（✔ 已实现并可测，
✘ 明确不支持并返回 `clause.ErrUnsupportedFeature`）：

| 能力 | MySQL | PostgreSQL | SQLite | 实现位置 |
| --- | --- | --- | --- | --- |
| 标识符引用 | 反引号 `` `x` `` | 双引号 `"x"`（内部双写转义） | 双引号 `"x"`（内部双写转义） | `QuoteIdentifier` |
| 占位符 | `?` | `$n`，与参数位置绑定 | `?`，与参数出现顺序绑定 | `GetPlaceholder` / `BuildPlaceholders` |
| LIKE 语义 | `LIKE`（受排序规则影响） | `ILIKE` / `NOT ILIKE` | `LIKE`（默认对 ASCII 大小写不敏感） | `GetLikeOperator` + `where.buildOperatorSQL` |
| 自增主键 | `AUTO_INCREMENT` | `SERIAL` / `BIGSERIAL` | `INTEGER PRIMARY KEY AUTOINCREMENT`（必须内联） | `column/*/BuildCreateSQL` |
| 主键回填 | `LastInsertId()` | `INSERT ... RETURNING` + 反射回写模型 | `LastInsertId()` | `session/insert.go` |
| 冲突忽略 | `INSERT IGNORE` | `ON CONFLICT DO NOTHING`（必须写在 VALUES 之后） | `INSERT OR IGNORE`（内联在 INSERT 之后） | `IgnoreInserter` / `BuildInsertSQL` |
| UPSERT | `ON DUPLICATE KEY UPDATE` | `ON CONFLICT (...) DO UPDATE SET` | `ON CONFLICT (...) DO UPDATE SET ... excluded.` | `Upserter` / `ConflictUpserter` |
| JSON 取值 | `JSON_EXTRACT(c, '$...')` | `(c #>> '{a,b}')` | `json_extract(c, '$.a.b')` | `JSONQuerier` |
| JSON 包含 | `JSON_CONTAINS(c, ?, '$...')` | `(c #> '{a,b}') @> $1::jsonb` | `EXISTS (SELECT 1 FROM json_each(c, '$.tags') WHERE value = ?)` | `JSONQuerier` |
| 全文检索 | `MATCH(...) AGAINST(?)` | `to_tsvector('simple', ...) @@ plainto/websearch_to_tsquery($1)` | ✘ 需 FTS5 虚拟表，单条子句无法表达 | `FullTextSearcher` |
| 分页 | `LIMIT/OFFSET` | `LIMIT/OFFSET` | `LIMIT/OFFSET`（`OFFSET` 只能跟在 `LIMIT` 之后） | `Clause` |
| 建表选项 | `ENGINE` / `CHARSET`；索引必须后置 | 无表选项，索引与注释必须后置 | 无表选项，索引必须后置 | `TableIndexer` / `TableCommenter` |
| 列注释 | 内联 `COMMENT '...'` | `COMMENT ON COLUMN` | ✘ 不支持（`comment(...)` 被忽略） | `TableCommenter` |
| 生成列 | `GENERATED ALWAYS AS (...) VIRTUAL` | `GENERATED ALWAYS AS (...) STORED` | `GENERATED ALWAYS AS (...) VIRTUAL` | `column/*/BuildCreateSQL` |
| `ON UPDATE` 时间戳 | 支持 | **不支持**，需触发器 | **不支持**，需触发器 | — |
| 表存在性查询 | `information_schema.tables.table_schema = DATABASE()` | `table_schema = current_schema()` | `sqlite_master` | `BuildTableQuerySQL` |
| 类型映射要点 | 整型/浮点/时间/`JSON`/`BLOB` | `NUMERIC(20)` 承接 `uint64`、`TIMESTAMPTZ`、`JSONB`、`BYTEA`、`INET`、`INTERVAL` | 五种存储类别：`INTEGER`/`REAL`/`TEXT`/`BLOB`/`NUMERIC`，`time.Time` → `DATETIME` | `datatype/*` |
| 重复键简写 `DUPLICATE` | ✔ | ✘ 返回 `ErrUnsupportedFeature`，指引改用 `UPSERT` | ✘ 同上（冲突目标必须显式） | `Upserter`（仅 MySQL） |
| 跨方言 UPSERT | 退化为 `ON DUPLICATE KEY UPDATE` | 冲突目标取模型主键，更新列单独指定 | 同 PostgreSQL，`SET "c" = excluded."c"` | `Session.UPSERT` / `ConflictUpserter` |

四处设计决策值得记录：

1. **能力接口而不是胖接口**。`REPLACE`、`RETURNING`、JSON、全文检索各自独立成接口，
   PostgreSQL 不必实现 `REPLACE`，MySQL 不必实现 `RETURNING`，SQLite 不必实现 `FullTextSearcher`；
   缺失能力统一返回 `ErrUnsupportedFeature`，而不是「生成一段看似合法其实报错的 SQL」。
2. **三方言的索引都拆成「建表 + 后置语句」**。MySQL 的列定义不接受普通 `INDEX`（列里的 `KEY`
   是主键同义词，建表语句直接报 1064），表级 `INDEX` 又与后置 `CREATE INDEX` 重名；PostgreSQL 与
   SQLite 同样无法内联索引，因此三方言都实现 `dialect.TableIndexer`，由 `Session.CreateTable`
   建表后逐条 `Exec`——不用多语句 `Exec`，避免驱动差异。命名与未命名的索引都只在这一处创建，
   不重复。
3. **`$n` 与参数位置绑定**，所以 PostgreSQL 的 VALUES 缓存键必须含 `argStartIndex`；
   MySQL / SQLite 的 `?` 只与出现顺序绑定，缓存键按「行数 × 列数」即可。
4. **SQLite 的自增主键必须内联**。`AUTOINCREMENT` 只在 `INTEGER PRIMARY KEY` 上是 rowid 别名，
   因此列层新增 `IsInlinePrimaryKey()` 参与 DDL，方言层用 `findInlinePrimaryKeyColumn()` 跳过后置的
   `PRIMARY KEY` 约束；复合主键里出现自增列直接报 `ErrSQLiteCompositeAutoPrimaryKey`。

## 七、易用性

**做得好的地方**

- **声明式建模**：结构体标签同时表达列名、类型、约束、注释，建表与读写共用一份定义；
  `wxorm:` 为主，兼容更早的 `orm:`，值为 `-` 表示忽略字段，迁移成本低。
- **阶段子句 + 自动清理**：`WHERE/ORDER/LIMIT/COLUMNS` 挂在会话上，一次 CRUD 调用后清空，
  不需要手工 `Reset`；重复使用同一会话不会串语句。
- **结果自带上下文**：`result.Result` 同时给出 SQL、参数、耗时与影响行数，排障时不必再开日志。
- **常见语义已补齐**：`First`（未命中返回 `ErrRecordNotFound`）、`Exist`（`SELECT 1 ... LIMIT 1`）、
  `COLUMNS`（只查部分列）把「自己筛空 / 数全表 / 全字段查询」这三类冗余代码收进框架。
- **跨方言同构**：`where.RAW` 写 `?` 会在 PG 下自动改写成 `$n`；`where.LIKE` 在 PG 下自动用 `ILIKE`，
  与 MySQL 默认大小写不敏感的排序规则对齐。

**仍有摩擦的地方**

| 问题 | 说明 | 建议 |
| --- | --- | --- |
| `INSERT(ignore bool, ...)` | 布尔参数在调用点不可读（`INSERT(true, ...)` 要回看签名） | 增加 `InsertIgnore(...)` 语义别名 |
| `DUPLICATE` 已标记为 MySQL 专用 | PostgreSQL / SQLite 下返回 `ErrUnsupportedFeature` 并提示改用 `UPSERT`；上下文代码迁移时会在第一次执行就报错，而不是报警「唯一约束不匹配」 | 跨方言代码统一用 `Session.UPSERT(update...)` |
| 列名是字符串 | 拼错列名只能等数据库报错 | 可引入列名常量生成或 `COLUMNS` 的编译期校验（成本较高） |
| 会话非并发安全 | 无编译期提示，误共享会数据竞争 | 文档已声明；可加 debug 模式下的使用态断言 |
| 调试输出走 `log.Printf` | 无法接入项目日志系统，也无法做慢 SQL 阈值 | 增加 `SetLogger` 接口（`Printf` 形态即可） |
| 模型标签容错 | 历史上 `column('name')`、`generated(...)` 都有坑 | 本轮已修：引号裁剪 + 表达式不再 `strings.ToUpper` |

## 八、拓展性

**扩展点分层清晰**

| 想扩展什么 | 需要改哪里 | 是否需要动 `session` / `statement` |
| --- | --- | --- |
| 新方言的语法差异 | 实现 `clause.Clause`（18 个方法） | 不需要 |
| 新方言的独有能力 | 追加实现对应能力接口（7 个可选接口） | 不需要（子句层用类型断言） |
| 新方言的 DDL 差异 | 实现 `dialect.Dialect` + 可选 `TableIndexer` / `TableCommenter` | 不需要 |
| 新子句（如 `FOR UPDATE`） | `statement` 增加 `Name` + 构造器，`session` 增加链式方法 | 需要 |
| 新条件 / 新搜索语义 | `statement/where` 增加 `Condition` 实现 | 不需要 |

代价也要说清楚：

1. **`Dialect` 仍是胖接口**（建表、存在性、列解析、类型转换、索引、注释合计 30+ 方法），
   新方言首版工作量不小。SQLite 落地时并没有抽 `BaseClause`，而是复用了 MySQL 版已有的
   `splitIdentifierModifiers`、`_SQL_*` 常量与 `quoteJSONPath` 工具函数；`BaseClause` 抽取仍在
   待办（见 [ISSUES.md](../ISSUES.md) #63）。
2. **新增一项「能力」要动 4–5 处**：`clause` 接口 → 各方言实现 → `statement` 子句 → `session` 接线 →
   `where` 暴露。漏改不会有编译错误，只有运行时断言失败，因此每个能力都应配一条
   「能力缺失必须报 `ErrUnsupportedFeature`」的测试（本轮已为 `REPLACE` / `RETURNING` / JSON / 全文检索补齐）。
3. **缺少中间件式扩展**：只有模型钩子（`CallBeforeInsert` 等），没有全局的「执行前/后」回调，
   想在框架层做租户隔离、审计、慢 SQL 采样都需要改代码。

## 九、功能完备度：MySQL / PostgreSQL / SQLite 主要场景

| 场景 | MySQL | PostgreSQL | SQLite | 结论 |
| --- | --- | --- | --- | --- |
| 单条 / 批量 CRUD | ✔ | ✔ | ✔ | 可用；批量走 `VALUES` 分批（100/500/1000 三档） |
| 事务与 panic 回滚 | ✔ | ✔ | ✔ | `Transact` 把 panic 转 error 并回滚 |
| 插入去重（幂等写入） | ✔ `INSERT IGNORE` | ✔ `ON CONFLICT DO NOTHING` | ✔ `INSERT OR IGNORE` | 语义对齐 |
| UPSERT（按主键覆盖指定列） | ✔ | ✔ | ✔ | 推荐 `Session.UPSERT(...)` |
| 主键回填 | ✔ 单条 `LastInsertId` | ✔ 单条 `RETURNING` 回写模型 | ✔ 批量 `RETURNING` 回写；单条 `LastInsertId` | 批量 MySQL 不回填；单条 MySQL / SQLite 需取 `Result` |
| 分页 | ✔ | ✔ | ✔ | `OFFSET` 强制要求 `LIMIT`（三方言统一约束） |
| JSON 查询 | ✔ | ✔ | ✔（JSON1） | 路径统一 `$.a.b`，方言内部转换；SQLite 用 `json_each` 表达数组包含 |
| 全文检索 | ✔ | ✔ | ✘ | SQLite 需要 FTS5 虚拟表，调用 `FULLTEXT` 返回 `ErrUnsupportedFeature` |
| DDL（建表 / 索引 / 注释） | ✔ | ✔ | 部分 | SQLite 无列/表注释；三方言的索引统一由 `TableIndexer` 后置创建 |
| 表存在性检查 | ✔ | ✔ | ✔ | 两种 `information_schema` + `sqlite_master` |
| 原生 SQL 逃生舱 | ✔ | ✔ | ✔ | `Exec/Query/QueryRow`（含 `*Context` 变体） |
| 关联查询（N:1 `JOIN`） | ✔ | ✔ | ✔ | DSL 支持 `JOIN` / `INNER_JOIN` / `LEFT_JOIN`；SQLite 的 RIGHT/FULL 需 3.39+ |
| 1:N 集合映射 / `Preload` | ✔ | ✔ | ✔ | `PRELOAD` 两段式：父查询 + `IN` 批量子查询 + 内存合并，只加载一层；见 [preload-design.md](preload-design.md) |
| 软删除 | ✘ | ✘ | ✘ | 需要业务自建 `deleted_at` + 手动条件 |
| 流式读取（大结果集） | 部分 | 部分 | 部分 | `SELECT` 一次性扫描到切片；大结果集请用 `Query` 拿 `*sql.Rows` |
| 迁移（类型 / 索引 / 注释变更） | ✘ | ✘ | ✘ | `Migrate` 只做增删列 |

PostgreSQL 侧还有三处必须知道的边界：

- **单条与批量的主键回填能力不同**：单条插入只有 PostgreSQL 走 `RETURNING` 直接回填模型；
  批量插入 PostgreSQL / SQLite 走 `INSERT ... RETURNING` 逐行回填，MySQL 无法逐行取回，模型主键仍为零值。
  需要主键时取 `Result.LastInsertId()`，或自行 `RETURNING *` 走 `Query`。
- **没有 `ON UPDATE CURRENT_TIMESTAMP`**。`created` / `updated` 列在 INSERT 时由
  `DEFAULT CURRENT_TIMESTAMP` 填值，但 UPDATE 时不会自动刷新，需要触发器或业务赋值。
- **全文检索未建 GIN 索引**。`to_tsvector('simple', ...)` 每行现算，大表上是全表扫描；
  生产环境应额外 `CREATE INDEX ... USING GIN (to_tsvector(...))`。

SQLite 侧则有五处边界：

- **外键默认不校验**。`PRAGMA foreign_keys` 是连接级开关且默认关闭，需在 DSN 里打开
  （`modernc.org/sqlite` 用 `_pragma=foreign_keys(1)`，`mattn/go-sqlite3` 用 `_foreign_keys=on`）。
- **自增主键必须内联为单列 `INTEGER PRIMARY KEY`**。复合主键 + `auto` 报
  `ErrSQLiteCompositeAutoPrimaryKey`，`auto` 未标 `pk` 报 `ErrAutoIncrementRequiresPrimaryKey`。
- **没有 `ON UPDATE CURRENT_TIMESTAMP`**，`updated` 列只在 INSERT 时由 `DEFAULT CURRENT_TIMESTAMP`
  填值，UPDATE 不刷新；`column.SQLiteColumn.IsAutoUpdate()` 恒返回 false，不会生成非法 DDL。
- **没有注释与全文检索**，`comment(...)` 被忽略，`FullTextSearcher` 未实现。
- **`Migrate` 给已有表补带 `CURRENT_TIMESTAMP` 默认值的列会失败**（SQLite 的
  `ALTER TABLE ADD COLUMN` 只接受常量默认值），`created` 字段应在建表时就位。

## 十、企业级适用性

**已达标的**

- 连接池完整可调：`SetMaxOpenConnections` / `SetMaxIdleConnections` / `SetConnMaxLifetime` /
  `SetConnMaxIdleTime`，并通过 `GetStatus()` 暴露 `sql.DBStats`，可直接接监控。
- 事务语义正确：事务内不复用 `*sql.DB` 的预编译语句（这是上一轮的 P0），恐慌可回滚。
- 上下文贯通：`SetContext` + `SetTimeout`（`0` 关闭），`*Context` 变体覆盖原生 SQL。
- 错误可判别：`ErrRecordNotFound`、`ErrTableNoPK`、`ErrMissingWhere`、`ErrUnsupportedFeature` 等
  都可用 `errors.Is` 分支处理。
- 方言隔离：schema 缓存按 `{driver, reflect.Type}` 分桶，同一进程可同时连 MySQL、PostgreSQL 与 SQLite。
- 零第三方依赖：只依赖标准库，供应链与升级成本可控。

**企业级仍有缺口**

| 缺口 | 影响 | 优先级 |
| --- | --- | --- |
| 日志不可注入 | 无法对接 zap/logrus/slog，也无慢 SQL 阈值与采样 | 高 |
| `Migrate` 不做类型/索引/注释 diff | 生产库结构演进仍需外部迁移工具 | 高 |
| 无软删除与审计字段约定 | 每个业务自己实现，易漏条件 | 中 |
| 主键回填不完整 | 单条 MySQL / SQLite 与批量 MySQL 均不回填模型，需取 `Result` | 中 |
| 会话非并发安全 | 每个 goroutine 必须独立 `NewSession` | 中（文档已声明） |
| 无分库分表 / 读写分离 | 超出 ORM 边界，需上层中间件 | 低 |
| 无 SQL 审计与拦截器 | 无法统一加租户条件、脱敏 | 中 |

结论：**中小规模业务库可以直接用于生产**（CRUD、事务、建表、幂等写入、分页、JSON 都齐）；
**大型系统需要在日志、迁移、关联查询三处补齐**，否则会被迫在业务层重复造轮子。

## 十一、与 GORM / xorm 的对比与优化空间

### 11.1 功能对比

| 维度 | wxorm | GORM v2 | xorm |
| --- | --- | --- | --- |
| 依赖 | 标准库 | 多（driver 生态、schema 解析） | 少 |
| 方言 | MySQL、PostgreSQL、SQLite | MySQL/PG/SQLite/SQL Server/ClickHouse 等 | MySQL/PG/SQLite/MSSQL/Oracle |
| 关联查询 / `Preload` | ✔（N:1 `JOIN` + 1:N `PRELOAD`） | ✔（HasOne/HasMany/Many2Many + 预加载） | ✔（`Join` / `Relation`） |
| 钩子 | ✔（模型方法） | ✔（Before/After Create/Update/...） | ✔（Before/After + 事件） |
| 软删除 | ✘ | ✔（`gorm.DeletedAt`） | ✔（`deleted` 标记） |
| 自动迁移 | 增删列 | 增删列 + 索引 + 约束 | 较完整（含索引/唯一） |
| 批量插入回填主键 | ✘ | 部分（按方言） | 部分 |
| 原生 SQL 逃生舱 | ✔ | ✔ | ✔ |
| 日志 / 可观测 | 硬编码 `log` | ✔（可插拔 logger） | ✔（logger 接口） |
| 缓存 | 仅语句/schema 缓存 | 无内置二级缓存 | 内置二级缓存（`cacher`） |
| 类型安全 | 字符串列名 | 字符串为主，支持泛型 API | 字符串为主 |
| 零依赖可审计 | ✔（~6300 行） | ✘ | 中等 |

### 11.2 性能对比（本机基准，仅看 ORM 自身开销）

| 指标 | wxorm 当前 | 说明 |
| --- | --- | --- |
| `Schema_Parse`（命中缓存） | 46.0 ns / 0 alloc | `sync.Map` 命中，未重复反射 |
| `GetReflectFieldValue` | 22.5 ns / 24 B / 1 alloc | 零值判断走 `IsZero`，无深度比较 |
| `Statement_INSERT` | 347 ns / 579 B / 9 allocs | 子句折叠 + 占位符配对 |
| `BuildInsertSQL` | 138 ns / 162 B / 3 allocs | 字符串装配，缓存命中后更低 |
| `Session_InsertOne`（内存驱动） | 3.3 µs / 38 allocs | 大头是驱动解析 SQL |

GORM 与 xorm 的同等基准**本轮未实测**（两个库都带自己的驱动与回调链，直接横向比容易把驱动开销算到
ORM 头上）。从结构上看，wxorm 少了回调链与动态 `clause` 拼装，单条插入的 ORM 固定开销应该在
亚微秒量级，而业务感知到的差异主要来自功能（预加载、软删除、自动迁移）而不是 ns。
结论：wxorm 现在的固定开销已经很低，**继续在同一条路径上抠 ns 收益有限**，优化空间集中在下面几处：

### 11.3 真正值得做的优化（按性价比排序）

> 第 1、2、4 项已在 v0.11.0 落地并实测，详见 12.7；下面保留原始分析作为背景。

1. **批量插入的 SQL 复用**：`insertBatches` 每次都重建 `Statement`；可以按「列集 + 批大小」缓存
   已构建的 SQL 模板，只替换参数，减少大事务里的字符串装配。
2. **`SELECT` 扫描路径**：当前每行都走 `reflect` 取值 + `ColumnTypes()` 映射；
   可以按 `schema` 预生成「列序号 → 字段索引」的静态数组，避免每行查 `ColumnIndexesByName`。
3. **`clause.Context` 的 `strings.Builder`**：上一轮已评估，改成 `[]byte` 要动 40+ 处断言，
   相对一次数据库往返属于噪声，暂不动。
4. **预编译语句粒度**：目前按「表名 + 列集」缓存；可以把 `INSERT`/`SELECT` 的模板缓存上移到
   `Engine`（跨会话共享），减少多会话重复构建。
5. **零分配 WHERE**：`where` 条件树每次构建，可用对象池复用 `expression` 节点——
   但要注意池化与并发的交互，收益需要基准验证。

功能侧的空间比性能侧大得多：日志注入、`Migrate` diff、软删除与 SQL 审计拦截，
这几项补上之后，与 GORM 的差距主要就只剩「生态与文档」了
（N:1 `JOIN` 已在 v0.13.0 落地，1:N `PRELOAD` 已在 v0.14.0 落地）。

## 十二、结论

- 分层与职责划分是这套代码的核心资产，后续演进应继续在层内补能力，而不是跨层打补丁。
- 上一轮 18 个 P0 缺陷的共同特征是「**静默**」：不报错、不 panic，只是结果不对。
  因此后续任何缓存、拼接、反射相关的改动，都应配一条能区分「命中正确」与「命中错误」的测试。
- 本轮的三个 P0 同样是静默型：跨模型预编译串味、JSON 路径与全文检索模式注入。共同点是
  **缓存键或拼接点漏了一个输入维度**——凡是“把字符串拼进 SQL”的地方，都应该有对应的注入测试。
- 性能优化的顺序应该是：先分层度量，再改「每条 SQL 一次」的固定成本，最后才碰数据循环内部。
  现在 `go test ./...`、`go vet ./...`、`gofmt -l .`、`go test -race` 均为干净输出，
  任何后续改动都能用同一套基准判断是否回归。

### 12.1 第三轮（Go 1.27 现代化 + 方言能力分层）

这一轮不再以「找 bug」为主线，而是由三个问题驱动：**换方言要改多少代码？两个主流数据库场景真的能跑吗？对标 GORM 还差什么？**

**代码层面落地的事**

1. **`Clause` 拆成「通用 18 方法 + 7 个可选能力接口」**。MySQL 与 PostgreSQL 各自只实现自己有的能力，
   缺失能力统一返回 `ErrUnsupportedFeature`。这是本轮最有价值的结构性改动：PostgreSQL 从此不需要为了
   通过编译而写一堆空实现。
2. **PostgreSQL 端到端可用**：类型映射、列 DDL（`SERIAL`/`BIGSERIAL`/`GENERATED ... STORED`）、
   建表、后置索引与注释、`information_schema` 存在性查询、`$n` 占位符、`ON CONFLICT`、`RETURNING` 全部落地。
3. **`Session.UPSERT(update...)`**：把「冲突目标」固定为模型主键；同时把 `DUPLICATE` 标记为 MySQL 专用，
   PostgreSQL 下直接返回 `ErrUnsupportedFeature` 并提示改用 `UPSERT`。宁可破坏少量旧调用，
   也不保留一个在不同方言下语义不同的 API。
4. **单条插入主键回填**：PG 走 `INSERT ... RETURNING` + reflect 回写，`LastInsertId()` 语义与 MySQL 对齐；
   `ON CONFLICT DO NOTHING` 命中时按 0 行成功处理。
5. **Go 1.27 现代化**：`reflect.TypeFor`、`slices.Sorted(maps.Keys(...))`、内置 `min`、`strconv.IntSize`
   取代 `unsafe.Sizeof(uintptr(0))`；类型映射的 `panic` 改为降级（`JSONB`/`LONGTEXT`）。
6. **两个真实缺陷**：`generated(...)` 表达式被 `strings.ToUpper` 改写、MySQL 列 DDL 完全忽略
   `Generated`；顺带修了 `column('name')` / `index('x')` 的引号残留。

**仍然要坚持的判断**

- 新增方言的成本已经收敛到「实现 `Clause` + 按需追加能力接口」，但 `Dialect` 本身仍是胖接口；
  下一个方言（SQLite）之前，应该先抽出 `BaseClause`。
- 性能不是瓶颈：`Schema` 命中 46 ns/0 alloc、单条插入的 ORM 固定开销 3.3 µs（含内存驱动）。
  继续优化的收益远小于补功能（日志注入、关联查询、`Migrate` diff、软删除）。
- 企业级落地的短板不在 SQL 生成，而在**可观测性与迁移**。这两块不补，用户会在业务层自己包一层，
  反而把框架的「零依赖可审计」优势抵消掉。
- 所有新增能力都配了「缺失时必须报错」的测试。这是方言分层的安全网：宁可显式失败，也不要静默降级。

### 12.2 第五轮（SQLite 方言落地）

这一轮验证的是「上面那套分层的可复制性」：在不动 `session` / `statement` 生产代码的前提下，
新增一个方言需要做多少事。结论是只需要在 `dialect` 自包内补齐三块：

1. **`datatype/sqlite.go`**：把 Go 类型收敛到 SQLite 的五个存储类别（`INTEGER`/`REAL`/`TEXT`/`BLOB`/`NUMERIC`）。
   `time.Time` 刻意映射为 `DATETIME` 而不是 `TEXT`——驱动只有在列声明为 DATE/DATETIME/TIMESTAMP 时
   才会把结果扫描回 `time.Time`。
2. **`column/sqlite.go`**：SQLite 的自增主键必须写成 `INTEGER PRIMARY KEY AUTOINCREMENT`，
   因此新增 `IsInlinePrimaryKey()`；`IsAutoUpdate()` 恒 false（没有 `ON UPDATE`）。
3. **`clause/sqlite.go` + `dialect/sqlite.go`**：`SQLiteClause` = `Clause` + `Replacer` +
   `ConflictUpserter` + `JSONQuerier`；建表把自增主键内联并跳过后置 `PRIMARY KEY`，
   索引用 `TableIndexer` 后置 `CREATE INDEX`，存在性查询走 `sqlite_master`。

刻意不实现的三项能力：`Upserter`（`DUPLICATE` 的冲突目标语义在 SQLite 不成立）、
`ReturningInserter`（`LastInsertId` 已足够）、`FullTextSearcher`（需要 FTS5 虚拟表）。
它们都沿用了同一个契约：**不实现就不会被断言命中，调用方得到 `ErrUnsupportedFeature`**。

顺带暴露的一个陈旧假设：`Dialect.GetPrimaryKeyColumns` 假设主键总是用表级约束声明。
SQLite 打破了这一点，所以先在 `column.BaseColumn` 上补了 `IsAutoIncrement()`（不进 `Column` 接口，
避免破坏外部实现），再在方言包内用可选接口断言探测。这类「接口缺一个只读查询」的问题，
比新增能力接口更隐蔽——它不会报错，只会生成重复声明的主键。

### 12.3 第六轮（函数与方法命名对齐 leego 约定）

leego 的 `docs/conventions.md` 把命名写成可检查规则：首词必动词、be 动词或情态动词；
构造函数只能用 `New<类型>` / `new<类型>` / `NewDefault<类型>` / `New<类型>With<特征>`；
`With` / `Without` 只留给返回 `Option` 的函数式选项。本轮把 wxorm 的存量名称逐条对齐，
不保留 Deprecated 别名——旧名留在库里，规则就永远不会被真正执行。

**导出 API**

| 旧名 | 新名 | 依据 |
| --- | --- | --- |
| `session.New` | `session.NewSession` | 构造函数 `New<类型>` |
| `statement.New` | `statement.NewStatement` | 构造函数 `New<类型>` |
| `clause.NewWithContext` | `NewContextFrom` | `NewFrom` 族；`With` 仅限选项函数 |
| `column.FormatHumpToUnderline` | `FormatCamelToSnake` | `Format` 动词 + 词性准确 |
| `Session.First` | `Session.FindFirst` | `Find` = 按条件搜索首个匹配 |
| `Session.WithContext` | `Session.SetContext` | `With` 仅限选项函数；与 `GetContext` 对称 |

**包内助手**：`_debug` → `logDebug`；`unsupportedFeature` → `newUnsupportedFeatureError`；
`toInt64` / `toText` / `toSliceAny` → `convertToInt64` / `convertToText` / `convertToAnySlice`；
`difference` → `diffStrings`；`normalJSONPath` → `normalizeJSONPath`；
`andOr` / `operatorSQL` / `postgresDatatype` / `postgresFullTextQueryFunc` →
`buildAndOr` / `buildOperatorSQL` / `buildPostgresDatatype` / `buildPostgresFullTextQueryFunc`；
`metadataGroups` / `primaryKeyColumns` / `foreignKeyColumns` / `sortedKeys` / `sortedMapKeys` →
`groupMetadataColumns` / `filterPrimaryKeyColumns` / `groupForeignKeyColumns` / `sortKeys` / `sortMapKeys`；
`autoPrimaryKeyColumn` / `inlinePrimaryKeyColumn` / `returningColumns` →
`findAutoPrimaryKeyColumn` / `findInlinePrimaryKeyColumn` / `findReturningColumns`。

**刻意保留的例外**：SQL 关键字与查询 DSL（`INSERT` / `SELECT` / `WHERE` / `EQ` / `RAW` 等）、
`sql.Result` 契约方法（`LastInsertId` / `RowsAffected`）、`error` / `fmt.Stringer` 接口方法与 `init`。
测试函数名（`Test*` / `Benchmark*`）本轮不动，符合 leego「测试辅助函数不套构造函数规则」的立场。

这轮改动只涉及名称，不改行为：`gofmt -l .`、`go build ./...`、`go vet ./...` 均为空输出，
`go test ./... -count=1` 全部通过。

### 12.4 第七轮（真实数据库回归：MySQL 8.4 / PostgreSQL 17 / SQLite 3.54）

前六轮的验证地基是内存驱动 `harness/examples/fakedriver`，它只能证明「wxorm 拼出的 SQL 自己认得」。
这一轮把同一批用例搬到真实容器（MySQL 8.4.13、PostgreSQL 17.12、SQLite 3.54.1）上跑，
目标有三个：验证 SQL 真的能被数据库接受、验证结果映射与事务语义、建立可重复的性能基线。

**零依赖约束不变**：真实驱动不可避免引入第三方库，因此新建独立子模块 `harness/integration/`
（`module gitee.com/leminewx/wxorm/harness/integration` + `replace gitee.com/leminewx/wxorm => ../..`），
根模块 `go.mod` 仍然只有 `module` 与 `go` 两行。用例通过环境变量覆盖连接信息，
连不上时默认 `t.Skip`，`WXORM_IT_STRICT=1` 时改为失败。连接参数与种子数据统一放在 `harness/testdata`。

```bash
cd harness/integration
go run ./cmd/preparedb             # 创建 wxorm_harness 真库，可重复执行
$env:WXORM_IT_STRICT = "1"          # PowerShell；Bash 用 WXORM_IT_STRICT=1
go test ./... -count=1             # 三方言全量
go test ./... -run "^$" -bench . -benchtime=1s
```

**这一轮抓到的真实缺陷**（均为内存驱动无法暴露的问题）

| # | 位置 | 现象 | 修复 |
| --- | --- | --- | --- |
| 1 | `column/mysql.go` | 列定义生成 `` `age` INT INDEX ``，MySQL 报 1064（列里的 `KEY` 是主键同义词） | 删除内联索引；MySQL 实现 `TableIndexer`，建表后 `CREATE INDEX` |
| 2 | `dialect/mysql.go` | 命名复合索引同时出现在建表语句与后置语句中，重名冲突 | 删除 `buildUnionIndexes`，命名/未命名索引统一由 `BuildTableIndexSQLs` 创建 |
| 3 | 三方言 `IsAutoInsert` / 列默认值 | `updated` 列不被视为数据库生成，插入零值时间，MySQL 报 1292 `'0000-00-00'` | `IsAutoInsert` 纳入 `Updated`；DDL 中 `created` / `updated` 均写 `DEFAULT CURRENT_TIMESTAMP` |
| 4 | `session/record.go` `selectMore` | `COLUMNS("not_exist")` 把未知列发给数据库，拿到方言的 1054 | 执行前用 `ColumnIndexesByName` 本地校验，返回 `ErrSelectColumnNotFound` |
| 5 | `session/record.go` `insertOne` | PostgreSQL 走 `RETURNING` 分支时提前 return，`AfterInsert` 钩子不触发 | RETURNING 成功后补 `CallMethod(_HOOK_AFTER_INSERT, value)` |
| 6 | `wxorm.go` `Migrate` | 第二次迁移报 `cached plan must not change result type`：pgx 缓存了 `SELECT *` 的执行计划 | 新增 `Dialect.BuildTableColumnsQuerySQL`（`information_schema.columns` / `pragma_table_info`）+ `Session.GetTableColumns` |

对应的测试基建也做了两处调整：`fakedriver` 新增 `information_schema.columns` 分支，保证根模块离线
测试仍能覆盖 `Migrate` 的新查询路径；能力矩阵测试中 MySQL 的 `TableIndexer` 从 `false` 改为 `true`。

**覆盖范围**（`harness/integration` 包，每条用例在三方言下各跑一遍）

| 用例 | 覆盖点 |
| --- | --- |
| `TestDDL` | 建表、`ExistsTable`、`GetSchema` 列名、删表 |
| `TestInsertSelect` | 单条/批量插入、`LastInsertId` 与 PG 主键回填、全量查询、`FindFirst`/`Exist`/`COUNT`/`LIKE`、`LIMIT`/`OFFSET`、`COLUMNS` 子集与未知列、`OFFSET` 无 `LIMIT` |
| `TestUpdateDelete` | 结构化更新、map 更新、缺 `WHERE` 报错、结构化/条件/无条件删除 |
| `TestWhereConditions` / `TestGroupHaving` | 17 种条件构造、`GROUP BY` + `HAVING` + `ORDER` |
| `TestOrderLimit` | 多列排序（`age DESC, name ASC`）、分页、排序字段不在返回列中 |
| `TestTransaction` | `Transact` 提交/回滚/panic、显式 `Begin`/`Rollback` |
| `TestIgnore` / `TestUpsert` / `TestDuplicate` | 冲突忽略、跨方言 UPSERT、`DUPLICATE` 仅 MySQL 可用 |
| `TestUniqueViolation` | 唯一约束真实生效，失败插入不污染数据 |
| `TestHooks` | 8 个钩子：单条/批量插入、逐行 `AfterSelect`、更新/删除、未命中不触发 |
| `TestTypes` | `int8`–`uint64`、`float32/64`、`bool`、中文/emoji、`[]byte`、`time.Time`、`*time.Time` |
| `TestMigrate` | 建表、加列带默认值、删列、列名核对、连续两次迁移幂等 |
| `TestBatchInsert` | 1 / 2 / 99 / 150 / 1200 行的分批边界 |
| `TestConcurrentSessions` / `TestSessionCloseRollback` | 8 worker × 25 次独立会话；未提交会话关闭即回滚 |

**性能基线**（本机 i5-14600KF，容器本机回环，`-benchtime=1s`，数据仅供横向比较）

| 基准 | MySQL | PostgreSQL | SQLite |
| --- | --- | --- | --- |
| `InsertBatch/rows=1` | 2.52 ms/op | 1.16 ms/op | 0.263 ms/op |
| `InsertBatch/rows=20` | 5.90 ms/op | 1.54 ms/op | 0.543 ms/op |
| `InsertBatch/rows=200` | 19.8 ms/op | 6.54 ms/op | 3.91 ms/op |
| `SelectPage`（100 行） | 1.11 ms/op | 0.639 ms/op | 0.222 ms/op |

折算单行插入成本：MySQL 约 0.10 ms、PostgreSQL 约 0.033 ms、SQLite 约 0.020 ms（200 行批量）。
单条插入的 1–2.5 ms 主要是自动提交的持久化开销，与 ORM 无关；批量越大，分摊越明显。
因此「批量写」应优先用 `INSERT(false, rows...)`，而不是循环单条插入。

**用真实库才能确认的边界**

- **MySQL 严格模式不接受零值 `time.Time`**（`0000-00-00` 非法）。可空时间用 `*time.Time`，
  非空时间列由 `created` / `updated` 默认值兜底。
- **MySQL 容器时区为 UTC 时，DSN 必须写 `loc=UTC`**，否则扫描出的时间整体偏移 8 小时；
  这不是 wxorm 的缺陷，而是「驱动时区」与「会话时区」必须一致。
- **SQLite 的 `INTEGER` 是有符号 8 字节**，`uint64` 超过 `MaxInt64` 会溢出；
  跨方言模型应避免依赖无符号 64 位上界。
- **索引变更不在 `Migrate` 范围内**：给既有列新增 `index` 标记不会自动建索引，仍需手动 DDL。

### 12.5 第八轮（测试资产归入 harness）

参照 leego 的目录约定，把分散在仓库根部的测试资产收拢到 `harness/`，并把连接参数与种子数据从代码里抽出来：

| 迁移前 | 迁移后 |
| --- | --- |
| `examples/` | `harness/examples/`（测试模型 + `fakedriver`） |
| `integration/` | `harness/integration/`（独立子模块，`replace => ../..`） |
| 代码内的 DSN 与种子行 | `harness/testdata/database.json`、`accounts.json` |

新增三个基础设施包：`harness/assetpath` 按源码位置与工作目录逐级定位 `harness/testdata`，
兼容 `go test` 的包目录工作目录；`harness/database` 解析连接配置并构造 MySQL / PostgreSQL DSN；
`harness/integration/cmd/preparedb` 创建 `wxorm_harness` 三个真库，可重复执行。

验证：在 MySQL 8.4.13 / PostgreSQL 17.12 / SQLite 3.54.1 真库上重新跑全量用例与基准，
根模块 `go test ./... -count=1 -p 2` 全绿，集成 `WXORM_IT_STRICT=1 go test ./...` 3.85s 通过，
`-race` 5.64s 通过；基准与 12.4 节基线基本一致：Insert 1 行 2.54/1.16/0.262 ms、SelectPage 100 行 1.09/0.647/0.220 ms；
PostgreSQL 200 行单次采样 7.44 ms（基线 6.54 ms），执行路径未变，差异来自容器调度。说明目录重组没有引入运行时开销。

### 12.6 第十轮（P0 收口：上下文、可观测性、会话池、批量回填、迁移差异）

本节反映 v0.10.0 的状态；前文的缺陷台账与「已知边界」若与此冲突，以本节为准。

| 主题 | 实现 | 关键约束 |
| --- | --- | --- |
| 上下文贯通 | `Exec` / `Query` / `QueryRow` 统一走 `execContext` / `queryContext` | `Query` 不能 `defer cancel()`，取消函数由结果集关闭后兜底；`SetTimeout(0)` 关闭超时 |
| 可观测性 | `Logger` + `QueryEvent` + `SetLogger` / `SetSlowThreshold` | 调试模式记录全部语句，非调试模式只上报慢 SQL；日志器需并发安全 |
| 会话池 | `sync.Pool` + `SetID` / `SetReleaser` / `Reset` | `Close` 先回滚事务再归零归还；重复 `Close` 无副作用；`Transact` 保证提交/回滚后归还 |
| 批量回填主键 | `clause.BatchReturningInserter` + `insertReturningMany` | PG / SQLite 按行回填；MySQL 与冲突忽略/冲突更新路径不回填；SQLite 单条仍走 `LastInsertId` |
| 迁移差异 | `Engine.DiffTable` + `TableDiff` / `ColumnDiff` | 类型差异只写日志不执行；缺失索引自动补建；方言无数据字典能力时跳过比对 |

验证：根模块 `gofmt -l .` 无输出、`go vet ./...`、`go test ./... -count=1`、`go test -race ./... -count=1` 全绿；
MySQL 8.4 / PostgreSQL 17 / SQLite 三方言真库全量用例（`WXORM_IT_STRICT=1`）与 `-race` 均通过，
新增的批量回填与差异报告用例在三方言上全部验收：PG / SQLite 批量插入逐行回填主键，MySQL 按契约不回填；
`DiffTable` 先报告新增列与缺失索引，`Migrate` 补建索引后差异为空。
性能方面 PostgreSQL / SQLite 与 12.4 基线持平；MySQL 在本环境单次查询被驱动层延迟 ACK 放大到约 44ms，
原始 `database/sql` 同 SQL 可复现（新连接首条约 4ms、复用连接 43~45ms），与 wxorm 代码无关，
该方言基准需在无此网络现象的机器上复测。第 12.4 节「索引变更不在 `Migrate` 范围内」的结论已在 v0.10.0 失效，
索引缺失现在会被自动补建。

### 12.7 第十一轮（P0 性能：语句缓存、结果集预分配、自适应分批）

本节反映 v0.11.0 的状态，对应 11.3 的第 1、2、4 项；第 3 项维持「暂不动」，第 5 项仍为待办。

| 主题 | 实现 | 关键约束 |
| --- | --- | --- |
| 查询语句缓存 | `StatementCache`（FIFO 淘汰）+ `Engine.SetStatementCacheSize` / `Session.SetStatementCache`；SELECT / COUNT / Exist / UPDATE / DELETE / 多行 INSERT 复用 | 事务内绕过；DDL 与 `Engine.Close` 清空；默认 256 条，防动态 SQL 无界堆积 |
| 结果集预分配 | `Session.limit` 记录 `LIMIT`，`reserveSelect` 按容量预分配并保留已有元素 | 上限 1024，无 `LIMIT` 起步 16；只扩容量不改长度 |
| 直接扫描 | `[]Struct` 先 `Append(Zero)` 再扫描到新元素，`[]*Struct` 仍逐行分配 | 钩子拿到的是切片元素指针，不再复制临时结构体 |
| 自适应分批 | `insertBatchSize` 按方言占位符上限与列数计算，单批上限 1000 | MySQL / PG 60000 个占位符，SQLite 30000；宽表自动收缩 |

实测（真库三方言，同机对比）：

- 分页 100 行：MySQL 43.9ms → 0.44ms（约 99×，消除逐次 prepare 的延迟 ACK）；
  PG 0.437ms → 0.40ms；SQLite 0.226ms → 0.194ms，分配约 108KB → 63.8KB（预分配 + 直接扫描）。
- 单行 `FindFirst`（缓存关 → 开）：MySQL 44.26ms → 2.60ms；PG 273.7µs → 208.9µs；SQLite 30.7µs → 17.3µs。
- 结构体 `UPDATE`（缓存关 → 开）：MySQL 44.31ms → 2.51ms；PG 787µs → 769µs；SQLite 250.1µs → 249.0µs。
- 批量写入（旧事务分批 → 单语句）：200 行 MySQL 93.65ms → 8.83ms、PG 4.22ms → 3.35ms、SQLite 0.926ms → 0.749ms；
  1000 行 MySQL 190.98ms → 71.93ms、PG 100.51ms → 61.65ms、SQLite 3.295ms → 2.563ms。

验证：根模块 `gofmt` / `vet` / `test` / `test -race` 全绿；真库 `WXORM_IT_STRICT=1 go test ./... -count=1`
（5.38s）与 `-race`（7.72s）三方言均通过；新增 `TestPreparedStatementCache`、`TestBatchInsertLarge` 覆盖
真库缓存路径与 1200 行分批。内存假驱动的 `Prepare` 不做真编译，根模块 `*Cached` 基准只用于回归监控，
不作为收益证据。

### 12.8 第十二轮（收尾：UPDATE 零值策略、主键顺序、单行直扫、访问器预编译）

本节是 v0.11.0 的收尾，补 12.7 之后暴露的语义偏差与固定开销；11.3 第 3 项维持「暂不动」，第 5 项仍为待办。

| 主题 | 实现 | 关键约束 |
| --- | --- | --- |
| UPDATE 零值语义 | `updateByStruct` 默认跳过零值字段；`OVERWRITE()` 整行覆盖（零值写回）；`UPDATE_COLUMNS(...)` 只写白名单列；全零结构体返回 `ErrUpdateStructEmpty` | 与 README、函数注释的「只更新非零字段」对齐；置零也可走 `UPDATE(map)`；不再返回 `nil, nil` 让调用方空指针 |
| 主键条件顺序 | `ExtractNonZeroPrimaryKeyValues` 按列顺序返回 `[]schema.FieldValue` | 取代 map 遍历；SQL 文本稳定才能稳定命中 `StatementCache` |
| 单行直扫 | 抽出 `buildSelect`，`selectFirst` 直接 `queryPrepared` + 单行 Scan | 先 `Set(reflect.Zero)` 保证未查询列归零；`LIMIT(1)` 覆盖调用方 LIMIT；`own.limit` 用后归零 |
| 字段访问器预编译 | 解析期预计算 `PrimaryKeyFields` / `UpdateFields`（`FieldIndex` 记列名 + 字段索引） | 旧 `Extract*` 全部委托新实现，行为不变；`deleteByStruct` 同步改用新访问器 |
| 基准注释卫生 | `prepareCachedBenchGroups` 上注明假驱动不做真编译 | `*Cached` 基准只作回归监控，不作为收益证据 |

实测与验证：

- 复合主键提取（`BenchmarkSchema_ExtractPrimaryKey`）：96.9ns / 400B / 3 allocs → 22.1ns / 64B / 1 alloc。
- `FindFirst` 直扫（真库，同机同数据）：SQLite 10.83µs → 10.47µs（少 4 allocs / 90B），
  PG 183.1µs → 180.4µs，MySQL 197.5µs ↔ 196.4µs 在噪声内。
- `copyArgs` 懒拷贝（结果对象不再复制，改由构建器重新 `append`）：15.9ns / 64B / 1 alloc → 44ns / 112B / 3 allocs。
  原因是 `clause.Context.Reset` 用 `args[:0]` 复用底层数组，结果对象交出所有权后构建器必须重新扩容；
  结论是不采纳，记录为 ISSUES #89。
- 根模块 `gofmt -l .` 无输出、`go vet ./...` 干净、`go test ./... -count=1` 与 `-race` 全绿；
  真库 `WXORM_IT_STRICT=1 go test ./... -count=1`（5.60s）与 `-race`（8.19s）三方言通过。
- 新增 `TestSession_First_DirectScan`、`TestSchema_ExtractNonZeroPrimaryKeyValues`、
  `TestSchema_ExtractUpdateNamesAndValues`；`TestUpdateDelete` 增加「部分结构体不清空未赋值列」与
  `OVERWRITE` 置零断言，`TestGroupUpdate_Struct_AutoWhere` 改为断言 `ErrUpdateStructEmpty`。

本轮最值得记的不是某个微基准，而是 #85：文档与注释都说「只更新非零字段」，实现却写回全部可更新列。
这类「文档承诺与实现偏离」的缺陷不会报错，只会在部分结构体更新时静默清空列，因此修复时优先对齐契约、
再补显式开关，而不是反向修改文档。

### 12.9 第十五轮（审计修复与大数支持）

本节反映 v0.15.0 的状态：按审计报告收敛「静默失真 / 静默丢数据」的默认值，同时让 `math/big` 在根模块
零依赖下可用。

| 主题 | 实现 | 关键约束 |
| --- | --- | --- |
| 大数支持 | `schema.BigKind`（`BIG_NONE` / `BIG_INT` / `BIG_FLOAT`）+ `Schema.BigFieldKinds` / `HasBigFields`；写入在 `GetReflectFieldValue` 统一格式化为文本（`big.Int.String()`、`big.Float.Text('f',30)`），读取由 `session.bigScanner` 先扫 `sql.NullString` 再 `SetString` / `ParseFloat(...,256,ToNearestEven)` | 只有含 big 字段的模型才建 scanner，普通模型零额外分支；`big.Rat` 解析阶段返回 `ErrUnsupportedBigRat` |
| 大数类型映射 | MySQL `big.Int → DECIMAL(65,0)`、`big.Float → DECIMAL(65,30)`；PostgreSQL `NUMERIC`；SQLite `TEXT` | MySQL 原先落到 `reflect.Struct` 分支生成 JSON 列；SQLite 的 `NUMERIC` 亲和性会把超大数转成 REAL |
| UPDATE 策略 | `IncludeZero()` 改名 `OVERWRITE()`；新增 `UPDATE_COLUMNS(...)` 白名单 | 白名单列零值也写回；`session.ErrUpdateColumnsEmpty`、`schema.ErrUnknownUpdateColumn` |
| 迁移安全 | `Migrate(value, ...MigrateOption)` 默认只报告 `DroppedColumns`，`WithDropMissingColumns()` 才执行 `DROP COLUMN` | 列名与类型改为折叠比较，避免仅改大小写被当成「删一列 + 加一列」 |
| 语句缓存并发 | FIFO 改 LRU + 引用计数（`Stmt.acquire` / `Release` / `retire`） | 淘汰只 retire，最后一个使用者归还后才 `Close`；导出 `Session.ClearStatementCache()` |
| 上下文生命周期 | `Query` / `QueryRow` 不再叠加会话超时 timer | 结果集消费完之前无法 `cancel`，叠加即泄漏；超时交给 `QueryContext` / `QueryRowContext` |
| 事务守卫 | `Begin` 委托 `BeginTx(ctx, opts)`；重复 `Begin` 报错；新增 `Engine.TransactContext` | 未开启就提交/回滚返回导出的 `ErrTransactionNotStartedYet` |
| 集合语义 | `IN()` 空集合生成 `1 = 0`，`NOTIN()` 生成 `1 = 1` | `COUNT` 遇 `GROUP` / `HAVING` 返回 `ErrCountWithGroupUnsupported`，不再静默丢分组 |
| 日志脱敏 | 慢 SQL 默认只输出参数个数（`QueryEvent.LogArgs`） | `SetLogParamValues(true)` 显式开启；调试模式始终输出 |
| 批量回填 | 多行 `RETURNING` 行数与写入行数不一致时报错 | 返回 `ErrBatchReturningMismatch`，不按错位下标回填主键 |

验证：根模块 `gofmt -l .` 无输出、`go vet ./...` 干净、`go test -race ./...` 全绿；真库
`WXORM_IT_STRICT=1 WXORM_IT_DB=mysql,postgres,sqlite go test -race ./...` 三方言通过（约 10.4s）。
新增 `schema/big_test.go`（`GetBigKind` / `EnsureBigSupported` / `FormatBigValue` / `ScanBigValue` / `Parse` 元数据）
与 `harness/integration/audit_fix_test.go`（`TestBigNumber`、`TestBigRatRejected`、`TestUpdateColumns`、
`TestCountRejectsGroup`、`TestMigrateKeepsDroppedColumns`）。

两处取舍值得记录：其一，`big.Float` 的文本形式固定 30 位小数，是为了与 MySQL `DECIMAL(65,30)` 对齐，
跨方言读写同一份文本；如果允许「按值长度自适应」，同一个数在 MySQL 与 SQLite 上会得到不同字符串，
回读比较立刻变得不可靠。其二，`big.Rat` 直接拒绝：有理数写成 `NUMERIC` 必然有损、写成文本又失去数值语义，
「建表成功、首次写入静默失真」比解析期报错危险得多。

## 十三、第十六轮：连接自锁与 DDL 超时修复（v0.16.0）

第二轮系统性审计（功能 / 并发 / 安全 / 稳定 / 性能）的落地结果。所有结论都在 `harness/integration`
三方言真库上复现或验证，内存假驱动无法暴露这些问题。

| 主题 | 实现 | 关键约束 |
| --- | --- | --- |
| 查询连接自锁 | 扫描完成后立即 `rows.Close()`，再执行 `AFTER_SELECT` 钩子与 `PRELOAD` 子查询；多行查询的逐行钩子延迟到循环之后统一执行 | 修复前 `SetMaxOpenConnections(1)` + `FindFirst` + `PRELOAD` 稳定 15s 后报 `context deadline exceeded`；默认池下一条查询固定占两条连接 |
| DDL 超时 | 新增 `Session.ExecDDL`：不叠加会话查询超时、仍继承会话 ctx；`CreateTable` / `DeleteTable` / `Migrate` 全部改用 | 修复前 1ns 超时下建表直接失败；MySQL 的 DDL 不能回滚，中断会留下半成品表结构 |
| 错误路径结果对象 | 执行失败返回 nil，结果对象自身在无底层结果时返回 `ErrNoExecutionResult` | 修复前 `res.RowsAffected()` 触发空指针 panic |
| LIMIT 语义 | `LIMIT(0)` 生成 `LIMIT 0`；负数返回 `ErrLimitNegative` | 修复前 `LIMIT(0)` 返回全表、`LIMIT 0 OFFSET 1` 生成非法 SQL |
| RAW 占位符 | 词法扫描：只有引号 / 注释 / 美元引用之外的 `?` 改写为 `$n` | jsonb 的 `?` 运算符与占位符同形，个数不一致时仍按原样输出 |
| schema 缓存 | 缓存键从（方言, 类型）扩展为（方言, 类型, 逻辑表名） | 修复前 `Partition=2024/2025` 两个分片都会解析到 2024 的表 |
| 引擎生命周期与配置 | `Close` 关闭后移除预编译语句；`Engine` 增加 `sync.RWMutex`，`NewSession` 一次取配置快照 | 修复前 `Close` 后仍持已关闭 `*Stmt`；setter 与 `NewSession` 并发有数据竞争 |
| 类型与错误语义 | MySQL `time.Duration → BIGINT`；枚举只大写类型关键字；JOIN 表名非法返回 `ErrJoinTableInvalid` | 修复前 Duration 存为时间点读不回；枚举取值里的 `enum` 被改写 |

验证：`gofmt -l .` 无输出、`go vet ./...` 干净、`go test ./...` 全绿；真库
`WXORM_IT_STRICT=1 WXORM_IT_DB=sqlite,mysql,postgres go test ./... -count=1` 三方言通过（约 9.1s）。
新增 `harness/integration/fix_audit2_test.go`（单连接池预加载、钩子内查询、1ns 超时下建表与 `ADD COLUMN`、
`LIMIT 0`、错误路径 nil 结果、分表读写隔离）与根模块单测（RAW 占位符扫描、分表缓存键、`LIMIT(0)`、
`Duration` 映射、枚举取值保留、nil 结果对象）。

## 十四、v1.0.0.0 正式发布

版本内容与 v0.16.0 一致：本轮只切换版本标识与发布基线，不改代码行为。

| 项目 | 内容 |
| --- | --- |
| 版本字符串 | `wxorm.go` 的 `version` 由 `v0.16.0` 改为 `v1.0.0.0`，启动标识、`RELEASE.md`、`README.md` 与 `ISSUES.md` 同步 |
| API 冻结 | 公开方法签名、导出错误变量、标签语义自本版本起只增不改，后续修复不得破坏已有调用 |
| 能力边界 | 三方言 CRUD / UPSERT / DDL / 索引 / 主键回填 / `JOIN` / `PRELOAD` 已在真库端到端验证；根模块仍零第三方依赖 |
| 遗留项 | `ISSUES.md` #111–#113（方言层缓存无界、`insertBatches` panic 丢栈、预加载共享超时预算）未修复，已入台账 |

版本号为四段式展示值；Go module 的标签与 `go get` 只接受三段式，仓库打标签时使用 `v1.0.0`。

验证沿用 v0.16.0 的结论且在新版本号下重跑：`gofmt -l .` 无输出、`go vet ./...` 干净、
`go test ./...` 与 `go test -race -p 1 ./...` 全绿；真库三方言 `go test ./...`（约 9s）与 `-race`（约 11s）均通过。
