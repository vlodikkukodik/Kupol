<script setup lang="ts">
import { AddOutline, TimerOutline } from '@vicons/ionicons5'
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NModal,
  NPopconfirm,
  NRadioButton,
  NRadioGroup,
  NSelect,
  NSpace,
  NSwitch,
  useMessage,
} from 'naive-ui'
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import {
  cronForm,
  cronJobResponseSchema,
  cronListSchema,
  cronRunStartedSchema,
  cronRunsSchema,
  fieldErrors,
  type CronInfo,
  type CronJob,
  type CronKind,
  type CronRun,
  type CronRunStatus,
} from '@/api/schemas'
import StatusChip from '@/components/StatusChip.vue'
import { formatDateTime, resolveMessage, useI18n } from '@/i18n'
import { useSitesStore } from '@/stores/sites'

const { t, locale } = useI18n()
const message = useMessage()
const sitesStore = useSitesStore()

const jobs = ref<CronJob[]>([])
const info = ref<CronInfo | null>(null)
const loading = ref(true)
const loadError = ref('')
const busy = ref<number | null>(null)

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

async function load() {
  try {
    const r = await api('/api/cron', { schema: cronListSchema })
    jobs.value = r.jobs
    info.value = r.info
    loadError.value = ''
  } catch (e) {
    loadError.value = errText(e, t('cron.loadFailed'))
  } finally {
    loading.value = false
  }
}

// Пока идёт запуск, журнал и карточки обновляются сами.
let poll: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  void sitesStore.load(t('sites.loadFailed'))
  poll = setInterval(() => {
    if (jobs.value.some((j) => j.last_status === 'running') || logJob.value) {
      void load()
      if (logJob.value) void loadRuns()
    }
  }, 4000)
})
onBeforeUnmount(() => clearInterval(poll))

const atLimit = computed(() => !!info.value && jobs.value.length >= info.value.max_jobs)
const siteOptions = computed(() => sitesStore.sites.map((s) => ({ label: s.host, value: s.id })))

// --- форма ---
const editing = ref<CronJob | 'new' | null>(null)
const form = reactive({ name: '', kind: 'http' as CronKind, schedule: '0 3 * * *', url: '', command: '', siteId: null as number | null, enabled: true })
const errors = ref<Record<string, string>>({})
const saving = ref(false)

const presets = computed(() => [
  { label: t('cron.presetEvery5'), value: '*/5 * * * *' },
  { label: t('cron.presetHourly'), value: '0 * * * *' },
  { label: t('cron.presetDaily'), value: '0 3 * * *' },
  { label: t('cron.presetWeekly'), value: '0 3 * * 1' },
])

function openForm(job: CronJob | 'new') {
  editing.value = job
  errors.value = {}
  if (job === 'new') {
    Object.assign(form, { name: '', kind: 'http', schedule: '0 3 * * *', url: '', command: '', siteId: null, enabled: true })
  } else {
    Object.assign(form, { name: job.name, kind: job.kind, schedule: job.schedule, url: job.url, command: job.command, siteId: job.site_id, enabled: job.enabled })
  }
}

const body = () => ({
  name: form.name.trim(),
  kind: form.kind,
  schedule: form.schedule.trim(),
  url: form.kind === 'http' ? form.url.trim() : '',
  command: form.kind === 'command' ? form.command : '',
  site_id: form.kind === 'command' ? (form.siteId ?? 0) : 0,
  enabled: form.enabled,
})

async function save() {
  const parsed = cronForm.safeParse({ name: form.name })
  errors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  saving.value = true
  const target = editing.value
  try {
    const isNew = target === 'new'
    await api(isNew ? '/api/cron' : `/api/cron/${(target as CronJob).id}`, {
      method: isNew ? 'POST' : 'PUT',
      body: body(),
      schema: cronJobResponseSchema,
    })
    editing.value = null
    message.success(t(isNew ? 'cron.created' : 'cron.saved'))
    await load()
  } catch (e) {
    if (e instanceof ApiError && e.field) errors.value = { [e.field === 'site_id' ? 'site' : e.field]: e.message }
    else message.error(errText(e, t('cron.saveFailed')))
  } finally {
    saving.value = false
  }
}

async function run(j: CronJob, work: () => Promise<void>, failed: string) {
  busy.value = j.id
  try {
    await work()
  } catch (e) {
    message.error(errText(e, failed))
  } finally {
    busy.value = null
  }
}

const toggle = (j: CronJob, enabled: boolean) =>
  run(j, async () => {
    await api(`/api/cron/${j.id}`, {
      method: 'PUT',
      body: { name: j.name, kind: j.kind, schedule: j.schedule, url: j.url, command: j.command, site_id: j.site_id ?? 0, enabled },
      schema: cronJobResponseSchema,
    })
    await load()
  }, t('cron.saveFailed'))

const remove = (j: CronJob) =>
  run(j, async () => {
    await api(`/api/cron/${j.id}`, { method: 'DELETE' })
    message.success(t('cron.removed'))
    await load()
  }, t('cron.removeFailed'))

const runNow = (j: CronJob) =>
  run(j, async () => {
    await api(`/api/cron/${j.id}/run`, { method: 'POST', schema: cronRunStartedSchema })
    message.success(t('cron.runStarted'))
    await load()
  }, t('cron.runFailed'))

// --- журнал ---
const logJob = ref<CronJob | null>(null)
const runs = ref<CronRun[]>([])
const logError = ref('')

async function loadRuns() {
  const j = logJob.value
  if (!j) return
  try {
    runs.value = (await api(`/api/cron/${j.id}/runs`, { schema: cronRunsSchema })).runs
    logError.value = ''
  } catch (e) {
    logError.value = errText(e, t('cron.logFailed'))
  }
}

function openLog(j: CronJob) {
  logJob.value = j
  runs.value = []
  void loadRuns()
}

const statusTone = (s: CronRunStatus | string) =>
  ({ ok: 'emerald', failed: 'rose', timeout: 'amber', skipped: 'slate', running: 'cyan' })[s] as 'emerald' | 'rose' | 'amber' | 'slate' | 'cyan' | undefined ?? 'slate'

function statusText(s: string): string {
  switch (s) {
    case 'running':
      return t('cron.status.running')
    case 'ok':
      return t('cron.status.ok')
    case 'failed':
      return t('cron.status.failed')
    case 'timeout':
      return t('cron.status.timeout')
    case 'skipped':
      return t('cron.status.skipped')
    default:
      return s
  }
}

// Системные причины приходят кодами: перевод лежит в интерфейсе, а не в записи журнала.
function reasonText(code: string): string {
  switch (code) {
    case 'private_address':
      return t('cron.reason.private_address')
    case 'bad_url':
      return t('cron.reason.bad_url')
    case 'too_many_redirects':
      return t('cron.reason.too_many_redirects')
    case 'commands_off':
      return t('cron.reason.commands_off')
    case 'site_missing':
      return t('cron.reason.site_missing')
    case 'runner_failed':
      return t('cron.reason.runner_failed')
    case 'overlap':
      return t('cron.reason.overlap')
    case 'aborted':
      return t('cron.reason.aborted')
    default:
      return code
  }
}

const kindText = (k: CronKind) => (k === 'http' ? t('cron.kindChip.http') : t('cron.kindChip.command'))
const siteHost = (id: number | null) => sitesStore.sites.find((s) => s.id === id)?.host ?? ''
</script>

<template>
  <div class="page">
    <header class="head rise">
      <div>
        <h1>{{ t('cron.title') }}</h1>
        <p class="note">{{ t('cron.hint') }}</p>
      </div>
      <n-button v-if="info" type="primary" :disabled="atLimit" @click="openForm('new')">
        <template #icon><n-icon :component="AddOutline" /></template>
        {{ t('cron.add') }}
      </n-button>
    </header>

    <n-alert v-if="loadError" type="error" :show-icon="false">{{ loadError }}</n-alert>
    <p v-if="info" class="note limits">
      {{ t('cron.limits', { used: jobs.length, max: info.max_jobs, min: info.min_interval_min, sec: info.timeout_sec, keep: info.keep_runs }) }}
    </p>
    <p v-if="!loading && !jobs.length && !loadError" class="note">{{ t('cron.empty') }}</p>

    <section v-for="(j, i) in jobs" :key="j.id" class="glass card rise" :style="{ '--i': i + 1 }" :data-testid="`job-${j.name}`">
      <div class="row">
        <span class="ic"><n-icon :size="18" :component="TimerOutline" /></span>
        <strong class="jname">{{ j.name }}</strong>
        <status-chip :tone="j.kind === 'http' ? 'cyan' : 'violet'">{{ kindText(j.kind) }}</status-chip>
        <status-chip v-if="j.last_status" :tone="statusTone(j.last_status)" :pulse="j.last_status === 'running'">{{ statusText(j.last_status) }}</status-chip>
        <status-chip v-if="!j.enabled" tone="slate">{{ t('cron.disabled') }}</status-chip>
        <span class="grow" />
        <label class="switch">
          <n-switch size="small" :value="j.enabled" :disabled="busy === j.id" :aria-label="`${t('cron.enabled')}: ${j.name}`" @update:value="(v: boolean) => toggle(j, v)" />
          {{ t('cron.enabled') }}
        </label>
      </div>

      <div class="what">
        <code class="sched">{{ j.schedule }}</code>
        <code class="target">{{ j.kind === 'http' ? j.url : `${siteHost(j.site_id)} › ${j.command}` }}</code>
      </div>

      <p class="note">
        {{ j.next_run_at ? t('cron.next', { date: formatDateTime(j.next_run_at, locale) }) : t('cron.nextNone') }} ·
        {{ j.last_run_at ? t('cron.last', { date: formatDateTime(j.last_run_at, locale) }) : t('cron.lastNone') }}
        <template v-if="j.fail_streak > 0"> · <span class="err">{{ t('cron.streak', { n: j.fail_streak }) }}</span></template>
      </p>

      <n-space :size="8">
        <n-button size="small" type="primary" :disabled="busy === j.id" @click="runNow(j)">{{ t('cron.runNow') }}</n-button>
        <n-button size="small" class="tint-cyan" @click="openLog(j)">{{ t('cron.log') }}</n-button>
        <n-button size="small" class="tint-violet" :disabled="busy === j.id" @click="openForm(j)">{{ t('cron.edit') }}</n-button>
        <n-popconfirm @positive-click="remove(j)">
          <template #trigger>
            <n-button size="small" class="tint-rose" :disabled="busy === j.id">{{ t('cron.remove') }}</n-button>
          </template>
          {{ t('cron.removeConfirm', { name: j.name }) }}
        </n-popconfirm>
      </n-space>
    </section>

    <n-modal :show="editing !== null" preset="card" :title="editing === 'new' ? t('cron.add') : t('cron.edit')" style="max-width: 560px" @update:show="editing = null">
      <n-form @submit.prevent="save">
        <n-form-item :label="t('cron.name')" :validation-status="errors.name ? 'error' : undefined" :feedback="errors.name ? resolveMessage(errors.name) : undefined">
          <n-input v-model:value="form.name" :placeholder="t('cron.namePlaceholder')" autocomplete="off" :input-props="{ 'aria-label': t('cron.name') }" @update:value="errors.name = ''" />
        </n-form-item>

        <n-form-item :label="t('cron.kind')" :feedback="info && !info.commands_enabled ? t('cron.commandsOff') : undefined">
          <n-radio-group v-model:value="form.kind" name="kind">
            <n-radio-button value="http">{{ t('cron.kindHttp') }}</n-radio-button>
            <n-radio-button value="command" :disabled="!info?.commands_enabled">{{ t('cron.kindCommand') }}</n-radio-button>
          </n-radio-group>
        </n-form-item>

        <n-form-item v-if="form.kind === 'http'" :label="t('cron.url')" :validation-status="errors.url ? 'error' : undefined" :feedback="errors.url ? resolveMessage(errors.url) : t('cron.urlHint')">
          <n-input v-model:value="form.url" :placeholder="t('cron.urlPlaceholder')" autocomplete="off" :input-props="{ 'aria-label': t('cron.url') }" @update:value="errors.url = ''" />
        </n-form-item>
        <template v-else>
          <n-form-item :label="t('cron.site')" :validation-status="errors.site ? 'error' : undefined" :feedback="errors.site ? resolveMessage(errors.site) : t('cron.siteHint')">
            <n-select v-model:value="form.siteId" :options="siteOptions" :placeholder="t('cron.sitePlaceholder')" :aria-label="t('cron.site')" @update:value="errors.site = ''" />
          </n-form-item>
          <n-form-item :label="t('cron.command')" :validation-status="errors.command ? 'error' : undefined" :feedback="errors.command ? resolveMessage(errors.command) : t('cron.commandHint', { sec: info?.timeout_sec ?? 60 })">
            <n-input v-model:value="form.command" type="textarea" :rows="3" :placeholder="t('cron.commandPlaceholder')" autocomplete="off" :input-props="{ 'aria-label': t('cron.command') }" @update:value="errors.command = ''" />
          </n-form-item>
        </template>

        <n-form-item :label="t('cron.schedule')" :validation-status="errors.schedule ? 'error' : undefined" :feedback="errors.schedule ? resolveMessage(errors.schedule) : t('cron.scheduleHint')">
          <n-input v-model:value="form.schedule" autocomplete="off" :input-props="{ 'aria-label': t('cron.schedule') }" @update:value="errors.schedule = ''" />
        </n-form-item>
        <div class="presets">
          <n-button v-for="p in presets" :key="p.value" size="tiny" class="tint-cyan" @click="((form.schedule = p.value), (errors.schedule = ''))">{{ p.label }}</n-button>
        </div>

        <label class="switch on">
          <n-switch v-model:value="form.enabled" :aria-label="t('cron.enabled')" />
          {{ t('cron.enabled') }}
        </label>

        <n-button type="primary" attr-type="submit" :loading="saving">{{ editing === 'new' ? t('cron.create') : t('cron.save') }}</n-button>
      </n-form>
    </n-modal>

    <n-modal :show="logJob !== null" preset="card" :title="t('cron.logTitle', { name: logJob?.name ?? '' })" style="max-width: 720px" @update:show="logJob = null">
      <n-alert v-if="logError" type="error" :show-icon="false">{{ logError }}</n-alert>
      <p v-else-if="!runs.length" class="note">{{ t('cron.logEmpty') }}</p>
      <ul class="runs">
        <li v-for="r in runs" :key="r.id" class="run">
          <div class="row">
            <status-chip :tone="statusTone(r.status)" :pulse="r.status === 'running'">{{ statusText(r.status) }}</status-chip>
            <span class="note">{{ formatDateTime(r.started_at, locale) }}</span>
            <span v-if="r.code" class="note">{{ t('cron.code', { n: r.code }) }}</span>
            <span v-if="r.finished_at" class="note">{{ t('cron.duration', { ms: r.duration_ms }) }}</span>
          </div>
          <p v-if="r.reason" class="note reason">{{ reasonText(r.reason) }}</p>
          <pre v-if="r.output" class="out">{{ r.output }}</pre>
          <p v-else-if="!r.reason && r.status !== 'running'" class="note">{{ t('cron.noOutput') }}</p>
        </li>
      </ul>
    </n-modal>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 18px;
  max-width: 900px;
}

.head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.head h1 {
  font-size: clamp(26px, 3vw, 34px);
  font-weight: 800;
}

.note {
  color: var(--text-dim);
  font-size: 13.5px;
  word-break: break-word;
}

.err {
  color: #fda4af;
}

.card {
  padding: 18px 22px 20px;
  display: grid;
  gap: 12px;
}

.row {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.grow {
  flex: 1;
}

.ic {
  display: inline-grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 10px;
  color: #fff;
  background: var(--grad-emerald);
}

.jname {
  font-size: 16px;
}

.what {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  align-items: center;
}

.sched {
  color: #a5f3fc;
}

.target {
  word-break: break-all;
}

.switch {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  color: var(--text-dim);
  font-size: 13.5px;
  cursor: pointer;
}

.switch.on {
  margin: 4px 0 16px;
  display: flex;
}

.presets {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin: -8px 0 14px;
}

.runs {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 14px;
}

.run {
  display: grid;
  gap: 6px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border);
}

.reason {
  color: #fde68a;
}

.out {
  margin: 0;
  padding: 10px 12px;
  max-height: 220px;
  overflow: auto;
  border-radius: 12px;
  background: rgba(0, 0, 0, 0.3);
  border: 1px solid var(--border);
  font-size: 12.5px;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
