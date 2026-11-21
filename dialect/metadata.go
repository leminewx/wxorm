package dialect

import (
	"maps"
	"slices"
	"strings"

	"gitee.com/leminewx/wxorm/dialect/column"
)

// 本文件收集各方言共用的「列集合 → 约束分组」逻辑
//
// MySQL、PostgreSQL 与 SQLite 的差异只在 SQL 文本（引号、引擎、索引写法），
// 分组算法完全一致，放在这里避免多份实现漂移。

// filterPrimaryKeyColumns 筛选主键列
func filterPrimaryKeyColumns(columns []column.Column) []column.Column {
	cols := make([]column.Column, 0, len(columns))
	for _, col := range columns {
		if col.IsPrimaryKey() {
			cols = append(cols, col)
		}
	}

	return cols
}

// groupForeignKeyColumns 按被引用表分组外键列
func groupForeignKeyColumns(columns []column.Column) map[string][]column.Column {
	groups := make(map[string][]column.Column, len(columns))
	for _, col := range columns {
		fk := col.GetForeignKey()
		if fk == nil || fk.Table == "" {
			continue
		}

		groups[fk.Table] = append(groups[fk.Table], col)
	}

	return groups
}

// groupMetadataColumns 按命名索引或命名唯一约束分组
func groupMetadataColumns(columns []column.Column, get func(column.Column) column.Metadata) map[string][]column.Column {
	groups := make(map[string][]column.Column, len(columns))
	for _, col := range columns {
		metadata := get(col)
		if metadata.Has && metadata.Value != "" {
			groups[metadata.Value] = append(groups[metadata.Value], col)
		}
	}

	return groups
}

// sortKeys 返回 map 的有序键集合，保证 DDL 生成结果稳定
func sortKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

/*
indexNameBase 把限定表名转为可用作索引名前缀的形式

索引名属于 Schema 内的对象，不能带库/Schema 限定：引号处理会把点号拆成多段，
生成 "tenant1"."t_users_name_idx" 这样的非法标识符。这里只取最后一段。
*/
func indexNameBase(tableName string) string {
	if idx := strings.LastIndexByte(tableName, '.'); idx >= 0 {
		return tableName[idx+1:]
	}

	return tableName
}
