package gateway

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/1344812937/go-web-quick-start/internal/config"
	"github.com/1344812937/go-web-quick-start/pkg/core/tx"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const DetailedLogRetentionDays = 5

type Store struct {
	db            *gorm.DB
	secretBox     *SecretBox
	configManager *config.ApplicationConfigManager
}

func NewStore(dataSource *tx.DataSource, configManager *config.ApplicationConfigManager) *Store {
	secretBox, err := NewSecretBox(os.Getenv("GATEWAY_MASTER_KEY"))
	if err != nil {
		panic(err)
	}
	store := &Store{
		db:            dataSource.Db(),
		secretBox:     secretBox,
		configManager: configManager,
	}
	if err := store.migrate(); err != nil {
		panic(fmt.Sprintf("migrate gateway database: %v", err))
	}
	if err := store.bootstrapAdmin(); err != nil {
		panic(fmt.Sprintf("bootstrap gateway administrator: %v", err))
	}
	store.cleanupExpired()
	go store.runCleanup()
	return store
}

func (s *Store) DB() *gorm.DB {
	return s.db
}

func (s *Store) SecretBox() *SecretBox {
	return s.secretBox
}

func (s *Store) migrate() error {
	if err := s.db.AutoMigrate(
		&AdminUser{},
		&AdminSession{},
		&Channel{},
		&GatewayModel{},
		&ChannelModel{},
		&ClientToken{},
		&ClientTokenModel{},
		&RelayRequestLog{},
		&RelayAttemptLog{},
		&TokenDailyStat{},
		&GatewayMigration{},
		&ResponseAffinity{},
		&SessionAffinity{},
	); err != nil {
		return err
	}
	return s.backfillTokenDailyStats()
}

func (s *Store) backfillTokenDailyStats() error {
	const migrationName = "token_daily_stats_v1"
	return s.db.Transaction(func(db *gorm.DB) error {
		var migration GatewayMigration
		err := db.First(&migration, "name = ?", migrationName).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var stats []TokenDailyStat
		if err := db.Model(&RelayRequestLog{}).Select(
			"date(created_at) AS date, token_id, COUNT(*) AS request_count, " +
				"SUM(CASE WHEN status_code BETWEEN 200 AND 299 THEN 1 ELSE 0 END) AS success_count, " +
				"COALESCE(SUM(input_tokens),0) AS input_tokens, COALESCE(SUM(output_tokens),0) AS output_tokens, " +
				"COALESCE(SUM(cached_tokens),0) AS cached_tokens, COALESCE(SUM(estimated_cost),0) AS estimated_cost, " +
				"COALESCE(SUM(duration_ms),0) AS duration_ms, COALESCE(SUM(attempt_count),0) AS attempt_count",
		).Group("date(created_at), token_id").Scan(&stats).Error; err != nil {
			return err
		}
		if len(stats) > 0 {
			if err := db.Create(&stats).Error; err != nil {
				return err
			}
		}
		return db.Create(&GatewayMigration{Name: migrationName, AppliedAt: time.Now()}).Error
	})
}

func (s *Store) bootstrapAdmin() error {
	var count int64
	if err := s.db.Model(&AdminUser{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	username := strings.TrimSpace(os.Getenv("GATEWAY_ADMIN_USERNAME"))
	password := os.Getenv("GATEWAY_ADMIN_PASSWORD")
	if username == "" || password == "" {
		return errorsForBootstrap()
	}
	if len(password) < 12 {
		return fmt.Errorf("GATEWAY_ADMIN_PASSWORD must contain at least 12 characters")
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	return s.db.Create(&AdminUser{
		Username:     username,
		PasswordHash: string(passwordHash),
		Enabled:      true,
	}).Error
}

func errorsForBootstrap() error {
	return fmt.Errorf("GATEWAY_ADMIN_USERNAME and GATEWAY_ADMIN_PASSWORD are required when no administrator exists")
}

func (s *Store) cleanupExpired() {
	now := time.Now()
	_ = s.db.Where("expires_at < ?", now).Delete(&AdminSession{}).Error
	_ = s.db.Where("expires_at < ?", now).Delete(&ResponseAffinity{}).Error
	_ = s.db.Where("expires_at < ?", now).Delete(&SessionAffinity{}).Error
	cutoff := now.Add(-DetailedLogRetentionDays * 24 * time.Hour)
	_ = s.db.Where("created_at < ?", cutoff).Delete(&RelayAttemptLog{}).Error
	_ = s.db.Where("created_at < ?", cutoff).Delete(&RelayRequestLog{}).Error
}

func (s *Store) runCleanup() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		s.cleanupExpired()
	}
}
