// Package database 封装 GORM 数据层。
// 每张表一个文件放在本包，建表走 AutoMigrate，首次安装由 Install() 统一调用。
package database

import (
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB 持有 GORM 句柄，封装连接与首次建表。
type DB struct {
	*gorm.DB
}

// Open 建立数据库连接。M1 仅支持 sqlite（默认）；mysql 留待后续里程碑接入。
func Open(driver, dsn string) (*DB, error) {
	var dialector gorm.Dialector
	switch driver {
	case "sqlite":
		dialector = sqlite.Open(dsn)
	case "mysql":
		return nil, fmt.Errorf("database driver %q not implemented yet", driver)
	default:
		return nil, fmt.Errorf("unknown database driver %q", driver)
	}
	gdb, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	return &DB{gdb}, nil
}

// Install 首次建表。新增实体必须在此登记，勿在业务代码里散落 AutoMigrate。
// 建表后执行一次性迁移（如把旧版 entries.read_later 搬到 entry_state）。
func (d *DB) Install() error {
	if err := d.AutoMigrate(&Source{}, &Entry{}, &EntryState{}, &SourceCursor{}, &FetchLog{}); err != nil {
		return err
	}
	return d.migrateReadLater()
}
