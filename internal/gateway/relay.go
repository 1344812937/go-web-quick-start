package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/1344812937/go-web-quick-start/internal/config"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const statusClientClosedRequest = 499

type PublicError struct {
	Status  int
	Message string
	Type    string
	Code    string
}

func (e *PublicError) Error() string {
	return e.Message
}

type RelayService struct {
	store         *Store
	router        *Router
	estimator     *TokenEstimator
	configManager *config.ApplicationConfigManager
	client        *http.Client
}

type relayExecution struct {
	requestID                string
	token                    *ClientToken
	modelID                  uint64
	sessionAffinityMappingID uint64
	endpoint                 string
	payload                  *RelayPayload
	rawBody                  []byte
	inputTokens              int64
	startedAt                time.Time
	attempts                 int
	usage                    Usage
	normalInputTokens        int64
	sentTokens               int64
	estimatedCost            int64
	upstreamCost             int64
	usageSources             map[string]struct{}
	costSources              map[string]struct{}
}

type attemptResult struct {
	response      *http.Response
	body          []byte
	usage         Usage
	sentTokens    int64
	estimatedCost int64
	upstreamCost  int64
	costSource    string
	latencyMS     int64
	streamError   error
}

func NewRelayService(store *Store, router *Router, estimator *TokenEstimator, configManager *config.ApplicationConfigManager) *RelayService {
	headerTimeout := 120 * time.Second
	if cfg := configManager.GetConfig(); cfg != nil && cfg.GatewayConfig.ResponseHeaderTimeoutSeconds > 0 {
		headerTimeout = time.Duration(cfg.GatewayConfig.ResponseHeaderTimeoutSeconds) * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = headerTimeout
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 20
	return &RelayService{
		store:         store,
		router:        router,
		estimator:     estimator,
		configManager: configManager,
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (s *RelayService) Relay(ctx context.Context, writer http.ResponseWriter, headers http.Header, rawQuery string, endpoint string, token *ClientToken, payload *RelayPayload, rawBody []byte) *PublicError {
	execution := &relayExecution{
		requestID:    uuid.NewString(),
		token:        token,
		endpoint:     endpoint,
		payload:      payload,
		rawBody:      rawBody,
		inputTokens:  s.estimator.EstimateJSON(rawBody),
		startedAt:    time.Now(),
		usageSources: make(map[string]struct{}),
		costSources:  make(map[string]struct{}),
	}
	writer.Header().Set("X-Request-Id", execution.requestID)

	plan, err := s.router.Plan(ctx, token, payload.Model, execution.inputTokens, payload.DeclaredMaxOutput, payload.PreviousResponseID, payload.SessionKey)
	if err != nil {
		publicErr := routePublicError(err)
		s.recordRequest(context.WithoutCancel(ctx), execution, publicErr.Status, publicErr.Code)
		return publicErr
	}
	execution.modelID = plan.Model.ID
	execution.sessionAffinityMappingID = plan.SessionAffinityMappingID

	maxAttempts := 3
	if cfg := s.configManager.GetConfig(); cfg != nil && cfg.GatewayConfig.MaxAttempts > 0 {
		maxAttempts = min(cfg.GatewayConfig.MaxAttempts, 3)
	}
	maxAttempts = min(maxAttempts, len(plan.Candidates))
	var lastNetworkError error

	for index := 0; index < maxAttempts; index++ {
		candidate := plan.Candidates[index]
		execution.attempts++
		result, attemptErr := s.performAttempt(ctx, writer, headers, rawQuery, execution, candidate, index == maxAttempts-1, plan.Affinity)
		if result != nil {
			s.addUsage(execution, result.usage, result.estimatedCost, result.upstreamCost, result.costSource, false)
		}
		if attemptErr == nil {
			return nil
		}
		lastNetworkError = attemptErr
		if ctx.Err() != nil || errors.Is(attemptErr, context.Canceled) {
			lastNetworkError = context.Canceled
			break
		}
		if plan.Affinity {
			publicErr := &PublicError{Status: http.StatusServiceUnavailable, Message: "The channel associated with previous_response_id is unavailable.", Type: "api_error", Code: "response_affinity_unavailable"}
			s.recordRequest(context.WithoutCancel(ctx), execution, publicErr.Status, publicErr.Code)
			return publicErr
		}
	}

	message := "All available upstream channels failed."
	status := http.StatusBadGateway
	code := "upstream_unavailable"
	if errors.Is(lastNetworkError, context.Canceled) {
		message = "The request was canceled."
		status = statusClientClosedRequest
		code = "request_canceled"
	}
	publicErr := &PublicError{Status: status, Message: message, Type: "api_error", Code: code}
	s.recordRequest(context.WithoutCancel(ctx), execution, publicErr.Status, publicErr.Code)
	return publicErr
}

func routePublicError(err error) *PublicError {
	switch {
	case errors.Is(err, ErrModelNotAllowed):
		return &PublicError{Status: http.StatusForbidden, Message: "The API token is not allowed to use this model.", Type: "invalid_request_error", Code: "model_not_allowed"}
	case errors.Is(err, ErrModelNotFound):
		return &PublicError{Status: http.StatusNotFound, Message: "The requested model does not exist or is not available.", Type: "invalid_request_error", Code: "model_not_found"}
	case errors.Is(err, ErrAffinityUnavailable):
		return &PublicError{Status: http.StatusServiceUnavailable, Message: "The channel associated with previous_response_id is unavailable.", Type: "api_error", Code: "response_affinity_unavailable"}
	case errors.Is(err, ErrNoAvailableChannel):
		return &PublicError{Status: http.StatusServiceUnavailable, Message: "No upstream channel is currently available for this model.", Type: "api_error", Code: "no_available_channel"}
	default:
		return &PublicError{Status: http.StatusInternalServerError, Message: "The gateway could not route this request.", Type: "api_error", Code: "gateway_error"}
	}
}

func (s *RelayService) performAttempt(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, rawQuery string, execution *relayExecution, candidate RouteCandidate, lastAttempt bool, affinity bool) (*attemptResult, error) {
	body, err := execution.payload.UpstreamBody(candidate.Mapping.UpstreamModel, execution.endpoint, candidate.Channel.SupportsStreamUsage)
	if err != nil {
		return nil, err
	}
	apiKey, err := s.store.secretBox.Decrypt(candidate.Channel.APIKeyCipher)
	if err != nil {
		return nil, err
	}
	upstreamURL := candidate.Channel.BaseURL + "/" + endpointPath(execution.endpoint)
	if rawQuery != "" {
		upstreamURL += "?" + rawQuery
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	copyUpstreamRequestHeaders(request.Header, incomingHeaders)
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Gateway-Request-Id", execution.requestID)

	sentTokens := s.estimator.EstimateJSON(body)
	execution.sentTokens += sentTokens
	started := time.Now()
	response, requestErr := s.client.Do(request)
	latency := time.Since(started).Milliseconds()
	logCtx := context.WithoutCancel(ctx)
	if requestErr != nil {
		if ctx.Err() == nil && !errors.Is(requestErr, context.Canceled) {
			s.recordChannelFailure(logCtx, candidate.Channel.ID, requestErr.Error())
		}
		s.recordAttempt(logCtx, execution, candidate, attemptResult{sentTokens: sentTokens, latencyMS: latency}, 0, false, requestErr)
		return nil, requestErr
	}

	response.Body = s.withIdleTimeout(response.Body)
	if shouldRetryStatus(response.StatusCode) {
		responseBody, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		usage, _ := ParseUsage(responseBody)
		result := &attemptResult{response: response, body: responseBody, usage: usage, sentTokens: sentTokens, costSource: CostSourceFailedZero, latencyMS: latency}
		s.recordChannelFailure(logCtx, candidate.Channel.ID, fmt.Sprintf("HTTP %d", response.StatusCode))
		s.recordAttempt(logCtx, execution, candidate, *result, response.StatusCode, false, readErr)
		if readErr != nil {
			return result, readErr
		}
		if lastAttempt && !affinity {
			s.addUsage(execution, usage, 0, 0, CostSourceFailedZero, false)
			s.recordRequest(logCtx, execution, response.StatusCode, upstreamErrorCode(responseBody))
			writeBufferedResponse(writer, response, responseBody)
			return nil, nil
		}
		return result, fmt.Errorf("upstream returned HTTP %d", response.StatusCode)
	}

	if execution.payload.Stream && isEventStream(response.Header) && response.StatusCode >= 200 && response.StatusCode < 300 {
		return s.streamResponse(ctx, writer, execution, candidate, response, latency, sentTokens)
	}

	responseBody, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	usage, hasUsage := ParseUsage(responseBody)
	if !hasUsage && response.StatusCode >= 200 && response.StatusCode < 300 {
		usage = Usage{InputTokens: execution.inputTokens, OutputTokens: s.estimator.EstimateJSON(responseBody), Source: "estimated_tiktoken"}
	}
	success := response.StatusCode >= 200 && response.StatusCode < 300 && readErr == nil
	estimatedCost, upstreamCost, costSource := attemptCosts(candidate.Mapping, usage, responseBody, success)
	result := &attemptResult{response: response, body: responseBody, usage: usage, sentTokens: sentTokens, estimatedCost: estimatedCost, upstreamCost: upstreamCost, costSource: costSource, latencyMS: latency}
	if readErr != nil {
		s.recordChannelFailure(logCtx, candidate.Channel.ID, readErr.Error())
	} else if success {
		s.recordChannelSuccess(logCtx, candidate.Channel.ID, latency)
	} else if !shouldRetryStatus(response.StatusCode) {
		s.recordChannelResponsive(logCtx, candidate.Channel.ID)
	}
	s.recordAttempt(logCtx, execution, candidate, *result, response.StatusCode, success, readErr)
	if readErr != nil {
		return result, readErr
	}
	s.addUsage(execution, usage, estimatedCost, upstreamCost, costSource, success)
	code := upstreamErrorCode(responseBody)
	s.recordRequest(logCtx, execution, response.StatusCode, code)
	if execution.endpoint == "responses" && success {
		s.router.RecordAffinity(logCtx, ResponseID(responseBody), candidate.Mapping.ID)
	}
	if success {
		s.router.RecordSessionAffinityAfterSuccess(logCtx, execution.token.ID, execution.modelID, execution.payload.SessionKey, execution.sessionAffinityMappingID, candidate.Mapping.ID)
	}
	writeBufferedResponse(writer, response, responseBody)
	return nil, nil
}

func (s *RelayService) streamResponse(ctx context.Context, writer http.ResponseWriter, execution *relayExecution, candidate RouteCandidate, response *http.Response, latency int64, sentTokens int64) (*attemptResult, error) {
	reader := bufio.NewReader(response.Body)
	firstEvent, err := readSSEEvent(reader)
	if err != nil || len(firstEvent) == 0 {
		_ = response.Body.Close()
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		logCtx := context.WithoutCancel(ctx)
		if ctx.Err() == nil && !errors.Is(err, context.Canceled) {
			s.recordChannelFailure(logCtx, candidate.Channel.ID, err.Error())
		}
		result := attemptResult{response: response, sentTokens: sentTokens, latencyMS: latency, streamError: err}
		s.recordAttempt(logCtx, execution, candidate, result, response.StatusCode, false, err)
		return &result, err
	}

	copyUpstreamResponseHeaders(writer.Header(), response.Header, true)
	writer.WriteHeader(response.StatusCode)
	flusher, _ := writer.(http.Flusher)
	usage := Usage{}
	upstreamCost := upstreamCostSnapshot{}
	outputEstimate := int64(0)
	responseID := ""
	consumeSSEEvent(firstEvent, s.estimator, &usage, &upstreamCost, &outputEstimate, &responseID)
	if _, writeErr := writer.Write(firstEvent); writeErr != nil {
		_ = response.Body.Close()
		s.finishStream(ctx, execution, candidate, response, latency, sentTokens, usage, upstreamCost, outputEstimate, responseID, writeErr, true)
		return nil, nil
	}
	if flusher != nil {
		flusher.Flush()
	}

	streamErr := error(nil)
	downstreamError := false
	for {
		event, readErr := readSSEEvent(reader)
		if len(event) > 0 {
			consumeSSEEvent(event, s.estimator, &usage, &upstreamCost, &outputEstimate, &responseID)
			if _, writeErr := writer.Write(event); writeErr != nil {
				streamErr = writeErr
				downstreamError = true
				break
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				streamErr = readErr
			}
			break
		}
	}
	_ = response.Body.Close()
	s.finishStream(ctx, execution, candidate, response, latency, sentTokens, usage, upstreamCost, outputEstimate, responseID, streamErr, downstreamError)
	return nil, nil
}

func (s *RelayService) finishStream(ctx context.Context, execution *relayExecution, candidate RouteCandidate, response *http.Response, latency int64, sentTokens int64, usage Usage, upstreamSnapshot upstreamCostSnapshot, outputEstimate int64, responseID string, streamErr error, downstreamError bool) {
	if usage.Source == "" {
		usage = Usage{InputTokens: execution.inputTokens, OutputTokens: outputEstimate, Source: "estimated_tiktoken"}
	}
	logCtx := context.WithoutCancel(ctx)
	clientCanceled := downstreamError || ctx.Err() != nil || errors.Is(streamErr, context.Canceled)
	success := streamErr == nil && !clientCanceled
	estimatedCost := int64(0)
	upstreamCost := int64(0)
	costSource := CostSourceFailedZero
	if success {
		estimatedCost = CalculateCostMicros(candidate.Mapping, usage)
		upstreamCost = estimatedCost
		costSource = CostSourceFallback
		if upstreamSnapshot.valid {
			upstreamCost = upstreamSnapshot.micros
			costSource = CostSourceUpstream
		}
	}
	result := attemptResult{response: response, usage: usage, sentTokens: sentTokens, estimatedCost: estimatedCost, upstreamCost: upstreamCost, costSource: costSource, latencyMS: latency, streamError: streamErr}
	if streamErr == nil || clientCanceled {
		s.recordChannelSuccess(logCtx, candidate.Channel.ID, latency)
	} else {
		s.recordChannelFailure(logCtx, candidate.Channel.ID, streamErr.Error())
	}
	s.recordAttempt(logCtx, execution, candidate, result, response.StatusCode, success, streamErr)
	s.addUsage(execution, usage, estimatedCost, upstreamCost, costSource, success)
	requestStatus := response.StatusCode
	errorCode := ""
	if clientCanceled {
		requestStatus = statusClientClosedRequest
		errorCode = "request_canceled"
	} else if streamErr != nil {
		errorCode = "stream_interrupted"
	}
	s.recordRequest(logCtx, execution, requestStatus, errorCode)
	if execution.endpoint == "responses" && streamErr == nil {
		s.router.RecordAffinity(logCtx, responseID, candidate.Mapping.ID)
	}
	if streamErr == nil {
		s.router.RecordSessionAffinityAfterSuccess(logCtx, execution.token.ID, execution.modelID, execution.payload.SessionKey, execution.sessionAffinityMappingID, candidate.Mapping.ID)
	}
}

func endpointPath(endpoint string) string {
	if endpoint == "responses" {
		return "responses"
	}
	return "chat/completions"
}

func shouldRetryStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

func isEventStream(header http.Header) bool {
	return strings.Contains(strings.ToLower(header.Get("Content-Type")), "text/event-stream")
}

func readSSEEvent(reader *bufio.Reader) ([]byte, error) {
	var event bytes.Buffer
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			event.Write(line)
			if bytes.Equal(line, []byte("\n")) || bytes.Equal(line, []byte("\r\n")) {
				return event.Bytes(), nil
			}
		}
		if err != nil {
			return event.Bytes(), err
		}
	}
}

type upstreamCostSnapshot struct {
	micros int64
	valid  bool
}

func consumeSSEEvent(event []byte, estimator *TokenEstimator, usage *Usage, upstreamCost *upstreamCostSnapshot, outputEstimate *int64, responseID *string) {
	for _, line := range bytes.Split(event, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		if parsed, ok := ParseUsage(data); ok {
			*usage = parsed
		}
		if micros, ok := ParseUpstreamCostMicros(data); ok {
			upstreamCost.micros = micros
			upstreamCost.valid = true
		}
		*outputEstimate += estimator.EstimateJSON(data)
		if *responseID == "" {
			*responseID = ResponseID(data)
		}
	}
}

func copyUpstreamRequestHeaders(destination http.Header, source http.Header) {
	for key, values := range source {
		canonical := textproto.CanonicalMIMEHeaderKey(key)
		if requestHeaderBlocked(canonical) {
			continue
		}
		for _, value := range values {
			destination.Add(canonical, value)
		}
	}
}

func requestHeaderBlocked(key string) bool {
	switch key {
	case "Authorization", "Cookie", "Content-Length", "Connection", "Proxy-Authorization", "Proxy-Connection", "Transfer-Encoding", "Upgrade", "Host", "Origin", "Referer", "Accept-Encoding", "Forwarded":
		return true
	}
	return strings.HasPrefix(key, "X-Forwarded-")
}

func copyUpstreamResponseHeaders(destination http.Header, source http.Header, streaming bool) {
	for key, values := range source {
		canonical := textproto.CanonicalMIMEHeaderKey(key)
		if canonical == "Connection" || canonical == "Transfer-Encoding" || canonical == "Keep-Alive" || canonical == "Proxy-Authenticate" || canonical == "Trailer" || canonical == "Upgrade" || canonical == "Set-Cookie" || canonical == "Set-Cookie2" || (streaming && canonical == "Content-Length") {
			continue
		}
		for _, value := range values {
			destination.Add(canonical, value)
		}
	}
}

func writeBufferedResponse(writer http.ResponseWriter, response *http.Response, body []byte) {
	copyUpstreamResponseHeaders(writer.Header(), response.Header, false)
	writer.WriteHeader(response.StatusCode)
	_, _ = writer.Write(body)
}

func (s *RelayService) withIdleTimeout(body io.ReadCloser) io.ReadCloser {
	timeout := 300 * time.Second
	if cfg := s.configManager.GetConfig(); cfg != nil && cfg.GatewayConfig.StreamIdleTimeoutSeconds > 0 {
		timeout = time.Duration(cfg.GatewayConfig.StreamIdleTimeoutSeconds) * time.Second
	}
	return &idleReadCloser{ReadCloser: body, timeout: timeout}
}

type idleReadCloser struct {
	io.ReadCloser
	timeout time.Duration
}

type readResult struct {
	n   int
	err error
}

func (r *idleReadCloser) Read(buffer []byte) (int, error) {
	result := make(chan readResult, 1)
	go func() {
		n, err := r.ReadCloser.Read(buffer)
		result <- readResult{n: n, err: err}
	}()
	timer := time.NewTimer(r.timeout)
	defer timer.Stop()
	select {
	case completed := <-result:
		return completed.n, completed.err
	case <-timer.C:
		_ = r.ReadCloser.Close()
		completed := <-result
		if completed.n > 0 {
			return completed.n, nil
		}
		return 0, errors.New("upstream stream idle timeout")
	}
}

func attemptCosts(mapping ChannelModel, usage Usage, responseBody []byte, success bool) (int64, int64, string) {
	if !success {
		return 0, 0, CostSourceFailedZero
	}
	estimatedCost := CalculateCostMicros(mapping, usage)
	if upstreamCost, ok := ParseUpstreamCostMicros(responseBody); ok {
		return estimatedCost, upstreamCost, CostSourceUpstream
	}
	return estimatedCost, estimatedCost, CostSourceFallback
}

func (s *RelayService) addUsage(execution *relayExecution, usage Usage, estimatedCost int64, upstreamCost int64, costSource string, billable bool) {
	execution.usage.InputTokens += usage.InputTokens
	execution.normalInputTokens += normalInputTokens(usage)
	execution.usage.OutputTokens += usage.OutputTokens
	execution.usage.CachedTokens += usage.CachedTokens
	execution.usage.CacheWriteTokens += usage.CacheWriteTokens
	if usage.Source != "" {
		execution.usageSources[usage.Source] = struct{}{}
	}
	if billable {
		execution.estimatedCost += estimatedCost
		execution.upstreamCost += upstreamCost
		if costSource != "" {
			execution.costSources[costSource] = struct{}{}
		}
	}
}

func (s *RelayService) recordAttempt(ctx context.Context, execution *relayExecution, candidate RouteCandidate, result attemptResult, status int, success bool, attemptErr error) {
	message := ""
	if attemptErr != nil {
		message = attemptErr.Error()
		if len(message) > 2000 {
			message = message[:2000]
		}
	}
	if !success {
		result.estimatedCost = 0
		result.upstreamCost = 0
		result.costSource = CostSourceFailedZero
	}
	log := RelayAttemptLog{
		RequestID:         execution.requestID,
		ChannelID:         candidate.Channel.ID,
		ChannelName:       candidate.Channel.Name,
		ChannelBaseURL:    candidate.Channel.BaseURL,
		ChannelModelID:    candidate.Mapping.ID,
		UpstreamModel:     candidate.Mapping.UpstreamModel,
		StatusCode:        status,
		InputTokens:       result.usage.InputTokens,
		NormalInputTokens: normalInputTokens(result.usage),
		OutputTokens:      result.usage.OutputTokens,
		CachedTokens:      result.usage.CachedTokens,
		CacheWriteTokens:  result.usage.CacheWriteTokens,
		SentTokens:        result.sentTokens,
		EstimatedCost:     result.estimatedCost,
		UpstreamCost:      result.upstreamCost,
		CostSource:        result.costSource,
		UsageSource:       result.usage.Source,
		LatencyMS:         result.latencyMS,
		Success:           success,
		ErrorMessage:      message,
	}
	_ = s.store.db.WithContext(ctx).Create(&log).Error
}

func (s *RelayService) recordRequest(ctx context.Context, execution *relayExecution, status int, errorCode string) {
	usageSource := ""
	if len(execution.usageSources) == 1 {
		for source := range execution.usageSources {
			usageSource = source
		}
	} else if len(execution.usageSources) > 1 {
		usageSource = "mixed"
	}
	costSource := CostSourceFailedZero
	if len(execution.costSources) == 1 {
		for source := range execution.costSources {
			costSource = source
		}
	} else if len(execution.costSources) > 1 {
		costSource = CostSourceMixed
	}
	estimatedCost := execution.estimatedCost
	upstreamCost := execution.upstreamCost
	if status < 200 || status >= 300 {
		estimatedCost = 0
		upstreamCost = 0
		costSource = CostSourceFailedZero
	}
	now := time.Now().UTC()
	log := RelayRequestLog{
		ID:                    execution.requestID,
		TokenID:               execution.token.ID,
		TokenName:             execution.token.Name,
		TokenKeyPrefix:        execution.token.KeyPrefix,
		Endpoint:              execution.endpoint,
		RequestedModel:        execution.payload.Model,
		CodexSessionID:        truncateRunes(execution.payload.SessionKey, 512),
		CodexSessionSource:    execution.payload.SessionSource,
		RequestParametersJSON: execution.payload.RequestParametersJSON,
		StatusCode:            status,
		InputTokens:           execution.usage.InputTokens,
		NormalInputTokens:     execution.normalInputTokens,
		OutputTokens:          execution.usage.OutputTokens,
		CachedTokens:          execution.usage.CachedTokens,
		CacheWriteTokens:      execution.usage.CacheWriteTokens,
		SentTokens:            execution.sentTokens,
		EstimatedCost:         estimatedCost,
		UpstreamCost:          upstreamCost,
		CostSource:            costSource,
		UsageSource:           usageSource,
		AttemptCount:          execution.attempts,
		DurationMS:            time.Since(execution.startedAt).Milliseconds(),
		Stream:                execution.payload.Stream,
		ErrorCode:             errorCode,
		CreatedAt:             now,
	}
	successCount := int64(0)
	if status >= 200 && status < 300 {
		successCount = 1
	}
	stat := TokenDailyStat{
		Date:              now.Format(time.DateOnly),
		TokenID:           execution.token.ID,
		RequestCount:      1,
		SuccessCount:      successCount,
		InputTokens:       log.InputTokens,
		NormalInputTokens: log.NormalInputTokens,
		OutputTokens:      log.OutputTokens,
		CachedTokens:      log.CachedTokens,
		CacheWriteTokens:  log.CacheWriteTokens,
		SentTokens:        log.SentTokens,
		EstimatedCost:     log.EstimatedCost,
		UpstreamCost:      log.UpstreamCost,
		DurationMS:        log.DurationMS,
		AttemptCount:      int64(log.AttemptCount),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	_ = s.store.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		if err := db.Create(&log).Error; err != nil {
			return err
		}
		return db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "date"}, {Name: "token_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"request_count":       gorm.Expr("request_count + excluded.request_count"),
				"success_count":       gorm.Expr("success_count + excluded.success_count"),
				"input_tokens":        gorm.Expr("input_tokens + excluded.input_tokens"),
				"normal_input_tokens": gorm.Expr("normal_input_tokens + excluded.normal_input_tokens"),
				"output_tokens":       gorm.Expr("output_tokens + excluded.output_tokens"),
				"cached_tokens":       gorm.Expr("cached_tokens + excluded.cached_tokens"),
				"cache_write_tokens":  gorm.Expr("cache_write_tokens + excluded.cache_write_tokens"),
				"sent_tokens":         gorm.Expr("sent_tokens + excluded.sent_tokens"),
				"estimated_cost":      gorm.Expr("estimated_cost + excluded.estimated_cost"),
				"upstream_cost":       gorm.Expr("upstream_cost + excluded.upstream_cost"),
				"duration_ms":         gorm.Expr("duration_ms + excluded.duration_ms"),
				"attempt_count":       gorm.Expr("attempt_count + excluded.attempt_count"),
				"updated_at":          now,
			}),
		}).Create(&stat).Error
	})
}

func (s *RelayService) recordChannelFailure(ctx context.Context, channelID uint64, message string) {
	if len(message) > 2000 {
		message = message[:2000]
	}
	now := time.Now()
	_ = s.store.db.WithContext(ctx).Model(&Channel{}).Where("id = ?", channelID).Updates(map[string]any{
		"consecutive_failures": gorm.Expr("consecutive_failures + 1"),
		"last_error":           message,
		"last_health_at":       now,
	}).Error
	openUntil := now.Add(60 * time.Second)
	_ = s.store.db.WithContext(ctx).Model(&Channel{}).Where("id = ? AND consecutive_failures >= 3", channelID).Update("circuit_open_until", openUntil).Error
}

func (s *RelayService) recordChannelSuccess(ctx context.Context, channelID uint64, latencyMS int64) {
	_ = s.store.db.WithContext(ctx).Model(&Channel{}).Where("id = ?", channelID).Updates(map[string]any{
		"consecutive_failures": 0,
		"circuit_open_until":   nil,
		"last_error":           "",
		"last_health_at":       time.Now(),
		"latency_ewma": gorm.Expr(
			"CASE WHEN latency_ewma <= 0 THEN ? ELSE latency_ewma * 0.8 + ? * 0.2 END",
			latencyMS, latencyMS,
		),
	}).Error
}

func (s *RelayService) recordChannelResponsive(ctx context.Context, channelID uint64) {
	_ = s.store.db.WithContext(ctx).Model(&Channel{}).Where("id = ?", channelID).Updates(map[string]any{
		"consecutive_failures": 0,
		"circuit_open_until":   nil,
		"last_health_at":       time.Now(),
	}).Error
}

func upstreamErrorCode(body []byte) string {
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		return payload.Error.Code
	}
	return ""
}
