package dialect

import (
	"errors"
	"testing"
)

// 测试方言注册与查找
func TestRegisterAndGet(t *testing.T) {
	dial := NewMySQLDialect()
	Register(" Test-Driver ", dial)

	// 注册名会被归一化（去空格 + 小写）
	if got := Get("test-driver"); got != dial {
		t.Errorf("expected registered dialect, got %v", got)
	}

	if got := Get("not-exists"); got != nil {
		t.Errorf("expected nil for unregistered driver, got %v", got)
	}
}

// 测试 Lookup 在未注册驱动时返回明确错误
func TestLookup_UnsupportedDriver(t *testing.T) {
	if _, err := Lookup("mysql"); err != nil {
		t.Fatalf("expected mysql to be registered: %v", err)
	}

	// PostgreSQL 的三个别名（postgres/postgresql/pgx）共用同一份方言
	var postgresDialect Dialect
	for _, name := range []string{Postgres, Postgresql, Pgx} {
		dial, err := Lookup(name)
		if err != nil {
			t.Fatalf("expected %s to be registered: %v", name, err)
		}

		if postgresDialect == nil {
			postgresDialect = dial
		} else if dial != postgresDialect {
			t.Errorf("expected %s to share the same dialect instance", name)
		}

		if dial.GetDriverName() != Postgres {
			t.Errorf("expected driver name %q, got %q", Postgres, dial.GetDriverName())
		}
	}

	if _, err := Lookup("oracle"); !errors.Is(err, ErrUnsupportedDriver) {
		t.Errorf("expected ErrUnsupportedDriver, got %v", err)
	}
}
