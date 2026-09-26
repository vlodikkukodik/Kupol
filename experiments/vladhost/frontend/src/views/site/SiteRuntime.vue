<script setup lang="ts">
import { NAlert, NButton, NFormItem, NInput, NRadioButton, NRadioGroup, NSelect, NSpace, useMessage } from 'naive-ui'
import { computed, onMounted, reactive, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import { runtimeLogsSchema, runtimeResponseSchema, type RuntimeKind, type Site, type SiteRuntime } from '@/api/schemas'
import StatusChip from '@/components/StatusChip.vue'
import { resolveMessage, useI18n } from '@/i18n'

const props = defineProps<{ site: Site }>()
const { t } = useI18n()
const message = useMessage()

const url = computed(() => `/api/sites/${props.site.id}/runtime`)
const current = ref<SiteRuntime | null>(null)
const loadError = ref('')
const saving = ref(false)
const restarting = ref(false)
const errors = ref<Record<string, string>>({})
const form = reactive({ runtime: 'static' as RuntimeKind, version: '', command: '' })

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

function fill(r: SiteRuntime) {
  current.value = r
  form.runtime = r.runtime
  form.version = r.version || r.caps.php[r.caps.php.length - 1] || ''
  form.command = r.command
}

async function load() {
  try {
    fill((await api(url.value, { schema: runtimeResponseSchema })).runtime)
    loadError.value = ''
  } catch (e) {
    loadError.value = errText(e, t('runtime.loadFailed'))
  }
}
onMounted(load)

const caps = computed(() => current.value?.caps)
const available = (k: RuntimeKind) => {
  const c = caps.value
  if (!c || k === 'static') return true
  if (k === 'php') return c.php.length > 0
  if (k === 'node') return c.node !== ''
  return c.python !== ''
}
const versionOptions = computed(() => (caps.value?.php ?? []).map((v) => ({ label: `PHP ${v}`, value: v })))
const isApp = computed(() => form.runtime === 'node' || form.runtime === 'python')
const dirty = computed(() => {
  const c = current.value
  if (!c) return false
  if (form.runtime !== c.runtime) return true
  if (form.runtime === 'php') return form.version !== c.version
  return isApp.value && form.command.trim() !== c.command
})

function kindLabel(k: RuntimeKind): string {
  switch (k) {
    case 'php':
      return t('runtime.kinds.php')
    case 'node':
      return t('runtime.kinds.node')
    case 'python':
      return t('runtime.kinds.python')
    default:
      return t('runtime.kinds.static')
  }
}

const kindHint = computed(() => {
  switch (form.runtime) {
    case 'php':
      return t('runtime.kindHints.php')
    case 'node':
      return t('runtime.kindHints.node')
    case 'python':
      return t('runtime.kindHints.python')
    default:
      return t('runtime.kindHints.static')
  }
})

function stateText(s: string): string {
  switch (s) {
    case 'active':
      return t('runtime.states.active')
    case 'failed':
      return t('runtime.states.failed')
    case 'inactive':
      return t('runtime.states.inactive')
    default:
      return t('runtime.states.unknown')
  }
}
const stateTone = (s: string) => (s === 'active' ? 'emerald' : s === 'failed' ? 'rose' : s === 'inactive' ? 'slate' : 'amber')

async function save() {
  errors.value = {}
  saving.value = true
  try {
    const r = await api(url.value, {
      method: 'PUT',
      body: { runtime: form.runtime, version: form.runtime === 'php' ? form.version : '', command: isApp.value ? form.command.trim() : '' },
      schema: runtimeResponseSchema,
    })
    fill(r.runtime)
    message.success(t('runtime.applied'))
    if (logs.value !== null) void loadLogs()
  } catch (e) {
    if (e instanceof ApiError && e.field) errors.value = { [e.field]: e.message }
    else message.error(errText(e, t('runtime.applyFailed')))
  } finally {
    saving.value = false
  }
}

async function restart() {
  restarting.value = true
  try {
    fill((await api(`${url.value}/restart`, { method: 'POST', schema: runtimeResponseSchema })).runtime)
    message.success(t('runtime.restarted'))
    if (logs.value !== null) void loadLogs()
  } catch (e) {
    message.error(errText(e, t('runtime.restartFailed')))
  } finally {
    restarting.value = false
  }
}

// --- журнал ---
const logs = ref<string | null>(null)
const logsBusy = ref(false)
const logsError = ref('')
async function loadLogs() {
  logsBusy.value = true
  logsError.value = ''
  try {
    logs.value = (await api(`${url.value}/logs`, { schema: runtimeLogsSchema })).logs
  } catch (e) {
    logsError.value = errText(e, t('runtime.logsFailed'))
  } finally {
    logsBusy.value = false
  }
}
</script>

<template>
  <div class="page">
    <header class="head rise">
      <h1>{{ t('runtime.title') }}</h1>
      <p class="note">{{ t('runtime.hint') }}</p>
    </header>

    <n-alert v-if="loadError" type="error" :show-icon="false">{{ loadError }}</n-alert>

    <template v-if="current">
      <section class="glass card rise" style="--i: 1">
        <n-form-item :label="t('runtime.kind')" :show-feedback="false">
          <n-radio-group v-model:value="form.runtime" name="runtime">
            <n-radio-button v-for="k in (['static', 'php', 'node', 'python'] as const)" :key="k" :value="k" :disabled="!available(k)">
              {{ kindLabel(k) }}<template v-if="!available(k)"> ({{ t('runtime.notInstalled') }})</template>
            </n-radio-button>
          </n-radio-group>
        </n-form-item>
        <p class="note">{{ kindHint }}</p>

        <n-form-item v-if="form.runtime === 'php'" :label="t('runtime.version')" :validation-status="errors.version ? 'error' : undefined" :feedback="errors.version ? resolveMessage(errors.version) : undefined">
          <n-select v-model:value="form.version" :options="versionOptions" :aria-label="t('runtime.version')" style="max-width: 220px" />
        </n-form-item>

        <n-form-item
          v-if="isApp"
          :label="t('runtime.command')"
          :validation-status="errors.command ? 'error' : undefined"
          :feedback="errors.command ? resolveMessage(errors.command) : t('runtime.commandHint')"
        >
          <n-input
            v-model:value="form.command"
            :placeholder="form.runtime === 'node' ? t('runtime.commandPlaceholderNode') : t('runtime.commandPlaceholderPython')"
            autocomplete="off"
            :input-props="{ 'aria-label': t('runtime.command') }"
            @update:value="errors.command = ''"
          />
        </n-form-item>

        <n-alert v-if="errors.runtime" type="error" :show-icon="false">{{ resolveMessage(errors.runtime) }}</n-alert>

        <n-space :size="10" align="center">
          <n-button type="primary" :loading="saving" :disabled="!dirty" @click="save">{{ t('runtime.apply') }}</n-button>
          <n-button v-if="current.runtime !== 'static'" class="tint-violet" :loading="restarting" :disabled="dirty" @click="restart">{{ t('runtime.restart') }}</n-button>
          <template v-if="current.runtime !== 'static'">
            <span class="note">{{ t('runtime.state') }}:</span>
            <status-chip :tone="stateTone(current.state)" data-testid="runtime-state">{{ stateText(current.state) }}</status-chip>
            <span v-if="current.port" class="note">{{ t('runtime.port', { port: current.port }) }}</span>
          </template>
        </n-space>
      </section>

      <section v-if="current.runtime !== 'static'" class="glass card rise" style="--i: 2">
        <div class="row">
          <h3>{{ t('runtime.logs') }}</h3>
          <span class="grow" />
          <n-button size="small" class="tint-cyan" :loading="logsBusy" @click="loadLogs">
            {{ logs === null ? t('runtime.logsShow') : t('runtime.logsRefresh') }}
          </n-button>
        </div>
        <p class="note">{{ current.runtime === 'php' ? t('runtime.logsHintPhp') : t('runtime.logsHintApp') }}</p>
        <n-alert v-if="logsError" type="error" :show-icon="false">{{ logsError }}</n-alert>
        <template v-if="logs !== null">
          <pre v-if="logs.trim()" class="out" data-testid="runtime-logs">{{ logs }}</pre>
          <p v-else class="note">{{ t('runtime.logsEmpty') }}</p>
        </template>
      </section>

      <section class="glass card rise" style="--i: 3">
        <p class="note">{{ t('runtime.scope') }}</p>
        <h3>{{ t('runtime.depsTitle') }}</h3>
        <p class="note">{{ t('runtime.deps') }}</p>
        <p class="note">{{ t('runtime.limits') }}</p>
      </section>
    </template>
  </div>
</template>

<style scoped>
.page {
  display: grid;
  gap: 18px;
  max-width: 900px;
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

.card {
  padding: 18px 22px 20px;
  display: grid;
  gap: 12px;
}

.card h3 {
  font-size: 16px;
}

.row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.grow {
  flex: 1;
}

.out {
  margin: 0;
  padding: 10px 12px;
  max-height: 320px;
  overflow: auto;
  border-radius: 12px;
  background: rgba(0, 0, 0, 0.3);
  border: 1px solid var(--border);
  font-size: 12.5px;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
