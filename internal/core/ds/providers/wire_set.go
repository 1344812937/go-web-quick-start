package providers

import (
	"os"
	"path/filepath"
	"tank-tool/pkg/core/tx"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func NewMultiDataSource() *tx.MultiDataSource {
	multiDataSource := tx.GetMultiDataSource()
	primaryDS := getPrimaryDS()
	multiDataSource.Register(primaryDS)
	return multiDataSource
}

// GetPrimaryDataSource 从 MultiDataSource 获取主数据源，供 Wire 注入使用。
func GetPrimaryDataSource(mds *tx.MultiDataSource) *tx.DataSource {
	return mds.GetSource("primary")
}

var primaryDBPath = filepath.Join(".", "data", "data.db")

func PrimaryDBPath() string {
	return primaryDBPath
}

func getPrimaryDS() *tx.DataSource {
	dbDir := filepath.Dir(primaryDBPath)
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		panic("failed to create database directory: " + err.Error())
	}
	db, err := gorm.Open(sqlite.Open(primaryDBPath), &gorm.Config{})
	if err != nil {
		panic("failed to connect database: " + err.Error())
	}
	if err := configurePrimarySQLite(db); err != nil {
		panic("failed to configure sqlite pragmas: " + err.Error())
	}
	return tx.CreateDataSource("primary", db)
}

func configurePrimarySQLite(db *gorm.DB) error {
	mode, err := queryPrimaryAutoVacuumMode(db)
	if err != nil {
		return err
	}
	if mode == 1 {
		return nil
	}
	if err := db.Exec("PRAGMA auto_vacuum = FULL").Error; err != nil {
		return err
	}
	hasTables, err := hasPrimaryUserTables(db)
	if err != nil {
		return err
	}
	if hasTables {
		if err := db.Exec("VACUUM").Error; err != nil {
			return err
		}
	}
	mode, err = queryPrimaryAutoVacuumMode(db)
	if err != nil {
		return err
	}
	if mode != 1 {
		return gorm.ErrInvalidDB
	}
	return nil
}

func queryPrimaryAutoVacuumMode(db *gorm.DB) (int, error) {
	var mode int
	if err := db.Raw("PRAGMA auto_vacuum").Scan(&mode).Error; err != nil {
		return 0, err
	}
	return mode, nil
}

func hasPrimaryUserTables(db *gorm.DB) (bool, error) {
	var count int64
	err := db.Raw("SELECT COUNT(1) FROM sqlite_master WHERE type = ? AND name NOT LIKE ?", "table", "sqlite_%").Scan(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
