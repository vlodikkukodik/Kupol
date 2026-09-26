<script setup lang="ts">
import { TrashOutline } from '@vicons/ionicons5'
import { NAlert, NButton, NFormItem, NIcon, NInput, NPopconfirm, NRadioButton, NRadioGroup, NSwitch, useMessage } from 'naive-ui'
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, ApiError } from '@/api/client'
import { siteSettingsResponseSchema, type Site, type SiteSettings } from '@/api/schemas'
import { resolveMessage, useI18n, type MessageKey } from '@/i18n'
import { useSitesStore } from '@/stores/sites'

const props = defineProps<{ site: Site }>()
const { t } = useI18n()
const store = useSitesStore()
const message = useMessage()
const router = useRouter()
const busy = ref(false)

const errText = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback)

// --- настройки, которые применяет веб-шлюз ---
const settingsUrl = computed(() => `/api/sites/${props.site.id}/settings`)
const loaded = ref(false)
const loadError = ref('')
const saving = ref(false)
const errors = ref<Record<string, string>>({})

const form = reactive({ rootDir: '', index: '', autoindex: false, www: '' as SiteSettings['www'], hsts: false })
const errorFiles = reactive<Record<string, string>>({})
const statuses = ref<number[]>([])
const defaultIndex = ref<string[]>([])
const maxIndex = ref(5)

const statusKey = {
  400: 'siteSettings.status.c400',
  401: 'siteSettings.status.c401',
  403: 'siteSettings.status.c403',
  404: 'siteSettings.status.c404',
  405: 'siteSettings.status.c405',
  410: 'siteSettings.status.c410',
  429: 'siteSettings.status.c429',
  500: 'siteSettings.status.c500',
  503: 'siteSettings.status.c503',
} as const satisfies Record<number, MessageKey>
const statusLabel = (code: number) => {
  const key = statusKey[code as keyof typeof statusKey]
  return key ? t(key) : ''
}

function apply(r: { settings: SiteSettings; default_index: string[]; error_statuses: number[]; max_index: number }) {
  form.rootDir = r.settings.root_dir
  form.index = r.settings.index.join(', ')
  form.autoindex = r.settings.autoindex
  form.www = r.settings.www
  form.hsts = r.settings.hsts
  statuses.value = r.error_statuses
  defaultIndex.value = r.default_index
  maxIndex.value = r.max_index
  for (const code of r.error_statuses) errorFiles[String(code)] = r.settings.error_pages[String(code)] ?? ''
}

async function loadSettings() {
  loadError.value = ''
  try {
    apply(await api(settingsUrl.value, { schema: siteSettingsResponseSchema }))
  } catch (e) {
    loadError.value = errText(e, t('siteSettings.loadFailed'))
  } finally {
    loaded.value = true
  }
}
onMounted(() => void loadSettings())

async function saveSettings() {
  saving.value = true
  errors.value = {}
  const errorPages: Record<string, string> = {}
  for (const [code, file] of Object.entries(errorFiles)) if (file.trim()) errorPages[code] = file.trim()
  try {
    const r = await api(settingsUrl.value, {
      method: 'PUT',
      body: {
        root_dir: form.rootDir.trim(),
        index: form.index.split(',').map((n) => n.trim()).filter(Boolean),
        autoindex: form.autoindex,
        error_pages: errorPages,
        www: form.www,
        hsts: form.hsts,
      },
      schema: siteSettingsResponseSchema,
    })
    apply(r)
    message.success(t('siteSettings.saved'))
  } catch (e) {
    if (e instanceof ApiError && e.field) errors.value = { [e.field]: e.message }
    else message.error(errText(e, t('siteSettings.saveFailed')))
  } finally {
    saving.value = false
  }
}

const err = (field: string) => (errors.value[field] ? resolveMessage(errors.value[field] ?? '') : undefined)

// --- удаление сайта ---
async function remove() {
  busy.value = true
  try {
    await api(`/api/sites/${props.site.id}`, { method: 'DELETE' })
    await router.push({ name: 'sites' })
    await store.load(t('sites.loadFailed'))
  } catch (e) {
    message.error(errText(e, t('sites.deleteFailed')))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="page">
    <header class="head rise">
      <h1>{{ t('siteArea.settings') }}</h1>
      <p>{{ t('siteSettings.hint') }}</p>
    </header>

    <n-alert v-if="loadError" type="error" :show-icon="false">
      {{ loadError }} <n-button size="tiny" @click="loadSettings">{{ t('common.retry') }}</n-button>
    </n-alert>
    <div v-else-if="!loaded" class="skeleton" style="height: 220px" />

    <form v-else class="form" @submit.prevent="saveSettings">
      <section class="glass card rise" style="--i: 1">
        <h3>{{ t('siteSettings.publish') }}</h3>
        <n-form-item :label="t('siteSettings.rootDir')" :validation-status="errors.root_dir ? 'error' : undefined" :feedback="err('root_dir') ?? t('siteSettings.rootDirHint')">
          <n-input v-model:value="form.rootDir" :placeholder="t('siteSettings.rootDirPlaceholder')" autocomplete="off" :input-props="{ 'aria-label': t('siteSettings.rootDir') }" @update:value="errors.root_dir = ''" />
        </n-form-item>
        <n-form-item
          :label="t('siteSettings.index')"
          :validation-status="errors.index ? 'error' : undefined"
          :feedback="err('index') ?? t('siteSettings.indexHint', { max: maxIndex, default: defaultIndex.join(', ') })"
        >
          <n-input v-model:value="form.index" :placeholder="t('siteSettings.indexPlaceholder')" autocomplete="off" :input-props="{ 'aria-label': t('siteSettings.index') }" @update:value="errors.index = ''" />
        </n-form-item>
      </section>

      <section class="glass card rise" style="--i: 2">
        <h3>{{ t('siteSettings.catalogs') }}</h3>
        <label class="switch">
          <n-switch v-model:value="form.autoindex" :aria-label="t('siteSettings.autoindex')" />
          <span><strong>{{ t('siteSettings.autoindex') }}</strong><small>{{ t('siteSettings.autoindexHint') }}</small></span>
        </label>
      </section>

      <section class="glass card rise" style="--i: 3">
        <h3>{{ t('siteSettings.errorPages') }}</h3>
        <p class="note">{{ t('siteSettings.errorPagesHint') }}</p>
        <p v-if="errors.error_pages" class="bad" role="alert">{{ err('error_pages') }}</p>
        <div class="grid">
          <n-form-item v-for="code in statuses" :key="code" :label="`${code} — ${statusLabel(code)}`">
            <n-input
              v-model:value="errorFiles[String(code)]"
              :placeholder="t('siteSettings.errorFilePlaceholder')"
              autocomplete="off"
              :input-props="{ 'aria-label': `${code} — ${statusLabel(code)}` }"
              @update:value="errors.error_pages = ''"
            />
          </n-form-item>
        </div>
      </section>

      <section class="glass card rise" style="--i: 4">
        <h3>{{ t('siteSettings.addresses') }}</h3>
        <n-alert type="info" :show-icon="false" class="gap">{{ t('siteSettings.httpsNote') }}</n-alert>
        <n-form-item :label="t('siteSettings.www')" :validation-status="errors.www ? 'error' : undefined" :feedback="err('www') ?? t('siteSettings.wwwHint')">
          <n-radio-group v-model:value="form.www" :aria-label="t('siteSettings.www')">
            <n-radio-button value="">{{ t('siteSettings.wwwNone') }}</n-radio-button>
            <n-radio-button value="add">{{ t('siteSettings.wwwAdd') }}</n-radio-button>
            <n-radio-button value="remove">{{ t('siteSettings.wwwRemove') }}</n-radio-button>
          </n-radio-group>
        </n-form-item>
        <label class="switch">
          <n-switch v-model:value="form.hsts" :aria-label="t('siteSettings.hsts')" />
          <span><strong>{{ t('siteSettings.hsts') }}</strong><small>{{ t('siteSettings.hstsHint') }}</small></span>
        </label>
      </section>

      <n-button type="primary" size="large" attr-type="submit" :loading="saving" class="save">{{ t('siteSettings.save') }}</n-button>
    </form>

    <section class="glass danger rise" style="--i: 5">
      <h3>{{ t('siteArea.dangerTitle') }}</h3>
      <p>{{ t('siteArea.dangerHint') }}</p>
      <n-popconfirm @positive-click="remove">
        <template #trigger>
          <n-button class="tint-rose" :disabled="busy">
            <template #icon><n-icon :component="TrashOutline" /></template>
            {{ t('sites.deleteSite') }}
          </n-button>
        </template>
        {{ t('sites.deleteConfirm', { host: site.host }) }}
      </n-popconfirm>
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
  margin: 4px 0 0;
  color: var(--text-dim);
  font-size: 13.5px;
}

.form {
  display: grid;
  gap: 18px;
}

.card {
  padding: 18px 22px 12px;
}

.card h3 {
  font-size: 16px;
  margin-bottom: 12px;
}

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 0 18px;
  margin-top: 12px;
}

.switch {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  margin: 4px 0 12px;
  cursor: pointer;
}

.switch span {
  display: grid;
}

.switch small {
  color: var(--text-dim);
  font-size: 12.5px;
}

.gap {
  margin-bottom: 14px;
}

.bad {
  margin: 8px 0 0;
  color: #fda4af;
  font-size: 13.5px;
}

.save {
  justify-self: start;
}

.danger {
  padding: 20px 24px;
  border-color: rgba(251, 113, 133, 0.35) !important;
}

.danger p {
  color: var(--text-dim);
  margin: 6px 0 14px;
}
</style>
