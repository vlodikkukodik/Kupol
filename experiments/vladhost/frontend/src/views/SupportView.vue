<script setup lang="ts">
import { AddOutline, ChatbubblesOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NForm, NFormItem, NIcon, NInput, NSelect, NTabPane, NTabs, useMessage } from 'naive-ui'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError } from '@/api/client'
import { fieldErrors, ticketCreatedSchema, ticketForm, ticketListSchema, ticketQueueSchema, type TicketItem } from '@/api/schemas'
import { formatDateTime, resolveMessage, useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const { t, locale } = useI18n()
const message = useMessage()
const router = useRouter()
const auth = useAuthStore()

const own = ref<TicketItem[]>([])
const queue = ref<TicketItem[]>([])
const categories = ref<string[]>([])
const maxOpen = ref(5)
const loadError = ref('')
const tab = ref<'own' | 'all'>('own')

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)
const openCount = computed(() => own.value.filter((x) => x.status !== 'closed').length)

// Ключи перечислены явно: сторож i18n ищет их в коде как строки.
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
const statusLabel = (s: string) => (s === 'open' ? t('support.status.open') : s === 'answered' ? t('support.status.answered') : t('support.status.closed'))
const categoryOptions = computed(() => categories.value.map((c) => ({ label: categoryLabel(c), value: c })))
const statusOptions = computed(() => [
  { label: t('support.filterAll'), value: '' },
  { label: t('support.status.open'), value: 'open' },
  { label: t('support.status.answered'), value: 'answered' },
  { label: t('support.status.closed'), value: 'closed' },
])

async function loadOwn() {
  try {
    const r = await api('/api/tickets', { schema: ticketListSchema })
    own.value = r.tickets
    categories.value = r.categories
    maxOpen.value = r.max_open
    loadError.value = ''
  } catch (e) {
    loadError.value = errText(e, t('support.loadFailed'))
  }
}

const status = ref('')
const search = ref('')
let timer: ReturnType<typeof setTimeout> | undefined

async function loadQueue() {
  if (!auth.isAdmin) return
  try {
    const params = new URLSearchParams()
    if (status.value) params.set('status', status.value)
    if (search.value.trim()) params.set('q', search.value.trim())
    queue.value = (await api(`/api/admin/tickets?${params}`, { schema: ticketQueueSchema })).tickets
  } catch (e) {
    loadError.value = errText(e, t('support.loadFailed'))
  }
}

onMounted(async () => {
  await loadOwn()
  if (auth.isAdmin) {
    tab.value = 'all'
    await loadQueue()
  }
})
watch([status, search], () => {
  clearTimeout(timer)
  timer = setTimeout(loadQueue, 250)
})

// --- новое обращение ---
const form = reactive({ subject: '', category: null as string | null, message: '' })
const errors = ref<Record<string, string>>({})
const sending = ref(false)

async function create() {
  const parsed = ticketForm.safeParse({ subject: form.subject, category: form.category ?? '', message: form.message })
  errors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  sending.value = true
  try {
    const r = await api('/api/tickets', { method: 'POST', body: parsed.data, schema: ticketCreatedSchema })
    message.success(t('support.created'))
    form.subject = ''
    form.message = ''
    form.category = null
    await router.push({ name: 'ticket', params: { id: r.ticket.id } })
  } catch (e) {
    if (e instanceof ApiError && e.field) errors.value = { [e.field]: e.message }
    else message.error(errText(e, t('support.createFailed')))
  } finally {
    sending.value = false
  }
}

const when = (iso: string) => formatDateTime(iso, locale.value)
</script>

<template>
  <div class="page">
    <header class="head rise">
      <h1>{{ t('support.title') }}</h1>
      <p class="note">{{ t('support.hint') }}</p>
      <p class="note">
        {{ t('support.helpHint') }}
        <router-link :to="{ name: 'help' }">{{ t('support.openHelp') }}</router-link>
      </p>
    </header>

    <n-alert v-if="loadError" type="error" :show-icon="false">{{ loadError }}</n-alert>

    <n-tabs v-model:value="tab" type="line" animated>
      <n-tab-pane name="own" :tab="t('support.myTickets')">
        <div class="stack">
          <section class="glass card rise" style="--i: 1">
            <h2>{{ t('support.newTitle') }}</h2>
            <p class="note">{{ t('support.openLimit', { used: openCount, max: maxOpen }) }}</p>
            <n-form class="form" @submit.prevent="create">
              <n-form-item :label="t('support.subject')" :validation-status="errors.subject ? 'error' : undefined" :feedback="errors.subject ? resolveMessage(errors.subject) : undefined">
                <n-input v-model:value="form.subject" :placeholder="t('support.subjectPlaceholder')" maxlength="120" :input-props="{ 'aria-label': t('support.subject') }" @update:value="errors.subject = ''" />
              </n-form-item>
              <n-form-item :label="t('support.category')" :validation-status="errors.category ? 'error' : undefined" :feedback="errors.category ? resolveMessage(errors.category) : undefined">
                <n-select v-model:value="form.category" :options="categoryOptions" :aria-label="t('support.category')" data-testid="ticket-category" @update:value="errors.category = ''" />
              </n-form-item>
              <n-form-item :label="t('support.message')" :validation-status="errors.message ? 'error' : undefined" :feedback="errors.message ? resolveMessage(errors.message) : undefined">
                <n-input
                  v-model:value="form.message"
                  type="textarea"
                  :rows="6"
                  :placeholder="t('support.messagePlaceholder')"
                  :input-props="{ 'aria-label': t('support.message') }"
                  @update:value="errors.message = ''"
                />
              </n-form-item>
              <n-button type="primary" attr-type="submit" :loading="sending" data-testid="ticket-send">
                <template #icon><n-icon :component="AddOutline" /></template>
                {{ t('support.send') }}
              </n-button>
            </n-form>
          </section>

          <section class="glass card rise" style="--i: 2">
            <h2>{{ t('support.myTickets') }}</h2>
            <p v-if="!own.length && !loadError" class="note" data-testid="tickets-empty">{{ t('support.empty') }}</p>
            <ul v-else class="tickets" data-testid="tickets-own">
              <li v-for="x in own" :key="x.id">
                <router-link :to="{ name: 'ticket', params: { id: x.id } }" class="row" :data-testid="`ticket-${x.id}`">
                  <n-icon :component="ChatbubblesOutline" :size="18" />
                  <strong class="subject">{{ x.subject }}</strong>
                  <span :class="['badge', x.status]">{{ statusLabel(x.status) }}</span>
                  <span class="note">{{ categoryLabel(x.category) }} · {{ t('support.updated', { date: when(x.updated_at) }) }}</span>
                </router-link>
              </li>
            </ul>
          </section>
        </div>
      </n-tab-pane>

      <n-tab-pane v-if="auth.isAdmin" name="all" :tab="t('support.allTickets')">
        <section class="glass card rise">
          <div class="filters">
            <n-select v-model:value="status" :options="statusOptions" style="width: 200px" :aria-label="t('support.filterAll')" />
            <n-input v-model:value="search" clearable :placeholder="t('support.searchPlaceholder')" :input-props="{ 'aria-label': t('support.searchPlaceholder') }" />
          </div>
          <p v-if="!queue.length" class="note" data-testid="queue-empty">{{ t('support.emptyAll') }}</p>
          <ul v-else class="tickets" data-testid="tickets-queue">
            <li v-for="x in queue" :key="x.id">
              <router-link :to="{ name: 'ticket', params: { id: x.id } }" class="row" :data-testid="`queue-${x.id}`">
                <n-icon :component="ChatbubblesOutline" :size="18" />
                <strong class="subject">{{ x.subject }}</strong>
                <span :class="['badge', x.status]">{{ statusLabel(x.status) }}</span>
                <span class="note">{{ t('support.from', { user: x.username }) }} · {{ categoryLabel(x.category) }} · {{ t('support.messages', { n: x.messages }) }} · {{ t('support.updated', { date: when(x.updated_at) }) }}</span>
              </router-link>
            </li>
          </ul>
        </section>
      </n-tab-pane>
    </n-tabs>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 16px;
  max-width: 900px;
}

.head h1 {
  font-size: clamp(26px, 3vw, 34px);
  font-weight: 800;
}

h2 {
  font-size: 19px;
  font-weight: 700;
}

.note {
  color: var(--text-dim);
  font-size: 13.5px;
  word-break: break-word;
}

.stack {
  display: grid;
  gap: 18px;
}

.card {
  padding: 18px 22px 20px;
  display: grid;
  gap: 12px;
}

.form {
  max-width: 680px;
}

.filters {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}

.tickets {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 4px;
}

.row {
  display: flex;
  align-items: baseline;
  gap: 10px;
  flex-wrap: wrap;
  padding: 10px 6px;
  border-bottom: 1px solid var(--border);
  text-decoration: none;
  color: inherit;
}

.row:hover {
  background: rgba(139, 92, 246, 0.08);
}

.subject {
  word-break: break-word;
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
</style>
