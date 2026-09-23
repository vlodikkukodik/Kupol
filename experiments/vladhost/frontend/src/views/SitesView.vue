<script setup lang="ts">
import { NAlert, NButton, NCard, NEmpty, NForm, NFormItem, NInput, NModal, NPopconfirm, NProgress, NSpace, NTag, useMessage } from 'naive-ui'
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError } from '@/api/client'
import { fieldErrors, ftpGrantSchema, siteForm, siteResponseSchema, sitesSchema, type Site } from '@/api/schemas'
import { formatBytes } from '@/lib/format'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const message = useMessage()
const router = useRouter()

const sites = ref<Site[]>([])
const limits = ref({ max_sites: 1, disk_quota_bytes: 0 })
const loading = ref(true)
const loadError = ref('')

const form = reactive({ slug: '' })
const errors = ref<Record<string, string>>({})
const creating = ref(false)
const busyId = ref<number | null>(null)

const canCreate = computed(() => sites.value.length < limits.value.max_sites)
const used = computed(() => sites.value.reduce((sum, s) => sum + s.disk_bytes, 0))
const usedPercent = computed(() =>
  limits.value.disk_quota_bytes ? Math.min(100, Math.round((used.value / limits.value.disk_quota_bytes) * 100)) : 0,
)
const hostPreview = computed(() => `${form.slug.trim().toLowerCase() || 'сайт'}.${auth.user?.username}.vladinc.ru`)

const fmtDate = (iso: string) => new Date(iso).toLocaleString('ru-RU')
const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

async function load() {
  loadError.value = ''
  try {
    const r = await api('/api/sites', { schema: sitesSchema })
    sites.value = r.sites
    limits.value = r.limits
  } catch (e) {
    loadError.value = errText(e, 'Не удалось загрузить сайты')
  } finally {
    loading.value = false
  }
}

async function create() {
  const parsed = siteForm.safeParse(form)
  errors.value = parsed.success ? {} : fieldErrors(parsed.error)
  if (!parsed.success) return
  creating.value = true
  try {
    await api('/api/sites', { method: 'POST', body: parsed.data, schema: siteResponseSchema })
    form.slug = ''
    await load()
  } catch (e) {
    if (e instanceof ApiError && e.field) errors.value = { [e.field]: e.message }
    else message.error(errText(e, 'Не удалось создать сайт'))
  } finally {
    creating.value = false
  }
}

async function deploy(site: Site, file: File | undefined) {
  if (!file) return
  if (!file.name.toLowerCase().endsWith('.zip')) {
    message.error('Нужен архив в формате .zip')
    return
  }
  busyId.value = site.id
  try {
    const body = new FormData()
    body.append('file', file)
    await api(`/api/sites/${site.id}/deploy`, { method: 'POST', body, schema: siteResponseSchema })
    message.success('Сайт обновлён')
    await load()
  } catch (e) {
    message.error(errText(e, 'Не удалось загрузить сайт'))
  } finally {
    busyId.value = null
  }
}

function onPick(site: Site, ev: Event) {
  const input = ev.target as HTMLInputElement
  void deploy(site, input.files?.[0])
  input.value = '' // чтобы тот же файл можно было выбрать повторно
}

async function retryCert(site: Site) {
  busyId.value = site.id
  try {
    await api(`/api/sites/${site.id}/cert/retry`, { method: 'POST', schema: siteResponseSchema })
    await load()
  } catch (e) {
    message.error(errText(e, 'Не удалось повторить выпуск'))
  } finally {
    busyId.value = null
  }
}

// Выданный FTP-пароль: показывается один раз, на сервере остаётся только хеш.
const grant = ref<{ site: Site; password: string } | null>(null)

async function enableFtp(site: Site) {
  busyId.value = site.id
  try {
    const r = await api(`/api/sites/${site.id}/ftp`, { method: 'POST', schema: ftpGrantSchema })
    grant.value = { site: r.site, password: r.password }
    await load()
  } catch (e) {
    message.error(errText(e, 'Не удалось включить FTP'))
  } finally {
    busyId.value = null
  }
}

async function disableFtp(site: Site) {
  busyId.value = site.id
  try {
    await api(`/api/sites/${site.id}/ftp`, { method: 'DELETE', schema: siteResponseSchema })
    await load()
  } catch (e) {
    message.error(errText(e, 'Не удалось отключить FTP'))
  } finally {
    busyId.value = null
  }
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success('Скопировано')
  } catch {
    message.error('Не удалось скопировать — выделите текст вручную')
  }
}

async function remove(site: Site) {
  busyId.value = site.id
  try {
    await api(`/api/sites/${site.id}`, { method: 'DELETE' })
    await load()
  } catch (e) {
    message.error(errText(e, 'Не удалось удалить сайт'))
  } finally {
    busyId.value = null
  }
}

// Пока сертификат выпускается, обновляем список сам — иначе пришлось бы перезагружать страницу.
let poll: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  poll = setInterval(() => {
    if (sites.value.some((s) => s.cert_status === 'pending')) void load()
  }, 5000)
})
onBeforeUnmount(() => clearInterval(poll))
</script>

<template>
  <div class="page">
    <h2>Сайты</h2>

    <n-alert v-if="loadError" type="error" :show-icon="false" class="gap">
      {{ loadError }} <n-button size="tiny" @click="load">Повторить</n-button>
    </n-alert>

    <n-card size="small" class="gap" title="Диск">
      <n-progress type="line" :percentage="usedPercent" :show-indicator="false" />
      <div class="muted">{{ formatBytes(used) }} из {{ formatBytes(limits.disk_quota_bytes) }}</div>
    </n-card>

    <n-card v-for="s in sites" :key="s.id" size="small" class="gap">
      <template #header>
        <!-- Пока сертификата нет, адрес открыть нельзя: http перенаправляет на https. -->
        <a v-if="s.cert_status === 'active' || s.cert_status === 'none'" :href="s.url" target="_blank" rel="noopener">{{ s.host }}</a>
        <span v-else>{{ s.host }}</span>
      </template>
      <template #header-extra>
        <n-space :size="6">
          <n-tag v-if="s.cert_status === 'pending'" type="info" size="small" round>HTTPS выпускается…</n-tag>
          <n-tag v-else-if="s.cert_status === 'failed'" type="error" size="small" round>HTTPS не выпущен</n-tag>
          <n-tag :type="s.status === 'live' ? 'success' : 'default'" size="small" round>
            {{ s.status === 'live' ? 'опубликован' : 'пустой' }}
          </n-tag>
        </n-space>
      </template>
      <n-alert v-if="s.cert_status === 'failed'" type="error" :show-icon="false" class="gap">
        Не удалось выпустить сертификат: {{ s.cert_error || 'неизвестная ошибка' }}
        <n-button size="tiny" :loading="busyId === s.id" @click="retryCert(s)">Повторить</n-button>
      </n-alert>
      <div class="muted">
        <template v-if="s.deployed_at">Обновлён {{ fmtDate(s.deployed_at) }}, {{ formatBytes(s.disk_bytes) }}</template>
        <template v-else>Загрузите zip с index.html в корне архива</template>
      </div>
      <div v-if="s.ftp.available" class="ftp">
        <template v-if="s.ftp.enabled">
          <div class="muted">
            FTPS: <code>{{ s.ftp.host }}</code>, порт <code>{{ s.ftp.port }}</code>, логин <code>{{ s.ftp.username }}</code>
          </div>
          <n-space class="actions">
            <n-popconfirm @positive-click="enableFtp(s)">
              <template #trigger>
                <n-button size="tiny" :disabled="busyId === s.id">Новый FTP-пароль</n-button>
              </template>
              Выдать новый пароль? Старый перестанет работать, открытые FTP-сессии закроются.
            </n-popconfirm>
            <n-button size="tiny" quaternary type="error" :disabled="busyId === s.id" @click="disableFtp(s)">Отключить FTP</n-button>
          </n-space>
        </template>
        <n-button v-else size="tiny" :loading="busyId === s.id" @click="enableFtp(s)">Включить FTP</n-button>
      </div>
      <n-space class="actions">
        <n-button size="small" type="primary" :loading="busyId === s.id" tag="label">
          Загрузить zip
          <input type="file" accept=".zip,application/zip" class="file" @change="onPick(s, $event)">
        </n-button>
        <n-button size="small" @click="router.push({ name: 'files', params: { id: s.id } })">Файлы</n-button>
        <n-popconfirm @positive-click="remove(s)">
          <template #trigger>
            <n-button size="small" quaternary type="error" :disabled="busyId === s.id">Удалить</n-button>
          </template>
          Удалить сайт {{ s.host }} и все его файлы?
        </n-popconfirm>
      </n-space>
    </n-card>

    <n-modal :show="grant !== null" preset="card" title="Доступ по FTP" style="max-width: 460px" @update:show="grant = null">
      <template v-if="grant">
        <p class="note">Пароль показывается один раз — сохраните его сейчас. Потерянный пароль можно заменить новым.</p>
        <dl class="creds">
          <dt>Сервер</dt>
          <dd><code>{{ grant.site.ftp.host }}</code></dd>
          <dt>Порт</dt>
          <dd><code>{{ grant.site.ftp.port }}</code></dd>
          <dt>Логин</dt>
          <dd>
            <code>{{ grant.site.ftp.username }}</code>
            <n-button size="tiny" quaternary @click="copyText(grant.site.ftp.username ?? '')">Копировать</n-button>
          </dd>
          <dt>Пароль</dt>
          <dd>
            <code data-testid="ftp-password">{{ grant.password }}</code>
            <n-button size="tiny" quaternary @click="copyText(grant.password)">Копировать</n-button>
          </dd>
        </dl>
        <p class="note">
          Подключение: FTPS (явный TLS, «FTP поверх TLS»), пассивный режим. Обычный FTP без шифрования сервер не принимает.
        </p>
      </template>
    </n-modal>

    <n-empty v-if="!loading && !sites.length" description="Сайтов пока нет" class="gap" />

    <n-card v-if="canCreate" title="Новый сайт" size="small">
      <n-form @submit.prevent="create">
        <n-form-item
          label="Имя сайта"
          :validation-status="errors.slug ? 'error' : undefined"
          :feedback="errors.slug ?? `Адрес: ${hostPreview}`"
        >
          <n-input v-model:value="form.slug" placeholder="blog" autocomplete="off" :input-props="{ 'aria-label': 'Имя сайта' }" />
        </n-form-item>
        <n-button type="primary" attr-type="submit" :loading="creating">Создать</n-button>
      </n-form>
    </n-card>
    <n-alert v-else-if="!loading" type="info" :show-icon="false">
      Достигнут лимит: сайтов на аккаунт — {{ limits.max_sites }}.
    </n-alert>
  </div>
</template>

<style scoped>
.page { max-width: 720px; }
h2 { margin: 0 0 16px; }
.gap { margin-bottom: 16px; }
.muted { opacity: 0.7; font-size: 13px; margin-top: 8px; }
.actions { margin-top: 12px; }
.ftp { margin-top: 12px; }
.note { font-size: 13px; opacity: 0.8; }
.creds { display: grid; grid-template-columns: max-content 1fr; gap: 6px 16px; margin: 12px 0; }
.creds dt { opacity: 0.7; }
.creds dd { margin: 0; display: flex; align-items: center; gap: 8px; word-break: break-all; }
.file { display: none; }
</style>
