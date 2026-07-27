package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

const sessionGroupExpression = "CASE WHEN codex_session_id <> '' THEN codex_session_id ELSE id END"

type SessionLogQuery struct {
	Session   string
	Model     string
	TokenID   uint64
	ChannelID uint64
	From      time.Time
	To        time.Time
	Page      int
	PageSize  int
}

type SessionDetailQuery struct {
	SessionID string
	RequestID string
	TokenID   uint64
	Page      int
	PageSize  int
}

type SessionChannelView struct {
	ChannelID        uint64     `json:"channelId"`
	ChannelName      string     `json:"channelName"`
	ChannelBaseURL   string     `json:"channelBaseUrl"`
	ChannelModelID   uint64     `json:"channelModelId"`
	UpstreamModel    string     `json:"upstreamModel"`
	AssignmentSource string     `json:"assignmentSource"`
	Enabled          bool       `json:"enabled"`
	MappingEnabled   bool       `json:"mappingEnabled"`
	CircuitOpenUntil *time.Time `json:"circuitOpenUntil"`
	LastUsedAt       time.Time  `json:"lastUsedAt"`
}

type SessionLogSummary struct {
	GroupID           string              `gorm:"column:group_id" json:"-"`
	SessionID         string              `json:"sessionId"`
	SessionSource     string              `json:"sessionSource"`
	Identified        bool                `json:"identified"`
	FallbackRequestID string              `json:"fallbackRequestId"`
	TokenID           uint64              `json:"tokenId"`
	TokenName         string              `json:"tokenName"`
	TokenKeyPrefix    string              `json:"tokenKeyPrefix"`
	LatestModel       string              `json:"latestModel"`
	LatestEndpoint    string              `json:"latestEndpoint"`
	RequestCount      int64               `json:"requestCount"`
	SuccessCount      int64               `json:"successCount"`
	SuccessRate       float64             `json:"successRate"`
	AttemptCount      int64               `json:"attemptCount"`
	InputTokens       int64               `json:"inputTokens"`
	NormalInputTokens int64               `json:"normalInputTokens"`
	OutputTokens      int64               `json:"outputTokens"`
	CachedTokens      int64               `json:"cachedTokens"`
	CacheWriteTokens  int64               `json:"cacheWriteTokens"`
	SentTokens        int64               `json:"sentTokens"`
	CacheHitRate      float64             `json:"cacheHitRate"`
	EstimatedCost     int64               `json:"estimatedCostMicros"`
	UpstreamCost      int64               `json:"upstreamCostMicros"`
	AverageDurationMS float64             `json:"averageDurationMs"`
	FirstSeenAt       time.Time           `json:"firstSeenAt"`
	LastSeenAt        time.Time           `json:"lastSeenAt"`
	CurrentChannel    *SessionChannelView `gorm:"-" json:"currentChannel"`
}

type SessionLogPage struct {
	Items    []SessionLogSummary `json:"items"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"pageSize"`
}

type SessionLogDetail struct {
	Summary      SessionLogSummary  `json:"summary"`
	Requests     []RelayRequestView `json:"requests"`
	RequestTotal int64              `json:"requestTotal"`
	Page         int                `json:"page"`
	PageSize     int                `json:"pageSize"`
}

func normalizeSessionPage(page *int, pageSize *int) {
	if *page < 1 {
		*page = 1
	}
	if *pageSize < 1 || *pageSize > 200 {
		*pageSize = 25
	}
}

func (s *ManagementService) SessionLogs(ctx context.Context, query SessionLogQuery) (*SessionLogPage, error) {
	normalizeSessionPage(&query.Page, &query.PageSize)
	cutoff := time.Now().Add(-DetailedLogRetentionDays * 24 * time.Hour)

	grouped := applySessionLogFilters(s.store.db.WithContext(ctx).Model(&RelayRequestLog{}), query, cutoff).
		Select("token_id, " + sessionGroupExpression + " AS group_id").
		Group("token_id, " + sessionGroupExpression)
	var total int64
	if err := s.store.db.WithContext(ctx).Table("(?) AS session_groups", grouped).Count(&total).Error; err != nil {
		return nil, err
	}

	items := make([]SessionLogSummary, 0, query.PageSize)
	selectSQL := sessionGroupExpression + " AS group_id, " +
		"CASE WHEN codex_session_id <> '' THEN codex_session_id ELSE '' END AS session_id, " +
		"CASE WHEN codex_session_id <> '' THEN 1 ELSE 0 END AS identified, " +
		"CASE WHEN codex_session_id = '' THEN id ELSE '' END AS fallback_request_id, token_id, " +
		"COUNT(*) AS request_count, SUM(CASE WHEN status_code BETWEEN 200 AND 299 THEN 1 ELSE 0 END) AS success_count, " +
		"COALESCE(SUM(attempt_count), 0) AS attempt_count, COALESCE(SUM(input_tokens), 0) AS input_tokens, " +
		"COALESCE(SUM(normal_input_tokens), 0) AS normal_input_tokens, " +
		"COALESCE(SUM(output_tokens), 0) AS output_tokens, COALESCE(SUM(cached_tokens), 0) AS cached_tokens, " +
		"COALESCE(SUM(cache_write_tokens), 0) AS cache_write_tokens, " +
		"COALESCE(SUM(sent_tokens), 0) AS sent_tokens, " +
		"COALESCE(SUM(estimated_cost), 0) AS estimated_cost, COALESCE(SUM(upstream_cost), 0) AS upstream_cost, COALESCE(SUM(duration_ms), 0) AS total_duration_ms, " +
		"MIN(unixepoch(created_at)) AS first_seen_unix, MAX(unixepoch(created_at)) AS last_seen_unix"
	type aggregateRow struct {
		SessionLogSummary
		TotalDurationMS int64
		FirstSeenUnix   int64
		LastSeenUnix    int64
	}
	var rows []aggregateRow
	if err := applySessionLogFilters(s.store.db.WithContext(ctx).Model(&RelayRequestLog{}), query, cutoff).
		Select(selectSQL).Group("token_id, " + sessionGroupExpression).
		Order("last_seen_unix DESC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		summary := row.SessionLogSummary
		summary.FirstSeenAt = time.Unix(row.FirstSeenUnix, 0).UTC()
		summary.LastSeenAt = time.Unix(row.LastSeenUnix, 0).UTC()
		finishSessionSummary(&summary, row.TotalDurationMS)
		if err := s.populateSessionSummary(ctx, &summary, cutoff); err != nil {
			return nil, err
		}
		items = append(items, summary)
	}
	return &SessionLogPage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}

func applySessionLogFilters(db *gorm.DB, query SessionLogQuery, cutoff time.Time) *gorm.DB {
	db = db.Where("created_at >= ?", cutoff)
	if value := strings.TrimSpace(query.Session); value != "" {
		pattern := "%" + value + "%"
		db = db.Where("(codex_session_id LIKE ? OR id LIKE ?)", pattern, pattern)
	}
	if value := strings.TrimSpace(query.Model); value != "" {
		db = db.Where("requested_model = ?", value)
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
	return db
}

func finishSessionSummary(summary *SessionLogSummary, totalDurationMS int64) {
	if summary.RequestCount > 0 {
		summary.SuccessRate = float64(summary.SuccessCount) / float64(summary.RequestCount)
		summary.AverageDurationMS = float64(totalDurationMS) / float64(summary.RequestCount)
	}
	summary.InputTokens = max(summary.InputTokens, 0)
	summary.NormalInputTokens = max(summary.NormalInputTokens, 0)
	summary.OutputTokens = max(summary.OutputTokens, 0)
	summary.CachedTokens = min(max(summary.CachedTokens, 0), summary.InputTokens)
	summary.CacheWriteTokens = min(max(summary.CacheWriteTokens, 0), summary.InputTokens-summary.CachedTokens)
	summary.NormalInputTokens = min(summary.NormalInputTokens, summary.InputTokens-summary.CachedTokens-summary.CacheWriteTokens)
	summary.SentTokens = max(summary.SentTokens, 0)
	if summary.InputTokens > 0 {
		summary.CacheHitRate = float64(summary.CachedTokens) / float64(summary.InputTokens)
	}
}

func (s *ManagementService) SessionLogDetail(ctx context.Context, query SessionDetailQuery) (*SessionLogDetail, error) {
	normalizeSessionPage(&query.Page, &query.PageSize)
	query.SessionID = strings.TrimSpace(query.SessionID)
	query.RequestID = strings.TrimSpace(query.RequestID)
	if (query.SessionID == "" && query.RequestID == "") || (query.SessionID != "" && query.TokenID == 0) {
		return nil, errors.New("sessionId with tokenId or requestId is required")
	}
	cutoff := time.Now().Add(-DetailedLogRetentionDays * 24 * time.Hour)
	base := applySessionIdentity(s.store.db.WithContext(ctx).Model(&RelayRequestLog{}).Where("created_at >= ?", cutoff), query)
	var requestTotal int64
	if err := base.Count(&requestTotal).Error; err != nil {
		return nil, err
	}
	if requestTotal == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	type detailAggregate struct {
		SuccessCount      int64
		AttemptCount      int64
		InputTokens       int64
		NormalInputTokens int64
		OutputTokens      int64
		CachedTokens      int64
		CacheWriteTokens  int64
		SentTokens        int64
		EstimatedCost     int64
		UpstreamCost      int64
		TotalDurationMS   int64
		FirstSeenUnix     int64
		LastSeenUnix      int64
	}
	var aggregate detailAggregate
	if err := applySessionIdentity(s.store.db.WithContext(ctx).Model(&RelayRequestLog{}).Where("created_at >= ?", cutoff), query).
		Select("SUM(CASE WHEN status_code BETWEEN 200 AND 299 THEN 1 ELSE 0 END) AS success_count, " +
			"COALESCE(SUM(attempt_count), 0) AS attempt_count, COALESCE(SUM(input_tokens), 0) AS input_tokens, " +
			"COALESCE(SUM(normal_input_tokens), 0) AS normal_input_tokens, " +
			"COALESCE(SUM(output_tokens), 0) AS output_tokens, COALESCE(SUM(cached_tokens), 0) AS cached_tokens, " +
			"COALESCE(SUM(cache_write_tokens), 0) AS cache_write_tokens, " +
			"COALESCE(SUM(sent_tokens), 0) AS sent_tokens, " +
			"COALESCE(SUM(estimated_cost), 0) AS estimated_cost, COALESCE(SUM(upstream_cost), 0) AS upstream_cost, COALESCE(SUM(duration_ms), 0) AS total_duration_ms, " +
			"MIN(unixepoch(created_at)) AS first_seen_unix, MAX(unixepoch(created_at)) AS last_seen_unix").Scan(&aggregate).Error; err != nil {
		return nil, err
	}
	summary := SessionLogSummary{
		SessionID:         query.SessionID,
		Identified:        query.SessionID != "",
		FallbackRequestID: query.RequestID,
		TokenID:           query.TokenID,
		RequestCount:      requestTotal,
		SuccessCount:      aggregate.SuccessCount,
		AttemptCount:      aggregate.AttemptCount,
		InputTokens:       aggregate.InputTokens,
		NormalInputTokens: aggregate.NormalInputTokens,
		OutputTokens:      aggregate.OutputTokens,
		CachedTokens:      aggregate.CachedTokens,
		CacheWriteTokens:  aggregate.CacheWriteTokens,
		SentTokens:        aggregate.SentTokens,
		EstimatedCost:     aggregate.EstimatedCost,
		UpstreamCost:      aggregate.UpstreamCost,
		FirstSeenAt:       time.Unix(aggregate.FirstSeenUnix, 0).UTC(),
		LastSeenAt:        time.Unix(aggregate.LastSeenUnix, 0).UTC(),
	}
	finishSessionSummary(&summary, aggregate.TotalDurationMS)
	if err := s.populateSessionSummary(ctx, &summary, cutoff); err != nil {
		return nil, err
	}

	var logs []RelayRequestLog
	if err := applySessionIdentity(s.store.db.WithContext(ctx).Model(&RelayRequestLog{}).Where("created_at >= ?", cutoff), query).
		Order("created_at ASC, id ASC").Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize).
		Find(&logs).Error; err != nil {
		return nil, err
	}
	requests := make([]RelayRequestView, 0, len(logs))
	for _, log := range logs {
		view, err := s.relayRequestView(ctx, log)
		if err != nil {
			return nil, err
		}
		requests = append(requests, view)
	}
	return &SessionLogDetail{Summary: summary, Requests: requests, RequestTotal: requestTotal, Page: query.Page, PageSize: query.PageSize}, nil
}

func applySessionIdentity(db *gorm.DB, query SessionDetailQuery) *gorm.DB {
	if query.SessionID != "" {
		return db.Where("token_id = ? AND codex_session_id = ?", query.TokenID, query.SessionID)
	}
	return db.Where("id = ? AND codex_session_id = ''", query.RequestID)
}

func (s *ManagementService) populateSessionSummary(ctx context.Context, summary *SessionLogSummary, cutoff time.Time) error {
	latestDB := s.store.db.WithContext(ctx).Model(&RelayRequestLog{}).Where("created_at >= ?", cutoff)
	if summary.Identified {
		latestDB = latestDB.Where("token_id = ? AND codex_session_id = ?", summary.TokenID, summary.SessionID)
	} else {
		latestDB = latestDB.Where("id = ? AND codex_session_id = ''", summary.FallbackRequestID)
	}
	var latest RelayRequestLog
	if err := latestDB.Order("created_at DESC, id DESC").First(&latest).Error; err != nil {
		return err
	}
	summary.TokenID = latest.TokenID
	summary.TokenName = latest.TokenName
	summary.TokenKeyPrefix = latest.TokenKeyPrefix
	if summary.TokenName == "" || summary.TokenKeyPrefix == "" {
		var token ClientToken
		if err := s.store.db.WithContext(ctx).First(&token, latest.TokenID).Error; err == nil {
			if summary.TokenName == "" {
				summary.TokenName = token.Name
			}
			if summary.TokenKeyPrefix == "" {
				summary.TokenKeyPrefix = token.KeyPrefix
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if summary.Identified {
		summary.SessionID = latest.CodexSessionID
		summary.SessionSource = latest.CodexSessionSource
	} else {
		summary.SessionSource = "unavailable"
		summary.FallbackRequestID = latest.ID
	}
	if summary.SessionSource == "" {
		summary.SessionSource = "unavailable"
	}
	summary.LatestModel = latest.RequestedModel
	summary.LatestEndpoint = latest.Endpoint
	current, err := s.currentSessionChannel(ctx, *summary, latest.RequestedModel, cutoff)
	if err != nil {
		return err
	}
	summary.CurrentChannel = current
	return nil
}

func (s *ManagementService) currentSessionChannel(ctx context.Context, summary SessionLogSummary, modelName string, cutoff time.Time) (*SessionChannelView, error) {
	if summary.Identified {
		var model GatewayModel
		modelErr := s.store.db.WithContext(ctx).Where("name = ?", modelName).First(&model).Error
		if modelErr == nil {
			var affinity SessionAffinity
			affinityErr := s.store.db.WithContext(ctx).
				Where("token_id = ? AND model_id = ? AND session_hash = ? AND expires_at > ?", summary.TokenID, model.ID, hashSecret(summary.SessionID), time.Now()).
				First(&affinity).Error
			if affinityErr == nil {
				var mapping ChannelModel
				if err := s.store.db.WithContext(ctx).First(&mapping, affinity.ChannelModelID).Error; err == nil {
					var channel Channel
					if err := s.store.db.WithContext(ctx).First(&channel, mapping.ChannelID).Error; err == nil {
						return &SessionChannelView{
							ChannelID:        channel.ID,
							ChannelName:      channel.Name,
							ChannelBaseURL:   channel.BaseURL,
							ChannelModelID:   mapping.ID,
							UpstreamModel:    mapping.UpstreamModel,
							AssignmentSource: "session_affinity",
							Enabled:          channel.Enabled,
							MappingEnabled:   mapping.Enabled,
							CircuitOpenUntil: channel.CircuitOpenUntil,
							LastUsedAt:       affinity.UpdatedAt,
						}, nil
					} else if !errors.Is(err, gorm.ErrRecordNotFound) {
						return nil, err
					}
				} else if !errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, err
				}
			} else if !errors.Is(affinityErr, gorm.ErrRecordNotFound) {
				return nil, affinityErr
			}
		} else if !errors.Is(modelErr, gorm.ErrRecordNotFound) {
			return nil, modelErr
		}
	}

	attemptDB := s.store.db.WithContext(ctx).Table("relay_attempt_logs AS a").
		Select("a.*").Joins("JOIN relay_request_logs AS r ON r.id = a.request_id").
		Where("r.created_at >= ?", cutoff)
	if summary.Identified {
		attemptDB = attemptDB.Where("r.token_id = ? AND r.codex_session_id = ?", summary.TokenID, summary.SessionID)
	} else {
		attemptDB = attemptDB.Where("r.id = ? AND r.codex_session_id = ''", summary.FallbackRequestID)
	}
	var attempt RelayAttemptLog
	if err := attemptDB.Order("a.success DESC, a.created_at DESC, a.id DESC").First(&attempt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	current := &SessionChannelView{
		ChannelID:        attempt.ChannelID,
		ChannelName:      attempt.ChannelName,
		ChannelBaseURL:   attempt.ChannelBaseURL,
		ChannelModelID:   attempt.ChannelModelID,
		UpstreamModel:    attempt.UpstreamModel,
		AssignmentSource: "latest_attempt",
		LastUsedAt:       attempt.CreatedAt,
	}
	if attempt.Success {
		current.AssignmentSource = "latest_successful_attempt"
	}
	var mapping ChannelModel
	if err := s.store.db.WithContext(ctx).First(&mapping, attempt.ChannelModelID).Error; err == nil {
		current.MappingEnabled = mapping.Enabled
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var channel Channel
	if err := s.store.db.WithContext(ctx).First(&channel, attempt.ChannelID).Error; err == nil {
		current.Enabled = channel.Enabled
		current.CircuitOpenUntil = channel.CircuitOpenUntil
		if current.ChannelName == "" {
			current.ChannelName = channel.Name
		}
		if current.ChannelBaseURL == "" {
			current.ChannelBaseURL = channel.BaseURL
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return current, nil
}

func (s *ManagementService) relayRequestView(ctx context.Context, log RelayRequestLog) (RelayRequestView, error) {
	attempts := make([]RelayAttemptLog, 0)
	if err := s.store.db.WithContext(ctx).Where("request_id = ?", log.ID).Order("created_at ASC, id ASC").Find(&attempts).Error; err != nil {
		return RelayRequestView{}, err
	}
	channelCache := make(map[uint64]Channel)
	for index := range attempts {
		if attempts[index].ChannelName != "" && attempts[index].ChannelBaseURL != "" {
			continue
		}
		channel, ok := channelCache[attempts[index].ChannelID]
		if !ok {
			if err := s.store.db.WithContext(ctx).First(&channel, attempts[index].ChannelID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return RelayRequestView{}, err
			}
			channelCache[channel.ID] = channel
		}
		if attempts[index].ChannelName == "" {
			attempts[index].ChannelName = channel.Name
		}
		if attempts[index].ChannelBaseURL == "" {
			attempts[index].ChannelBaseURL = channel.BaseURL
		}
	}
	return RelayRequestView{
		RelayRequestLog:   log,
		RequestParameters: decodeRequestParameters(log.RequestParametersJSON),
		Attempts:          attempts,
	}, nil
}

func decodeRequestParameters(value string) map[string]any {
	parameters := make(map[string]any)
	decoder := json.NewDecoder(bytes.NewBufferString(value))
	decoder.UseNumber()
	if strings.TrimSpace(value) == "" || decoder.Decode(&parameters) != nil {
		return map[string]any{}
	}
	return parameters
}
