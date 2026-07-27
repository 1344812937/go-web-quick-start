<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import ChatTranscript from '@/components/ChatTranscript.vue'
import type { RelayAttemptLog, RelayRequestLog } from '@/types/gateway'
import { conversation, deltaDescription, requestConversation } from '@/utils/conversation'

interface RequestPayloadDialogProps {
  /** Request whose retained payloads should be inspected. */
  request: RelayRequestLog | null
}

type DetailMode = 'chat' | 'source'

const { request } = defineProps<RequestPayloadDialogProps>()
const open = defineModel<boolean>({ required: true })
const activeTab = ref('final')
const detailMode = ref<DetailMode>('chat')
const detailModeOptions: Array<{ label: string; value: DetailMode }> = [
  { label: '聊天', value: 'chat' },
  { label: '源码', value: 'source' },
]
const title = computed(() => request ? `调用详情 · ${request.id}` : '调用详情')
const originalMessages = computed(() => requestConversation(request?.requestBody ?? ''))
const finalMessages = computed(() => conversation(request?.requestBody ?? '', request?.responseBody ?? ''))

function attemptMessages(attempt: RelayAttemptLog) {
  return conversation(request?.requestBody ?? '', attempt.responseBody)
}

function formattedPayload(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) return ''
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2)
  } catch {
    return value
  }
}

watch(
  () => [open.value, request?.id],
  ([isOpen]) => {
    if (!isOpen) return
    activeTab.value = 'final'
    detailMode.value = 'chat'
  },
)
</script>

<template>
  <el-dialog v-model="open" :title="title" width="min(1120px, 96vw)" append-to-body destroy-on-close>
    <div v-if="request" class="payload-dialog">
      <div class="payload-toolbar">
        <div class="payload-meta">
          <span><strong>端点</strong>{{ request.endpoint === 'chat' ? 'Chat Completions' : 'Responses' }}</span>
          <span><strong>模型</strong><code>{{ request.requestedModel }}</code></span>
          <span><strong>状态</strong>{{ request.statusCode }}</span>
          <span><strong>尝试</strong>{{ request.attemptCount }}</span>
        </div>
        <el-segmented v-model="detailMode" :options="detailModeOptions" size="small" aria-label="详情展示方式" />
      </div>

      <el-tabs v-model="activeTab" class="payload-tabs">
        <el-tab-pane label="原始请求 / 增量上下文" name="original">
          <el-alert v-if="request.requestBodyTruncated" title="原始正文超过 4 MiB，留存内容已截断" type="warning" :closable="false" show-icon />
          <el-alert v-if="deltaDescription(request.requestBody)" :title="deltaDescription(request.requestBody)" type="info" :closable="false" show-icon />
          <ChatTranscript v-if="detailMode === 'chat'" :messages="originalMessages" />
          <pre v-else-if="request.requestBody" class="payload-code">{{ formattedPayload(request.requestBody) }}</pre>
          <div v-else class="payload-empty">没有留存原始请求正文</div>
        </el-tab-pane>

        <el-tab-pane v-for="(attempt, index) in request.attempts" :key="attempt.id" :label="`尝试 ${index + 1}`" :name="`attempt-${attempt.id}`">
          <div class="attempt-meta">
            <span><strong>渠道</strong>{{ attempt.channelName || `渠道 #${attempt.channelId}` }}</span>
            <span><strong>上游模型</strong><code>{{ attempt.upstreamModel }}</code></span>
            <span><strong>HTTP</strong>{{ attempt.statusCode || '网络错误' }}</span>
            <span><strong>结果</strong>{{ attempt.success ? '成功' : '失败' }}</span>
          </div>
          <template v-if="detailMode === 'chat'">
            <el-alert v-if="deltaDescription(request.requestBody)" :title="deltaDescription(request.requestBody)" type="info" :closable="false" show-icon />
            <ChatTranscript :messages="attemptMessages(attempt)" />
          </template>
          <template v-else>
            <section class="payload-section">
              <h3>发送到上游的请求</h3>
              <el-alert v-if="attempt.requestBodyTruncated" title="原始正文超过 4 MiB，留存内容已截断" type="warning" :closable="false" show-icon />
              <el-alert v-if="deltaDescription(attempt.requestBody)" :title="deltaDescription(attempt.requestBody)" type="info" :closable="false" show-icon />
              <pre v-if="attempt.requestBody" class="payload-code">{{ formattedPayload(attempt.requestBody) }}</pre>
              <div v-else class="payload-empty">本次尝试没有可展示的请求正文</div>
            </section>
            <section class="payload-section">
              <h3>上游返回</h3>
              <el-alert v-if="attempt.responseBodyTruncated" title="正文超过 4 MiB，当前内容已截断" type="warning" :closable="false" show-icon />
              <pre v-if="attempt.responseBody" class="payload-code">{{ formattedPayload(attempt.responseBody) }}</pre>
              <div v-else class="payload-empty">本次尝试没有收到响应正文</div>
            </section>
          </template>
        </el-tab-pane>

        <el-tab-pane label="最终响应" name="final">
          <el-alert v-if="request.responseBodyTruncated" title="正文超过 4 MiB，当前内容已截断" type="warning" :closable="false" show-icon />
          <el-alert v-if="detailMode === 'chat' && deltaDescription(request.requestBody)" :title="deltaDescription(request.requestBody)" type="info" :closable="false" show-icon />
          <ChatTranscript v-if="detailMode === 'chat'" :messages="finalMessages" />
          <pre v-else-if="request.responseBody" class="payload-code">{{ formattedPayload(request.responseBody) }}</pre>
          <div v-else class="payload-empty">没有留存最终响应正文</div>
        </el-tab-pane>
      </el-tabs>
    </div>
    <template #footer><el-button @click="open = false">关闭</el-button></template>
  </el-dialog>
</template>

<style scoped>
.payload-dialog { min-width: 0; }
.payload-toolbar { display: flex; align-items: flex-start; justify-content: space-between; gap: 18px; }
.payload-meta, .attempt-meta { display: flex; flex-wrap: wrap; gap: 10px 24px; padding-bottom: 14px; color: var(--rose-text-muted); font-size: 12px; }
.payload-meta span, .attempt-meta span { display: flex; align-items: baseline; gap: 7px; min-width: 0; }
.payload-meta strong, .attempt-meta strong { color: var(--rose-text); }
.payload-meta code, .attempt-meta code { overflow-wrap: anywhere; }
.payload-tabs { min-width: 0; }
.payload-tabs :deep(.el-alert) { margin-bottom: 12px; }
.payload-section + .payload-section { margin-top: 20px; padding-top: 18px; border-top: 1px solid var(--rose-border); }
.payload-section h3 { margin: 0 0 10px; color: var(--rose-text); font-size: 13px; }
.payload-code { max-height: 58vh; margin: 0; padding: 14px; overflow: auto; border: 1px solid var(--rose-border); background: var(--rose-surface-muted); color: var(--rose-text); font: 12px/1.6 var(--rose-font-mono); white-space: pre-wrap; overflow-wrap: anywhere; }
.payload-empty { padding: 40px 12px; color: var(--rose-text-muted); text-align: center; }
@media (max-width: 640px) { .payload-toolbar { align-items: stretch; flex-direction: column; } .payload-toolbar .el-segmented { align-self: flex-end; } }
</style>
