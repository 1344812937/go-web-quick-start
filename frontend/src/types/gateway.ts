export type RoutingStrategy = 'priority_weighted' | 'lowest_cost' | 'lowest_latency'
export type CostSource = 'upstream' | 'estimated_fallback' | 'mixed' | 'failed_zero'

export interface AdminSession {
  /** Persistent administrator identifier. */
  id: number
  /** Administrator login name displayed in the console. */
  username: string
}

export interface ChannelModel {
  /** Persistent channel-model mapping identifier. */
  id: number
  /** Channel owning this mapping. */
  channelId: number
  /** Public model exposed by the gateway. */
  modelId: number
  /** Model identifier sent to this upstream provider. */
  upstreamModel: string
  /** Higher values are attempted before lower priority groups. */
  priority: number
  /** Relative selection weight inside the same priority group. */
  weight: number
  /** Input price in micro-USD per one million tokens. */
  inputPriceMicros: number
  /** Output price in micro-USD per one million tokens. */
  outputPriceMicros: number
  /** Cached input price in micro-USD per one million tokens, or null to use input price. */
  cachedInputPriceMicros: number | null
  /** Cache-write input price in micro-USD per one million tokens, or null to use input price. */
  cacheWritePriceMicros: number | null
  /** Persisted price multiplier in basis points; 10000 represents 1.00x. */
  priceMultiplierBasisPoints: number
  /** Whether this mapping can receive new requests. */
  enabled: boolean
  /** Mapping creation timestamp in RFC 3339 format. */
  createdAt: string
  /** Mapping update timestamp in RFC 3339 format. */
  updatedAt: string
}

export interface ChannelLatencyPoint {
  /** Timestamp of the successful upstream attempt in RFC 3339 format. */
  recordedAt: string
  /** Time to receive the upstream response headers in milliseconds. */
  latencyMs: number
}

export interface ChannelMetrics {
  /** Chronological latency points for the latest 48 successful attempts within five days. */
  latencySeries: ChannelLatencyPoint[]
  /** Most recent successful upstream latency in milliseconds, or zero without a sample. */
  latestLatencyMs: number
  /** Total successful attempts with a positive latency sample within five days. */
  latencySampleCount: number
  /** Upstream-reported input tokens from successful attempts within five days. */
  inputTokens: number
  /** Upstream-reported cached input tokens included in inputTokens within five days. */
  cachedTokens: number
  /** cachedTokens divided by inputTokens, or zero when no input usage is known. */
  cacheHitRate: number
}

export interface Channel {
  /** Persistent channel identifier. */
  id: number
  /** Administrator-facing provider name. */
  name: string
  /** OpenAI-compatible upstream base URL, normally ending in /v1. */
  baseUrl: string
  /** Whether new requests may be routed to the channel. */
  enabled: boolean
  /** Whether Chat streaming requests may include stream_options.include_usage. */
  supportsStreamUsage: boolean
  /** Channel-wide official-price multiplier in basis points; 10000 represents 1.00x. */
  priceMultiplierBasisPoints: number
  /** Number of consecutive retryable failures. */
  consecutiveFailures: number
  /** Circuit reopening timestamp, or null when the circuit is closed. */
  circuitOpenUntil: string | null
  /** Successful request latency EWMA in milliseconds. */
  latencyEwmaMs: number
  /** Last health observation timestamp. */
  lastHealthAt: string | null
  /** Last retryable upstream error without request content. */
  lastError: string
  /** Whether an encrypted upstream API key is stored. */
  apiKeyConfigured: boolean
  /** Public-to-upstream model mappings configured for the channel. */
  models: ChannelModel[]
  /** Recent performance and cache metrics derived from five-day detailed attempt logs. */
  metrics: ChannelMetrics
  /** Channel creation timestamp in RFC 3339 format. */
  createdAt: string
  /** Channel update timestamp in RFC 3339 format. */
  updatedAt: string
}

export interface ChannelModelDiscoveryRequest {
  /** Existing channel identifier, or zero when testing an unsaved channel. */
  channelId: number
  /** OpenAI-compatible base URL currently entered in the channel form. */
  baseUrl: string
  /** Replacement or unsaved upstream key; blank reuses the stored encrypted key. */
  apiKey: string
}

export interface OfficialModelPrice {
  /** Official regular-input price in micro-USD per one million tokens. */
  inputPriceMicros: number
  /** Official output price in micro-USD per one million tokens. */
  outputPriceMicros: number
  /** Official cached-input price in micro-USD per one million tokens. */
  cachedInputPriceMicros: number | null
  /** Official cache-write price in micro-USD per one million tokens, or null when the catalog shows no price. */
  cacheWritePriceMicros: number | null
  /** Official catalog source URL embedded by this gateway build. */
  source: string
  /** ISO currency code used by every catalog price. */
  currency: 'USD'
  /** Billing unit used by every catalog price. */
  unit: 'per_1m_tokens'
  /** OpenAI processing and context tier represented by this price. */
  contextTier: 'standard_short_context'
  /** Immutable version identifier for the embedded price catalog. */
  catalogVersion: string
  /** Catalog review date in YYYY-MM-DD format. */
  updatedAt: string
}

export interface UpstreamModel {
  /** Model identifier returned by the upstream /models endpoint. */
  id: string
  /** Upstream owner label, or an empty string when omitted by the provider. */
  ownedBy: string
  /** Upstream Unix creation timestamp, or zero when omitted by the provider. */
  created: number
  /** Public model identifier registered for this upstream model. */
  publicModelId: number
  /** Whether the public model was automatically created by this discovery request. */
  publicModelCreated: boolean
  /** Exact-match OpenAI Standard short-context text-token price, or null for an unlisted model. */
  officialPrice: OfficialModelPrice | null
}

export interface ChannelModelDiscovery {
  /** Deduplicated upstream models sorted by model identifier. */
  models: UpstreamModel[]
  /** Time to receive the upstream response in milliseconds. */
  latencyMs: number
  /** HTTP status returned by the upstream models endpoint. */
  status: number
  /** Completion timestamp of this discovery request in RFC 3339 format. */
  fetchedAt: string
}

export interface GatewayModel {
  /** Persistent public model identifier. */
  id: number
  /** Model name accepted by public OpenAI-compatible endpoints. */
  name: string
  /** Candidate ordering strategy used for this model. */
  routingStrategy: RoutingStrategy
  /** Whether the model is listed and accepts requests. */
  enabled: boolean
  /** Model creation timestamp in RFC 3339 format. */
  createdAt: string
  /** Model update timestamp in RFC 3339 format. */
  updatedAt: string
}

export interface ClientToken {
  /** Persistent client token identifier. */
  id: number
  /** Administrator-facing token name. */
  name: string
  /** Redacted prefix used to identify the token after issuance. */
  keyPrefix: string
  /** Whether the token may authenticate public API requests. */
  enabled: boolean
  /** Whether every enabled public model is allowed. */
  allowAllModels: boolean
  /** Maximum accepted requests in each fixed one-minute window. */
  rpm: number
  /** Maximum concurrent in-flight requests. */
  maxConcurrency: number
  /** Last successful authentication timestamp, or null when unused. */
  lastUsedAt: string | null
  /** Explicit model permissions when allowAllModels is false. */
  modelIds: number[]
  /** Long-term daily statistics retained after detailed logs expire. */
  statistics: TokenStatistics
  /** Token creation timestamp in RFC 3339 format. */
  createdAt: string
  /** Token update timestamp in RFC 3339 format. */
  updatedAt: string
}

export interface TokenStatistics {
  /** Total requests recorded for this logical client token. */
  requests: number
  /** Requests completed with a final 2xx status. */
  successes: number
  /** Total reported or estimated input tokens. */
  inputTokens: number
  /** Non-cached input tokens billed at the regular input price. */
  normalInputTokens: number
  /** Total reported or estimated output tokens. */
  outputTokens: number
  /** Cached input tokens included in inputTokens. */
  cachedTokens: number
  /** Cache-write input tokens included in inputTokens. */
  cacheWriteTokens: number
  /** Gateway-local token count of request bodies actually sent upstream, including retries. */
  sentTokens: number
  /** Total estimated cost in micro-USD. */
  estimatedCostMicros: number
  /** Total upstream-reported cost in micro-USD, with estimate fallback when the upstream omits cost. */
  upstreamCostMicros: number
  /** Average end-to-end request latency in milliseconds. */
  averageLatencyMs: number
  /** Total upstream attempts across requests. */
  attempts: number
}

export interface IssuedClientToken {
  /** Persisted token metadata. */
  token: ClientToken
  /** One-time plaintext sk- token that cannot be retrieved later. */
  secret: string
}

export interface DashboardDaily {
  /** UTC calendar date in YYYY-MM-DD format. */
  date: string
  /** Requests received on the date. */
  requests: number
  /** Requests completed with a 2xx status. */
  successes: number
  /** Input tokens reported or estimated on the date. */
  inputTokens: number
  /** Output tokens reported or estimated on the date. */
  outputTokens: number
  /** Estimated cost in micro-USD. */
  estimatedCostMicros: number
  /** Upstream cost in micro-USD, with estimate fallback when the upstream omits cost. */
  upstreamCostMicros: number
}

export interface DashboardBreakdown {
  /** Channel or public model label. */
  name: string
  /** Requests represented by this row. */
  requests: number
  /** Estimated cost in micro-USD. */
  estimatedCostMicros: number
  /** Upstream cost in micro-USD, with estimate fallback when the upstream omits cost. */
  upstreamCostMicros: number
}

export interface DashboardSummary {
  /** Total requests across long-term per-token statistics. */
  requests: number
  /** Fraction from 0 to 1 completed with a 2xx status. */
  successRate: number
  /** Total input tokens across retained logs. */
  inputTokens: number
  /** Total output tokens across retained logs. */
  outputTokens: number
  /** Total estimated cost in micro-USD. */
  estimatedCostMicros: number
  /** Primary total cost in micro-USD, reported by upstream or estimated when absent. */
  upstreamCostMicros: number
  /** Mean end-to-end request latency in milliseconds. */
  averageLatencyMs: number
  /** Daily metrics for the most recent 14 days. */
  daily: DashboardDaily[]
  /** Highest-usage channel breakdown from five-day detailed logs. */
  channels: DashboardBreakdown[]
  /** Highest-usage public model breakdown from five-day detailed logs. */
  models: DashboardBreakdown[]
}

export type AttemptSelectionReason =
  | 'initial_route'
  | 'response_affinity'
  | 'session_affinity'
  | 'channel_disabled'
  | 'mapping_disabled'
  | 'circuit_open'
  | 'affinity_target_missing'
  | 'retryable_status'
  | 'transport_error'
  | 'response_error'
  | 'gateway_preparation_error'
  | 'circuit_opened'
  | ''

export interface RelayAttemptLog {
  /** Persistent attempt identifier. */
  id: number
  /** Parent public request identifier. */
  requestId: string
  /** Selected channel identifier. */
  channelId: number
  /** Channel name captured when the attempt was made. */
  channelName: string
  /** OpenAI-compatible channel base URL captured when the attempt was made. */
  channelBaseUrl: string
  /** Selected channel-model mapping identifier. */
  channelModelId: number
  /** Model identifier sent upstream. */
  upstreamModel: string
  /** Channel identifier used immediately before this selection, or zero for an initial route. */
  previousChannelId: number
  /** Historical name of the channel used immediately before this selection. */
  previousChannelName: string
  /** Stable reason code describing why this channel was selected; blank on legacy rows. */
  selectionReason: AttemptSelectionReason
  /** Sanitized, bounded diagnostic detail for the selection reason. */
  selectionDetail: string
  /** Upstream HTTP status, or zero for a transport error. */
  statusCode: number
  /** Input tokens charged or estimated for this attempt. */
  inputTokens: number
  /** Non-cached input tokens charged or estimated for this attempt. */
  normalInputTokens: number
  /** Output tokens charged or estimated for this attempt. */
  outputTokens: number
  /** Cached input tokens within inputTokens. */
  cachedTokens: number
  /** Cache-write input tokens within inputTokens. */
  cacheWriteTokens: number
  /** Gateway-local token count of the request body sent for this network attempt. */
  sentTokens: number
  /** Attempt estimated cost in micro-USD. */
  estimatedCostMicros: number
  /** Attempt upstream cost in micro-USD, falling back to estimated cost only for successful attempts. */
  upstreamCostMicros: number
  /** Monetary origin; failed attempts are always failed_zero. */
  costSource: CostSource
  /** upstream, estimated_tiktoken, mixed, or empty when unknown. */
  usageSource: string
  /** Time to upstream response headers in milliseconds. */
  latencyMs: number
  /** Whether the attempt completed successfully. */
  success: boolean
  /** Sanitized transport or status failure detail. */
  errorMessage: string
  /** Attempt creation timestamp in RFC 3339 format. */
  createdAt: string
}

export interface RelayRequestLog {
  /** Public request UUID also returned as X-Request-Id. */
  id: string
  /** Client token identifier used by the request. */
  tokenId: number
  /** Client token name captured when the request was received. */
  tokenName: string
  /** Redacted client token prefix captured when the request was received. */
  tokenKeyPrefix: string
  /** chat or responses public endpoint family. */
  endpoint: string
  /** Public model requested by the client. */
  requestedModel: string
  /** Codex client session identifier when one could be extracted. */
  codexSessionId: string
  /** Payload field used to identify the Codex session, or unavailable. */
  codexSessionSource: string
  /** Allowlisted non-content API parameters retained for five-day diagnostics. */
  requestParameters: Record<string, unknown>
  /** Final HTTP status returned to the client. */
  statusCode: number
  /** Total input tokens across known attempts. */
  inputTokens: number
  /** Total non-cached input tokens across known attempts. */
  normalInputTokens: number
  /** Total output tokens across known attempts. */
  outputTokens: number
  /** Total cached input tokens across known attempts. */
  cachedTokens: number
  /** Total cache-write input tokens across known attempts. */
  cacheWriteTokens: number
  /** Gateway-local token count of request bodies actually sent upstream, including retries. */
  sentTokens: number
  /** Total estimated cost in micro-USD across known attempts. */
  estimatedCostMicros: number
  /** Total upstream cost in micro-USD across successful attempts, with per-attempt estimate fallback. */
  upstreamCostMicros: number
  /** Aggregate monetary origin across successful attempts, or failed_zero for a failed request. */
  costSource: CostSource
  /** upstream, estimated_tiktoken, mixed, or empty when unknown. */
  usageSource: string
  /** Number of upstream attempts made. */
  attemptCount: number
  /** End-to-end request duration in milliseconds. */
  durationMs: number
  /** Whether the client requested an SSE response. */
  stream: boolean
  /** Stable gateway or upstream error code. */
  errorCode: string
  /** Request creation timestamp in RFC 3339 format. */
  createdAt: string
  /** Ordered upstream attempts for this request. */
  attempts: RelayAttemptLog[]
}

export interface LogPage {
  /** Request logs for the selected page. */
  items: RelayRequestLog[]
  /** Total matching request count. */
  total: number
  /** One-based page number. */
  page: number
  /** Maximum rows returned in this page. */
  pageSize: number
}

export interface SessionChannel {
  /** Persistent channel identifier. */
  channelId: number
  /** Current or historically captured channel name. */
  channelName: string
  /** Current or historically captured OpenAI-compatible base URL. */
  channelBaseUrl: string
  /** Channel-model mapping assigned to the session. */
  channelModelId: number
  /** Model identifier sent to the assigned upstream provider. */
  upstreamModel: string
  /** session_affinity, latest_successful_attempt, or latest_attempt. */
  assignmentSource: string
  /** Whether the live channel remains enabled. */
  enabled: boolean
  /** Whether the live channel-model mapping remains enabled. */
  mappingEnabled: boolean
  /** Circuit reopening timestamp, or null when the circuit is closed. */
  circuitOpenUntil: string | null
  /** Last assignment or attempt timestamp in RFC 3339 format. */
  lastUsedAt: string
}

export interface CodexSessionSummary {
  /** Extracted Codex session identifier, blank for an unidentified request. */
  sessionId: string
  /** Payload field used to identify the session, or unavailable. */
  sessionSource: string
  /** Whether multiple requests can be reliably grouped into this session. */
  identified: boolean
  /** Request identifier used as the conservative group key when no session ID exists. */
  fallbackRequestId: string
  /** Client token identifier used by the session. */
  tokenId: number
  /** Client token name captured by the latest request. */
  tokenName: string
  /** Redacted client token prefix captured by the latest request. */
  tokenKeyPrefix: string
  /** Public model used by the latest request. */
  latestModel: string
  /** chat or responses endpoint used by the latest request. */
  latestEndpoint: string
  /** Requests retained for this session within the five-day window. */
  requestCount: number
  /** Requests completed with a 2xx status. */
  successCount: number
  /** Fraction from 0 to 1 of retained requests completed successfully. */
  successRate: number
  /** Total upstream attempts made by retained requests. */
  attemptCount: number
  /** Total input tokens across retained requests. */
  inputTokens: number
  /** Total non-cached input tokens across retained requests. */
  normalInputTokens: number
  /** Total output tokens across retained requests. */
  outputTokens: number
  /** Cached input tokens included in inputTokens. */
  cachedTokens: number
  /** Cache-write input tokens included in inputTokens. */
  cacheWriteTokens: number
  /** Gateway-local token count of request bodies actually sent upstream, including retries. */
  sentTokens: number
  /** cachedTokens divided by inputTokens, or zero without input usage. */
  cacheHitRate: number
  /** Total estimated cost in micro-USD. */
  estimatedCostMicros: number
  /** Total upstream cost in micro-USD, with estimate fallback when absent. */
  upstreamCostMicros: number
  /** Mean end-to-end request latency in milliseconds. */
  averageDurationMs: number
  /** Earliest retained request timestamp in RFC 3339 format. */
  firstSeenAt: string
  /** Latest retained request timestamp in RFC 3339 format. */
  lastSeenAt: string
  /** Current affinity assignment or latest historical channel, when available. */
  currentChannel: SessionChannel | null
}

export interface CodexSessionPage {
  /** Session aggregates for the selected page. */
  items: CodexSessionSummary[]
  /** Total matching session groups. */
  total: number
  /** One-based page number. */
  page: number
  /** Maximum session groups returned on this page. */
  pageSize: number
}

export interface CodexSessionDetail {
  /** Full five-day aggregate for the selected session. */
  summary: CodexSessionSummary
  /** Retained request details for the selected detail page. */
  requests: RelayRequestLog[]
  /** Total retained request count for detail pagination. */
  requestTotal: number
  /** One-based request detail page number. */
  page: number
  /** Maximum request details returned on this page. */
  pageSize: number
}

export interface ApplicationSettings {
  /** HTTP listener configuration. */
  webConfig: { host: string; port: string }
  /** Legacy node configuration preserved when settings are saved. */
  nodeConfig: { sharedToken: string }
  /** Runtime gateway limits and retention configuration. */
  gatewayConfig: {
    maxAttempts: number
    requestBodyLimitMB: number
    responseHeaderTimeoutSeconds: number
    streamIdleTimeoutSeconds: number
    sessionTTLHours: number
    secureCookie: boolean
  }
}
