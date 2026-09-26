<script setup lang="ts">
import { NAlert, NButton, NInput, NPopconfirm, useMessage } from 'naive-ui'
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api, ApiError } from '@/api/client'
import { fieldErrors, ticketReplyForm, ticketViewSchema, type TicketView } from '@/api/schemas'
import { formatDateTime, resolveMessage, useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const { t, locale } = useI18n()
const message = useMessage()
const route = useRoute()
const auth = useAuthStore()

const view = ref<TicketView | null>(null)
const loadError = ref('')
const missing = ref(false)
const id = computed(() => Number(route.params.id))

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

async function load() {
  try {
    view.value = (await api(`/api/tickets/${id.value}`, { schema: ticketViewSchema })).ticket
    loadError.value = ''
    missing.value = false
  } catch (e) {
    view.value = null
    if (e instanceof ApiError && e.status === 404) missing.value = true
    else loadError.value = errText(e, t('support.loadOneFailed'))
  }
}
onMounted(load)
watch(id, load)

// Ключи перечислены явно: сторож i18n ищет их в коде как строки.
const statusLabel = (s: string) => (s === 'open' ? t('support.status.open') : s === 'answered' ? t('support.status.answered') : t('support.status.closed'))
const categoryLabel = (c: string) =>
  c === 'general'
    ? t('support.categories.general')
    : c === 'sites'
      ? t('support.categories.sites')
      : c === 'domains'
        ? t('support.categories.domains')
        : c === 'mail'
          ? t('support.categories.mail')
          : c === 'dns'
            ? t('support.categories.dns')
            : c === 'databases'
              ? t('support.categories.databases')
              : t('support.categories.other')

const text = ref('')
const errors = ref<Record<string, string>>({})
const sending = ref(false)

async function reply() {
  const parsed = ticketReplyForm.safeParse({ message: text.value })
  errors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  sending.value = true
  try {
    view.value = (await api(`/api/tickets/${id.value}/messages`, { method: 'POST', body: parsed.data, schema: ticketViewSchema })).ticket
    text.value = ''
    message.success(t('support.replySent'))
  } catch (e) {
    if (e instanceof ApiError && e.field) errors.value = { [e.field]: e.message }
    else message.error(errText(e, t('support.replyFailed')))
  } finally {
    sending.value = false
  }
}

async function changeState(action: 'close' | 'reopen') {
  try {
    view.value = (await api(`/api/tickets/${id.value}/${action}`, { method: 'POST', schema: ticketViewSchema })).ticket
  } catch (e) {
    message.error(errText(e, t('support.actionFailed')))
  }
}

const when = (iso: string) => formatDateTime(iso, locale.value)
const authorOf = (m: TicketView['thread'][number]) => (m.staff ? (auth.isAdmin && m.author ? `${t('support.staff')} · ${m.author}` : t('support.staff')) : m.author || t('support.you'))
</script>

<template>
  <div class="page">
    <router-link :to="{ name: 'support' }" class="back">← {{ t('support.back') }}</router-link>
    <n-alert v-if="loadError" type="error" :show-icon="false">{{ loadError }}</n-alert>
    <p v-if="missing" class="note" data-testid="ticket-missing">{{ t('support.notFound') }}</p>

    <template v-if="view">
      <header class="head rise">
        <h1>{{ t('support.ticketTitle', { id: view.id }) }}</h1>
        <h2 data-testid="ticket-subject">{{ view.subject }}</h2>
        <p class="meta">
          <span :class="['badge', view.status]" data-testid="ticket-status">{{ statusLabel(view.status) }}</span>
          <span class="note">{{ t('support.category_label', { name: categoryLabel(view.category) }) }}</span>
          <span v-if="auth.isAdmin" class="note">{{ t('support.from', { user: view.username }) }}<template v-if="view.email"> · {{ view.email }}</template></span>
        </p>
      </header>

      <ul class="thread" data-testid="ticket-thread">
        <li v-for="m in view.thread" :key="m.id" :class="['msg', m.staff ? 'staff' : 'mine']">
          <div class="who">
            <strong>{{ authorOf(m) }}</strong>
            <span class="note">{{ when(m.created_at) }}</span>
          </div>
          <p class="body">{{ m.body }}</p>
        </li>
      </ul>

      <section class="glass card rise">
        <p v-if="view.status === 'closed'" class="note">{{ t('support.closedNote') }}</p>
        <p v-if="!view.can_reply" class="note">{{ t('support.full') }}</p>
        <template v-else>
          <n-input
            v-model:value="text"
            type="textarea"
            :rows="5"
            :placeholder="t('support.replyPlaceholder')"
            :status="errors.message ? 'error' : undefined"
            :input-props="{ 'aria-label': t('support.reply') }"
            @update:value="errors.message = ''"
          />
          <p v-if="errors.message" class="err">{{ resolveMessage(errors.message) }}</p>
          <div class="actions">
            <n-button type="primary" :loading="sending" data-testid="ticket-reply" @click="reply">{{ t('support.reply') }}</n-button>
            <n-button v-if="view.status === 'closed'" class="tint-cyan" data-testid="ticket-reopen" @click="changeState('reopen')">{{ t('support.reopen') }}</n-button>
            <n-popconfirm v-else @positive-click="changeState('close')">
              <template #trigger>
                <n-button class="tint-rose" data-testid="ticket-close">{{ t('support.close') }}</n-button>
              </template>
              {{ t('support.closeConfirm') }}
            </n-popconfirm>
          </div>
        </template>
      </section>
    </template>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 16px;
  max-width: 860px;
}

.head h1 {
  font-size: clamp(24px, 3vw, 30px);
  font-weight: 800;
}

.head h2 {
  font-size: 18px;
  font-weight: 600;
  word-break: break-word;
}

.meta {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  align-items: baseline;
}

.note {
  color: var(--text-dim);
  font-size: 13.5px;
}

.back {
  font-size: 14px;
}

.badge {
  padding: 2px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
  background: rgba(148, 163, 184, 0.18);
}

.badge.open {
  background: rgba(251, 191, 36, 0.18);
  color: #fcd34d;
}

.badge.answered {
  background: rgba(52, 211, 153, 0.18);
  color: #6ee7b7;
}

.thread {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 12px;
}

.msg {
  padding: 12px 16px;
  border-radius: 14px;
  border: 1px solid var(--border);
}

.msg.staff {
  border-left: 4px solid #8b5cf6;
  background: rgba(139, 92, 246, 0.1);
}

.who {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  align-items: baseline;
  margin-bottom: 6px;
}

.body {
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.55;
}

.card {
  padding: 16px 20px 18px;
  display: grid;
  gap: 12px;
}

.actions {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}

.err {
  color: #fda4af;
  font-size: 13.5px;
}
</style>
