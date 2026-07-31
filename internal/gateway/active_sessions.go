package gateway

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
)

type ActiveSessionQuery struct {
	Session     string
	ActiveSince time.Time
	Page        int
	PageSize    int
}

type ActiveSessionPage struct {
	Items    []SessionLogSummary `json:"items"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"pageSize"`
}

func activeSessionRows(db *gorm.DB, query ActiveSessionQuery) *gorm.DB {
	db = db.Table("relay_session_states AS session_state").
		Joins("JOIN relay_request_logs AS latest_request ON latest_request.id = session_state.latest_request_id").
		Joins("LEFT JOIN client_tokens AS client_token ON client_token.id = session_state.token_id").
		Where("session_state.thread_source = ?", codexThreadSourceUser).
		Where("session_state.last_activity_at >= ?", utcQueryTime(query.ActiveSince))
	if value := strings.TrimSpace(query.Session); value != "" {
		pattern := "%" + value + "%"
		db = db.Where(
			"(session_state.title LIKE ? OR session_state.session_id LIKE ? OR latest_request.id LIKE ?)",
			pattern, pattern, pattern,
		)
	}
	return db
}

func (s *ManagementService) ActiveSessions(ctx context.Context, query ActiveSessionQuery) (*ActiveSessionPage, error) {
	// Keep the hot list on materialized session state; five-day aggregates are loaded only for one opened session.
	normalizeSessionPage(&query.Page, &query.PageSize)
	if query.ActiveSince.IsZero() {
		query.ActiveSince = time.Now().UTC().Add(-30 * time.Minute)
	}
	detailCutoff := time.Now().UTC().Add(-DetailedLogRetentionDays * 24 * time.Hour)

	var total int64
	if err := activeSessionRows(s.store.db.WithContext(ctx), query).Count(&total).Error; err != nil {
		return nil, err
	}

	items := make([]SessionLogSummary, 0, query.PageSize)
	if err := activeSessionRows(s.store.db.WithContext(ctx), query).
		Select(
			"session_state.session_id AS session_id, "+
				"COALESCE(NULLIF(session_state.title, ''), latest_request.session_name) AS session_name, "+
				"COALESCE(NULLIF(session_state.session_source, ''), NULLIF(latest_request.codex_session_source, ''), 'unavailable') AS session_source, "+
				"COALESCE(NULLIF(session_state.client_kind, ''), latest_request.client_kind) AS client_kind, "+
				"session_state.thread_source AS thread_source, 1 AS identified, session_state.token_id AS token_id, "+
				"COALESCE(NULLIF(latest_request.token_name, ''), client_token.name) AS token_name, "+
				"COALESCE(NULLIF(latest_request.token_key_prefix, ''), client_token.key_prefix) AS token_key_prefix, "+
				"latest_request.requested_model AS latest_model, latest_request.endpoint AS latest_endpoint, "+
				"(SELECT COUNT(*) FROM relay_request_logs AS session_request "+
				"WHERE session_request.token_id = session_state.token_id "+
				"AND session_request.codex_session_id = session_state.session_id "+
				"AND session_request.created_at >= ?) AS request_count, "+
				"session_state.created_at AS first_seen_at, session_state.last_activity_at AS last_seen_at",
			detailCutoff,
		).
		Order("session_state.last_activity_at DESC, session_state.session_id ASC").
		Offset((query.Page - 1) * query.PageSize).
		Limit(query.PageSize).
		Scan(&items).Error; err != nil {
		return nil, err
	}
	return &ActiveSessionPage{Items: items, Total: total, Page: query.Page, PageSize: query.PageSize}, nil
}
