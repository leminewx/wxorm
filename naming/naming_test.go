package naming

import (
	"errors"
	"testing"
)

func TestStrategyGetTableName(t *testing.T) {
	strategy := Strategy{Prefix: "tenant1_", Suffix: "_v2"}
	if got := strategy.GetTableName("users"); got != "tenant1_users_v2" {
		t.Fatalf("GetTableName = %q, want %q", got, "tenant1_users_v2")
	}

	if got := (Strategy{}).GetTableName("users"); got != "users" {
		t.Fatalf("零值策略不应改变表名，got %q", got)
	}
}

func TestStrategyQualify(t *testing.T) {
	strategy := Strategy{Schema: "tenant1"}
	if got := strategy.Qualify("users"); got != "tenant1.users" {
		t.Fatalf("Qualify = %q, want %q", got, "tenant1.users")
	}

	if got := (Strategy{}).Qualify("users"); got != "users" {
		t.Fatalf("Schema 为空时不应拼接库名，got %q", got)
	}

	if got := strategy.Qualify(""); got != "" {
		t.Fatalf("空表名应返回空，got %q", got)
	}
}

func TestStrategyValidate(t *testing.T) {
	valid := []Strategy{
		{},
		{Prefix: "tenant1_"},
		{Schema: "tenant1"},
		{Suffix: "_v2"},
		{Prefix: "租户_"},
	}
	for _, strategy := range valid {
		if err := strategy.Validate(); err != nil {
			t.Errorf("策略 %#v 应合法，实际: %v", strategy, err)
		}
	}

	invalid := []Strategy{
		{Prefix: "tenant.1_"},
		{Prefix: "tenant`1"},
		{Schema: `tenant"1`},
		{Suffix: " v2"},
		{Prefix: "a;b"},
		{Prefix: `a\b`},
	}
	for _, strategy := range invalid {
		err := strategy.Validate()
		if !errors.Is(err, ErrInvalidSegment) {
			t.Errorf("策略 %#v 应返回 ErrInvalidSegment，实际: %v", strategy, err)
		}
	}
}

func TestStrategyString(t *testing.T) {
	if got := (Strategy{}).String(); got != "naming(none)" {
		t.Fatalf("String = %q", got)
	}

	got := Strategy{Schema: "s", Prefix: "p_", Suffix: "_x"}.String()
	if want := "naming(schema=s, prefix=p_, suffix=_x)"; got != want {
		t.Fatalf("String = %q, want %q", got, want)
	}
}
