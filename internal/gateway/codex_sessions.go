package gateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

const (
	codexTitleSessionSource    = "codex_title_generation"
	codexGuardianSessionSource = "codex_guardian"
	codexGuardianSessionPrefix = "guardian:"
	codexTitlePromptMarker     = "User prompt:"
	codexAttachedPromptMarker  = "## My request for Codex:"
	codexTitleMatchWindow      = 5 * time.Minute
	codexTitleStartTolerance   = 30 * time.Second
)

var codexAuxiliarySessionSources = []string{codexTitleSessionSource, codexGuardianSessionSource}

func loggedCodexSessionIdentity(sessionID string, source string) (string, string) {
	sessionID = strings.TrimSpace(sessionID)
	if strings.HasPrefix(sessionID, codexGuardianSessionPrefix) {
		canonical := strings.TrimSpace(strings.TrimPrefix(sessionID, codexGuardianSessionPrefix))
		if canonical != "" {
			return canonical, codexGuardianSessionSource
		}
	}
	return sessionID, source
}

func codexRequestPrompt(body []byte) string {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return ""
	}
	if input, exists := payload["input"]; exists {
		return normalizeCodexPrompt(latestResponsesInputText(input))
	}
	if messages, ok := payload["messages"].([]any); ok {
		return normalizeCodexPrompt(latestUserMessageText(messages))
	}
	return ""
}

func codexTitleRequestPrompt(body []byte) (string, bool) {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return "", false
	}
	text, _ := payload["text"].(map[string]any)
	format, _ := text["format"].(map[string]any)
	if format["type"] != "json_schema" || format["name"] != "codex_output_schema" {
		return "", false
	}
	schema, _ := format["schema"].(map[string]any)
	properties, _ := schema["properties"].(map[string]any)
	if len(properties) != 2 || properties["title"] == nil || properties["description"] == nil {
		return "", false
	}
	instruction := latestResponsesInputText(payload["input"])
	markerIndex := strings.Index(instruction, codexTitlePromptMarker)
	if markerIndex < 0 {
		return "", false
	}
	prompt := normalizeCodexPrompt(instruction[markerIndex+len(codexTitlePromptMarker):])
	return prompt, prompt != ""
}

func normalizeCodexPrompt(value string) string {
	if markerIndex := strings.LastIndex(value, codexAttachedPromptMarker); markerIndex >= 0 {
		value = value[markerIndex+len(codexAttachedPromptMarker):]
	}
	if imageIndex := strings.Index(value, "<image "); imageIndex >= 0 {
		value = value[:imageIndex]
	}
	return strings.Join(strings.Fields(value), " ")
}

func codexLogPayloadMetadata(requestBody []byte, responseBody []byte) (string, bool, string) {
	if titlePrompt, isTitleRequest := codexTitleRequestPrompt(requestBody); isTitleRequest {
		return hashCodexPrompt(titlePrompt), true, codexGeneratedTitle(responseBody)
	}
	return hashCodexPrompt(codexRequestPrompt(requestBody)), false, ""
}

func hashCodexPrompt(value string) string {
	if value == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func codexGeneratedTitle(responseBody []byte) string {
	output := responsesOutputText(responseBody)
	if output == "" {
		return ""
	}
	var result struct {
		Title string `json:"title"`
	}
	if json.Unmarshal([]byte(output), &result) != nil {
		return ""
	}
	return truncateRunes(normalizeSessionName(result.Title), 80)
}

func responsesOutputText(body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return ""
	}
	if !bytes.HasPrefix(trimmed, []byte("data:")) && !bytes.HasPrefix(trimmed, []byte("event:")) {
		payload, ok := decodeJSONObject(trimmed)
		if !ok {
			return ""
		}
		return responseObjectText(payload)
	}
	var doneText string
	var deltaText strings.Builder
	var completed map[string]any
	for _, line := range bytes.Split(trimmed, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		var event map[string]any
		if json.Unmarshal(data, &event) != nil {
			continue
		}
		typeName, _ := event["type"].(string)
		switch typeName {
		case "response.output_text.done":
			doneText, _ = event["text"].(string)
		case "response.output_text.delta":
			if delta, ok := event["delta"].(string); ok {
				deltaText.WriteString(delta)
			}
		case "response.completed":
			completed, _ = event["response"].(map[string]any)
		}
	}
	if strings.TrimSpace(doneText) != "" {
		return doneText
	}
	if completedText := responseObjectText(completed); completedText != "" {
		return completedText
	}
	return deltaText.String()
}

func responseObjectText(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if outputText, ok := payload["output_text"].(string); ok {
		return outputText
	}
	output, _ := payload["output"].([]any)
	var result strings.Builder
	for _, rawItem := range output {
		item, _ := rawItem.(map[string]any)
		result.WriteString(contentText(item["content"]))
	}
	return result.String()
}

func (s *Store) mergePrecedingCodexTitleRequest(db *gorm.DB, log *RelayRequestLog, rawBody []byte, mainStartedAt time.Time) (string, error) {
	if log.CodexSessionID == "" || log.CodexSessionSource != "prompt_cache_key" {
		return "", nil
	}
	var existingMainRequests int64
	if err := db.Model(&RelayRequestLog{}).
		Where("token_id = ? AND codex_session_id = ? AND codex_session_source NOT IN ?", log.TokenID, log.CodexSessionID, codexAuxiliarySessionSources).
		Count(&existingMainRequests).Error; err != nil {
		return "", err
	}
	if existingMainRequests > 0 {
		return "", nil
	}
	mainPromptHash := log.CodexPromptHash
	if mainPromptHash == "" {
		mainPromptHash = hashCodexPrompt(codexRequestPrompt(rawBody))
	}
	if mainPromptHash == "" {
		return "", nil
	}
	var candidates []RelayRequestLog
	if err := db.Select("id, token_id, codex_session_id, codex_session_source, session_name, codex_prompt_hash, codex_title_request, codex_generated_title, request_body, response_body, duration_ms, created_at").
		Where("token_id = ? AND codex_session_id <> ? AND codex_session_source = ? AND outcome = ?", log.TokenID, log.CodexSessionID, "prompt_cache_key", RelayOutcomeSuccess).
		Where("created_at >= ? AND created_at <= ?", mainStartedAt.Add(-codexTitleStartTolerance), mainStartedAt.Add(codexTitleMatchWindow)).
		Where("codex_title_request = ? OR request_parameters_json LIKE ?", true, `%"text_format":"json_schema"%`).
		Order("created_at DESC, id DESC").Limit(8).Find(&candidates).Error; err != nil {
		return "", err
	}
	for index := range candidates {
		candidate := &candidates[index]
		candidateStartedAt := candidate.CreatedAt.Add(-time.Duration(max(candidate.DurationMS, 0)) * time.Millisecond)
		if candidateStartedAt.Before(mainStartedAt.Add(-codexTitleStartTolerance)) || candidateStartedAt.After(mainStartedAt.Add(codexTitleStartTolerance)) {
			continue
		}
		candidatePromptHash := candidate.CodexPromptHash
		if candidatePromptHash == "" {
			titlePrompt, isTitleRequest := codexTitleRequestPrompt([]byte(decompressStoredPayload(candidate.RequestBody)))
			if !isTitleRequest {
				continue
			}
			candidatePromptHash = hashCodexPrompt(titlePrompt)
		}
		if candidatePromptHash != mainPromptHash {
			continue
		}
		var candidateRequestCount int64
		if err := db.Model(&RelayRequestLog{}).
			Where("token_id = ? AND codex_session_id = ?", candidate.TokenID, candidate.CodexSessionID).
			Count(&candidateRequestCount).Error; err != nil {
			return "", err
		}
		if candidateRequestCount != 1 {
			continue
		}
		title := candidate.CodexGeneratedTitle
		if title == "" {
			title = codexGeneratedTitle([]byte(decompressStoredPayload(candidate.ResponseBody)))
		}
		if title == "" {
			continue
		}
		return s.mergeCodexTitleLog(db, *candidate, log.CodexSessionID, title)
	}
	return "", nil
}

func (s *Store) mergeFollowingCodexTitleRequest(db *gorm.DB, log *RelayRequestLog, titleStartedAt time.Time) (string, string, error) {
	if log.CodexSessionID == "" || log.CodexSessionSource != "prompt_cache_key" || !log.CodexTitleRequest || log.CodexGeneratedTitle == "" || log.CodexPromptHash == "" {
		return "", "", nil
	}
	var candidates []RelayRequestLog
	if err := db.Select("id, token_id, codex_session_id, codex_session_source, codex_prompt_hash, request_body, duration_ms, created_at").
		Where("token_id = ? AND codex_session_id <> ? AND codex_session_id <> ''", log.TokenID, log.CodexSessionID).
		Where("codex_session_source = ? AND codex_title_request = ? AND outcome = ?", "prompt_cache_key", false, RelayOutcomeSuccess).
		Where("created_at >= ? AND created_at <= ?", titleStartedAt.Add(-codexTitleStartTolerance), titleStartedAt.Add(codexTitleMatchWindow)).
		Order("created_at DESC, id DESC").Limit(8).Find(&candidates).Error; err != nil {
		return "", "", err
	}
	for index := range candidates {
		candidate := &candidates[index]
		candidateStartedAt := candidate.CreatedAt.Add(-time.Duration(max(candidate.DurationMS, 0)) * time.Millisecond)
		if candidateStartedAt.Before(titleStartedAt.Add(-codexTitleStartTolerance)) || candidateStartedAt.After(titleStartedAt.Add(codexTitleStartTolerance)) {
			continue
		}
		candidatePromptHash := candidate.CodexPromptHash
		if candidatePromptHash == "" {
			candidatePromptHash = hashCodexPrompt(codexRequestPrompt([]byte(decompressStoredPayload(candidate.RequestBody))))
		}
		if candidatePromptHash != log.CodexPromptHash {
			continue
		}
		title, err := s.mergeCodexTitleLog(db, *log, candidate.CodexSessionID, log.CodexGeneratedTitle)
		if err != nil || title == "" {
			return "", "", err
		}
		if err := s.applyMergedCodexTitle(db, candidate.TokenID, candidate.CodexSessionID, candidate.ID, title, log.CreatedAt); err != nil {
			return "", "", err
		}
		return candidate.CodexSessionID, title, nil
	}
	return "", "", nil
}

func (s *Store) mergeCodexTitleLog(db *gorm.DB, candidate RelayRequestLog, canonicalSessionID string, generatedTitle string) (string, error) {
	title := generatedTitle
	var oldState RelaySessionState
	stateErr := db.Where("token_id = ? AND session_id = ?", candidate.TokenID, candidate.CodexSessionID).First(&oldState).Error
	if stateErr == nil && oldState.TitleCustomized && strings.TrimSpace(oldState.Title) != "" {
		title = oldState.Title
	} else if stateErr != nil && !errors.Is(stateErr, gorm.ErrRecordNotFound) {
		return "", stateErr
	}
	result := db.Model(&RelayRequestLog{}).
		Where("id = ? AND token_id = ? AND codex_session_id = ? AND codex_session_source = ?", candidate.ID, candidate.TokenID, candidate.CodexSessionID, "prompt_cache_key").
		Updates(map[string]any{
			"codex_session_id":     canonicalSessionID,
			"codex_session_source": codexTitleSessionSource,
			"session_name":         title,
		})
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected != 1 {
		return "", nil
	}
	if err := db.Where("token_id = ? AND session_id = ?", candidate.TokenID, candidate.CodexSessionID).Delete(&RelaySessionState{}).Error; err != nil {
		return "", err
	}
	return title, nil
}

func (s *Store) applyMergedCodexTitle(db *gorm.DB, tokenID uint64, sessionID string, requestID string, title string, now time.Time) error {
	var state RelaySessionState
	err := db.Where("token_id = ? AND session_id = ?", tokenID, sessionID).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return db.Create(&RelaySessionState{
			TokenID: tokenID, SessionID: sessionID, Title: title, LatestRequestID: requestID,
			CreatedAt: now, UpdatedAt: now,
		}).Error
	}
	if err != nil || state.TitleCustomized {
		return err
	}
	return db.Model(&RelaySessionState{}).
		Where("token_id = ? AND session_id = ?", tokenID, sessionID).
		Updates(map[string]any{"title": title, "updated_at": now}).Error
}

func (s *Store) backfillCodexAuxiliarySessions() error {
	const migrationName = "codex_auxiliary_sessions_v2"
	var migration GatewayMigration
	err := s.db.First(&migration, "name = ?", migrationName).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	cutoff := time.Now().UTC().Add(-DetailedLogRetentionDays * 24 * time.Hour)
	return s.db.Transaction(func(db *gorm.DB) error {
		var guardians []RelayRequestLog
		if err := db.Select("id, codex_session_id").
			Where("created_at >= ? AND codex_session_id LIKE ?", cutoff, codexGuardianSessionPrefix+"%").
			Find(&guardians).Error; err != nil {
			return err
		}
		for _, guardian := range guardians {
			canonical, source := loggedCodexSessionIdentity(guardian.CodexSessionID, "prompt_cache_key")
			if canonical == guardian.CodexSessionID {
				continue
			}
			if err := db.Model(&RelayRequestLog{}).Where("id = ?", guardian.ID).
				Updates(map[string]any{"codex_session_id": canonical, "codex_session_source": source}).Error; err != nil {
				return err
			}
		}

		var titleRequests []RelayRequestLog
		if err := db.Select("id, token_id, codex_session_id, codex_session_source, session_name, request_body, response_body, duration_ms, created_at").
			Where("created_at >= ? AND codex_session_source = ? AND outcome = ?", cutoff, "prompt_cache_key", RelayOutcomeSuccess).
			Where("request_parameters_json LIKE ?", `%"text_format":"json_schema"%`).
			Order("created_at ASC, id ASC").Find(&titleRequests).Error; err != nil {
			return err
		}
		for _, titleRequest := range titleRequests {
			titlePrompt, isTitleRequest := codexTitleRequestPrompt([]byte(decompressStoredPayload(titleRequest.RequestBody)))
			if !isTitleRequest {
				continue
			}
			title := codexGeneratedTitle([]byte(decompressStoredPayload(titleRequest.ResponseBody)))
			if title == "" {
				continue
			}
			var possibleMainRequests []RelayRequestLog
			if err := db.Select("id, token_id, codex_session_id, codex_session_source, request_body, duration_ms, created_at").
				Where("token_id = ? AND codex_session_id <> ? AND codex_session_id <> '' AND codex_session_source = ?", titleRequest.TokenID, titleRequest.CodexSessionID, "prompt_cache_key").
				Where("created_at >= ? AND created_at <= ?", titleRequest.CreatedAt.Add(-codexTitleMatchWindow), titleRequest.CreatedAt.Add(codexTitleMatchWindow)).
				Order("created_at ASC, id ASC").Limit(20).Find(&possibleMainRequests).Error; err != nil {
				return err
			}
			titleStartedAt := titleRequest.CreatedAt.Add(-time.Duration(max(titleRequest.DurationMS, 0)) * time.Millisecond)
			for _, mainRequest := range possibleMainRequests {
				mainStartedAt := mainRequest.CreatedAt.Add(-time.Duration(max(mainRequest.DurationMS, 0)) * time.Millisecond)
				if mainStartedAt.Before(titleStartedAt.Add(-codexTitleStartTolerance)) || mainStartedAt.After(titleStartedAt.Add(codexTitleStartTolerance)) {
					continue
				}
				if codexRequestPrompt([]byte(decompressStoredPayload(mainRequest.RequestBody))) != titlePrompt {
					continue
				}
				mergedTitle, err := s.mergeCodexTitleLog(db, titleRequest, mainRequest.CodexSessionID, title)
				if err != nil {
					return err
				}
				if mergedTitle != "" {
					if err := s.applyMergedCodexTitle(db, mainRequest.TokenID, mainRequest.CodexSessionID, mainRequest.ID, mergedTitle, mainRequest.CreatedAt); err != nil {
						return err
					}
				}
				break
			}
		}
		return db.Create(&GatewayMigration{Name: migrationName, AppliedAt: time.Now()}).Error
	})
}
