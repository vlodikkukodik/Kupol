<script setup lang="ts">
import { ArchiveOutline, CloudDownloadOutline, RefreshOutline, SaveOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NIcon, NPopconfirm, useMessage } from 'naive-ui'
import { onMounted, ref } from 'vue'
import { api, ApiError, apiBlob } from '@/api/client'
import { backupResponseSchema, backupsSchema, siteResponseSchema, type Backup, type Site } from '@/api/schemas'
import EmptyState from '@/components/EmptyState.vue'
import { formatBytes, formatDateTime, useI18n } from '@/i18n'
import { useSitesStore } from '@/stores/sites'

const props = defineProps<{ site: Site }>()
const { t, locale } = useI18n()
const store = useSitesStore()
const message = useMessage()

const items = ref<Backup[]>([])
const loading = ref(true)
const creating = ref(false)
const restoring = ref(0)
const error = ref('')

const base = () => `/api/sites/${props.site.id}/backups`

async function load() {
  if (!store.backupsAvailable) {
    loading.value = false
    return
  }
  error.value = ''
  try {
    const r = await api(base(), { schema: backupsSchema })
    items.value = r.backups
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : t('backups.loadFailed')
  } finally {
    loading.value = false
  }
}

async function create() {
  creating.value = true
  try {
    await api(base(), { method: 'POST', body: {}, schema: backupResponseSchema })
    message.success(t('backups.created'))
    await load()
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : t('backups.createFailed'))
  } finally {
    creating.value = false
  }
}

async function download(url: string, name: string, fail: string) {
  try {
    const blob = await apiBlob(url)
    const href = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = href
    a.download = name
    a.click()
    URL.revokeObjectURL(href)
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : fail)
  }
}

function downloadBackup(b: Backup) {
  void download(`${base()}/${b.id}/download`, `${props.site.host}-${b.day}.zip`, t('backups.downloadFailed'))
}

function downloadArchive() {
  void download(`/api/sites/${props.site.id}/archive`, `${props.site.host}.zip`, t('backups.archiveFailed'))
}

async function restore(b: Backup) {
  restoring.value = b.id
  try {
    await api(`${base()}/${b.id}/restore`, { method: 'POST', body: {}, schema: siteResponseSchema })
    message.success(t('backups.restored'))
    await store.load(t('sites.loadFailed'))
  } catch (e) {
    message.error(e instanceof ApiError ? e.message : t('backups.restoreFailed'))
  } finally {
    restoring.value = 0
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <header class="head rise">
      <h1>{{ t('siteArea.backups') }}</h1>
      <p>{{ t('backups.hint') }}</p>
    </header>

    <n-alert v-if="!store.backupsAvailable" type="info" :show-icon="false">{{ t('backups.unavailable') }}</n-alert>

    <template v-else>
      <section class="bar glass rise" style="--i: 1">
        <n-button class="tint-violet" :loading="creating" @click="create">
          <template #icon><n-icon :component="SaveOutline" /></template>
          {{ t('backups.create') }}
        </n-button>
        <n-button class="tint-cyan" @click="downloadArchive">
          <template #icon><n-icon :component="ArchiveOutline" /></template>
          {{ t('backups.archive') }}
        </n-button>
        <span class="grow" />
        <n-button class="tint-emerald" @click="load">
          <template #icon><n-icon :component="RefreshOutline" /></template>
          {{ t('backups.refresh') }}
        </n-button>
      </section>

      <n-alert v-if="error" type="error" :show-icon="false">
        {{ error }} <n-button size="tiny" @click="load">{{ t('common.retry') }}</n-button>
      </n-alert>

      <div v-if="loading" class="skeleton" style="height: 220px" />

      <empty-state v-else-if="!items.length && !error" :title="t('backups.empty')" :hint="t('backups.emptyHint')" class="glass" />

      <section v-else class="table glass rise" style="--i: 2">
        <div class="scroll">
          <table>
            <thead>
              <tr>
                <th>{{ t('backups.cols.date') }}</th>
                <th class="num">{{ t('backups.cols.size') }}</th>
                <th class="num">{{ t('backups.cols.files') }}</th>
                <th class="right">{{ t('backups.cols.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="b in items" :key="b.id">
                <td class="nowrap" :title="formatDateTime(b.taken_at, locale)">
                  {{ new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date(b.day + 'T00:00:00Z')) }}
                </td>
                <td class="num nowrap">{{ formatBytes(b.bytes, locale) }}</td>
                <td class="num">{{ b.files }}</td>
                <td class="acts">
                  <n-button size="small" class="tint-cyan" @click="downloadBackup(b)">
                    <template #icon><n-icon :component="CloudDownloadOutline" /></template>
                    {{ t('backups.download') }}
                  </n-button>
                  <n-popconfirm
                    :positive-text="t('backups.restore')"
                    :negative-text="t('common.cancel')"
                    @positive-click="restore(b)"
                  >
                    <template #trigger>
                      <n-button size="small" class="tint-rose" :loading="restoring === b.id">
                        {{ t('backups.restore') }}
                      </n-button>
                    </template>
                    {{ t('backups.restoreConfirm', { date: b.day }) }}
                  </n-popconfirm>
                </td>
              </tr>
            </tbody>
          </table>
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

.grow {
  flex: 1;
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
  vertical-align: middle;
}

.num {
  text-align: right;
}

.right {
  text-align: right;
}

.nowrap {
  white-space: nowrap;
}

.acts {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}
</style>
