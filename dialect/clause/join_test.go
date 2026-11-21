package clause

import (
	"errors"
	"testing"
)

// TestJoinKindKeyword 校验 JOIN 类型到 SQL 关键字的映射
func TestJoinKindKeyword(t *testing.T) {
	cases := map[JoinKind]string{
		JOIN_INNER: "INNER JOIN",
		JOIN_LEFT:  "LEFT JOIN",
		JOIN_RIGHT: "RIGHT JOIN",
		JOIN_FULL:  "FULL JOIN",
		JOIN_CROSS: "CROSS JOIN",
	}

	for kind, want := range cases {
		if got := kind.Keyword(); got != want {
			t.Errorf("%s.Keyword() = %q, want %q", kind, got, want)
		}

		if got := kind.String(); got != want {
			t.Errorf("%s.String() = %q, want %q", kind, got, want)
		}
	}

	if got := JoinKind("NATURAL").Keyword(); got != "" {
		t.Errorf("未知类型的 Keyword() = %q, want empty", got)
	}

	if got := JoinKind("NATURAL").String(); got != "NATURAL" {
		t.Errorf("未知类型的 String() = %q, want %q", got, "NATURAL")
	}
}

// TestBuildJoinSQLPerDialect 校验三方言的 JOIN 子句文本与能力范围
func TestBuildJoinSQLPerDialect(t *testing.T) {
	tests := []struct {
		name        string
		cla         Clause
		table       string
		alias       string
		want        string
		supportFull bool
	}{
		{
			name:        "mysql",
			cla:         NewMySQLClause(),
			table:       "authors",
			alias:       "author",
			want:        "\nLEFT JOIN\n    `authors` AS `author`",
			supportFull: false,
		},
		{
			name:        "postgres",
			cla:         NewPostgresClause(),
			table:       "authors",
			alias:       "author",
			want:        "\nLEFT JOIN\n    \"authors\" AS \"author\"",
			supportFull: true,
		},
		{
			name:        "sqlite",
			cla:         NewSQLiteClause(),
			table:       "authors",
			alias:       "author",
			want:        "\nLEFT JOIN\n    \"authors\" AS \"author\"",
			supportFull: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			joiner, ok := tt.cla.(Joiner)
			if !ok {
				t.Fatalf("%T 未实现 clause.Joiner", tt.cla)
			}

			ctx := NewContext()
			if err := joiner.BuildJoinSQL(ctx, JOIN_LEFT, tt.table, tt.alias); err != nil {
				t.Fatalf("BuildJoinSQL error: %v", err)
			}

			if sql, _ := ctx.GetResult(); sql != tt.want {
				t.Errorf("SQL = %q, want %q", sql, tt.want)
			}

			// CROSS JOIN 使用同样的关键字和后缀
			ctx.Reset()
			if err := joiner.BuildJoinSQL(ctx, JOIN_CROSS, tt.table, tt.alias); err != nil {
				t.Fatalf("BuildJoinSQL(CROSS) error: %v", err)
			}
			wantCross := "\nCROSS JOIN" + tt.want[len("\nLEFT JOIN"):]
			if sql, _ := ctx.GetResult(); sql != wantCross {
				t.Errorf("CROSS SQL = %q, want %q", sql, wantCross)
			}

			// FULL JOIN：MySQL 不支持，其余方言支持
			ctx.Reset()
			err := joiner.BuildJoinSQL(ctx, JOIN_FULL, tt.table, tt.alias)
			if tt.supportFull {
				if err != nil {
					t.Errorf("FULL JOIN 应受支持，实际: %v", err)
				}
			} else if !errors.Is(err, ErrUnsupportedFeature) {
				t.Errorf("FULL JOIN 应返回 ErrUnsupportedFeature，实际: %v", err)
			}

			// 未知类型必须被拒绝，而不是生成语法错误的 SQL
			ctx.Reset()
			if err := joiner.BuildJoinSQL(ctx, JoinKind("NATURAL"), tt.table, tt.alias); !errors.Is(err, ErrInvalidJoinKind) {
				t.Errorf("未知 JOIN 类型应返回 ErrInvalidJoinKind，实际: %v", err)
			}

			// 表名与别名缺失时在构建期报错
			ctx.Reset()
			if err := joiner.BuildJoinSQL(ctx, JOIN_LEFT, "", tt.alias); !errors.Is(err, ErrJoinTableIsEmpty) {
				t.Errorf("空表名应返回 ErrJoinTableIsEmpty，实际: %v", err)
			}

			ctx.Reset()
			if err := joiner.BuildJoinSQL(ctx, JOIN_LEFT, tt.table, ""); !errors.Is(err, ErrJoinAliasIsEmpty) {
				t.Errorf("空别名应返回 ErrJoinAliasIsEmpty，实际: %v", err)
			}
		})
	}
}

// TestJoinCapabilityCompile 确认三种方言都满足能力接口
func TestJoinCapabilityCompile(t *testing.T) {
	var _ Joiner = NewMySQLClause()
	var _ Joiner = NewPostgresClause()
	var _ Joiner = NewSQLiteClause()
}
