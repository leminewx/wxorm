package statement

import (
	"fmt"
	"strings"
	"testing"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/dialect/clause"
	"gitee.com/leminewx/wxorm/statement/where"
)

// Test INSERT
func TestINSERT(t *testing.T) {
	tests := []struct {
		name        string
		table       string
		ignore      bool
		columns     []string
		wantName    Name
		wantError   error
		wantSQLPart string
	}{
		{
			name:      "正常INSERT",
			table:     "users",
			ignore:    false,
			columns:   []string{"id", "name", "email"},
			wantName:  NAME_INSERT,
			wantError: nil,
		},
		{
			name:      "INSERT IGNORE",
			table:     "users",
			ignore:    true,
			columns:   []string{"id", "name"},
			wantName:  NAME_INSERT,
			wantError: nil,
		},
		{
			name:      "空表名",
			table:     "",
			ignore:    false,
			columns:   []string{"id", "name"},
			wantName:  NAME_INSERT,
			wantError: dialect.ErrTableNameIsEmpty,
		},
		{
			name:      "空列",
			table:     "users",
			ignore:    false,
			columns:   []string{},
			wantName:  NAME_INSERT,
			wantError: ErrColumnsIsEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, buildFunc := INSERT(tt.table, tt.ignore, tt.columns...)
			assertEqual(t, name, tt.wantName, "函数名不匹配")

			if buildFunc != nil {
				ctx := clause.NewContext()
				cla := clause.NewMySQLClause()

				err := buildFunc(ctx, cla)
				assertError(t, err, tt.wantError, "错误不匹配")

				if tt.wantError == nil {
					sql, _ := ctx.GetResult()
					assertContains(t, sql, tt.table, "SQL应该包含表名")
					assertContains(t, sql, "INSERT", "SQL应该包含INSERT关键字")

					if tt.ignore {
						assertContains(t, sql, "IGNORE", "SQL应该包含IGNORE关键字")
					}
				}
			}
		})
	}
}

// Test REPLACE
func TestREPLACE(t *testing.T) {
	tests := []struct {
		name      string
		table     string
		columns   []string
		wantName  Name
		wantError error
	}{
		{
			name:      "正常REPLACE",
			table:     "users",
			columns:   []string{"id", "name"},
			wantName:  NAME_REPLACE,
			wantError: nil,
		},
		{
			name:      "多列REPLACE",
			table:     "settings",
			columns:   []string{"key", "value", "updated_at"},
			wantName:  NAME_REPLACE,
			wantError: nil,
		},
		{
			name:      "空表名",
			table:     "",
			columns:   []string{"key"},
			wantName:  NAME_REPLACE,
			wantError: dialect.ErrTableNameIsEmpty,
		},
		{
			name:      "空列",
			table:     "users",
			columns:   []string{},
			wantName:  NAME_REPLACE,
			wantError: ErrColumnsIsEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, buildFunc := REPLACE(tt.table, tt.columns...)
			assertEqual(t, name, tt.wantName, "函数名不匹配")

			if buildFunc != nil {
				ctx := clause.NewContext()
				cla := clause.NewMySQLClause()

				err := buildFunc(ctx, cla)
				assertError(t, err, tt.wantError, "错误不匹配")

				if tt.wantError == nil {
					sql, _ := ctx.GetResult()
					assertContains(t, sql, "REPLACE", "SQL应该包含REPLACE关键字")
				}
			}
		})
	}
}

// Test VALUES
func TestVALUES(t *testing.T) {
	tests := []struct {
		name      string
		values    [][]any
		wantName  Name
		wantError error
	}{
		{
			name:      "单行数据",
			values:    [][]any{{1, "John", "john@example.com"}},
			wantName:  NAME_VALUES,
			wantError: nil,
		},
		{
			name: "多行数据",
			values: [][]any{
				{1, "John", "john@example.com"},
				{2, "Jane", "jane@example.com"},
				{3, "Bob", "bob@example.com"},
			},
			wantName:  NAME_VALUES,
			wantError: nil,
		},
		{
			name:      "空数据",
			values:    [][]any{},
			wantName:  NAME_VALUES,
			wantError: ErrValuesIsEmpty,
		},
		{
			name:      "空数组",
			values:    [][]any{{}},
			wantName:  NAME_VALUES,
			wantError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, buildFunc := VALUES(tt.values...)
			assertEqual(t, name, tt.wantName, "函数名不匹配")

			if buildFunc != nil {
				ctx := clause.NewContext()
				cla := clause.NewMySQLClause()

				err := buildFunc(ctx, cla)
				assertError(t, err, tt.wantError, "错误不匹配")

				if tt.wantError == nil {
					sql, args := ctx.GetResult()
					assertContains(t, sql, "VALUES", "SQL应该包含VALUES关键字")

					// 验证参数数量
					expectedArgs := 0
					for _, row := range tt.values {
						expectedArgs += len(row)
					}
					assertLen(t, args, expectedArgs, "参数数量不匹配")
				}
			}
		})
	}
}

// Test DUPLICATE
func TestDUPLICATE(t *testing.T) {
	tests := []struct {
		name      string
		columns   []string
		wantName  Name
		wantError error
	}{
		{
			name:      "单列",
			columns:   []string{"name"},
			wantName:  NAME_DUPLICATE,
			wantError: nil,
		},
		{
			name:      "多列",
			columns:   []string{"name", "email", "updated_at"},
			wantName:  NAME_DUPLICATE,
			wantError: nil,
		},
		{
			name:      "空列",
			columns:   []string{},
			wantName:  NAME_DUPLICATE,
			wantError: ErrColumnsIsEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, buildFunc := DUPLICATE(tt.columns...)
			assertEqual(t, name, tt.wantName, "函数名不匹配")

			if buildFunc != nil {
				ctx := clause.NewContext()
				cla := clause.NewMySQLClause()

				err := buildFunc(ctx, cla)
				assertError(t, err, tt.wantError, "错误不匹配")

				if tt.wantError == nil {
					sql, _ := ctx.GetResult()
					assertContains(t, sql, "DUPLICATE", "SQL应该包含DUPLICATE关键字")
				}
			}
		})
	}
}

// Test DELETE
func TestDELETE(t *testing.T) {
	tests := []struct {
		name      string
		table     string
		wantName  Name
		wantError error
	}{
		{
			name:      "正常删除",
			table:     "users",
			wantName:  NAME_DELETE,
			wantError: nil,
		},
		{
			name:      "带数据库前缀",
			table:     "test_db.users",
			wantName:  NAME_DELETE,
			wantError: nil,
		},
		{
			name:      "空表名",
			table:     "",
			wantName:  NAME_DELETE,
			wantError: dialect.ErrTableNameIsEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, buildFunc := DELETE(tt.table)
			assertEqual(t, name, tt.wantName, "函数名不匹配")

			if buildFunc != nil {
				ctx := clause.NewContext()
				cla := clause.NewMySQLClause()

				err := buildFunc(ctx, cla)
				assertError(t, err, tt.wantError, "错误不匹配")

				if tt.wantError == nil {
					sql, _ := ctx.GetResult()
					assertContains(t, sql, "DELETE", "SQL应该包含DELETE关键字")
				}
			}
		})
	}
}

// Test WHERE
func TestWHERE(t *testing.T) {
	tests := []struct {
		name       string
		condition  where.Condition
		wantName   Name
		wantError  error
		wantHasSQL bool
	}{
		{
			name:       "正常条件",
			condition:  where.EQ("id", 1),
			wantName:   NAME_WHERE,
			wantError:  nil,
			wantHasSQL: true,
		},
		{
			name:       "复杂条件",
			condition:  where.EQ("status", "active").AND(where.GTE("age", 18)),
			wantName:   NAME_WHERE,
			wantError:  nil,
			wantHasSQL: true,
		},
		{
			name:       "空条件",
			condition:  nil,
			wantName:   NAME_WHERE,
			wantError:  ErrConditionIsNil,
			wantHasSQL: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, buildFunc := WHERE(tt.condition)
			assertEqual(t, name, tt.wantName, "函数名不匹配")

			if buildFunc != nil {
				ctx := clause.NewContext()
				cla := clause.NewMySQLClause()

				err := buildFunc(ctx, cla)
				assertError(t, err, tt.wantError, "错误不匹配")

				if tt.wantError == nil {
					sql, _ := ctx.GetResult()
					if tt.wantHasSQL {
						assertContains(t, sql, "WHERE", "SQL应该包含WHERE关键字")
					}
				}
			}
		})
	}
}

// Test UPDATE
func TestUPDATE(t *testing.T) {
	tests := []struct {
		name      string
		table     string
		wantName  Name
		wantError error
	}{
		{
			name:      "正常更新",
			table:     "users",
			wantName:  NAME_UPDATE,
			wantError: nil,
		},
		{
			name:      "空表名",
			table:     "",
			wantName:  NAME_UPDATE,
			wantError: dialect.ErrTableNameIsEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, buildFunc := UPDATE(tt.table)
			assertEqual(t, name, tt.wantName, "函数名不匹配")

			if buildFunc != nil {
				ctx := clause.NewContext()
				cla := clause.NewMySQLClause()

				err := buildFunc(ctx, cla)
				assertError(t, err, tt.wantError, "错误不匹配")

				if tt.wantError == nil {
					sql, _ := ctx.GetResult()
					assertContains(t, sql, "UPDATE", "SQL应该包含UPDATE关键字")
				}
			}
		})
	}
}

// Test SET
func TestSET(t *testing.T) {
	tests := []struct {
		name      string
		values    map[string]any
		wantName  Name
		wantError error
	}{
		{
			name: "单列设置",
			values: map[string]any{
				"name": "John",
			},
			wantName:  NAME_SET,
			wantError: nil,
		},
		{
			name: "多列设置",
			values: map[string]any{
				"name":   "John",
				"email":  "john@example.com",
				"age":    30,
				"active": true,
				"score":  95.5,
			},
			wantName:  NAME_SET,
			wantError: nil,
		},
		{
			name:      "空值映射",
			values:    map[string]any{},
			wantName:  NAME_SET,
			wantError: ErrSetValuesIsEmpty,
		},
		{
			name: "包含nil值",
			values: map[string]any{
				"deleted_at": nil,
				"updated_at": "2024-01-01",
			},
			wantName:  NAME_SET,
			wantError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, buildFunc := SET(tt.values)
			assertEqual(t, name, tt.wantName, "函数名不匹配")

			if buildFunc != nil {
				ctx := clause.NewContext()
				cla := clause.NewMySQLClause()

				err := buildFunc(ctx, cla)
				assertError(t, err, tt.wantError, "错误不匹配")

				if tt.wantError == nil {
					sql, args := ctx.GetResult()
					assertContains(t, sql, "SET", "SQL应该包含SET关键字")

					// 验证参数数量
					assertLen(t, args, len(tt.values), "参数数量不匹配")
				}
			}
		})
	}
}

// Test SELECT
func TestSELECT(t *testing.T) {
	tests := []struct {
		name      string
		columns   []string
		wantName  Name
		wantError error
	}{
		{
			name:      "选择所有列",
			columns:   []string{},
			wantName:  NAME_SELECT,
			wantError: nil,
		},
		{
			name:      "选择指定列",
			columns:   []string{"id", "name", "email"},
			wantName:  NAME_SELECT,
			wantError: nil,
		},
		{
			name:      "带表名前缀",
			columns:   []string{"users.id", "users.name", "profiles.bio"},
			wantName:  NAME_SELECT,
			wantError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, buildFunc := SELECT("test", tt.columns...)
			assertEqual(t, name, tt.wantName, "函数名不匹配")

			if buildFunc != nil {
				ctx := clause.NewContext()
				cla := clause.NewMySQLClause()

				err := buildFunc(ctx, cla)
				assertError(t, err, tt.wantError, "错误不匹配")

				sql, _ := ctx.GetResult()
				assertContains(t, sql, "SELECT", "SQL应该包含SELECT关键字")

				if len(tt.columns) == 0 {
					assertContains(t, sql, "*", "无列时应选择*")
				}
			}
		})
	}
}

// Test FROM
func TestFROM(t *testing.T) {
	tests := []struct {
		name      string
		table     string
		wantName  Name
		wantError error
	}{
		{
			name:      "简单表名",
			table:     "users",
			wantName:  NAME_FROM,
			wantError: nil,
		},
		{
			name:      "带数据库前缀",
			table:     "production.users",
			wantName:  NAME_FROM,
			wantError: nil,
		},
		{
			name:      "空表名",
			table:     "",
			wantName:  NAME_FROM,
			wantError: dialect.ErrTableNameIsEmpty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, buildFunc := FROM(tt.table)
			assertEqual(t, name, tt.wantName, "函数名不匹配")

			if buildFunc != nil {
				ctx := clause.NewContext()
				cla := clause.NewMySQLClause()

				err := buildFunc(ctx, cla)
				assertError(t, err, tt.wantError, "错误不匹配")

				if tt.wantError == nil {
					sql, _ := ctx.GetResult()
					assertContains(t, sql, "FROM", "SQL应该包含FROM关键字")
				}
			}
		})
	}
}

// Test GROUP
func TestGROUP(t *testing.T) {
	tests := []struct {
		name       string
		columns    []string
		wantName   Name
		wantError  error
		wantHasSQL bool
	}{
		{
			name:       "单列分组",
			columns:    []string{"department"},
			wantName:   NAME_GROUP,
			wantError:  nil,
			wantHasSQL: true,
		},
		{
			name:       "多列分组",
			columns:    []string{"department", "status", "year"},
			wantName:   NAME_GROUP,
			wantError:  nil,
			wantHasSQL: true,
		},
		{
			name:       "空列（跳过）",
			columns:    []string{},
			wantName:   NAME_GROUP,
			wantError:  nil,
			wantHasSQL: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, buildFunc := GROUP(tt.columns...)
			assertEqual(t, name, tt.wantName, "函数名不匹配")

			if buildFunc != nil {
				ctx := clause.NewContext()
				cla := clause.NewMySQLClause()

				err := buildFunc(ctx, cla)
				assertError(t, err, tt.wantError, "错误不匹配")

				sql, _ := ctx.GetResult()
				if tt.wantHasSQL {
					assertContains(t, sql, "GROUP BY", "SQL应该包含GROUP BY关键字")
				} else {
					if sql != "" {
						t.Errorf("空列时不应该生成SQL，但得到: %s", sql)
					}
				}
			}
		})
	}
}

// Test HAVING
func TestHAVING(t *testing.T) {
	tests := []struct {
		name       string
		condition  where.Condition
		wantName   Name
		wantError  error
		wantHasSQL bool
	}{
		{
			name:       "聚合条件",
			condition:  where.GT("COUNT(*)", 10),
			wantName:   NAME_HAVING,
			wantError:  nil,
			wantHasSQL: true,
		},
		{
			name:       "复杂条件",
			condition:  where.GT("COUNT(*)", 10).AND(where.GT("AVG(score)", 80)),
			wantName:   NAME_HAVING,
			wantError:  nil,
			wantHasSQL: true,
		},
		{
			name:       "空条件",
			condition:  nil,
			wantName:   NAME_HAVING,
			wantError:  ErrConditionIsNil,
			wantHasSQL: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, buildFunc := HAVING(tt.condition)
			assertEqual(t, name, tt.wantName, "函数名不匹配")

			if buildFunc != nil {
				ctx := clause.NewContext()
				cla := clause.NewMySQLClause()

				err := buildFunc(ctx, cla)
				assertError(t, err, tt.wantError, "错误不匹配")

				if tt.wantError == nil {
					sql, _ := ctx.GetResult()
					if tt.wantHasSQL {
						assertContains(t, sql, "HAVING", "SQL应该包含HAVING关键字")
					}
				}
			}
		})
	}
}

// Test ORDER
func TestORDER(t *testing.T) {
	tests := []struct {
		name       string
		columns    []string
		wantName   Name
		wantError  error
		wantHasSQL bool
	}{
		{
			name:       "单列排序",
			columns:    []string{"created_at"},
			wantName:   NAME_ORDER,
			wantError:  nil,
			wantHasSQL: true,
		},
		{
			name:       "多列排序",
			columns:    []string{"last_name", "first_name", "age"},
			wantName:   NAME_ORDER,
			wantError:  nil,
			wantHasSQL: true,
		},
		{
			name:       "空列（跳过）",
			columns:    []string{},
			wantName:   NAME_ORDER,
			wantError:  nil,
			wantHasSQL: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, buildFunc := ORDER(tt.columns...)
			assertEqual(t, name, tt.wantName, "函数名不匹配")

			if buildFunc != nil {
				ctx := clause.NewContext()
				cla := clause.NewMySQLClause()

				err := buildFunc(ctx, cla)
				assertError(t, err, tt.wantError, "错误不匹配")

				sql, _ := ctx.GetResult()
				if tt.wantHasSQL {
					assertContains(t, sql, "ORDER BY", "SQL应该包含ORDER BY关键字")
				} else {
					if sql != "" {
						t.Errorf("空列时不应该生成SQL，但得到: %s", sql)
					}
				}
			}
		})
	}
}

// Test LIMIT
func TestLIMIT(t *testing.T) {
	tests := []struct {
		name       string
		limit      int
		wantName   Name
		wantError  error
		wantHasSQL bool
	}{
		{
			name:       "正数限制",
			limit:      10,
			wantName:   NAME_LIMIT,
			wantError:  nil,
			wantHasSQL: true,
		},
		{
			name:       "限制1",
			limit:      1,
			wantName:   NAME_LIMIT,
			wantError:  nil,
			wantHasSQL: true,
		},
		{
			name:       "零限制（生成 LIMIT 0）",
			limit:      0,
			wantName:   NAME_LIMIT,
			wantError:  nil,
			wantHasSQL: true,
		},
		{
			name:       "负数限制（报错）",
			limit:      -5,
			wantName:   NAME_LIMIT,
			wantError:  ErrLimitNegative,
			wantHasSQL: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, buildFunc := LIMIT(tt.limit)
			assertEqual(t, name, tt.wantName, "函数名不匹配")

			if buildFunc != nil {
				ctx := clause.NewContext()
				cla := clause.NewMySQLClause()

				err := buildFunc(ctx, cla)
				assertError(t, err, tt.wantError, "错误不匹配")

				sql, _ := ctx.GetResult()
				if tt.wantHasSQL {
					assertContains(t, sql, "LIMIT", "SQL应该包含LIMIT关键字")
				} else {
					if sql != "" {
						t.Errorf("无效限制时不应该生成SQL，但得到: %s", sql)
					}
				}
			}
		})
	}
}

// Test OFFSET
func TestOFFSET(t *testing.T) {
	tests := []struct {
		name       string
		offset     int
		wantName   Name
		wantError  error
		wantHasSQL bool
	}{
		{
			name:       "正数偏移",
			offset:     20,
			wantName:   NAME_OFFSET,
			wantError:  nil,
			wantHasSQL: true,
		},
		{
			name:       "零偏移（跳过）",
			offset:     0,
			wantName:   NAME_OFFSET,
			wantError:  nil,
			wantHasSQL: false,
		},
		{
			name:       "负数偏移（跳过）",
			offset:     -10,
			wantName:   NAME_OFFSET,
			wantError:  nil,
			wantHasSQL: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, buildFunc := OFFSET(tt.offset)
			assertEqual(t, name, tt.wantName, "函数名不匹配")

			if buildFunc != nil {
				ctx := clause.NewContext()
				cla := clause.NewMySQLClause()

				err := buildFunc(ctx, cla)
				assertError(t, err, tt.wantError, "错误不匹配")

				sql, _ := ctx.GetResult()
				if tt.wantHasSQL {
					assertContains(t, sql, "OFFSET", "SQL应该包含OFFSET关键字")
				} else {
					if sql != "" {
						t.Errorf("无效偏移时不应该生成SQL，但得到: %s", sql)
					}
				}
			}
		})
	}
}

// 集成测试
func TestStatementIntegration(t *testing.T) {
	// 测试完整的SQL语句构建
	t.Run("完整SELECT语句", func(t *testing.T) {
		ctx := clause.NewContext()
		cla := clause.NewMySQLClause()

		// 构建SELECT语句
		_, selectFunc := SELECT("id", "name", "email")
		if err := selectFunc(ctx, cla); err != nil {
			t.Fatalf("SELECT失败: %v", err)
		}

		_, fromFunc := FROM("users")
		if err := fromFunc(ctx, cla); err != nil {
			t.Fatalf("FROM失败: %v", err)
		}

		_, whereFunc := WHERE(where.EQ("status", "active").AND(where.GTE("age", 18)))
		if err := whereFunc(ctx, cla); err != nil {
			t.Fatalf("WHERE失败: %v", err)
		}

		_, orderFunc := ORDER("created_at")
		if err := orderFunc(ctx, cla); err != nil {
			t.Fatalf("ORDER失败: %v", err)
		}

		_, limitFunc := LIMIT(10)
		if err := limitFunc(ctx, cla); err != nil {
			t.Fatalf("LIMIT失败: %v", err)
		}

		_, offsetFunc := OFFSET(20)
		if err := offsetFunc(ctx, cla); err != nil {
			t.Fatalf("OFFSET失败: %v", err)
		}

		sql, args := ctx.GetResult()

		// 验证完整的SQL
		assertContains(t, sql, "SELECT", "SQL应该包含SELECT")
		assertContains(t, sql, "FROM", "SQL应该包含FROM")
		assertContains(t, sql, "WHERE", "SQL应该包含WHERE")
		assertContains(t, sql, "ORDER BY", "SQL应该包含ORDER BY")
		assertContains(t, sql, "LIMIT", "SQL应该包含LIMIT")
		assertContains(t, sql, "OFFSET", "SQL应该包含OFFSET")

		// 验证参数
		assertLen(t, args, 2, "应该有2个参数") // status="active", age=18
		if len(args) >= 1 {
			assertEqual(t, args[0], "active", "第一个参数应该是active")
		}
		if len(args) >= 2 {
			assertEqual(t, args[1], 18, "第二个参数应该是18")
		}
	})

	t.Run("完整INSERT语句", func(t *testing.T) {
		ctx := clause.NewContext()
		cla := clause.NewMySQLClause()

		_, insertFunc := INSERT("users", false, "name", "email", "age")
		if err := insertFunc(ctx, cla); err != nil {
			t.Fatalf("INSERT失败: %v", err)
		}

		_, valuesFunc := VALUES([]any{"John", "john@example.com", 30})
		if err := valuesFunc(ctx, cla); err != nil {
			t.Fatalf("VALUES失败: %v", err)
		}

		sql, args := ctx.GetResult()

		assertContains(t, sql, "INSERT INTO", "SQL应该包含INSERT INTO")
		assertContains(t, sql, "VALUES", "SQL应该包含VALUES")
		assertLen(t, args, 3, "应该有3个参数")
	})

	t.Run("完整UPDATE语句", func(t *testing.T) {
		ctx := clause.NewContext()
		cla := clause.NewMySQLClause()

		_, updateFunc := UPDATE("users")
		if err := updateFunc(ctx, cla); err != nil {
			t.Fatalf("UPDATE失败: %v", err)
		}

		_, setFunc := SET(map[string]any{
			"name":  "John Updated",
			"email": "john.updated@example.com",
		})
		if err := setFunc(ctx, cla); err != nil {
			t.Fatalf("SET失败: %v", err)
		}

		_, whereFunc := WHERE(where.EQ("id", 1))
		if err := whereFunc(ctx, cla); err != nil {
			t.Fatalf("WHERE失败: %v", err)
		}

		sql, args := ctx.GetResult()

		assertContains(t, sql, "UPDATE", "SQL应该包含UPDATE")
		assertContains(t, sql, "SET", "SQL应该包含SET")
		assertContains(t, sql, "WHERE", "SQL应该包含WHERE")
		assertLen(t, args, 3, "应该有3个参数") // name, email, id
	})
}

// 错误常量测试（确保导出的错误常量正确）
func TestErrorConstants(t *testing.T) {
	if ErrColumnsIsEmpty == nil {
		t.Error("ErrColumnsIsEmpty should not be nil")
	}
	if ErrValuesIsEmpty == nil {
		t.Error("ErrValuesIsEmpty should not be nil")
	}
	if ErrConditionIsNil == nil {
		t.Error("ErrConditionIsNil should not be nil")
	}
	if ErrSetValuesIsEmpty == nil {
		t.Error("ErrSetValuesIsEmpty should not be nil")
	}
	if dialect.ErrTableNameIsEmpty == nil {
		t.Error("ErrTableNameIsEmpty should not be nil")
	}
}

// 测试函数返回的构建函数是否为nil
func TestClauseBuildFuncReturns(t *testing.T) {
	// 测试正常情况返回非nil构建函数
	tests := []struct {
		name string
		fn   func() (Name, ClauseBuildFunc)
	}{
		{"INSERT", func() (Name, ClauseBuildFunc) { return INSERT("users", false, "id") }},
		{"SELECT", func() (Name, ClauseBuildFunc) { return SELECT("id") }},
		{"FROM", func() (Name, ClauseBuildFunc) { return FROM("users") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, buildFunc := tt.fn()
			if buildFunc == nil {
				t.Errorf("%s should return non-nil build function", tt.name)
			}
		})
	}
}

func TestStatement_SELECT(t *testing.T) {
	selectName, selectClause := SELECT("DISTINCT column1", "column2", "column3")
	fromName, fromClause := FROM("testers")
	whereName, whereClause := WHERE(
		where.EQ("column1", "column2").
			AND(
				where.NE("column2", 1),
				where.LIKE("column3", "%aaa%"),
			),
	)
	groupName, groupClause := GROUP("column2")
	havingName, havingClause := HAVING(
		where.EQ("column1", "column2").
			AND(
				where.NE("column2", 1),
				where.LIKE("column3", "%aaa%"),
			),
	)
	orderName, orderClause := ORDER("column1 ASC", "column2 DESC", "column")
	limitName, limitClause := LIMIT(10)
	offsetName, offsetClause := OFFSET(10)

	cla := clause.NewMySQLClause()
	sql, args, err := NewStatement("test").
		AddClause(selectName, selectClause).
		AddClause(fromName, fromClause).
		AddClause(whereName, whereClause).
		AddClause(groupName, groupClause).
		AddClause(havingName, havingClause).
		AddClause(orderName, orderClause).
		AddClause(limitName, limitClause).
		AddClause(offsetName, offsetClause).
		BuildSQL(cla, NAME_SELECT, NAME_FROM, NAME_WHERE, NAME_GROUP, NAME_HAVING, NAME_ORDER, NAME_LIMIT, NAME_OFFSET)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println(sql)
	fmt.Println(args)
}

func TestStatement_INSERT(t *testing.T) {
	stat := NewStatement("test")
	dial := dialect.NewMySQLDialect()

	for range 3 {
		// 写入 INSERT 子句和 VALUES 子句
		stat.AddClause(INSERT("test", true, "id", "name", "age", "sex", "email")).
			AddClause(VALUES([]any{"1", "张三", 18, "男", "zhangsan@example.com"}))

		// 构建完整的 INSERT 语句
		if sql, args, err := stat.BuildSQL(dial,
			NAME_INSERT,
			NAME_VALUES,
			NAME_SELECT,
			NAME_DUPLICATE,
		); err != nil {
			t.Fatal(err)
		} else {
			fmt.Println(sql)
			fmt.Println(args)
			fmt.Println()
		}

		stat.Reset()
	}
}

/*
cpu: Intel(R) Core(TM) i5-14600KF		20    	 4461744	       274.6 ns/op	     536 B/op	       8 allocs/op
*/
func BenchmarkStatement_INSERT(b *testing.B) {
	stat := NewStatement("test")
	dial := dialect.NewMySQLDialect()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		// 写入 INSERT 子句和 VALUES 子句
		stat.AddClause(INSERT("test", true, "id", "name", "age", "sex", "email")).
			AddClause(VALUES([]any{"1", "张三", 18, "男", "zhangsan@example.com"}))

		// 构建完整的 INSERT 语句
		if _, _, err := stat.BuildSQL(dial,
			NAME_INSERT,
			NAME_VALUES,
			NAME_SELECT,
			NAME_DUPLICATE,
		); err != nil {
			b.Fatal(err)
		}

		stat.Reset()
	}
}

// Test helper functions
func assertError(t *testing.T, err error, wantErr error, msg string) {
	t.Helper()
	if wantErr != nil {
		if err == nil {
			t.Errorf("%s: expected error %v, got nil", msg, wantErr)
		} else if err.Error() != wantErr.Error() {
			t.Errorf("%s: expected error %v, got %v", msg, wantErr, err)
		}
	} else {
		if err != nil {
			t.Errorf("%s: unexpected error: %v", msg, err)
		}
	}
}

func assertContains(t *testing.T, s, substr string, msg string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("%s: expected %q to contain %q", msg, s, substr)
	}
}

func assertEqual(t *testing.T, got, want any, msg string) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", msg, got, want)
	}
}

func assertLen(t *testing.T, slice []any, length int, msg string) {
	t.Helper()
	if len(slice) != length {
		t.Errorf("%s: got length %d, want %d", msg, len(slice), length)
	}
}
