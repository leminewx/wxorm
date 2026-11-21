package clause

import (
	"context"
	"strconv"
	"strings"
)

// Context 定义上下文的结构类型
type Context struct {
	ctx      context.Context
	builder  strings.Builder // 全局 SQL 构建器
	args     []any           // 全局参数列表
	argIndex int             // 参数索引
	hasLimit bool            // 是否已写入 LIMIT 子句
}

// NewContext 新建 SQL 构建器上下文
func NewContext() *Context {
	ctx := &Context{
		ctx: context.Background(),
	}
	ctx.builder.Grow(1024)
	return ctx
}

// NewContextFrom 新建携带 context.Context 的 SQL 构建器上下文
func NewContextFrom(ctx context.Context) *Context {
	c := NewContext()
	if ctx != nil {
		c.ctx = ctx
	}

	return c
}

// Write 写入字节数组
func (own *Context) Write(v []byte) error {
	_, err := own.builder.Write(v)
	return err
}

// WriteInt 写入整数
func (own *Context) WriteInt(i int) error {
	_, err := own.builder.WriteString(strconv.FormatInt(int64(i), 10))
	return err
}

// WriteByte 写入字节
func (own *Context) WriteByte(b byte) error {
	return own.builder.WriteByte(b)
}

// WriteString 写入字符串
func (own *Context) WriteString(s string) error {
	_, err := own.builder.WriteString(s)
	return err
}

// WritePlaceholderWithArg 写入占位符，并添加参数
func (own *Context) WritePlaceholderWithArg(cla Clause, arg any) error {
	own.argIndex++
	if _, err := own.builder.WriteString(cla.GetPlaceholder(own.argIndex)); err != nil {
		return err
	}

	own.args = append(own.args, arg)
	return nil
}

// Truncate 从起始位置开始截取指定长度的 SQL 和参数数量
func (own *Context) Truncate(sqlLength, argNumber int) error {
	if sqlLength >= 0 && sqlLength <= own.builder.Len() {
		current := own.builder.String()
		own.builder.Reset()
		if _, err := own.builder.WriteString(current[:sqlLength]); err != nil {
			return err
		}
	}

	own.argIndex = argNumber
	if argNumber >= 0 && argNumber <= len(own.args) {
		own.args = own.args[:argNumber]
	}

	return nil
}

// AddArgs 添加参数
func (own *Context) AddArgs(args ...any) {
	if len(args) == 0 {
		return
	}

	if own.args == nil {
		own.args = make([]any, 0)
	}

	for _, arg := range args {
		own.argIndex++
		own.args = append(own.args, arg)
	}
}

// GetResult 获取构建结果
func (own *Context) GetResult() (string, []any) {
	return own.builder.String(), own.args
}

// GetSQLLengthAndArgNumber 获取已构建查询语句的长度和参数的数量
func (own *Context) GetSQLLengthAndArgNumber() (int, int) {
	return own.builder.Len(), own.argIndex
}

// MarkLimit 标记已写入 LIMIT 子句
func (own *Context) MarkLimit() { own.hasLimit = true }

// HasLimit 判断是否已写入 LIMIT 子句
func (own *Context) HasLimit() bool { return own.hasLimit }

// Reset 重置上下文
func (own *Context) Reset() {
	own.builder.Reset()
	own.args = own.args[:0]
	own.argIndex = 0
	own.hasLimit = false
}
