package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	copilotClientKind        = "copilot"
	copilotChatSessionSource = "copilot_chat_history"
	copilotHeaderSession     = "copilot_header.session_id"
	copilotChatActiveWindow  = 12 * time.Hour
)

type inferredChatSession struct {
	ID                string
	Source            string
	ClientKind        string
	ClientFingerprint string
}

func copilotClientFingerprint(headers http.Header) (string, bool) {
	selected := make(map[string]string)
	detected := false
	for _, key := range []string{
		"User-Agent",
		"Copilot-Integration-Id",
		"X-Github-Copilot-Integration-Id",
		"Editor-Version",
		"Editor-Plugin-Version",
		"X-Editor-Plugin-Version",
		"X-Client-Name",
	} {
		value := strings.TrimSpace(headers.Get(key))
		if value == "" {
			continue
		}
		selected[strings.ToLower(key)] = value
		if strings.Contains(strings.ToLower(key+" "+value), "copilot") {
			detected = true
		}
	}
	for key, values := range headers {
		if !strings.Contains(strings.ToLower(key), "copilot") {
			continue
		}
		detected = true
		selected[strings.ToLower(key)] = truncateRunes(strings.Join(values, ","), 512)
	}
	if !detected {
		return "", false
	}
	return hashJSON(selected), true
}

func copilotHeaderSessionID(headers http.Header) string {
	for _, key := range []string{
		"X-Copilot-Session-Id",
		"X-Github-Copilot-Session-Id",
		"X-Copilot-Thread-Id",
		"X-Github-Copilot-Thread-Id",
	} {
		if value := truncateRunes(strings.TrimSpace(headers.Get(key)), 512); value != "" {
			return value
		}
	}
	return ""
}

func (s *Store) resolveCopilotChatSession(ctx context.Context, tokenID uint64, headers http.Header, body []byte, now time.Time) (*inferredChatSession, error) {
	fingerprint, detected := copilotClientFingerprint(headers)
	if !detected || tokenID == 0 {
		return nil, nil
	}
	if headerSessionID := copilotHeaderSessionID(headers); headerSessionID != "" {
		return &inferredChatSession{
			ID: headerSessionID, Source: copilotHeaderSession,
			ClientKind: copilotClientKind, ClientFingerprint: fingerprint,
		}, nil
	}

	manifest, ok := buildPayloadManifest(body)
	if !ok || len(manifest.Arrays["messages"]) == 0 {
		return nil, nil
	}
	manifestJSONBytes, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	manifestJSON := string(manifestJSONBytes)
	historyHash := hashJSON(manifest.Arrays["messages"])
	cutoff := now.Add(-copilotChatActiveWindow)
	resolvedID := ""

	err = s.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		var existing RelayChatSessionClaim
		err := db.Where(
			"token_id = ? AND client_fingerprint = ? AND request_history_hash = ? AND updated_at >= ?",
			tokenID, fingerprint, historyHash, cutoff,
		).First(&existing).Error
		if err == nil {
			resolvedID = existing.SessionID
			return db.Model(&existing).Update("updated_at", now).Error
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		resolvedID, err = resolveChatHistoryCandidate(db, tokenID, fingerprint, manifest.Arrays["messages"], cutoff)
		if err != nil {
			return err
		}
		if resolvedID == "" {
			resolvedID = "chat_" + uuid.NewString()
		}

		claim := RelayChatSessionClaim{
			TokenID: tokenID, ClientFingerprint: fingerprint, RequestHistoryHash: historyHash,
			SessionID: resolvedID, RequestManifestJSON: manifestJSON, CreatedAt: now, UpdatedAt: now,
		}
		result := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&claim)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			if err := db.Where(
				"token_id = ? AND client_fingerprint = ? AND request_history_hash = ?",
				tokenID, fingerprint, historyHash,
			).First(&claim).Error; err != nil {
				return err
			}
			resolvedID = claim.SessionID
		}

		state := RelaySessionState{
			TokenID: tokenID, SessionID: resolvedID, SessionSource: copilotChatSessionSource,
			ClientKind: copilotClientKind, ClientFingerprint: fingerprint,
			RequestManifestJSON: manifestJSON, PayloadManifestJSON: manifestJSON,
			CreatedAt: now, UpdatedAt: now,
		}
		return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&state).Error
	})
	if err != nil {
		return nil, err
	}
	return &inferredChatSession{
		ID: resolvedID, Source: copilotChatSessionSource,
		ClientKind: copilotClientKind, ClientFingerprint: fingerprint,
	}, nil
}

func resolveChatHistoryCandidate(db *gorm.DB, tokenID uint64, fingerprint string, current []string, cutoff time.Time) (string, error) {
	scores := make(map[string]int)
	var states []RelaySessionState
	if err := db.Select("session_id, request_manifest_json, payload_manifest_json").
		Where("token_id = ? AND client_fingerprint = ? AND updated_at >= ?", tokenID, fingerprint, cutoff).
		Order("updated_at DESC").Limit(128).Find(&states).Error; err != nil {
		return "", err
	}
	for _, state := range states {
		requestHistory := manifestConversationHashes(state.RequestManifestJSON, "messages")
		expectedHistory := manifestConversationHashes(state.PayloadManifestJSON, "messages")
		if len(expectedHistory) > len(requestHistory) && isHashPrefix(expectedHistory, current) {
			scores[state.SessionID] = max(scores[state.SessionID], 4)
		}
		if len(requestHistory) < len(current) && isHashPrefix(requestHistory, current) {
			scores[state.SessionID] = max(scores[state.SessionID], 3)
		}
	}

	var claims []RelayChatSessionClaim
	if err := db.Select("session_id, request_manifest_json").
		Where("token_id = ? AND client_fingerprint = ? AND updated_at >= ?", tokenID, fingerprint, cutoff).
		Order("updated_at DESC").Limit(128).Find(&claims).Error; err != nil {
		return "", err
	}
	for _, claim := range claims {
		history := manifestConversationHashes(claim.RequestManifestJSON, "messages")
		if len(history) < len(current) && isHashPrefix(history, current) {
			scores[claim.SessionID] = max(scores[claim.SessionID], 2)
		}
	}

	bestID := ""
	bestScore := 0
	ambiguous := false
	for sessionID, score := range scores {
		if score > bestScore {
			bestID, bestScore, ambiguous = sessionID, score, false
		} else if score == bestScore && score > 0 && sessionID != bestID {
			ambiguous = true
		}
	}
	if ambiguous {
		return "", nil
	}
	return bestID, nil
}

func manifestConversationHashes(manifestJSON string, key string) []string {
	var manifest payloadManifest
	if manifestJSON == "" || json.Unmarshal([]byte(manifestJSON), &manifest) != nil {
		return nil
	}
	return manifest.Arrays[key]
}

func isHashPrefix(prefix []string, values []string) bool {
	if len(prefix) == 0 || len(prefix) > len(values) {
		return false
	}
	for index := range prefix {
		if prefix[index] != values[index] {
			return false
		}
	}
	return true
}
