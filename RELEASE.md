# RELEASE

## v1.0.0.0 — 2026-11-22

**首个正式发布版本（GA）**。版本内容与 v0.16.0 一致，不引入新功能与新 API：本轮只切换
版本标识与发布基线，并冻结公开 API。

### 发布基线

- 公开 API 自本版本起冻结：方法签名、导出错误变量、结构体标签语义不再做破坏性变更；
  后续修复以新增 API 或新增选项的方式提供。
- 三方言（MySQL / PostgreSQL / SQLite）的 CRUD、UPSERT、DDL、索引、主键回填、
  `JOIN`（N:1）与 `PRELOAD`（1:N）均已在真库上端到端验证。
- 根模块保持零第三方依赖，真实驱动只存在于 `harness/integration` 子模块。

### 验证

- `gofmt -l .` 无输出；`go vet ./...` 干净；`go test ./...` 与 `go test -race -p 1 ./...` 全绿。
- 真库三方言：`WXORM_IT_STRICT=1 WXORM_IT_DB=sqlite,mysql,postgres go test ./...` 通过（约 9s），
  同一组合叠加 `-race` 通过（约 11s）。

### 版本号说明

- 对外展示版本为四段式 `v1.0.0.0`（`wxorm.go` 的 `version`、启动标识与本文档一致）。
- Go module 的标签与 `go get` 只接受三段式语义化版本，因此仓库打标签时使用 `v1.0.0`；
  四段式只作为发布序号，不参与模块解析。

### 已知遗留

- `ISSUES.md` 的 #111–#113（方言层缓存无界、`insertBatches` panic 丢栈、预加载共享超时预算）
  仍未修复，已入台账；不影响 1.0 的既有能力边界。

## v0.16.0 — 2026-11-22

第十六轮：**并发自锁、DDL 超时与静默失真修复**。把第二轮系统性审计发现的
P1/P2 问题全部落地，重点处理「单连接池下查询自锁」与「DDL 被查询超时打断」
两类会直接造成线上故障的缺陷，并在三方言真库上补齐回归用例。

### 修复

- 查询连接自锁：`FindFirst` / `SELECT` 在结果集未关闭时就执行 `AFTER_SELECT` 钩子与
  `PRELOAD` 子查询，单连接池下自锁到会话超时（实测 15s 后 `context deadline exceeded`），
  默认池下一条查询固定占两条连接。改为扫描完成后立即 `rows.Close()`，钩子在结果集
  释放后统一执行（多行查询的钩子也延迟到循环之后）。
- DDL 超时：新增 `Session.ExecDDL`（不叠加会话查询超时、仍继承会话上下文），
  `CreateTable` / `DeleteTable` / `Migrate` 的建表、`ALTER TABLE`、建索引全部改用。
  修复前在 1ns 超时下建表直接报 `context deadline exceeded`。
- 错误路径结果对象：执行失败时返回 nil（不再返回带 nil 内层的 `result`），
  结果对象自身在无底层结果时返回 `ErrNoExecutionResult`，调用 `RowsAffected()` 不再 panic。
- `LIMIT(0)` 生成 `LIMIT 0`（不再退化成全表查询），`LIMIT(0)+OFFSET(n)` 不再生成非法 SQL；
  负数返回 `ErrLimitNegative`。
- `where.RAW` 改为词法扫描占位符：字符串字面量、引用标识符、行/块注释与 PostgreSQL
  美元引用内的 `?` 原样保留，只有真正的占位符改写为 `$n`。
- schema 缓存键加入逻辑表名：实现 `TableNameGetter` 的分表/多租户模型不再共用首个实例的
  表名（修复前 2024/2025 两个分片都会落到 2024 的表）。表名为空时不写入缓存。
- `Engine.Close` 在关闭预编译语句后一并移除条目；重复 `Close` 保持幂等。
- `Engine` 增加 `sync.RWMutex`：配置 setter 与 `NewSession` 并发不再有数据竞争，
  `NewSession` 一次性取配置快照。
- `time.Duration` 在 MySQL 上映射为 `BIGINT`（纳秒）而不是 `TIMESTAMP`，读写不再失真。
- 枚举列只大写类型关键字，枚举取值里的 `enum` 不再被改写。
- JOIN 表名含非法字符返回新增的 `ErrJoinTableInvalid`，不再误报 `ErrJoinTableIsEmpty`。

### 新增 API

- `Session.ExecDDL(sql, args...)`：执行不叠加查询超时的 DDL。
- `result.ErrNoExecutionResult`：结果对象缺少底层结果时的错误。
- `session.ErrJoinTableInvalid`：JOIN 表名非法。
- `statement.ErrLimitNegative`：`LIMIT` 传入负数。

### 测试

- 根模块新增：`where.RAW` 占位符扫描（字面量/引用/注释/美元引用/转义引号）、
  分表缓存键、`LIMIT(0)` 语义、`Duration` 映射、枚举取值保留、nil 结果对象。
- `harness/integration/fix_audit2_test.go`（三方言）：单连接池下的单行/切片预加载、
  `AFTER_SELECT` 钩子内查询、1ns 超时下的建表与 `ADD COLUMN`、`LIMIT 0`、
  错误路径 nil 结果、分表读写隔离。

### 兼容性

- 模型未使用 `time.Duration` 字段时 SQL 文本不变；已有 `TIMESTAMP` 旧表的 `Duration` 列
  需要手工迁移类型。
- `LIMIT(0)` 的语义由「忽略」变为「返回 0 行」，分页代码若依赖旧行为需显式判断。
- 执行失败时结果对象为 nil：同时读取 `res` 与 `err` 的调用方必须先处理 `err`。
- 未实现 `TableNameGetter`、未使用 `PRELOAD` / 钩子内查询、未在运行期改配置的用法不受影响。

## v0.15.0 — 2026-11-22

第十五轮：**审计修复与大数支持**。逐项收敛「静默失真」与「静默丢数据」的默认值，
并让 `math/big` 在根模块零依赖的前提下真正可用。

### 新增

- `schema`：`big.Int` / `big.Float` 字段支持——`BigKind`（`BIG_NONE` / `BIG_INT` / `BIG_FLOAT`）、
  `Schema.BigFieldKinds` / `HasBigFields`、`GetBigKind` / `FormatBigValue` / `ScanBigValue` /
  `EnsureBigSupported`；`ErrUnsupportedBigRat`、`ErrBigValueMalformed`。
- `session`：`bigScanner`（先扫进 `sql.NullString` 再回填）、`UPDATE_COLUMNS(columns ...string)`、
  `BeginTx(ctx, opts)`、`ClearStatementCache()`、`SetLogParamValues(bool)`；
  `ErrUpdateColumnsEmpty`、`ErrCountWithGroupUnsupported`、`ErrBatchReturningMismatch`。
- `wxorm`：`Migrate(value any, options ...MigrateOption)` 与 `WithDropMissingColumns()`；
  `Engine.TransactContext(ctx, txFunc)`、`Engine.SetLogParamValues(bool)`。
- 导出错误 `ErrTransactionAlreadyStarted` / `ErrTransactionNotStartedYet`；`QueryEvent.LogArgs`。

### 变更（破坏性）

- `IncludeZero()` 改名 `OVERWRITE()`，**不保留旧名**：需要整行覆盖的调用点需同步改名；
  只改指定列请用新增的 `UPDATE_COLUMNS(...)`。
- `Migrate` 默认不再删除模型之外的列：字段删除/改名/改大小写只进入 `DroppedColumns` 报告，
  需要 `wxorm.WithDropMissingColumns()` 才执行 `DROP COLUMN`。
- `Begin()` 在事务已开启时返回 `ErrTransactionAlreadyStarted`，不再静默覆盖旧事务。
- `Query` / `QueryRow` 不再叠加会话超时 timer（无法在消费完成时 cancel 会泄漏连接）；
  需要超时请用 `QueryContext` / `QueryRowContext`。
- 慢 SQL 日志默认只输出参数个数，不再落参数原值；需要定位数据时显式 `SetLogParamValues(true)`。

### 语义约定

- 大数写入格式化为十进制文本（`big.Int` 用 `String()`、`big.Float` 用 `Text('f', 30)`），
  DDL 映射为 MySQL `DECIMAL(65,0)` / `DECIMAL(65,30)`、PostgreSQL `NUMERIC`、SQLite `TEXT`；
  `big.Rat` 在模型解析阶段返回 `ErrUnsupportedBigRat`。
- `where.IN()` 空集合生成恒假 `1 = 0`、`NOTIN()` 生成恒真 `1 = 1`，不再生成非法 SQL 或报错。
- `COUNT` 遇到 `GROUP` / `HAVING` 返回 `ErrCountWithGroupUnsupported`，不再静默丢分组。
- 语句缓存改 LRU + 引用计数：淘汰只标记退役，最后一个使用者归还后才 `Close`，
  并发下不再出现 `sql: statement is closed`。
- `Migrate` 的列名与列类型比较不区分大小写，避免仅改大小写被判定成「删一列 + 加一列」。
- 多行 `RETURNING` 行数与写入行数不一致时返回 `ErrBatchReturningMismatch`，
  不按下标错位回填主键。

### 测试与文档

- 新增 `session/stmt_test.go` 并发用例、`schema/big_test.go`、`harness/integration/audit_fix_test.go`；
  真库覆盖 big.Int 30 位大数、big.Float 30 位小数、SQL NULL 回读、`big.Rat` 拒绝、
  `UPDATE_COLUMNS` 白名单与零值、`COUNT` 拒绝分组、`Migrate` 默认不删列。
- README 新增「16. 大数支持」章节，同步 `UPDATE_COLUMNS` / `OVERWRITE`、`Migrate` 选项、
  `SetLogParamValues`、`TransactContext` / `BeginTx`、缓存 LRU 与 `ClearStatementCache`；
  `docs/roadmap.md` 新增阶段十一，`docs/analysis.md` 与 `ISSUES.md` 同步台账。

### 兼容性

模型未使用 `big.*`、未调用 `OVERWRITE` / `UPDATE_COLUMNS` / `Migrate` 选项时，SQL 文本与读写结果不变。
需要关注的只有四处默认值变化：`Migrate` 不删列、`Begin` 重复调用报错、`Query` 不再叠加超时、
慢 SQL 参数脱敏。

## v0.14.0 — 2026-11-22

第十四轮：**关联预加载（Preload）**。补上 1:N 集合与 N:1 嵌套对象：不拼 JOIN，用
「父查询 + `WHERE 关联列 IN (...)` 批量子查询 + 内存合并」保持分页与父行数不变。
设计与验收见 [docs/preload-design.md](docs/preload-design.md)。

### 新增

- `schema`：`preload(<column>)` 标签、`Relation`、`RelationKind`（`RELATION_BELONGS_TO` / `RELATION_HAS_MANY`）、
  `Schema.Relations` / `RelationIndexesByName`、`GetRelationByName` / `SelectRelations`；
  `ErrInvalidPreloadTag`、`ErrPreloadFieldType`、`ErrPreloadRelationConflict`、`ErrPreloadUnknownField`。
- `session`：`PRELOAD(fields ...string)`；`ErrPreloadUnsupported`、`ErrPreloadColumnNotFound`、
  `ErrPreloadRequiresSingleKey`、`ErrPreloadKeyNotSelected`。
- `harness/examples/stubdriver`：不解析 SQL 的脚本化行驱动（`Push` / `PushError` / `PushExec` /
  `Calls` / `ArgCounts`），用于覆盖真库与 fakedriver 都不好构造的结果映射边界。

### 语义约定

- 方向由字段类型推导：`*T` / `T` 为 belongs_to（本表列 → 目标主键），`[]T` / `[]*T` 为 has_many（目标表列 → 本表主键）。
- `PRELOAD()` 空参表示加载全部声明；只加载一层，不递归；目标表照常套用 naming 策略。
- 子查询固定「目标表全部列 + `IN` + `ORDER BY` 目标主键」，键去重后每批最多 30000 个占位符。
- `*T` 无匹配保持 `nil`；`[]T` 无匹配返回空切片（非 nil）。
- 声明一次性：`SELECT` / `FindFirst` 取走归零；`COUNT` / `Exist` 静默丢弃；写语句返回 `ErrPreloadUnsupported`。
- 与 `COLUMNS` 同用必须包含分组键，否则 `ErrPreloadKeyNotSelected`。
- 子查询走会话连接（事务内可见未提交数据），但不触发模型钩子。

### 测试与文档

- 新增 `schema/preload_test.go`、`session/preload_test.go`、`session/scan_test.go`、
  `harness/integration/preload_test.go`；覆盖标签解析、列集隔离、SQL 形态、状态机、断批、
  零值键、共享键去重、错误路径与 stub 映射边界。
- 真库：SQLite / MySQL / PostgreSQL 三方言覆盖 belongs_to、has_many、空集合、`COLUMNS`、
  naming 前缀、JOIN 共存与事务内预加载（5 个用例 × 3 方言）。
- README 新增「关联预加载」章节、`preload(...)` 标签与 API 速查；`docs/analysis.md` 功能矩阵
  与缺口表同步；`docs/join-design.md` 第 8 节标记已落地。
- 同时在「已知限制」澄清主键回填差异：单条仅 PostgreSQL 回填模型；批量 PostgreSQL / SQLite 回填，
  MySQL 不回填。

### 兼容性

纯新增：不声明 `preload` 标签且不调用 `PRELOAD` 时，SQL 文本、方法与错误语义与 v0.13.0 完全一致。

## v0.13.0 — 2026-11-21

第十三轮：**N:1 联表查询**。在不破坏既有 API 的前提下，把「主表 + 关联表」的过滤、排序、分页
放进同一条 SQL，并保持结果映射、预编译缓存、超时与日志链路不变。设计方案与验收清单见
[docs/join-design.md](docs/join-design.md)。

### 新增

- `dialect/clause`：`JoinKind`（`JOIN_INNER` / `LEFT` / `RIGHT` / `FULL` / `CROSS`）与可选能力接口
  `Joiner{BuildJoinSQL, SupportsJoinKind}`；MySQL / PostgreSQL / SQLite 均实现，基础 `Clause` 不新增方法。
- `statement`：`SELECT_COLUMNS`（显式投影）、`JOIN`（单函数遍历多个 join，避免同名覆盖）、
  `COUNT_DISTINCT`（联表统计去重）。
- `statement/where`：`Column` 与 `EQ_COLUMN`，用于 ON 条件里的列对列比较（右侧写标识符，不占位符）。
- `schema`：`join(<alias>.<column>)` 标签、`JoinColumn`、`Schema.JoinColumns`、
  `Schema.JoinColumnIndexesByName`、`GetJoinColumnByName`；结果列别名固定为 `<alias>_<column>`。
- `session`：`JOIN` / `INNER_JOIN` / `LEFT_JOIN`；`ErrJoinUnsupported`、`ErrJoinTableRequired`、
  `ErrJoinAliasRequired`、`ErrJoinAliasInvalid`、`ErrJoinRequiresSinglePK`、`ErrJoinCountColumnUnsupported`、
  `ErrJoinAliasNotJoined`、`ErrJoinColumnNotNilable`。

### 语义约定

- `SELECT` / `FindFirst` / `COUNT` / `Exist` 支持 JOIN；`GROUP` / `HAVING` / `ORDER` / `LIMIT` 照常可用（列需自行限定）。
- 主表列统一用物理表名限定，避免关联表同名列歧义；`COLUMNS` 可同时指定主表列与联表结果列别名。
- 外连接下联表字段必须可空（指针/接口/切片/map 或实现 `sql.Scanner`），否则构建期返回 `ErrJoinColumnNotNilable`。
- JOIN + `COUNT` 自动改为 `COUNT(DISTINCT 主表.单列主键)`，不接受列参数；无主键/复合主键返回 `ErrJoinRequiresSinglePK`。
- `INSERT` / `UPDATE` / `DELETE` 存在 JOIN 时拒绝执行并清空状态，避免状态泄漏。
- join 表名是逻辑表名，会套用 naming 的前缀/后缀/Schema；别名不套策略且必须显式。
- MySQL 不支持 `FULL JOIN`，返回 `clause.ErrUnsupportedFeature`；SQLite 的 RIGHT/FULL 依赖 3.39+。

### 测试与文档

- 新增 `dialect/clause/join_test.go`、`statement/join_test.go`、`statement/where/column_test.go`、
  `schema/join_test.go`、`session/join_test.go`；`harness/integration/join_test.go` 覆盖三方言
  N:1 映射、LEFT NULL、`COLUMNS`、分页、`COUNT` / `Exist` 与 naming 前后缀。
- 内存驱动（`harness/examples/fakedriver`）仍未实现 JOIN，会话层单测只验证 SQL 构建与错误路径。
- README 新增「联表查询」章节与 `join(...)` 标签说明，`docs/analysis.md` 同步移除「不提供 JOIN」的限制。

### 兼容性

纯新增：既有方法签名、SQL 文本与错误语义均未改变；不声明 `join` 标签且不调用 `JOIN` 时行为与 v0.12.0 完全一致。

## v0.12.0 — 2026-11-21

第十二轮：**仓库更名为 wxorm**。模块路径、包名、错误前缀、结构体标签键、环境变量与文档全面切换；
改名前的标签键不保留，旧模型需同步改写（升级清单见本条末尾）。

### 变更（破坏性）

- 模块路径 `gitee.com/leminewx/worm` → `gitee.com/leminewx/wxorm`，`go.mod`、导入路径与依赖方需同步；
- 根包名 `worm` → `wxorm`，调用改为 `wxorm.NewEngine(...)`；
- 错误前缀 `worm:` → `wxorm:`，涉及 `dialect`、`schema`、`session`、`statement` 与根包；
- 结构体标签键 `worm:` → `wxorm:`，**不保留旧键**：旧模型需把 `worm:` 改为 `wxorm:`（`orm:` 仍兼容）；
  解析模型时若仍遇到旧键，直接返回新增的 `schema.ErrLegacyTagRemoved`，避免旧模型被静默跳过而少列；
- 启动标识字形改为 `WXORM`，`AppName` 输出 `WxORM`；ASCII 样式改为纯 ASCII 井号体；
- 集成测试环境变量 `WORM_IT_*` → `WXORM_IT_*`，专用库名 `worm_harness` → `wxorm_harness`，
  升级后需重新执行 `harness/integration/cmd/preparedb` 建库；
- 同步 README、docs/analysis.md、docs/roadmap.md 与 ISSUES.md。

### 升级清单

1. `go.mod` 与所有导入路径改为 `gitee.com/leminewx/wxorm`；
2. 调用方包名由 `worm` 改为 `wxorm`；
3. 模型标签 `worm:"..."` 改为 `wxorm:"..."`（`orm:` 可保留），否则解析模型时返回 `schema.ErrLegacyTagRemoved`；
4. 集成测试环境变量 `WORM_IT_*` 改为 `WXORM_IT_*`，并重跑 `preparedb` 建 `wxorm_harness` 库。

## v0.11.0 — 2026-11-21

第十一轮：**查询语句缓存、结果集预分配与自适应分批**。把 v0.10.0 性能分析中的两项 P0 与一项 P1
全部落地，重点消除 MySQL 上「每次查询都重新 prepare」的固定开销，并降低结果集与批量写入的分配。
根模块继续保持零第三方依赖。

同轮收尾补齐四项：单行直扫、主键顺序稳定、UPDATE 零值策略修正与字段访问器预编译。

### 新增

- `session.StatementCache`：带容量上限的查询语句缓存，按写入顺序淘汰最旧语句；
  `NewStatementCache(max)` / `LoadOrPrepare(query, prepare)` / `Len` / `Clear`。
- `Engine.SetStatementCacheSize(size)`：设置引擎级查询缓存容量，`0` 或负数关闭；
  默认 `session.DefaultStatementCacheSize = 256`，由所有会话共享。
- `Session.SetStatementCache(cache)`：会话级注入/关闭查询缓存；会话重置时保留，与连接、方言同属引擎级依赖。
- 新增基准：`BenchmarkSessionSelectRowsCached` / `BenchmarkSessionFirstCached` /
  `BenchmarkSessionUpdateStructCached`，用于对照关闭缓存时的固定开销。
- `Session.IncludeZero()`：`UPDATE(struct)` 时把零值字段一并写回（默认跳过零值）。
- `Schema.PrimaryKeyFields` / `Schema.UpdateFields` 与 `FieldIndex` / `FieldValue`：预计算主键列与可更新列
  的字段索引；新增 `Schema.ExtractNonZeroPrimaryKeyValues`（按列顺序输出）与
  `Schema.ExtractUpdateNamesAndValues(value, includeZero)`。
- `session.ErrUpdateStructEmpty`：全零结构体更新不再静默执行。
- 新增基准：`BenchmarkSchema_ExtractPrimaryKey`。
- 启动标识：`ShowLogo()` / `WriteLogo(w)` / `LogoStyle` / `SetLogoStyle` / `GetLogoStyle`，
  内置宽幅艺术字、实心像素块、双线阴影、纯 ASCII 四种样式；样式用 `atomic.Int32` 保存，
  可并发打印；`WriteLogo(nil)` 返回明确错误而不静默丢弃输出。

### 变更

- `SELECT` / `COUNT` / `Exist` / `UPDATE` / `DELETE` 与多行 `INSERT` 在非事务场景复用预编译语句；
  事务内一律绕过缓存（语句绑定在 `*sql.DB` 上，进入事务会逃逸回滚语义）。
- `CreateTable` / `DeleteTable` / `Engine.Migrate` / `Engine.Close` 会清空查询缓存，避免 DDL 后执行计划失效。
- 结果集按 `LIMIT` 预分配容量（上限 1024，无 `LIMIT` 时起步 16，保留调用方已有元素）；
  `[]Struct` 直接扩容后扫描到新元素，省去每行的临时实例与整结构体拷贝。
- 批量 `INSERT` 改为按方言占位符上限与列数自适应分批：MySQL / PostgreSQL 单条最多 60000 个占位符，
  SQLite 30000，单批行数上限 1000；不再按 100 / 500 / 1000 的固定档位拆事务。
- `UPDATE(struct)` 默认只写非零字段（与 README、注释声明的契约一致）：部分结构体不会再把未赋值的列清空；
  全零结构体返回 `ErrUpdateStructEmpty`，需要显式置零时用 `IncludeZero()` 或 `UPDATE(map)`。
- 自动主键 WHERE 改为按列顺序生成：复合主键不再因 map 遍历产生不同 SQL 文本，预编译缓存稳定命中。
- `FindFirst` 与 `SELECT(&struct)` 直接扫描单行：抽出 `buildSelect` 复用 SELECT 构建逻辑，
  省掉中转切片与整结构体拷贝；未查询列归零，`LIMIT` 状态用后即清，不再残留到下一条语句。
- `show` 类取值接口（`Extract*`）改为遍历预计算字段索引，不再逐列判断主键 / 自动插入。

### 性能（本机 i5-14600KF，真库三方言）

| 场景 | MySQL | PostgreSQL | SQLite |
| --- | --- | --- | --- |
| 分页 100 行（优化前 → 后） | 43.9ms → 0.44ms（约 99×） | 0.437ms → 0.40ms | 0.226ms → 0.194ms；分配约 108KB → 63.8KB |
| 单行 `FindFirst`（缓存关 → 开） | 44.26ms → 2.60ms（约 17×） | 273.7µs → 208.9µs | 30.7µs → 17.3µs |
| 结构体 `UPDATE`（缓存关 → 开） | 44.31ms → 2.51ms（约 17.6×） | 787µs → 769µs | 250.1µs → 249.0µs |
| 批量 200 行（旧事务分批 → 单语句） | 93.65ms → 8.83ms | 4.22ms → 3.35ms | 0.926ms → 0.749ms |
| 批量 1000 行（旧事务分批 → 单语句） | 190.98ms → 71.93ms | 100.51ms → 61.65ms | 3.295ms → 2.563ms |

MySQL 的旧数据正是 v0.10.0 记录的「复用连接逐次 prepare 被延迟 ACK 放大」现象；
复用预编译语句后该开销消失。SQLite 的写路径仍由落盘主导，缓存对 `UPDATE` 几乎无影响；
收益集中在查询侧与批量写入的语句数。PG 的 RTT 较短，收益主要来自批量合并与少量分配下降。

收尾项实测（同机）：

- 复合主键提取（map → 切片）：96.9ns / 400B / 3 allocs → 22.1ns / 64B / 1 alloc。
- 单行 `FindFirst`（切片中转 → 直扫）：MySQL 197.5µs ↔ 196.4µs（噪声内）、PG 183.1µs → 180.4µs、
  SQLite 10.83µs → 10.47µs；三方言均减少 4 次分配，SQLite 少 90B。
- 参数切片懒拷贝为**不采纳**项：`Context.Reset` 用 `args[:0]` 复用数组，交给结果对象后构建器只能重新 append，
  4 个参数实测 15.9ns / 64B / 1 alloc → 44ns / 112B / 3 allocs。

### 测试

- 根模块：`gofmt -l .` 无输出、`go vet ./...` ok、`go test ./... -count=1` 全绿、
  `go test -race ./... -count=1` 全绿。
- 新增单测：`TestStatementCache_LoadOrPrepare`（命中/淘汰/清空）、`TestStatementCache_ConcurrentLoad`
  （16 协程只保留一条语句）、`TestSession_PreparedQueryCache`（查询/统计/更新/删除复用）、
  `TestSession_PreparedCacheBypassedInTx`（事务内不写缓存）、`TestSession_DDLClearsCache`、
  `TestSession_SelectPrealloc`（LIMIT 预分配且保留预置元素）、`TestInsertBatchSize`（三方言与宽表边界）、
  `TestInsertLargeBatch`（1200 行自动拆批）、`TestSession_First_DirectScan`（指定列 / 未查询列归零 / LIMIT 1）、
  `TestSchema_ExtractNonZeroPrimaryKeyValues`（复合主键顺序）、`TestSchema_ExtractUpdateNamesAndValues`（零值策略）。
- 真库：`WXORM_IT_STRICT=1 go test ./... -count=1` 三方言通过（5.60s），`-race` 通过（8.19s）；
  新增 `TestPreparedStatementCache`（真库单行查询/统计/更新/删除）与 `TestBatchInsertLarge`
  （1200 行分批，PG / SQLite 逐行回填主键），`TestUpdateDelete` 补「部分结构体不清空未赋值列」与
  `IncludeZero` 置零用例，三方言均通过。
- 说明：内存假驱动的 `Prepare` 只保存 SQL、`Stmt.Exec` 仍会完整解析，因此根模块基准无法体现
  prepare 收益；缓存收益以真库数据为准，假驱动新增的三个 `*Cached` 基准仅用于监控回归。

### 命名

除构造函数 `New*` 外，本轮新增导出函数/方法首词均为动词：`SetStatementCacheSize`、
`SetStatementCache`、`LoadOrPrepare`、`Clear`、`Len`、`IncludeZero`、`ExtractNonZeroPrimaryKeyValues`、
`ExtractUpdateNamesAndValues`；`StatementCache` / `FieldIndex` / `FieldValue` 为类型名。

---

## v0.10.0 — 2026-11-21

第十轮：**上下文贯通、可观测性、会话池与迁移差异报告**。五项 P0 优化一次落地，
根模块继续零第三方依赖；新增导出 API 全部遵循「首词动词」命名（构造函数 `New*` 除外）。

### 破坏性变更

- `Session.Close` 不再只是回滚事务：它会归零会话状态并归还引擎内部会话池；重复 `Close` 无副作用，
  归还后不得继续使用。直接 `session.NewSession` 构造（不经过引擎）的会话没有池，`Close` 等价于归零。
- `Exec` / `Query` / `QueryRow` 统一受会话超时约束（`SetTimeout`）；需要长查询时显式 `SetTimeout(0)`。
- 批量插入回填主键：PostgreSQL / SQLite 的多行 `INSERT` 自动追加 `RETURNING <自增主键>`，
  按行序回填模型并让 `LastInsertId` 返回最后一行主键；MySQL 无法逐行取回，保持不回填。
  冲突忽略 `INSERT(true, ...)` 与 `UPSERT` / `DUPLICATE` 路径仍不回填，避免冲突行不返回导致行序错位。
- `Migrate` 在补列/删列之外补建模型声明但数据库缺失的索引；列类型差异只输出日志并继续执行。
  方言不实现数据字典读取能力时跳过对应比对，不阻断迁移。

### 新增

- `Logger` / `QueryEvent` / `NewDefaultLogger`：可注入 SQL 观测器，事件包含会话 ID、耗时、SQL、参数、
  调试/慢标志；根包提供 `wxorm.Logger`、`wxorm.QueryEvent` 别名。
- `Engine.SetLogger` / `Engine.SetSlowThreshold` / `Session.SetLogger` / `Session.SetSlowThreshold`：
  调试模式记录全部语句，非调试模式只上报达到阈值的慢 SQL。
- `Engine.DiffTable(model)` + `TableDiff` / `ColumnDiff`：只读结构差异报告（列增删、类型变化、缺失索引）。
- `Session.GetTableColumnTypes` / `Session.GetTableIndexes` 与方言可选接口 `dialect.TableColumnTyper` /
  `dialect.TableIndexReader`；MySQL / PostgreSQL / SQLite 均已实现。
- `clause.BatchReturningInserter`：批量 `INSERT ... RETURNING` 的独立能力接口（PostgreSQL / SQLite 实现，
  MySQL 不实现），不改变 SQLite 单条仍走 `LastInsertId` 的既有语义。
- 会话池：`Session.SetID` / `SetReleaser` / `Reset`；引擎用 `sync.Pool` 复用已归零的会话，
  `Transact` 在提交/回滚后自动归还。

### 修复

- **P1 原生 SQL 接口不受超时约束**（#32）：`Exec` / `Query` / `QueryRow` 统一走会话 ctx。
- **P1 日志硬编码 `log.Printf`**（#59）：改为可注入 `Logger`，追加可配置的慢 SQL 阈值。
- **P1 批量插入不回填主键**（#60）：PostgreSQL / SQLite 按行回填，MySQL 明确不回填。
- **P1 `Migrate` 不检测索引与列类型变化**（#31 / #61）：新增 `DiffTable`，`Migrate` 自动补建索引，
  类型差异写日志提示。

### 测试

- 根模块：`gofmt -l .` 无输出、`go vet ./...` ok、`go test ./... -count=1` 全绿、`go test -race ./... -count=1` 全绿。
- 真库：`go run ./cmd/preparedb` 创建 MySQL 8.4 / PostgreSQL 17 / SQLite 三方言库后，
  `WXORM_IT_STRICT=1 go test ./... -count=1` 通过（6.99s），`-race` 通过（8.72s）；
  新增 `TestBatchInsertReturnsPrimaryKeys`（PG/SQLite 逐行回填、MySQL 明确不回填）与
  `TestMigrateDiffReport`（差异报告、缺失索引补建、迁移后差异为空）三方言均通过。
- 性能：PostgreSQL / SQLite 与 v0.9.0 基线持平（批量 200 行 PG 5.82ms vs 6.54ms、SQLite 4.79ms vs 3.91ms；
  SelectPage PG 0.437ms vs 0.639ms、SQLite 0.226ms vs 0.220ms）。MySQL 本环境单次查询被驱动层延迟 ACK
  放大到约 44ms，原始 `database/sql` 同 SQL 可复现（新连接首条 4ms、复用连接 43~45ms，与 wxorm 无关），
  因此该方言基准在本环境不具可比性，正确性验证不受影响。
- 新增单测：`migrate_test.go`、`session/pool_test.go`、`session/insert_test.go`、
  `TestBuildSQL_PostgresBatchReturning`、`TestSession_GetTableColumnTypes`、`TestSession_GetTableIndexes`、
  `TestMySQL_Logger`，以及 `fakedriver` 对 `CREATE INDEX` 与 `information_schema.statistics` 的支持。

### 命名

除构造函数 `New*` 外，本轮新增导出函数/方法首词均为动词：`DiffTable`、`IsEmpty`、`SetLogger`、
`SetSlowThreshold`、`GetTableColumnTypes`、`GetTableIndexes`、`SetID`、`SetReleaser`、`Reset`、`LogQuery`；
`ColumnDiff.String` 属于 `fmt.Stringer` 契约。

---

## v0.9.0 — 2026-11-21

第九轮：**命名策略与 P0 隐患收敛**。把「逻辑表名」与「物理表名」拆开，新增 `naming` 包支撑多租户/分库分表；
同时清理此前遗留的注入面、吞错与无界缓存。根模块继续保持零第三方依赖。

### 破坏性变更

- `Session.ExistsTable()` 由 `bool` 改为 `(bool, error)`：连接、权限或查询失败不再被当成「表不存在」，
  `Migrate` 也不再据此误判缺表后去建表。`sql.ErrNoRows` 仍视为「不存在」。
- `Dialect.BuildTableQuerySQL` / `BuildTableColumnsQuerySQL` 的第二个参数语义由「库名」改为
  `namespace`（MySQL 为库、PostgreSQL 为 schema、SQLite 忽略）；为空时回落到 `DATABASE()` /
  `current_schema()`，不再从 DSN 解析的库名兜底。
- 自定义 `Dialect` 实现需同步：三个方法的签名变化，并按方言选择 `?` / `$n` 占位符，库名与表名一律走参数。

### 新增

- **`naming` 包**：`Strategy{Schema, Prefix, Suffix}`，`IsZero` / `GetTableName` / `Qualify` / `Validate` /
  `String`。`Validate` 拒绝点号、引号、反引号、单引号、反斜杠、分号、零字节与空白，违例返回
  `naming.ErrInvalidSegment`，不会静默退化成逻辑表名。
- **`Engine.SetNaming` / `GetNaming`**：引擎级默认策略，`NewSession` 时下发给会话。
- **`Session.SetNaming` / `GetNaming` / `GetTableName` / `GetQualifiedTableName`**：会话级覆盖；
  `GetTableName` 返回物理表名，`GetQualifiedTableName` 返回带库/Schema 的限定名。
- **`wxorm.PanicError`**：`Transact` 内的 panic 被包装为 `*PanicError`，携带 `Value` 与 `Stack`，
  `Unwrap` 在 panic 值本身是 `error` 时返回它，使 `errors.Is` / `errors.As` 可穿透事务边界。
- **`dialect.TableCaseInsensitive`** 可选接口：MySQL / SQLite 返回 true，`ExistsTable` 用
  `strings.EqualFold` 比对 `information_schema` 返回的名字，解决 `lower_case_table_names=1` 下的误判。
- **`Session.namespace()`** 内部约定：只返回命名策略显式指定的 `Schema`，为空时由方言回落；
  不回退到连接串库名，避免把 PostgreSQL 的库（catalog）误当成 schema。

### 修复

- **P0 MySQL 列 DDL 列名未转义**：列名（含索引列）统一反引号引用，保留字或含特殊字符的列不再报 1064。
- **P0 元数据查询注入面**：`BuildTableQuerySQL` / `BuildTableColumnsQuerySQL` 全部参数化，表名含 `'`
  无法再闭合字面量。
- **P1 限定表名的索引名非法**：`tenant1.t_users` 原先拼出 `tenant1.t_users_name_idx`，被标识符引用拆成
  多段；新增 `dialect.indexNameBase` 取最后一个点之后的段，生成 `users_name_idx`。
- **P1 LIMIT/OFFSET 无界缓存**：以整数为键的 `limitCache` / `offsetCache` 在随机分页下无上限堆积，
  移除缓存改为 `clause.Context.WriteInt` 直接输出。
- **P1 方言注册表并发不安全**：`Register` / `Get` 共用普通 map，并发下触发运行时 fatal error；
  改为 `sync.RWMutex` 保护，并用 `normalizeDriverName`（`ToLower` + `TrimSpace`）归一化键。
- **P1 同一模型多前缀串预编译语句**：`schema` 只缓存逻辑表名与列集，物理表名相关的 INSERT 缓存键
  （`GetInsertCacheKey(table)` / `GetInsertIgnoreCacheKey(table)`）在会话层计算，同表不同前缀不共用语句。
- **P0 PostgreSQL 元数据查询漏判表已存在**：`namespace` 原先回退到连接串库名，PG 实际需要 `table_schema`
  匹配 `public`，导致建表后 `ExistsTable` 仍为 false、`Migrate` 重复建表；改为只认命名策略并交给方言回落。
  根模块单测（内存驱动）无法覆盖该语义，由真库集成测试暴露。

### 测试

- 新增 `session/naming_test.go`：前缀、库限定、插入隔离、非法策略、零值策略、库名来源共 6 个用例。
- `TestMySQL_Naming`：引擎级策略下发后建表与 `ExistsTable` 均使用物理表名。
- `TestMySQL_Transact_Panic` 强化：断言 `*PanicError`、`errors.Is(err, ErrPanicSentinel)`、栈非空。
- PostgreSQL / SQLite 元数据测试改写为断言参数化 SQL 与参数顺序，并验证注入串只出现在参数中。
- `TestMySQLBuildTableIndexSQLs` 增加限定表名断言，`schema.TestInsertCacheKey` 增加物理表名隔离断言。
- 根模块：`gofmt -l .` 无输出、`go vet ./...` ok、`go test ./... -count=1` 全部 ok。
- 真库集成：`cd harness/integration; go vet ./...; go test ./... -count=1 -race` → ok（MySQL / PostgreSQL / SQLite 三方言）。

---

## v0.8.0 — 2026-11-21

第八轮：**测试资产归入 harness，并落到真库**。参照 leego 的目录约定，把示例、集成测试与测试数据收拢到
`harness/` 下，根模块继续零依赖，测试数据只保留一份来源。

### 破坏性变更

- `examples` → `harness/examples`：导入路径变为 `gitee.com/leminewx/wxorm/harness/examples`；
  内存驱动路径变为 `.../harness/examples/fakedriver`。仓库内全部测试已同步。
- `integration/` → `harness/integration/`：子模块名改为 `gitee.com/leminewx/wxorm/harness/integration`，
  `replace` 改为 `=> ../..`。验收命令改为 `cd harness/integration`。

### 新增

- **`harness/testdata`**：`database.json` 保存 MySQL / PostgreSQL / SQLite 与内存驱动的连接参数，
  `accounts.json` 保存条件与分页用例共用的种子数据；SQLite 真库文件位于 `harness/testdata/sqlite/`（已忽略）。
- **`harness/assetpath`**：按源码位置与工作目录逐级定位 `harness/testdata`，兼容 `go test` 的包目录工作目录。
- **`harness/database`**：集中解析 `database.json` 并构造三方言 DSN；examples 与集成测试不再各自硬编码。
- **`harness/integration/cmd/preparedb`**：一条命令创建三个真库（`go run ./cmd/preparedb`），可重复执行。

### 行为变更

- 集成测试默认库从 `dev` 改为专用库 `wxorm_harness`；`WXORM_IT_MYSQL_DSN` / `WXORM_IT_PG_DSN` 仍可覆盖。
- SQLite 默认使用 `harness/testdata/sqlite/wxorm_harness.db`；把 `sqlite.file` 置空或设置
  `WXORM_IT_SQLITE_FILE` 可切换为临时库或自定义文件。

### 测试

- 真库创建：`cd harness/integration; go run ./cmd/preparedb` → mysql / postgres / sqlite 就绪。
- 根模块：`gofmt -l .`、`go vet ./...`、`go test ./... -count=1 -p 2` 全部 ok。
- 集成：`$env:WXORM_IT_STRICT = "1"; go test ./... -count=1` → ok（3.85s）；`-race` → ok（5.64s）。
- 基准：Insert 1 行 2.54/1.16/0.262 ms、200 行 19.4/7.44/3.84 ms、SelectPage 100 行 1.09/0.647/0.220 ms（MySQL/PG/SQLite）；
  PostgreSQL 200 行较 v0.7.0 单次采样高约 0.9 ms，执行路径未变，判断为容器调度噪声。

---

## v0.7.0 — 2026-11-21

第七轮：**真实数据库回归**。前六轮的验证地基是内存驱动，只能证明 wxorm 拼出的 SQL 自己能解析；
本轮把同一套用例搬到真实 MySQL 8.4.13、PostgreSQL 17.12 与 SQLite 3.54.1 上执行，修掉 6 个真实缺陷，
并建立三方言的性能基线。

### 新增

- **`harness/integration/` 独立子模块**：`module gitee.com/leminewx/wxorm/harness/integration` + `replace` 回根模块，
  引入 `go-sql-driver/mysql`、`jackc/pgx/v5`、`modernc.org/sqlite`；根模块 `go.mod` 仍为零第三方依赖。
- **三方言同一套用例**：DDL、CRUD、17 种条件、排序分页、事务、钩子、UPSERT、类型往返、迁移、
  分批边界、并发会话。环境变量 `WXORM_IT_MYSQL_DSN` / `WXORM_IT_PG_DSN` / `WXORM_IT_DB` / `WXORM_IT_STRICT`，
  连不上时默认跳过，`WXORM_IT_STRICT=1` 改为失败。
- **基准**：`BenchmarkInsertBatch`（1/20/200 行）与 `BenchmarkSelectPage`（100 行）。
- **`Dialect.BuildTableColumnsQuerySQL` + `Session.GetTableColumns`**：通过 `information_schema.columns` /
  `pragma_table_info` 读取列名，不再用 `SELECT *`。

### 修复

- **MySQL 列内联索引导致建表报 1064**（P0）：列定义里的 `INDEX` 非法；删除内联索引，
  MySQL 实现 `TableIndexer`，建表后逐条 `CREATE INDEX`。
- **命名复合索引重复创建**（P1）：建表语句与后置索引同名冲突；删除 `buildUnionIndexes`，
  命名与未命名索引统一由 `BuildTableIndexSQLs` 创建。
- **`updated` 列未由数据库生成**（P0）：插入零值时间，MySQL 严格模式报 1292 `'0000-00-00'`；
  三方言 `IsAutoInsert` 纳入 `Updated`，DDL 中 `created` / `updated` 均写 `DEFAULT CURRENT_TIMESTAMP`。
- **`COLUMNS` 未知列未本地校验**（P1）：改为返回 `ErrSelectColumnNotFound`，不再拿数据库的 1054。
- **PostgreSQL `RETURNING` 路径漏 `AfterInsert` 钩子**（P1）：补 `CallMethod(_HOOK_AFTER_INSERT, value)`。
- **PostgreSQL 第二次 `Migrate` 报 `cached plan must not change result type`**（P1）：
  pgx 缓存了 `SELECT *` 的计划；改用数据字典查列名，并给 `fakedriver` 补上对应分支。

### 行为变更（需注意）

- **MySQL 索引改为建表后创建**：`CreateTable` 会多执行若干 `CREATE INDEX`；
  未命名索引名为 `表名_字段名_idx`，命名索引沿用标签里的名字。
- **`created` / `updated` 在插入时由数据库填值**：手工传入非零时间仍会写入；
  UPDATE 时 MySQL 由 `ON UPDATE CURRENT_TIMESTAMP` 刷新，PG / SQLite 不会自动刷新。
- **能力矩阵**：MySQL 方言现在实现 `TableIndexer`。

### 测试

- 根模块：`gofmt -l .`、`go build ./...`、`go vet ./...` 无输出，`go test ./... -count=1` 全部 ok。
- 集成：`cd harness/integration; WXORM_IT_STRICT=1 go test ./... -count=1` 在 mysql / postgres / sqlite 三个子测试下全部 ok。

### 性能基线（本机 i5-14600KF，`-benchtime=1s`）

| 基准 | MySQL | PostgreSQL | SQLite |
| --- | --- | --- | --- |
| `InsertBatch/rows=1` | 2.52 ms/op | 1.16 ms/op | 0.263 ms/op |
| `InsertBatch/rows=20` | 5.90 ms/op | 1.54 ms/op | 0.543 ms/op |
| `InsertBatch/rows=200` | 19.8 ms/op | 6.54 ms/op | 3.91 ms/op |
| `SelectPage`（100 行） | 1.11 ms/op | 0.639 ms/op | 0.222 ms/op |

单条插入的毫秒级开销主要来自自动提交的持久化，与 ORM 无关；批量写入应优先用 `INSERT(false, rows...)`。

---

## v0.6.0 — 2026-11-20

第六轮：**函数/方法命名对齐 leego 约定**。leego 的可检查规则要求「首词必动词 / be 动词 / 情态动词」，
构造函数只能用 `New<类型>` 族，`With` 只留给返回 `Option` 的函数式选项；本轮把存量名称逐条对齐，
不保留 Deprecated 别名。

### 变更（不兼容）

导出 API：

- `session.New` → `session.NewSession`，`statement.New` → `statement.NewStatement`；
- `dialect/clause.NewWithContext` → `NewContextFrom`（与 `NewFrom` 族一致）；
- `column.FormatHumpToUnderline` → `FormatCamelToSnake`；
- `Session.First` → `Session.FindFirst`（`Find` = 按条件搜索首个匹配）；
- `Session.WithContext` → `Session.SetContext`（`With` 仅限函数式选项，且与 `GetContext` 对称）。

包内私有助手：

- 日志：`_debug` → `logDebug`（`Engine` / `Session` / `Stmt`）；
- 错误：`unsupportedFeature` → `newUnsupportedFeatureError`；
- 转换：`toInt64` / `toText` / `toSliceAny` → `convertToInt64` / `convertToText` / `convertToAnySlice`；
- 规范化：`normalJSONPath` → `normalizeJSONPath`；
- 装配：`andOr` → `buildAndOr`、`operatorSQL` → `buildOperatorSQL`、
  `postgresDatatype` → `buildPostgresDatatype`、`postgresFullTextQueryFunc` → `buildPostgresFullTextQueryFunc`；
- 集合：`difference` → `diffStrings`、`metadataGroups` → `groupMetadataColumns`、
  `primaryKeyColumns` → `filterPrimaryKeyColumns`、`foreignKeyColumns` → `groupForeignKeyColumns`、
  `sortedKeys` / `sortedMapKeys` → `sortKeys` / `sortMapKeys`；
- 查找：`autoPrimaryKeyColumn` → `findAutoPrimaryKeyColumn`、
  `inlinePrimaryKeyColumn` → `findInlinePrimaryKeyColumn`、`returningColumns` → `findReturningColumns`。

### 保留的例外

- SQL 关键字与查询 DSL：`INSERT` / `UPDATE` / `SELECT` / `WHERE` / `EQ` / `RAW` / `JSON_CONTAINS` 等；
- `sql.Result` 契约方法：`LastInsertId` / `RowsAffected`；
- `error`、`fmt.Stringer` 等接口方法，以及 `init`；
- 测试函数名（`Test*` / `Benchmark*`）本轮不动，只改因 API 改名而失效的调用点。

### 测试

- `gofmt -l .`、`go build ./...`、`go vet ./...` 均无输出；
- `go test ./... -count=1` 全部通过。

---

## v0.5.0 — 2026-11-20

第五轮：**SQLite 方言端到端落地**。没有改动 `session` / `statement` 的生产代码，只在 `dialect` 包内补齐三块：
类型映射、列 DDL、子句与建表；这也验证了前两轮抽出的「通用子句 + 可选能力接口」确实可以复制。

### 新增

- **SQLite 方言**（注册名 `sqlite` / `sqlite3`，共用同一实例）：
  - 类型映射收敛到五个存储类别：`INTEGER` / `REAL` / `TEXT` / `BLOB` / `NUMERIC`；
    `time.Time` → `DATETIME`（驱动据此把结果扫描回 `time.Time`），`bool` → `INTEGER`，JSON → `TEXT`；
  - 列 DDL：自增主键内联为 `INTEGER PRIMARY KEY AUTOINCREMENT`，`created` 列带
    `DEFAULT CURRENT_TIMESTAMP`，生成列默认 `VIRTUAL`；
  - 建表：复合主键 + 自增直接报错；索引用 `dialect.TableIndexer` 在建表后逐条 `CREATE INDEX`；
  - 子句：`?` 占位符、双引号标识符、`INSERT OR IGNORE`、`REPLACE INTO`、
    `ON CONFLICT (...) DO UPDATE SET ... excluded."c"`、`json_extract` / `json_each`；
  - 存在性查询走 `sqlite_master`，主键回填用 `LastInsertId()`。
- **`clause.SQLiteClause`**：`Clause` + `Replacer` + `ConflictUpserter` + `JSONQuerier`。
  刻意不包含 `Upserter`（`DUPLICATE` 的冲突目标语义不成立）、`ReturningInserter`（`LastInsertId` 已足够）、
  `FullTextSearcher`（需要 FTS5 虚拟表）。
- **`column.BaseColumn.IsAutoIncrement()` / `column.SQLiteColumn.IsInlinePrimaryKey()`**：
  让方言在不扩展 `Column` 接口的前提下探知「自增」与「已内联主键」。
- **`extractSQLiteDBName`**：支持普通路径、`file:` URI、`:memory:`，并按 Windows / Unix 两种
  分隔符取文件名。
- **新增错误**：`dialect.ErrSQLiteCompositeAutoPrimaryKey`（复合主键里出现自增列）、
  `column.ErrAutoIncrementRequiresPrimaryKey`（`auto` 未标在 `pk` 上）。

### 行为对照

| 调用 | MySQL | PostgreSQL | SQLite |
| --- | --- | --- | --- |
| `INSERT(true, ...)` | `INSERT IGNORE` | `ON CONFLICT DO NOTHING` | `INSERT OR IGNORE` |
| `Session.UPSERT("name")` | `ON DUPLICATE KEY UPDATE` | `ON CONFLICT (主键) DO UPDATE SET` | 同 PostgreSQL |
| `Session.DUPLICATE("name")` | 同上 | `ErrUnsupportedFeature` | `ErrUnsupportedFeature` |
| `where.JSON_CONTAINS(...)` | `JSON_CONTAINS` | `@> $1::jsonb` | `EXISTS (SELECT 1 FROM json_each(...))` |
| `where.FULLTEXT(...)` | `MATCH ... AGAINST` | `to_tsvector @@ ...` | `ErrUnsupportedFeature` |
| `LastInsertId()` | 可用 | 空（走 `RETURNING`） | 可用 |

### 已知边界

- SQLite 的外键是连接级开关（默认关），需要 DSN 参数或 `PRAGMA foreign_keys = ON`；
- 没有 `ON UPDATE CURRENT_TIMESTAMP`，`updated` 标记不生效；
- 没有表/列注释，`comment(...)` 被忽略；
- `Migrate` 给已有表补带 `CURRENT_TIMESTAMP` 默认值的列会失败（`ALTER TABLE ADD COLUMN` 只接受常量）；
- `uint64` 超过 `MaxInt64` 会溢出（SQLite 只有 8 字节有符号整型）。

### 测试

- 新增 `dialect/datatype/sqlite_test.go`、`dialect/column/sqlite_test.go`、`dialect/clause/sqlite_test.go`、
  `dialect/sqlite_test.go`、`statement/sqlite_test.go`，并在 `wxorm_test.go` 补 `TestSQLite_ExtractDBName`；
  `dialect/postgresql_test.go` 的能力矩阵同时断言三个方言的接口实现情况。
- 项目仍为零第三方依赖，模块内没有 SQLite 驱动，因此测试只验证 SQL 文本与能力断言；
  真实驱动需要使用者自行引入。
- `go test ./... -count=1` 全部 ok；`gofmt -l .`、`go build ./...`、`go vet ./...` 均无输出。

---

## v0.4.0 — 2026-11-20

第四轮：**把跨方言语义歧义彻底移除**。v0.3.0 虽然引入了 `UPSERT`，但 `DUPLICATE` 在 PostgreSQL 下
仍会把「更新列」当成「冲突目标」——能编译、能生成 SQL，只在执行时报「唯一约束不匹配」。
本版把 `DUPLICATE` 标记为 MySQL 专用，宁可在第一次执行就明确指出错误。

### 变更（不兼容）

- **`DUPLICATE` 仅 MySQL 可用**：PostgreSQL 方言不再实现 `clause.Upserter`，
  `statement.DUPLICATE` / `Session.DUPLICATE` 在 PG 下返回
  `clause.ErrUnsupportedFeature`，错误信息为
  `DUPLICATE (MySQL only), use UPSERT instead`。
- **`PostgresClause` 接口移除 `Upserter`**：自定义 PostgreSQL 方言如果依赖过
  `BuildDuplicateSQL`，需要改为实现 `ConflictUpserter.BuildUpsertSQL(ctx, conflict, update)`。
- **新增 `statement.ErrConflictTargetRequired`**：方言需要显式冲突目标但模型没有主键时返回，
  不再退化成 `DO NOTHING`（那会让调用方误以为数据被更新）。

### 新增

- `dialect.Clause` 的能力矩阵测试新增 `ConflictUpserter` 列；新增
  `TestPostgresDoesNotImplementUpserter` 作为反向断言，防止后续实现回退到「PG 也能 DUPLICATE」。

### 行为对照

| 调用 | MySQL | PostgreSQL |
| --- | --- | --- |
| `Session.UPSERT("name")` | `ON DUPLICATE KEY UPDATE \`name\` = VALUES(\`name\`)` | `ON CONFLICT (主键) DO UPDATE SET "name" = EXCLUDED."name"` |
| `Session.DUPLICATE("name")` | 同上 | `clause.ErrUnsupportedFeature` |
| `UPSERT` + 模型无主键 | 同上（MySQL 不需要目标） | `statement.ErrConflictTargetRequired` |

### 测试

- `go test ./... -count=1` 全部 ok；`go test -race ./session ./schema ./statement ./dialect/clause` 全部 ok。
- `gofmt -l .`、`go build ./...`、`go vet ./...` 均无输出。

---

## v0.3.0 — 2026-11-20

第三轮：**方言能力分层 + PostgreSQL 端到端可用 + Go 1.27 现代化**。
主线不是继续找 bug，而是回答三个问题：换方言要改多少代码、两个主流数据库场景能不能真跑、对标 GORM 还差什么。

### 新增

- **方言能力分层**：`clause.Clause` 只保留 18 个通用子句方法，其余能力拆为可选接口
  `Replacer` / `Upserter` / `ConflictUpserter` / `IgnoreInserter` / `ReturningInserter` /
  `JSONQuerier` / `FullTextSearcher`。缺失能力统一返回 `clause.ErrUnsupportedFeature`。
  聚合接口 `MySQLClause`、`PostgresClause` 保留，便于测试与渐进迁移。
- **PostgreSQL 端到端可用**：
  - 类型映射：`NUMERIC(20)` 承接 `uint64`、`TIMESTAMPTZ`、`INTERVAL`、`JSONB`、`BYTEA`、`INET`；
  - 列 DDL：`SERIAL` / `BIGSERIAL`、`GENERATED ALWAYS AS (...) STORED`、忽略 MySQL 专有片段；
  - 建表：无 `ENGINE`/`CHARSET`，索引与 `COMMENT ON` 作为后置语句（新增 `dialect.TableIndexer`、
    `dialect.TableCommenter` 两个扩展点）；
  - 存在性查询：`information_schema` + `table_schema = current_schema()`；
  - 占位符 `$n`、`ILIKE`、`ON CONFLICT`、`RETURNING`；
  - 注册名 `postgres` / `postgresql` / `pgx` 指向同一实例，共享 schema 缓存桶。
- **主键回填（PostgreSQL）**：单条 `INSERT` 自动追加 `RETURNING <自增主键>`，用 `QueryRowContext` 取回并
  reflect 回写模型；`result.LastInsertId()` 与 MySQL 语义对齐。`ON CONFLICT DO NOTHING` 命中时按 0 行成功处理。
- **`Session.UPSERT(update ...string)`**：冲突目标固定为模型主键，MySQL 退化为
  `ON DUPLICATE KEY UPDATE`，PostgreSQL 生成 `ON CONFLICT (主键) DO UPDATE SET`。
- **`statement.UPSERT(conflict, update...)`** 子句，以及 `schema.Schema.PrimaryKeyNames`。
- **`where.JSON_CONTAINS` / `where.FULLTEXT`**：把方言已有的 JSON/全文能力暴露到查询 API。
- **`where.RAW` 占位符改写**：统一用 `?` 书写，PostgreSQL 下自动改写成 `$n`。

### 修复

- **`generated(...)` 表达式被 `strings.ToUpper` 改写**（P1）：小写标识符/函数名被破坏，建表报错。
- **MySQL 列 DDL 忽略 `Generated`**（P1）：声明的生成列静默丢失；现生成
  `GENERATED ALWAYS AS (...) VIRTUAL`。
- **`column('name')` / `index('x')` / `unique('x')` 引号残留**（P1）：解析时未裁剪引号，
  生成 `user_'name'_idx` 这类非法标识符。
- **MySQL 类型映射**（P2）：`unsafe.Sizeof(uintptr(0))` → `strconv.IntSize`；删除 `rune → CHAR(1)` 的
  错误映射（会截断中文）；未知类型由 `panic` 改为降级为 `LONGTEXT`。

### 变更（需注意）

- **`Clause` 接口拆分**：自定义方言若只实现了旧的胖接口，需要按能力实现可选接口；
  否则相关子句会返回 `ErrUnsupportedFeature`（而不是静默生成 SQL）。
- **`DUPLICATE` 在 PostgreSQL 下的语义已文档化为「冲突目标 = 更新列」**：跨方言代码请改用 `UPSERT`。
- **PostgreSQL 的 `LIKE` 走 `ILIKE`**：与 MySQL 默认大小写不敏感的排序规则对齐；
  需要区分大小写时请用 `where.RAW`。

### Go 1.27 现代化

- `reflect.TypeFor[T]()` 取代 `reflect.TypeOf(T{})`；
- `slices.Sorted(maps.Keys(m))` 取代手写排序；
- 内置 `min` 取代手写三元判断；
- `strconv.IntSize` 取代 `unsafe`。

### 测试

- 新增/重写：`dialect/clause/postgresql_test.go`（11 个顶层用例，含多个子用例）、`dialect/postgresql_test.go`、
  `dialect/column/postgresql_test.go`、`dialect/datatype/postgresql_test.go`、
  `statement/postgresql_test.go`、`statement/where/search_condition_test.go`。
- 覆盖点：占位符起始序号、UPSERT 子句顺序、冲突目标与更新列分离、能力缺失报错、
  建表/索引/注释、information_schema、JSON 两种写法、全文检索模式映射、RAW 占位符改写。
- `go build ./...`、`go vet ./...`、`gofmt -l .` 均无输出；`go test ./... -count=1` 与
  `go test -race` 全部 ok。

### 基准（本机 i5-14600KF）

| 基准 | 数值 |
| --- | --- |
| `Schema_Parse`（命中缓存） | 46.0 ns / 0 B / 0 allocs |
| `GetReflectFieldValue` | 22.5 ns / 24 B / 1 alloc |
| `Session_InsertOne`（含内存驱动） | 3281 ns / 1775 B / 38 allocs |
| `Session_UpdateStruct`（含内存驱动） | 188.9 µs / 3275 allocs |

---

## v0.2.0 — 2026-11-20

第二轮：补上「正确性、安全、性能」三条线。重点是修掉 3 个会**静默给出错误结果**的 P0，
并用基准把 ORM 自身开销与驱动开销分开。

### 新增

- `Session.FindFirst(dest any)`：与 `SELECT` 不同，查不到时返回 `ErrRecordNotFound`，
  让「没数据」与「查询失败」可区分。
- `Session.Exist() (bool, error)`：生成 `SELECT 1 FROM ... LIMIT 1`，不统计全表。
- `Session.COLUMNS(columns ...string) *Session`：指定查询列，结果按**返回的列名**映射回模型字段；
  未知列返回 `ErrSelectColumnNotFound`。
- `dialect.Lookup(name) (Dialect, error)`：未注册方言返回 `ErrUnsupportedDriver`，不再返回 `nil`。
- 错误值：`ErrRecordNotFound`、`ErrSelectColumnNotFound`、`dialect.ErrUnsupportedDriver`、
  `clause.ErrInvalidFullTextMode`。
- `schema.Schema` 新增 `ColumnIndexesByName`、`InsertCacheKey`、`InsertIgnoreCacheKey`。
- `statement.EXISTS(table)` 子句。
- 基准：`session/bench_test.go`（SELECT / COLUMNS / First / Exist / INSERT / UPDATE）
  与 `BenchmarkGetReflectFieldValue`，结果记入 `docs/analysis.md` 第 5 节。

### 修复（P0）

- **INSERT 预编译串味**：缓存键原本只有「表名 + ignore 标志」。同一张表对应两个列集不同的模型时，
  会共用同一条预编译语句，参数按第一个模型的列顺序绑定——写错列且不报错。
  现在键由 `schema` 预计算，包含可插入列集。
- **JSON 路径注入**：路径被直接拼进 SQL 字符串字面量，含 `'` 即可闭合字面量。
  新增 `quoteJSONPath` 转义单引号。
- **全文检索模式注入**：`IN BOOLEAN MODE` 等模式原本原样拼接。
  新增白名单 `normalizeFullTextMode`（去空白 + 转大写 + 5 种合法值），其余报错。

### 修复（P1 / P2）

- `NewEngine`：先校验方言再连接；`Ping` 失败时关闭连接池，不再泄漏。
- `Migrate` 的 `ADD COLUMN`：复用列定义生成器，保留 `NOT NULL` / `DEFAULT` / `COMMENT`。
- `SetTimeout(0)`：关闭会话默认超时；新增 `execContext()`，无超时时直接复用会话 ctx，
  不再每条 SQL 都建一个 timer。
- `GetReflectFieldValue`：零值判断改用 `reflect.Value.IsZero`，取代 `reflect.DeepEqual`，
  `time.Time` 等结构体不再深度比较。
- 补齐 `session/result` 与 `dialect` 的空测试文件；新增 5 个测试文件覆盖上述修复。

### 变更（需注意）

- **`SetTimeout` 语义**：现在接受 `0`（关闭默认超时）与小于 1s 的值，负数忽略。
  原先小于等于 1s 会被静默忽略，现在会生效——传过小值的调用方会看到更严格的超时。
- **`Migrate` 生成的 DDL**：新增列时带上完整列定义，列注释、默认值、非空约束会一并写入。
- **方言名归一**：`dialect.Register` / `Get` 现在 `TrimSpace + ToLower`，
  `Get("MySQL")` 也能命中；`Get` 仍未注册时返回 `nil`，建议改用 `Lookup`。

### 性能

同机对比（`-benchmem`，优化前 → 优化后）：

| 基准 | 优化前 | 优化后 |
| --- | --- | --- |
| `Statement_INSERT` | 423.5 ns / 602 B | 347.4 ns / 579 B（−18%） |
| `Session_Insert-1` | 3219 ns / 40 allocs | 2207 ns / 39 allocs（−31%） |
| `Session_Insert-10` | 7488 ns | 5338 ns（−29%） |
| `Session_Insert-100` | 1.10 ms | 0.79 ms（−28%） |
| `GetReflectFieldValue` | `reflect.DeepEqual` | 18.0 ns / 24 B / 1 alloc |

注意：会话级基准里 allocs 几乎不变，因为大头是内存驱动解析 SQL（约 132 次/行），
不代表 ORM 没有优化。看 ORM 自身开销请看 `Statement_*` / `Schema_*`。

### 测试

- `go test ./... -count=1`：12 个包全部 ok（含新增 5 个测试文件）。
- `go test -race -count=1 . ./session ./schema ./dialect/clause ./statement`：全部 ok。
- `go vet ./...`、`go build ./...`、`gofmt -l .` 均无输出。

---

## v0.1.0 — 2026-11-20

首个可验证、可回归的版本。重点是**把「静默出错」的路径修掉**，并让测试脱离真实数据库。

### 新增

- `harness/examples/fakedriver`：`database/sql/driver` 的内存实现，注册名为 `mysql`。
  覆盖 CREATE / ALTER / DROP TABLE、INSERT（含 `IGNORE`）、SELECT（含 `COUNT` / `DISTINCT` / `GROUP BY` / `ORDER BY` / `LIMIT` / `OFFSET`）、
  UPDATE、DELETE、`information_schema.tables` 查询，以及事务提交/回滚与单列唯一约束。
- `harness/examples/fakedriver/fakedriver_test.go`：驱动自身的冒烟测试，防止测试地基失真。
- `main_test.go`、`session/main_test.go`：在 `TestMain` 中注册内存驱动。
- `Session.SetContext` / `Session.GetContext`：会话级上下文，SQL 执行可被取消与超时约束。
- 错误值 `ErrValuesIsEmpty`、`ErrOffsetRequiresLimit`、`ErrTableNoPK`、`ErrTablePKValuesZero`、`ErrMissingWhere`。
- 文档：`docs/analysis.md`（架构分析 + 缺陷台账）、`docs/roadmap.md`（分阶段计划 + 验收标准）。

### 修复（P0）

- SQL 生成：
  - `QuoteIdentifier` 正确处理含空格的名字，并保留 `DISTINCT` / `ASC` / `DESC` 修饰符。
  - 修掉四处缓存键碰撞：`SELECT` 列集、`INSERT` 列列表、`INSERT ... ON DUPLICATE KEY`、JSON 函数路径。
  - `BuildValuesSQL` 空输入不再 panic。
  - `BuildFullTextSearchSQL` 补回被丢弃的搜索词参数。
  - 外键 `ON UPDATE` 不再错写成 `ON DELETE CASCADE`。
  - `comment('unique')` 等带参数标记不再误触发 `UNIQUE` / `NOT NULL`。
- 模型与缓存：
  - schema 缓存键改为 `{driver, reflect.Type}`，消除同名局部类型互相污染。
  - 钩子统一使用会话当前模型，不再读取旧字段导致空指针。
- 会话时序：
  - 事务内不再复用 `*sql.DB` 的预编译语句，杜绝数据逃逸事务。
  - 每个 CRUD 入口 `defer statement.Reset()`，语句残留不再污染下一条 SQL。
  - 查询结果对 `args` 做防御性拷贝，历史结果不会被后续构建覆盖。
  - `New` 接受 `nil` 语句缓存。
- 迁移：
  - `Migrate` 不再用事务包裹 DDL；删除字段改为逐列 `ALTER TABLE ... DROP COLUMN`，不再重建整表。
  - 正确检查 `Rows.Columns()` 的错误并关闭结果集。
  - `extractDBName` 取最后一个 `/` 之后的部分，密码含 `/` 不再错位，空库名直接报错。

### 变更（不兼容）

- `examples.SetupTestDB(t testing.TB) *sql.DB` → `SetupTestDB() (*sql.DB, error)`。
  示例包不再依赖 `testing`，把错误处理交回调用方。
- 错误前缀由 `wxormxxx:` 统一为 `wxorm:`，依赖字符串匹配错误信息的调用方需要调整。
- `Migrate` 删除字段由「重建表」改为 `DROP COLUMN`：依赖重建表顺带清理索引/约束的调用方需要显式处理。

### 测试

- `go test ./... -count=1` 从 **46 个 FAIL** 变为 **11 个包全部 ok**。
- `go build ./...`、`go vet ./...`、`gofmt -l .` 均无输出/退出码 0。
- 测试不再需要 MySQL 服务与网络。

### 已知问题

见 [ISSUES.md](ISSUES.md) 的「待修复」：方言空实现、会话并发契约、`Migrate` 语义范围、
上下文未贯通到原生 SQL 接口、`session/result` 缺测试、无 CI 与基准基线。
