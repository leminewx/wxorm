package fakedriver

import (
	"database/sql"
	"testing"
)

// TestDriver_CRUD 验证内存驱动自身的 CRUD 行为，避免测试基础设施本身失真
func TestDriver_CRUD(t *testing.T) {
	Register()

	db, err := sql.Open("mysql", "root:root@tcp(localhost:3306)/test?charset=utf8mb4")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec("CREATE TABLE `users` (`id` INT AUTO_INCREMENT, `name` VARCHAR(50), `age` INT, PRIMARY KEY (`id`))"); err != nil {
		t.Fatalf("create: %v", err)
	}

	res, err := db.Exec("INSERT INTO `users` (`name`, `age`) VALUES (?, ?)", "Tom", 18)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	id, err := res.LastInsertId()
	if err != nil || id != 1 {
		t.Fatalf("lastInsertId = %d, err = %v, want 1", id, err)
	}

	var name string
	var age int
	if err := db.QueryRow("SELECT `id`, `name`, `age` FROM `users` WHERE `id` = ?", id).Scan(&id, &name, &age); err != nil {
		t.Fatalf("select: %v", err)
	}

	if name != "Tom" || age != 18 {
		t.Fatalf("got name=%q age=%d, want Tom/18", name, age)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM `users` WHERE `name` = ?", "Tom").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}

	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}
