<script setup lang="ts">
import { CheckmarkCircleOutline, CloseCircleOutline, CopyOutline, EllipseOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NForm, NFormItem, NIcon, NInput, NModal, NSelect, NSpin, useMessage } from 'naive-ui'
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import { cmsForm, cmsJobResponseSchema, cmsStatusResponseSchema, fieldErrors, type CmsJob, type CmsStatus, type Site } from '@/api/schemas'
import StatusChip from '@/components/StatusChip.vue'
import { formatDateTime, resolveMessage, useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import { useSitesStore } from '@/stores/sites'

const props = defineProps<{ site: Site }>()
const { t, locale } = useI18n()
const auth = useAuthStore()
const store = useSitesStore()
const message = useMessage()

const base = computed(() => `/api/sites/${props.site.id}/cms`)
const status = ref<CmsStatus | null>(null)
const job = ref<CmsJob | null>(null)
const loadError = ref('')
const starting = ref(false)
const errors = ref<Record<string, string>>({})
const form = reactive({ cms: 'wordpress', title: '', adminUser: auth.user?.username ?? '', adminEmail: auth.user?.email ?? '', locale: locale.value === 'it' ? 'it_IT' : 'ru_RU' })
// Пароли приходят один раз: показываются окном, пока пользователь его не закроет.
const credentials = ref<NonNullable<CmsJob['result']> | null>(null)

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

async function loadStatus() {
  try {
    status.value = (await api(base.value, { schema: cmsStatusResponseSchema })).cms
    loadError.value = ''
  } catch (e) {
    loadError.value = errText(e, t('cms.loadFailed'))
  }
}

// Ход установки: пока идёт, опрашиваем. Итог с паролями читается ровно один раз, поэтому запоминаем его сразу.
let timer: ReturnType<typeof setTimeout> | undefined
async function poll() {
  try {
    const j = (await api(`${base.value}/job`, { schema: cmsJobResponseSchema })).job
    if (j) job.value = j
    if (j?.result) credentials.value = j.result
    if (j?.status === 'running') {
      timer = setTimeout(poll, 1500)
      return
    }
    await loadStatus()
    void store.load(t('sites.loadFailed'))
  } catch (e) {
    loadError.value = errText(e, t('cms.loadFailed'))
  }
}

onMounted(async () => {
  await loadStatus()
  await poll()
})
onBeforeUnmount(() => clearTimeout(timer))

const installing = computed(() => job.value?.status === 'running')
const canInstall = computed(() => !!status.value && status.value.requirements.empty && status.value.requirements.database && status.value.requirements.php)
const localeOptions = computed(() =>
  (status.value?.locales ?? []).map((l) => ({ label: localeName(l), value: l })),
)
const appOptions = computed(() => (status.value?.catalog ?? []).map((a) => ({ label: `${a.name} — ${t('cms.version', { v: a.version })}`, value: a.id })))

function localeName(l: string): string {
  switch (l) {
    case 'ru_RU':
      return t('lang.ru')
    case 'it_IT':
      return t('lang.it')
    default:
      return t('cms.locales.en_US')
  }
}

function stepLabel(s: string): string {
  switch (s) {
    case 'runtime':
      return t('cms.stepLabels.runtime')
    case 'database':
      return t('cms.stepLabels.database')
    case 'download':
      return t('cms.stepLabels.download')
    case 'files':
      return t('cms.stepLabels.files')
    case 'config':
      return t('cms.stepLabels.config')
    case 'install':
      return t('cms.stepLabels.install')
    default:
      return t('cms.stepLabels.verify')
  }
}

// Состояние шага в списке: сделан, идёт, ждёт, сорвался.
function stepState(s: string): 'done' | 'active' | 'wait' | 'failed' {
  const j = job.value
  if (!j) return 'wait'
  const idx = j.steps.indexOf(s)
  const cur = j.steps.indexOf(j.step)
  if (j.status === 'done') return 'done'
  if (idx < cur) return 'done'
  if (idx === cur) return j.status === 'failed' ? 'failed' : 'active'
  return 'wait'
}

async function install() {
  const parsed = cmsForm.safeParse({ title: form.title, admin_user: form.adminUser, admin_email: form.adminEmail })
  errors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  starting.value = true
  try {
    await api(base.value, {
      method: 'POST',
      body: { cms: form.cms, title: parsed.data.title, admin_user: parsed.data.admin_user, admin_email: parsed.data.admin_email, locale: form.locale },
    })
    job.value = { status: 'running', step: 'runtime', steps: ['runtime', 'database', 'download', 'files', 'config', 'install', 'verify'] }
    void poll()
  } catch (e) {
    if (e instanceof ApiError && e.field) errors.value = { [e.field === 'admin_user' ? 'adminUser' : e.field === 'admin_email' ? 'adminEmail' : e.field]: e.message }
    else message.error(errText(e, t('cms.startFailed')))
  } finally {
    starting.value = false
  }
}

function retry() {
  job.value = null
  void loadStatus()
}

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
      <h1>{{ t('cms.title') }}</h1>
      <p class="note">{{ t('cms.hint') }}</p>
    </header>

    <n-alert v-if="loadError" type="error" :show-icon="false">{{ loadError }}</n-alert>
    <n-alert v-if="status && !status.available" type="info" :show-icon="false">{{ t('cms.unavailable') }}</n-alert>

    <template v-if="status && status.available">
      <!-- уже установлено -->
      <section v-if="status.installed && !installing && !credentials" class="glass card rise" style="--i: 1" data-testid="cms-installed">
        <h3>{{ t('cms.installedTitle', { name: status.installed.name, version: status.installed.version }) }}</h3>
        <p class="note">
          {{ t('cms.installedAt', { date: formatDateTime(status.installed.installed_at, locale) }) }} · {{ t('cms.installedDb', { db: status.installed.db_name }) }}
        </p>
        <div class="links">
          <a class="btn plain" :href="status.installed.url" target="_blank" rel="noopener noreferrer">{{ t('cms.openSite') }}</a>
          <a class="btn plain" :href="status.installed.admin_url" target="_blank" rel="noopener noreferrer">{{ t('cms.openAdmin') }}</a>
        </div>
        <p class="note">{{ t('cms.installedHint') }}</p>
      </section>

      <!-- идёт установка или она сорвалась -->
      <section v-else-if="job && (job.status === 'running' || job.status === 'failed')" class="glass card rise" style="--i: 1" data-testid="cms-progress">
        <div class="row">
          <h3>{{ job.status === 'running' ? t('cms.progress') : t('cms.failedTitle') }}</h3>
          <n-spin v-if="job.status === 'running'" :size="18" />
        </div>
        <p v-if="job.status === 'running'" class="note">{{ t('cms.progressHint') }}</p>
        <ol class="steps">
          <li v-for="s in job.steps" :key="s" :class="stepState(s)" :data-step="s">
            <n-icon :size="20" :component="stepState(s) === 'done' ? CheckmarkCircleOutline : stepState(s) === 'failed' ? CloseCircleOutline : EllipseOutline" />
            <span>{{ stepLabel(s) }}</span>
          </li>
        </ol>
        <template v-if="job.status === 'failed'">
          <n-alert type="error" :show-icon="false" data-testid="cms-failure">{{ job.failure?.message }}</n-alert>
          <p class="note">{{ t('cms.failedHint') }}</p>
          <n-button type="primary" @click="retry">{{ t('cms.retry') }}</n-button>
        </template>
      </section>

      <!-- форма установки -->
      <section v-else-if="!status.installed" class="glass card rise" style="--i: 1">
        <n-form class="form" @submit.prevent="install">
          <n-form-item :label="t('cms.app')" :show-feedback="false">
            <n-select v-model:value="form.cms" :options="appOptions" :aria-label="t('cms.app')" />
          </n-form-item>
          <n-form-item :label="t('cms.siteTitle')" :validation-status="errors.title ? 'error' : undefined" :feedback="errors.title ? resolveMessage(errors.title) : undefined">
            <n-input v-model:value="form.title" :placeholder="t('cms.siteTitlePlaceholder')" :input-props="{ 'aria-label': t('cms.siteTitle') }" @update:value="errors.title = ''" />
          </n-form-item>
          <n-form-item :label="t('cms.adminUser')" :validation-status="errors.adminUser || errors.admin_user ? 'error' : undefined" :feedback="resolveMessage(errors.adminUser ?? errors.admin_user ?? '') || undefined">
            <n-input v-model:value="form.adminUser" autocomplete="off" :input-props="{ 'aria-label': t('cms.adminUser') }" @update:value="errors.adminUser = ''" />
          </n-form-item>
          <n-form-item :label="t('cms.adminEmail')" :validation-status="errors.adminEmail || errors.admin_email ? 'error' : undefined" :feedback="resolveMessage(errors.adminEmail ?? errors.admin_email ?? '') || undefined">
            <n-input v-model:value="form.adminEmail" autocomplete="off" :input-props="{ 'aria-label': t('cms.adminEmail') }" @update:value="errors.adminEmail = ''" />
          </n-form-item>
          <n-form-item :label="t('cms.locale')" :show-feedback="false">
            <n-select v-model:value="form.locale" :options="localeOptions" :aria-label="t('cms.locale')" style="max-width: 220px" />
          </n-form-item>

          <div class="reqs">
            <strong>{{ t('cms.requirements') }}</strong>
            <div class="chips">
              <status-chip :tone="status.requirements.php ? 'emerald' : 'rose'">{{ t('cms.reqPhp') }}</status-chip>
              <status-chip :tone="status.requirements.database ? 'emerald' : 'rose'">{{ t('cms.reqDatabase') }}</status-chip>
              <status-chip :tone="status.requirements.empty ? 'emerald' : 'rose'">{{ t('cms.reqEmpty') }}</status-chip>
            </div>
            <p v-if="!status.requirements.empty" class="note">{{ t('cms.reqEmptyHint') }}</p>
            <p v-if="!status.requirements.database" class="note">{{ t('cms.reqDatabaseHint') }}</p>
          </div>

          <n-button type="primary" attr-type="submit" :loading="starting" :disabled="!canInstall">{{ t('cms.install') }}</n-button>
        </n-form>
      </section>
    </template>

    <n-modal :show="credentials !== null" preset="card" :title="t('cms.doneTitle')" style="max-width: 520px" :mask-closable="false" @update:show="credentials = null">
      <template v-if="credentials">
        <p class="note">{{ t('cms.doneHint') }}</p>
        <dl class="creds">
          <dt>{{ t('cms.adminLogin') }}</dt>
          <dd>
            <code data-testid="cms-admin-user">{{ credentials.admin_user }}</code>
            <n-button size="tiny" class="tint-violet" @click="copyText(credentials.admin_user)">
              <template #icon><n-icon :component="CopyOutline" /></template>
              {{ t('common.copy') }}
            </n-button>
          </dd>
          <dt>{{ t('cms.adminPassword') }}</dt>
          <dd>
            <code class="pw" data-testid="cms-admin-password">{{ credentials.admin_password }}</code>
            <n-button size="tiny" class="tint-violet" @click="copyText(credentials.admin_password)">
              <template #icon><n-icon :component="CopyOutline" /></template>
              {{ t('common.copy') }}
            </n-button>
          </dd>
          <dt>{{ t('cms.dbName') }}</dt>
          <dd><code>{{ credentials.db_name }}</code></dd>
          <dt>{{ t('cms.dbPassword') }}</dt>
          <dd>
            <code class="pw">{{ credentials.db_password }}</code>
            <n-button size="tiny" class="tint-violet" @click="copyText(credentials.db_password)">
              <template #icon><n-icon :component="CopyOutline" /></template>
              {{ t('common.copy') }}
            </n-button>
          </dd>
        </dl>
        <div class="links">
          <a class="btn plain" :href="credentials.url" target="_blank" rel="noopener noreferrer">{{ t('cms.openSite') }}</a>
          <a class="btn plain" :href="credentials.admin_url" target="_blank" rel="noopener noreferrer">{{ t('cms.openAdmin') }}</a>
        </div>
        <n-button type="primary" data-testid="cms-close" @click="credentials = null">{{ t('cms.close') }}</n-button>
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

.card {
  padding: 18px 22px 20px;
  display: grid;
  gap: 12px;
}

.card h3 {
  font-size: 17px;
}

.row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.form {
  max-width: 480px;
}

.reqs {
  display: grid;
  gap: 8px;
  margin: 4px 0 16px;
}

.chips {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.steps {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 8px;
}

.steps li {
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--text-dim);
  transition: color 0.3s;
}

.steps li.done {
  color: #6ee7b7;
}

.steps li.active {
  color: #fff;
  font-weight: 650;
}

.steps li.failed {
  color: #fda4af;
  font-weight: 650;
}

.links {
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
  margin: 8px 0 14px;
}

.btn {
  display: inline-block;
  padding: 7px 16px;
  border-radius: 12px;
  border: 1px solid var(--border);
  background: rgba(255, 255, 255, 0.07);
  color: var(--text);
  font-weight: 650;
  transition: background 0.25s;
}

.btn:hover {
  background: rgba(255, 255, 255, 0.13);
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
