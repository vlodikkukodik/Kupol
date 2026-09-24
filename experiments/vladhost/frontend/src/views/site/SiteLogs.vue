<script setup lang="ts">
import { DownloadOutline, RefreshOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NIcon, NInput, NRadioButton, NRadioGroup, NSwitch, useMessage } from 'naive-ui'
import { onBeforeUnmount, ref, watch } from 'vue'
import { api, ApiError, apiBlob } from '@/api/client'
import { logPageSchema, logStatusClasses, type LogEntry, type LogKind, type LogStatusClass, type Site } from '@/api/schemas'
import EmptyState from '@/components/EmptyState.vue'
import StatusChip from '@/components/StatusChip.vue'
import { formatBytes, formatTimestamp, useI18n, type MessageKey } from '@/i18n'
import { useSitesStore } from '@/stores/sites'

const props = defineProps<{ site: Site }>()
const { t, locale } = useI18n()
const store = useSitesStore()
const message = useMessage()

const PAGE = 100
const AUTO_MS = 10_000

const kind = ref<LogKind>('access')
const status = ref<LogStatusClass>('')
const search = ref('')
const auto = ref(false)
const entries = ref<LogEntry[]>([])
const hasMore = ref(false)
const loading = ref(true)
const loadingMore = ref(false)
const error = ref('')

// Ответ устаревшего запроса (пользователь уже сменил фильтр) не должен затирать свежий.
let seq = 0

function query(before?: string): string {
  const p = new URLSearchParams({ kind: kind.value, limit: String(PAGE) })
  if (kind.value === 'access' && status.value) p.set('status', status.value)
  if (search.value.trim()) p.set('q', search.value.trim())
  if (before) p.set('before', before)
  return p.toString()
}

async function load() {
  if (!store.logsAvailable) return
  const mine = ++seq
  error.value = ''
  try {
    const r = await api(`/api/sites/${props.site.id}/logs?${query()}`, { schema: logPageSchema })
    if (mine !== seq) return
    entries.value = r.entries
    hasMore.value = r.has_more
  } catch (e) {
    if (mine === seq) error.value = e instanceof ApiError ? e.message : t('logs.loadFailed')
  } finally {
    if (mine === seq) loading.value = false
  }
}

async function more() {
  const last = entries.value[entries.value.length - 1]
  if (!last) return
  const mine = seq
  loadingMore.value = true
  try {
    const r = await api(`/api/sites/${props.site.id}/logs?${query(last.t)}`, { schema: logPageSchema })
    if (mine !== seq) return
    entries.value = [...entries.value, ...r.entries]
    hasMore.value = r.has_more
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : t('logs.loadFailed'))
  } finally {
    loadingMore.value = false
  }
}

async function download() {
  try {
    const blob = await apiBlob(`/api/sites/${props.site.id}/logs/download?kind=${kind.value}`)
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${props.site.host}-${kind.value}.log`
    a.click()
    URL.revokeObjectURL(url)
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : t('logs.downloadFailed'))
  }
}

// Поиск идёт с небольшой задержкой, чтобы не запрашивать журнал на каждую букву.
let debounce: ReturnType<typeof setTimeout> | undefined
watch(search, () => {
  clearTimeout(debounce)
  debounce = setTimeout(() => void load(), 350)
})
watch([kind, status], () => {
  loading.value = true
  void load()
})

let timer: ReturnType<typeof setInterval> | undefined
watch(auto, (on) => {
  clearInterval(timer)
  if (on) timer = setInterval(() => void load(), AUTO_MS)
})
onBeforeUnmount(() => {
  clearTimeout(debounce)
  clearInterval(timer)
})

void load()

const tone = (s: number | undefined) => (s === undefined ? 'slate' : s >= 500 ? 'rose' : s >= 400 ? 'amber' : s >= 300 ? 'cyan' : 'emerald')

const codeKey = {
  not_found: 'logs.code.not_found',
  forbidden: 'logs.code.forbidden',
  unauthorized: 'logs.code.unauthorized',
  bad_request: 'logs.code.bad_request',
  method_not_allowed: 'logs.code.method_not_allowed',
  gone: 'logs.code.gone',
  too_many_requests: 'logs.code.too_many_requests',
  server_error: 'logs.code.server_error',
  error: 'logs.code.error',
} as const satisfies Record<string, MessageKey>

function reason(code: string | undefined): string {
  const key = codeKey[(code ?? 'error') as keyof typeof codeKey]
  return key ? t(key) : (code ?? '')
}

const statusLabel = (c: LogStatusClass) => (c === '' ? t('logs.status.all') : c)
</script>

<template>
  <div class="page">
    <header class="head rise">
      <h1>{{ t('siteArea.logs') }}</h1>
      <p>{{ t('logs.hint') }}</p>
    </header>

    <n-alert v-if="!store.logsAvailable" type="info" :show-icon="false">{{ t('logs.unavailable') }}</n-alert>

    <template v-else>
      <section class="bar glass rise" style="--i: 1">
        <n-radio-group v-model:value="kind" size="medium">
          <n-radio-button value="access">{{ t('logs.kind.access') }}</n-radio-button>
          <n-radio-button value="error">{{ t('logs.kind.error') }}</n-radio-button>
        </n-radio-group>

        <n-radio-group v-if="kind === 'access'" v-model:value="status" size="medium" :aria-label="t('logs.cols.status')">
          <n-radio-button v-for="c in logStatusClasses" :key="c" :value="c">{{ statusLabel(c) }}</n-radio-button>
        </n-radio-group>

        <n-input
          v-model:value="search"
          class="search"
          clearable
          :placeholder="t('logs.searchPlaceholder')"
          :input-props="{ 'aria-label': t('logs.searchLabel') }"
        />

        <span class="grow" />
        <label class="auto"><n-switch v-model:value="auto" size="small" /> {{ t('logs.auto') }}</label>
        <n-button class="tint-cyan" @click="load">
          <template #icon><n-icon :component="RefreshOutline" /></template>
          {{ t('logs.refresh') }}
        </n-button>
        <n-button class="tint-violet" @click="download">
          <template #icon><n-icon :component="DownloadOutline" /></template>
          {{ t('logs.download') }}
        </n-button>
      </section>

      <n-alert v-if="error" type="error" :show-icon="false">
        {{ error }} <n-button size="tiny" @click="load">{{ t('common.retry') }}</n-button>
      </n-alert>

      <div v-if="loading" class="skeleton" style="height: 220px" />

      <empty-state
        v-else-if="!entries.length && !error"
        :title="search || status ? t('logs.emptyFiltered') : t('logs.empty')"
        :hint="search || status ? '' : t('logs.emptyHint')"
        class="glass"
      />

      <section v-else-if="entries.length" class="table glass rise" style="--i: 2">
        <div class="scroll">
          <table v-if="kind === 'access'">
            <thead>
              <tr>
                <th>{{ t('logs.cols.time') }}</th>
                <th>{{ t('logs.cols.ip') }}</th>
                <th>{{ t('logs.cols.request') }}</th>
                <th>{{ t('logs.cols.status') }}</th>
                <th class="num">{{ t('logs.cols.size') }}</th>
                <th class="num">{{ t('logs.cols.duration') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(e, i) in entries" :key="e.t + i" :title="e.ua">
                <td class="nowrap">{{ formatTimestamp(e.t, locale) }}</td>
                <td class="nowrap"><code>{{ e.ip }}</code></td>
                <td class="req">
                  <span class="method">{{ e.m }}</span> <span class="path">{{ e.p }}</span>
                  <span v-if="e.host !== site.host" class="dom">{{ e.host }}</span>
                </td>
                <td><status-chip :tone="tone(e.s)">{{ e.s }}</status-chip></td>
                <td class="num nowrap">{{ formatBytes(e.b ?? 0, locale) }}</td>
                <td class="num nowrap">{{ t('logs.ms', { n: e.ms ?? 0 }) }}</td>
              </tr>
            </tbody>
          </table>

          <table v-else>
            <thead>
              <tr>
                <th>{{ t('logs.cols.time') }}</th>
                <th>{{ t('logs.cols.reason') }}</th>
                <th>{{ t('logs.cols.ip') }}</th>
                <th>{{ t('logs.cols.path') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="(e, i) in entries" :key="e.t + i">
                <td class="nowrap">{{ formatTimestamp(e.t, locale) }}</td>
                <td><status-chip :tone="e.code === 'server_error' ? 'rose' : 'amber'">{{ reason(e.code) }}</status-chip></td>
                <td class="nowrap"><code>{{ e.ip }}</code></td>
                <td class="req">
                  <span class="path">{{ e.p }}</span>
                  <span v-if="e.host !== site.host" class="dom">{{ e.host }}</span>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-if="hasMore" class="more">
          <n-button :loading="loadingMore" class="tint-violet" @click="more">{{ t('logs.more') }}</n-button>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 18px;
  max-width: 1080px;
}

.head h1 {
  font-size: clamp(26px, 3vw, 34px);
  font-weight: 800;
}

.head p {
  margin: 4px 0 0;
  color: var(--text-dim);
}

.bar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  padding: 14px 16px;
}

.search {
  width: 260px;
  max-width: 100%;
}

.grow {
  flex: 1;
}

.auto {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  color: var(--text-dim);
  font-size: 13.5px;
  cursor: pointer;
}

.table {
  padding: 6px 8px 12px;
}

.scroll {
  overflow-x: auto;
}

table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13.5px;
}

th {
  padding: 10px 12px;
  text-align: left;
  color: var(--text-faint);
  font-size: 12px;
  font-weight: 650;
  letter-spacing: 0.06em;
  text-transform: uppercase;
  white-space: nowrap;
}

td {
  padding: 8px 12px;
  border-top: 1px solid var(--border);
  vertical-align: top;
}

.num {
  text-align: right;
}

.nowrap {
  white-space: nowrap;
}

.req {
  min-width: 220px;
  overflow-wrap: anywhere;
}

.method {
  color: var(--text-dim);
  font-weight: 650;
}

.dom {
  display: inline-block;
  margin-left: 8px;
  padding: 0 8px;
  border-radius: 999px;
  color: #a5f3fc;
  background: rgba(34, 211, 238, 0.12);
  font-size: 12px;
}

.more {
  display: flex;
  justify-content: center;
  padding-top: 12px;
}
</style>
