package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1344812937/go-web-quick-start/internal/config"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type disconnectedStreamWriter struct {
	header http.Header
	status int
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

type errorReadCloser struct{}

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func (errorReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("response body unavailable")
}

func (errorReadCloser) Close() error {
	return nil
}

func (w *disconnectedStreamWriter) Header() http.Header {
	return w.header
}

func (w *disconnectedStreamWriter) WriteHeader(status int) {
	w.status = status
}

func (w *disconnectedStreamWriter) Write([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}

func (w *disconnectedStreamWriter) Flush() {}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(
		&AdminUser{}, &AdminSession{}, &Channel{}, &GatewayModel{}, &ChannelModel{},
		&ClientToken{}, &ClientTokenModel{}, &RelayRequestLog{}, &RelayAttemptLog{}, &TokenDailyStat{}, &GatewayMigration{},
		&ResponseAffinity{}, &SessionAffinity{},
	); err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	box, err := NewSecretBox(key)
	if err != nil {
		t.Fatal(err)
	}
	return &Store{db: db, secretBox: box, configManager: &config.ApplicationConfigManager{}}
}

func createRouteFixture(t *testing.T, store *Store, strategy string, channelURLs ...string) (*ClientToken, GatewayModel, []Channel, []ChannelModel) {
	t.Helper()
	model := GatewayModel{Name: "public-model", RoutingStrategy: strategy, Enabled: true}
	if err := store.db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	token := ClientToken{Name: "test", KeyHash: hashSecret("sk-test"), KeyPrefix: "sk-test", Enabled: true, AllowAllModels: true, RPM: 60, MaxConcurrency: 10}
	if err := store.db.Create(&token).Error; err != nil {
		t.Fatal(err)
	}
	secret, err := store.secretBox.Encrypt("upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	channels := make([]Channel, 0, len(channelURLs))
	mappings := make([]ChannelModel, 0, len(channelURLs))
	for index, channelURL := range channelURLs {
		channel := Channel{Name: fmt.Sprintf("channel-%d", index+1), BaseURL: channelURL + "/v1", APIKeyCipher: secret, Enabled: true, SupportsStreamUsage: true}
		if err := store.db.Create(&channel).Error; err != nil {
			t.Fatal(err)
		}
		mapping := ChannelModel{
			ChannelID: channel.ID, ModelID: model.ID, UpstreamModel: fmt.Sprintf("upstream-%d", index+1),
			Priority: 100 - index, Weight: 100, InputPriceMicros: 1_000_000, OutputPriceMicros: 1_000_000, Enabled: true,
		}
		if err := store.db.Create(&mapping).Error; err != nil {
			t.Fatal(err)
		}
		channels = append(channels, channel)
		mappings = append(mappings, mapping)
	}
	return &token, model, channels, mappings
}

func newTestRelay(store *Store) *RelayService {
	access := NewClientAccessService(store)
	router := NewRouter(store, access)
	return NewRelayService(store, router, NewTokenEstimator(), &config.ApplicationConfigManager{})
}

func TestSecretBoxAndHashing(t *testing.T) {
	store := newTestStore(t)
	ciphertext, err := store.secretBox.Encrypt("sk-upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	if ciphertext == "sk-upstream-secret" || strings.Contains(ciphertext, "upstream") {
		t.Fatal("ciphertext contains plaintext")
	}
	plaintext, err := store.secretBox.Decrypt(ciphertext)
	if err != nil || plaintext != "sk-upstream-secret" {
		t.Fatalf("Decrypt() = %q, %v", plaintext, err)
	}
	if hashSecret("sk-client") == "sk-client" || hashSecret("sk-client") != hashSecret("sk-client") {
		t.Fatal("secret hash is not deterministic and one-way")
	}
}

func TestBootstrapAdminFromEnvironment(t *testing.T) {
	store := newTestStore(t)
	t.Setenv("GATEWAY_ADMIN_USERNAME", "bootstrap-admin")
	t.Setenv("GATEWAY_ADMIN_PASSWORD", "bootstrap-password")
	if err := store.bootstrapAdmin(); err != nil {
		t.Fatal(err)
	}
	var user AdminUser
	if err := store.db.Where("username = ?", "bootstrap-admin").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("bootstrap-password")) != nil {
		t.Fatal("bootstrap password was not stored with bcrypt")
	}
}

func TestCalculateCostMicrosSeparatesCacheReadAndWrite(t *testing.T) {
	cacheReadPrice := int64(500_000)
	cacheWritePrice := int64(3_000_000)
	mapping := ChannelModel{
		InputPriceMicros:       2_000_000,
		OutputPriceMicros:      8_000_000,
		CachedInputPriceMicros: &cacheReadPrice,
		CacheWritePriceMicros:  &cacheWritePrice,
	}
	usage := Usage{InputTokens: 1000, CachedTokens: 200, CacheWriteTokens: 100, OutputTokens: 500}
	if got, want := CalculateCostMicros(mapping, usage), int64(5800); got != want {
		t.Fatalf("CalculateCostMicros() = %d, want %d", got, want)
	}
	if got, want := normalInputTokens(usage), int64(700); got != want {
		t.Fatalf("normalInputTokens() = %d, want %d", got, want)
	}
	if got := normalInputTokens(Usage{InputTokens: 10, CachedTokens: 8, CacheWriteTokens: 7}); got != 0 {
		t.Fatalf("normalInputTokens() should clamp invalid cache details, got %d", got)
	}
	mapping.CachedInputPriceMicros = nil
	mapping.CacheWritePriceMicros = nil
	if got, want := CalculateCostMicros(mapping, usage), int64(6000); got != want {
		t.Fatalf("CalculateCostMicros() fallback = %d, want %d", got, want)
	}
}

func TestParseUsageReadsCacheReadAndWriteTokens(t *testing.T) {
	openAIUsage, ok := ParseUsage([]byte(`{"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":2,"cache_creation_tokens":3}}}`))
	if !ok || openAIUsage.InputTokens != 10 || openAIUsage.OutputTokens != 5 || openAIUsage.CachedTokens != 2 || openAIUsage.CacheWriteTokens != 3 {
		t.Fatalf("OpenAI usage = %+v, parsed = %v", openAIUsage, ok)
	}
	anthropicUsage, ok := ParseUsage([]byte(`{"usage":{"input_tokens":5,"output_tokens":2,"cache_read_input_tokens":7,"cache_creation_input_tokens":3}}`))
	if !ok || anthropicUsage.InputTokens != 15 || anthropicUsage.OutputTokens != 2 || anthropicUsage.CachedTokens != 7 || anthropicUsage.CacheWriteTokens != 3 {
		t.Fatalf("Anthropic usage = %+v, parsed = %v", anthropicUsage, ok)
	}
}

func TestParseUpstreamCostMicrosUsesWhitelistedPriority(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int64
		ok   bool
	}{
		{name: "usage cost", body: `{"usage":{"cost":0.0012345,"total_cost":0.9},"cost":0.8}`, want: 1235, ok: true},
		{name: "usage total cost", body: `{"usage":{"total_cost":"0.000002"}}`, want: 2, ok: true},
		{name: "top level cost", body: `{"cost":"0"}`, want: 0, ok: true},
		{name: "invalid falls back", body: `{"usage":{"cost":"not-a-number","total_cost":"0.4"}}`, want: 400000, ok: true},
		{name: "absent", body: `{"usage":{"input_tokens":1}}`, want: 0, ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ParseUpstreamCostMicros([]byte(test.body))
			if got != test.want || ok != test.ok {
				t.Fatalf("ParseUpstreamCostMicros() = %d, %v; want %d, %v", got, ok, test.want, test.ok)
			}
		})
	}
}

func TestOpenAIOfficialPriceExactMatch(t *testing.T) {
	tests := []struct {
		modelID                                string
		input, cachedInput, cacheWrite, output int64
	}{
		{modelID: "gpt-5.6-sol", input: 5_000_000, cachedInput: 500_000, cacheWrite: 6_250_000, output: 30_000_000},
		{modelID: "gpt-5.6-terra", input: 2_500_000, cachedInput: 250_000, cacheWrite: 3_125_000, output: 15_000_000},
		{modelID: "gpt-5.6-luna", input: 1_000_000, cachedInput: 100_000, cacheWrite: 1_250_000, output: 6_000_000},
		{modelID: "gpt-5.5", input: 5_000_000, cachedInput: 500_000, output: 30_000_000},
		{modelID: "gpt-5.5-pro", input: 30_000_000, output: 180_000_000},
		{modelID: "gpt-5.4", input: 2_500_000, cachedInput: 250_000, output: 15_000_000},
		{modelID: "gpt-5.4-mini", input: 750_000, cachedInput: 75_000, output: 4_500_000},
		{modelID: "gpt-5.4-nano", input: 200_000, cachedInput: 20_000, output: 1_250_000},
		{modelID: "gpt-5.4-pro", input: 30_000_000, output: 180_000_000},
		{modelID: "gpt-5.2", input: 1_750_000, cachedInput: 175_000, output: 14_000_000},
		{modelID: "gpt-5.2-pro", input: 21_000_000, output: 168_000_000},
		{modelID: "gpt-5.1", input: 1_250_000, cachedInput: 125_000, output: 10_000_000},
		{modelID: "gpt-5", input: 1_250_000, cachedInput: 125_000, output: 10_000_000},
		{modelID: "gpt-5-mini", input: 250_000, cachedInput: 25_000, output: 2_000_000},
		{modelID: "gpt-5-nano", input: 50_000, cachedInput: 5_000, output: 400_000},
		{modelID: "gpt-5-pro", input: 15_000_000, output: 120_000_000},
		{modelID: "codex-auto-review", input: 5_000_000, cachedInput: 500_000, output: 3_000_000},
		{modelID: "gpt-5.3-codex-spark", input: 3_150_000, cachedInput: 315_000, output: 25_200_000},
		{modelID: "gpt-4.1", input: 2_000_000, cachedInput: 500_000, output: 8_000_000},
		{modelID: "gpt-4.1-mini", input: 400_000, cachedInput: 100_000, output: 1_600_000},
		{modelID: "gpt-4.1-nano", input: 100_000, cachedInput: 25_000, output: 400_000},
		{modelID: "gpt-4o", input: 2_500_000, cachedInput: 1_250_000, output: 10_000_000},
		{modelID: "gpt-4o-mini", input: 150_000, cachedInput: 75_000, output: 600_000},
		{modelID: "o4-mini", input: 1_100_000, cachedInput: 275_000, output: 4_400_000},
		{modelID: "o3", input: 2_000_000, cachedInput: 500_000, output: 8_000_000},
		{modelID: "o3-mini", input: 1_100_000, cachedInput: 550_000, output: 4_400_000},
		{modelID: "o3-pro", input: 20_000_000, output: 80_000_000},
	}
	optionalMicros := func(value int64) *int64 {
		if value == 0 {
			return nil
		}
		return &value
	}
	equalOptional := func(got, want *int64) bool {
		return got == nil && want == nil || got != nil && want != nil && *got == *want
	}
	for _, test := range tests {
		t.Run(test.modelID, func(t *testing.T) {
			price := OpenAIOfficialPrice(test.modelID)
			if price == nil || price.ContextTier != OpenAIPriceCatalogTier || price.InputPriceMicros != test.input || price.OutputPriceMicros != test.output || !equalOptional(price.CachedInputPriceMicros, optionalMicros(test.cachedInput)) || !equalOptional(price.CacheWritePriceMicros, optionalMicros(test.cacheWrite)) {
				t.Fatalf("official price = %+v", price)
			}
		})
	}
	if OpenAIOfficialPrice("gpt-4.1-custom") != nil {
		t.Fatal("unknown model received a guessed official price")
	}
}

func TestReplaceChannelModelsRejectsNegativeCacheWritePrice(t *testing.T) {
	store := newTestStore(t)
	_, model, channels, _ := createRouteFixture(t, store, RoutingPriorityWeighted, "http://one.invalid")
	negativePrice := int64(-1)
	_, err := NewManagementService(store).ReplaceChannelModels(context.Background(), channels[0].ID, []ChannelModelInput{{
		ModelID:               model.ID,
		UpstreamModel:         "upstream-model",
		Weight:                100,
		CacheWritePriceMicros: &negativePrice,
		Enabled:               true,
	}})
	if err == nil {
		t.Fatal("negative cache-write price was accepted")
	}
}

func TestReplaceChannelModelsPersistsDisabledMapping(t *testing.T) {
	store := newTestStore(t)
	_, model, channels, _ := createRouteFixture(t, store, RoutingPriorityWeighted, "http://one.invalid")
	priceMultiplierBasisPoints := int64(1_700)

	mappings, err := NewManagementService(store).ReplaceChannelModels(context.Background(), channels[0].ID, []ChannelModelInput{{
		ModelID:                    model.ID,
		UpstreamModel:              "upstream-model",
		Weight:                     100,
		PriceMultiplierBasisPoints: &priceMultiplierBasisPoints,
		Enabled:                    false,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(mappings) != 1 || mappings[0].Enabled || mappings[0].PriceMultiplierBasisPoints != priceMultiplierBasisPoints {
		t.Fatalf("ReplaceChannelModels() = %+v; want one disabled mapping", mappings)
	}

	var stored ChannelModel
	if err := store.db.Where("channel_id = ? AND model_id = ?", channels[0].ID, model.ID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Enabled || stored.PriceMultiplierBasisPoints != priceMultiplierBasisPoints {
		t.Fatalf("stored mapping = %+v", stored)
	}
}

func TestReplaceChannelModelsDefaultsAndValidatesPriceMultiplier(t *testing.T) {
	store := newTestStore(t)
	_, model, channels, _ := createRouteFixture(t, store, RoutingPriorityWeighted, "http://one.invalid")
	management := NewManagementService(store)
	mappings, err := management.ReplaceChannelModels(context.Background(), channels[0].ID, []ChannelModelInput{{
		ModelID: model.ID, UpstreamModel: "upstream-model", Weight: 100, Enabled: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(mappings) != 1 || mappings[0].PriceMultiplierBasisPoints != DefaultPriceMultiplierBasisPoints {
		t.Fatalf("default multiplier mappings = %+v", mappings)
	}
	invalid := MaxPriceMultiplierBasisPoints + 1
	if _, err := management.ReplaceChannelModels(context.Background(), channels[0].ID, []ChannelModelInput{{
		ModelID: model.ID, UpstreamModel: "upstream-model", Weight: 100, PriceMultiplierBasisPoints: &invalid, Enabled: true,
	}}); err == nil {
		t.Fatal("invalid price multiplier was accepted")
	}
}

func TestChannelPriceMultiplierPersistsDefaultsAndValidates(t *testing.T) {
	store := newTestStore(t)
	management := NewManagementService(store)
	created, err := management.CreateChannel(context.Background(), ChannelInput{
		Name: "priced channel", BaseURL: "http://upstream.invalid/v1", APIKey: "secret", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.PriceMultiplierBasisPoints != DefaultPriceMultiplierBasisPoints {
		t.Fatalf("default channel multiplier = %d, want %d", created.PriceMultiplierBasisPoints, DefaultPriceMultiplierBasisPoints)
	}

	custom := int64(1_700)
	updated, err := management.UpdateChannel(context.Background(), created.ID, ChannelInput{
		Name: "priced channel", BaseURL: "http://upstream.invalid/v1", Enabled: true, PriceMultiplierBasisPoints: &custom,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.PriceMultiplierBasisPoints != custom {
		t.Fatalf("updated channel multiplier = %d, want %d", updated.PriceMultiplierBasisPoints, custom)
	}
	var stored Channel
	if err := store.db.First(&stored, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.PriceMultiplierBasisPoints != custom {
		t.Fatalf("stored channel multiplier = %d, want %d", stored.PriceMultiplierBasisPoints, custom)
	}
	preserved, err := management.UpdateChannel(context.Background(), created.ID, ChannelInput{
		Name: "priced channel", BaseURL: "http://upstream.invalid/v1", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if preserved.PriceMultiplierBasisPoints != custom {
		t.Fatalf("channel multiplier after omitted update = %d, want %d", preserved.PriceMultiplierBasisPoints, custom)
	}

	invalid := MaxPriceMultiplierBasisPoints + 1
	if _, err := management.UpdateChannel(context.Background(), created.ID, ChannelInput{
		Name: "priced channel", BaseURL: "http://upstream.invalid/v1", Enabled: true, PriceMultiplierBasisPoints: &invalid,
	}); err == nil {
		t.Fatal("invalid channel price multiplier was accepted")
	}
}

func TestBackfillTokenStatsAndLogFieldsRunOnce(t *testing.T) {
	store := newTestStore(t)
	createdAt := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	logs := []RelayRequestLog{
		{ID: "request-1", TokenID: 7, Endpoint: "responses", RequestedModel: "model-a", StatusCode: http.StatusOK, InputTokens: 10, OutputTokens: 4, CachedTokens: 3, CacheWriteTokens: 2, EstimatedCost: 12, AttemptCount: 1, DurationMS: 100, CreatedAt: createdAt},
		{ID: "request-2", TokenID: 7, Endpoint: "responses", RequestedModel: "model-a", StatusCode: http.StatusBadGateway, InputTokens: 6, OutputTokens: 0, EstimatedCost: 5, AttemptCount: 2, DurationMS: 300, CreatedAt: createdAt.Add(time.Hour)},
	}
	if err := store.db.Create(&logs).Error; err != nil {
		t.Fatal(err)
	}
	attempt := RelayAttemptLog{RequestID: logs[0].ID, ChannelID: 1, ChannelModelID: 1, UpstreamModel: "model-a", StatusCode: http.StatusOK, InputTokens: 10, CachedTokens: 3, CacheWriteTokens: 2, CreatedAt: createdAt}
	if err := store.db.Create(&attempt).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.backfillTokenDailyStats(); err != nil {
		t.Fatal(err)
	}
	if err := store.backfillTokenDailyStats(); err != nil {
		t.Fatal(err)
	}
	if err := store.backfillTokenLogFields(); err != nil {
		t.Fatal(err)
	}
	if err := store.backfillTokenLogFields(); err != nil {
		t.Fatal(err)
	}
	var stat TokenDailyStat
	if err := store.db.First(&stat, "date = ? AND token_id = ?", "2026-07-20", 7).Error; err != nil {
		t.Fatal(err)
	}
	if stat.RequestCount != 2 || stat.SuccessCount != 1 || stat.InputTokens != 16 || stat.NormalInputTokens != 11 || stat.OutputTokens != 4 || stat.CachedTokens != 3 || stat.CacheWriteTokens != 2 || stat.SentTokens != 16 || stat.EstimatedCost != 17 || stat.UpstreamCost != 17 || stat.DurationMS != 400 || stat.AttemptCount != 3 {
		t.Fatalf("backfilled stat = %+v", stat)
	}
	var backfilledRequest RelayRequestLog
	if err := store.db.First(&backfilledRequest, "id = ?", logs[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	var backfilledAttempt RelayAttemptLog
	if err := store.db.First(&backfilledAttempt, attempt.ID).Error; err != nil {
		t.Fatal(err)
	}
	if backfilledRequest.NormalInputTokens != 5 || backfilledRequest.SentTokens != 10 || backfilledAttempt.NormalInputTokens != 5 || backfilledAttempt.SentTokens != 10 {
		t.Fatalf("backfilled token details = request %+v attempt %+v", backfilledRequest, backfilledAttempt)
	}
	statistics, err := NewManagementService(store).tokenStatistics(context.Background(), 7)
	if err != nil || statistics.NormalInputTokens != 11 || statistics.CacheWriteTokens != 2 || statistics.SentTokens != 16 || statistics.UpstreamCost != 17 {
		t.Fatalf("token statistics = %+v, error = %v", statistics, err)
	}
}

func TestBackfillCostFieldsZerosRecentFailuresAndPreservesOlderAggregates(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()
	recentDate := now.Format(time.DateOnly)
	oldDate := now.Add(-(DetailedLogRetentionDays*24*time.Hour + 48*time.Hour)).Format(time.DateOnly)
	logs := []RelayRequestLog{
		{ID: "recent-success", TokenID: 9, Endpoint: "responses", RequestedModel: "model-a", StatusCode: http.StatusOK, InputTokens: 10, EstimatedCost: 11, CreatedAt: now.Add(-time.Hour)},
		{ID: "recent-failure", TokenID: 9, Endpoint: "responses", RequestedModel: "model-a", StatusCode: http.StatusBadGateway, InputTokens: 20, EstimatedCost: 22, CreatedAt: now.Add(-30 * time.Minute)},
	}
	if err := store.db.Create(&logs).Error; err != nil {
		t.Fatal(err)
	}
	attempts := []RelayAttemptLog{
		{RequestID: logs[0].ID, ChannelID: 1, ChannelModelID: 1, UpstreamModel: "model-a", StatusCode: http.StatusOK, EstimatedCost: 11, Success: true, CreatedAt: logs[0].CreatedAt},
		{RequestID: logs[1].ID, ChannelID: 1, ChannelModelID: 1, UpstreamModel: "model-a", StatusCode: http.StatusBadGateway, EstimatedCost: 22, Success: false, CreatedAt: logs[1].CreatedAt},
	}
	if err := store.db.Create(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	stats := []TokenDailyStat{
		{Date: recentDate, TokenID: 9, RequestCount: 2, SuccessCount: 1, EstimatedCost: 33},
		{Date: oldDate, TokenID: 9, RequestCount: 4, SuccessCount: 3, EstimatedCost: 77},
	}
	if err := store.db.Create(&stats).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.backfillCostFields(); err != nil {
		t.Fatal(err)
	}
	if err := store.backfillCostFields(); err != nil {
		t.Fatal(err)
	}
	var recentFailure RelayRequestLog
	if err := store.db.First(&recentFailure, "id = ?", "recent-failure").Error; err != nil {
		t.Fatal(err)
	}
	if recentFailure.EstimatedCost != 0 || recentFailure.UpstreamCost != 0 || recentFailure.CostSource != CostSourceFailedZero {
		t.Fatalf("recent failed request = %+v", recentFailure)
	}
	var recentSuccess RelayRequestLog
	if err := store.db.First(&recentSuccess, "id = ?", "recent-success").Error; err != nil {
		t.Fatal(err)
	}
	if recentSuccess.EstimatedCost != 11 || recentSuccess.UpstreamCost != 11 || recentSuccess.CostSource != CostSourceFallback {
		t.Fatalf("recent successful request = %+v", recentSuccess)
	}
	var recentStat TokenDailyStat
	if err := store.db.First(&recentStat, "date = ? AND token_id = ?", recentDate, 9).Error; err != nil {
		t.Fatal(err)
	}
	if recentStat.RequestCount != 2 || recentStat.SuccessCount != 1 || recentStat.EstimatedCost != 11 || recentStat.UpstreamCost != 11 {
		t.Fatalf("rebuilt recent stat = %+v", recentStat)
	}
	var oldStat TokenDailyStat
	if err := store.db.First(&oldStat, "date = ? AND token_id = ?", oldDate, 9).Error; err != nil {
		t.Fatal(err)
	}
	if oldStat.EstimatedCost != 77 || oldStat.UpstreamCost != 77 {
		t.Fatalf("preserved old stat = %+v", oldStat)
	}
}

func TestCleanupExpiredKeepsOnlyFiveDaysOfDetails(t *testing.T) {
	store := newTestStore(t)
	oldCreatedAt := time.Now().Add(-(DetailedLogRetentionDays*24*time.Hour + time.Hour))
	recentCreatedAt := time.Now().Add(-(DetailedLogRetentionDays*24*time.Hour - time.Hour))
	logs := []RelayRequestLog{
		{ID: "old-request", TokenID: 1, Endpoint: "chat", RequestedModel: "model-a", StatusCode: http.StatusOK, CreatedAt: oldCreatedAt},
		{ID: "recent-request", TokenID: 1, Endpoint: "chat", RequestedModel: "model-a", StatusCode: http.StatusOK, CreatedAt: recentCreatedAt},
	}
	if err := store.db.Create(&logs).Error; err != nil {
		t.Fatal(err)
	}
	attempts := []RelayAttemptLog{
		{RequestID: "old-request", ChannelID: 1, ChannelModelID: 1, UpstreamModel: "model-a", CreatedAt: oldCreatedAt},
		{RequestID: "recent-request", ChannelID: 1, ChannelModelID: 1, UpstreamModel: "model-a", CreatedAt: recentCreatedAt},
	}
	if err := store.db.Create(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	stat := TokenDailyStat{Date: oldCreatedAt.UTC().Format(time.DateOnly), TokenID: 1, RequestCount: 1}
	if err := store.db.Create(&stat).Error; err != nil {
		t.Fatal(err)
	}
	store.cleanupExpired()
	var requestCount int64
	if err := store.db.Model(&RelayRequestLog{}).Count(&requestCount).Error; err != nil || requestCount != 1 {
		t.Fatalf("request count = %d, err = %v", requestCount, err)
	}
	var attemptCount int64
	if err := store.db.Model(&RelayAttemptLog{}).Count(&attemptCount).Error; err != nil || attemptCount != 1 {
		t.Fatalf("attempt count = %d, err = %v", attemptCount, err)
	}
	var statCount int64
	if err := store.db.Model(&TokenDailyStat{}).Count(&statCount).Error; err != nil || statCount != 1 {
		t.Fatalf("stat count = %d, err = %v", statCount, err)
	}
}

func TestDashboardUsesTokenStatsWithoutDoubleCountingDetails(t *testing.T) {
	store := newTestStore(t)
	_, _, channels, mappings := createRouteFixture(t, store, RoutingPriorityWeighted, "http://one.invalid")
	stat := TokenDailyStat{
		Date: "2026-07-20", TokenID: 1, RequestCount: 8, SuccessCount: 6,
		InputTokens: 100, OutputTokens: 40, EstimatedCost: 900, UpstreamCost: 700, DurationMS: 1600,
	}
	if err := store.db.Create(&stat).Error; err != nil {
		t.Fatal(err)
	}
	requestLog := RelayRequestLog{
		ID: "detail-request", TokenID: 1, Endpoint: "responses", RequestedModel: "public-model",
		StatusCode: http.StatusOK, InputTokens: 10, OutputTokens: 2, EstimatedCost: 50, UpstreamCost: 30, CreatedAt: time.Now(),
	}
	if err := store.db.Create(&requestLog).Error; err != nil {
		t.Fatal(err)
	}
	attemptLog := RelayAttemptLog{
		RequestID: requestLog.ID, ChannelID: channels[0].ID, ChannelModelID: mappings[0].ID,
		UpstreamModel: mappings[0].UpstreamModel, StatusCode: http.StatusOK, EstimatedCost: 50, UpstreamCost: 30, Success: true, CreatedAt: time.Now(),
	}
	if err := store.db.Create(&attemptLog).Error; err != nil {
		t.Fatal(err)
	}
	summary, err := NewManagementService(store).Dashboard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if summary.Requests != 8 || summary.InputTokens != 100 || summary.OutputTokens != 40 || summary.EstimatedCost != 900 || summary.UpstreamCost != 700 || summary.AverageLatency != 200 {
		t.Fatalf("dashboard totals = %+v", summary)
	}
	if len(summary.Channels) != 1 || summary.Channels[0].Requests != 1 || summary.Channels[0].UpstreamCost != 30 || len(summary.Models) != 1 || summary.Models[0].Requests != 1 || summary.Models[0].UpstreamCost != 30 {
		t.Fatalf("dashboard breakdowns = channels %+v models %+v", summary.Channels, summary.Models)
	}
}

func TestListChannelsIncludesRecentLatencyAndCacheMetrics(t *testing.T) {
	store := newTestStore(t)
	secret, err := store.secretBox.Encrypt("upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	channels := []Channel{
		{Name: "measured", BaseURL: "https://measured.example/v1", APIKeyCipher: secret, Enabled: true},
		{Name: "empty", BaseURL: "https://empty.example/v1", APIKeyCipher: secret, Enabled: true},
	}
	if err := store.db.Create(&channels).Error; err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	latencyAttempts := make([]RelayAttemptLog, 0, 50)
	for index := 0; index < 50; index++ {
		latencyAttempts = append(latencyAttempts, RelayAttemptLog{
			RequestID:      fmt.Sprintf("latency-%d", index),
			ChannelID:      channels[0].ID,
			ChannelModelID: 1,
			UpstreamModel:  "upstream-model",
			StatusCode:     http.StatusOK,
			InputTokens:    10,
			CachedTokens:   5,
			UsageSource:    "upstream",
			LatencyMS:      int64(100 + index),
			Success:        true,
			CreatedAt:      now.Add(-4*time.Hour + time.Duration(index)*time.Minute),
		})
	}
	if err := store.db.Create(&latencyAttempts).Error; err != nil {
		t.Fatal(err)
	}
	extraAttempts := []RelayAttemptLog{
		{RequestID: "usage-without-latency", ChannelID: channels[0].ID, ChannelModelID: 1, UpstreamModel: "upstream-model", StatusCode: http.StatusOK, InputTokens: 100, CachedTokens: 25, UsageSource: "upstream", Success: true, CreatedAt: now.Add(-30 * time.Minute)},
		{RequestID: "estimated-usage", ChannelID: channels[0].ID, ChannelModelID: 1, UpstreamModel: "upstream-model", StatusCode: http.StatusOK, InputTokens: 400, UsageSource: "estimated_tiktoken", LatencyMS: 75, Success: true, CreatedAt: now.Add(-25 * time.Minute)},
		{RequestID: "failed", ChannelID: channels[0].ID, ChannelModelID: 1, UpstreamModel: "upstream-model", StatusCode: http.StatusBadGateway, InputTokens: 1000, CachedTokens: 1000, UsageSource: "upstream", LatencyMS: 888, Success: false, CreatedAt: now.Add(-20 * time.Minute)},
		{RequestID: "expired", ChannelID: channels[0].ID, ChannelModelID: 1, UpstreamModel: "upstream-model", StatusCode: http.StatusOK, InputTokens: 1000, CachedTokens: 1000, UsageSource: "upstream", LatencyMS: 999, Success: true, CreatedAt: now.Add(-(DetailedLogRetentionDays*24*time.Hour + time.Hour))},
	}
	if err := store.db.Create(&extraAttempts).Error; err != nil {
		t.Fatal(err)
	}

	views, err := NewManagementService(store).ListChannels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var measured, empty *ChannelView
	for index := range views {
		switch views[index].ID {
		case channels[0].ID:
			measured = &views[index]
		case channels[1].ID:
			empty = &views[index]
		}
	}
	if measured == nil || empty == nil {
		t.Fatalf("channel views = %+v", views)
	}
	if measured.Metrics.LatencySampleCount != 51 || len(measured.Metrics.LatencySeries) != channelLatencyPointLimit {
		t.Fatalf("latency metrics = %+v", measured.Metrics)
	}
	if measured.Metrics.LatencySeries[0].LatencyMS != 103 || measured.Metrics.LatencySeries[channelLatencyPointLimit-2].LatencyMS != 149 || measured.Metrics.LatencySeries[channelLatencyPointLimit-1].LatencyMS != 75 || measured.Metrics.LatestLatencyMS != 75 {
		t.Fatalf("latency series is not the latest chronological window: %+v", measured.Metrics.LatencySeries)
	}
	for index := 1; index < len(measured.Metrics.LatencySeries); index++ {
		if measured.Metrics.LatencySeries[index-1].RecordedAt.After(measured.Metrics.LatencySeries[index].RecordedAt) {
			t.Fatalf("latency series is not chronological: %+v", measured.Metrics.LatencySeries)
		}
	}
	if measured.Metrics.InputTokens != 600 || measured.Metrics.CachedTokens != 275 || math.Abs(measured.Metrics.CacheHitRate-275.0/600.0) > 0.000001 {
		t.Fatalf("cache metrics = %+v", measured.Metrics)
	}
	if empty.Metrics.LatencySeries == nil || len(empty.Metrics.LatencySeries) != 0 || empty.Metrics.InputTokens != 0 || empty.Metrics.CacheHitRate != 0 {
		t.Fatalf("empty channel metrics = %+v", empty.Metrics)
	}
}

func TestDiscoverChannelModelsUsesStoredSecretAndNormalizesList(t *testing.T) {
	store := newTestStore(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Errorf("request path = %q", request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "Bearer upstream-secret" {
			t.Errorf("Authorization = %q", authorization)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"object":"list","data":[{"id":"model-z","owned_by":"provider-z","created":22},{"id":"model-a","owned_by":"provider-a","created":11},{"id":"model-z"},{"id":"  "}]}`))
	}))
	defer upstream.Close()
	ciphertext, err := store.secretBox.Encrypt("upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	channel := Channel{Name: "discover", BaseURL: upstream.URL + "/v1", APIKeyCipher: ciphertext, Enabled: true}
	if err := store.db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	management := NewManagementService(store)
	latency, status, err := management.TestChannel(context.Background(), channel.ID)
	if err != nil || latency < 0 || status != http.StatusOK {
		t.Fatalf("TestChannel() = latency %d, status %d, error %v", latency, status, err)
	}
	var modelCount int64
	if err := store.db.Model(&GatewayModel{}).Count(&modelCount).Error; err != nil {
		t.Fatal(err)
	}
	if modelCount != 0 {
		t.Fatalf("channel test created %d public models", modelCount)
	}
	discovery, err := management.DiscoverChannelModels(context.Background(), ChannelModelDiscoveryInput{ChannelID: channel.ID})
	if err != nil {
		t.Fatal(err)
	}
	if discovery.Status != http.StatusOK || len(discovery.Models) != 2 || discovery.Models[0].ID != "model-a" || discovery.Models[1].ID != "model-z" {
		t.Fatalf("discovery = %+v", discovery)
	}
	if discovery.Models[0].OwnedBy != "provider-a" || discovery.Models[0].Created != 11 {
		t.Fatalf("first model = %+v", discovery.Models[0])
	}
	if !discovery.Models[0].PublicModelCreated || !discovery.Models[1].PublicModelCreated || discovery.Models[0].PublicModelID == 0 || discovery.Models[1].PublicModelID == 0 {
		t.Fatalf("discovered public models = %+v", discovery.Models)
	}
	var publicModels []GatewayModel
	if err := store.db.Order("name asc").Find(&publicModels).Error; err != nil {
		t.Fatal(err)
	}
	if len(publicModels) != 2 || publicModels[0].Name != "model-a" || publicModels[1].Name != "model-z" {
		t.Fatalf("public models = %+v", publicModels)
	}
	for _, publicModel := range publicModels {
		if !publicModel.Enabled || publicModel.RoutingStrategy != RoutingPriorityWeighted {
			t.Fatalf("public model defaults = %+v", publicModel)
		}
	}
	var refreshed Channel
	if err := store.db.First(&refreshed, channel.ID).Error; err != nil {
		t.Fatal(err)
	}
	if refreshed.LastHealthAt == nil || refreshed.LastError != "" {
		t.Fatalf("channel health = %+v", refreshed)
	}

	unsaved, err := management.DiscoverChannelModels(context.Background(), ChannelModelDiscoveryInput{
		BaseURL: upstream.URL + "/v1",
		APIKey:  "upstream-secret",
	})
	if err != nil || len(unsaved.Models) != 2 {
		t.Fatalf("unsaved discovery = %+v, %v", unsaved, err)
	}
	if unsaved.Models[0].PublicModelCreated || unsaved.Models[1].PublicModelCreated || unsaved.Models[0].PublicModelID != discovery.Models[0].PublicModelID || unsaved.Models[1].PublicModelID != discovery.Models[1].PublicModelID {
		t.Fatalf("idempotent public model discovery = %+v", unsaved.Models)
	}
}

func TestDiscoverChannelModelsIncludesExactOfficialPrice(t *testing.T) {
	store := newTestStore(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":[{"id":"gpt-4.1"},{"id":"gpt-4.1-custom"}]}`))
	}))
	defer upstream.Close()
	discovery, err := NewManagementService(store).DiscoverChannelModels(context.Background(), ChannelModelDiscoveryInput{
		BaseURL: upstream.URL,
		APIKey:  "upstream-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Models) != 2 || discovery.Models[0].OfficialPrice == nil || discovery.Models[0].OfficialPrice.Currency != "USD" || discovery.Models[0].OfficialPrice.Unit != OpenAIPriceCatalogUnit || discovery.Models[1].OfficialPrice != nil {
		t.Fatalf("discovery prices = %+v", discovery.Models)
	}
}

func TestClientAccessLimitsAndModelPermission(t *testing.T) {
	store := newTestStore(t)
	access := NewClientAccessService(store)
	token := &ClientToken{ID: 9, Enabled: true, RPM: 10, MaxConcurrency: 1}
	release, err := access.Acquire(token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := access.Acquire(token); !errors.Is(err, ErrConcurrencyLimit) {
		t.Fatalf("Acquire() error = %v, want concurrency error", err)
	}
	release()
	secondRelease, err := access.Acquire(token)
	if err != nil {
		t.Fatal(err)
	}
	secondRelease()

	token.RPM = 2
	if _, err := access.Acquire(token); !errors.Is(err, ErrRateLimitExceeded) {
		t.Fatalf("Acquire() error = %v, want RPM error", err)
	}

	model := GatewayModel{Name: "restricted", RoutingStrategy: RoutingPriorityWeighted, Enabled: true}
	if err := store.db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := access.AuthorizeModel(context.Background(), token, model.ID); !errors.Is(err, ErrModelNotAllowed) {
		t.Fatalf("AuthorizeModel() error = %v", err)
	}
	if err := store.db.Create(&ClientTokenModel{TokenID: token.ID, ModelID: model.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := access.AuthorizeModel(context.Background(), token, model.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRouterOrdersStrategies(t *testing.T) {
	router := &Router{random: func(int) int { return 0 }}
	candidates := []RouteCandidate{
		{Channel: Channel{ID: 1, LatencyEWMA: 80}, Mapping: ChannelModel{Priority: 1, Weight: 1}, Cost: 30},
		{Channel: Channel{ID: 2, LatencyEWMA: 0}, Mapping: ChannelModel{Priority: 2, Weight: 1}, Cost: 20},
		{Channel: Channel{ID: 3, LatencyEWMA: 40}, Mapping: ChannelModel{Priority: 2, Weight: 10}, Cost: 10},
	}
	router.orderCandidates(RoutingPriorityWeighted, candidates)
	if candidates[0].Mapping.Priority != 2 || candidates[2].Mapping.Priority != 1 {
		t.Fatalf("priority order = %+v", candidates)
	}
	router.orderCandidates(RoutingLowestCost, candidates)
	if candidates[0].Cost != 10 || candidates[2].Cost != 30 {
		t.Fatalf("cost order = %+v", candidates)
	}
	router.orderCandidates(RoutingLowestLatency, candidates)
	if candidates[0].Channel.ID != 2 || candidates[1].Channel.ID != 3 {
		t.Fatalf("latency order = %+v", candidates)
	}
}

func TestRouterSmoothWeightedPriorityBalancesAllocations(t *testing.T) {
	tests := []struct {
		name        string
		weights     []int
		allocations int
		want        []int
	}{
		{name: "equal weights", weights: []int{1, 1}, allocations: 20, want: []int{10, 10}},
		{name: "four to one", weights: []int{4, 1}, allocations: 50, want: []int{40, 10}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := &Router{random: func(int) int { return 0 }}
			base := make([]RouteCandidate, len(test.weights))
			for index, weight := range test.weights {
				id := uint64(index + 1)
				base[index] = RouteCandidate{
					Channel: Channel{ID: id},
					Mapping: ChannelModel{ID: id, ModelID: 1, Priority: 10, Weight: weight},
				}
			}
			counts := make([]int, len(test.weights))
			for range test.allocations {
				candidates := append([]RouteCandidate(nil), base...)
				router.orderCandidates(RoutingPriorityWeighted, candidates)
				counts[int(candidates[0].Mapping.ID)-1]++
			}
			for index, want := range test.want {
				if counts[index] != want {
					t.Fatalf("allocation counts = %v, want %v", counts, test.want)
				}
			}
		})
	}
}

func TestRouterSmoothWeightedPriorityBalancesConcurrentAllocations(t *testing.T) {
	router := &Router{random: func(int) int { return 0 }}
	base := []RouteCandidate{
		{Channel: Channel{ID: 1}, Mapping: ChannelModel{ID: 1, ModelID: 1, Priority: 10, Weight: 1}},
		{Channel: Channel{ID: 2}, Mapping: ChannelModel{ID: 2, ModelID: 1, Priority: 10, Weight: 1}},
	}
	const allocations = 100
	results := make(chan uint64, allocations)
	for range allocations {
		go func() {
			candidates := append([]RouteCandidate(nil), base...)
			router.orderCandidates(RoutingPriorityWeighted, candidates)
			results <- candidates[0].Mapping.ID
		}()
	}
	counts := [2]int{}
	for range allocations {
		counts[<-results-1]++
	}
	if counts != [2]int{50, 50} {
		t.Fatalf("concurrent allocation counts = %v, want [50 50]", counts)
	}
}

func TestRouterSessionAffinityDoesNotConsumeWeightedAllocation(t *testing.T) {
	store := newTestStore(t)
	token, model, _, mappings := createRouteFixture(t, store, RoutingPriorityWeighted, "http://one.invalid", "http://two.invalid")
	if err := store.db.Model(&ChannelModel{}).Where("model_id = ?", model.ID).Update("priority", 100).Error; err != nil {
		t.Fatal(err)
	}
	router := NewRouter(store, NewClientAccessService(store))
	router.random = func(int) int { return 0 }

	firstPlan, err := router.Plan(context.Background(), token, model.Name, 10, 10, "", "new-session-a")
	if err != nil {
		t.Fatal(err)
	}
	firstMappingID := firstPlan.Candidates[0].Mapping.ID
	router.RecordSessionAffinity(context.Background(), token.ID, model.ID, "new-session-a", firstMappingID)
	for range 5 {
		stickyPlan, stickyErr := router.Plan(context.Background(), token, model.Name, 10, 10, "", "new-session-a")
		if stickyErr != nil {
			t.Fatal(stickyErr)
		}
		if stickyPlan.Candidates[0].Mapping.ID != firstMappingID {
			t.Fatalf("sticky mapping = %d, want %d", stickyPlan.Candidates[0].Mapping.ID, firstMappingID)
		}
	}

	secondPlan, err := router.Plan(context.Background(), token, model.Name, 10, 10, "", "new-session-b")
	if err != nil {
		t.Fatal(err)
	}
	wantSecondMappingID := mappings[0].ID
	if wantSecondMappingID == firstMappingID {
		wantSecondMappingID = mappings[1].ID
	}
	if secondPlan.Candidates[0].Mapping.ID != wantSecondMappingID {
		t.Fatalf("second new session mapping = %d, want %d", secondPlan.Candidates[0].Mapping.ID, wantSecondMappingID)
	}
}

func TestParseRelayPayloadExtractsCodexSessionKey(t *testing.T) {
	payload, err := ParseRelayPayload([]byte(`{
		"model":"public-model",
		"prompt_cache_key":" codex-session ",
		"client_metadata":{"session_id":"fallback-session","private":"do-not-log"},
		"messages":[{"role":"user","content":"private prompt"}],
		"temperature":0.2,
		"reasoning":{"effort":"high","private":"do-not-log"},
		"tools":[{"type":"function","function":{"name":"lookup","description":"private tool description"}}],
		"unknown_field":"private unknown value"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if payload.SessionKey != "codex-session" || payload.SessionSource != "prompt_cache_key" {
		t.Fatalf("session = %q from %q", payload.SessionKey, payload.SessionSource)
	}
	parameters := decodeRequestParameters(payload.RequestParametersJSON)
	if parameters["model"] != "public-model" || parameters["temperature"] != json.Number("0.2") || parameters["message_count"] != json.Number("1") || parameters["tool_count"] != json.Number("1") {
		t.Fatalf("request parameters = %#v", parameters)
	}
	for _, blocked := range []string{"prompt_cache_key", "client_metadata", "messages", "unknown_field"} {
		if _, exists := parameters[blocked]; exists {
			t.Fatalf("sensitive parameter %q was retained: %#v", blocked, parameters)
		}
	}
	if strings.Contains(payload.RequestParametersJSON, "private") {
		t.Fatalf("parameter snapshot contains request content: %s", payload.RequestParametersJSON)
	}
	fallback, err := ParseRelayPayload([]byte(`{"model":"public-model","client_metadata":{"session_id":"session-from-metadata","thread_id":"thread-from-metadata"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if fallback.SessionKey != "session-from-metadata" || fallback.SessionSource != "client_metadata.session_id" {
		t.Fatalf("fallback session = %q from %q", fallback.SessionKey, fallback.SessionSource)
	}
	unavailable, err := ParseRelayPayload([]byte(`{"model":"public-model","input":"private prompt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if unavailable.SessionKey != "" || unavailable.SessionSource != "unavailable" || strings.Contains(unavailable.RequestParametersJSON, "private prompt") {
		t.Fatalf("unavailable session payload = %+v", unavailable)
	}
}

func TestSessionLogsAggregateExistingFiveDayDetails(t *testing.T) {
	store := newTestStore(t)
	token, model, channels, mappings := createRouteFixture(t, store, RoutingPriorityWeighted, "http://one.invalid", "http://two.invalid")
	now := time.Now()
	logs := []RelayRequestLog{
		{ID: "session-request-1", TokenID: token.ID, TokenName: token.Name, TokenKeyPrefix: token.KeyPrefix, Endpoint: "responses", RequestedModel: model.Name, CodexSessionID: "codex-session", CodexSessionSource: "prompt_cache_key", RequestParametersJSON: `{"temperature":0.2}`, StatusCode: http.StatusOK, InputTokens: 100, NormalInputTokens: 60, OutputTokens: 20, CachedTokens: 30, CacheWriteTokens: 10, SentTokens: 100, EstimatedCost: 70, UpstreamCost: 60, AttemptCount: 1, DurationMS: 100, CreatedAt: now.Add(-2 * time.Hour)},
		{ID: "session-request-2", TokenID: token.ID, TokenName: token.Name, TokenKeyPrefix: token.KeyPrefix, Endpoint: "responses", RequestedModel: model.Name, CodexSessionID: "codex-session", CodexSessionSource: "prompt_cache_key", RequestParametersJSON: `{"reasoning":{"effort":"high"}}`, StatusCode: http.StatusBadGateway, InputTokens: 10, NormalInputTokens: 10, SentTokens: 20, EstimatedCost: 0, UpstreamCost: 0, AttemptCount: 2, DurationMS: 300, CreatedAt: now.Add(-time.Hour)},
		{ID: "unknown-request", TokenID: token.ID, TokenName: token.Name, TokenKeyPrefix: token.KeyPrefix, Endpoint: "chat", RequestedModel: model.Name, CodexSessionSource: "unavailable", RequestParametersJSON: `{}`, StatusCode: http.StatusOK, InputTokens: 50, OutputTokens: 10, CachedTokens: 5, EstimatedCost: 20, UpstreamCost: 15, AttemptCount: 0, DurationMS: 80, CreatedAt: now.Add(-30 * time.Minute)},
		{ID: "expired-request", TokenID: token.ID, Endpoint: "responses", RequestedModel: model.Name, CodexSessionID: "codex-session", StatusCode: http.StatusOK, InputTokens: 1000, CreatedAt: now.Add(-(DetailedLogRetentionDays*24*time.Hour + time.Hour))},
	}
	if err := store.db.Create(&logs).Error; err != nil {
		t.Fatal(err)
	}
	attempts := []RelayAttemptLog{
		{RequestID: logs[0].ID, ChannelID: channels[0].ID, ChannelName: channels[0].Name, ChannelBaseURL: channels[0].BaseURL, ChannelModelID: mappings[0].ID, UpstreamModel: mappings[0].UpstreamModel, SelectionReason: SelectionReasonInitialRoute, StatusCode: http.StatusOK, InputTokens: 100, NormalInputTokens: 60, OutputTokens: 20, CachedTokens: 30, CacheWriteTokens: 10, SentTokens: 100, Success: true, CreatedAt: logs[0].CreatedAt},
		{RequestID: logs[1].ID, ChannelID: channels[0].ID, ChannelName: channels[0].Name, ChannelBaseURL: channels[0].BaseURL, ChannelModelID: mappings[0].ID, UpstreamModel: mappings[0].UpstreamModel, StatusCode: http.StatusInternalServerError, SentTokens: 10, Success: false, CreatedAt: logs[1].CreatedAt},
		{RequestID: logs[1].ID, ChannelID: channels[1].ID, ChannelName: channels[1].Name, ChannelBaseURL: channels[1].BaseURL, ChannelModelID: mappings[1].ID, UpstreamModel: mappings[1].UpstreamModel, PreviousChannelID: channels[0].ID, PreviousChannelName: channels[0].Name, SelectionReason: SelectionReasonRetryableStatus, SelectionDetail: "HTTP 500", StatusCode: http.StatusBadGateway, SentTokens: 10, Success: false, CreatedAt: logs[1].CreatedAt.Add(time.Second)},
	}
	if err := store.db.Create(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	NewRouter(store, NewClientAccessService(store)).RecordSessionAffinity(context.Background(), token.ID, model.ID, "codex-session", mappings[1].ID)

	management := NewManagementService(store)
	page, err := management.SessionLogs(context.Background(), SessionLogQuery{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("session page = %+v", page)
	}
	var identified, unknown *SessionLogSummary
	for index := range page.Items {
		if page.Items[index].Identified {
			identified = &page.Items[index]
		} else {
			unknown = &page.Items[index]
		}
	}
	if identified == nil || unknown == nil {
		t.Fatalf("session summaries = %+v", page.Items)
	}
	if identified.RequestCount != 2 || identified.SuccessCount != 1 || identified.AttemptCount != 3 || identified.InputTokens != 110 || identified.NormalInputTokens != 70 || identified.OutputTokens != 20 || identified.CachedTokens != 30 || identified.CacheWriteTokens != 10 || identified.SentTokens != 120 || identified.EstimatedCost != 70 || identified.UpstreamCost != 60 {
		t.Fatalf("identified summary = %+v", identified)
	}
	if identified.CurrentChannel == nil || identified.CurrentChannel.ChannelID != channels[1].ID || identified.CurrentChannel.AssignmentSource != "session_affinity" {
		t.Fatalf("current channel = %+v", identified.CurrentChannel)
	}
	if unknown.FallbackRequestID != "unknown-request" || unknown.RequestCount != 1 || unknown.CurrentChannel != nil {
		t.Fatalf("unknown summary = %+v", unknown)
	}
	filtered, err := management.SessionLogs(context.Background(), SessionLogQuery{ChannelID: channels[1].ID, Page: 1, PageSize: 25})
	if err != nil || filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Items[0].SessionID != "codex-session" {
		t.Fatalf("channel-filtered sessions = %+v, %v", filtered, err)
	}

	detail, err := management.SessionLogDetail(context.Background(), SessionDetailQuery{SessionID: "codex-session", TokenID: token.ID, Page: 1, PageSize: 25})
	if err != nil {
		t.Fatal(err)
	}
	if detail.RequestTotal != 2 || len(detail.Requests) != 2 || detail.Requests[0].ID != "session-request-1" || len(detail.Requests[1].Attempts) != 2 {
		t.Fatalf("session detail = %+v", detail)
	}
	if detail.Summary.NormalInputTokens != 70 || detail.Summary.CacheWriteTokens != 10 || detail.Summary.SentTokens != 120 || detail.Summary.UpstreamCost != 60 || detail.Requests[0].CacheWriteTokens != 10 || detail.Requests[0].Attempts[0].CacheWriteTokens != 10 || detail.Requests[0].Attempts[0].SentTokens != 100 {
		t.Fatalf("session cache-write detail = %+v", detail)
	}
	if detail.Requests[0].Attempts[0].SelectionReason != SelectionReasonInitialRoute || detail.Requests[1].Attempts[0].SelectionReason != "" || detail.Requests[1].Attempts[1].SelectionReason != SelectionReasonRetryableStatus || detail.Requests[1].Attempts[1].PreviousChannelID != channels[0].ID {
		t.Fatalf("session selection metadata = %+v", detail.Requests)
	}
	secondPage, err := management.SessionLogDetail(context.Background(), SessionDetailQuery{SessionID: "codex-session", TokenID: token.ID, Page: 2, PageSize: 1})
	if err != nil || len(secondPage.Requests) != 1 || secondPage.Requests[0].ID != "session-request-2" || secondPage.RequestTotal != 2 {
		t.Fatalf("session detail second page = %+v, %v", secondPage, err)
	}
	reasoning, ok := detail.Requests[1].RequestParameters["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "high" {
		t.Fatalf("detail parameters = %#v", detail.Requests[0].RequestParameters)
	}
	unknownDetail, err := management.SessionLogDetail(context.Background(), SessionDetailQuery{RequestID: "unknown-request", Page: 1, PageSize: 25})
	if err != nil || unknownDetail.RequestTotal != 1 || unknownDetail.Summary.Identified {
		t.Fatalf("unknown detail = %+v, %v", unknownDetail, err)
	}
}

func TestResponsesAffinityPinsOriginalMapping(t *testing.T) {
	store := newTestStore(t)
	token, model, channels, mappings := createRouteFixture(t, store, RoutingLowestCost, "http://one.invalid", "http://two.invalid")
	access := NewClientAccessService(store)
	router := NewRouter(store, access)
	router.RecordAffinity(context.Background(), "resp_123", mappings[1].ID)
	plan, err := router.Plan(context.Background(), token, model.Name, 10, 10, "resp_123", "")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Affinity || len(plan.Candidates) != 1 || plan.Candidates[0].Mapping.ID != mappings[1].ID {
		t.Fatalf("affinity plan = %+v", plan)
	}
	if plan.InitialSelection.Reason != SelectionReasonResponseAffinity {
		t.Fatalf("response affinity selection = %+v", plan.InitialSelection)
	}
	openUntil := time.Now().Add(time.Minute)
	if err := store.db.Model(&channels[1]).Update("circuit_open_until", openUntil).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := router.Plan(context.Background(), token, model.Name, 10, 10, "resp_123", ""); !errors.Is(err, ErrAffinityUnavailable) {
		t.Fatalf("Plan() error = %v, want affinity unavailable", err)
	}
}

func TestCodexSessionAffinityPinsMappingAndAllowsCircuitFailover(t *testing.T) {
	store := newTestStore(t)
	token, model, channels, mappings := createRouteFixture(t, store, RoutingPriorityWeighted, "http://one.invalid", "http://two.invalid")
	router := NewRouter(store, NewClientAccessService(store))
	router.RecordSessionAffinity(context.Background(), token.ID, model.ID, "codex-session-a", mappings[1].ID)

	plan, err := router.Plan(context.Background(), token, model.Name, 10, 10, "", "codex-session-a")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.SessionAffinity || plan.Affinity || len(plan.Candidates) != 2 || plan.Candidates[0].Mapping.ID != mappings[1].ID {
		t.Fatalf("session affinity plan = %+v", plan)
	}
	if plan.InitialSelection.Reason != SelectionReasonSessionAffinity {
		t.Fatalf("session affinity selection = %+v", plan.InitialSelection)
	}

	otherPlan, err := router.Plan(context.Background(), token, model.Name, 10, 10, "", "codex-session-b")
	if err != nil {
		t.Fatal(err)
	}
	if otherPlan.SessionAffinity || otherPlan.Candidates[0].Mapping.ID != mappings[0].ID {
		t.Fatalf("independent session plan = %+v", otherPlan)
	}

	openUntil := time.Now().Add(time.Minute)
	if err := store.db.Model(&channels[1]).Update("circuit_open_until", openUntil).Error; err != nil {
		t.Fatal(err)
	}
	failoverPlan, err := router.Plan(context.Background(), token, model.Name, 10, 10, "", "codex-session-a")
	if err != nil {
		t.Fatal(err)
	}
	if !failoverPlan.SessionAffinity || len(failoverPlan.Candidates) != 1 || failoverPlan.Candidates[0].Mapping.ID != mappings[0].ID {
		t.Fatalf("circuit failover plan = %+v", failoverPlan)
	}
	if failoverPlan.InitialSelection.Reason != SelectionReasonCircuitOpen || failoverPlan.InitialSelection.PreviousChannelID != channels[1].ID || failoverPlan.InitialSelection.PreviousChannelName != channels[1].Name || failoverPlan.InitialSelection.Detail == "" {
		t.Fatalf("circuit failover selection = %+v", failoverPlan.InitialSelection)
	}
}

func TestCodexSessionAffinityClassifiesUnavailableTarget(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*testing.T, *Store, Channel, ChannelModel)
		wantReason string
	}{
		{
			name: "channel disabled",
			mutate: func(t *testing.T, store *Store, channel Channel, _ ChannelModel) {
				t.Helper()
				if err := store.db.Model(&channel).Update("enabled", false).Error; err != nil {
					t.Fatal(err)
				}
			},
			wantReason: SelectionReasonChannelDisabled,
		},
		{
			name: "mapping disabled",
			mutate: func(t *testing.T, store *Store, _ Channel, mapping ChannelModel) {
				t.Helper()
				if err := store.db.Model(&mapping).Update("enabled", false).Error; err != nil {
					t.Fatal(err)
				}
			},
			wantReason: SelectionReasonMappingDisabled,
		},
		{
			name: "target deleted",
			mutate: func(t *testing.T, store *Store, _ Channel, mapping ChannelModel) {
				t.Helper()
				if err := store.db.Delete(&mapping).Error; err != nil {
					t.Fatal(err)
				}
			},
			wantReason: SelectionReasonAffinityTargetMissing,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newTestStore(t)
			token, model, channels, mappings := createRouteFixture(t, store, RoutingPriorityWeighted, "http://one.invalid", "http://two.invalid")
			router := NewRouter(store, NewClientAccessService(store))
			router.RecordSessionAffinity(context.Background(), token.ID, model.ID, "codex-session", mappings[0].ID)
			if err := store.db.Create(&RelayRequestLog{
				ID: "historical-request", TokenID: token.ID, RequestedModel: model.Name,
				CodexSessionID: "codex-session", CreatedAt: time.Now().Add(-time.Minute),
			}).Error; err != nil {
				t.Fatal(err)
			}
			if err := store.db.Create(&RelayAttemptLog{
				RequestID: "historical-request", ChannelID: channels[0].ID, ChannelName: channels[0].Name,
				ChannelModelID: mappings[0].ID, UpstreamModel: mappings[0].UpstreamModel, CreatedAt: time.Now().Add(-time.Minute),
			}).Error; err != nil {
				t.Fatal(err)
			}

			test.mutate(t, store, channels[0], mappings[0])
			plan, err := router.Plan(context.Background(), token, model.Name, 10, 10, "", "codex-session")
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Candidates) != 1 || plan.Candidates[0].Channel.ID != channels[1].ID {
				t.Fatalf("failover candidates = %+v", plan.Candidates)
			}
			selection := plan.InitialSelection
			if selection.Reason != test.wantReason || selection.PreviousChannelID != channels[0].ID || selection.PreviousChannelName != channels[0].Name {
				t.Fatalf("selection = %+v, want reason %q and previous channel %d", selection, test.wantReason, channels[0].ID)
			}
		})
	}
}

func TestCircuitOpensAfterThreeFailuresAndRecovers(t *testing.T) {
	store := newTestStore(t)
	_, _, channels, _ := createRouteFixture(t, store, RoutingPriorityWeighted, "http://one.invalid")
	relay := newTestRelay(store)
	for range 3 {
		relay.recordChannelFailure(context.Background(), channels[0].ID, "retryable failure")
	}
	var channel Channel
	if err := store.db.First(&channel, channels[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if channel.ConsecutiveFailures != 3 || channel.CircuitOpenUntil == nil || !channel.CircuitOpenUntil.After(time.Now()) {
		t.Fatalf("channel circuit state = %+v", channel)
	}
	channelID := channel.ID
	relay.recordChannelSuccess(context.Background(), channelID, 42)
	channel = Channel{}
	if err := store.db.First(&channel, channelID).Error; err != nil {
		t.Fatal(err)
	}
	if channel.ConsecutiveFailures != 0 || channel.CircuitOpenUntil != nil || channel.LatencyEWMA != 42 {
		t.Fatalf("recovered channel = %+v", channel)
	}
}

func TestRelayRetriesJSONAndRewritesAuthorizationAndModel(t *testing.T) {
	store := newTestStore(t)
	var firstCalls atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		firstCalls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"error":{"message":"temporary","type":"api_error","code":"temporary"},"usage":{"prompt_tokens":100,"completion_tokens":20,"cost":"9.99"}}`))
	}))
	defer first.Close()
	var secondCalls atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		secondCalls.Add(1)
		if request.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != "upstream-2" || body["unknown_field"] != "preserved" {
			t.Errorf("upstream body = %+v", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Header().Set("Set-Cookie", "gateway_admin_session=poisoned; Path=/api")
		_, _ = writer.Write([]byte(`{"id":"chatcmpl_1","object":"chat.completion","usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":2,"cache_creation_tokens":1},"cost":"0.000042"}}`))
	}))
	defer second.Close()
	token, _, _, _ := createRouteFixture(t, store, RoutingPriorityWeighted, first.URL, second.URL)
	payloadBody := []byte(`{"model":"public-model","prompt_cache_key":"codex-session-log","temperature":0.4,"messages":[{"role":"user","content":"hello"}],"unknown_field":"preserved"}`)
	payload, err := ParseRelayPayload(payloadBody)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	relay := newTestRelay(store)
	if publicErr := relay.Relay(context.Background(), recorder, http.Header{"Authorization": []string{"Bearer client-secret"}}, "", "chat", token, payload, payloadBody); publicErr != nil {
		t.Fatal(publicErr)
	}
	if recorder.Code != http.StatusOK || firstCalls.Load() != 1 || secondCalls.Load() != 1 {
		t.Fatalf("status=%d calls=%d/%d body=%s", recorder.Code, firstCalls.Load(), secondCalls.Load(), recorder.Body.String())
	}
	if cookie := recorder.Header().Get("Set-Cookie"); cookie != "" {
		t.Fatalf("upstream Set-Cookie was forwarded: %q", cookie)
	}
	var requestLog RelayRequestLog
	if err := store.db.First(&requestLog).Error; err != nil {
		t.Fatal(err)
	}
	if requestLog.AttemptCount != 2 || requestLog.InputTokens != 110 || requestLog.NormalInputTokens != 107 || requestLog.OutputTokens != 25 || requestLog.CachedTokens != 2 || requestLog.CacheWriteTokens != 1 || requestLog.SentTokens <= 0 || requestLog.EstimatedCost != 15 || requestLog.UpstreamCost != 42 || requestLog.CostSource != CostSourceUpstream {
		t.Fatalf("request log = %+v", requestLog)
	}
	if requestLog.CodexSessionID != "codex-session-log" || requestLog.CodexSessionSource != "prompt_cache_key" || requestLog.TokenName != token.Name || requestLog.TokenKeyPrefix != token.KeyPrefix {
		t.Fatalf("request log identity snapshot = %+v", requestLog)
	}
	if !strings.Contains(requestLog.RequestParametersJSON, `"temperature":0.4`) || strings.Contains(requestLog.RequestParametersJSON, "hello") || strings.Contains(requestLog.RequestParametersJSON, "unknown_field") {
		t.Fatalf("request parameter snapshot = %s", requestLog.RequestParametersJSON)
	}
	var attemptLogs []RelayAttemptLog
	if err := store.db.Order("id ASC").Find(&attemptLogs).Error; err != nil {
		t.Fatal(err)
	}
	if len(attemptLogs) != 2 || attemptLogs[0].ChannelName == "" || attemptLogs[0].ChannelBaseURL == "" || attemptLogs[1].ChannelName == "" || attemptLogs[1].NormalInputTokens != 7 || attemptLogs[1].CacheWriteTokens != 1 || attemptLogs[0].SentTokens <= 0 || attemptLogs[1].SentTokens <= 0 || attemptLogs[0].EstimatedCost != 0 || attemptLogs[0].UpstreamCost != 0 || attemptLogs[0].CostSource != CostSourceFailedZero || attemptLogs[1].UpstreamCost != 42 || requestLog.SentTokens != attemptLogs[0].SentTokens+attemptLogs[1].SentTokens {
		t.Fatalf("attempt channel snapshots = %+v", attemptLogs)
	}
	if attemptLogs[0].SelectionReason != SelectionReasonInitialRoute || attemptLogs[1].SelectionReason != SelectionReasonRetryableStatus || attemptLogs[1].SelectionDetail != "HTTP 500" || attemptLogs[1].PreviousChannelID != attemptLogs[0].ChannelID || attemptLogs[1].PreviousChannelName != attemptLogs[0].ChannelName {
		t.Fatalf("attempt selection metadata = %+v", attemptLogs)
	}
	var stat TokenDailyStat
	if err := store.db.First(&stat).Error; err != nil {
		t.Fatal(err)
	}
	if stat.RequestCount != 1 || stat.SuccessCount != 1 || stat.InputTokens != 110 || stat.NormalInputTokens != 107 || stat.OutputTokens != 25 || stat.CachedTokens != 2 || stat.CacheWriteTokens != 1 || stat.SentTokens != requestLog.SentTokens || stat.EstimatedCost != 15 || stat.UpstreamCost != 42 || stat.AttemptCount != 2 {
		t.Fatalf("daily stat = %+v", stat)
	}
}

func TestRelayZerosCostsForNonSuccessAndNetworkFailure(t *testing.T) {
	t.Run("non success response", func(t *testing.T) {
		store := newTestStore(t)
		upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":{"code":"bad_request"},"usage":{"input_tokens":20,"output_tokens":4,"cost":"1.25"}}`))
		}))
		defer upstream.Close()
		token, _, _, _ := createRouteFixture(t, store, RoutingPriorityWeighted, upstream.URL)
		body := []byte(`{"model":"public-model","input":"hello"}`)
		payload, err := ParseRelayPayload(body)
		if err != nil {
			t.Fatal(err)
		}
		if publicErr := newTestRelay(store).Relay(context.Background(), httptest.NewRecorder(), nil, "", "responses", token, payload, body); publicErr != nil {
			t.Fatal(publicErr)
		}
		var requestLog RelayRequestLog
		if err := store.db.First(&requestLog).Error; err != nil {
			t.Fatal(err)
		}
		var attemptLog RelayAttemptLog
		if err := store.db.First(&attemptLog).Error; err != nil {
			t.Fatal(err)
		}
		if requestLog.InputTokens != 20 || requestLog.OutputTokens != 4 || requestLog.EstimatedCost != 0 || requestLog.UpstreamCost != 0 || requestLog.CostSource != CostSourceFailedZero || attemptLog.EstimatedCost != 0 || attemptLog.UpstreamCost != 0 || attemptLog.CostSource != CostSourceFailedZero {
			t.Fatalf("request = %+v, attempt = %+v", requestLog, attemptLog)
		}
	})

	t.Run("network failure", func(t *testing.T) {
		store := newTestStore(t)
		token, _, _, _ := createRouteFixture(t, store, RoutingPriorityWeighted, "http://network.invalid")
		body := []byte(`{"model":"public-model","input":"hello"}`)
		payload, err := ParseRelayPayload(body)
		if err != nil {
			t.Fatal(err)
		}
		relay := newTestRelay(store)
		relay.client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network unavailable")
		})
		publicErr := relay.Relay(context.Background(), httptest.NewRecorder(), nil, "", "responses", token, payload, body)
		if publicErr == nil || publicErr.Status != http.StatusBadGateway {
			t.Fatalf("Relay() error = %+v", publicErr)
		}
		var requestLog RelayRequestLog
		if err := store.db.First(&requestLog).Error; err != nil {
			t.Fatal(err)
		}
		var attemptLog RelayAttemptLog
		if err := store.db.First(&attemptLog).Error; err != nil {
			t.Fatal(err)
		}
		if requestLog.EstimatedCost != 0 || requestLog.UpstreamCost != 0 || requestLog.CostSource != CostSourceFailedZero || attemptLog.EstimatedCost != 0 || attemptLog.UpstreamCost != 0 || attemptLog.CostSource != CostSourceFailedZero {
			t.Fatalf("request = %+v, attempt = %+v", requestLog, attemptLog)
		}
	})
}

func TestRelayStreamUsesLastValidCostSnapshot(t *testing.T) {
	store := newTestStore(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"usage\":{\"input_tokens\":10,\"output_tokens\":1,\"cost\":\"0.000100\"}}\n\n"))
		_, _ = writer.Write([]byte("data: {\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_cost\":\"0.000250\"}}\n\n"))
		_, _ = writer.Write([]byte("data: {\"usage\":{\"cost\":\"invalid\"}}\n\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()
	token, _, _, _ := createRouteFixture(t, store, RoutingPriorityWeighted, upstream.URL)
	body := []byte(`{"model":"public-model","stream":true,"input":"hello"}`)
	payload, err := ParseRelayPayload(body)
	if err != nil {
		t.Fatal(err)
	}
	if publicErr := newTestRelay(store).Relay(context.Background(), httptest.NewRecorder(), nil, "", "responses", token, payload, body); publicErr != nil {
		t.Fatal(publicErr)
	}
	var requestLog RelayRequestLog
	if err := store.db.First(&requestLog).Error; err != nil {
		t.Fatal(err)
	}
	if requestLog.InputTokens != 10 || requestLog.OutputTokens != 2 || requestLog.EstimatedCost != 12 || requestLog.UpstreamCost != 250 || requestLog.CostSource != CostSourceUpstream {
		t.Fatalf("stream request = %+v", requestLog)
	}
}

func TestRequestCostSourceBecomesMixedAcrossSuccessfulAttempts(t *testing.T) {
	store := newTestStore(t)
	token := ClientToken{ID: 99, Name: "mixed", KeyPrefix: "sk-mixed"}
	execution := &relayExecution{
		requestID: "mixed-request", token: &token, endpoint: "responses", payload: &RelayPayload{Model: "model-a"}, startedAt: time.Now(),
		usageSources: map[string]struct{}{}, costSources: map[string]struct{}{},
	}
	relay := newTestRelay(store)
	relay.addUsage(execution, Usage{InputTokens: 1, Source: "upstream"}, 10, 11, CostSourceUpstream, true)
	relay.addUsage(execution, Usage{OutputTokens: 1, Source: "estimated_tiktoken"}, 20, 20, CostSourceFallback, true)
	relay.recordRequest(context.Background(), execution, http.StatusOK, "")
	var requestLog RelayRequestLog
	if err := store.db.First(&requestLog, "id = ?", execution.requestID).Error; err != nil {
		t.Fatal(err)
	}
	if requestLog.EstimatedCost != 30 || requestLog.UpstreamCost != 31 || requestLog.CostSource != CostSourceMixed || requestLog.UsageSource != "mixed" {
		t.Fatalf("mixed request = %+v", requestLog)
	}
}

func TestRelayDoesNotCountSentTokensBeforeNetworkAttempt(t *testing.T) {
	store := newTestStore(t)
	token, _, channels, _ := createRouteFixture(t, store, RoutingPriorityWeighted, "http://never-called.invalid")
	if err := store.db.Model(&Channel{}).Where("id = ?", channels[0].ID).Update("api_key_cipher", "invalid-ciphertext").Error; err != nil {
		t.Fatal(err)
	}
	payloadBody := []byte(`{"model":"public-model","input":"hello"}`)
	payload, err := ParseRelayPayload(payloadBody)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	publicErr := newTestRelay(store).Relay(context.Background(), recorder, nil, "", "responses", token, payload, payloadBody)
	if publicErr == nil || publicErr.Status != http.StatusBadGateway {
		t.Fatalf("Relay() error = %+v, want 502", publicErr)
	}
	var requestLog RelayRequestLog
	if err := store.db.First(&requestLog).Error; err != nil {
		t.Fatal(err)
	}
	if requestLog.SentTokens != 0 {
		t.Fatalf("sent tokens = %d, want 0 before an upstream network call", requestLog.SentTokens)
	}
	var attempts []RelayAttemptLog
	if err := store.db.Order("id ASC").Find(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 || attempts[0].SentTokens != 0 || attempts[0].SelectionReason != SelectionReasonInitialRoute || attempts[0].ErrorMessage != "gateway preparation failed: credential_decrypt" {
		t.Fatalf("preparation attempt logs = %+v", attempts)
	}
}

func TestRelayRecordsRetrySelectionReasons(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprintf("status %d", status), func(t *testing.T) {
			store := newTestStore(t)
			first := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				writer.WriteHeader(status)
				_, _ = writer.Write([]byte(`{"error":{"code":"retry"}}`))
			}))
			defer first.Close()
			second := successfulResponseServer()
			defer second.Close()
			token, _, _, _ := createRouteFixture(t, store, RoutingPriorityWeighted, first.URL, second.URL)

			relayTestRequest(t, newTestRelay(store), token)
			attempts := relayAttempts(t, store)
			if len(attempts) != 2 || attempts[1].SelectionReason != SelectionReasonRetryableStatus || attempts[1].SelectionDetail != fmt.Sprintf("HTTP %d", status) || attempts[1].PreviousChannelID != attempts[0].ChannelID || attempts[1].PreviousChannelName != attempts[0].ChannelName {
				t.Fatalf("attempts = %+v", attempts)
			}
		})
	}

	t.Run("transport error", func(t *testing.T) {
		store := newTestStore(t)
		second := successfulResponseServer()
		defer second.Close()
		token, _, _, _ := createRouteFixture(t, store, RoutingPriorityWeighted, "http://network.invalid", second.URL)
		relay := newTestRelay(store)
		transportCalls := 0
		relay.client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			transportCalls++
			if transportCalls == 1 {
				return nil, errors.New("network unavailable")
			}
			return http.DefaultTransport.RoundTrip(request)
		})

		relayTestRequest(t, relay, token)
		attempts := relayAttempts(t, store)
		if len(attempts) != 2 || attempts[1].SelectionReason != SelectionReasonTransportError || attempts[1].SelectionDetail != "upstream_request" || attempts[1].PreviousChannelID != attempts[0].ChannelID {
			t.Fatalf("attempts = %+v", attempts)
		}
	})

	t.Run("response read error", func(t *testing.T) {
		store := newTestStore(t)
		second := successfulResponseServer()
		defer second.Close()
		token, _, _, _ := createRouteFixture(t, store, RoutingPriorityWeighted, "http://response.invalid", second.URL)
		relay := newTestRelay(store)
		transportCalls := 0
		relay.client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			transportCalls++
			if transportCalls == 1 {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       errorReadCloser{},
					Request:    request,
				}, nil
			}
			return http.DefaultTransport.RoundTrip(request)
		})

		relayTestRequest(t, relay, token)
		attempts := relayAttempts(t, store)
		if len(attempts) != 2 || attempts[1].SelectionReason != SelectionReasonResponseError || attempts[1].SelectionDetail != "response_body_read" || attempts[1].PreviousChannelID != attempts[0].ChannelID {
			t.Fatalf("attempts = %+v", attempts)
		}
	})

	t.Run("gateway preparation error", func(t *testing.T) {
		store := newTestStore(t)
		second := successfulResponseServer()
		defer second.Close()
		token, _, channels, _ := createRouteFixture(t, store, RoutingPriorityWeighted, "http://never-called.invalid", second.URL)
		if err := store.db.Model(&Channel{}).Where("id = ?", channels[0].ID).Update("api_key_cipher", "invalid-ciphertext").Error; err != nil {
			t.Fatal(err)
		}

		relayTestRequest(t, newTestRelay(store), token)
		attempts := relayAttempts(t, store)
		if len(attempts) != 2 || attempts[0].SentTokens != 0 || attempts[1].SelectionReason != SelectionReasonGatewayPreparationError || attempts[1].SelectionDetail != "credential_decrypt" || attempts[1].PreviousChannelID != attempts[0].ChannelID {
			t.Fatalf("attempts = %+v", attempts)
		}
	})

	t.Run("new circuit", func(t *testing.T) {
		store := newTestStore(t)
		first := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(`{"error":{"code":"retry"}}`))
		}))
		defer first.Close()
		second := successfulResponseServer()
		defer second.Close()
		token, _, channels, _ := createRouteFixture(t, store, RoutingPriorityWeighted, first.URL, second.URL)
		if err := store.db.Model(&Channel{}).Where("id = ?", channels[0].ID).Update("consecutive_failures", 2).Error; err != nil {
			t.Fatal(err)
		}

		relayTestRequest(t, newTestRelay(store), token)
		attempts := relayAttempts(t, store)
		if len(attempts) != 2 || attempts[1].SelectionReason != SelectionReasonCircuitOpened || attempts[1].SelectionDetail == "" || attempts[1].PreviousChannelID != attempts[0].ChannelID {
			t.Fatalf("attempts = %+v", attempts)
		}
	})
}

func successfulResponseServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"resp_retry","status":"completed","usage":{"input_tokens":5,"output_tokens":1}}`))
	}))
}

func relayTestRequest(t *testing.T, relay *RelayService, token *ClientToken) {
	t.Helper()
	body := []byte(`{"model":"public-model","input":"hello"}`)
	payload, err := ParseRelayPayload(body)
	if err != nil {
		t.Fatal(err)
	}
	if publicErr := relay.Relay(context.Background(), httptest.NewRecorder(), nil, "", "responses", token, payload, body); publicErr != nil {
		t.Fatal(publicErr)
	}
}

func relayAttempts(t *testing.T, store *Store) []RelayAttemptLog {
	t.Helper()
	var attempts []RelayAttemptLog
	if err := store.db.Order("id ASC").Find(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	return attempts
}

func TestRelayMovesCodexSessionAffinityAfterRetryableFailure(t *testing.T) {
	store := newTestStore(t)
	var firstCalls atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		firstCalls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"error":{"message":"temporary","type":"api_error","code":"temporary"}}`))
	}))
	defer first.Close()
	var secondCalls atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		secondCalls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"resp_session","object":"response","status":"completed","usage":{"input_tokens":10,"output_tokens":2,"input_tokens_details":{"cached_tokens":4}}}`))
	}))
	defer second.Close()
	token, model, _, mappings := createRouteFixture(t, store, RoutingPriorityWeighted, first.URL, second.URL)
	relay := newTestRelay(store)

	relayRequest := func(sessionKey string) {
		body := []byte(fmt.Sprintf(`{"model":"public-model","prompt_cache_key":%q,"input":"hello"}`, sessionKey))
		payload, err := ParseRelayPayload(body)
		if err != nil {
			t.Fatal(err)
		}
		if publicErr := relay.Relay(context.Background(), httptest.NewRecorder(), http.Header{}, "", "responses", token, payload, body); publicErr != nil {
			t.Fatal(publicErr)
		}
	}

	relayRequest("codex-session-a")
	relayRequest("codex-session-a")
	if firstCalls.Load() != 1 || secondCalls.Load() != 2 {
		t.Fatalf("same-session calls = %d/%d, want 1/2", firstCalls.Load(), secondCalls.Load())
	}
	var affinity SessionAffinity
	if err := store.db.Where("token_id = ? AND model_id = ? AND session_hash = ?", token.ID, model.ID, hashSecret("codex-session-a")).First(&affinity).Error; err != nil {
		t.Fatal(err)
	}
	if affinity.ChannelModelID != mappings[1].ID {
		t.Fatalf("affinity mapping = %d, want %d", affinity.ChannelModelID, mappings[1].ID)
	}

	relayRequest("codex-session-b")
	if firstCalls.Load() != 2 || secondCalls.Load() != 3 {
		t.Fatalf("different-session calls = %d/%d, want 2/3", firstCalls.Load(), secondCalls.Load())
	}
}

func TestRelayKeepsCodexSessionAffinityUntilOriginalChannelIsUnavailable(t *testing.T) {
	store := newTestStore(t)
	var firstCalls atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		call := firstCalls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		if call == 1 {
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(`{"error":{"message":"temporary","type":"api_error","code":"temporary"}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"id":"resp_original","object":"response","status":"completed","usage":{"input_tokens":4,"output_tokens":1}}`))
	}))
	defer first.Close()
	var secondCalls atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		secondCalls.Add(1)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"resp_fallback","object":"response","status":"completed","usage":{"input_tokens":4,"output_tokens":1}}`))
	}))
	defer second.Close()
	token, model, _, mappings := createRouteFixture(t, store, RoutingPriorityWeighted, first.URL, second.URL)
	router := NewRouter(store, NewClientAccessService(store))
	router.RecordSessionAffinity(context.Background(), token.ID, model.ID, "codex-session-sticky", mappings[0].ID)
	relay := NewRelayService(store, router, NewTokenEstimator(), &config.ApplicationConfigManager{})

	for range 2 {
		body := []byte(`{"model":"public-model","prompt_cache_key":"codex-session-sticky","input":"hello"}`)
		payload, err := ParseRelayPayload(body)
		if err != nil {
			t.Fatal(err)
		}
		if publicErr := relay.Relay(context.Background(), httptest.NewRecorder(), http.Header{}, "", "responses", token, payload, body); publicErr != nil {
			t.Fatal(publicErr)
		}
	}
	if firstCalls.Load() != 2 || secondCalls.Load() != 1 {
		t.Fatalf("calls = %d/%d, want 2/1", firstCalls.Load(), secondCalls.Load())
	}
	var affinity SessionAffinity
	if err := store.db.Where("token_id = ? AND model_id = ? AND session_hash = ?", token.ID, model.ID, hashSecret("codex-session-sticky")).First(&affinity).Error; err != nil {
		t.Fatal(err)
	}
	if affinity.ChannelModelID != mappings[0].ID {
		t.Fatalf("affinity mapping = %d, want original %d", affinity.ChannelModelID, mappings[0].ID)
	}
}

func TestRelayStopsAfterClientCancellationWithoutPenalizingChannels(t *testing.T) {
	store := newTestStore(t)
	started := make(chan struct{})
	releaseUpstream := make(chan struct{})
	first := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		close(started)
		<-releaseUpstream
	}))
	defer first.Close()
	defer close(releaseUpstream)
	var secondCalls atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		secondCalls.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	defer second.Close()
	token, _, channels, _ := createRouteFixture(t, store, RoutingPriorityWeighted, first.URL, second.URL)
	body := []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`)
	payload, err := ParseRelayPayload(body)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *PublicError, 1)
	go func() {
		done <- newTestRelay(store).Relay(ctx, httptest.NewRecorder(), http.Header{}, "", "chat", token, payload, body)
	}()
	<-started
	cancel()
	publicErr := <-done
	if publicErr == nil || publicErr.Status != statusClientClosedRequest || publicErr.Code != "request_canceled" {
		t.Fatalf("Relay() error = %+v", publicErr)
	}
	if secondCalls.Load() != 0 {
		t.Fatalf("second channel calls = %d, want 0", secondCalls.Load())
	}
	for _, fixture := range channels {
		var channel Channel
		if err := store.db.First(&channel, fixture.ID).Error; err != nil {
			t.Fatal(err)
		}
		if channel.ConsecutiveFailures != 0 {
			t.Fatalf("channel %d failures = %d", channel.ID, channel.ConsecutiveFailures)
		}
	}
	var requestLog RelayRequestLog
	if err := store.db.First(&requestLog).Error; err != nil {
		t.Fatal(err)
	}
	if requestLog.StatusCode != statusClientClosedRequest || requestLog.AttemptCount != 1 || requestLog.ErrorCode != "request_canceled" {
		t.Fatalf("request log = %+v", requestLog)
	}
}

func TestSSEClientDisconnectDoesNotOpenCircuit(t *testing.T) {
	store := newTestStore(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: {\"id\":\"chatcmpl_disconnect\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		writer.(http.Flusher).Flush()
	}))
	defer upstream.Close()
	token, _, channels, _ := createRouteFixture(t, store, RoutingPriorityWeighted, upstream.URL)
	body := []byte(`{"model":"public-model","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	payload, err := ParseRelayPayload(body)
	if err != nil {
		t.Fatal(err)
	}
	writer := &disconnectedStreamWriter{header: make(http.Header)}
	if publicErr := newTestRelay(store).Relay(context.Background(), writer, http.Header{}, "", "chat", token, payload, body); publicErr != nil {
		t.Fatal(publicErr)
	}
	var channel Channel
	if err := store.db.First(&channel, channels[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if channel.ConsecutiveFailures != 0 || channel.CircuitOpenUntil != nil {
		t.Fatalf("channel health = %+v", channel)
	}
	var requestLog RelayRequestLog
	if err := store.db.First(&requestLog).Error; err != nil {
		t.Fatal(err)
	}
	if requestLog.StatusCode != statusClientClosedRequest || requestLog.ErrorCode != "request_canceled" {
		t.Fatalf("request log = %+v", requestLog)
	}
}

func TestRelayRetriesBeforeFirstSSEEvent(t *testing.T) {
	store := newTestStore(t)
	first := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		writer.(http.Flusher).Flush()
	}))
	defer first.Close()
	var secondCalls atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		secondCalls.Add(1)
		writer.Header().Set("Content-Type", "text/event-stream")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("data: {\"id\":\"chatcmpl_2\",\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		_, _ = writer.Write([]byte("data: {\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1}}\n\n"))
		_, _ = writer.Write([]byte("data: [DONE]\n\n"))
	}))
	defer second.Close()
	token, _, _, _ := createRouteFixture(t, store, RoutingPriorityWeighted, first.URL, second.URL)
	body := []byte(`{"model":"public-model","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
	payload, err := ParseRelayPayload(body)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	if publicErr := newTestRelay(store).Relay(context.Background(), recorder, http.Header{}, "", "chat", token, payload, body); publicErr != nil {
		t.Fatal(publicErr)
	}
	if secondCalls.Load() != 1 || !strings.Contains(recorder.Body.String(), "[DONE]") {
		t.Fatalf("second calls=%d body=%s", secondCalls.Load(), recorder.Body.String())
	}
	var attempts int64
	if err := store.db.Model(&RelayAttemptLog{}).Count(&attempts).Error; err != nil || attempts != 2 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}

func TestAdminSessionIsHashedAndPasswordChangeRevokesIt(t *testing.T) {
	store := newTestStore(t)
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("initial-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := AdminUser{Username: "admin", PasswordHash: string(passwordHash), Enabled: true}
	if err := store.db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	auth := NewAdminAuthService(store, &config.ApplicationConfigManager{})
	rawToken, _, err := auth.Login("admin", "initial-password")
	if err != nil {
		t.Fatal(err)
	}
	var session AdminSession
	if err := store.db.First(&session).Error; err != nil {
		t.Fatal(err)
	}
	if session.TokenHash == rawToken || session.TokenHash != hashSecret(rawToken) {
		t.Fatal("administrator session was not stored as a hash")
	}
	if _, err := auth.Authenticate(rawToken); err != nil {
		t.Fatal(err)
	}
	if err := auth.ChangePassword(user.ID, "initial-password", "replacement-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Authenticate(rawToken); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("Authenticate() error = %v, want invalid session", err)
	}
}
