<script setup lang="ts">
import { AddOutline, CopyOutline, ServerOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NForm, NFormItem, NIcon, NInput, NModal, NPopconfirm, NRadioButton, NRadioGroup, NSpace, useMessage } from 'naive-ui'
import { computed, onMounted, reactive, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import {
  databaseCreatedSchema,
  databaseForm,
  databaseResponseSchema,
  databasesSchema,
  fieldErrors,
  webClientSchema,
  type Database,
  type DbEngine,
  type DbInfo,
} from '@/api/schemas'
import StatusChip from '@/components/StatusChip.vue'
import { formatDateTime, resolveMessage, useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const { t, locale, formatBytes } = useI18n()
const message = useMessage()
const auth = useAuthStore()

const databases = ref<Database[]>([])
const info = ref<DbInfo | null>(null)
const loading = ref(true)
const loadError = ref('')
const busy = ref<number | null>(null)

const engineName = (e: DbEngine) => (e === 'postgres' ? t('databases.engine.postgres') : t('databases.engine.mariadb'))
const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

async function load() {
  try {
    const r = await api('/api/databases', { schema: databasesSchema })
    databases.value = r.databases
    info.value = r.info
    loadError.value = ''
  } catch (e) {
    loadError.value = errText(e, t('databases.loadFailed'))
  } finally {
    loading.value = false
  }
}
onMounted(load)

const engines = computed(() => info.value?.engines ?? [])
const countOf = (e: DbEngine) => databases.value.filter((d) => d.engine === e).length
const full = (e: DbEngine) => !!info.value && countOf(e) >= info.value.per_engine

// --- создание ---
const form = reactive<{ engine: DbEngine; name: string }>({ engine: 'postgres', name: '' })
const formError = ref('')
const creating = ref(false)
const namePreview = computed(() => `${auth.user?.username ?? 'user'}_${form.name.trim().toLowerCase() || 'name'}`)

// Выданный пароль: показывается один раз.
const grant = ref<{ db: Database; password: string } | null>(null)

async function create() {
  const parsed = databaseForm.safeParse({ name: form.name })
  formError.value = parsed.success ? '' : (fieldErrors(parsed.error).name ?? '')
  if (!parsed.success) return
  creating.value = true
  try {
    const r = await api('/api/databases', { method: 'POST', body: { engine: form.engine, name: parsed.data.name }, schema: databaseCreatedSchema })
    grant.value = { db: r.database, password: r.password }
    form.name = ''
    message.success(t('databases.created'))
    await load()
  } catch (e) {
    if (e instanceof ApiError && e.field === 'name') formError.value = e.message
    else message.error(errText(e, t('databases.createFailed')))
  } finally {
    creating.value = false
  }
}

async function run(d: Database, work: () => Promise<void>, failed: string) {
  busy.value = d.id
  try {
    await work()
  } catch (e) {
    message.error(errText(e, failed))
  } finally {
    busy.value = null
  }
}

const resetPassword = (d: Database) =>
  run(d, async () => {
    const r = await api(`/api/databases/${d.id}/password`, { method: 'POST', schema: databaseCreatedSchema })
    grant.value = { db: r.database, password: r.password }
  }, t('databases.passwordFailed'))

const remove = (d: Database) =>
  run(d, async () => {
    await api(`/api/databases/${d.id}`, { method: 'DELETE' })
    message.success(t('databases.removed'))
    await load()
  }, t('databases.removeFailed'))

const check = (d: Database) =>
  run(d, async () => {
    await api(`/api/databases/${d.id}/check`, { method: 'POST', schema: databaseResponseSchema })
    message.success(t('databases.checked'))
    await load()
  }, t('databases.checkFailed'))

// Вход в Adminer: одноразовая ссылка открывается в новой вкладке.
const openWeb = (d: Database) =>
  run(d, async () => {
    const r = await api(`/api/databases/${d.id}/web`, { method: 'POST', schema: webClientSchema })
    window.open(r.url, '_blank', 'noopener')
  }, t('databases.webFailed'))

// --- внешний доступ ---
const addrText = reactive<Record<number, string>>({})
const addrError = reactive<Record<number, string>>({})
const textOf = (d: Database) => addrText[d.id] ?? d.addrs.join('\n')

const saveAddrs = (d: Database) =>
  run(d, async () => {
    const addrs = textOf(d).split(/[\s,;]+/).filter(Boolean)
    try {
      await api(`/api/databases/${d.id}/addrs`, { method: 'PUT', body: { addrs }, schema: databaseResponseSchema })
    } catch (e) {
      if (e instanceof ApiError && e.status === 400) {
        addrError[d.id] = e.message
        return
      }
      throw e
    }
    delete addrText[d.id]
    delete addrError[d.id]
    message.success(t('databases.addrsSaved'))
    await load()
  }, t('databases.addrsFailed'))

const limitOf = () => info.value?.size_limit ?? 0
const percentOf = (d: Database) => (limitOf() ? Math.min(100, Math.round((d.size_bytes / limitOf()) * 100)) : 0)

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success(t('common.copied'))
  } catch {
    message.error(t('common.copyFailed'))
  }
}
</script>

<template>
  <div class="page">
    <header class="head rise">
      <h1>{{ t('databases.title') }}</h1>
      <p class="note">{{ t('databases.hint') }}</p>
    </header>

    <n-alert v-if="loadError" type="error" :show-icon="false">{{ loadError }}</n-alert>

    <section v-if="info" class="glass card rise" style="--i: 1">
      <n-alert v-if="engines.every(full)" type="info" :show-icon="false">{{ t('databases.limit', { max: info.per_engine }) }}</n-alert>
      <n-form v-else class="form" @submit.prevent="create">
        <n-form-item :label="t('databases.engineLabel')">
          <n-radio-group v-model:value="form.engine" name="engine">
            <n-radio-button v-for="e in engines" :key="e" :value="e" :disabled="full(e)">{{ engineName(e) }}</n-radio-button>
          </n-radio-group>
        </n-form-item>
        <n-form-item
          :label="t('databases.name')"
          :validation-status="formError ? 'error' : undefined"
          :feedback="formError ? resolveMessage(formError) : t('databases.namePreview', { name: namePreview })"
        >
          <n-input v-model:value="form.name" :placeholder="t('databases.namePlaceholder')" autocomplete="off" :input-props="{ 'aria-label': t('databases.name') }" @update:value="formError = ''" />
        </n-form-item>
        <n-button type="primary" attr-type="submit" :loading="creating" :disabled="full(form.engine)">
          <template #icon><n-icon :component="AddOutline" /></template>
          {{ t('databases.create') }}
        </n-button>
      </n-form>
    </section>

    <p v-if="!loading && !databases.length && !loadError" class="note">{{ t('databases.empty') }}</p>

    <section v-for="(d, i) in databases" :key="d.id" class="glass card db rise" :style="{ '--i': i + 2 }" :data-testid="`db-${d.name}`">
      <div class="row">
        <span class="ic"><n-icon :size="18" :component="ServerOutline" /></span>
        <code class="dbname">{{ d.name }}</code>
        <status-chip :tone="d.engine === 'postgres' ? 'cyan' : 'violet'">{{ engineName(d.engine) }}</status-chip>
        <status-chip v-if="d.status === 'frozen'" tone="rose">{{ t('databases.frozen') }}</status-chip>
        <span class="grow" />
        <span class="note">{{ t('databases.created_at', { date: formatDateTime(d.created_at, locale) }) }}</span>
      </div>

      <div class="bar" role="img" :aria-label="t('databases.usage', { used: formatBytes(d.size_bytes), limit: formatBytes(limitOf()) })">
        <span class="fill" :class="{ hot: percentOf(d) >= 90 }" :style="{ width: `${percentOf(d)}%` }" />
      </div>
      <p class="note">{{ t('databases.usage', { used: formatBytes(d.size_bytes), limit: formatBytes(limitOf()) }) }}</p>
      <n-alert v-if="d.status === 'frozen'" type="warning" :show-icon="false">{{ t('databases.frozenHint') }}</n-alert>

      <p class="note">
        {{ t('databases.connection', { host: info?.host ?? '', port: info?.ports[d.engine] ?? 0 }) }}
        <template v-if="info?.external[d.engine]"> · {{ t('databases.tls') }}</template>
      </p>

      <div v-if="info?.external[d.engine]" class="addrs">
        <label class="lbl" :for="`addrs-${d.id}`">{{ t('databases.addrs') }}</label>
        <n-input
          :value="textOf(d)"
          type="textarea"
          :rows="2"
          :placeholder="t('databases.addrsPlaceholder')"
          :input-props="{ id: `addrs-${d.id}`, autocomplete: 'off' }"
          :status="addrError[d.id] ? 'error' : undefined"
          @update:value="(v: string) => { addrText[d.id] = v; delete addrError[d.id] }"
        />
        <p class="note" :class="{ err: addrError[d.id] }">{{ addrError[d.id] ? resolveMessage(addrError[d.id]!) : t('databases.addrsHint', { max: info.max_addrs }) }}</p>
        <n-button size="small" class="tint-cyan" :disabled="busy === d.id" @click="saveAddrs(d)">{{ t('databases.addrsSave') }}</n-button>
      </div>
      <p v-else class="note">{{ t('databases.addrsOff') }}</p>

      <n-space :size="8" class="actions">
        <n-button v-if="info?.web_client" size="small" type="primary" :disabled="busy === d.id" @click="openWeb(d)">{{ t('databases.openWeb') }}</n-button>
        <n-button size="small" class="tint-emerald" :disabled="busy === d.id" @click="check(d)">{{ t('databases.check') }}</n-button>
        <n-popconfirm @positive-click="resetPassword(d)">
          <template #trigger>
            <n-button size="small" class="tint-violet" :disabled="busy === d.id">{{ t('databases.newPassword') }}</n-button>
          </template>
          {{ t('databases.newPasswordConfirm') }}
        </n-popconfirm>
        <n-popconfirm @positive-click="remove(d)">
          <template #trigger>
            <n-button size="small" class="tint-rose" :disabled="busy === d.id">{{ t('databases.remove') }}</n-button>
          </template>
          {{ t('databases.removeConfirm', { name: d.name }) }}
        </n-popconfirm>
      </n-space>
    </section>

    <n-modal :show="grant !== null" preset="card" :title="t('databases.dialogTitle')" style="max-width: 480px" @update:show="grant = null">
      <template v-if="grant && info">
        <p class="note">{{ t('databases.once') }}</p>
        <dl class="creds">
          <dt>{{ t('databases.host') }}</dt>
          <dd><code>{{ info.host }}</code></dd>
          <dt>{{ t('databases.port') }}</dt>
          <dd><code>{{ info.ports[grant.db.engine] }}</code></dd>
          <dt>{{ t('databases.login') }}</dt>
          <dd>
            <code data-testid="db-login">{{ grant.db.name }}</code>
            <n-button size="tiny" class="tint-violet" @click="copyText(grant.db.name)">
              <template #icon><n-icon :component="CopyOutline" /></template>
              {{ t('common.copy') }}
            </n-button>
          </dd>
          <dt>{{ t('databases.password') }}</dt>
          <dd>
            <code data-testid="db-password" class="pw">{{ grant.password }}</code>
            <n-button size="tiny" class="tint-violet" @click="copyText(grant.password)">
              <template #icon><n-icon :component="CopyOutline" /></template>
              {{ t('common.copy') }}
            </n-button>
          </dd>
        </dl>
      </template>
    </n-modal>
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

.note.err {
  color: #fda4af;
}

.card {
  padding: 18px 22px 20px;
  display: grid;
  gap: 12px;
}

.form {
  max-width: 460px;
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
  background: var(--grad-violet);
}

.dbname {
  font-size: 15px;
  font-weight: 700;
}

.bar {
  height: 8px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.08);
  overflow: hidden;
}

.fill {
  display: block;
  height: 100%;
  border-radius: 999px;
  background: var(--grad-emerald);
  transition: width 0.6s ease;
}

.fill.hot {
  background: var(--grad-amber);
}

.addrs {
  display: grid;
  gap: 8px;
  justify-items: start;
}

.addrs :deep(.n-input) {
  width: 100%;
}

.lbl {
  font-weight: 650;
}

.creds {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 10px 16px;
  margin: 14px 0;
  padding: 14px 16px;
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border);
}

.creds dt {
  color: var(--text-faint);
}

.creds dd {
  margin: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  word-break: break-all;
}

.pw {
  font-size: 15px;
  letter-spacing: 0.04em;
  color: #fde68a;
  background: rgba(251, 191, 36, 0.1);
  border-color: rgba(251, 191, 36, 0.35);
}
</style>
