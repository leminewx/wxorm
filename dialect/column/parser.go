package column

import (
	"fmt"
	"regexp"
	"strings"

	"gitee.com/leminewx/wxorm/dialect/datatype"
)

var (
	// 基本标记
	regexpPk      = regexp.MustCompile(`(?i)\bpk\b`)
	regexpAuto    = regexp.MustCompile(`(?i)\bauto\b`)
	regexpNull    = regexp.MustCompile(`(?i)\bnull\b`)
	regexpNotNull = regexp.MustCompile(`(?i)\bnotnull\b`)
	regexpIndex   = regexp.MustCompile(`(?i)\bindex\b`)
	regexpUnique  = regexp.MustCompile(`(?i)\bunique\b`)
	regexpCreated = regexp.MustCompile(`(?i)\bcreated\b`)
	regexpUpdated = regexp.MustCompile(`(?i)\bupdated\b`)

	// 带参数标记
	regexpColumn      = regexp.MustCompile(`(?i)column\(\s*([^)]*)\s*\)`)
	regexpType        = regexp.MustCompile(`(?i)type\(\s*([^()]*(?:\([^()]*\)[^()]*)*)\s*\)`)
	regexpForeignKey  = regexp.MustCompile(`(?i)fk\(\s*((?:[^()]|\([^()]*\))*)\s*\)`)
	regexpNamedIndex  = regexp.MustCompile(`(?i)index\(\s*([^)]*)\s*\)`)
	regexpNamedUnique = regexp.MustCompile(`(?i)unique\(\s*([^)]*)\s*\)`)
	regexpCheck       = regexp.MustCompile(`(?i)check\(\s*((?:[^()]|\([^()]*\))*)\s*\)`)
	regexpDefault     = regexp.MustCompile(`(?i)default\(\s*([^)]*)\s*\)`)
	regexpGenerated   = regexp.MustCompile(`(?i)generated\(\s*([^)]*)\s*\)`)
	regexpCharset     = regexp.MustCompile(`(?i)charset\(\s*([^)]*)\s*\)`)
	regexpCollate     = regexp.MustCompile(`(?i)collate\(\s*([^)]*)\s*\)`)
	regexpFormat      = regexp.MustCompile(`(?i)format\(\s*([^)]*)\s*\)`)
	regexpStorage     = regexp.MustCompile(`(?i)storage\(\s*([^)]*)\s*\)`)
	regexpComment     = regexp.MustCompile(`(?i)comment\(\s*'([^']*(?:'[^']*)*)'\s*\)`)
)

// Parser 定义标签解析器的结构类型
type Parser struct{}

// Parse 解析标签
func (own Parser) Parse(tag string) (*BaseColumn, error) {
	col := &BaseColumn{
		Default:    Metadata{},
		Nullable:   Metadata{},
		Generated:  Metadata{},
		Index:      Metadata{},
		Unique:     Metadata{},
		ForeignKey: nil,
	}

	tag = strings.TrimSpace(tag)
	if tag == "" {
		return col, nil
	}

	// 解析基本的标记
	if err := own.parseBasicTags(stripParamGroups(tag), col); err != nil {
		return nil, err
	}

	// 解析带参数的标记
	if err := own.parseParamTags(tag, col); err != nil {
		return nil, err
	}

	// 解析外键标记
	if err := own.parseForeignKey(tag, col); err != nil {
		return nil, err
	}

	return col, nil
}

/*
stripParamGroups 移除带参数的标记

comment('unique')、check(x is not null) 等标记内部可能包含 index、unique、null 这类词，
直接对整串匹配会把它们误判为无参标记，因此先剔除带参数的标记再解析。
*/
func stripParamGroups(tag string) string {
	groups := []*regexp.Regexp{
		regexpComment,
		regexpCheck,
		regexpDefault,
		regexpGenerated,
		regexpColumn,
		regexpType,
		regexpForeignKey,
		regexpNamedIndex,
		regexpNamedUnique,
		regexpCharset,
		regexpCollate,
		regexpFormat,
		regexpStorage,
	}

	stripped := tag
	for _, group := range groups {
		stripped = group.ReplaceAllString(stripped, " ")
	}

	return stripped
}

// parseBasicTags 解析无参数的基本标记
func (own Parser) parseBasicTags(tag string, col *BaseColumn) error {
	// 解析布尔标记
	col.AutoIncrement = regexpAuto.MatchString(tag)
	col.PrimaryKey = regexpPk.MatchString(tag)
	col.Created = regexpCreated.MatchString(tag)
	col.Updated = regexpUpdated.MatchString(tag)

	// 解析索引和唯一约束
	if regexpIndex.MatchString(tag) {
		col.Index = Metadata{Has: true}
	}

	if regexpUnique.MatchString(tag) {
		col.Unique = Metadata{Has: true}
	}

	// 解析可为空标记
	if regexpNotNull.MatchString(tag) {
		col.Nullable = Metadata{Has: true, Value: "NOT NULL"}
	} else if regexpNull.MatchString(tag) {
		col.Nullable = Metadata{Has: true, Value: "NULL"}
	}

	return nil
}

// parseParamTags 解析带参数的标记
func (Parser) parseParamTags(tag string, col *BaseColumn) error {
	// 解析列名（允许写成 column('name')，引号会被剔除）
	if matches := regexpColumn.FindStringSubmatch(tag); len(matches) > 1 {
		col.Column = trimQuotes(matches[1])
	}

	// 解析数据类型
	if matches := regexpType.FindStringSubmatch(tag); len(matches) > 1 {
		typeStr := strings.TrimSpace(matches[1])
		if strings.HasPrefix(strings.ToLower(typeStr), "enum") {
			// 只把类型关键字大写，枚举取值保持原样：
			// 全量替换会把 'lo-enum' 这类取值改写成 'lo-ENUM'，建表后默认值/写入值将对不上
			col.Datatype = datatype.Datatype(strings.Replace(typeStr, "enum", "ENUM", 1))
		} else {
			col.Datatype = datatype.Datatype(strings.ToUpper(typeStr))
		}
	}

	// 解析默认值
	if matches := regexpDefault.FindStringSubmatch(tag); len(matches) > 1 {
		col.Default = Metadata{Has: true, Value: matches[1]}
	}

	// 解析生成列（表达式原样保留，大写化会改变引用到的列名）
	if matches := regexpGenerated.FindStringSubmatch(tag); len(matches) > 1 {
		col.Generated = Metadata{Has: true, Value: strings.TrimSpace(matches[1])}
	}

	// 解析注释
	if matches := regexpComment.FindStringSubmatch(tag); len(matches) > 1 {
		col.Comment = "'" + strings.ReplaceAll(matches[1], "'", "''") + "'"
	}

	// 解析字符集
	if matches := regexpCharset.FindStringSubmatch(tag); len(matches) > 1 {
		col.Charset = matches[1]
	}

	// 解析排序规则
	if matches := regexpCollate.FindStringSubmatch(tag); len(matches) > 1 {
		col.Collate = matches[1]
	}

	// 解析格式
	if matches := regexpFormat.FindStringSubmatch(tag); len(matches) > 1 {
		col.Format = strings.ToUpper(matches[1])
	}

	// 解析存储
	if matches := regexpStorage.FindStringSubmatch(tag); len(matches) > 1 {
		col.Storage = strings.ToUpper(matches[1])
	}

	// 解析检查约束
	if matches := regexpCheck.FindStringSubmatch(tag); len(matches) > 1 {
		col.Check = matches[1]
	}

	// 解析命名索引
	if matches := regexpNamedIndex.FindStringSubmatch(tag); len(matches) > 1 {
		col.Index = Metadata{Has: true, Value: trimQuotes(matches[1])}
	}

	// 解析命名唯一约束
	if matches := regexpNamedUnique.FindStringSubmatch(tag); len(matches) > 1 {
		col.Unique = Metadata{Has: true, Value: trimQuotes(matches[1])}
	}

	return nil
}

// trimQuotes 剔除标记参数外层可能存在的单/双引号
func trimQuotes(value string) string {
	return strings.Trim(strings.TrimSpace(value), "'\"")
}

/*
parseForeignKey 解析外键标记
  - 格式：fk(group(id),'fk_user_group',odc,ouc)
*/
func (Parser) parseForeignKey(tag string, col *BaseColumn) error {
	matches := regexpForeignKey.FindStringSubmatch(tag)
	if len(matches) <= 1 {
		return nil
	}

	fkStr := matches[1]
	parts := strings.Split(fkStr, ",")
	if len(parts) < 1 {
		return fmt.Errorf("wxorm: invalid foreign key format: %s", fkStr)
	}

	fk := &ForeignKey{}

	// 解析第一个部分：表名(列名)
	firstPart := strings.TrimSpace(parts[0])
	if idx := strings.Index(firstPart, "("); idx > 0 && strings.HasSuffix(firstPart, ")") {
		fk.Table = strings.TrimSpace(firstPart[:idx])
		fk.Column = strings.TrimSpace(firstPart[idx+1 : len(firstPart)-1])
	} else {
		return fmt.Errorf("wxorm: invalid foreign key reference: %s", firstPart)
	}

	// 解析剩余部分
	for i := 1; i < len(parts); i++ {
		part := strings.TrimSpace(parts[i])

		// 解析级联操作
		switch part {
		case "odc":
			fk.OnDeleteCascade = true
		case "ouc":
			fk.OnUpdateCascade = true
		default:
			// 解析外键名称
			if strings.HasPrefix(part, "'") && strings.HasSuffix(part, "'") {
				fk.Name = strings.TrimSpace(part[1 : len(part)-1])
			} else {
				fk.Name = part
			}
		}
	}

	col.ForeignKey = fk
	return nil
}
