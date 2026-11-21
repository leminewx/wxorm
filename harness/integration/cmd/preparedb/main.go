// 命令 preparedb 创建集成测试所需的真实数据库，可重复执行。
//
// 用法（在 harness/integration 目录）：
//
//	go run ./cmd/preparedb                     # 创建 MySQL、PostgreSQL 与 SQLite 真库
//	go run ./cmd/preparedb -dialect mysql      # 只创建 MySQL 库
//
// 连接参数来自 harness/testdata/database.json。
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gitee.com/leminewx/wxorm/harness/database"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// _TIMEOUT 为建库与探活超时
const _TIMEOUT = 15 * time.Second

func main() {
	dialect := flag.String("dialect", "all", "创建哪个方言的数据库：all / mysql / postgres / sqlite")
	flag.Parse()

	config, err := database.Load()
	if err != nil {
		log.Fatalf("读取测试数据失败: %v", err)
	}

	tasks := []struct {
		name    string
		prepare func(context.Context) error
	}{
		{"mysql", func(ctx context.Context) error { return prepareMySQL(ctx, config.MySQL) }},
		{"postgres", func(ctx context.Context) error { return preparePostgres(ctx, config.Postgres) }},
		{"sqlite", func(_ context.Context) error { return prepareSQLite(config) }},
	}

	matched := false
	for _, task := range tasks {
		if *dialect != "all" && *dialect != task.name {
			continue
		}

		matched = true
		ctx, cancel := context.WithTimeout(context.Background(), _TIMEOUT)
		err := task.prepare(ctx)
		cancel()
		if err != nil {
			log.Fatalf("创建 %s 数据库失败: %v", task.name, err)
		}

		fmt.Printf("%-9s 就绪\n", task.name)
	}

	if !matched {
		log.Fatalf("未知方言 %q，可选 all / mysql / postgres / sqlite", *dialect)
	}
}

// prepareMySQL 连接 server（不指定库）并创建目标数据库
func prepareMySQL(ctx context.Context, endpoint database.Endpoint) error {
	server := endpoint
	server.Database = ""

	db, err := sql.Open("mysql", database.BuildMySQLDSN(server))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err = db.PingContext(ctx); err != nil {
		return err
	}

	statement := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci",
		quoteMySQLName(endpoint.Database))
	_, err = db.ExecContext(ctx, statement)
	return err
}

// preparePostgres 连接 postgres 维护库并创建目标数据库
func preparePostgres(ctx context.Context, endpoint database.Endpoint) error {
	server := endpoint
	server.Database = "postgres"

	db, err := sql.Open("pgx", database.BuildPostgresDSN(server))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err = db.PingContext(ctx); err != nil {
		return err
	}

	var exists int
	err = db.QueryRowContext(ctx, "SELECT 1 FROM pg_database WHERE datname = $1", endpoint.Database).Scan(&exists)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	_, err = db.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE "%s"`, quotePostgresName(endpoint.Database)))
	return err
}

// prepareSQLite 确保数据文件所在目录存在，并打开文件建立 WAL 模式
func prepareSQLite(config database.Config) error {
	file := database.ResolveSQLite(config)
	if file == "" {
		return errors.New("database.json 未配置 sqlite.file")
	}

	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}

	dsn := "file:" + filepath.ToSlash(file) + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err = db.Ping(); err != nil {
		return err
	}

	_, err = db.Exec("PRAGMA user_version = 1")
	return err
}

// quoteMySQLName 转义 MySQL 标识符中的反引号
func quoteMySQLName(name string) string {
	return strings.ReplaceAll(name, "`", "``")
}

// quotePostgresName 转义 PostgreSQL 标识符中的双引号
func quotePostgresName(name string) string {
	return strings.ReplaceAll(name, `"`, `""`)
}
