package gateway

import (
	"errors"
	"strings"
	"time"

	"github.com/1344812937/go-web-quick-start/internal/config"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var (
	ErrInvalidCredentials = errors.New("invalid administrator credentials")
	ErrInvalidSession     = errors.New("invalid or expired administrator session")
)

type AdminAuthService struct {
	store         *Store
	configManager *config.ApplicationConfigManager
}

func NewAdminAuthService(store *Store, configManager *config.ApplicationConfigManager) *AdminAuthService {
	return &AdminAuthService{store: store, configManager: configManager}
}

func (s *AdminAuthService) Login(username string, password string) (string, *AdminUser, error) {
	var user AdminUser
	err := s.store.db.Where("username = ? AND enabled = ?", strings.TrimSpace(username), true).First(&user).Error
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return "", nil, ErrInvalidCredentials
	}
	rawToken, err := generateSecret("")
	if err != nil {
		return "", nil, err
	}
	now := time.Now()
	ttl := s.currentSessionTTL()
	session := AdminSession{
		TokenHash: hashSecret(rawToken),
		UserID:    user.ID,
		ExpiresAt: sessionExpiresAt(now, ttl),
		CreatedAt: now,
		LastSeen:  now,
	}
	if err := s.store.db.Create(&session).Error; err != nil {
		return "", nil, err
	}
	return rawToken, &user, nil
}

func (s *AdminAuthService) Authenticate(rawToken string) (*AdminUser, error) {
	if strings.TrimSpace(rawToken) == "" {
		return nil, ErrInvalidSession
	}
	var session AdminSession
	now := time.Now()
	err := s.store.db.Where("token_hash = ?", hashSecret(rawToken)).First(&session).Error
	if err != nil {
		return nil, ErrInvalidSession
	}
	ttl := s.currentSessionTTL()
	if ttl > 0 && !session.CreatedAt.Add(ttl).After(now) {
		_ = s.store.db.Delete(&session).Error
		return nil, ErrInvalidSession
	}
	var user AdminUser
	if err := s.store.db.Where("id = ? AND enabled = ?", session.UserID, true).First(&user).Error; err != nil {
		return nil, ErrInvalidSession
	}
	_ = s.store.db.Model(&session).Updates(map[string]any{
		"expires_at": sessionExpiresAt(session.CreatedAt, ttl),
		"last_seen":  now,
	}).Error
	return &user, nil
}

func sessionExpiresAt(createdAt time.Time, ttl time.Duration) time.Time {
	if ttl <= 0 {
		return time.Time{}
	}
	return createdAt.Add(ttl)
}

func (s *AdminAuthService) currentSessionTTL() time.Duration {
	if s.configManager == nil {
		return 0
	}
	return config.EffectiveSessionTTL(s.configManager.GetConfig())
}

func (s *AdminAuthService) Logout(rawToken string) error {
	if strings.TrimSpace(rawToken) == "" {
		return nil
	}
	return s.store.db.Where("token_hash = ?", hashSecret(rawToken)).Delete(&AdminSession{}).Error
}

func (s *AdminAuthService) ChangePassword(userID uint64, currentPassword string, nextPassword string) error {
	if len(nextPassword) < 12 {
		return errors.New("new password must contain at least 12 characters")
	}
	var user AdminUser
	if err := s.store.db.First(&user, userID).Error; err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)) != nil {
		return ErrInvalidCredentials
	}
	nextHash, err := bcrypt.GenerateFromPassword([]byte(nextPassword), 12)
	if err != nil {
		return err
	}
	return s.store.db.Transaction(func(db *gorm.DB) error {
		if err := db.Model(&user).Update("password_hash", string(nextHash)).Error; err != nil {
			return err
		}
		return db.Where("user_id = ?", user.ID).Delete(&AdminSession{}).Error
	})
}
