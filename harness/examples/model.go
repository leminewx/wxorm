package examples

import (
	"database/sql"
	"time"

	"gitee.com/leminewx/wxorm/harness/database"
)

// 测试数据库连接信息，来自 harness/testdata/database.json
var (
	DBName   = "test"
	DBSource = "root:root@tcp(127.0.0.1:3306)/test?charset=utf8mb4&parseTime=true&loc=UTC"
)

func init() {
	config, err := database.Load()
	if err != nil {
		panic("examples: 读取测试数据失败: " + err.Error())
	}

	if config.Fake.Name != "" {
		DBName = config.Fake.Name
	}

	if config.Fake.DSN != "" {
		DBSource = config.Fake.DSN
	}
}

// SetupTestDB 建立数据库连接，并设置连接池
//
// 不依赖 testing 包，避免示例包把测试依赖带进生产构建；调用方自行处理错误。
func SetupTestDB() (*sql.DB, error) {
	db, err := sql.Open("mysql", DBSource)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

type Group struct {
	ID        int64     `orm:"column(id) pk auto comment('组ID')"`
	Name      string    `wxorm:"notnull comment('组名称')"`
	CreatedBy string    `orm:"type(varchar(64)) comment('创建人')"`
	CreatedAt time.Time `wxorm:"created comment('创建时间')"`
}

func (own *Group) GetTableName() string {
	return "groups"
}

type User struct {
	ID        int    `orm:"column(id) type(int) pk auto comment('用户ID')"`
	Name      string `wxorm:"column(name) type(varchar(50)) comment('用户名')"`
	Age       int    `orm:"column(age) type(int) default(0) comment('年龄')"`
	Email     string `wxorm:"column(email) type(varchar(100)) unique comment('邮箱')"`
	createdAt string `orm:"-"` // 不解析的字段（非导出+无标签）
	UpdatedAt string `wxorm:"column(updated_at) type(timestamp) created updated comment('更新时间')"`
	GroupID   int64  `orm:"column(group_id) type(bigint) fk(groups(id),'fk_user_group') comment('组ID')"`
}

func (own *User) GetTableName() string {
	return "users"
}

type Filter struct {
	ExportedField   int     `wxorm:"column(exported)"`   // 可导出字段
	unexportedField float64 `wxorm:"column(unexported)"` // 非导出字段
	UnexportedField bool    `wxorm:"-"`                  // 非导出字段
	NoTagField      string  // 不可导出字段
}

func (own *Filter) GetTableName() string {
	return "models"
}
