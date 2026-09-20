// Package database 封装 GORM 数据层。
// 每张表一个文件放在本包，建表走 AutoMigrate，首次安装由 Install() 统一调用。
package database

import (
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// DB 持有 GORM 句柄，封装连接与首次建表。
// driver 记录当前驱动标识（sqlite / mysql），供 FTS 等驱动相关逻辑分流。
type DB struct {
	*gorm.DB
	driver string
}

// Open 建立数据库连接。M1 仅支持 sqlite（默认）；mysql 留待后续里程碑接入。
// sqlite 走注册了 bigram SQL 函数的自定义驱动（见 entry_fts.go），供全文检索切词。
func Open(driver, dsn string) (*DB, error) {
	var dialector gorm.Dialector
	switch driver {
	case "sqlite":
		dialector = sqlite.New(sqlite.Config{DriverName: bigramDriverName, DSN: dsn})
	case "mysql":
		return nil, fmt.Errorf("database driver %q not implemented yet", driver)
	default:
		return nil, fmt.Errorf("unknown database driver %q", driver)
	}
	gdb, err := gorm.Open(dialector, &gorm.Config{
		Logger: gormLogger(),
	})
	if err != nil {
		return nil, err
	}
	return &DB{DB: gdb, driver: driver}, nil
}

// Install 首次建表。新增实体必须在此登记，勿在业务代码里散落 AutoMigrate。
// 建表后执行一次性迁移（如把旧版 entries.read_later 搬到 entry_state）。
// FTS5 是 sqlite 专属，仅在 sqlite 驱动下落地；mysql 的全文检索到阶段 20 单独迁移。
func (d *DB) Install() error {
	if err := d.AutoMigrate(&Source{}, &Entry{}, &EntryState{}, &SourceCursor{}, &FetchLog{}); err != nil {
		return err
	}
	if err := d.migrateReadLater(); err != nil {
		return err
	}
	if d.driver == "sqlite" {
		return d.installFTS()
	}
	return nil
}
