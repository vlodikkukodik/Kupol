<script setup lang="ts">
import {
  NAlert, NBreadcrumb, NBreadcrumbItem, NButton, NCard, NDataTable, NEmpty, NInput, NModal, NPopconfirm, NSpace, NTag,
  useDialog, useMessage, type DataTableColumns,
} from 'naive-ui'
import { computed, h, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError, api } from '@/api/client'
import { filesApi, type FileEntry } from '@/api/files'
import { sitesSchema, type Site } from '@/api/schemas'
import CodeEditor from '@/components/CodeEditor.vue'
import { formatBytes } from '@/lib/format'
import { baseName, breadcrumbs, joinPath, parentPath, validateName } from '@/lib/paths'

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
const fmtDate = (iso: string) => new Date(iso).toLocaleString('ru-RU')
const crumbs = computed(() => breadcrumbs(dir.value, site.value?.host ?? 'сайт'))

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
    loadError.value = errText(e, 'Не удалось загрузить список файлов')
  } finally {
    loading.value = false
  }
}

async function confirmDiscard(): Promise<boolean> {
  if (!dirty.value) return true
  return new Promise((resolve) => {
    dialog.warning({
      title: 'Несохранённые изменения',
      content: `Изменения в «${baseName(openPath.value ?? '')}» будут потеряны.`,
      positiveText: 'Отбросить',
      negativeText: 'Остаться',
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
    message.error(errText(e, 'Не удалось открыть файл'))
  }
}

async function save() {
  if (openPath.value === null || saving.value) return
  saving.value = true
  try {
    await filesApi.save(siteId.value, openPath.value, content.value)
    original.value = content.value
    message.success('Сохранено')
    await Promise.all([loadDir(), loadSite()])
  } catch (e) {
    message.error(errText(e, 'Не удалось сохранить'))
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
    p.error = err
    return
  }
  try {
    await p.run(p.value.trim())
    prompt.value = null
    await Promise.all([loadDir(), loadSite()])
  } catch (e) {
    p.error = errText(e, 'Операция не удалась')
  }
}

const newFile = () =>
  ask('Новый файл', '', async (name) => {
    const path = joinPath(dir.value, name)
    await filesApi.save(siteId.value, path, '')
    openPath.value = path
    original.value = content.value = ''
  })
const newFolder = () => ask('Новая папка', '', (name) => filesApi.mkdir(siteId.value, joinPath(dir.value, name)))
const rename = (e: FileEntry) =>
  ask(`Переименовать «${e.name}»`, e.name, async (name) => {
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
    message.error(errText(err, 'Не удалось удалить'))
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
      message.error(`${f.name}: ${errText(e, 'не загружен')}`)
      break
    }
  }
  if (files.length) await Promise.all([loadDir(), loadSite()])
}

const columns: DataTableColumns<FileEntry> = [
  {
    title: 'Имя',
    key: 'name',
    render: (e) =>
      h('a', { href: '#', class: 'name', onClick: (ev: Event) => (ev.preventDefault(), void open(e)) }, [e.is_dir ? '📁 ' : '', e.name]),
  },
  { title: 'Размер', key: 'size', width: 100, render: (e) => (e.is_dir ? '' : formatBytes(e.size)) },
  { title: 'Изменён', key: 'mod_time', width: 170, render: (e) => fmtDate(e.mod_time) },
  {
    title: '',
    key: 'actions',
    width: 200,
    render: (e) =>
      h(NSpace, { size: 4, wrapItem: false }, () => [
        h(NButton, { size: 'tiny', quaternary: true, onClick: () => rename(e) }, () => 'Переименовать'),
        h(
          NPopconfirm,
          { onPositiveClick: () => remove(e) },
          {
            trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, () => 'Удалить'),
            default: () => `Удалить «${e.name}»${e.is_dir ? ' со всем содержимым' : ''}?`,
          },
        ),
      ]),
  },
]

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
    loadError.value = errText(e, 'Не удалось загрузить сайт')
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
    <n-space align="center" class="head">
      <n-button size="small" quaternary @click="router.push({ name: 'sites' })">← Сайты</n-button>
      <h2>{{ site?.host ?? 'Файлы' }}</h2>
    </n-space>

    <n-alert v-if="!loading && !site" type="error" :show-icon="false">Сайт не найден.</n-alert>
    <template v-else>
      <n-alert v-if="loadError" type="error" :show-icon="false" class="gap">{{ loadError }}</n-alert>

      <n-card v-if="openPath !== null" size="small" class="gap">
        <template #header>
          {{ openPath }}
          <n-tag v-if="dirty" size="small" type="warning" round>не сохранён</n-tag>
        </template>
        <template #header-extra>
          <n-space>
            <n-button size="small" type="primary" :loading="saving" :disabled="!dirty" @click="save">Сохранить (Ctrl+S)</n-button>
            <n-button size="small" @click="closeEditor">Закрыть</n-button>
          </n-space>
        </template>
        <code-editor v-model="content" :filename="openPath" @save="save" />
      </n-card>

      <n-card size="small">
        <template #header>
          <n-breadcrumb>
            <n-breadcrumb-item v-for="c in crumbs" :key="c.path" @click="go(c.path)">{{ c.name }}</n-breadcrumb-item>
          </n-breadcrumb>
        </template>
        <template #header-extra>
          <n-space>
            <n-button v-if="dir" size="small" quaternary @click="go(parentPath(dir))">Наверх</n-button>
            <n-button size="small" @click="newFile">Новый файл</n-button>
            <n-button size="small" @click="newFolder">Новая папка</n-button>
            <n-button size="small" type="primary" @click="fileInput?.click()">Загрузить файлы</n-button>
          </n-space>
        </template>
        <input ref="fileInput" type="file" multiple class="file" @change="onUpload">
        <n-data-table v-if="entries.length" :columns="columns" :data="entries" :bordered="false" size="small" :loading="loading" />
        <n-empty v-else-if="!loading" description="Папка пуста" />
      </n-card>
    </template>

    <n-modal :show="prompt !== null" preset="card" :title="prompt?.title" style="max-width: 400px" @update:show="prompt = null">
      <template v-if="prompt">
        <n-input v-model:value="prompt.value" autofocus :input-props="{ 'aria-label': prompt.title }" :status="prompt.error ? 'error' : undefined" @keydown.enter="submitPrompt" />
        <div v-if="prompt.error" class="err">{{ prompt.error }}</div>
        <n-space justify="end" class="modal-actions">
          <n-button @click="prompt = null">Отмена</n-button>
          <n-button type="primary" @click="submitPrompt">Готово</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.page { max-width: 960px; }
.head h2 { margin: 0; }
.gap { margin-bottom: 16px; }
.file { display: none; }
.err { color: #e88080; font-size: 13px; margin-top: 6px; }
.modal-actions { margin-top: 16px; }
:deep(.name) { color: inherit; text-decoration: none; }
:deep(.name:hover) { text-decoration: underline; }
</style>
