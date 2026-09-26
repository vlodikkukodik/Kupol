<script setup lang="ts">
// Файлы (этап 6.1): картинки и аудио. Загрузка — по одноразовому билету; файл отдаётся читателю только с допуском
// не ниже выбранного, а по ключу (а не по номеру) его нельзя перебрать. Ключ вставляется в блок документа.
import { computed, ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { ApiError, isApiError } from '@/api/client'
import { uploadsApi } from '@/api/endpoints'
import type { Upload } from '@/api/generated/uploads'
import { keys } from '@/api/query'
import { describeApiError } from '@/composables/useForm'
import { levelNames } from '@/lib/levels'
import { formatDateTime } from '@/lib/format'
import { t } from '@/i18n'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiEmpty from '@/ui/UiEmpty.vue'
import UiField from '@/ui/UiField.vue'
import UiSelect from '@/ui/UiSelect.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import ErrorView from '../ErrorView.vue'

const client = useQueryClient()
const list = useQuery({ queryKey: keys.teamUploads(), queryFn: ({ signal }) => uploadsApi.list({ signal }), staleTime: 0 })
const requestId = computed(() => (isApiError(list.error.value) ? list.error.value.requestId : ''))

const file = ref<File | null>(null)
const level = ref('0')
const fileError = ref('')
const failure = ref('')
const notice = ref('')
const busy = ref(false)
const input = ref<HTMLInputElement | null>(null)
const levels = computed(() => levelNames().map((label, i) => ({ value: String(i), label: `${i} · ${label}` })))

function onPick(e: Event) {
  file.value = (e.target as HTMLInputElement).files?.[0] ?? null
}

function fail(err: unknown) {
  if (!(err instanceof ApiError)) throw err
  failure.value = describeApiError(err)
}

async function upload() {
  failure.value = ''
  notice.value = ''
  fileError.value = ''
  if (!file.value) {
    fileError.value = t('teamUploads.pick')
    return
  }
  busy.value = true
  try {
    await uploadsApi.upload(file.value, Number(level.value))
    notice.value = t('teamUploads.uploaded')
    file.value = null
    if (input.value) input.value.value = ''
    await client.invalidateQueries({ queryKey: keys.teamUploads() })
  } catch (err) {
    fail(err)
  } finally {
    busy.value = false
  }
}

async function remove(item: Upload) {
  failure.value = ''
  notice.value = ''
  try {
    await uploadsApi.remove(item.id)
    notice.value = t('teamUploads.removed')
    await client.invalidateQueries({ queryKey: keys.teamUploads() })
  } catch (err) {
    fail(err)
  }
}

async function copy(item: Upload) {
  try {
    await navigator.clipboard.writeText(item.key)
    notice.value = t('teamUploads.copied')
  } catch {
    notice.value = item.key
  }
}
</script>

<template>
  <ErrorView v-if="list.isError.value" :request-id="requestId" :retrying="list.isFetching.value" @retry="list.refetch()" />
  <UiSheet v-else as="section" aria-labelledby="uploads-title" data-testid="team-uploads">
    <h2 id="uploads-title">{{ $t('teamUploads.title') }}</h2>
    <p class="lead">{{ $t('teamUploads.lead') }}</p>
    <p class="visually-hidden" role="status">{{ notice }}</p>
    <UiAlert v-if="notice" tone="success" :live="false">{{ notice }}</UiAlert>
    <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>

    <form novalidate class="upload" @submit.prevent="upload">
      <UiField :label="$t('teamUploads.file')" required :error="fileError">
        <input ref="input" type="file" accept="image/jpeg,image/png,image/webp,audio/mpeg,audio/ogg" data-testid="upload-file" @change="onPick">
      </UiField>
      <UiField :label="$t('teamUploads.level')"><UiSelect v-model="level" :options="levels" /></UiField>
      <UiButton type="submit" variant="primary" :loading="busy" data-testid="upload-submit">{{ $t('teamUploads.upload') }}</UiButton>
    </form>

    <UiSkeleton v-if="list.isPending.value" :lines="3" :label="$t('teamUploads.loading')" />
    <UiEmpty v-else-if="!list.data.value?.length" icon="stamp" :title="$t('teamUploads.empty')" />
    <ul v-else class="files" data-testid="uploads-list">
      <li v-for="u in list.data.value" :key="u.id">
        <img v-if="u.kind === 'image'" :src="`/api/uploads/${u.key}/thumb`" alt="" width="96" loading="lazy">
        <span v-else class="files__audio" aria-hidden="true">♪</span>
        <div class="files__info">
          <strong>{{ u.name }}</strong>
          <span>{{ u.mime }} · {{ $t('teamUploads.size', { kb: Math.ceil(u.size / 1024) }) }}<template v-if="u.width && u.height"> · {{ $t('teamUploads.dimensions', { w: u.width, h: u.height }) }}</template> · {{ levelNames()[u.level] }}</span>
          <span class="files__meta">{{ formatDateTime(u.created_at) }}<template v-if="u.owner"> · {{ $t('teamUploads.owner', { who: u.owner }) }}</template></span>
          <code>{{ u.key }}</code>
        </div>
        <div class="files__actions">
          <UiButton size="sm" variant="ghost" @click="copy(u)">{{ $t('teamUploads.copy') }}</UiButton>
          <UiButton size="sm" variant="ghost" @click="remove(u)">{{ $t('teamUploads.remove') }}</UiButton>
        </div>
      </li>
    </ul>
  </UiSheet>
</template>

<style scoped>
.lead {
  max-width: 44rem;
  color: var(--text-muted);
}
.upload {
  display: grid;
  gap: var(--space-2);
  max-width: 34rem;
  margin-bottom: var(--space-4);
}
.files {
  margin: 0;
  padding: 0;
  list-style: none;
}
.files li {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
  align-items: center;
  padding: var(--space-3) 0;
  border-top: 1px dashed var(--border-strong);
}
.files img {
  height: auto;
  border: 1px solid var(--border-strong);
}
.files__audio {
  display: grid;
  place-items: center;
  width: 96px;
  height: 64px;
  border: 1px solid var(--border-strong);
  font-size: 2rem;
}
.files__info {
  display: grid;
  flex: 1;
  min-width: 14rem;
  gap: 2px;
  overflow-wrap: anywhere;
}
.files__meta {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.files__actions {
  display: flex;
  gap: var(--space-1);
}
</style>
