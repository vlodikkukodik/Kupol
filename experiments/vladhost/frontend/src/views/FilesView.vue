<script setup lang="ts">
import {
  AddOutline,
  ArrowBack,
  ArrowUpOutline,
  ChevronForward,
  CloudUploadOutline,
  CreateOutline,
  DocumentTextOutline,
  FolderOutline,
  SaveOutline,
  ShieldCheckmarkOutline,
  TrashOutline,
} from '@vicons/ionicons5'
import { NAlert, NButton, NDataTable, NIcon, NInput, NModal, NPopconfirm, NSpace, useDialog, useMessage, type DataTableColumns } from 'naive-ui'
import { computed, h, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError, api } from '@/api/client'
import { filesApi, type FileEntry, type HtaccessReport } from '@/api/files'
import { sitesSchema, type Site } from '@/api/schemas'
import CodeEditor from '@/components/CodeEditor.vue'
import EmptyState from '@/components/EmptyState.vue'
import FileTypeIcon from '@/components/FileTypeIcon.vue'
import StatusChip from '@/components/StatusChip.vue'
import { formatBytes, formatDateTime, resolveMessage, useI18n, type MessageKey } from '@/i18n'
import { baseName, breadcrumbs, joinPath, parentPath, validateName } from '@/lib/paths'

const { t, locale } = useI18n()
const route = useRoute()
const router = useRouter()
const message = useMessage()
const dialog = useDialog()

const siteId = computed(() => Number(route.params.id))
const site = ref<Site | null>(null)
const dir = ref(typeof route.query.path === 'string' ? route.query.path : '')
const entries = ref<FileEntry[]>([])
const loading = ref(true)
const loadError = ref('')

// Проверка .htaccess: шлюз выполняет только часть директив Apache, остальные молча игнорируются.
const hasHtaccess = computed(() => entries.value.some((e) => e.name === '.htaccess' && !e.is_dir))
const htaccessReport = ref<HtaccessReport | null>(null)
const diagKey: Record<string, MessageKey> = {
  unsupported: 'files.htaccess.unsupported',
  syntax: 'files.htaccess.syntax',
  regex: 'files.htaccess.regex',
  too_big: 'files.htaccess.tooBig',
  too_many: 'files.htaccess.tooMany',
}

async function checkHtaccess() {
  try {
    htaccessReport.value = await filesApi.htaccess(siteId.value)
  } catch (e) {
    message.error(errText(e, t('files.htaccess.loadFailed')))
  }
}

// Открытый в редакторе файл.
const openPath = ref<string | null>(null)
const original = ref('')
const content = ref('')
const saving = ref(false)
const dirty = computed(() => openPath.value !== null && content.value !== original.value)

// Диалог ввода имени (новый файл / папка / переименование).
const prompt = ref<{ title: string; value: string; error: string; run: (name: string) => Promise<void> } | null>(null)
const fileInput = ref<HTMLInputElement>()

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)
const crumbs = computed(() => breadcrumbs(dir.value, site.value?.host ?? t('files.rootFallback')))

async function loadSite() {
  const r = await api('/api/sites', { schema: sitesSchema })
  site.value = r.sites.find((s) => s.id === siteId.value) ?? null
}

async function loadDir() {
  loading.value = true
  loadError.value = ''
  try {
    entries.value = await filesApi.list(siteId.value, dir.value)
  } catch (e) {
    entries.value = []
    loadError.value = errText(e, t('files.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function confirmDiscard(): Promise<boolean> {
  if (!dirty.value) return true
  return new Promise((resolve) => {
    dialog.warning({
      title: t('files.discardTitle'),
      content: t('files.discardBody', { name: baseName(openPath.value ?? '') }),
      positiveText: t('files.discard'),
      negativeText: t('files.stay'),
      onPositiveClick: () => resolve(true),
      onNegativeClick: () => resolve(false),
      onClose: () => resolve(false),
    })
  })
}

async function go(path: string) {
  if (!(await confirmDiscard())) return
  openPath.value = null
  dir.value = path
  void router.replace({ query: path ? { path } : {} })
  await loadDir()
}

async function open(entry: FileEntry) {
  const path = joinPath(dir.value, entry.name)
  if (entry.is_dir) return go(path)
  if (!(await confirmDiscard())) return
  try {
    const text = await filesApi.read(siteId.value, path)
    openPath.value = path
    original.value = content.value = text
  } catch (e) {
    message.error(errText(e, t('files.openFailed')))
  }
}

async function save() {
  if (openPath.value === null || saving.value) return
  saving.value = true
  try {
    await filesApi.save(siteId.value, openPath.value, content.value)
    original.value = content.value
    message.success(t('files.saved'))
    await Promise.all([loadDir(), loadSite()])
  } catch (e) {
    message.error(errText(e, t('files.saveFailed')))
  } finally {
    saving.value = false
  }
}

async function closeEditor() {
  if (await confirmDiscard()) openPath.value = null
}

function ask(title: string, value: string, run: (name: string) => Promise<void>) {
  prompt.value = { title, value, error: '', run }
}

async function submitPrompt() {
  const p = prompt.value
  if (!p) return
  const err = validateName(p.value)
  if (err) {
    p.error = t(err)
    return
  }
  try {
    await p.run(p.value.trim())
    prompt.value = null
    await Promise.all([loadDir(), loadSite()])
  } catch (e) {
    p.error = errText(e, t('files.opFailed'))
  }
}

const newFile = () =>
  ask(t('files.dialogNewFile'), '', async (name) => {
    const path = joinPath(dir.value, name)
    await filesApi.save(siteId.value, path, '')
    openPath.value = path
    original.value = content.value = ''
  })
const newFolder = () => ask(t('files.dialogNewFolder'), '', (name) => filesApi.mkdir(siteId.value, joinPath(dir.value, name)))
const rename = (e: FileEntry) =>
  ask(t('files.renameTitle', { name: e.name }), e.name, async (name) => {
    const from = joinPath(dir.value, e.name)
    await filesApi.rename(siteId.value, from, joinPath(dir.value, name))
    if (openPath.value === from) openPath.value = null
  })

async function remove(e: FileEntry) {
  const path = joinPath(dir.value, e.name)
  try {
    await filesApi.remove(siteId.value, path)
    if (openPath.value === path || openPath.value?.startsWith(path + '/')) openPath.value = null
    await Promise.all([loadDir(), loadSite()])
  } catch (err) {
    message.error(errText(err, t('files.deleteFailed')))
  }
}

async function onUpload(ev: Event) {
  const input = ev.target as HTMLInputElement
  const files = Array.from(input.files ?? [])
  input.value = ''
  for (const f of files) {
    try {
      await filesApi.upload(siteId.value, dir.value, f)
    } catch (e) {
      message.error(`${f.name}: ${errText(e, t('files.uploadFailed', { name: f.name }))}`)
      break
    }
  }
  if (files.length) await Promise.all([loadDir(), loadSite()])
}

// Колонки пересчитываются при смене языка: заголовки и подписи кнопок берутся из каталога.
const columns = computed<DataTableColumns<FileEntry>>(() => [
  {
    title: t('files.columns.name'),
    key: 'name',
    render: (e) =>
      h('span', { class: 'namecell' }, [
        h(FileTypeIcon, { name: e.name, dir: e.is_dir }),
        h('a', { href: '#', class: 'name plain', onClick: (ev: Event) => (ev.preventDefault(), void open(e)) }, e.name),
      ]),
  },
  { title: t('files.columns.size'), key: 'size', width: 110, render: (e) => (e.is_dir ? '' : formatBytes(e.size, locale.value)) },
  { title: t('files.columns.modified'), key: 'mod_time', width: 190, render: (e) => formatDateTime(e.mod_time, locale.value) },
  {
    title: '',
    key: 'actions',
    width: 250,
    render: (e) =>
      h(NSpace, { size: 6, wrapItem: false, justify: 'end' }, () => [
        h(
          NButton,
          { size: 'tiny', class: 'tint-violet', onClick: () => rename(e) },
          { default: () => t('files.rename'), icon: () => h(NIcon, null, { default: () => h(CreateOutline) }) },
        ),
        h(
          NPopconfirm,
          { onPositiveClick: () => remove(e) },
          {
            trigger: () =>
              h(
                NButton,
                { size: 'tiny', class: 'tint-rose' },
                { default: () => t('files.delete'), icon: () => h(NIcon, null, { default: () => h(TrashOutline) }) },
              ),
            default: () => t(e.is_dir ? 'files.deleteConfirmFolder' : 'files.deleteConfirm', { name: e.name }),
          },
        ),
      ]),
  },
])

// Защита от закрытия вкладки с несохранёнными правками.
const beforeUnload = (ev: BeforeUnloadEvent) => {
  if (dirty.value) ev.preventDefault()
}

watch(siteId, () => location.reload())

onMounted(async () => {
  window.addEventListener('beforeunload', beforeUnload)
  try {
    await loadSite()
  } catch (e) {
    loadError.value = errText(e, t('files.loadSiteFailed'))
  }
  if (!site.value) {
    loading.value = false
    return
  }
  await loadDir()
})
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
</script>

<template>
  <div class="page">
    <header class="head rise">
      <n-button class="tint-cyan" @click="router.push({ name: 'sites' })">
        <template #icon><n-icon :component="ArrowBack" /></template>
        {{ t('files.back') }}
      </n-button>
      <h1>{{ site?.host ?? t('files.rootFallback') }}</h1>
    </header>

    <n-alert v-if="!loading && !site" type="error" :show-icon="false">{{ t('files.notFound') }}</n-alert>
    <template v-else>
      <n-alert v-if="loadError" type="error" :show-icon="false">{{ loadError }}</n-alert>

      <transition name="page">
        <section v-if="openPath !== null" class="editor glass">
          <div class="editor-bar">
            <span class="ed-ic"><n-icon :size="18" :component="DocumentTextOutline" /></span>
            <span class="ed-path">{{ openPath }}</span>
            <status-chip v-if="dirty" tone="amber" pulse>{{ t('files.unsaved') }}</status-chip>
            <span class="grow" />
            <n-space :size="8">
              <n-button type="primary" :loading="saving" :disabled="!dirty" @click="save">
                <template #icon><n-icon :component="SaveOutline" /></template>
                {{ t('files.saveButton') }}
              </n-button>
              <n-button @click="closeEditor">{{ t('files.close') }}</n-button>
            </n-space>
          </div>
          <code-editor v-model="content" :filename="openPath" @save="save" />
        </section>
      </transition>

      <section class="browser glass rise" style="--i: 1">
        <div class="toolbar">
          <nav class="crumbs">
            <template v-for="(c, i) in crumbs" :key="c.path">
              <n-icon v-if="i > 0" class="sep" :size="14" :component="ChevronForward" />
              <button type="button" class="crumb" :class="{ last: i === crumbs.length - 1 }" @click="go(c.path)">
                <n-icon v-if="i === 0" :size="15" :component="FolderOutline" />
                {{ c.name }}
              </button>
            </template>
          </nav>
          <n-space :size="8" class="tools">
            <n-button v-if="dir" size="small" @click="go(parentPath(dir))">
              <template #icon><n-icon :component="ArrowUpOutline" /></template>
              {{ t('files.up') }}
            </n-button>
            <n-button size="small" class="tint-emerald" @click="newFile">
              <template #icon><n-icon :component="AddOutline" /></template>
              {{ t('files.newFile') }}
            </n-button>
            <n-button size="small" class="tint-amber" @click="newFolder">
              <template #icon><n-icon :component="FolderOutline" /></template>
              {{ t('files.newFolder') }}
            </n-button>
            <n-button v-if="hasHtaccess" size="small" class="tint-pink" @click="checkHtaccess">
              <template #icon><n-icon :component="ShieldCheckmarkOutline" /></template>
              {{ t('files.htaccess.button') }}
            </n-button>
            <n-button size="small" type="primary" @click="fileInput?.click()">
              <template #icon><n-icon :component="CloudUploadOutline" /></template>
              {{ t('files.upload') }}
            </n-button>
          </n-space>
        </div>

        <input ref="fileInput" type="file" multiple class="file" @change="onUpload">
        <n-data-table v-if="entries.length" :columns="columns" :data="entries" :bordered="false" :loading="loading" />
        <empty-state v-else-if="!loading" :title="t('files.empty')" :hint="t('files.emptyHint')" />
      </section>
    </template>

    <n-modal :show="htaccessReport !== null" preset="card" :title="t('files.htaccess.title')" style="max-width: 620px" @update:show="htaccessReport = null">
      <template v-if="htaccessReport">
        <p class="note">{{ t('files.htaccess.hint') }}</p>
        <p v-if="!htaccessReport.length" class="note">{{ t('files.htaccess.none') }}</p>
        <div v-for="f in htaccessReport" :key="f.path" class="hta-file">
          <div class="hta-head">
            <code>{{ f.path }}</code>
            <status-chip v-if="!f.diags.length" tone="emerald">{{ t('files.htaccess.ok') }}</status-chip>
          </div>
          <ul v-if="f.diags.length" class="hta-list">
            <li v-for="(d, i) in f.diags" :key="i">
              <status-chip tone="amber">{{ d.line ? t('files.htaccess.line', { line: d.line }) : '—' }}</status-chip>
              {{ t(diagKey[d.code] ?? 'files.htaccess.unsupported', { directive: d.directive }) }}
            </li>
          </ul>
        </div>
      </template>
    </n-modal>

    <n-modal :show="prompt !== null" preset="card" :title="prompt?.title" style="max-width: 420px" @update:show="prompt = null">
      <template v-if="prompt">
        <n-input
          v-model:value="prompt.value"
          size="large"
          autofocus
          :input-props="{ 'aria-label': prompt.title }"
          :status="prompt.error ? 'error' : undefined"
          @keydown.enter="submitPrompt"
        />
        <div v-if="prompt.error" class="err">{{ resolveMessage(prompt.error) }}</div>
        <n-space justify="end" class="modal-actions">
          <n-button @click="prompt = null">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="submitPrompt">{{ t('files.apply') }}</n-button>
        </n-space>
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

.head {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-wrap: wrap;
}

.head h1 {
  font-size: clamp(20px, 2.6vw, 28px);
  font-weight: 800;
  word-break: break-all;
}

.editor {
  padding: 14px 14px 16px;
}

.editor-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  padding: 2px 4px 14px;
}

.ed-ic {
  display: inline-grid;
  place-items: center;
  width: 34px;
  height: 34px;
  border-radius: 10px;
  color: #fff;
  background: var(--grad-cyan);
}

.ed-path {
  font-family: var(--mono);
  font-size: 14px;
  font-weight: 600;
  word-break: break-all;
}

.grow {
  flex: 1;
}

.browser {
  padding: 16px 18px 12px;
}

.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}

.crumbs {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-wrap: wrap;
}

.sep {
  color: var(--text-faint);
}

.crumb {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 12px;
  border: 1px solid transparent;
  border-radius: 999px;
  background: transparent;
  color: var(--text-dim);
  font: inherit;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition:
    background 0.25s,
    color 0.25s,
    border-color 0.25s;
}

.crumb:hover {
  background: rgba(167, 139, 250, 0.16);
  color: #fff;
}

.crumb.last {
  color: #fff;
  background: rgba(255, 255, 255, 0.08);
  border-color: var(--border);
}

.file {
  display: none;
}

.err {
  color: var(--rose);
  font-size: 13px;
  margin-top: 8px;
}

.modal-actions {
  margin-top: 18px;
}

.note {
  font-size: 13.5px;
  color: var(--text-dim);
}

.hta-file {
  margin-top: 14px;
  padding: 12px 14px;
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--border);
}

.hta-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.hta-list {
  list-style: none;
  margin: 10px 0 0;
  padding: 0;
  display: grid;
  gap: 8px;
  font-size: 14px;
}

:deep(.namecell) {
  display: inline-flex;
  align-items: center;
  gap: 12px;
}

:deep(.name) {
  color: var(--text);
  font-weight: 600;
}

:deep(.name:hover) {
  color: #c4b5fd;
}
</style>
