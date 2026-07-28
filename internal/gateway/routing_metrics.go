package gateway

import (
	"context"
	"sort"
	"time"

	"gorm.io/gorm"
)

const (
	routingMetricWindow              = 30 * time.Minute
	routingHistorySampleSize         = 100
	routingColdStartExplorationShare = 0.20
)

type recentRoutingMetric struct {
	LatencyMS        float64
	CacheHitRate     float64
	CacheSampleCount int64
	CacheRate        float64
	CacheTokenCount  int64
	RouteCount       int64
	RouteShare       float64
	RouteSampleSize  int64
}

func loadRecentRoutingMetrics(ctx context.Context, db *gorm.DB, mappingIDs []uint64, now time.Time) (map[uint64]recentRoutingMetric, error) {
	metrics := make(map[uint64]recentRoutingMetric, len(mappingIDs))
	if len(mappingIDs) == 0 {
		return metrics, nil
	}
	for _, mappingID := range mappingIDs {
		metrics[mappingID] = recentRoutingMetric{}
	}
	type row struct {
		ChannelModelID uint64
		RouteCount     int64
		LatencyTotal   int64
		LatencySamples int64
		InputTokens    int64
		CachedTokens   int64
		CacheHits      int64
		CacheSamples   int64
	}
	var rows []row
	err := db.WithContext(ctx).Model(&RelayAttemptLog{}).
		Select("channel_model_id, COALESCE(SUM(latency_ms), 0) AS latency_total, SUM(CASE WHEN latency_ms > 0 THEN 1 ELSE 0 END) AS latency_samples, COALESCE(SUM(input_tokens), 0) AS input_tokens, COALESCE(SUM(cached_tokens), 0) AS cached_tokens, SUM(CASE WHEN input_tokens > 0 AND cached_tokens > 0 THEN 1 ELSE 0 END) AS cache_hits, SUM(CASE WHEN input_tokens > 0 THEN 1 ELSE 0 END) AS cache_samples").
		Where("channel_model_id IN ? AND created_at >= ?", mappingIDs, now.Add(-routingMetricWindow)).
		Group("channel_model_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, item := range rows {
		metric := recentRoutingMetric{}
		if item.LatencySamples > 0 {
			metric.LatencyMS = float64(item.LatencyTotal) / float64(item.LatencySamples)
		}
		if item.CacheSamples > 0 {
			metric.CacheHitRate = min(max(float64(item.CacheHits)/float64(item.CacheSamples), 0), 1)
			metric.CacheSampleCount = item.CacheSamples
		}
		if item.InputTokens > 0 {
			metric.CacheRate = min(max(float64(item.CachedTokens)/float64(item.InputTokens), 0), 1)
			metric.CacheTokenCount = item.InputTokens
		}
		metrics[item.ChannelModelID] = metric
	}
	var latest []RelayAttemptLog
	if err := db.WithContext(ctx).Order("created_at DESC, id DESC").Limit(routingHistorySampleSize).Find(&latest).Error; err != nil {
		return nil, err
	}
	mappingSet := make(map[uint64]struct{}, len(mappingIDs))
	for _, mappingID := range mappingIDs {
		mappingSet[mappingID] = struct{}{}
	}
	for _, attempt := range latest {
		if _, exists := mappingSet[attempt.ChannelModelID]; !exists {
			continue
		}
		metric := metrics[attempt.ChannelModelID]
		metric.RouteCount++
		metrics[attempt.ChannelModelID] = metric
	}
	for mappingID, metric := range metrics {
		metric.RouteSampleSize = int64(len(latest))
		if len(latest) > 0 {
			metric.RouteShare = float64(metric.RouteCount) / float64(len(latest))
		}
		metrics[mappingID] = metric
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
		costAdvantage := (float64(max(minCost, 0)) + 1) / (float64(max(candidate.Cost, 0)) + 1)
		costFactor := 0.95 + 0.05*min(max(costAdvantage, 0), 1)
		latencyFactor := 1.0
		if strategy == RoutingLowestLatency {
			latencyFactor = 0.7 + 0.3*min(bestLatency/latency, 1)
		}
		successFactor := 0.75 + 0.5*float64(candidateSuccessBasisPoints(candidate))/float64(routeProbabilityScale)
		cacheHitFactor := 1.0
		if candidate.RecentCacheSamples > 0 {
			cacheHitFactor = 0.5 + 1.5*min(max(candidate.RecentCacheHitRate, 0), 1)
		}
		cacheRateFactor := 1.0
		if candidate.RecentCacheTokens > 0 {
			cacheRateFactor = 0.8 + 0.4*min(max(candidate.RecentCacheRate, 0), 1)
		}
		recentRouteFactor := 1 / (1 + 4*min(max(candidate.RecentRouteShare, 0), 1))
		weightFactor := float64(max(candidate.Mapping.Weight, 1)) / 100
		if strategy == RoutingLowestCost {
			costFactor = 0.9 + 0.1*min(max(costAdvantage, 0), 1)
		}
		expectation := max(weightFactor*recentRouteFactor*cacheHitFactor*successFactor*cacheRateFactor*latencyFactor*costFactor, 0.000001)
		if strategy == RoutingPriorityWeighted && candidate.Mapping.Priority < maxPriority {
			expectation = 0
		}
		total += expectation
		decision.Candidates[index] = RouteDecisionCandidate{
			ChannelID: candidate.Channel.ID, ChannelName: candidate.Channel.Name, ChannelModelID: candidate.Mapping.ID, UpstreamModel: candidate.Mapping.UpstreamModel,
			Priority: candidate.Mapping.Priority, Weight: candidate.Mapping.Weight, ExpectedCostMicros: candidate.Cost, SuccessRate: float64(candidateSuccessBasisPoints(candidate)) / float64(routeProbabilityScale),
			LatencyMS: observedLatency, CacheHitRate: candidate.RecentCacheHitRate, CacheSampleCount: candidate.RecentCacheSamples, CacheRate: candidate.RecentCacheRate, CacheTokenCount: candidate.RecentCacheTokens, RecentRouteCount: candidate.RecentRouteCount,
			RecentRouteShare: candidate.RecentRouteShare, RouteSampleSize: candidate.RouteSampleSize, Expectation: expectation,
		}
	}
	applyColdStartExploration(strategy, candidates, decision, maxPriority, total)
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

func applyColdStartExploration(strategy string, candidates []RouteCandidate, decision *RouteDecision, maxPriority int, total float64) {
	if total <= 0 || len(candidates) == 0 {
		return
	}
	coldStartWeight := int64(0)
	for _, candidate := range candidates {
		if coldStartExplorationEligible(strategy, candidate, maxPriority) {
			coldStartWeight += int64(max(candidate.Mapping.Weight, 1))
		}
	}
	if coldStartWeight == 0 {
		return
	}

	// Reserve bounded traffic for candidates that cannot build quality metrics yet.
	// Configured weights still control how that exploration traffic is shared.
	for index, candidate := range candidates {
		probability := (1 - routingColdStartExplorationShare) * decision.Candidates[index].Expectation / total
		if coldStartExplorationEligible(strategy, candidate, maxPriority) {
			probability += routingColdStartExplorationShare * float64(max(candidate.Mapping.Weight, 1)) / float64(coldStartWeight)
		}
		decision.Candidates[index].Expectation = probability * total
	}
}

func coldStartExplorationEligible(strategy string, candidate RouteCandidate, maxPriority int) bool {
	if strategy == RoutingPriorityWeighted && candidate.Mapping.Priority < maxPriority {
		return false
	}
	return candidate.RecentAttemptCount == 0 && candidate.RecentCacheSamples == 0 && candidate.RecentCacheTokens == 0
}

func deterministicRouteDecision(strategy string, mode string, candidates []RouteCandidate) *RouteDecision {
	decision := &RouteDecision{Strategy: strategy, Mode: mode, Candidates: make([]RouteDecisionCandidate, 0, len(candidates))}
	for index, candidate := range candidates {
		decision.Candidates = append(decision.Candidates, RouteDecisionCandidate{ChannelID: candidate.Channel.ID, ChannelName: candidate.Channel.Name, ChannelModelID: candidate.Mapping.ID, UpstreamModel: candidate.Mapping.UpstreamModel, Priority: candidate.Mapping.Priority, Weight: candidate.Mapping.Weight, ExpectedCostMicros: candidate.Cost, SuccessRate: candidate.RecentSuccessRate, LatencyMS: candidate.RecentLatencyMS, CacheHitRate: candidate.RecentCacheHitRate, CacheSampleCount: candidate.RecentCacheSamples, CacheRate: candidate.RecentCacheRate, CacheTokenCount: candidate.RecentCacheTokens, RecentRouteCount: candidate.RecentRouteCount, RecentRouteShare: candidate.RecentRouteShare, RouteSampleSize: candidate.RouteSampleSize, Expectation: boolScore(index == 0), Probability: boolScore(index == 0), Selected: index == 0})
	}
	return decision
}

func boolScore(value bool) float64 {
	if value {
		return 1
	}
	return 0
}
