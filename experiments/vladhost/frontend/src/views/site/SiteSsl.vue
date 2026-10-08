<script setup lang="ts">
import { LockClosedOutline, RefreshOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NIcon, NPopconfirm, useMessage } from 'naive-ui'
import { computed, ref } from 'vue'
import { api, ApiError } from '@/api/client'
import { domainResponseSchema, siteResponseSchema, type CertInfo, type Site } from '@/api/schemas'
import StatusChip from '@/components/StatusChip.vue'
import { formatDateTime, useI18n } from '@/i18n'
import { CERT_WARN_DAYS, daysUntil } from '@/lib/summary'
import { useSitesStore } from '@/stores/sites'

const props = defineProps<{ site: Site }>()
const { t, locale } = useI18n()
const store = useSitesStore()
const message = useMessage()

type State = 'active' | 'pending' | 'waiting' | 'failed' | 'none'
interface Row {
  host: string
  kind: 'site' | 'sub' | 'custom'
  state: State
  error: string
  cert: CertInfo | null
  renewAt: string | null
  domainId: number | null
}

// Все имена сайта: его адрес, поддомены и свои домены. Статусы сайта и доменов приводятся к одному виду.
const rows = computed<Row[]>(() => {
  const s = props.site
  const site: Row = {
    host: s.host,
    kind: 'site',
    state: s.cert_status === 'active' ? 'active' : s.cert_status === 'pending' ? 'pending' : s.cert_status === 'failed' ? 'failed' : 'none',
    error: s.cert_error,
    cert: s.cert,
    renewAt: s.cert_renew_at,
    domainId: null,
  }
  const domains = s.domains.map<Row>((d) => ({
    host: d.host,
    kind: d.kind === 'sub' ? 'sub' : 'custom',
    state: d.status === 'active' ? 'active' : d.status === 'pending_cert' ? 'pending' : d.status === 'pending_dns' ? 'waiting' : 'failed',
    error: d.error,
    cert: d.cert,
    renewAt: d.cert_renew_at,
    domainId: d.id,
  }))
  return [site, ...domains]
})

const issuing = computed(() => props.site.cert_status !== 'none') // выпуск сертификатов на сервере включён

const kindKey = { site: 'ssl.kind.site', sub: 'ssl.kind.sub', custom: 'ssl.kind.custom' } as const
const stateKey = {
  active: 'ssl.state.active',
  pending: 'ssl.state.pending',
  waiting: 'ssl.state.waiting',
  failed: 'ssl.state.failed',
  none: 'ssl.state.none',
} as const
const stateTone = { active: 'emerald', pending: 'amber', waiting: 'amber', failed: 'rose', none: 'slate' } as const

// Срок: обычные сертификаты живут 90 суток и продлеваются за 30 — поэтому предупреждаем не при 30, а при 14 и меньше.
function expiry(c: CertInfo): { days: number; tone: 'emerald' | 'amber' | 'rose' } {
  const days = daysUntil(c.not_after)
  return { days, tone: days <= 7 ? 'rose' : days <= CERT_WARN_DAYS ? 'amber' : 'emerald' }
}

const cooling = (r: Row) => !!r.renewAt && Date.parse(r.renewAt) > Date.now()
const canRenew = (r: Row) => r.state === 'active' && !cooling(r)

const busy = ref<string | null>(null)
const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)
const reload = () => store.load(t('sites.loadFailed'))

async function renew(r: Row) {
  busy.value = r.host
  try {
    await api(`/api/sites/${props.site.id}/certs/renew`, { method: 'POST', body: { host: r.host } })
    message.success(t('ssl.renewRequested'))
    await reload()
  } catch (e) {
    message.error(errText(e, t('ssl.renewFailed')))
  } finally {
    busy.value = null
  }
}

// Повтор неудавшегося выпуска: у адреса сайта и у домена разные точки входа.
async function retry(r: Row) {
  busy.value = r.host
  try {
    if (r.domainId === null) await api(`/api/sites/${props.site.id}/cert/retry`, { method: 'POST', schema: siteResponseSchema })
    else await api(`/api/sites/${props.site.id}/domains/${r.domainId}/check`, { method: 'POST', schema: domainResponseSchema })
    await reload()
  } catch (e) {
    message.error(errText(e, t('sites.cert.retryFailed')))
  } finally {
    busy.value = null
  }
}
</script>

<template>
  <div class="page">
    <header class="head rise">
      <h1>{{ t('siteArea.ssl') }}</h1>
      <p>{{ t('ssl.hint') }}</p>
    </header>

    <n-alert v-if="!issuing" type="info" :show-icon="false">{{ t('ssl.unavailable') }}</n-alert>

    <ul v-else class="list">
      <li v-for="(r, i) in rows" :key="r.host" class="cert glass rise" :style="{ '--i': i + 1 }">
        <div class="top">
          <span class="ic"><n-icon :size="18" :component="LockClosedOutline" /></span>
          <div class="who">
            <a v-if="r.state === 'active'" :href="`https://${r.host}`" target="_blank" rel="noopener" class="host">{{ r.host }}</a>
            <span v-else class="host">{{ r.host }}</span>
            <span class="kind">{{ t(kindKey[r.kind]) }}</span>
          </div>
          <status-chip :tone="stateTone[r.state]" :pulse="r.state === 'pending'">{{ t(stateKey[r.state]) }}</status-chip>
        </div>

        <dl v-if="r.cert" class="info">
          <dt>{{ t('ssl.validUntil') }}</dt>
          <dd>
            {{ formatDateTime(r.cert.not_after, locale) }}
            <status-chip :tone="expiry(r.cert).tone">
              {{ expiry(r.cert).days >= 0 ? t('ssl.daysLeft', { n: expiry(r.cert).days }) : t('ssl.expired') }}
            </status-chip>
          </dd>
          <dt>{{ t('ssl.issuedAt') }}</dt>
          <dd>{{ formatDateTime(r.cert.not_before, locale) }}</dd>
          <dt>{{ t('ssl.issuer') }}</dt>
          <dd>{{ r.cert.issuer || '—' }}</dd>
          <template v-if="r.cert.names.length">
            <dt>{{ t('ssl.names') }}</dt>
            <dd class="names">{{ r.cert.names.join(', ') }}</dd>
          </template>
        </dl>
        <p v-else-if="r.state === 'active'" class="note">{{ t('ssl.noInfo') }}</p>
        <p v-else-if="r.state === 'waiting'" class="note">{{ t('ssl.waitingDns') }}</p>

        <n-alert v-if="r.error && r.state === 'failed'" type="error" :show-icon="false" class="gap">
          {{ t('sites.cert.failedBody', { reason: r.error }) }}
        </n-alert>
        <n-alert v-else-if="r.error && r.state === 'active'" type="warning" :show-icon="false" class="gap">
          {{ t('ssl.renewFailedBody', { reason: r.error }) }}
        </n-alert>

        <div class="actions">
          <span v-if="r.state === 'active'" class="note">{{ t('ssl.autoRenew') }}</span>
          <span class="grow" />
          <n-button v-if="r.state === 'failed'" size="small" class="tint-cyan" :loading="busy === r.host" @click="retry(r)">
            <template #icon><n-icon :component="RefreshOutline" /></template>
            {{ t('common.retry') }}
          </n-button>
          <n-popconfirm v-if="r.state === 'active'" @positive-click="renew(r)">
            <template #trigger>
              <n-button size="small" class="tint-violet" :disabled="!canRenew(r) || busy === r.host" :loading="busy === r.host">
                <template #icon><n-icon :component="RefreshOutline" /></template>
                {{ t('ssl.renew') }}
              </n-button>
            </template>
            {{ t('ssl.renewConfirm') }}
          </n-popconfirm>
        </div>
        <p v-if="r.state === 'active' && cooling(r) && r.renewAt" class="note cool">{{ t('ssl.cooldown', { date: formatDateTime(r.renewAt, locale) }) }}</p>
      </li>
    </ul>
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

.head p,
.note {
  margin: 4px 0 0;
  color: var(--text-dim);
  font-size: 13.5px;
}

.list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: 14px;
}

.cert {
  padding: 18px 22px;
}

.top {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
}

.ic {
  display: inline-grid;
  place-items: center;
  width: 36px;
  height: 36px;
  border-radius: 11px;
  color: #fff;
  background: var(--grad-emerald);
}

.who {
  flex: 1;
  min-width: 200px;
  display: grid;
}

.host {
  font-weight: 700;
  font-size: 16px;
  overflow-wrap: anywhere;
}

.kind {
  color: var(--text-dim);
  font-size: 12.5px;
}

.info {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 8px 18px;
  margin: 14px 0 0;
  font-size: 14px;
}

.info dt {
  color: var(--text-faint);
}

.info dd {
  margin: 0;
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.names {
  overflow-wrap: anywhere;
}

.gap {
  margin-top: 12px;
}

.actions {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 14px;
}

.grow {
  flex: 1;
}

.cool {
  text-align: right;
}
</style>
