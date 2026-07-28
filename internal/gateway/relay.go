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
	"unicode/utf8"

	"github.com/1344812937/go-web-quick-start/internal/config"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	statusClientClosedRequest      = 499
	maxDetailedPayloadBytes        = 4 << 20
	maxPreTokenStreamBufferBytes   = 256 << 10
	upstreamApplicationErrorStatus = http.StatusBadGateway
)

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
	firstTokenMS             int64
	latencyMS                int64
	durationMS               int64
	responseBody             []byte
	responseBodyTruncated    bool
}

type attemptResult struct {
	response         *http.Response
	body             []byte
	requestBody      []byte
	bodyTruncated    bool
	usage            Usage
	sentTokens       int64
	estimatedCost    int64
	upstreamCost     int64
	costSource       string
	firstTokenMS     int64
	latencyMS        int64
	durationMS       int64
	streamError      error
	outcome          string
	retryReason      string
	retryDetail      string
	circuitOpenUntil *time.Time
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
		execution.responseBody = publicErrorBody(publicErr)
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
	selection := plan.InitialSelection

	for index := 0; index < maxAttempts; index++ {
		candidate := plan.Candidates[index]
		execution.attempts++
		result, attemptErr := s.performAttempt(ctx, writer, headers, rawQuery, execution, candidate, selection, index == maxAttempts-1, plan.Affinity)
		if result != nil {
			s.addUsage(execution, result.usage, result.estimatedCost, result.upstreamCost, result.costSource, result.outcome == RelayOutcomeCanceled)
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
			execution.responseBody = publicErrorBody(publicErr)
			s.recordRequest(context.WithoutCancel(ctx), execution, publicErr.Status, publicErr.Code)
			return publicErr
		}
		selection = retrySelection(candidate, result)
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
	execution.responseBody = publicErrorBody(publicErr)
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

func (s *RelayService) performAttempt(ctx context.Context, writer http.ResponseWriter, incomingHeaders http.Header, rawQuery string, execution *relayExecution, candidate RouteCandidate, selection RouteSelection, lastAttempt bool, affinity bool) (*attemptResult, error) {
	execution.latencyMS = 0
	execution.durationMS = 0
	body, err := execution.payload.UpstreamBody(candidate.Mapping.UpstreamModel, execution.endpoint, candidate.Channel.SupportsStreamUsage)
	if err != nil {
		return s.recordPreparationFailure(ctx, execution, candidate, selection, "payload_transform", nil, err)
	}
	apiKey, err := s.store.secretBox.Decrypt(candidate.Channel.APIKeyCipher)
	if err != nil {
		return s.recordPreparationFailure(ctx, execution, candidate, selection, "credential_decrypt", body, err)
	}
	upstreamURL := candidate.Channel.BaseURL + "/" + endpointPath(execution.endpoint)
	if rawQuery != "" {
		upstreamURL += "?" + rawQuery
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return s.recordPreparationFailure(ctx, execution, candidate, selection, "request_build", body, err)
	}
	copyUpstreamRequestHeaders(request.Header, incomingHeaders)
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Gateway-Request-Id", execution.requestID)

	sentTokens := s.estimator.EstimateJSON(body)
	execution.sentTokens += sentTokens
	started := time.Now()
	response, requestErr := s.client.Do(request)
	responseReceivedAt := time.Now()
	latency := elapsedMilliseconds(started, responseReceivedAt)
	logCtx := context.WithoutCancel(ctx)
	if requestErr != nil {
		execution.durationMS = elapsedMilliseconds(execution.startedAt, responseReceivedAt)
		result := &attemptResult{
			requestBody: body,
			sentTokens:  sentTokens,
			latencyMS:   latency,
			durationMS:  latency,
			retryReason: SelectionReasonTransportError,
			retryDetail: "upstream_request",
		}
		if ctx.Err() != nil || errors.Is(requestErr, context.Canceled) {
			result.outcome = RelayOutcomeCanceled
			result.usage = Usage{InputTokens: execution.inputTokens, Source: "estimated_tiktoken"}
			result.estimatedCost = CalculateCostMicros(candidate.Mapping, result.usage)
			result.upstreamCost = result.estimatedCost
			result.costSource = CostSourceFallback
		}
		if ctx.Err() == nil && !errors.Is(requestErr, context.Canceled) {
			result.circuitOpenUntil = s.recordChannelFailure(logCtx, candidate.Channel.ID, requestErr.Error())
		}
		s.recordAttempt(logCtx, execution, candidate, selection, *result, 0, false, requestErr)
		return result, requestErr
	}
	execution.latencyMS = elapsedMilliseconds(execution.startedAt, responseReceivedAt)

	response.Body = s.withIdleTimeout(response.Body)
	if execution.payload.Stream && isEventStream(response.Header) && response.StatusCode >= 200 && response.StatusCode < 300 {
		return s.streamResponse(ctx, writer, execution, candidate, selection, response, started, latency, sentTokens, body)
	}

	responseBody, readErr := io.ReadAll(response.Body)
	responseFinishedAt := time.Now()
	_ = response.Body.Close()
	execution.durationMS = elapsedMilliseconds(execution.startedAt, responseFinishedAt)
	usage, hasUsage := ParseUsage(responseBody)
	channelFailure, channelFailureDetail := upstreamChannelFailure(response.StatusCode, responseBody)
	if shouldRetryStatus(response.StatusCode) || channelFailure {
		result := &attemptResult{
			response: response, body: responseBody, requestBody: body, usage: usage, sentTokens: sentTokens,
			costSource: CostSourceFailedZero, latencyMS: latency, durationMS: elapsedMilliseconds(started, responseFinishedAt),
			retryReason: SelectionReasonRetryableStatus, retryDetail: fmt.Sprintf("HTTP %d", response.StatusCode),
		}
		attemptErr := readErr
		failureMessage := fmt.Sprintf("HTTP %d", response.StatusCode)
		if attemptErr == nil {
			attemptErr = fmt.Errorf("upstream returned HTTP %d", response.StatusCode)
		}
		if channelFailure {
			failureMessage = channelFailureDetail
			result.retryDetail = truncateRunes(channelFailureDetail, 512)
			attemptErr = errors.New(channelFailureDetail)
		}
		if readErr != nil {
			result.retryReason = SelectionReasonResponseError
			result.retryDetail = "response_body_read"
		}
		if channelFailure {
			result.circuitOpenUntil = s.recordChannelUnavailable(logCtx, candidate.Channel.ID, failureMessage)
		} else {
			result.circuitOpenUntil = s.recordChannelFailure(logCtx, candidate.Channel.ID, failureMessage)
		}
		s.recordAttempt(logCtx, execution, candidate, selection, *result, response.StatusCode, false, attemptErr)
		if readErr != nil {
			return result, readErr
		}
		if lastAttempt && !affinity {
			s.addUsage(execution, usage, 0, 0, CostSourceFailedZero, false)
			execution.responseBody = responseBody
			s.recordRequest(logCtx, execution, response.StatusCode, upstreamErrorCode(responseBody))
			writeBufferedResponse(writer, response, responseBody)
			return nil, nil
		}
		return result, attemptErr
	}

	if appErr, failed := upstreamApplicationError(responseBody); failed && response.StatusCode >= 200 && response.StatusCode < 300 && readErr == nil {
		result := &attemptResult{
			response: response, body: responseBody, requestBody: body, usage: usage, sentTokens: sentTokens,
			costSource: CostSourceFailedZero, latencyMS: latency, durationMS: elapsedMilliseconds(started, responseFinishedAt),
			retryReason: SelectionReasonUpstreamApplicationError, retryDetail: truncateRunes(appErr.Message, 512),
		}
		result.circuitOpenUntil = s.recordChannelFailure(logCtx, candidate.Channel.ID, appErr.Error())
		s.recordAttempt(logCtx, execution, candidate, selection, *result, response.StatusCode, false, appErr)
		return result, appErr
	}
	if !hasUsage && response.StatusCode >= 200 && response.StatusCode < 300 {
		usage = Usage{InputTokens: execution.inputTokens, OutputTokens: s.estimator.EstimateJSON(responseBody), Source: "estimated_tiktoken"}
	}
	success := response.StatusCode >= 200 && response.StatusCode < 300 && readErr == nil
	estimatedCost, upstreamCost, costSource := attemptCosts(candidate.Mapping, usage, responseBody, success)
	result := &attemptResult{response: response, body: responseBody, requestBody: body, usage: usage, sentTokens: sentTokens, estimatedCost: estimatedCost, upstreamCost: upstreamCost, costSource: costSource, latencyMS: latency, durationMS: elapsedMilliseconds(started, responseFinishedAt)}
	if readErr != nil {
		result.retryReason = SelectionReasonResponseError
		result.retryDetail = "response_body_read"
		result.circuitOpenUntil = s.recordChannelFailure(logCtx, candidate.Channel.ID, readErr.Error())
	} else if success {
		s.recordChannelSuccess(logCtx, candidate.Channel.ID, latency)
	} else if !shouldRetryStatus(response.StatusCode) {
		s.recordChannelResponsive(logCtx, candidate.Channel.ID)
	}
	s.recordAttempt(logCtx, execution, candidate, selection, *result, response.StatusCode, success, readErr)
	if readErr != nil {
		return result, readErr
	}
	s.addUsage(execution, usage, estimatedCost, upstreamCost, costSource, success)
	code := upstreamErrorCode(responseBody)
	execution.responseBody = responseBody
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

func (s *RelayService) streamResponse(ctx context.Context, writer http.ResponseWriter, execution *relayExecution, candidate RouteCandidate, selection RouteSelection, response *http.Response, started time.Time, latency int64, sentTokens int64, requestBody []byte) (*attemptResult, error) {
	reader := bufio.NewReader(response.Body)
	flusher, _ := writer.(http.Flusher)
	usage := Usage{}
	upstreamCost := upstreamCostSnapshot{}
	outputEstimate := int64(0)
	responseID := ""
	terminalSuccess := false
	result := attemptResult{response: response, requestBody: requestBody, sentTokens: sentTokens, latencyMS: latency}
	capture := payloadCapture{}
	pending := bytes.Buffer{}
	committed := false
	commit := func() error {
		if committed {
			return nil
		}
		copyUpstreamResponseHeaders(writer.Header(), response.Header, true)
		writer.WriteHeader(response.StatusCode)
		committed = true
		if pending.Len() > 0 {
			if _, err := writer.Write(pending.Bytes()); err != nil {
				return err
			}
			pending.Reset()
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	}

	streamErr := error(nil)
	downstreamError := false
	receivedEvent := false
	for {
		event, readErr := readSSEEvent(reader)
		if len(event) > 0 {
			receivedEvent = true
			capture.Write(event)
			consumeSSEEvent(event, s.estimator, &usage, &upstreamCost, &outputEstimate, &responseID)
			hasOutput := sseEventHasOutputToken(event)
			appErr, hasApplicationError := sseApplicationError(event)
			if sseEventIsTerminalSuccess(event) {
				terminalSuccess = true
			}

			if !committed {
				_, _ = pending.Write(event)
				if hasApplicationError && !hasOutput {
					finishedAt := time.Now()
					_ = response.Body.Close()
					execution.durationMS = elapsedMilliseconds(execution.startedAt, finishedAt)
					result.body, result.bodyTruncated = capture.Snapshot()
					result.usage = usage
					result.durationMS = elapsedMilliseconds(started, finishedAt)
					result.costSource = CostSourceFailedZero
					result.retryReason = SelectionReasonUpstreamApplicationError
					result.retryDetail = truncateRunes(appErr.Message, 512)
					logCtx := context.WithoutCancel(ctx)
					result.circuitOpenUntil = s.recordChannelFailure(logCtx, candidate.Channel.ID, appErr.Error())
					s.recordAttempt(logCtx, execution, candidate, selection, result, response.StatusCode, false, appErr)
					return &result, appErr
				}
				if hasOutput {
					recordFirstToken(event, &result, execution, started)
					if writeErr := commit(); writeErr != nil {
						streamErr = writeErr
						downstreamError = true
						break
					}
				} else if pending.Len() > maxPreTokenStreamBufferBytes {
					if writeErr := commit(); writeErr != nil {
						streamErr = writeErr
						downstreamError = true
						break
					}
				}
				if hasApplicationError {
					streamErr = appErr
					break
				}
			} else {
				recordFirstToken(event, &result, execution, started)
				if _, writeErr := writer.Write(event); writeErr != nil {
					streamErr = writeErr
					downstreamError = true
					break
				}
				if flusher != nil {
					flusher.Flush()
				}
				if hasApplicationError {
					streamErr = appErr
					break
				}
			}
			if terminalSuccess {
				break
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
	finishedAt := time.Now()
	execution.durationMS = elapsedMilliseconds(execution.startedAt, finishedAt)
	result.durationMS = elapsedMilliseconds(started, finishedAt)
	result.body, result.bodyTruncated = capture.Snapshot()
	if !terminalSuccess && streamErr == nil && receivedEvent {
		streamErr = io.ErrUnexpectedEOF
	}
	if !committed && streamErr != nil {
		if !receivedEvent {
			result.retryDetail = "stream_first_event"
		} else {
			result.retryDetail = "stream_before_first_token"
		}
		result.usage = usage
		clientCanceled := ctx.Err() != nil || errors.Is(streamErr, context.Canceled) || downstreamError
		if clientCanceled {
			result.outcome = RelayOutcomeCanceled
			if result.usage.Source == "" {
				result.usage = Usage{InputTokens: execution.inputTokens, OutputTokens: outputEstimate, Source: "estimated_tiktoken"}
			}
			result.estimatedCost = CalculateCostMicros(candidate.Mapping, result.usage)
			result.upstreamCost = result.estimatedCost
			result.costSource = CostSourceFallback
			if upstreamCost.valid {
				result.upstreamCost = upstreamCost.micros
				result.costSource = CostSourceUpstream
			}
		} else {
			result.costSource = CostSourceFailedZero
		}
		result.retryReason = SelectionReasonResponseError
		logCtx := context.WithoutCancel(ctx)
		if ctx.Err() == nil && !errors.Is(streamErr, context.Canceled) {
			result.circuitOpenUntil = s.recordChannelFailure(logCtx, candidate.Channel.ID, streamErr.Error())
		}
		s.recordAttempt(logCtx, execution, candidate, selection, result, response.StatusCode, false, streamErr)
		return &result, streamErr
	}
	if !committed {
		if !receivedEvent {
			err := io.ErrUnexpectedEOF
			result.retryReason = SelectionReasonResponseError
			result.retryDetail = "stream_first_event"
			logCtx := context.WithoutCancel(ctx)
			result.circuitOpenUntil = s.recordChannelFailure(logCtx, candidate.Channel.ID, err.Error())
			s.recordAttempt(logCtx, execution, candidate, selection, result, response.StatusCode, false, err)
			return &result, err
		}
		if writeErr := commit(); writeErr != nil {
			streamErr = writeErr
			downstreamError = true
		}
	}
	s.finishStream(ctx, execution, candidate, selection, result, usage, upstreamCost, outputEstimate, responseID, streamErr, downstreamError, terminalSuccess)
	return nil, nil
}

func (s *RelayService) finishStream(ctx context.Context, execution *relayExecution, candidate RouteCandidate, selection RouteSelection, result attemptResult, usage Usage, upstreamSnapshot upstreamCostSnapshot, outputEstimate int64, responseID string, streamErr error, downstreamError bool, terminalSuccess bool) {
	if usage.Source == "" {
		usage = Usage{InputTokens: execution.inputTokens, OutputTokens: outputEstimate, Source: "estimated_tiktoken"}
	}
	logCtx := context.WithoutCancel(ctx)
	if terminalSuccess {
		streamErr = nil
		downstreamError = false
	}
	clientCanceled := !terminalSuccess && (downstreamError || ctx.Err() != nil || errors.Is(streamErr, context.Canceled))
	success := terminalSuccess || (streamErr == nil && !clientCanceled)
	if clientCanceled {
		result.outcome = RelayOutcomeCanceled
	} else if success {
		result.outcome = RelayOutcomeSuccess
	} else {
		result.outcome = RelayOutcomeFailed
	}
	estimatedCost := int64(0)
	upstreamCost := int64(0)
	costSource := CostSourceFailedZero
	if success || clientCanceled {
		estimatedCost = CalculateCostMicros(candidate.Mapping, usage)
		upstreamCost = estimatedCost
		costSource = CostSourceFallback
		if upstreamSnapshot.valid {
			upstreamCost = upstreamSnapshot.micros
			costSource = CostSourceUpstream
		}
	}
	result.usage = usage
	result.estimatedCost = estimatedCost
	result.upstreamCost = upstreamCost
	result.costSource = costSource
	result.streamError = streamErr
	if streamErr == nil || clientCanceled {
		s.recordChannelSuccess(logCtx, candidate.Channel.ID, result.latencyMS)
	} else {
		s.recordChannelFailure(logCtx, candidate.Channel.ID, streamErr.Error())
	}
	s.recordAttempt(logCtx, execution, candidate, selection, result, result.response.StatusCode, success, streamErr)
	s.addUsage(execution, usage, estimatedCost, upstreamCost, costSource, success || clientCanceled)
	requestStatus := result.response.StatusCode
	errorCode := ""
	if clientCanceled {
		requestStatus = statusClientClosedRequest
		errorCode = "request_canceled"
	} else if streamErr != nil {
		requestStatus = upstreamApplicationErrorStatus
		errorCode = "stream_interrupted"
		var appErr *upstreamApplicationFailure
		if errors.As(streamErr, &appErr) {
			errorCode = "upstream_application_error"
		}
	}
	execution.responseBody = result.body
	execution.responseBodyTruncated = result.bodyTruncated
	s.recordRequest(logCtx, execution, requestStatus, errorCode)
	if execution.endpoint == "responses" && success {
		s.router.RecordAffinity(logCtx, responseID, candidate.Mapping.ID)
	}
	if success {
		s.router.RecordSessionAffinityAfterSuccess(logCtx, execution.token.ID, execution.modelID, execution.payload.SessionKey, execution.sessionAffinityMappingID, candidate.Mapping.ID)
	}
}

func elapsedMilliseconds(started time.Time, finished time.Time) int64 {
	elapsed := finished.Sub(started).Milliseconds()
	return max(elapsed, int64(1))
}

func recordFirstToken(event []byte, result *attemptResult, execution *relayExecution, attemptStarted time.Time) {
	if result.firstTokenMS > 0 || !sseEventHasOutputToken(event) {
		return
	}
	now := time.Now()
	result.firstTokenMS = elapsedMilliseconds(attemptStarted, now)
	if execution.firstTokenMS == 0 {
		execution.firstTokenMS = elapsedMilliseconds(execution.startedAt, now)
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

func upstreamChannelFailure(status int, body []byte) (bool, string) {
	category := ""
	switch status {
	case http.StatusUnauthorized:
		category = "upstream authentication failed"
	case http.StatusPaymentRequired:
		category = "upstream account balance unavailable"
	case http.StatusBadRequest, http.StatusForbidden:
		normalized := strings.ToLower(string(body))
		markers := []string{
			"预扣费额度失败", "余额不足", "余额额度", "需要预扣费额度", "账户余额",
			"insufficient_quota", "insufficient quota", "insufficient balance", "insufficient credit",
			"credit balance", "credits exhausted", "out of credits", "billing hard limit",
			"billing_hard_limit", "payment required", "quota exceeded", "quota_exceeded",
			"invalid api key", "invalid_api_key", "api key is invalid", "api key disabled",
			"authentication failed", "account disabled", "key disabled", "账号已停用",
		}
		for _, marker := range markers {
			if strings.Contains(normalized, marker) {
				category = "upstream account or credential unavailable"
				break
			}
		}
	}
	if category == "" {
		return false, ""
	}
	message := ""
	if appErr, failed := upstreamApplicationError(body); failed {
		message = strings.TrimSpace(appErr.Message)
	}
	if message == "" {
		message = strings.Join(strings.Fields(string(body)), " ")
	}
	if message == "" {
		return true, fmt.Sprintf("%s (HTTP %d)", category, status)
	}
	return true, fmt.Sprintf("%s (HTTP %d): %s", category, status, truncateRunes(message, 512))
}

func retrySelection(candidate RouteCandidate, result *attemptResult) RouteSelection {
	selection := RouteSelection{
		PreviousChannelID:   candidate.Channel.ID,
		PreviousChannelName: candidate.Channel.Name,
		Reason:              SelectionReasonTransportError,
		Detail:              "upstream_request",
	}
	if result == nil {
		return selection
	}
	if result.circuitOpenUntil != nil {
		selection.Reason = SelectionReasonCircuitOpened
		selection.Detail = result.circuitOpenUntil.UTC().Format(time.RFC3339)
		return selection
	}
	if result.retryReason != "" {
		selection.Reason = result.retryReason
		selection.Detail = result.retryDetail
	}
	return selection
}

func (s *RelayService) recordPreparationFailure(ctx context.Context, execution *relayExecution, candidate RouteCandidate, selection RouteSelection, detail string, requestBody []byte, cause error) (*attemptResult, error) {
	execution.durationMS = elapsedMilliseconds(execution.startedAt, time.Now())
	result := &attemptResult{
		requestBody: requestBody,
		retryReason: SelectionReasonGatewayPreparationError,
		retryDetail: detail,
	}
	s.recordAttempt(
		context.WithoutCancel(ctx),
		execution,
		candidate,
		selection,
		*result,
		0,
		false,
		fmt.Errorf("gateway preparation failed: %s", detail),
	)
	return result, cause
}

func isEventStream(header http.Header) bool {
	return strings.Contains(strings.ToLower(header.Get("Content-Type")), "text/event-stream")
}

type upstreamApplicationFailure struct {
	Message string
	Code    string
}

func (e *upstreamApplicationFailure) Error() string {
	if e.Code == "" {
		return "upstream application error: " + e.Message
	}
	return fmt.Sprintf("upstream application error (%s): %s", e.Code, e.Message)
}

func upstreamApplicationError(data []byte) (*upstreamApplicationFailure, bool) {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		message := strings.TrimSpace(string(data))
		lower := strings.ToLower(message)
		if strings.Contains(lower, "model is at capacity") || strings.Contains(lower, "try a different model") {
			return &upstreamApplicationFailure{Message: truncateRunes(message, 2000), Code: "model_at_capacity"}, true
		}
		return nil, false
	}

	typeName, _ := payload["type"].(string)
	if value, exists := payload["error"]; exists && value != nil {
		if failure := applicationFailureDetails(value); failure != nil {
			return failure, true
		}
		return &upstreamApplicationFailure{Message: "upstream application error", Code: "upstream_error"}, true
	}
	response, _ := payload["response"].(map[string]any)
	responseStatus, _ := response["status"].(string)
	if typeName == "response.failed" || strings.EqualFold(responseStatus, "failed") {
		if failure := applicationFailureDetails(response["error"]); failure != nil {
			return failure, true
		}
		return &upstreamApplicationFailure{Message: "upstream response failed", Code: "response_failed"}, true
	}
	if typeName == "error" {
		if failure := applicationFailureDetails(payload); failure != nil {
			return failure, true
		}
		return &upstreamApplicationFailure{Message: "upstream application error", Code: "upstream_error"}, true
	}
	return nil, false
}

func applicationFailureDetails(value any) *upstreamApplicationFailure {
	switch typed := value.(type) {
	case string:
		if message := strings.TrimSpace(typed); message != "" {
			return &upstreamApplicationFailure{Message: truncateRunes(message, 2000)}
		}
	case map[string]any:
		message, _ := typed["message"].(string)
		code, _ := typed["code"].(string)
		if code == "" {
			code, _ = typed["type"].(string)
		}
		message = strings.TrimSpace(message)
		if message == "" {
			if nested, exists := typed["error"]; exists {
				return applicationFailureDetails(nested)
			}
			message = code
		}
		if message != "" {
			return &upstreamApplicationFailure{Message: truncateRunes(message, 2000), Code: truncateRunes(code, 80)}
		}
	}
	return nil
}

func sseApplicationError(event []byte) (*upstreamApplicationFailure, bool) {
	for _, line := range bytes.Split(event, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		if failure, ok := upstreamApplicationError(data); ok {
			return failure, true
		}
	}
	return nil, false
}

func sseEventIsTerminalSuccess(event []byte) bool {
	for _, line := range bytes.Split(event, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if bytes.Equal(data, []byte("[DONE]")) {
			return true
		}
		var payload map[string]any
		if json.Unmarshal(data, &payload) == nil && payload["type"] == "response.completed" {
			return true
		}
	}
	return false
}

type payloadCapture struct {
	buffer    bytes.Buffer
	truncated bool
}

func (c *payloadCapture) Write(data []byte) {
	remaining := maxDetailedPayloadBytes - c.buffer.Len()
	if remaining <= 0 {
		c.truncated = c.truncated || len(data) > 0
		return
	}
	if len(data) > remaining {
		_, _ = c.buffer.Write(data[:remaining])
		c.truncated = true
		return
	}
	_, _ = c.buffer.Write(data)
}

func (c *payloadCapture) Snapshot() ([]byte, bool) {
	return bytes.Clone(c.buffer.Bytes()), c.truncated
}

func storedPayload(data []byte, alreadyTruncated bool) (string, bool) {
	truncated := alreadyTruncated || len(data) > maxDetailedPayloadBytes
	if len(data) > maxDetailedPayloadBytes {
		data = data[:maxDetailedPayloadBytes]
	}
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
		truncated = true
	}
	return string(data), truncated
}

func publicErrorBody(publicErr *PublicError) []byte {
	body, _ := json.Marshal(struct {
		Error struct {
			Message string  `json:"message"`
			Type    string  `json:"type"`
			Param   *string `json:"param"`
			Code    string  `json:"code"`
		} `json:"error"`
	}{Error: struct {
		Message string  `json:"message"`
		Type    string  `json:"type"`
		Param   *string `json:"param"`
		Code    string  `json:"code"`
	}{Message: publicErr.Message, Type: publicErr.Type, Code: publicErr.Code}})
	return body
}

func requestSessionName(body []byte) string {
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	if messages, ok := payload["messages"].([]any); ok {
		if text := latestUserMessageText(messages); text != "" {
			return truncateRunes(normalizeSessionName(text), 10)
		}
	}
	if input, exists := payload["input"]; exists {
		if text := latestResponsesInputText(input); text != "" {
			return truncateRunes(normalizeSessionName(text), 10)
		}
	}
	return ""
}

func latestUserMessageText(messages []any) string {
	for index := len(messages) - 1; index >= 0; index-- {
		item := messages[index]
		message, _ := item.(map[string]any)
		role, _ := message["role"].(string)
		if role != "user" {
			continue
		}
		if text := contentText(message["content"]); text != "" {
			return text
		}
	}
	return ""
}

func latestResponsesInputText(input any) string {
	if text, ok := input.(string); ok {
		return text
	}
	items, _ := input.([]any)
	for index := len(items) - 1; index >= 0; index-- {
		item := items[index]
		switch typed := item.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return typed
			}
		case map[string]any:
			role, hasRole := typed["role"].(string)
			if hasRole && role != "user" {
				continue
			}
			if text := contentText(typed["content"]); text != "" {
				return text
			}
			if !hasRole {
				if text := contentText(typed); text != "" {
					return text
				}
			}
		}
	}
	return ""
}

func contentText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := contentText(item); strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, " ")
	case map[string]any:
		contentType, _ := typed["type"].(string)
		if contentType != "" && contentType != "text" && contentType != "input_text" && contentType != "output_text" {
			return ""
		}
		if text, exists := typed["text"]; exists {
			return contentText(text)
		}
		if content, exists := typed["content"]; exists {
			return contentText(content)
		}
		if value, exists := typed["value"]; exists && (contentType == "text" || contentType == "input_text") {
			return contentText(value)
		}
	}
	return ""
}

func normalizeSessionName(value string) string {
	return strings.Join(strings.Fields(value), " ")
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

func sseEventHasOutputToken(event []byte) bool {
	for _, line := range bytes.Split(event, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		var payload map[string]any
		if json.Unmarshal(data, &payload) != nil {
			continue
		}
		if delta, ok := payload["delta"]; ok && generatedDeltaHasContent(delta) {
			return true
		}
		choices, _ := payload["choices"].([]any)
		for _, item := range choices {
			choice, _ := item.(map[string]any)
			if delta, ok := choice["delta"]; ok && generatedDeltaHasContent(delta) {
				return true
			}
		}
	}
	return false
}

func generatedDeltaHasContent(value any) bool {
	switch typed := value.(type) {
	case string:
		return typed != ""
	case []any:
		for _, item := range typed {
			if generatedDeltaHasContent(item) {
				return true
			}
		}
	case map[string]any:
		for key, item := range typed {
			if key == "role" || key == "index" || key == "type" {
				continue
			}
			if generatedDeltaHasContent(item) {
				return true
			}
		}
	}
	return false
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

func (s *RelayService) recordAttempt(ctx context.Context, execution *relayExecution, candidate RouteCandidate, selection RouteSelection, result attemptResult, status int, success bool, attemptErr error) {
	message := ""
	if attemptErr != nil {
		message = truncateRunes(attemptErr.Error(), 2000)
	}
	outcome := result.outcome
	if outcome == "" {
		if success {
			outcome = RelayOutcomeSuccess
		} else {
			outcome = RelayOutcomeFailed
		}
	}
	if outcome == RelayOutcomeFailed {
		result.estimatedCost = 0
		result.upstreamCost = 0
		result.costSource = CostSourceFailedZero
	}
	compactedRequestBody := compactAttemptPayload(result.requestBody, execution.rawBody, execution.requestID)
	requestBody, requestBodyTruncated := storedPayload(compactedRequestBody, len(result.requestBody) > maxDetailedPayloadBytes)
	responseBody, responseBodyTruncated := storedPayload(result.body, result.bodyTruncated)
	log := RelayAttemptLog{
		RequestID:             execution.requestID,
		ChannelID:             candidate.Channel.ID,
		ChannelName:           candidate.Channel.Name,
		ChannelBaseURL:        candidate.Channel.BaseURL,
		ChannelModelID:        candidate.Mapping.ID,
		UpstreamModel:         candidate.Mapping.UpstreamModel,
		PreviousChannelID:     selection.PreviousChannelID,
		PreviousChannelName:   truncateRunes(selection.PreviousChannelName, 120),
		SelectionReason:       truncateRunes(selection.Reason, 48),
		SelectionDetail:       truncateRunes(selection.Detail, 512),
		RequestBody:           requestBody,
		RequestBodyTruncated:  requestBodyTruncated,
		ResponseBody:          responseBody,
		ResponseBodyTruncated: responseBodyTruncated,
		StatusCode:            status,
		Outcome:               outcome,
		InputTokens:           result.usage.InputTokens,
		NormalInputTokens:     normalInputTokens(result.usage),
		OutputTokens:          result.usage.OutputTokens,
		CachedTokens:          result.usage.CachedTokens,
		CacheWriteTokens:      result.usage.CacheWriteTokens,
		SentTokens:            result.sentTokens,
		EstimatedCost:         result.estimatedCost,
		UpstreamCost:          result.upstreamCost,
		CostSource:            result.costSource,
		UsageSource:           result.usage.Source,
		FirstTokenMS:          result.firstTokenMS,
		LatencyMS:             result.latencyMS,
		DurationMS:            result.durationMS,
		Success:               success,
		ErrorMessage:          message,
	}
	_ = s.store.db.WithContext(ctx).Create(&log).Error
}

func (s *RelayService) recordRequest(ctx context.Context, execution *relayExecution, status int, errorCode string) {
	outcome := relayRequestOutcome(status, errorCode)
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
	if outcome == RelayOutcomeFailed {
		estimatedCost = 0
		upstreamCost = 0
		costSource = CostSourceFailedZero
	}
	now := time.Now().UTC()
	durationMS := execution.durationMS
	if durationMS == 0 {
		durationMS = elapsedMilliseconds(execution.startedAt, now)
	}
	responseBody, responseBodyTruncated := storedPayload(execution.responseBody, execution.responseBodyTruncated)
	sessionName := requestSessionName(execution.rawBody)
	log := RelayRequestLog{
		ID:                    execution.requestID,
		TokenID:               execution.token.ID,
		TokenName:             execution.token.Name,
		TokenKeyPrefix:        execution.token.KeyPrefix,
		Endpoint:              execution.endpoint,
		RequestedModel:        execution.payload.Model,
		CodexSessionID:        truncateRunes(execution.payload.SessionKey, 512),
		CodexSessionSource:    execution.payload.SessionSource,
		SessionName:           sessionName,
		RequestParametersJSON: execution.payload.RequestParametersJSON,
		ResponseBody:          responseBody,
		ResponseBodyTruncated: responseBodyTruncated,
		StatusCode:            status,
		Outcome:               outcome,
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
		FirstTokenMS:          execution.firstTokenMS,
		LatencyMS:             execution.latencyMS,
		DurationMS:            durationMS,
		Stream:                execution.payload.Stream,
		ErrorCode:             errorCode,
		CreatedAt:             now,
	}
	successCount := int64(0)
	if outcome == RelayOutcomeSuccess {
		successCount = 1
	}
	canceledCount := int64(0)
	if outcome == RelayOutcomeCanceled {
		canceledCount = 1
	}
	firstTokenSamples := int64(0)
	if log.FirstTokenMS > 0 {
		firstTokenSamples = 1
	}
	latencySamples := int64(0)
	if log.LatencyMS > 0 {
		latencySamples = 1
	}
	stat := TokenDailyStat{
		Date:              now.Format(time.DateOnly),
		TokenID:           execution.token.ID,
		RequestCount:      1,
		SuccessCount:      successCount,
		CanceledCount:     canceledCount,
		InputTokens:       log.InputTokens,
		NormalInputTokens: log.NormalInputTokens,
		OutputTokens:      log.OutputTokens,
		CachedTokens:      log.CachedTokens,
		CacheWriteTokens:  log.CacheWriteTokens,
		SentTokens:        log.SentTokens,
		EstimatedCost:     log.EstimatedCost,
		UpstreamCost:      log.UpstreamCost,
		FirstTokenMS:      log.FirstTokenMS,
		FirstTokenSamples: firstTokenSamples,
		LatencyMS:         log.LatencyMS,
		LatencySamples:    latencySamples,
		DurationMS:        log.DurationMS,
		AttemptCount:      int64(log.AttemptCount),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	var responseForManifest []byte
	if outcome == RelayOutcomeSuccess {
		responseForManifest = execution.responseBody
	}
	_ = s.store.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		compactedRequestBody := compactSessionPayload(
			db,
			execution.token.ID,
			execution.payload.SessionKey,
			execution.requestID,
			sessionName,
			execution.rawBody,
			responseForManifest,
			now,
		)
		log.RequestBody, log.RequestBodyTruncated = storedPayload(compactedRequestBody, len(execution.rawBody) > maxDetailedPayloadBytes)
		if err := db.Create(&log).Error; err != nil {
			return err
		}
		return db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "date"}, {Name: "token_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"request_count":       gorm.Expr("request_count + excluded.request_count"),
				"success_count":       gorm.Expr("success_count + excluded.success_count"),
				"canceled_count":      gorm.Expr("canceled_count + excluded.canceled_count"),
				"input_tokens":        gorm.Expr("input_tokens + excluded.input_tokens"),
				"normal_input_tokens": gorm.Expr("normal_input_tokens + excluded.normal_input_tokens"),
				"output_tokens":       gorm.Expr("output_tokens + excluded.output_tokens"),
				"cached_tokens":       gorm.Expr("cached_tokens + excluded.cached_tokens"),
				"cache_write_tokens":  gorm.Expr("cache_write_tokens + excluded.cache_write_tokens"),
				"sent_tokens":         gorm.Expr("sent_tokens + excluded.sent_tokens"),
				"estimated_cost":      gorm.Expr("estimated_cost + excluded.estimated_cost"),
				"upstream_cost":       gorm.Expr("upstream_cost + excluded.upstream_cost"),
				"first_token_ms":      gorm.Expr("first_token_ms + excluded.first_token_ms"),
				"first_token_samples": gorm.Expr("first_token_samples + excluded.first_token_samples"),
				"latency_ms":          gorm.Expr("latency_ms + excluded.latency_ms"),
				"latency_samples":     gorm.Expr("latency_samples + excluded.latency_samples"),
				"duration_ms":         gorm.Expr("duration_ms + excluded.duration_ms"),
				"attempt_count":       gorm.Expr("attempt_count + excluded.attempt_count"),
				"updated_at":          now,
			}),
		}).Create(&stat).Error
	})
}

func relayRequestOutcome(status int, errorCode string) string {
	if errorCode == "request_canceled" || status == statusClientClosedRequest {
		return RelayOutcomeCanceled
	}
	if status >= 200 && status < 300 && errorCode == "" {
		return RelayOutcomeSuccess
	}
	return RelayOutcomeFailed
}

func (s *RelayService) recordChannelFailure(ctx context.Context, channelID uint64, message string) *time.Time {
	return s.recordChannelFailureState(ctx, channelID, message, false)
}

func (s *RelayService) recordChannelUnavailable(ctx context.Context, channelID uint64, message string) *time.Time {
	return s.recordChannelFailureState(ctx, channelID, message, true)
}

func (s *RelayService) recordChannelFailureState(ctx context.Context, channelID uint64, message string, immediate bool) *time.Time {
	message = truncateRunes(message, 2000)
	now := time.Now()
	_ = s.store.db.WithContext(ctx).Model(&Channel{}).Where("id = ?", channelID).Updates(map[string]any{
		"consecutive_failures": gorm.Expr("consecutive_failures + 1"),
		"last_error":           message,
		"last_health_at":       now,
	}).Error
	openUntil := now.Add(60 * time.Second)
	query := s.store.db.WithContext(ctx).Model(&Channel{}).Where("id = ?", channelID)
	if !immediate {
		query = query.Where("consecutive_failures >= 3")
	}
	_ = query.Update("circuit_open_until", openUntil).Error
	var channel Channel
	if err := s.store.db.WithContext(ctx).Select("circuit_open_until").First(&channel, channelID).Error; err != nil || channel.CircuitOpenUntil == nil || !channel.CircuitOpenUntil.After(now) {
		return nil
	}
	return channel.CircuitOpenUntil
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
