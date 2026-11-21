# ISSUES

问题记录与状态跟踪。详细分析见 [docs/analysis.md](docs/analysis.md)，修复计划见 [docs/roadmap.md](docs/roadmap.md)。

状态：`已修复` / `待修复` / `不修复（已记录）`；当前版本 `v1.0.0.0`（正式发布，公开 API 已冻结）。

## 已修复

| ID | 优先级 | 问题 | 影响 | 位置 |
| --- | --- | --- | --- | --- |
| #01 | P0 | `QuoteIdentifier` 对含空格的名字原样返回 | 标识符未引用，存在注入面 | `dialect/clause/mysql.go` |
| #02 | P0 | `BuildSelectSQL` 仅按表名缓存 | 不同列集共用一条 SQL，`SELECT` 退化为 `SELECT *` | `dialect/clause/mysql.go` |
| #03 | P0 | `BuildInsertSQL` 缓存键缺列列表；`BuildDuplicateSQL` 列名无分隔符拼接 | 不同语句互相覆盖 | `dialect/clause/mysql.go` |
| #04 | P0 | JSON 子句缓存键无分隔符、路径未规范化 | 命中错误 JSON 函数、路径非法 | `dialect/clause/mysql.go` |
| #05 | P0 | `BuildValuesSQL` 空切片 `values[0]` panic | 进程崩溃 | `dialect/clause/mysql.go` |
| #06 | P0 | `BuildFullTextSearchSQL` 丢弃搜索词 | 全文检索变成全表匹配 | `dialect/clause/mysql.go` |
| #07 | P0 | 外键 `ON UPDATE` 分支生成 `ON DELETE CASCADE` | 级联语义与声明不符 | `dialect/mysql.go` |
| #08 | P0 | `comment('unique')` 等参数型标记误触发 `UNIQUE` / `NOT NULL` | 建表多出约束 | `dialect/column/parser.go` |
| #09 | P0 | schema 缓存以 `Type.String()` 为键 | 函数内同名局部类型互相污染 | `schema/cache.go` |
| #10 | P0 | 钩子读取 `own.schema.Model` | 切换模型后钩子作用于旧模型；nil 时 panic | `session/hooks.go` |
| #11 | P0 | 事务内复用 `*sql.DB` 预编译语句 | 语句逃逸事务，回滚不生效 | `session/record.go` |
| #12 | P0 | `Session` 缺少 `ctx` | `context.WithTimeout(nil)` panic | `session/session.go` |
| #13 | P0 | 构造器不清理 `statement` | 上一条 `WHERE` 泄漏到下一条语句 | `session/record.go` |
| #14 | P0 | 结果对象直接持有 `args` 切片 | 历史结果被后续构建覆盖 | `session/result/*.go` |
| #15 | P0 | `Migrate` 用事务包 DDL、删字段重建整表 | 索引/约束/自增丢失，数据风险 | `wxorm.go` |
| #16 | P0 | `Migrate` 忽略 `Rows.Columns()` 错误 | 失败后继续用空列集 | `wxorm.go` |
| #17 | P0 | `extractDBName` 用 `Split("/")[1]` | 密码含 `/` 时解析错位，空库名不报错 | `wxorm.go` |
| #18 | P0 | `New` 接受 `nil` 语句缓存 | `sync.Map` 空指针 | `session/session.go` |
| #19 | P1 | `BuildSetSQL` 依赖 `map` 遍历顺序 | SQL 文本不稳定，预编译缓存命中率低 | `dialect/clause/mysql.go` |
| #20 | P1 | DDL 索引/唯一/外键遍历 `map` | 生成的 DDL 每次不同 | `dialect/mysql.go` |
| #21 | P1 | `updateByStruct` 主键为零值时静默返回 | 调用方以为更新成功 | `session/record.go` |
| #22 | P1 | `ensureWhereClause` 不区分无主键与主键零值 | 错误信息误导排查方向 | `session/record.go` |
| #23 | P1 | `OFFSET` 无 `LIMIT` | 生成 MySQL 非法 SQL | `session/record.go` |
| #24 | P1 | `if own.statement.Reset(); err != nil` 恒假 | 错误分支永远不会执行 | `session/record.go` |
| #25 | P1 | 错误前缀写成 `wxormxxx:` | 错误信息不一致 | `dialect/*`、`schema/*` |
| #26 | P1 | 手写自旋锁 `Engine.lock` | 计数溢出后永不自增 | `wxorm.go` |
| #27 | P1 | 测试依赖内网 MySQL，且 `go.mod` 无驱动 | 任何环境都无法跑测试 | `harness/examples/model.go`、`*_test.go` |
| #28 | P2 | `examples` 包 `import "testing"` | 测试依赖进入生产构建 | `harness/examples/model.go` |
| #29 | P1 | 未注册方言仍可被 `Get` 取到，返回 `nil` 或半成品实现 | 新增 `dialect.Lookup` 返回 `ErrUnsupportedDriver`；`Register` / `Get` 对名字归一化 | `dialect/dialect.go` |
| #33 | P2 | `session/result` 三个测试文件为空 | 补齐 `NewResult` / `NewSelectResult` / `NewBatchResult` / `copyArgs` 语义测试 | `session/result/result_test.go` |
| #35 | P2 | 无基准基线 | 新增 4 层共 11 个基准，数据写入 `docs/analysis.md` 第 5 节 | `session/bench_test.go`、`schema/zero_test.go` |
| #39 | P0 | INSERT 预编译缓存键只有「表名 + ignore 标志」 | 同一张表两个模型共用一条语句，参数按错误列顺序绑定 | `session/record.go`、`schema/schema.go` |
| #40 | P0 | JSON 路径直接拼进 SQL 字符串字面量 | 路径含 `'` 可闭合字面量追加任意 SQL | `dialect/clause/mysql.go` |
| #41 | P0 | 全文检索模式原样拼接进 SQL | 同上的注入面 | `dialect/clause/mysql.go` |
| #42 | P1 | `NewEngine` 在 `Ping`/方言解析失败时不关连接池 | 连接泄漏 | `wxorm.go` |
| #43 | P1 | `Migrate` 的 `ADD COLUMN` 只写列名与类型 | 丢 `NOT NULL` / `DEFAULT` / `COMMENT` | `wxorm.go` |
| #44 | P1 | 默认超时 10s 无法关闭，且每条 SQL 都建 timer | 长查询无法豁免；短查询多一次分配 | `session/session.go` |
| #45 | P1 | `GetReflectFieldValue` 用 `reflect.DeepEqual` 判零值 | `time.Time` 等结构体每次深度比较 | `schema/schema.go` |
| #46 | P2 | `stringsBuilderPool` 取出的 builder 已 `Reset` | 池无收益，只留同步开销 | `dialect/clause/clause.go` |
| #47 | P2 | 缺少 `First` / `Exist` / `COLUMNS` | 调用方只能 `SELECT` 后自己判空、用 `COUNT` 判存在、无法指定列 | `session/record.go`、`statement/clause.go` |
| #49 | P0 | PostgreSQL 方言四个文件是注释/空实现 | `Get("postgres")` 后执行会得到残缺 SQL，等于「注册了不能用」 | `dialect/postgresql.go`、`clause/postgresql.go`、`column/postgresql.go`、`datatype/postgresql.go` |
| #50 | P0 | PostgreSQL 的 `INSERT IGNORE` 被内联进 INSERT | PostgreSQL 无此语法，直接报错；冲突忽略必须在 VALUES 之后 | `dialect/clause/postgresql.go`、`session/record.go` |
| #51 | P0 | PostgreSQL 插入后拿不到自增主键 | `LastInsertId` 在 PG 不可用，业务拿不到新 ID | `session/insert.go`、`schema/schema.go` |
| #52 | P1 | `DUPLICATE` 跨方言语义不一致 | MySQL 的「更新列」在 PG 被当成「冲突目标」，搬迁代码报 `no unique constraint` | `statement/clause.go`、`dialect/clause/postgresql.go`、`session/record.go` |
| #53 | P1 | `generated(...)` 表达式被 `strings.ToUpper` 改写 | 列名/函数名大小写被改，建表报错 | `dialect/column/parser.go` |
| #54 | P1 | MySQL 列 DDL 完全忽略 `Generated` | 声明的生成列在建表时静默丢失 | `dialect/column/mysql.go` |
| #55 | P1 | `column('name')` / `index('x')` 引号残留 | 生成 `"user_'name'_idx"` 这类非法标识符 | `dialect/column/parser.go` |
| #56 | P2 | int 位宽用 `unsafe.Sizeof` 判断；`rune` 误映射 `CHAR(1)`；未知类型 `panic` | 跨平台行为不一致；中文单字被截断；进程崩溃 | `dialect/datatype/mysql.go` |
| #57 | P2 | 方言能力挤在单一 `Clause` 接口 | 新方言必须为空能力写空实现，无法表达「不支持」 | `dialect/clause/clause.go` |
| #58 | P2 | DSN 只支持 MySQL 格式 | `postgres://` 与 libpq keyword DSN 无法解析库名 | `wxorm.go` |
| #64 | P0 | `DUPLICATE` 在 PostgreSQL 下静默生成错位的冲突目标 | 「能编译、能出 SQL，执行才报唯一约束不匹配」；现 PG 不再实现 `Upserter`，返回 `ErrUnsupportedFeature` 并指引用 `UPSERT` | `clause/postgresql.go`、`statement/clause.go` |
| #65 | P1 | `UPSERT` 无冲突目标时无明确错误 | 模型无主键会让 PG 报模糊错误；新增 `statement.ErrConflictTargetRequired` | `statement/clause.go`、`session/record.go` |
| #66 | P1 | `dialect/column/sqlite.go`、`datatype/sqlite.go` 是注释桩，SQLite 无法使用 | `Get("sqlite")` 后建表/读写没有可用实现；现补齐类型映射、列 DDL、子句与建表（自增主键内联、索引后置） | `dialect/sqlite.go`、`dialect/clause/sqlite.go`、`dialect/column/sqlite.go`、`dialect/datatype/sqlite.go` |
| #67 | P2 | 自增主键原先只能以表级 `PRIMARY KEY` 声明 | SQLite 的自增必须内联为 `INTEGER PRIMARY KEY AUTOINCREMENT`；新增 `column.BaseColumn.IsAutoIncrement()` + `IsInlinePrimaryKey()` 供方言探测 | `dialect/column/column.go`、`dialect/sqlite.go` |
| #68 | P2 | `extractDBName` 只识别 MySQL / PostgreSQL DSN | 以 `sqlite` / `sqlite3` 建引擎时报「unsupported database driver」；新增 `extractSQLiteDBName`（支持路径、`file:` URI、`:memory:`） | `wxorm.go` |
| #69 | P0 | MySQL 列定义生成内联 `INDEX` | 列里的 `KEY` 是主键同义词，建表直接报 1064；删除内联索引，MySQL 改为 `TableIndexer` 后置 `CREATE INDEX` | `dialect/column/mysql.go`、`dialect/mysql.go` |
| #70 | P0 | `updated` 列不被视为数据库生成 | 插入零值时间，MySQL 严格模式报 1292 `'0000-00-00'`；三方言 `IsAutoInsert` 纳入 `Updated`，DDL 写 `DEFAULT CURRENT_TIMESTAMP` | `dialect/column/{mysql,postgresql,sqlite}.go` |
| #71 | P1 | `COLUMNS` 未知列未本地校验 | 把未知列发给数据库，返回方言的 1054 而非 `ErrSelectColumnNotFound` | `session/record.go` |
| #72 | P1 | PostgreSQL `RETURNING` 路径提前返回 | 插入成功但 `AfterInsert` 钩子不触发；RETURNING 后补 `CallMethod` | `session/record.go` |
| #73 | P1 | PostgreSQL 第二次 `Migrate` 报 `cached plan must not change result type` | `SELECT *` 的执行计划被 pgx 缓存；改用 `information_schema.columns` / `pragma_table_info` 查列名 | `dialect/{mysql,postgresql,sqlite}.go`、`session/table.go`、`wxorm.go` |
| #74 | P1 | MySQL 命名复合索引在建表语句与后置语句中重复创建 | 同名索引第二次创建报冲突；删除 `buildUnionIndexes`，索引统一由 `BuildTableIndexSQLs` 创建 | `dialect/mysql.go` |
| #75 | P0 | MySQL 列名与索引列名未做反引号转义 | 列名是保留字（如 `order` / `key`）或含特殊字符时建表/索引语句报 1064 | `dialect/column/mysql.go`、`dialect/mysql.go` |
| #76 | P0 | 元数据查询把库名与表名直接拼进 SQL 字面量 | 表名含 `'` 可闭合字面量；改为参数化（MySQL `?`、PG `$n`、SQLite `?`），方言统一走 `BuildTableQuerySQL(ctx, namespace, table)` | `dialect/{mysql,postgresql,sqlite}.go`、`session/table.go` |
| #77 | P0 | 物理表名与逻辑表名混为一谈，无法按租户/环境加前缀 | 新增 `naming.Strategy`（`Schema` / `Prefix` / `Suffix`）与 `Engine.SetNaming` / `Session.SetNaming`；schema 缓存只存逻辑表名，物理表名、限定名与预编译缓存键在会话层计算，同模型多前缀互不污染 | `naming/`、`session/{session,table,record}.go`、`wxorm.go` |
| #78 | P0 | `ExistsTable` 只返回 `bool`，查询失败被当成「表不存在」 | 连接/权限错误会让 `Migrate` 误判缺表并去建表；签名改为 `(bool, error)`，`sql.ErrNoRows` 视为不存在，大小写不敏感方言（MySQL/SQLite）用 `EqualFold` 比对 | `session/table.go`、`dialect/dialect.go`、`wxorm.go` |
| #79 | P1 | `Transact` 内 panic 被转成普通 error | `recover` 后丢失 panic 值与调用栈，`errors.Is/As` 无法穿透；新增 `*PanicError`（`Value`/`Stack`/`Unwrap`） | `wxorm.go` |
| #80 | P1 | LIMIT/OFFSET 以整数为键的无界缓存；方言注册表无锁 | 随机分页持续堆积缓存；并发 `Register`/`Get` 触发 map 并发读写 fatal error。移除 LIMIT/OFFSET 缓存（改 `ctx.WriteInt`），注册表加 `RWMutex` 并归一化方言名 | `dialect/clause/{mysql,postgresql,sqlite}.go`、`dialect/dialect.go` |
| #81 | P1 | 限定表名直接拼进索引名 | `tenant1.t_users` 生成 `tenant1.t_users_name_idx`，被标识符引用拆成多段成为非法索引名；新增 `indexNameBase` 取最后一个点之后的段 | `dialect/metadata.go`、`dialect/{mysql,postgresql,sqlite}.go` |
| #31 | P1 | `Migrate` 不检测类型/索引变化 | 结构漂移静默通过；新增 `DiffTable` 只读报告，`Migrate` 自动补建缺失索引，列类型差异写日志提示 | `migrate.go`、`wxorm.go` |
| #32 | P1 | `Exec` / `Query` / `QueryRow` 不受会话超时约束 | 请求取消或超时对原生 SQL 无效；统一走会话 ctx 与超时 | `session/session.go` |
| #59 | P1 | 日志硬编码 `log.Printf` | 无法接入调用方日志体系、无法只采集慢 SQL；新增可注入 `Logger` + `QueryEvent` + 慢 SQL 阈值 | `session/logger.go`、`wxorm.go` |
| #60 | P1 | 批量插入不回填主键 | 模型主键仍为零值；PostgreSQL / SQLite 走 `INSERT ... RETURNING` 按行回填，MySQL 保持不回填 | `dialect/clause/{clause,postgresql,sqlite}.go`、`session/insert.go` |
| #61 | P1 | `Migrate` 仍只增删列 | 索引缺失与类型漂移无提示；已并入 #31 的差异报告 | `migrate.go` |
| #82 | P0 | SELECT / COUNT / Exist / UPDATE / DELETE 每次调用都重新 prepare | MySQL 复用连接上被驱动层延迟 ACK 放大到约 44ms；新增有界 `StatementCache`，非事务场景复用预编译语句，事务内绕过，DDL 后清空 | `session/stmt.go`、`session/record.go`、`session/table.go`、`wxorm.go` |
| #83 | P1 | 结果集逐行 `reflect.New` + `Append` 复制 | 100 行查询分配约 108KB；按 `LIMIT` 预分配容量，`[]Struct` 直接扫描到切片元素（保留调用方已有元素） | `session/record.go`、`session/session.go` |
| #84 | P1 | 批量插入固定按 100/500/1000 行拆事务 | 200~999 行被拆成多次往返；改为按方言占位符上限与列数自适应分批（单批上限 1000） | `session/record.go` |
| #85 | P0 | `UPDATE(struct)` 实际写回零值字段，与 README / 注释声明的「只更新非零字段」不符 | 部分结构体更新会把未赋值的列清空；改为默认跳过零值，全零结构体返回 `ErrUpdateStructEmpty`，需要置零时用 `UPDATE_COLUMNS(...)` 或 map 更新 | `schema/schema.go`、`session/record.go` |
| #86 | P1 | 复合主键的 WHERE 条件来自 map 遍历，顺序随机 | 同一条语句每次生成不同 SQL 文本，预编译缓存无法稳定命中；改为按列顺序输出 `[]schema.FieldValue` | `schema/schema.go`、`session/record.go` |
| #87 | P1 | `FindFirst` / `SELECT(&struct)` 先查成切片再取首行 | 多一次切片分配与整结构体拷贝；抽出 `buildSelect` 后直接扫描单行，并清掉残留的 LIMIT 状态 | `session/record.go` |
| #88 | P1 | `Extract*` 每次遍历所有列并逐列判断主键 / 自动插入 | 复合主键提取 96.9ns / 400B / 3 allocs；预计算 `PrimaryKeyFields` / `UpdateFields` 后为 22.1ns / 64B / 1 alloc | `schema/schema.go` |
| #90 | P0 | `IncludeZero()` 只能整行覆盖，无法只更新指定列 | 只想写回某一列的零值时必须退回 map 并手写列名；改名为 `OVERWRITE()`（不保留旧名），新增 `UPDATE_COLUMNS(...)` 白名单与 `schema.ErrUnknownUpdateColumn` / `session.ErrUpdateColumnsEmpty` | `session/record.go`、`schema/schema.go` |
| #91 | P0 | `Migrate` 默认执行 `DROP COLUMN` | 字段删除、改名或仅改大小写都会在无人确认时销毁整列数据；改为默认只报告到 `DroppedColumns`，`WithDropMissingColumns()` 显式开启 | `wxorm.go`、`migrate.go` |
| #92 | P1 | 迁移的列名 / 列类型差异区分大小写 | 仅改大小写被判定为「删一列 + 加一列」；`diffStrings` / `diffColumnTypes` 改为折叠比较 | `wxorm.go`、`migrate.go` |
| #93 | P0 | `big.Int` / `big.Float` 不能作为模型字段 | MySQL 上 `big.Int` 误落到 JSON 列；驱动不接受 struct 参数、读取侧也没有 `sql.Scanner`。新增 `BigKind` / `BigFieldKinds` / `HasBigFields` 与文本桥接（写入 `String()` / `Text('f',30)`，读取经 `sql.NullString` 回填），MySQL `DECIMAL(65,0)` / `DECIMAL(65,30)`、PostgreSQL `NUMERIC`、SQLite `TEXT`（`NUMERIC` 亲和性会转 REAL）；`big.Rat` 解析阶段返回 `ErrUnsupportedBigRat` | `schema/big.go`、`session/big_scan.go`、`dialect/datatype/*` |
| #94 | P0 | 语句缓存淘汰时立即 `Close` 正在执行的语句 | 并发查询报 `sql: statement is closed`；改为 LRU + 引用计数，淘汰只标记退役，最后一个使用者 `Release` 后才 `Close`，并导出 `Session.ClearStatementCache()` | `session/stmt.go`、`session/session.go` |
| #95 | P1 | `Query` / `QueryRow` 叠加会话超时 timer 无法释放 | 结果集消费完之前无法 `cancel`，每次查询泄漏一个 timer 与 ctx；提前 `cancel` 又会让 `Next` 报 context canceled。改为直接使用会话 ctx，超时交给 `QueryContext` / `QueryRowContext` | `session/session.go`、`session/preload.go` |
| #96 | P1 | `Begin()` 重复调用静默覆盖旧事务 | 旧事务既不提交也不回滚，连接与行锁滞留；改为返回 `ErrTransactionAlreadyStarted`，补 `BeginTx(ctx, opts)` 与 `Engine.TransactContext`，未开启事务就提交/回滚返回导出的 `ErrTransactionNotStartedYet` | `session/transaction.go`、`wxorm.go` |
| #97 | P2 | `IN()` 空集合报错、`COUNT` 静默丢弃 `GROUP` / `HAVING` | 空集合改为按集合语义生成 `1 = 0` / `1 = 1`；`COUNT` 遇分组返回 `ErrCountWithGroupUnsupported`，不再把分组条件丢掉后统计全表 | `statement/where/expression_condition.go`、`session/record.go` |
| #98 | P1 | 慢 SQL 日志输出参数原值 | 集中式日志会长期留存手机号、身份证、密码；改为默认只输出参数个数（`QueryEvent.LogArgs`），`SetLogParamValues(true)` 显式开启，调试模式不受影响 | `session/logger.go`、`session/session.go`、`wxorm.go` |
| #99 | P1 | 批量 `RETURNING` 行数不一致时静默 `break` | 主键按下标错位回填，写进错误行；改为返回 `ErrBatchReturningMismatch` | `session/insert.go` |
| #100 | P0 | 查询结果集未关闭就执行钩子与预加载 | `AFTER_SELECT` 钩子与 `PRELOAD` 子查询在 `rows` 打开期间发起：单连接池（含 `SetMaxOpenConnections(1)`）下自锁到会话超时，默认池下一条查询固定占用两条连接；改为扫描完成后立即 `rows.Close()`，钩子在结果集释放后统一执行 | `session/record.go` |
| #101 | P1 | DDL / Migrate 叠加会话查询超时 | 建表、`ALTER TABLE`、建索引走 `Session.Exec`，受默认 10s 超时约束；MySQL 的 DDL 不能回滚，中断会留下半成品表结构。新增 `Session.ExecDDL`（不叠超时、仍继承会话上下文），`CreateTable` / `DeleteTable` / `Migrate` 全部改用 | `session/session.go`、`session/table.go`、`wxorm.go`、`migrate.go` |
| #102 | P1 | 执行失败仍返回非 nil 结果对象 | 错误路径返回带 nil 内层的 `result`，调用 `LastInsertId()` / `RowsAffected()` 直接空指针 panic；改为错误时返回 nil，并让结果对象在无底层结果时返回 `ErrNoExecutionResult` | `session/record.go`、`session/result/result.go` |
| #103 | P1 | `LIMIT(0)` 被当成未设置 | `LIMIT(0)` 不生成子句，分页参数算错时静默退化成全表查询；`LIMIT(0)+OFFSET(1)` 还会生成 `OFFSET 1` 这种非法 SQL。改为 0 生成 `LIMIT 0`，负数返回 `ErrLimitNegative` | `statement/clause.go` |
| #104 | P1 | `where.RAW` 按 `?` 切分改写占位符 | 字符串字面量、引用标识符、注释与 PostgreSQL 美元引用内的 `?` 被当成占位符，`$n` 编号错位；改为词法扫描，只改写引号/注释之外的 `?`（jsonb 的 `?` 运算符与占位符形态相同，个数不一致时仍按原样输出） | `statement/where/raw_condition.go` |
| #105 | P1 | schema 缓存按模型类型冻结表名 | 缓存键只有（方言, 类型），实现 `TableNameGetter` 的分表/多租户模型首个实例会把表名钉死，后续实例读写全部落到错误的表；改为（方言, 类型, 逻辑表名），表名为空时不缓存 | `schema/cache.go`、`schema/schema.go` |
| #106 | P2 | `Engine.Close` 只关闭预编译语句不移除 | 关闭后的引擎仍持有已关闭的 `*Stmt`，重复取用会得到 `sql: statement is closed`；改为关闭后删除条目，`Close` 重复调用保持幂等 | `wxorm.go` |
| #107 | P2 | `Engine` 配置读写无锁 | `SetDebug` / `SetSessionTimeout` / `SetLogger` / `SetSlowThreshold` / `SetLogParamValues` / `SetStatementCacheSize` / `SetNaming` 与 `NewSession` 并发时数据竞争；新增 `sync.RWMutex`，`NewSession` 一次性取配置快照 | `wxorm.go` |
| #108 | P2 | `time.Duration` 映射为 MySQL `TIMESTAMP` | Go 中时长是 int64 纳秒，存成时间点后写进去的值无法原样读回；改为 `BIGINT`（与 SQLite `INTEGER` 对齐） | `dialect/datatype/mysql.go`、`dialect/datatype/datatype.go` |
| #109 | P2 | 枚举类型全量替换 `enum` → `ENUM` | 枚举取值里的 `enum` 也被大写，建表后默认值/写入值对不上；改为只替换首次出现的类型关键字 | `dialect/column/parser.go` |
| #110 | P2 | JOIN 表名含非法字符返回错表为空的错误 | 错误语义误导（表名非空却报 `ErrJoinTableIsEmpty`）；新增 `ErrJoinTableInvalid` | `session/join.go` |

## 待修复

| ID | 优先级 | 问题 | 建议方案 |
| --- | --- | --- | --- |
| #30 | P1 | `Session` 非并发安全 | 并发契约已在 README「已知限制」声明；可选：调试模式下加使用态断言 |
| #34 | P2 | 内存驱动不支持 `JOIN` / 子查询 / `BETWEEN` / `HAVING` | 按新增用例逐步扩展 |
| #36 | P2 | 无 CI 配置 | 落地 `gofmt` / `vet` / `build` / `test -race` 四道门禁 |
| #48 | P2 | 零值判断未覆盖指针/接口/切片等所有种类 | 当前实现已对无效值做守卫；后续补表驱动测试固化语义 |
| #62 | P2 | PostgreSQL 全文检索不建 GIN 索引 | 可在建表时识别 `FULLTEXT` 列，生成 `CREATE INDEX ... USING GIN` |
| #63 | P2 | `Dialect` 仍是胖接口 | 抽出 `BaseClause`（ANSI 近似实现），新方言只覆盖差异（SQLite 在 v0.5.0 未抽，直接复用 MySQL 版工具函数） |
| #111 | P2 | 方言层 `sync.Map` 缓存无容量上限 | `quotedCache` / `fromCache` / `selectCache` 等的键含标识符，动态列名会让内存单调增长；建议改成有界 LRU 或接入引擎级缓存（当前模型列名在编译期固定，实际风险低） |
| #112 | P3 | 事务回调 panic 在批量插入路径丢失调用栈 | `insertBatches` 用 `fmt.Errorf("%v", p)` 转换 panic，失去类型与栈；建议复用 `PanicError` 或在事务层统一捕获 |
| #113 | P3 | 预加载的全部关联共用一份超时预算 | `loadPreloads` 只创建一次 `execContext`，关联多且慢时后面的子查询会被前面的耗时吃掉；建议每条关联单独计时或按关系数缩放预算 |

## 不修复（已记录）

| ID | 说明 |
| --- | --- |
| #37 | 迁移删列已改为显式选项（`WithDropMissingColumns()`）：默认不动数据库，确认后才执行 `DROP COLUMN`；仅保留「不做隐式备份」这一设计取舍 |
| #38 | 内存驱动只保证「测试用到的 SQL 子集」语义正确，不追求 MySQL 语法全覆盖 |
| #89 | 结果对象创建时一次性复制参数切片（`copyArgs`）。曾评估「懒拷贝」：`clause.Context.Reset` 用 `args[:0]` 复用数组，把切片所有权交给结果对象后，构建器下次只能重新 append —— 实测 4 个参数从 15.9ns / 64B / 1 alloc 变为 44ns / 112B / 3 allocs，反而更差，因此保留一次性复制 |
