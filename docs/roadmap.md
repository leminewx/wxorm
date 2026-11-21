# wxorm 更新与优化路线图

本文件是「按计划执行」的施工单：每个阶段都有明确的**改动清单**与**可验证的验收命令**，
完成一个阶段就更新状态，不在阶段之间夹带无关改动。

状态标记：`已完成` / `进行中` / `待开始`。

当前发布：`v1.0.0.0`（正式版）。下方阶段记录止于 v0.16.0，1.0 未引入功能变更。

---

## 阶段一：让项目可验证（已完成）

**目标**：任何人 clone 下来、不装 MySQL，就能跑通 `go test ./...`。

| 改动 | 说明 |
| --- | --- |
| 新增 `harness/examples/fakedriver` | `database/sql/driver` 内存实现，以 `mysql` 名字注册，覆盖 wxorm 实际生成的 SQL 子集 |
| 新增 `main_test.go`、`session/main_test.go` | `TestMain` 中 `fakedriver.Register()` |
| 新增 `harness/examples/fakedriver/fakedriver_test.go` | 驱动自身的 CRUD / 唯一冲突 / 计数冒烟测试，防止测试地基失真 |
| `examples.SetupTestDB` 去掉 `testing.TB` | 示例包不再把测试依赖带进生产构建 |

**验收**

```bash
go test ./... -count=1     # 全部 ok，无 FAIL，无连接超时
```

结果：11 个包全部 `ok`；原先 46 个 FAIL 清零。

---

## 阶段二：修复 P0 缺陷（已完成）

**目标**：把「静默给出错误结果」的路径全部堵住。

改动集中在四处：

1. **SQL 文本生成**（`dialect/clause/mysql.go`、`dialect/mysql.go`、`dialect/column/parser.go`）
   标识符引用、缓存键、空切片 panic、全文检索参数、外键 `ON UPDATE`、带参数标记误判。
2. **模型与缓存**（`schema/cache.go`、`schema/schema.go`、`session/hooks.go`）
   缓存键改为 `{driver, reflect.Type}`；钩子改用会话当前模型。
3. **会话时序**（`session/session.go`、`session/record.go`、`session/result/*.go`）
   事务内不复用 `*sql.DB` 预编译语句；补 `ctx`；每个导出方法清理语句残留；结果参数做拷贝。
4. **DDL 迁移**（`wxorm.go`）
   `Migrate` 脱离事务；删字段改 `ALTER TABLE ... DROP COLUMN`；不再吞错。

完整清单（含问题描述与修复方式）见 [analysis.md](./analysis.md) 第 3.1、3.2 节。

**验收**

```bash
go build ./...             # 退出码 0
go vet ./...               # 退出码 0
go test ./... -count=1     # 全部 ok
gofmt -l .                 # 无输出
```

---

## 阶段三：补齐方言能力（已完成）

**目标**：让「支持的方言」名副其实，要么补齐，要么明确不卖。

原状况：`postgresql.go`、`column/postgresql.go`、`datatype/postgresql.go` 存在大量空实现，
一旦被 `Get` 到就会静默产生坏 SQL。

- [x] 第一步：`dialect.Lookup` 未注册时返回 `ErrUnsupportedDriver`；`Register` / `Get` 对名字归一化。
- [x] 第二步：把 `Clause` 拆为「通用子句 + 可选能力接口」（`Upserter`、`IgnoreInserter`、
  `ReturningInserter`、`JSONQuerier`、`FullTextSearcher`），缺失能力返回 `ErrUnsupportedFeature`。
- [x] 第三步：PostgreSQL 端到端落地——类型映射、列 DDL（`SERIAL`/`BIGSERIAL`/`GENERATED ... STORED`）、
  建表与后置索引/注释、`information_schema`、`$n`、`ON CONFLICT`、`RETURNING`。
- [x] 第四步：`Session.UPSERT` 把冲突目标固定为模型主键，跨方言语义一致；`DUPLICATE` 标记为 MySQL 专用，
  PostgreSQL 下返回 `ErrUnsupportedFeature`。
- [x] 第五步：SQLite 方言——类型映射收敛到五个存储类别、自增主键内联为
  `INTEGER PRIMARY KEY AUTOINCREMENT`、`INSERT OR IGNORE` / `REPLACE INTO` /
  `ON CONFLICT (...) DO UPDATE SET` / `json_extract` / `json_each`、`sqlite_master`
  存在性查询、`TableIndexer` 后置索引；`DUPLICATE` 与全文检索明确不支持。
  抽 `BaseClause` 没有做（SQLite 直接复用 MySQL 版工具函数），仍留在待办 #63。

**验收**

- `dialect.Lookup` 对未注册方言返回 `ErrUnsupportedDriver`。
- 每个已注册方言都有独立的 SQL 生成用例（含标识符引用、占位符、分页、插入去重、UPSERT）。
- 已有：`dialect/clause/postgresql_test.go`、`dialect/postgresql_test.go`、`dialect/column/postgresql_test.go`、
  `dialect/datatype/postgresql_test.go`、`statement/postgresql_test.go`、`statement/where/search_condition_test.go`；
  SQLite 侧对应 `dialect/clause/sqlite_test.go`、`dialect/sqlite_test.go`、`dialect/column/sqlite_test.go`、
  `dialect/datatype/sqlite_test.go`、`statement/sqlite_test.go`，并在 `dialect/postgresql_test.go` 的
  能力矩阵中同时断言三个方言的接口实现情况。

---

## 阶段四：行为一致性（进行中）

**目标**：让同类操作在边界条件下表现一致，减少「猜 API」的成本。

| 待办 | 具体做法 | 状态 |
| --- | --- | --- |
| `Migrate` 语义收紧 | 新增只读 `DiffTable`；`Migrate` 补建缺失索引，列类型差异写日志提示，不再静默跳过 | 已完成 |
| 日志可注入 | `Logger` / `QueryEvent` + `SetLogger` / `SetSlowThreshold`，默认实现保持原格式 | 已完成 |
| 批量插入回填主键 | PostgreSQL / SQLite 新增 `clause.BatchReturningInserter`，多行 `RETURNING` 按行回填；MySQL 明确不回填 | 已完成 |
| 上下文贯通 | `Exec` / `Query` / `QueryRow` 统一走会话 ctx 与超时；长查询用 `SetTimeout(0)` 显式豁免 | 已完成 |
| 并发契约写进文档 | 明确「一个 `Session` 只能串行使用」，并给出并发场景的标准写法（每个 goroutine 各自 `NewSession`） | 已完成 |
| 错误分类 | 引入 `errors.Is` 可判别的错误族（无主键 / 缺 WHERE / 参数为空 / 方言不支持），便于调用方分支处理 | 进行中（`ErrRecordNotFound` `ErrSelectColumnNotFound` `ErrUnsupportedDriver` `ErrInvalidFullTextMode` `ErrConflictTargetRequired` 已可判别） |
| 常用语义补齐 | `FindFirst`（未命中报错）、`Exist`（不数全表）、`COLUMNS`（指定查询列） | 已完成 |
| `session/result` 补测试 | 三个空测试文件优先补上 `GetExecutionSQL` / `GetExecutionArgs` / `CopyArgs` 语义 | 已完成 |

**验收**

- 每个新增行为都有对应测试；`go test ./...` 仍全绿。
- 文档明确写出「不支持/未实现」的行为，不留猜测空间。

---

## 阶段五：工程化（进行中）

| 待办 | 说明 | 状态 |
| --- | --- | --- |
| CI 流水线 | `gofmt -l`、`go vet`、`go build`、`go test -race` 四道门禁 | 待开始 |
| 基准基线 | 已覆盖 `BuildSQL` / `Statement` / `Schema` / `Session` 四层，数据记入 [analysis.md](./analysis.md) 第 5 节 | 已完成 |
| 覆盖率报告 | 当前 `session/result`、方言扩展分支是覆盖盲区 | 进行中（`session/result`、`dialect` 已补） |
| 扩内存驱动 | 已补 `information_schema.columns`（配合 `Migrate` 查列名）；`BETWEEN` / `GROUP BY ... HAVING` 已在真实库用例覆盖，驱动侧按需再扩 | 部分完成 |

---

## 阶段六：真实数据库回归（已完成）

**目标**：把 fakedriver 能通过的用例搬到真实 MySQL / PostgreSQL / SQLite 上，确认 SQL 真能被数据库接受，
并建立可重复的性能基线。

| 改动 | 说明 |
| --- | --- |
| 新增 `harness/integration/` 独立子模块 | 引入 `go-sql-driver/mysql`、`pgx/v5`、`modernc.org/sqlite`；`replace => ../..` 回根模块，根 `go.mod` 保持零依赖 |
| 三方言同一套用例 | DDL / CRUD / 条件 / 排序分页 / 事务 / 钩子 / UPSERT / 类型 / 迁移 / 分批 / 并发，每个用例三方言都跑一遍 |
| 环境变量开关 | `WXORM_IT_MYSQL_DSN` / `WXORM_IT_PG_DSN` / `WXORM_IT_SQLITE_FILE` / `WXORM_IT_DB` / `WXORM_IT_STRICT`；未安装数据库时默认跳过 |
| 性能基线 | `BenchmarkInsertBatch`（1/20/200 行）与 `BenchmarkSelectPage`（100 行），数据记入 [analysis.md](./analysis.md) 12.4 节 |

**验证发现的 6 个真实缺陷**（MySQL 内联索引、`updated` 时间戳、`COLUMNS` 未知列、PG `AfterInsert`、
PG 迁移缓存计划、命名索引重复）已全部修复，详细对比见 [analysis.md](./analysis.md) 12.4 节。

**验收**

```powershell
cd harness/integration
go run ./cmd/preparedb             # 创建 wxorm_harness 真库
$env:WXORM_IT_STRICT = "1"
go test ./... -count=1             # 三方言全量通过
go test ./... -run "^$" -bench . -benchtime=1s
```

---

## 阶段七：测试资产归入 harness（已完成）

参照 leego 的目录约定，把示例、集成测试与测试数据收拢到 `harness/`，避免测试资产散落在仓库根部：

| 改动 | 说明 |
| --- | --- |
| `examples/` → `harness/examples/` | 测试模型与 `fakedriver` 的导入路径改为 `.../harness/examples` |
| `integration/` → `harness/integration/` | 子模块名与 `replace` 同比下移 |
| 新增 `harness/testdata` | `database.json`（连接参数）、`accounts.json`（种子数据）、`sqlite/`（真库，已忽略） |
| 新增 `harness/assetpath` / `harness/database` | 定位测试数据目录；解析配置并构造 DSN |
| 新增 `harness/integration/cmd/preparedb` | 一条命令创建 MySQL / PostgreSQL / SQLite 真库 |

**验收**：`cd harness/integration; go run ./cmd/preparedb; $env:WXORM_IT_STRICT="1"; go test ./... -count=1`
三方言全绿，`-race` 无报告，基准与阶段六基线一致。

---

## 阶段八：命名策略与 P0 隐患收敛（已完成）

把「逻辑表名」与「物理表名」拆开，支撑多租户/分库分表；同步清理遗留的注入面、吞错与无界缓存：

| 改动 | 说明 |
| --- | --- |
| 新增 `naming` 包 | `Strategy{Schema, Prefix, Suffix}` 与 `Validate`；schema 缓存只存逻辑表名，物理表名/限定名/预编译键在会话层计算 |
| `Engine.SetNaming` / `Session.SetNaming` | 引擎级默认 + 会话级覆盖；新增 `GetTableName` / `GetQualifiedTableName` |
| `ExistsTable` 改为 `(bool, error)` | 查询失败不再当成「表不存在」；`sql.ErrNoRows` 视为不存在；大小写不敏感方言用 `EqualFold` |
| 元数据查询参数化 | MySQL `?`、PostgreSQL `$n`、SQLite `?`；接口第二参数统一为 `namespace` |
| `Transact` 返回 `*PanicError` | 保留 panic 值与调用栈，`Unwrap` 使 `errors.Is/As` 可穿透 |
| 移除 LIMIT/OFFSET 缓存、方言注册表加锁 | 无界缓存改 `ctx.WriteInt`；`RWMutex` + 方言名归一化 |
| MySQL 列名转义、索引名去限定 | 列名反引号引用；`indexNameBase` 取最后一个点之后的段 |

**验收**：根模块 `gofmt -l .` 无输出、`go vet ./...`、`go test ./... -count=1` 全部 ok；
`cd harness/integration; go vet ./...; go test ./... -count=1 -race` 三方言全绿。
真库回归曾捕获「PostgreSQL 把连接库名当成 schema」的漏判，已修正并补根模块用例。

---

## 阶段九：可观测性、会话池与迁移补全（已完成）

v0.10.0 把阶段四余下的四项一次做完，并补上会话复用与结构差异报告：

| 改动 | 说明 |
| --- | --- |
| 上下文贯通 | `Exec` / `Query` / `QueryRow` 统一走会话 ctx 与超时；`SetTimeout(0)` 关闭 |
| 可注入日志 | `Logger` / `QueryEvent` / `SetLogger` / `SetSlowThreshold`；默认实现保持原格式 |
| 会话池 | `sync.Pool` + `SetID` / `SetReleaser` / `Reset`；`Close` 回滚后归零归还，`Transact` 自动归还 |
| 批量回填主键 | 新增 `clause.BatchReturningInserter`；PG / SQLite 多行 `RETURNING` 按行回填，MySQL 不回填 |
| 迁移差异报告 | `Engine.DiffTable` + `TableDiff` / `ColumnDiff`；`Migrate` 补建缺失索引，类型差异只提示 |
| 方言数据字典读取 | `dialect.TableColumnTyper` / `TableIndexReader`，三方言均已实现 |

**验收**：根模块 `gofmt` / `vet` / `test` / `test -race` 全绿；`cmd/preparedb` 建库后
MySQL 8.4 / PostgreSQL 17 / SQLite 三方言真库全量用例与 `-race` 均通过，含批量主键回填与迁移差异报告用例。
PostgreSQL / SQLite 基准与 v0.9.0 持平；MySQL 本环境基准受驱动层延迟 ACK 影响（原始 `database/sql` 同样复现），
需在无此现象的环境复测。

---

## 阶段十：查询缓存与批量写入性能（已完成）

把性能分析中的两项 P0 与一项 P1 落地，目标是不改语义的前提下消除重复 prepare、降低扫描分配、
减少批量写入的语句数：

| 改动 | 说明 |
| --- | --- |
| 查询语句缓存 | `StatementCache`（FIFO、默认 256）+ `Engine.SetStatementCacheSize` / `Session.SetStatementCache`；SELECT / COUNT / Exist / UPDATE / DELETE / 多行 INSERT 复用 |
| 缓存边界 | 事务内绕过；事务提交/回滚不遗留；`CreateTable` / `DeleteTable` / `Migrate` / `Engine.Close` 清空 |
| 结果集预分配 | `LIMIT` 记录为容量提示（上限 1024，无 LIMIT 起步 16），保留调用方预置元素 |
| 直接扫描 | `[]Struct` 扩容后直接扫描到切片元素，去掉每行临时实例与整结构体拷贝 |
| 自适应分批 | 按方言占位符上限与列数计算单批行数，上限 1000；不再按 100/500/1000 固定拆事务 |
| UPDATE 零值策略 | `UPDATE(struct)` 默认只写非零字段；需要整行覆盖时用 `OVERWRITE()`，只改指定列时用 `UPDATE_COLUMNS(...)` 或 `UPDATE(map)`；全零结构体返回 `ErrUpdateStructEmpty` |
| 主键条件稳定 | 复合主键按列顺序输出 `[]schema.FieldValue`，替代 map 遍历，保证 SQL 文本稳定命中语句缓存 |
| 单行直扫 | 抽出 `buildSelect` 后 `FindFirst` / `SELECT(&struct)` 直接扫描单行，不再经切片中转并清掉残留 LIMIT |
| 字段访问器预编译 | 解析期预计算 `PrimaryKeyFields` / `UpdateFields`（`FieldIndex`），`Extract*` 不再逐列判断主键与自动插入 |

同轮收尾另评估了结果对象参数的懒拷贝（`copyArgs`）：`clause.Context.Reset` 用 `args[:0]` 复用底层数组，
交出所有权后构建器必须重新 `append`，实测 15.9ns/64B/1 alloc 变 44ns/112B/3 allocs，不采纳，记录在 ISSUES #89。

**验收**：根模块 `gofmt` / `vet` / `test` / `test -race` 全绿；`cmd/preparedb` 建库后
`WXORM_IT_STRICT=1 go test ./... -count=1`（5.60s）与 `-race`（8.19s）三方言全绿，
新增 `TestPreparedStatementCache`、`TestBatchInsertLarge`、`TestSession_First_DirectScan`、
`TestSchema_ExtractNonZeroPrimaryKeyValues`、`TestSchema_ExtractUpdateNamesAndValues`。实测：

- 分页 100 行 MySQL 43.9ms → 0.44ms、SQLite 0.226ms → 0.194ms（分配约 108KB → 63.8KB）；
- 单行查询 MySQL 44.26ms → 2.60ms、PG 273.7µs → 208.9µs、SQLite 30.7µs → 17.3µs；
  `FindFirst` 直扫后 SQLite 10.83µs → 10.47µs（少 4 allocs / 90B），PG 183.1µs → 180.4µs，MySQL 在噪声内；
- 批量 1000 行 MySQL 190.98ms → 71.93ms、PG 100.51ms → 61.65ms、SQLite 3.295ms → 2.563ms；
- 复合主键提取 96.9ns / 400B / 3 allocs → 22.1ns / 64B / 1 alloc（`BenchmarkSchema_ExtractPrimaryKey`）。

---

## 阶段十一：审计修复与大数支持（已完成）

按审计报告逐项收敛「静默失真」与「静默丢数据」的默认行为，并让 `math/big` 真正可用（根模块仍零依赖）：

| 改动 | 说明 |
| --- | --- |
| 大数支持 | `big.Int` / `big.Float` 作为模型字段可直接读写：写入格式化为十进制文本，读取经 `sql.NullString` 回填；新增 `schema.BigKind` / `BigFieldKinds` / `HasBigFields` / `FormatBigValue` / `ScanBigValue` 与 `session.bigScanner`，普通模型零额外分支 |
| 大数类型映射 | MySQL `DECIMAL(65,0)` / `DECIMAL(65,30)`（原先误落 JSON）、PostgreSQL `NUMERIC`、SQLite `TEXT`（`NUMERIC` 亲和性会把超大数转 REAL） |
| `big.Rat` 明确拒绝 | 模型解析阶段返回 `schema.ErrUnsupportedBigRat`，避免建表成功而写入失真 |
| UPDATE 零值策略改名 | `IncludeZero()` 改名 `OVERWRITE()`（不保留旧名），新增 `UPDATE_COLUMNS(...)` 白名单；未知列 `schema.ErrUnknownUpdateColumn`、空白名单 `session.ErrUpdateColumnsEmpty` |
| `Migrate` 默认不删列 | 模型之外的列只进入 `DroppedColumns` 报告，`WithDropMissingColumns()` 才执行 `DROP COLUMN`；列名/类型比较改为不区分大小写 |
| 语句缓存并发安全 | FIFO 改 LRU + 引用计数（`Stmt.acquire/Release/retire`），淘汰不立即 `Close`；新增导出的 `Session.ClearStatementCache()` |
| 上下文生命周期 | `Query` / `QueryRow` 不再叠加会话超时 timer（无法在消费完成时 cancel 会泄漏），改由 `*Context` 变体显式控制 |
| 事务守卫 | `Begin` 委托 `BeginTx(ctx, opts)`；重复 `Begin` 返回 `ErrTransactionAlreadyStarted`，未开启就提交/回滚返回导出的 `ErrTransactionNotStartedYet`；新增 `Engine.TransactContext` |
| 集合语义 | `IN()` 空集合生成恒假 `1 = 0`、`NOTIN()` 生成恒真 `1 = 1`，不再报错；`COUNT` 遇到 `GROUP` / `HAVING` 返回 `ErrCountWithGroupUnsupported` |
| 日志脱敏 | 慢 SQL 默认只输出参数个数（`QueryEvent.LogArgs`），`SetLogParamValues(true)` 显式开启；调试模式不受影响 |
| 批量回填校验 | 多行 `RETURNING` 行数与写入行数不一致时返回 `ErrBatchReturningMismatch`，不再按错位下标回填主键 |

**验收**：根模块 `gofmt -l .` 无输出、`go vet ./...`、`go test -race ./...` 全绿；
`cd harness/integration; WXORM_IT_STRICT=1 WXORM_IT_DB=mysql,postgres,sqlite go test -race ./...` 全绿。
真库新增 `TestBigNumber`（big.Int 30 位大数 / big.Float 30 位小数 / SQL NULL 回读）、`TestBigRatRejected`、
`TestUpdateColumns`、`TestCountRejectsGroup`、`TestMigrateKeepsDroppedColumns`。

---

## 执行约定

1. **一轮一件事**：一个阶段内可以并行，但不跨阶段混改，避免回归时无法定位。
2. **先取证再动手**：测试期望与实现不一致时，先判断是「实现错」还是「测试错」。
   本轮的 `dialect/column` 断言漂移属于测试错，`BuildSelectSQL` 返回 `SELECT *` 属于实现错。
3. **每层改完立刻验证**：`dialect` → `schema` → `session` → `wxorm`，逐层 `go build` + `go test`。
4. **文档与代码同批提交**：缺陷修完就更新本文件状态与 [ISSUES.md](../ISSUES.md)，不留「以后补」。
