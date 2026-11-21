package column

import (
	"fmt"
	"testing"

	"gitee.com/leminewx/wxorm/dialect/clause"
)

func TestMySQLBuildCreateSQL(t *testing.T) {
	tags := []string{
		"column(id) type(int) pk auto comment('ID')",
		"column(name) type(varchar(30)) notnull index(index_name1) default('') comment('用户名')",
		"column(age) type(smallint) notnull unique(unique_name1) default(0) comment('年龄') collate(utf8_unicode_ci)",
		"column(sex) type(enum('boy','girl')) notnull comment('性别')",
		"column(email) type(varchar(255)) notnull unique(unique_name2) default('') comment('邮箱地址') collate(utf8_unicode_ci)",
		"column(role_id) type(bigint(32)) index(index_name1) fk(role(id),'fk_user_role',odc) comment('所属角色')",
		"column(auth_id) type(bigint(32)) index(index_name2) fk(group(auth_id),'fk_user_group',odc) pk comment('所属组权限')",
		"column(group_id) type(bigint(32)) unique(unique_name2) fk(group(id),'fk_user_group',odc,ouc) comment('所属组')",
		"column(register_at) type(timestamp) created comment('注册时间')",
		"column(login_at) type(datetime) created updated comment('登录时间')",
		"column(logout_at) type(datetime) updated comment('登录时间')",
	}

	ctx := clause.NewContext()
	for _, tag := range tags {
		col, err := NewMySQLColumn(tag)
		if err != nil {
			t.Fatal(err)
		}

		if err := col.BuildCreateSQL(ctx); err != nil {
			t.Fatal(err)
		}

		fmt.Println(ctx.GetResult())
		ctx.Reset()
	}
}
