<script setup lang="ts">
import { computed } from 'vue'
import { ChatDotRound, Setting, Tools, User } from '@element-plus/icons-vue'
import MarkdownIt from 'markdown-it'
import type { ConversationMessage } from '@/utils/conversation'

interface ChatTranscriptProps {
  /** Ordered user, system, tool, and assistant messages to render. */
  messages: ConversationMessage[]
}

const { messages } = defineProps<ChatTranscriptProps>()
const markdown = new MarkdownIt({ html: false, breaks: true, linkify: true, typographer: false })
markdown.renderer.rules.link_open = (tokens, index, options, _env, self) => {
  tokens[index].attrSet('target', '_blank')
  tokens[index].attrSet('rel', 'noreferrer noopener')
  return self.renderToken(tokens, index, options)
}

const renderedMessages = computed(() => messages.map((message) => ({
  ...message,
  html: markdown.render(message.content),
})))

function roleIcon(role: ConversationMessage['role']) {
  if (role === 'user') return User
  if (role === 'tool') return Tools
  if (role === 'system' || role === 'developer') return Setting
  return ChatDotRound
}
</script>

<template>
  <div v-if="renderedMessages.length" class="chat-transcript">
    <article v-for="(message, index) in renderedMessages" :key="`${message.id}-${index}`" class="chat-message" :class="`is-${message.role}`">
      <header><el-icon><component :is="roleIcon(message.role)" /></el-icon><strong>{{ message.label }}</strong></header>
      <div class="markdown-body" v-html="message.html" />
    </article>
  </div>
  <div v-else class="chat-empty">正文中没有可解析的对话消息，可切换到源码查看完整内容</div>
</template>

<style scoped>
.chat-transcript { display: grid; border-block: 1px solid var(--rose-border); }
.chat-message { display: grid; grid-template-columns: 104px minmax(0, 1fr); gap: 18px; padding: 18px 14px; background: var(--rose-surface); }
.chat-message + .chat-message { border-top: 1px solid var(--rose-border); }
.chat-message.is-assistant { background: var(--rose-surface-muted); }
.chat-message.is-error { border-left: 3px solid var(--rose-danger); background: var(--rose-danger-soft); }
.chat-message header { display: flex; align-items: center; align-self: start; gap: 7px; color: var(--rose-text-muted); font-size: 12px; }
.chat-message header .el-icon { color: var(--rose-primary); font-size: 15px; }
.chat-message.is-error header, .chat-message.is-error header .el-icon { color: var(--rose-danger); }
.markdown-body { min-width: 0; color: var(--rose-text); font-size: 13px; line-height: 1.72; overflow-wrap: anywhere; }
.markdown-body :deep(> :first-child) { margin-top: 0; }
.markdown-body :deep(> :last-child) { margin-bottom: 0; }
.markdown-body :deep(p), .markdown-body :deep(ul), .markdown-body :deep(ol), .markdown-body :deep(blockquote), .markdown-body :deep(pre) { margin: 0 0 10px; }
.markdown-body :deep(ul), .markdown-body :deep(ol) { padding-left: 22px; }
.markdown-body :deep(blockquote) { padding: 6px 12px; border-left: 3px solid var(--rose-border-strong); color: var(--rose-text-muted); }
.markdown-body :deep(code) { padding: 2px 5px; border: 1px solid var(--rose-border); background: var(--rose-surface); font-family: var(--rose-font-mono); font-size: .92em; }
.markdown-body :deep(pre) { max-width: 100%; padding: 12px 14px; overflow: auto; border: 1px solid var(--rose-border); background: var(--rose-surface-muted); color: var(--rose-text); }
.markdown-body :deep(pre code) { padding: 0; border: 0; color: inherit; background: transparent; }
.markdown-body :deep(table) { width: 100%; border-collapse: collapse; }
.markdown-body :deep(th), .markdown-body :deep(td) { padding: 7px 9px; border: 1px solid var(--rose-border); text-align: left; }
.markdown-body :deep(a) { color: var(--rose-primary-hover); }
.chat-empty { padding: 44px 16px; color: var(--rose-text-muted); text-align: center; }
@media (max-width: 640px) { .chat-message { grid-template-columns: 1fr; gap: 10px; padding: 15px 10px; } }
</style>
