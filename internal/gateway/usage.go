package gateway

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"

	"github.com/tiktoken-go/tokenizer"
)

type TokenEstimator struct {
	codec tokenizer.Codec
}

func NewTokenEstimator() *TokenEstimator {
	codec, err := tokenizer.Get(tokenizer.O200kBase)
	if err != nil {
		panic(err)
	}
	return &TokenEstimator{codec: codec}
}

func (e *TokenEstimator) EstimateJSON(data []byte) int64 {
	if len(data) == 0 {
		return 0
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	text := ""
	if err := decoder.Decode(&value); err == nil {
		var builder strings.Builder
		collectText(value, &builder)
		text = builder.String()
	}
	if strings.TrimSpace(text) == "" {
		text = string(data)
	}
	ids, _, err := e.codec.Encode(text)
	if err != nil {
		return int64(math.Ceil(float64(len([]rune(text))) / 4))
	}
	return int64(len(ids))
}

func collectText(value any, builder *strings.Builder) {
	switch typed := value.(type) {
	case string:
		builder.WriteString(typed)
		builder.WriteByte('\n')
	case []any:
		for _, item := range typed {
			collectText(item, builder)
		}
	case map[string]any:
		for key, item := range typed {
			if key == "id" || key == "model" || key == "object" || key == "type" {
				continue
			}
			collectText(item, builder)
		}
	}
}

func ParseUsage(data []byte) (Usage, bool) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return Usage{}, false
	}
	usageMap := findUsageMap(value)
	if usageMap == nil {
		return Usage{}, false
	}
	usage := Usage{
		InputTokens:  firstInt(usageMap, "input_tokens", "prompt_tokens"),
		OutputTokens: firstInt(usageMap, "output_tokens", "completion_tokens"),
		Source:       "upstream",
	}
	for _, detailKey := range []string{"input_tokens_details", "prompt_tokens_details"} {
		if details, ok := usageMap[detailKey].(map[string]any); ok {
			usage.CachedTokens = firstInt(details, "cached_tokens")
			break
		}
	}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CachedTokens == 0 {
		return Usage{}, false
	}
	return usage, true
}

func findUsageMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		if usage, ok := typed["usage"].(map[string]any); ok {
			return usage
		}
		for _, nested := range typed {
			if found := findUsageMap(nested); found != nil {
				return found
			}
		}
	case []any:
		for _, nested := range typed {
			if found := findUsageMap(nested); found != nil {
				return found
			}
		}
	}
	return nil
}

func firstInt(values map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch value := values[key].(type) {
		case json.Number:
			parsed, _ := value.Int64()
			return parsed
		case float64:
			return int64(value)
		case int64:
			return value
		case int:
			return int64(value)
		}
	}
	return 0
}

func ResponseID(data []byte) string {
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return ""
	}
	if id, ok := value["id"].(string); ok {
		return id
	}
	if response, ok := value["response"].(map[string]any); ok {
		if id, ok := response["id"].(string); ok {
			return id
		}
	}
	return ""
}

func CalculateCostMicros(mapping ChannelModel, usage Usage) int64 {
	cached := usage.CachedTokens
	if cached < 0 {
		cached = 0
	}
	if cached > usage.InputTokens {
		cached = usage.InputTokens
	}
	normalInput := usage.InputTokens - cached
	cachedPrice := mapping.InputPriceMicros
	if mapping.CachedInputPriceMicros != nil {
		cachedPrice = *mapping.CachedInputPriceMicros
	}
	return tokenPrice(normalInput, mapping.InputPriceMicros) +
		tokenPrice(cached, cachedPrice) +
		tokenPrice(usage.OutputTokens, mapping.OutputPriceMicros)
}

func tokenPrice(tokens int64, priceMicrosPerMillion int64) int64 {
	if tokens <= 0 || priceMicrosPerMillion <= 0 {
		return 0
	}
	return (tokens*priceMicrosPerMillion + 999999) / 1000000
}
