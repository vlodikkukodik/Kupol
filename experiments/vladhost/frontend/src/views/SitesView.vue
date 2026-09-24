<script setup lang="ts">
import {
  AddOutline,
  CloudUploadOutline,
  CopyOutline,
  FolderOpenOutline,
  GlobeOutline,
  KeyOutline,
  LinkOutline,
  RefreshOutline,
  TrashOutline,
} from '@vicons/ionicons5'
import { NAlert, NButton, NForm, NFormItem, NIcon, NInput, NModal, NPopconfirm, NSpace, useMessage } from 'naive-ui'
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError } from '@/api/client'
import {
  domainForm, domainResponseSchema, fieldErrors, ftpGrantSchema, siteForm, siteResponseSchema, sitesSchema, type Domain, type Site,
} from '@/api/schemas'
import EmptyState from '@/components/EmptyState.vue'
import StatusChip from '@/components/StatusChip.vue'
import { formatBytes, formatDateTime, resolveMessage, useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const { t, locale } = useI18n()
const auth = useAuthStore()
const message = useMessage()
const router = useRouter()

const sites = ref<Site[]>([])
const limits = ref({ max_sites: 1, disk_quota_bytes: 0 })
const domainConfig = ref({ available: false, server_ips: [] as string[], per_site: 0 })
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
const hostPreview = computed(() => `${form.slug.trim().toLowerCase() || '…'}.${auth.user?.username}.vladinc.ru`)

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

async function load() {
  loadError.value = ''
  try {
    const r = await api('/api/sites', { schema: sitesSchema })
    sites.value = r.sites
    limits.value = r.limits
    domainConfig.value = r.domain_config
  } catch (e) {
    loadError.value = errText(e, t('sites.loadFailed'))
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
    else message.error(errText(e, t('sites.createFailed')))
  } finally {
    creating.value = false
  }
}

async function deploy(site: Site, file: File | undefined) {
  if (!file) return
  if (!file.name.toLowerCase().endsWith('.zip')) {
    message.error(t('sites.zipOnly'))
    return
  }
  busyId.value = site.id
  try {
    const body = new FormData()
    body.append('file', file)
    await api(`/api/sites/${site.id}/deploy`, { method: 'POST', body, schema: siteResponseSchema })
    message.success(t('sites.deployed'))
    await load()
  } catch (e) {
    message.error(errText(e, t('sites.deployFailed')))
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
    message.error(errText(e, t('sites.cert.retryFailed')))
  } finally {
    busyId.value = null
  }
}

// --- свои домены ---
const newDomain = reactive<Record<number, string>>({})
const domainErrors = ref<Record<number, string>>({})
const serverIp = computed(() => domainConfig.value.server_ips.join(', '))

async function addDomain(site: Site) {
  const parsed = domainForm.safeParse({ host: newDomain[site.id] ?? '' })
  if (!parsed.success) {
    domainErrors.value = { ...domainErrors.value, [site.id]: fieldErrors(parsed.error).host ?? '' }
    return
  }
  domainErrors.value = { ...domainErrors.value, [site.id]: '' }
  busyId.value = site.id
  try {
    await api(`/api/sites/${site.id}/domains`, { method: 'POST', body: { host: parsed.data.host }, schema: domainResponseSchema })
    newDomain[site.id] = ''
    message.success(t('sites.domains.added'))
    await load()
  } catch (e) {
    if (e instanceof ApiError && e.field === 'host') domainErrors.value = { ...domainErrors.value, [site.id]: e.message }
    else message.error(errText(e, t('sites.domains.addFailed')))
  } finally {
    busyId.value = null
  }
}

const domainBusy = ref<number | null>(null)

async function checkDomain(site: Site, d: Domain) {
  domainBusy.value = d.id
  try {
    await api(`/api/sites/${site.id}/domains/${d.id}/check`, { method: 'POST', schema: domainResponseSchema })
    await load()
  } catch (e) {
    message.error(errText(e, t('sites.domains.checkFailed')))
  } finally {
    domainBusy.value = null
  }
}

async function removeDomain(site: Site, d: Domain) {
  domainBusy.value = d.id
  try {
    await api(`/api/sites/${site.id}/domains/${d.id}`, { method: 'DELETE' })
    await load()
  } catch (e) {
    message.error(errText(e, t('sites.domains.removeFailed')))
  } finally {
    domainBusy.value = null
  }
}

const domainTone = { pending_dns: 'amber', pending_cert: 'cyan', active: 'emerald', failed: 'rose' } as const
const domainStatusKey = {
  pending_dns: 'sites.domains.status.pendingDns',
  pending_cert: 'sites.domains.status.pendingCert',
  active: 'sites.domains.status.active',
  failed: 'sites.domains.status.failed',
} as const
const problemKey = {
  no_a: 'sites.domains.problem.noA',
  wrong_ip: 'sites.domains.problem.wrongIp',
  has_aaaa: 'sites.domains.problem.hasAaaa',
  lookup: 'sites.domains.problem.lookup',
} as const

function domainProblem(d: Domain): string {
  const key = problemKey[d.problem as keyof typeof problemKey]
  return key ? t(key, { found: d.found.join(', '), ip: serverIp.value }) : ''
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
    message.error(errText(e, t('sites.ftp.enableFailed')))
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
    message.error(errText(e, t('sites.ftp.disableFailed')))
  } finally {
    busyId.value = null
  }
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success(t('common.copied'))
  } catch {
    message.error(t('common.copyFailed'))
  }
}

async function remove(site: Site) {
  busyId.value = site.id
  try {
    await api(`/api/sites/${site.id}`, { method: 'DELETE' })
    await load()
  } catch (e) {
    message.error(errText(e, t('sites.deleteFailed')))
  } finally {
    busyId.value = null
  }
}

// Пока сертификат выпускается, обновляем список сам — иначе пришлось бы перезагружать страницу.
let poll: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void load()
  poll = setInterval(() => {
    const waiting = sites.value.some(
      (s) => s.cert_status === 'pending' || s.domains.some((d) => d.status === 'pending_dns' || d.status === 'pending_cert'),
    )
    if (waiting) void load()
  }, 5000)
})
onBeforeUnmount(() => clearInterval(poll))
</script>

<template>
  <div class="page">
    <header class="head rise">
      <div>
        <h1>{{ t('sites.title') }}</h1>
        <p>{{ t('sites.subtitle') }}</p>
      </div>
    </header>

    <n-alert v-if="loadError" type="error" :show-icon="false" class="gap">
      {{ loadError }} <n-button size="tiny" @click="load">{{ t('common.retry') }}</n-button>
    </n-alert>

    <section class="disk glass rise" style="--i: 1">
      <div class="disk-top">
        <span class="disk-title">{{ t('sites.disk') }}</span>
        <span class="disk-val">{{ t('sites.diskUsed', { used: formatBytes(used, locale), total: formatBytes(limits.disk_quota_bytes, locale) }) }}</span>
      </div>
      <div class="meter"><span :style="{ width: `${Math.max(usedPercent, used > 0 ? 2 : 0)}%` }" /></div>
    </section>

    <div v-if="loading" class="skeletons">
      <div class="skeleton" style="height: 168px" />
    </div>

    <transition-group v-else name="list" tag="div" class="sites">
      <article v-for="s in sites" :key="s.id" class="site glass lift">
        <div class="site-head">
          <span class="globe"><n-icon :size="22" :component="GlobeOutline" /></span>
          <div class="titles">
            <!-- Пока сертификата нет, адрес открыть нельзя: http перенаправляет на https. -->
            <a v-if="s.cert_status === 'active' || s.cert_status === 'none'" :href="s.url" target="_blank" rel="noopener" class="host">{{ s.host }}</a>
            <span v-else class="host">{{ s.host }}</span>
            <div class="meta">
              <template v-if="s.deployed_at">
                {{ t('sites.updated', { date: formatDateTime(s.deployed_at, locale), size: formatBytes(s.disk_bytes, locale) }) }}
              </template>
              <template v-else>{{ t('sites.uploadHint') }}</template>
            </div>
          </div>
          <div class="chips">
            <status-chip v-if="s.cert_status === 'pending'" tone="amber" pulse>{{ t('sites.cert.pending') }}</status-chip>
            <status-chip v-else-if="s.cert_status === 'failed'" tone="rose">{{ t('sites.cert.failed') }}</status-chip>
            <status-chip v-else-if="s.cert_status === 'active'" tone="cyan">{{ t('sites.cert.active') }}</status-chip>
            <status-chip :tone="s.status === 'live' ? 'emerald' : 'slate'">
              {{ s.status === 'live' ? t('sites.status.live') : t('sites.status.empty') }}
            </status-chip>
          </div>
        </div>

        <n-alert v-if="s.cert_status === 'failed'" type="error" :show-icon="false" class="gap">
          {{ t('sites.cert.failedBody', { reason: s.cert_error || t('sites.cert.unknownReason') }) }}
          <n-button size="tiny" :loading="busyId === s.id" @click="retryCert(s)">
            <template #icon><n-icon :component="RefreshOutline" /></template>
            {{ t('common.retry') }}
          </n-button>
        </n-alert>

        <div v-if="s.ftp.available" class="ftp">
          <span class="ftp-ic"><n-icon :size="18" :component="KeyOutline" /></span>
          <div class="ftp-body">
            <strong>{{ t('sites.ftp.title') }}</strong>
            <span v-if="s.ftp.enabled" class="ftp-line">
              {{ t('sites.ftp.connection', { host: s.ftp.host ?? '', port: s.ftp.port ?? 0, user: s.ftp.username ?? '' }) }}
            </span>
          </div>
          <n-space v-if="s.ftp.enabled" :size="8">
            <n-popconfirm @positive-click="enableFtp(s)">
              <template #trigger>
                <n-button size="small" class="tint-violet" :disabled="busyId === s.id">{{ t('sites.ftp.newPassword') }}</n-button>
              </template>
              {{ t('sites.ftp.newPasswordConfirm') }}
            </n-popconfirm>
            <n-button size="small" class="tint-rose" :disabled="busyId === s.id" @click="disableFtp(s)">{{ t('sites.ftp.disable') }}</n-button>
          </n-space>
          <n-button v-else size="small" class="tint-violet" :loading="busyId === s.id" @click="enableFtp(s)">{{ t('sites.ftp.enable') }}</n-button>
        </div>

        <div v-if="domainConfig.available" class="domains">
          <div class="dom-head">
            <span class="dom-ic"><n-icon :size="18" :component="LinkOutline" /></span>
            <strong>{{ t('sites.domains.title') }}</strong>
          </div>
          <p class="note">{{ t('sites.domains.hint', { ip: serverIp }) }}</p>

          <ul v-if="s.domains.length" class="dom-list">
            <li v-for="d in s.domains" :key="d.id" class="dom-row">
              <div class="dom-main">
                <a v-if="d.status === 'active'" :href="`https://${d.host}`" target="_blank" rel="noopener" class="dom-host">{{ d.host }}</a>
                <span v-else class="dom-host">{{ d.host }}</span>
                <status-chip :tone="domainTone[d.status]" :pulse="d.status === 'pending_dns' || d.status === 'pending_cert'">
                  {{ t(domainStatusKey[d.status]) }}
                </status-chip>
                <span class="grow" />
                <n-button v-if="d.status === 'pending_dns' || d.status === 'failed'" size="small" class="tint-cyan" :loading="domainBusy === d.id" @click="checkDomain(s, d)">
                  <template #icon><n-icon :component="RefreshOutline" /></template>
                  {{ t('sites.domains.check') }}
                </n-button>
                <n-popconfirm @positive-click="removeDomain(s, d)">
                  <template #trigger>
                    <n-button size="small" class="tint-rose" :disabled="domainBusy === d.id">{{ t('sites.domains.remove') }}</n-button>
                  </template>
                  {{ t('sites.domains.removeConfirm', { host: d.host }) }}
                </n-popconfirm>
              </div>
              <template v-if="d.status === 'pending_dns'">
                <div class="dom-todo">
                  <code>{{ t('sites.domains.instruction', { host: d.host, ip: serverIp }) }}</code>
                  <n-button size="tiny" class="tint-violet" @click="copyText(domainConfig.server_ips[0] ?? '')">
                    <template #icon><n-icon :component="CopyOutline" /></template>
                    {{ t('sites.domains.copyIp') }}
                  </n-button>
                </div>
                <p v-if="domainProblem(d)" class="dom-problem">{{ domainProblem(d) }}</p>
                <p class="note">{{ t('sites.domains.ttlNote') }}</p>
              </template>
              <p v-else-if="d.status === 'failed'" class="dom-problem">{{ t('sites.domains.problem.failedBody', { reason: d.error || t('sites.cert.unknownReason') }) }}</p>
            </li>
          </ul>
          <p v-else class="note">{{ t('sites.domains.empty') }}</p>

          <n-form class="dom-form" @submit.prevent="addDomain(s)">
            <n-form-item
              :label="t('sites.domains.label')"
              :validation-status="domainErrors[s.id] ? 'error' : undefined"
              :feedback="domainErrors[s.id] ? resolveMessage(domainErrors[s.id] ?? '') : t('sites.domains.wwwHint')"
            >
              <n-input v-model:value="newDomain[s.id]" :placeholder="t('sites.domains.placeholder')" autocomplete="off" :input-props="{ 'aria-label': t('sites.domains.label') }" />
            </n-form-item>
            <n-button type="primary" attr-type="submit" :loading="busyId === s.id && !!newDomain[s.id]">
              <template #icon><n-icon :component="AddOutline" /></template>
              {{ t('sites.domains.add') }}
            </n-button>
          </n-form>
        </div>

        <n-space class="actions" :size="10">
          <n-button type="primary" :loading="busyId === s.id" tag="label">
            <template #icon><n-icon :component="CloudUploadOutline" /></template>
            {{ t('sites.uploadZip') }}
            <input type="file" accept=".zip,application/zip" class="file" @change="onPick(s, $event)">
          </n-button>
          <n-button class="tint-cyan" @click="router.push({ name: 'files', params: { id: s.id } })">
            <template #icon><n-icon :component="FolderOpenOutline" /></template>
            {{ t('sites.files') }}
          </n-button>
          <n-popconfirm @positive-click="remove(s)">
            <template #trigger>
              <n-button class="tint-rose" :disabled="busyId === s.id">
                <template #icon><n-icon :component="TrashOutline" /></template>
                {{ t('sites.deleteSite') }}
              </n-button>
            </template>
            {{ t('sites.deleteConfirm', { host: s.host }) }}
          </n-popconfirm>
        </n-space>
      </article>
    </transition-group>

    <empty-state v-if="!loading && !sites.length" :title="t('sites.emptyTitle')" :hint="t('sites.emptyHint')" class="glass" />

    <section v-if="canCreate && !loading" class="new glass rise">
      <h3>{{ t('sites.newTitle') }}</h3>
      <n-form @submit.prevent="create">
        <n-form-item
          :label="t('sites.name')"
          :validation-status="errors.slug ? 'error' : undefined"
          :feedback="errors.slug ? resolveMessage(errors.slug) : t('sites.addressPreview', { host: hostPreview })"
        >
          <n-input v-model:value="form.slug" size="large" :placeholder="t('sites.namePlaceholder')" autocomplete="off" :input-props="{ 'aria-label': t('sites.name') }" />
        </n-form-item>
        <n-button type="primary" size="large" attr-type="submit" :loading="creating">
          <template #icon><n-icon :component="AddOutline" /></template>
          {{ t('common.create') }}
        </n-button>
      </n-form>
    </section>
    <n-alert v-else-if="!loading && sites.length" type="info" :show-icon="false">
      {{ t('sites.limitReached', { max: limits.max_sites }) }}
    </n-alert>

    <n-modal :show="grant !== null" preset="card" :title="t('sites.ftp.dialogTitle')" style="max-width: 480px" @update:show="grant = null">
      <template v-if="grant">
        <p class="note">{{ t('sites.ftp.once') }}</p>
        <dl class="creds">
          <dt>{{ t('sites.ftp.server') }}</dt>
          <dd><code>{{ grant.site.ftp.host }}</code></dd>
          <dt>{{ t('sites.ftp.port') }}</dt>
          <dd><code>{{ grant.site.ftp.port }}</code></dd>
          <dt>{{ t('sites.ftp.login') }}</dt>
          <dd>
            <code>{{ grant.site.ftp.username }}</code>
            <n-button size="tiny" class="tint-violet" @click="copyText(grant.site.ftp.username ?? '')">
              <template #icon><n-icon :component="CopyOutline" /></template>
              {{ t('common.copy') }}
            </n-button>
          </dd>
          <dt>{{ t('sites.ftp.password') }}</dt>
          <dd>
            <code data-testid="ftp-password" class="pw">{{ grant.password }}</code>
            <n-button size="tiny" class="tint-violet" @click="copyText(grant.password)">
              <template #icon><n-icon :component="CopyOutline" /></template>
              {{ t('common.copy') }}
            </n-button>
          </dd>
        </dl>
        <p class="note">{{ t(grant.site.ftp.allow_plain ? 'sites.ftp.howTo' : 'sites.ftp.howToSecure') }}</p>
      </template>
    </n-modal>
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

.gap {
  margin-bottom: 4px;
}

.disk {
  padding: 18px 22px;
}

.disk-top {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
  font-weight: 650;
}

.disk-val {
  color: var(--text-dim);
  font-weight: 500;
}

.meter {
  height: 9px;
  border-radius: 99px;
  background: rgba(255, 255, 255, 0.09);
  overflow: hidden;
}

.meter span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--grad-primary);
  box-shadow: 0 0 16px rgba(139, 92, 246, 0.75);
  transition: width 0.9s var(--ease);
}

.sites {
  position: relative;
  display: grid;
  gap: 18px;
}

.site {
  position: relative;
  padding: 22px 24px 20px;
  overflow: hidden;
}

/* Цветная полоска слева — фирменный акцент карточки */
.site::before {
  content: '';
  position: absolute;
  inset: 0 auto 0 0;
  width: 4px;
  background: var(--grad-primary);
}

.site-head {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-wrap: wrap;
}

.globe {
  display: inline-grid;
  place-items: center;
  width: 48px;
  height: 48px;
  flex: none;
  border-radius: 15px;
  color: #fff;
  background: var(--grad-cyan);
  box-shadow: 0 12px 26px -10px rgba(6, 182, 212, 0.9);
  transition: transform 0.4s var(--ease);
}

.titles {
  flex: 1;
  min-width: 200px;
}

.host {
  font-size: 18px;
  font-weight: 750;
  letter-spacing: -0.015em;
  overflow-wrap: anywhere;
}

span.host {
  color: var(--text);
}

.meta {
  margin-top: 2px;
  color: var(--text-dim);
  font-size: 13.5px;
}

.chips {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.ftp {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
  margin-top: 16px;
  padding: 12px 14px;
  border-radius: 14px;
  background: rgba(167, 139, 250, 0.08);
  border: 1px solid rgba(167, 139, 250, 0.22);
}

.ftp-ic {
  display: inline-grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 10px;
  color: #fff;
  background: var(--grad-violet);
}

.ftp-body {
  flex: 1;
  min-width: 180px;
  display: grid;
}

.ftp-line {
  color: var(--text-dim);
  font-size: 13px;
  word-break: break-word;
}

.actions {
  margin-top: 16px;
}

.domains {
  margin-top: 16px;
  padding: 14px 16px;
  border-radius: 14px;
  background: rgba(34, 211, 238, 0.06);
  border: 1px solid rgba(34, 211, 238, 0.22);
}

.dom-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.dom-ic {
  display: inline-grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 10px;
  color: #fff;
  background: var(--grad-cyan);
}

.dom-list {
  list-style: none;
  margin: 10px 0;
  padding: 0;
  display: grid;
  gap: 10px;
}

.dom-row {
  padding: 10px 12px;
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border);
}

.dom-main {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.dom-host {
  font-weight: 700;
  overflow-wrap: anywhere;
}

.grow {
  flex: 1;
}

.dom-todo {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 8px;
}

.dom-problem {
  margin: 8px 0 0;
  font-size: 13.5px;
  color: #fcd34d;
}

.dom-form {
  margin-top: 8px;
  max-width: 420px;
}

.file {
  display: none;
}

.new {
  padding: 22px 24px;
}

.new h3 {
  margin-bottom: 14px;
  font-size: 17px;
}

.note {
  font-size: 13.5px;
  color: var(--text-dim);
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
