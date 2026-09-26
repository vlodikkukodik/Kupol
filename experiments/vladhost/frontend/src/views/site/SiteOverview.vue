<script setup lang="ts">
import { CloudUploadOutline, OpenOutline, RefreshOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NIcon, useMessage } from 'naive-ui'
import { ref } from 'vue'
import { api, ApiError } from '@/api/client'
import { siteResponseSchema, type Site } from '@/api/schemas'
import StatusChip from '@/components/StatusChip.vue'
import { formatBytes, formatDateTime, useI18n } from '@/i18n'
import { useSitesStore } from '@/stores/sites'

const props = defineProps<{ site: Site }>()
const { t, locale } = useI18n()
const store = useSitesStore()
const message = useMessage()
const busy = ref(false)

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

async function deploy(file: File | undefined) {
  if (!file) return
  if (!file.name.toLowerCase().endsWith('.zip')) {
    message.error(t('sites.zipOnly'))
    return
  }
  busy.value = true
  try {
    const body = new FormData()
    body.append('file', file)
    await api(`/api/sites/${props.site.id}/deploy`, { method: 'POST', body, schema: siteResponseSchema })
    message.success(t('sites.deployed'))
    await store.load(t('sites.loadFailed'))
  } catch (e) {
    message.error(errText(e, t('sites.deployFailed')))
  } finally {
    busy.value = false
  }
}

function onPick(ev: Event) {
  const input = ev.target as HTMLInputElement
  void deploy(input.files?.[0])
  input.value = '' // чтобы тот же файл можно было выбрать повторно
}

async function retryCert() {
  busy.value = true
  try {
    await api(`/api/sites/${props.site.id}/cert/retry`, { method: 'POST', schema: siteResponseSchema })
    await store.load(t('sites.loadFailed'))
  } catch (e) {
    message.error(errText(e, t('sites.cert.retryFailed')))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="page">
    <header class="head rise">
      <h1>{{ t('siteArea.overview') }}</h1>
      <p>{{ t('siteArea.overviewHint') }}</p>
    </header>

    <n-alert v-if="site.cert_status === 'failed'" type="error" :show-icon="false">
      {{ t('sites.cert.failedBody', { reason: site.cert_error || t('sites.cert.unknownReason') }) }}
      <n-button size="tiny" :loading="busy" @click="retryCert">
        <template #icon><n-icon :component="RefreshOutline" /></template>
        {{ t('common.retry') }}
      </n-button>
    </n-alert>

    <section class="glass card rise" style="--i: 1">
      <div class="row">
        <span class="k">{{ t('siteArea.address') }}</span>
        <!-- Пока сертификата нет, адрес открыть нельзя: http перенаправляет на https. -->
        <a v-if="site.cert_status === 'active' || site.cert_status === 'none'" :href="site.url" target="_blank" rel="noopener" class="host">
          {{ site.host }} <n-icon :component="OpenOutline" />
        </a>
        <span v-else class="host">{{ site.host }}</span>
      </div>
      <div class="row">
        <span class="k">{{ t('siteArea.state') }}</span>
        <span class="chips">
          <status-chip v-if="site.cert_status === 'pending'" tone="amber" pulse>{{ t('sites.cert.pending') }}</status-chip>
          <status-chip v-else-if="site.cert_status === 'failed'" tone="rose">{{ t('sites.cert.failed') }}</status-chip>
          <status-chip v-else-if="site.cert_status === 'active'" tone="cyan">{{ t('sites.cert.active') }}</status-chip>
          <status-chip :tone="site.status === 'live' ? 'emerald' : 'slate'">
            {{ site.status === 'live' ? t('sites.status.live') : t('sites.status.empty') }}
          </status-chip>
        </span>
      </div>
      <div class="row">
        <span class="k">{{ t('siteArea.disk') }}</span>
        <span>{{ formatBytes(site.disk_bytes, locale) }}</span>
      </div>
      <div class="row">
        <span class="k">{{ t('siteArea.deployed') }}</span>
        <span>{{ site.deployed_at ? formatDateTime(site.deployed_at, locale) : '—' }}</span>
      </div>
    </section>

    <section class="glass card rise" style="--i: 2">
      <h3>{{ t('siteArea.publish') }}</h3>
      <p class="note">{{ t('sites.uploadHint') }}</p>
      <n-button type="primary" :loading="busy" tag="label">
        <template #icon><n-icon :component="CloudUploadOutline" /></template>
        {{ t('sites.uploadZip') }}
        <input type="file" accept=".zip,application/zip" class="file" @change="onPick">
      </n-button>
    </section>
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
  margin: 4px 0 12px;
  color: var(--text-dim);
}

.card {
  padding: 20px 24px;
}

.card h3 {
  font-size: 17px;
}

.row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 10px 0;
  border-bottom: 1px solid var(--border);
}

.row:last-child {
  border-bottom: 0;
}

.k {
  width: 140px;
  flex: none;
  color: var(--text-dim);
}

.host {
  font-weight: 700;
  overflow-wrap: anywhere;
}

.chips {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.file {
  display: none;
}
</style>
