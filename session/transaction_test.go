package session

import (
	"fmt"
	"testing"

	"gitee.com/leminewx/wxorm/harness/examples"
)

// TestSession_Begin_Success 测试成功开始事务
func TestSession_Begin_Success(t *testing.T) {
	group, user := setupTestDB(t)
	defer func() {
		if err := recover(); err != nil {
			t.Error(err)
		}

		teardownTestDB(t, group, user)
	}()

	// 开始事务
	err := user.Begin()
	if err != nil {
		t.Errorf("Begin failed: %v", err)
	}

	// 检查事务是否已开始
	if user.tx == nil {
		t.Error("Transaction should be started but it's nil")
	}

	// 在事务中执行操作
	var _ = examples.User{}
	sqlStr := fmt.Sprintf("INSERT INTO %s (`name`, `age`, `email`, `group_id`) VALUES (?, ?, ?, ?)", user.schema.TableName)
	result, err := user.Exec(sqlStr, "John Doe", 18, "john@example.com", 1)
	if err != nil {
		t.Errorf("Exec in transaction failed: %v", err)
	}

	if lastId, err := result.LastInsertId(); err != nil {
		t.Errorf("LastInsertId failed: %v", err)
	} else if lastId != 1 {
		t.Errorf("LastInsertId failed: %v", lastId)
	}
}

// TestSession_Commit_Success 测试成功提交事务
func TestSession_Commit_Success(t *testing.T) {
	_, user := setupTestDB(t) // 测试后，手动删除

	// 开始事务
	err := user.Begin()
	if err != nil {
		t.Fatalf("Begin failed: %v", err)
	}

	// 在事务中插入数据
	var _ = examples.User{}
	sqlStr := fmt.Sprintf("INSERT INTO %s (`name`, `age`, `email`, `group_id`) VALUES (?, ?, ?, ?)", user.schema.TableName)
	if _, err = user.Exec(sqlStr, "John Doe", 18, "john@example.com", 1); err != nil {
		t.Fatalf("Exec in transaction failed: %v", err)
	}

	// 提交事务
	if err = user.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
}
