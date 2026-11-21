package integration

import (
	"time"

	"gitee.com/leminewx/wxorm/session"
)

// Account 是三个方言共用的账户模型。
//
// 列定义只依赖 Go 类型映射，不写 type(...) 标签，这样同一份模型能同时建在
// MySQL、PostgreSQL 与 SQLite 上，验证的是方言层的默认类型映射。
type Account struct {
	ID        int64     `orm:"column(id) pk auto comment('主键')"`
	Name      string    `orm:"column(name) notnull comment('姓名')"`
	Email     string    `orm:"column(email) unique comment('邮箱')"`
	Age       int       `orm:"column(age) index default(0) comment('年龄')"`
	Balance   float64   `orm:"column(balance) default(0) comment('余额')"`
	Active    bool      `orm:"column(active) default(true) comment('是否启用')"`
	Payload   []byte    `orm:"column(payload) null comment('二进制载荷')"`
	Note      string    `orm:"column(note) null comment('备注')"`
	CreatedAt time.Time `orm:"column(created_at) created comment('创建时间')"`
	UpdatedAt time.Time `orm:"column(updated_at) updated comment('更新时间')"`
}

// GetTableName 返回账户表名
func (*Account) GetTableName() string {
	return tableName("account")
}

// Hooked 用于验证 8 个钩子的调用顺序与次数
type Hooked struct {
	ID   int64  `orm:"column(id) pk auto"`
	Name string `orm:"column(name)"`

	BeforeInsert int `orm:"-"`
	AfterInsert  int `orm:"-"`
	BeforeUpdate int `orm:"-"`
	AfterUpdate  int `orm:"-"`
	BeforeDelete int `orm:"-"`
	AfterDelete  int `orm:"-"`
	BeforeSelect int `orm:"-"`
	AfterSelect  int `orm:"-"`
}

// GetTableName 返回钩子表名
func (*Hooked) GetTableName() string {
	return tableName("hooked")
}

func (own *Hooked) CallBeforeInsert(_ *session.Session) error { own.BeforeInsert++; return nil }
func (own *Hooked) CallAfterInsert(_ *session.Session) error  { own.AfterInsert++; return nil }
func (own *Hooked) CallBeforeUpdate(_ *session.Session) error { own.BeforeUpdate++; return nil }
func (own *Hooked) CallAfterUpdate(_ *session.Session) error  { own.AfterUpdate++; return nil }
func (own *Hooked) CallBeforeDelete(_ *session.Session) error { own.BeforeDelete++; return nil }
func (own *Hooked) CallAfterDelete(_ *session.Session) error  { own.AfterDelete++; return nil }
func (own *Hooked) CallBeforeSelect(_ *session.Session) error { own.BeforeSelect++; return nil }
func (own *Hooked) CallAfterSelect(_ *session.Session) error  { own.AfterSelect++; return nil }
