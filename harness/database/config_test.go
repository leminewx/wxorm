package database

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestLoad 验证 database.json 可解析出三方言配置
func TestLoad(t *testing.T) {
	config, err := Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}

	if config.MySQL.Database == "" || config.Postgres.Database == "" {
		t.Fatalf("数据库名不应为空: %+v", config)
	}

	if config.Fake.Name == "" || config.Fake.DSN == "" {
		t.Fatalf("内存驱动配置不应为空: %+v", config.Fake)
	}
}

// TestBuildDSN 验证 DSN 拼接结果
func TestBuildDSN(t *testing.T) {
	endpoint := Endpoint{
		Host:     "127.0.0.1",
		Port:     3306,
		User:     "root",
		Password: "secret",
		Database: "wxorm_harness",
	}

	mysql := BuildMySQLDSN(endpoint)
	for _, want := range []string{"root:secret@tcp(127.0.0.1:3306)/wxorm_harness", "parseTime=true", "loc=UTC"} {
		if !strings.Contains(mysql, want) {
			t.Errorf("MySQL DSN 缺少 %q: %s", want, mysql)
		}
	}

	postgres := BuildPostgresDSN(endpoint)
	if !strings.Contains(postgres, "postgres://root:secret@127.0.0.1:3306/wxorm_harness") {
		t.Errorf("PostgreSQL DSN 不符合预期: %s", postgres)
	}
}

// TestResolveSQLite 验证相对路径基于 harness/testdata
func TestResolveSQLite(t *testing.T) {
	config := Config{SQLite: SQLite{File: "sqlite/wxorm_harness.db"}}
	got := ResolveSQLite(config)
	if !filepath.IsAbs(got) || !strings.HasSuffix(got, filepath.Join("harness", "testdata", "sqlite", "wxorm_harness.db")) {
		t.Fatalf("ResolveSQLite = %q", got)
	}

	if got := ResolveSQLite(Config{}); got != "" {
		t.Errorf("未配置时应返回空字符串，实际 %q", got)
	}
}
