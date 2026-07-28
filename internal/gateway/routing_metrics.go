package gateway

import (
	"context"
	"math"
	"sort"
	"time"

	"gorm.io/gorm"
)

const routingMetricWindow = 30 * time.Minute

type recentRoutingMetric struct {
	LatencyMS         float64
	CacheRate         float64
	RouteCount        int64
	ConsecutiveRoutes int64
}

func loadRecentRoutingMetrics(ctx context.Context, db *gorm.DB, mappingIDs []uint64, now time.Time) (map[uint64]recentRoutingMetric, error) {
	metrics := make(map[uint64]recentRoutingMetric, len(mappingIDs))
	if len(mappingIDs) == 0 {
		return metrics, nil
	}
	type row struct {
		ChannelModelID uint64
		RouteCount     int64
		LatencyTotal   int64
		LatencySamples int64
		InputTokens    int64
		CachedTokens   int64
	}
	var rows []row
	err := db.WithContext(ctx).Model(&RelayAttemptLog{}).
		Select("channel_model_id, COUNT(*) AS route_count, COALESCE(SUM(latency_ms), 0) AS latency_total, SUM(CASE WHEN latency_ms > 0 THEN 1 ELSE 0 END) AS latency_samples, COALESCE(SUM(input_tokens), 0) AS input_tokens, COALESCE(SUM(cached_tokens), 0) AS cached_tokens").
		Where("channel_model_id IN ? AND created_at >= ?", mappingIDs, now.Add(-routingMetricWindow)).
		Group("channel_model_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, item := range rows {
		metric := recentRoutingMetric{RouteCount: item.RouteCount}
		if item.LatencySamples > 0 {
			metric.LatencyMS = float64(item.LatencyTotal) / float64(item.LatencySamples)
		}
		if item.InputTokens > 0 {
			metric.CacheRate = min(max(float64(item.CachedTokens)/float64(item.InputTokens), 0), 1)
		}
		metrics[item.ChannelModelID] = metric
	}
	var latest []RelayAttemptLog
	if err := db.WithContext(ctx).Where("channel_model_id IN ? AND created_at >= ?", mappingIDs, now.Add(-routingMetricWindow)).Order("created_at DESC, id DESC").Limit(100).Find(&latest).Error; err != nil {
		return nil, err
	}
	if len(latest) > 0 {
		selected := latest[0].ChannelModelID
		metric := metrics[selected]
		for _, attempt := range latest {
			if attempt.ChannelModelID != selected {
				break
			}
			metric.ConsecutiveRoutes++
		}
		metrics[selected] = metric
	}
	return metrics, nil
}

func (r *Router) expectationProbabilityOrder(strategy string, candidates []RouteCandidate) *RouteDecision {
	switch strategy {
	case RoutingLowestCost:
		sortCandidatesByCost(candidates)
	case RoutingLowestLatency:
		sortCandidatesByLatency(candidates)
	default:
		sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Mapping.Priority > candidates[j].Mapping.Priority })
	}
	decision := &RouteDecision{Strategy: strategy, Mode: "probability", Candidates: make([]RouteDecisionCandidate, len(candidates))}
	if len(candidates) == 0 {
		return decision
	}
	minCost := candidates[0].Cost
	bestLatency := float64(0)
	maxPriority := candidates[0].Mapping.Priority
	for _, candidate := range candidates {
		minCost = min(minCost, candidate.Cost)
		latency := candidate.RecentLatencyMS
		if latency <= 0 {
			latency = candidate.Channel.LatencyEWMA
		}
		if latency > 0 && (bestLatency == 0 || latency < bestLatency) {
			bestLatency = latency
		}
		maxPriority = max(maxPriority, candidate.Mapping.Priority)
	}
	if bestLatency <= 0 {
		bestLatency = 1
	}
	total := float64(0)
	for index, candidate := range candidates {
		observedLatency := candidate.RecentLatencyMS
		if observedLatency <= 0 {
			observedLatency = candidate.Channel.LatencyEWMA
		}
		latency := observedLatency
		if latency <= 0 {
			latency = bestLatency
		}
		costFactor := (float64(max(minCost, 0)) + 1) / (float64(max(candidate.Cost, 0)) + 1)
		latencyFactor := min(bestLatency/latency, 1)
		successFactor := 0.75 + 0.25*float64(candidateSuccessBasisPoints(candidate))/float64(routeProbabilityScale)
		cacheFactor := 0.9 + 0.2*min(max(candidate.RecentCacheRate, 0), 1)
		loadFactor := 1 / math.Sqrt(1+float64(candidate.RecentRouteCount)/12)
		streakFactor := 1 / (1 + 0.12*float64(candidate.ConsecutiveRoutes))
		weightFactor := float64(max(candidate.Mapping.Weight, 1)) / 100
		if strategy == RoutingLowestCost {
			costFactor *= costFactor
		}
		if strategy == RoutingLowestLatency {
			latencyFactor *= latencyFactor
		}
		expectation := max(weightFactor*costFactor*latencyFactor*successFactor*cacheFactor*loadFactor*streakFactor, 0.000001)
		if strategy == RoutingPriorityWeighted && candidate.Mapping.Priority < maxPriority {
			expectation = 0
		}
		total += expectation
		decision.Candidates[index] = RouteDecisionCandidate{
			ChannelID: candidate.Channel.ID, ChannelName: candidate.Channel.Name, ChannelModelID: candidate.Mapping.ID, UpstreamModel: candidate.Mapping.UpstreamModel,
			Priority: candidate.Mapping.Priority, Weight: candidate.Mapping.Weight, ExpectedCostMicros: candidate.Cost, SuccessRate: float64(candidateSuccessBasisPoints(candidate)) / float64(routeProbabilityScale),
			LatencyMS: observedLatency, CacheHitRate: candidate.RecentCacheRate, RecentRouteCount: candidate.RecentRouteCount, ConsecutiveRoutes: candidate.ConsecutiveRoutes, Expectation: expectation,
		}
	}
	point := float64(0)
	if total > 0 && r.random != nil {
		point = float64(r.random(1_000_000)) / 1_000_000 * total
	}
	cumulative := float64(0)
	selectedIndex := 0
	for index := range decision.Candidates {
		decision.Candidates[index].Probability = decision.Candidates[index].Expectation / total
	}
	for index := range decision.Candidates {
		cumulative += decision.Candidates[index].Expectation
		if point < cumulative {
			selectedIndex = index
			break
		}
	}
	decision.Candidates[selectedIndex].Selected = true
	selected := candidates[selectedIndex]
	copy(candidates[1:selectedIndex+1], candidates[:selectedIndex])
	candidates[0] = selected
	return decision
}

func deterministicRouteDecision(strategy string, mode string, candidates []RouteCandidate) *RouteDecision {
	decision := &RouteDecision{Strategy: strategy, Mode: mode, Candidates: make([]RouteDecisionCandidate, 0, len(candidates))}
	for index, candidate := range candidates {
		decision.Candidates = append(decision.Candidates, RouteDecisionCandidate{ChannelID: candidate.Channel.ID, ChannelName: candidate.Channel.Name, ChannelModelID: candidate.Mapping.ID, UpstreamModel: candidate.Mapping.UpstreamModel, Priority: candidate.Mapping.Priority, Weight: candidate.Mapping.Weight, ExpectedCostMicros: candidate.Cost, SuccessRate: candidate.RecentSuccessRate, LatencyMS: candidate.RecentLatencyMS, CacheHitRate: candidate.RecentCacheRate, RecentRouteCount: candidate.RecentRouteCount, ConsecutiveRoutes: candidate.ConsecutiveRoutes, Expectation: boolScore(index == 0), Probability: boolScore(index == 0), Selected: index == 0})
	}
	return decision
}

func boolScore(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
