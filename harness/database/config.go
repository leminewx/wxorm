/*
Package database 读取 harness/testdata/database.json，集中管理测试连接参数。

配置只保存本地开发环境的默认值，CI 或其它环境应通过环境变量覆盖。
*/
package database

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitee.com/leminewx/wxorm/harness/assetpath"
)

// Config 对应 database.json 全文
type Config struct {
	Fake     Fake     `json:"fake"`
	MySQL    Endpoint `json:"mysql"`
	Postgres Endpoint `json:"postgres"`
	SQLite   SQLite   `json:"sqlite"`
}

// Fake 是内存驱动的库名与 DSN；内存驱动会忽略 DSN 的内容
type Fake struct {
	Name string `json:"name"`
	DSN  string `json:"dsn"`
}

// Endpoint 描述一个网络数据库的连接参数
type Endpoint struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
}

// SQLite 描述 SQLite 数据文件；相对路径基于 harness/testdata
type SQLite struct {
	File string `json:"file"`
}

// Load 读取并解析 database.json
func Load() (Config, error) {
	content, err := os.ReadFile(filepath.Join(assetpath.GetDir(), "database.json"))
	if err != nil {
		return Config{}, err
	}

	var config Config
	if err = json.Unmarshal(content, &config); err != nil {
		return Config{}, err
	}

	return config, nil
}

// BuildMySQLDSN 把配置拼成 go-sql-driver/mysql 可用的 DSN
func BuildMySQLDSN(endpoint Endpoint) string {
	// 容器内 time_zone 为 UTC，DSN 必须用 loc=UTC，否则扫描出的时间会整体偏移
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true&loc=UTC",
		endpoint.User, endpoint.Password, endpoint.Host, endpoint.Port, endpoint.Database)
}

// BuildPostgresDSN 把配置拼成 pgx 可用的 DSN
func BuildPostgresDSN(endpoint Endpoint) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		endpoint.User, endpoint.Password, endpoint.Host, endpoint.Port, endpoint.Database)
}

// ResolveSQLite 返回 SQLite 文件的绝对路径；未配置时返回空字符串
func ResolveSQLite(config Config) string {
	file := strings.TrimSpace(config.SQLite.File)
	if file == "" {
		return ""
	}

	if !filepath.IsAbs(file) {
		file = filepath.Join(assetpath.GetDir(), filepath.FromSlash(file))
	}

	return file
}
