<script setup lang="ts">
import { ref } from 'vue'
import { ApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import { describeApiError } from '@/composables/useForm'
import { t } from '@/i18n'
import UiButton from '@/ui/UiButton.vue'

// Скачивание документа файлом. JSON — тот же формат, что принимает «Загрузить из файла» (экспорт → правка → загрузка);
// Markdown — для чтения и правки текста вне архива, обратно не загружается. Выгружается СОХРАНЁННАЯ версия: несохранённые
// правки в редакторе в файл не попадают, о чём говорит подсказка.
const props = defineProps<{ docId: number; dirty: boolean }>()

const busy = ref<'json' | 'md' | ''>('')
const notice = ref('')
const failure = ref('')

async function save(format: 'json' | 'md') {
  busy.value = format
  notice.value = ''
  failure.value = ''
  try {
    const file = await teamApi.exportDocument(props.docId, format)
    const url = URL.createObjectURL(file.blob)
    const a = document.createElement('a')
    a.href = url
    a.download = file.filename
    document.body.append(a)
    a.click()
    a.remove()
    setTimeout(() => URL.revokeObjectURL(url), 10_000)
    notice.value = t('exportFiles.saved', { name: file.filename })
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    failure.value = describeApiError(err)
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <span class="export" data-testid="export">
    <UiButton variant="link" icon="download" :loading="busy === 'json'" :aria-describedby="dirty ? 'export-hint' : undefined" data-testid="export-json" @click="save('json')">
      {{ $t('exportFiles.json') }}
    </UiButton>
    <UiButton variant="link" icon="download" :loading="busy === 'md'" :aria-describedby="dirty ? 'export-hint' : undefined" data-testid="export-md" @click="save('md')">
      {{ $t('exportFiles.md') }}
    </UiButton>
    <span v-if="dirty" id="export-hint" class="hint">{{ $t('exportFiles.hint') }}</span>
    <span class="visually-hidden" role="status">{{ notice }}</span>
    <span v-if="failure" class="failure" role="alert" data-testid="export-error">{{ failure }}</span>
  </span>
</template>

<style scoped>
.export {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-1) var(--space-3);
}
.hint {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.failure {
  color: var(--red-800);
  font-size: var(--text-sm);
}
</style>
