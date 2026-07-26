package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	maxUpstreamModelsResponseBytes = 4 << 20
	channelLatencyPointLimit       = 48
)

type ChannelInput struct {
	Name                string `json:"name"`
	BaseURL             string `json:"baseUrl"`
	APIKey              string `json:"apiKey"`
	Enabled             bool   `json:"enabled"`
	SupportsStreamUsage bool   `json:"supportsStreamUsage"`
}

type ChannelModelDiscoveryInput struct {
	ChannelID uint64 `json:"channelId"`
	BaseURL   string `json:"baseUrl"`
	APIKey    string `json:"apiKey"`
}

type UpstreamModelView struct {
	ID      string `json:"id"`
	OwnedBy string `json:"ownedBy"`
	Created int64  `json:"created"`
}

type ChannelModelDiscovery struct {
	Models    []UpstreamModelView `json:"models"`
	LatencyMS int64               `json:"latencyMs"`
	Status    int                 `json:"status"`
	FetchedAt time.Time           `json:"fetchedAt"`
}

type ChannelView struct {
	Channel
	APIKeyConfigured bool           `json:"apiKeyConfigured"`
	Models           []ChannelModel `json:"models"`
	Metrics          ChannelMetrics `json:"metrics"`
}

type ChannelLatencyPoint struct {
	RecordedAt time.Time `json:"recordedAt"`
	LatencyMS  int64     `json:"latencyMs"`
}

type ChannelMetrics struct {
	LatencySeries      []ChannelLatencyPoint `json:"latencySeries"`
	LatestLatencyMS    int64                 `json:"latestLatencyMs"`
	LatencySampleCount int64                 `json:"latencySampleCount"`
	InputTokens        int64                 `json:"inputTokens"`
	CachedTokens       int64                 `json:"cachedTokens"`
	CacheHitRate       float64               `json:"cacheHitRate"`
}

type GatewayModelInput struct {
	Name            string `json:"name"`
	RoutingStrategy string `json:"routingStrategy"`
	Enabled         bool   `json:"enabled"`
}

type ChannelModelInput struct {
	ModelID                uint64 `json:"modelId"`
	UpstreamModel          string `json:"upstreamModel"`
	Priority               int    `json:"priority"`
	Weight                 int    `json:"weight"`
	InputPriceMicros       int64  `json:"inputPriceMicros"`
	OutputPriceMicros      int64  `json:"outputPriceMicros"`
	CachedInputPriceMicros *int64 `json:"cachedInputPriceMicros"`
	Enabled                bool   `json:"enabled"`
}

type ClientTokenInput struct {
	Name           string   `json:"name"`
	Enabled        bool     `json:"enabled"`
	AllowAllModels bool     `json:"allowAllModels"`
	RPM            int      `json:"rpm"`
	MaxConcurrency int      `json:"maxConcurrency"`
	ModelIDs       []uint64 `json:"modelIds"`
}

type ClientTokenView struct {
	ClientToken
	ModelIDs   []uint64        `json:"modelIds"`
	Statistics TokenStatistics `json:"statistics"`
}

type TokenStatistics struct {
	Requests       int64   `json:"requests"`
	Successes      int64   `json:"successes"`
	InputTokens    int64   `json:"inputTokens"`
	OutputTokens   int64   `json:"outputTokens"`
	CachedTokens   int64   `json:"cachedTokens"`
	EstimatedCost  int64   `json:"estimatedCostMicros"`
	AverageLatency float64 `json:"averageLatencyMs"`
	Attempts       int64   `json:"attempts"`
}

type IssuedClientToken struct {
	Token  ClientTokenView `json:"token"`
	Secret string          `json:"secret"`
}

type DashboardSummary struct {
	Requests       int64                `json:"requests"`
	SuccessRate    float64              `json:"successRate"`
	InputTokens    int64                `json:"inputTokens"`
	OutputTokens   int64                `json:"outputTokens"`
	EstimatedCost  int64                `json:"estimatedCostMicros"`
	AverageLatency float64              `json:"averageLatencyMs"`
	Daily          []DashboardDaily     `json:"daily"`
	Channels       []DashboardBreakdown `json:"channels"`
	Models         []DashboardBreakdown `json:"models"`
}

type DashboardDaily struct {
	Date          string `json:"date"`
	Requests      int64  `json:"requests"`
	Successes     int64  `json:"successes"`
	InputTokens   int64  `json:"inputTokens"`
	OutputTokens  int64  `json:"outputTokens"`
	EstimatedCost int64  `json:"estimatedCostMicros"`
}

type DashboardBreakdown struct {
	Name          string `json:"name"`
	Requests      int64  `json:"requests"`
	EstimatedCost int64  `json:"estimatedCostMicros"`
}

type LogQuery struct {
	Model      string
	StatusCode int
	TokenID    uint64
	ChannelID  uint64
	From       time.Time
	To         time.Time
	Page       int
	PageSize   int
}

type RelayRequestView struct {
	RelayRequestLog
	RequestParameters map[string]any    `json:"requestParameters"`
	Attempts          []RelayAttemptLog `json:"attempts"`
}

type LogPage struct {
	Items    []RelayRequestView `json:"items"`
	Total    int64              `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"pageSize"`
}

type ManagementService struct {
	store *Store
}

func NewManagementService(store *Store) *ManagementService {
	return &ManagementService{store: store}
}

func normalizeBaseURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("base URL must be an absolute HTTP or HTTPS URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("base URL must use HTTP or HTTPS")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("base URL cannot contain user info, query parameters, or fragments")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return strings.TrimRight(parsed.String(), "/"), nil
}

func validateChannelInput(input ChannelInput, requireKey bool) (ChannelInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return input, errors.New("channel name is required")
	}
	baseURL, err := normalizeBaseURL(input.BaseURL)
	if err != nil {
		return input, err
	}
	input.BaseURL = baseURL
	if requireKey && strings.TrimSpace(input.APIKey) == "" {
		return input, errors.New("channel API key is required")
	}
	return input, nil
}

func (s *ManagementService) ListChannels(ctx context.Context) ([]ChannelView, error) {
	var channels []Channel
	if err := s.store.db.WithContext(ctx).Order("id desc").Find(&channels).Error; err != nil {
		return nil, err
	}
	channelIDs := make([]uint64, 0, len(channels))
	for _, channel := range channels {
		channelIDs = append(channelIDs, channel.ID)
	}
	metricsByChannel, err := s.channelMetrics(ctx, channelIDs)
	if err != nil {
		return nil, err
	}
	views := make([]ChannelView, 0, len(channels))
	for _, channel := range channels {
		var models []ChannelModel
		if err := s.store.db.WithContext(ctx).Where("channel_id = ?", channel.ID).Order("priority desc, id asc").Find(&models).Error; err != nil {
			return nil, err
		}
		views = append(views, ChannelView{
			Channel:          channel,
			APIKeyConfigured: channel.APIKeyCipher != "",
			Models:           models,
			Metrics:          metricsByChannel[channel.ID],
		})
	}
	return views, nil
}

func (s *ManagementService) channelMetrics(ctx context.Context, channelIDs []uint64) (map[uint64]ChannelMetrics, error) {
	metricsByChannel := make(map[uint64]ChannelMetrics, len(channelIDs))
	for _, channelID := range channelIDs {
		metricsByChannel[channelID] = ChannelMetrics{LatencySeries: []ChannelLatencyPoint{}}
	}
	if len(channelIDs) == 0 {
		return metricsByChannel, nil
	}

	cutoff := time.Now().Add(-DetailedLogRetentionDays * 24 * time.Hour)
	type channelAggregate struct {
		ChannelID          uint64
		LatencySampleCount int64
		InputTokens        int64
		CachedTokens       int64
	}
	var aggregates []channelAggregate
	if err := s.store.db.WithContext(ctx).Model(&RelayAttemptLog{}).
		Select("channel_id, SUM(CASE WHEN latency_ms > 0 THEN 1 ELSE 0 END) AS latency_sample_count, "+
			"COALESCE(SUM(CASE WHEN usage_source = 'upstream' THEN input_tokens ELSE 0 END), 0) AS input_tokens, "+
			"COALESCE(SUM(CASE WHEN usage_source = 'upstream' THEN cached_tokens ELSE 0 END), 0) AS cached_tokens").
		Where("channel_id IN ? AND created_at >= ? AND success = ?", channelIDs, cutoff, true).
		Group("channel_id").Scan(&aggregates).Error; err != nil {
		return nil, err
	}
	for _, aggregate := range aggregates {
		metrics := metricsByChannel[aggregate.ChannelID]
		metrics.LatencySampleCount = aggregate.LatencySampleCount
		metrics.InputTokens = max(aggregate.InputTokens, 0)
		metrics.CachedTokens = min(max(aggregate.CachedTokens, 0), metrics.InputTokens)
		if metrics.InputTokens > 0 {
			metrics.CacheHitRate = float64(metrics.CachedTokens) / float64(metrics.InputTokens)
		}
		metricsByChannel[aggregate.ChannelID] = metrics
	}

	type latencyRow struct {
		ChannelID  uint64
		LatencyMS  int64
		RecordedAt time.Time `gorm:"column:created_at"`
	}
	var rows []latencyRow
	if err := s.store.db.WithContext(ctx).Raw(`
		SELECT channel_id, latency_ms, created_at
		FROM (
			SELECT channel_id, latency_ms, created_at, id,
				ROW_NUMBER() OVER (PARTITION BY channel_id ORDER BY created_at DESC, id DESC) AS row_number
			FROM relay_attempt_logs
			WHERE channel_id IN ? AND created_at >= ? AND success = ? AND latency_ms > 0
		) AS recent_latency
		WHERE row_number <= ?
		ORDER BY channel_id ASC, created_at ASC, id ASC
	`, channelIDs, cutoff, true, channelLatencyPointLimit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		metrics := metricsByChannel[row.ChannelID]
		metrics.LatencySeries = append(metrics.LatencySeries, ChannelLatencyPoint{
			RecordedAt: row.RecordedAt,
			LatencyMS:  row.LatencyMS,
		})
		metrics.LatestLatencyMS = row.LatencyMS
		metricsByChannel[row.ChannelID] = metrics
	}
	return metricsByChannel, nil
}

func (s *ManagementService) CreateChannel(ctx context.Context, input ChannelInput) (*ChannelView, error) {
	input, err := validateChannelInput(input, true)
	if err != nil {
		return nil, err
	}
	cipherText, err := s.store.secretBox.Encrypt(strings.TrimSpace(input.APIKey))
	if err != nil {
		return nil, err
	}
	channel := Channel{
		Name:                input.Name,
		BaseURL:             input.BaseURL,
		APIKeyCipher:        cipherText,
		Enabled:             input.Enabled,
		SupportsStreamUsage: input.SupportsStreamUsage,
	}
	if err := s.store.db.WithContext(ctx).Create(&channel).Error; err != nil {
		return nil, err
	}
	return &ChannelView{
		Channel:          channel,
		APIKeyConfigured: true,
		Models:           []ChannelModel{},
		Metrics:          ChannelMetrics{LatencySeries: []ChannelLatencyPoint{}},
	}, nil
}

func (s *ManagementService) UpdateChannel(ctx context.Context, id uint64, input ChannelInput) (*ChannelView, error) {
	input, err := validateChannelInput(input, false)
	if err != nil {
		return nil, err
	}
	var channel Channel
	if err := s.store.db.WithContext(ctx).First(&channel, id).Error; err != nil {
		return nil, err
	}
	channel.Name = input.Name
	channel.BaseURL = input.BaseURL
	channel.Enabled = input.Enabled
	channel.SupportsStreamUsage = input.SupportsStreamUsage
	if strings.TrimSpace(input.APIKey) != "" {
		channel.APIKeyCipher, err = s.store.secretBox.Encrypt(strings.TrimSpace(input.APIKey))
		if err != nil {
			return nil, err
		}
	}
	if err := s.store.db.WithContext(ctx).Save(&channel).Error; err != nil {
		return nil, err
	}
	var models []ChannelModel
	_ = s.store.db.WithContext(ctx).Where("channel_id = ?", channel.ID).Find(&models).Error
	metricsByChannel, err := s.channelMetrics(ctx, []uint64{channel.ID})
	if err != nil {
		return nil, err
	}
	return &ChannelView{
		Channel:          channel,
		APIKeyConfigured: channel.APIKeyCipher != "",
		Models:           models,
		Metrics:          metricsByChannel[channel.ID],
	}, nil
}

func (s *ManagementService) DeleteChannel(ctx context.Context, id uint64) error {
	return s.store.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		if err := db.Where("channel_id = ?", id).Delete(&ChannelModel{}).Error; err != nil {
			return err
		}
		result := db.Delete(&Channel{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func (s *ManagementService) ReplaceChannelModels(ctx context.Context, channelID uint64, inputs []ChannelModelInput) ([]ChannelModel, error) {
	var channel Channel
	if err := s.store.db.WithContext(ctx).First(&channel, channelID).Error; err != nil {
		return nil, err
	}
	models := make([]ChannelModel, 0, len(inputs))
	seen := make(map[uint64]struct{}, len(inputs))
	for _, input := range inputs {
		if _, ok := seen[input.ModelID]; ok {
			return nil, fmt.Errorf("model %d is duplicated", input.ModelID)
		}
		seen[input.ModelID] = struct{}{}
		if strings.TrimSpace(input.UpstreamModel) == "" {
			return nil, errors.New("upstream model is required")
		}
		if input.Weight < 1 || input.Weight > 10000 {
			return nil, errors.New("weight must be between 1 and 10000")
		}
		if input.InputPriceMicros < 0 || input.OutputPriceMicros < 0 || (input.CachedInputPriceMicros != nil && *input.CachedInputPriceMicros < 0) {
			return nil, errors.New("model prices cannot be negative")
		}
		var count int64
		if err := s.store.db.WithContext(ctx).Model(&GatewayModel{}).Where("id = ?", input.ModelID).Count(&count).Error; err != nil || count == 0 {
			return nil, fmt.Errorf("model %d does not exist", input.ModelID)
		}
		models = append(models, ChannelModel{
			ChannelID:              channelID,
			ModelID:                input.ModelID,
			UpstreamModel:          strings.TrimSpace(input.UpstreamModel),
			Priority:               input.Priority,
			Weight:                 input.Weight,
			InputPriceMicros:       input.InputPriceMicros,
			OutputPriceMicros:      input.OutputPriceMicros,
			CachedInputPriceMicros: input.CachedInputPriceMicros,
			Enabled:                input.Enabled,
		})
	}
	err := s.store.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		if err := db.Where("channel_id = ?", channelID).Delete(&ChannelModel{}).Error; err != nil {
			return err
		}
		if len(models) == 0 {
			return nil
		}
		return db.Create(&models).Error
	})
	return models, err
}

func validRoutingStrategy(value string) bool {
	return value == RoutingPriorityWeighted || value == RoutingLowestCost || value == RoutingLowestLatency
}

func (s *ManagementService) ListModels(ctx context.Context) ([]GatewayModel, error) {
	var models []GatewayModel
	err := s.store.db.WithContext(ctx).Order("name asc").Find(&models).Error
	return models, err
}

func (s *ManagementService) CreateModel(ctx context.Context, input GatewayModelInput) (*GatewayModel, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return nil, errors.New("model name is required")
	}
	if !validRoutingStrategy(input.RoutingStrategy) {
		return nil, errors.New("invalid routing strategy")
	}
	model := GatewayModel{Name: input.Name, RoutingStrategy: input.RoutingStrategy, Enabled: input.Enabled}
	if err := s.store.db.WithContext(ctx).Create(&model).Error; err != nil {
		return nil, err
	}
	return &model, nil
}

func (s *ManagementService) UpdateModel(ctx context.Context, id uint64, input GatewayModelInput) (*GatewayModel, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || !validRoutingStrategy(input.RoutingStrategy) {
		return nil, errors.New("model name and routing strategy are invalid")
	}
	var model GatewayModel
	if err := s.store.db.WithContext(ctx).First(&model, id).Error; err != nil {
		return nil, err
	}
	model.Name = input.Name
	model.RoutingStrategy = input.RoutingStrategy
	model.Enabled = input.Enabled
	if err := s.store.db.WithContext(ctx).Save(&model).Error; err != nil {
		return nil, err
	}
	return &model, nil
}

func (s *ManagementService) DeleteModel(ctx context.Context, id uint64) error {
	return s.store.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		if err := db.Where("model_id = ?", id).Delete(&ChannelModel{}).Error; err != nil {
			return err
		}
		if err := db.Where("model_id = ?", id).Delete(&ClientTokenModel{}).Error; err != nil {
			return err
		}
		result := db.Delete(&GatewayModel{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func (s *ManagementService) ListTokens(ctx context.Context) ([]ClientTokenView, error) {
	var tokens []ClientToken
	if err := s.store.db.WithContext(ctx).Order("id desc").Find(&tokens).Error; err != nil {
		return nil, err
	}
	views := make([]ClientTokenView, 0, len(tokens))
	for _, token := range tokens {
		var links []ClientTokenModel
		if err := s.store.db.WithContext(ctx).Where("token_id = ?", token.ID).Find(&links).Error; err != nil {
			return nil, err
		}
		ids := make([]uint64, 0, len(links))
		for _, link := range links {
			ids = append(ids, link.ModelID)
		}
		statistics, err := s.tokenStatistics(ctx, token.ID)
		if err != nil {
			return nil, err
		}
		views = append(views, ClientTokenView{ClientToken: token, ModelIDs: ids, Statistics: statistics})
	}
	return views, nil
}

func (s *ManagementService) tokenStatistics(ctx context.Context, tokenID uint64) (TokenStatistics, error) {
	type totals struct {
		TokenStatistics
		DurationMS int64
	}
	var total totals
	err := s.store.db.WithContext(ctx).Model(&TokenDailyStat{}).Select(
		"COALESCE(SUM(request_count),0) AS requests, COALESCE(SUM(success_count),0) AS successes, "+
			"COALESCE(SUM(input_tokens),0) AS input_tokens, COALESCE(SUM(output_tokens),0) AS output_tokens, "+
			"COALESCE(SUM(cached_tokens),0) AS cached_tokens, COALESCE(SUM(estimated_cost),0) AS estimated_cost, "+
			"COALESCE(SUM(duration_ms),0) AS duration_ms, COALESCE(SUM(attempt_count),0) AS attempts",
	).Where("token_id = ?", tokenID).Scan(&total).Error
	if err != nil {
		return TokenStatistics{}, err
	}
	if total.Requests > 0 {
		total.AverageLatency = float64(total.DurationMS) / float64(total.Requests)
	}
	return total.TokenStatistics, nil
}

func validateTokenInput(input ClientTokenInput) (ClientTokenInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return input, errors.New("token name is required")
	}
	if input.RPM < 1 || input.RPM > 100000 {
		return input, errors.New("RPM must be between 1 and 100000")
	}
	if input.MaxConcurrency < 1 || input.MaxConcurrency > 10000 {
		return input, errors.New("max concurrency must be between 1 and 10000")
	}
	if !input.AllowAllModels && len(input.ModelIDs) == 0 {
		return input, errors.New("select at least one model or allow all models")
	}
	sort.Slice(input.ModelIDs, func(i, j int) bool { return input.ModelIDs[i] < input.ModelIDs[j] })
	return input, nil
}

func (s *ManagementService) issueToken(ctx context.Context, input ClientTokenInput, existing *ClientToken) (*IssuedClientToken, error) {
	input, err := validateTokenInput(input)
	if err != nil {
		return nil, err
	}
	secret, err := generateSecret("sk-")
	if err != nil {
		return nil, err
	}
	token := ClientToken{
		Name:           input.Name,
		KeyHash:        hashSecret(secret),
		KeyPrefix:      visibleKeyPrefix(secret),
		Enabled:        input.Enabled,
		AllowAllModels: input.AllowAllModels,
		RPM:            input.RPM,
		MaxConcurrency: input.MaxConcurrency,
	}
	if existing != nil {
		token.ID = existing.ID
		token.CreatedAt = existing.CreatedAt
	}
	err = s.store.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		if existing == nil {
			if err := db.Create(&token).Error; err != nil {
				return err
			}
		} else if err := db.Save(&token).Error; err != nil {
			return err
		}
		return replaceTokenModels(db, token.ID, input)
	})
	if err != nil {
		return nil, err
	}
	view, err := s.tokenView(ctx, token)
	if err != nil {
		return nil, err
	}
	return &IssuedClientToken{Token: *view, Secret: secret}, nil
}

func replaceTokenModels(db *gorm.DB, tokenID uint64, input ClientTokenInput) error {
	if err := db.Where("token_id = ?", tokenID).Delete(&ClientTokenModel{}).Error; err != nil {
		return err
	}
	if input.AllowAllModels {
		return nil
	}
	links := make([]ClientTokenModel, 0, len(input.ModelIDs))
	for _, modelID := range input.ModelIDs {
		links = append(links, ClientTokenModel{TokenID: tokenID, ModelID: modelID})
	}
	return db.Create(&links).Error
}

func (s *ManagementService) CreateToken(ctx context.Context, input ClientTokenInput) (*IssuedClientToken, error) {
	return s.issueToken(ctx, input, nil)
}

func (s *ManagementService) UpdateToken(ctx context.Context, id uint64, input ClientTokenInput) (*ClientTokenView, error) {
	input, err := validateTokenInput(input)
	if err != nil {
		return nil, err
	}
	var token ClientToken
	if err := s.store.db.WithContext(ctx).First(&token, id).Error; err != nil {
		return nil, err
	}
	token.Name = input.Name
	token.Enabled = input.Enabled
	token.AllowAllModels = input.AllowAllModels
	token.RPM = input.RPM
	token.MaxConcurrency = input.MaxConcurrency
	err = s.store.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		if err := db.Save(&token).Error; err != nil {
			return err
		}
		return replaceTokenModels(db, token.ID, input)
	})
	if err != nil {
		return nil, err
	}
	return s.tokenView(ctx, token)
}

func (s *ManagementService) RotateToken(ctx context.Context, id uint64) (*IssuedClientToken, error) {
	var token ClientToken
	if err := s.store.db.WithContext(ctx).First(&token, id).Error; err != nil {
		return nil, err
	}
	view, err := s.tokenView(ctx, token)
	if err != nil {
		return nil, err
	}
	input := ClientTokenInput{
		Name: token.Name, Enabled: token.Enabled, AllowAllModels: token.AllowAllModels,
		RPM: token.RPM, MaxConcurrency: token.MaxConcurrency, ModelIDs: view.ModelIDs,
	}
	return s.issueToken(ctx, input, &token)
}

func (s *ManagementService) tokenView(ctx context.Context, token ClientToken) (*ClientTokenView, error) {
	var links []ClientTokenModel
	if err := s.store.db.WithContext(ctx).Where("token_id = ?", token.ID).Find(&links).Error; err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(links))
	for _, link := range links {
		ids = append(ids, link.ModelID)
	}
	statistics, err := s.tokenStatistics(ctx, token.ID)
	if err != nil {
		return nil, err
	}
	return &ClientTokenView{ClientToken: token, ModelIDs: ids, Statistics: statistics}, nil
}

func (s *ManagementService) DeleteToken(ctx context.Context, id uint64) error {
	return s.store.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		if err := db.Where("token_id = ?", id).Delete(&ClientTokenModel{}).Error; err != nil {
			return err
		}
		result := db.Delete(&ClientToken{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func (s *ManagementService) TestChannel(ctx context.Context, id uint64) (int64, int, error) {
	discovery, err := s.DiscoverChannelModels(ctx, ChannelModelDiscoveryInput{ChannelID: id})
	if discovery == nil {
		return 0, 0, err
	}
	return discovery.LatencyMS, discovery.Status, err
}

func (s *ManagementService) DiscoverChannelModels(ctx context.Context, input ChannelModelDiscoveryInput) (*ChannelModelDiscovery, error) {
	baseURL := strings.TrimSpace(input.BaseURL)
	apiKey := strings.TrimSpace(input.APIKey)
	var channel *Channel
	if input.ChannelID > 0 {
		stored := &Channel{}
		if err := s.store.db.WithContext(ctx).First(stored, input.ChannelID).Error; err != nil {
			return nil, err
		}
		channel = stored
		if baseURL == "" {
			baseURL = stored.BaseURL
		}
		if apiKey == "" {
			decrypted, err := s.store.secretBox.Decrypt(stored.APIKeyCipher)
			if err != nil {
				return nil, err
			}
			apiKey = decrypted
		}
	}
	normalizedBaseURL, err := normalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if apiKey == "" {
		return nil, errors.New("channel API key is required to discover models")
	}
	discovery, discoverErr := fetchUpstreamModels(ctx, normalizedBaseURL, apiKey)
	if channel != nil {
		s.updateDiscoveryHealth(channel.ID, discovery, discoverErr)
	}
	return discovery, discoverErr
}

func fetchUpstreamModels(ctx context.Context, baseURL string, apiKey string) (*ChannelModelDiscovery, error) {
	discovery := &ChannelModelDiscovery{Models: []UpstreamModelView{}, FetchedAt: time.Now()}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return discovery, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	started := time.Now()
	resp, err := client.Do(req)
	discovery.LatencyMS = time.Since(started).Milliseconds()
	discovery.FetchedAt = time.Now()
	if err != nil {
		return discovery, err
	}
	defer resp.Body.Close()
	discovery.Status = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return discovery, fmt.Errorf("upstream returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamModelsResponseBytes+1))
	if err != nil {
		return discovery, fmt.Errorf("read upstream models response: %w", err)
	}
	if len(body) > maxUpstreamModelsResponseBytes {
		return discovery, errors.New("upstream models response is too large")
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return discovery, errors.New("upstream models response is not valid JSON")
	}
	if len(envelope.Data) == 0 {
		return discovery, errors.New("upstream models response does not contain data")
	}
	var models []struct {
		ID      string `json:"id"`
		OwnedBy string `json:"owned_by"`
		Created int64  `json:"created"`
	}
	if err := json.Unmarshal(envelope.Data, &models); err != nil {
		return discovery, errors.New("upstream models data is not a model list")
	}
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model.ID = strings.TrimSpace(model.ID)
		model.OwnedBy = strings.TrimSpace(model.OwnedBy)
		if model.ID == "" {
			continue
		}
		if _, exists := seen[model.ID]; exists {
			continue
		}
		seen[model.ID] = struct{}{}
		discovery.Models = append(discovery.Models, UpstreamModelView{ID: model.ID, OwnedBy: model.OwnedBy, Created: model.Created})
	}
	sort.Slice(discovery.Models, func(i, j int) bool { return discovery.Models[i].ID < discovery.Models[j].ID })
	return discovery, nil
}

func (s *ManagementService) updateDiscoveryHealth(channelID uint64, discovery *ChannelModelDiscovery, discoverErr error) {
	updates := map[string]any{"last_health_at": time.Now()}
	if discoverErr != nil {
		message := discoverErr.Error()
		if len(message) > 2000 {
			message = message[:2000]
		}
		updates["last_error"] = message
	} else {
		updates["last_error"] = ""
		updates["latency_ewma"] = float64(discovery.LatencyMS)
	}
	_ = s.store.db.Model(&Channel{}).Where("id = ?", channelID).Updates(updates).Error
}

func (s *ManagementService) Dashboard(ctx context.Context) (*DashboardSummary, error) {
	summary := &DashboardSummary{Daily: []DashboardDaily{}, Channels: []DashboardBreakdown{}, Models: []DashboardBreakdown{}}
	type totals struct {
		Requests      int64
		Successes     int64
		InputTokens   int64
		OutputTokens  int64
		EstimatedCost int64
		DurationMS    int64
	}
	var total totals
	err := s.store.db.WithContext(ctx).Model(&TokenDailyStat{}).Select(
		"COALESCE(SUM(request_count),0) AS requests, COALESCE(SUM(success_count),0) AS successes, " +
			"COALESCE(SUM(input_tokens),0) AS input_tokens, COALESCE(SUM(output_tokens),0) AS output_tokens, " +
			"COALESCE(SUM(estimated_cost),0) AS estimated_cost, COALESCE(SUM(duration_ms),0) AS duration_ms",
	).Scan(&total).Error
	if err != nil {
		return nil, err
	}
	summary.Requests = total.Requests
	summary.InputTokens = total.InputTokens
	summary.OutputTokens = total.OutputTokens
	summary.EstimatedCost = total.EstimatedCost
	if total.Requests > 0 {
		summary.SuccessRate = float64(total.Successes) / float64(total.Requests)
		summary.AverageLatency = float64(total.DurationMS) / float64(total.Requests)
	}
	dailyCutoff := time.Now().UTC().AddDate(0, 0, -13).Format(time.DateOnly)
	if err := s.store.db.WithContext(ctx).Model(&TokenDailyStat{}).
		Select("date, COALESCE(SUM(request_count),0) AS requests, COALESCE(SUM(success_count),0) AS successes, COALESCE(SUM(input_tokens),0) AS input_tokens, COALESCE(SUM(output_tokens),0) AS output_tokens, COALESCE(SUM(estimated_cost),0) AS estimated_cost").
		Where("date >= ?", dailyCutoff).Group("date").Order("date asc").Scan(&summary.Daily).Error; err != nil {
		return nil, err
	}
	detailCutoff := time.Now().Add(-DetailedLogRetentionDays * 24 * time.Hour)
	if err := s.store.db.WithContext(ctx).Table("relay_attempt_logs AS a").
		Select("c.name AS name, COUNT(*) AS requests, COALESCE(SUM(a.estimated_cost),0) AS estimated_cost").
		Joins("JOIN channels AS c ON c.id = a.channel_id").Where("a.created_at >= ?", detailCutoff).
		Group("c.name").Order("estimated_cost desc").Limit(8).Scan(&summary.Channels).Error; err != nil {
		return nil, err
	}
	if err := s.store.db.WithContext(ctx).Model(&RelayRequestLog{}).
		Select("requested_model AS name, COUNT(*) AS requests, COALESCE(SUM(estimated_cost),0) AS estimated_cost").
		Where("created_at >= ?", detailCutoff).Group("requested_model").Order("estimated_cost desc").Limit(8).Scan(&summary.Models).Error; err != nil {
		return nil, err
	}
	return summary, nil
}

func (s *ManagementService) Logs(ctx context.Context, query LogQuery) (*LogPage, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 || query.PageSize > 200 {
		query.PageSize = 50
	}
	detailCutoff := time.Now().Add(-DetailedLogRetentionDays * 24 * time.Hour)
	db := s.store.db.WithContext(ctx).Model(&RelayRequestLog{}).Where("created_at >= ?", detailCutoff)
	if strings.TrimSpace(query.Model) != "" {
		db = db.Where("requested_model LIKE ?", "%"+strings.TrimSpace(query.Model)+"%")
	}
	if query.StatusCode > 0 {
		db = db.Where("status_code = ?", query.StatusCode)
	}
	if query.TokenID > 0 {
		db = db.Where("token_id = ?", query.TokenID)
	}
	if query.ChannelID > 0 {
		db = db.Where("EXISTS (SELECT 1 FROM relay_attempt_logs a WHERE a.request_id = relay_request_logs.id AND a.channel_id = ?)", query.ChannelID)
	}
	if !query.From.IsZero() {
		db = db.Where("created_at >= ?", query.From)
	}
	if !query.To.IsZero() {
		db = db.Where("created_at <= ?", query.To)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, err
	}
	var logs []RelayRequestLog
	if err := db.Order("created_at desc").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).Find(&logs).Error; err != nil {
		return nil, err
	}
	items := make([]RelayRequestView, 0, len(logs))
	for _, log := range logs {
		view, err := s.relayRequestView(ctx, log)
		if err != nil {
			return nil, err
		}
		items = append(items, view)
	}
	return &LogPage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}
