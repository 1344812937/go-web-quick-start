package gateway

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrModelNotFound       = errors.New("requested model does not exist or is disabled")
	ErrNoAvailableChannel  = errors.New("no channel is currently available for this model")
	ErrAffinityUnavailable = errors.New("the channel for previous_response_id is unavailable")
)

type RouteCandidate struct {
	Channel Channel
	Mapping ChannelModel
	Cost    int64
}

type RoutePlan struct {
	Model                    GatewayModel
	Candidates               []RouteCandidate
	InitialSelection         RouteSelection
	Affinity                 bool
	SessionAffinity          bool
	SessionAffinityMappingID uint64
}

type RouteSelection struct {
	PreviousChannelID   uint64
	PreviousChannelName string
	Reason              string
	Detail              string
}

type Router struct {
	store  *Store
	access *ClientAccessService
	random func(int) int
}

func NewRouter(store *Store, access *ClientAccessService) *Router {
	return &Router{store: store, access: access, random: secureIntn}
}

func secureIntn(limit int) int {
	if limit <= 1 {
		return 0
	}
	var raw [8]byte
	if _, err := cryptorand.Read(raw[:]); err != nil {
		return int(time.Now().UnixNano() % int64(limit))
	}
	return int(binary.LittleEndian.Uint64(raw[:]) % uint64(limit))
}

func (r *Router) Plan(ctx context.Context, token *ClientToken, modelName string, inputTokens int64, declaredOutput int64, previousResponseID string, sessionKey string) (*RoutePlan, error) {
	var model GatewayModel
	if err := r.store.db.WithContext(ctx).Where("name = ? AND enabled = ?", modelName, true).First(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrModelNotFound
		}
		return nil, err
	}
	if err := r.access.AuthorizeModel(ctx, token, model.ID); err != nil {
		return nil, err
	}
	outputTokens := declaredOutput
	if outputTokens <= 0 {
		outputTokens = r.recentOutputMedian(ctx, model.Name)
	}

	if previousResponseID != "" {
		candidate, err := r.affinityCandidate(ctx, model.ID, previousResponseID, inputTokens, outputTokens)
		if err != nil {
			return nil, err
		}
		return &RoutePlan{
			Model:            model,
			Candidates:       []RouteCandidate{*candidate},
			InitialSelection: RouteSelection{Reason: SelectionReasonResponseAffinity},
			Affinity:         true,
		}, nil
	}

	candidates, err := r.availableCandidates(ctx, model.ID, inputTokens, outputTokens)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, ErrNoAvailableChannel
	}
	r.orderCandidates(model.RoutingStrategy, candidates)
	initialSelection := RouteSelection{Reason: SelectionReasonInitialRoute}
	if sessionKey != "" {
		affinity, affinityErr := r.sessionAffinity(ctx, token.ID, model.ID, sessionKey)
		if affinityErr != nil {
			return nil, affinityErr
		}
		if affinity != nil {
			if pinCandidate(candidates, affinity.ChannelModelID) {
				initialSelection = RouteSelection{Reason: SelectionReasonSessionAffinity}
			} else {
				initialSelection = r.unavailableSessionSelection(ctx, token.ID, model, sessionKey, affinity.ChannelModelID)
			}
			return &RoutePlan{
				Model:                    model,
				Candidates:               candidates,
				InitialSelection:         initialSelection,
				SessionAffinity:          true,
				SessionAffinityMappingID: affinity.ChannelModelID,
			}, nil
		}
	}
	return &RoutePlan{Model: model, Candidates: candidates, InitialSelection: initialSelection}, nil
}

func (r *Router) unavailableSessionSelection(ctx context.Context, tokenID uint64, model GatewayModel, sessionKey string, channelModelID uint64) RouteSelection {
	selection := RouteSelection{Reason: SelectionReasonAffinityTargetMissing}
	var mapping ChannelModel
	if err := r.store.db.WithContext(ctx).First(&mapping, channelModelID).Error; err == nil && mapping.ModelID == model.ID {
		selection.PreviousChannelID = mapping.ChannelID
		var channel Channel
		if err := r.store.db.WithContext(ctx).First(&channel, mapping.ChannelID).Error; err == nil {
			selection.PreviousChannelName = channel.Name
			switch {
			case !channel.Enabled:
				selection.Reason = SelectionReasonChannelDisabled
			case !mapping.Enabled:
				selection.Reason = SelectionReasonMappingDisabled
			case channel.CircuitOpenUntil != nil && channel.CircuitOpenUntil.After(time.Now()):
				selection.Reason = SelectionReasonCircuitOpen
				selection.Detail = channel.CircuitOpenUntil.UTC().Format(time.RFC3339)
			}
		}
	}
	if selection.PreviousChannelName == "" {
		var attempt RelayAttemptLog
		err := r.store.db.WithContext(ctx).Table("relay_attempt_logs AS a").
			Select("a.*").
			Joins("JOIN relay_request_logs AS request ON request.id = a.request_id").
			Where("request.token_id = ? AND request.requested_model = ? AND request.codex_session_id = ? AND a.channel_model_id = ?", tokenID, model.Name, sessionKey, channelModelID).
			Order("a.created_at DESC, a.id DESC").First(&attempt).Error
		if err == nil {
			selection.PreviousChannelID = attempt.ChannelID
			selection.PreviousChannelName = attempt.ChannelName
		}
	}
	return selection
}

func (r *Router) sessionAffinity(ctx context.Context, tokenID uint64, modelID uint64, sessionKey string) (*SessionAffinity, error) {
	var affinity SessionAffinity
	err := r.store.db.WithContext(ctx).Where(
		"token_id = ? AND model_id = ? AND session_hash = ? AND expires_at > ?",
		tokenID, modelID, hashSecret(sessionKey), time.Now(),
	).First(&affinity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &affinity, nil
}

func pinCandidate(candidates []RouteCandidate, channelModelID uint64) bool {
	for index := range candidates {
		if candidates[index].Mapping.ID != channelModelID {
			continue
		}
		if index > 0 {
			candidate := candidates[index]
			copy(candidates[1:index+1], candidates[:index])
			candidates[0] = candidate
		}
		return true
	}
	return false
}

func (r *Router) availableCandidates(ctx context.Context, modelID uint64, inputTokens int64, outputTokens int64) ([]RouteCandidate, error) {
	var mappings []ChannelModel
	if err := r.store.db.WithContext(ctx).Where("model_id = ? AND enabled = ?", modelID, true).Find(&mappings).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	candidates := make([]RouteCandidate, 0, len(mappings))
	for _, mapping := range mappings {
		var channel Channel
		if err := r.store.db.WithContext(ctx).First(&channel, mapping.ChannelID).Error; err != nil {
			continue
		}
		if !channel.Enabled || (channel.CircuitOpenUntil != nil && channel.CircuitOpenUntil.After(now)) {
			continue
		}
		usage := Usage{InputTokens: inputTokens, OutputTokens: outputTokens}
		candidates = append(candidates, RouteCandidate{Channel: channel, Mapping: mapping, Cost: CalculateCostMicros(mapping, usage)})
	}
	return candidates, nil
}

func (r *Router) affinityCandidate(ctx context.Context, modelID uint64, previousResponseID string, inputTokens int64, outputTokens int64) (*RouteCandidate, error) {
	var affinity ResponseAffinity
	err := r.store.db.WithContext(ctx).Where("response_hash = ? AND expires_at > ?", hashSecret(previousResponseID), time.Now()).First(&affinity).Error
	if err != nil {
		return nil, ErrAffinityUnavailable
	}
	var mapping ChannelModel
	if err := r.store.db.WithContext(ctx).Where("id = ? AND model_id = ? AND enabled = ?", affinity.ChannelModelID, modelID, true).First(&mapping).Error; err != nil {
		return nil, ErrAffinityUnavailable
	}
	var channel Channel
	if err := r.store.db.WithContext(ctx).First(&channel, mapping.ChannelID).Error; err != nil {
		return nil, ErrAffinityUnavailable
	}
	if !channel.Enabled || (channel.CircuitOpenUntil != nil && channel.CircuitOpenUntil.After(time.Now())) {
		return nil, ErrAffinityUnavailable
	}
	usage := Usage{InputTokens: inputTokens, OutputTokens: outputTokens}
	return &RouteCandidate{Channel: channel, Mapping: mapping, Cost: CalculateCostMicros(mapping, usage)}, nil
}

func (r *Router) recentOutputMedian(ctx context.Context, modelName string) int64 {
	var values []int64
	_ = r.store.db.WithContext(ctx).Model(&RelayRequestLog{}).
		Where("requested_model = ? AND output_tokens > 0 AND status_code BETWEEN 200 AND 299", modelName).
		Order("created_at desc").Limit(31).Pluck("output_tokens", &values).Error
	if len(values) == 0 {
		return 1024
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values[len(values)/2]
}

func (r *Router) orderCandidates(strategy string, candidates []RouteCandidate) {
	switch strategy {
	case RoutingLowestCost:
		sort.SliceStable(candidates, func(i, j int) bool {
			if candidates[i].Cost == candidates[j].Cost {
				return candidates[i].Channel.LatencyEWMA < candidates[j].Channel.LatencyEWMA
			}
			return candidates[i].Cost < candidates[j].Cost
		})
	case RoutingLowestLatency:
		sort.SliceStable(candidates, func(i, j int) bool {
			iUnknown := candidates[i].Channel.LatencyEWMA <= 0
			jUnknown := candidates[j].Channel.LatencyEWMA <= 0
			if iUnknown != jUnknown {
				return iUnknown
			}
			return candidates[i].Channel.LatencyEWMA < candidates[j].Channel.LatencyEWMA
		})
	default:
		r.weightedPriorityOrder(candidates)
	}
}

func (r *Router) weightedPriorityOrder(candidates []RouteCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Mapping.Priority > candidates[j].Mapping.Priority })
	ordered := make([]RouteCandidate, 0, len(candidates))
	for start := 0; start < len(candidates); {
		end := start + 1
		for end < len(candidates) && candidates[end].Mapping.Priority == candidates[start].Mapping.Priority {
			end++
		}
		group := append([]RouteCandidate(nil), candidates[start:end]...)
		for len(group) > 0 {
			total := 0
			for _, candidate := range group {
				total += max(candidate.Mapping.Weight, 1)
			}
			pick := r.random(total)
			selected := 0
			for index, candidate := range group {
				pick -= max(candidate.Mapping.Weight, 1)
				if pick < 0 {
					selected = index
					break
				}
			}
			ordered = append(ordered, group[selected])
			group = append(group[:selected], group[selected+1:]...)
		}
		start = end
	}
	copy(candidates, ordered)
}

func (r *Router) RecordAffinity(ctx context.Context, responseID string, channelModelID uint64) {
	if responseID == "" {
		return
	}
	record := ResponseAffinity{
		ResponseHash:   hashSecret(responseID),
		ChannelModelID: channelModelID,
		ExpiresAt:      time.Now().Add(30 * 24 * time.Hour),
	}
	_ = r.store.db.WithContext(ctx).Save(&record).Error
}

func (r *Router) RecordSessionAffinity(ctx context.Context, tokenID uint64, modelID uint64, sessionKey string, channelModelID uint64) {
	if tokenID == 0 || modelID == 0 || sessionKey == "" || channelModelID == 0 {
		return
	}
	now := time.Now()
	record := SessionAffinity{
		TokenID:        tokenID,
		ModelID:        modelID,
		SessionHash:    hashSecret(sessionKey),
		ChannelModelID: channelModelID,
		ExpiresAt:      now.Add(30 * 24 * time.Hour),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	_ = r.store.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "token_id"}, {Name: "model_id"}, {Name: "session_hash"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"channel_model_id", "expires_at", "updated_at",
		}),
	}).Create(&record).Error
}

func (r *Router) RecordSessionAffinityAfterSuccess(ctx context.Context, tokenID uint64, modelID uint64, sessionKey string, previousChannelModelID uint64, successfulChannelModelID uint64) {
	if previousChannelModelID != 0 && previousChannelModelID != successfulChannelModelID && r.sessionMappingAvailable(ctx, modelID, previousChannelModelID) {
		return
	}
	r.RecordSessionAffinity(ctx, tokenID, modelID, sessionKey, successfulChannelModelID)
}

func (r *Router) sessionMappingAvailable(ctx context.Context, modelID uint64, channelModelID uint64) bool {
	var mapping ChannelModel
	err := r.store.db.WithContext(ctx).Where("id = ? AND model_id = ? AND enabled = ?", channelModelID, modelID, true).First(&mapping).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false
	}
	if err != nil {
		return true
	}
	var channel Channel
	if err := r.store.db.WithContext(ctx).First(&channel, mapping.ChannelID).Error; err != nil {
		return !errors.Is(err, gorm.ErrRecordNotFound)
	}
	return channel.Enabled && (channel.CircuitOpenUntil == nil || !channel.CircuitOpenUntil.After(time.Now()))
}
