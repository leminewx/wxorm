package session

import (
	"database/sql"
	"log"
	"testing"

	"gitee.com/leminewx/wxorm/dialect"
	"gitee.com/leminewx/wxorm/harness/examples"
	"gitee.com/leminewx/wxorm/schema"
)

type account struct {
	ID       int `orm:"column(id) type(int) pk comment('用户ID')"`
	Password string
}

func (own *account) GetTableName() string {
	return "accounts"
}

func (own *account) CallBeforeInsert(_ *Session) error {
	own.ID += 1000
	log.Println("before inert:", own)
	return nil
}

func (own *account) CallAfterSelect(_ *Session) error {
	own.Password = "******"
	log.Println("after query", own)
	return nil
}

func TestSession_CallMethod(t *testing.T) {
	// 创建方言实例
	testDialect = dialect.Get("mysql")

	// 创建数据库连接
	var err error
	if testDB, err = sql.Open(testDialect.GetDriverName(), examples.DBSource); err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// 解析 accounts 表
	testAccountSchema, err := schema.Parse(testDialect, &account{})
	if err != nil {
		t.Fatalf("Failed to parse 'accounts' schema: %v", err)
	}

	// 创建 accounts 表
	sess := NewSession(0, examples.DBName, testDB, testDialect, nil)
	sess.SetSchema(testAccountSchema)
	sess.SetDebug(false)
	_ = sess.DeleteTable()
	if err := sess.CreateTable(); err != nil {
		t.Fatal(err)
	}

	_, _ = sess.INSERT(false, &account{1, "123456"}, &account{2, "qwerty"})

	a := &account{}
	if res, err := sess.SELECT(a); err != nil || a.ID != 1001 || a.Password != "******" {
		t.Fatalf("Failed to call hooks after query, got: %v, error: %v", a, err)
	} else {
		t.Log(res.GetExecutionSQL())
		t.Log(res.GetExecutionCost())
	}
}
