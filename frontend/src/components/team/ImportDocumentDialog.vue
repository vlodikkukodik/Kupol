<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { ImportResult, Problem } from '@/api/generated/documents'
import { describeApiError } from '@/composables/useForm'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiModal from '@/ui/UiModal.vue'

// «Загрузить из файла»: JSON того же вида, что отдаёт «Скачать JSON» (и что принимает команда `kupol doc import`). Сначала файл
// проверяется без сохранения — те же замечания с путями, что при настоящей загрузке, — и только потом создаётся черновик.
// Статус из файла не берётся: загруженное всегда черновик за тем, кто загрузил, и идёт обычным путём — правка и проверка.
const open = defineModel<boolean>('open', { required: true })
const router = useRouter()

/** Предел размера: тот же, что у запроса на сервере (документ передаётся как есть). */
const MAX_BYTES = 1000 * 1024

const input = ref<HTMLInputElement | null>(null)
const fileName = ref('')
const parsed = ref<unknown>(null)
const checked = ref<ImportResult | null>(null)
const problems = ref<Problem[]>([])
const failure = ref('')
const busy = ref(false)
const status = ref('')

watch(open, (isOpen) => {
  if (isOpen) reset()
})

function reset() {
  fileName.value = ''
  parsed.value = null
  checked.value = null
  problems.value = []
  failure.value = ''
  status.value = ''
  busy.value = false
  if (input.value) input.value.value = ''
}

function fail(err: unknown) {
  if (!(err instanceof ApiError)) throw err
  problems.value = err.problems
  failure.value = err.problems.length ? '' : (err.fields.code ?? describeApiError(err))
}

async function pick(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]
  checked.value = null
  problems.value = []
  failure.value = ''
  parsed.value = null
  fileName.value = file?.name ?? ''
  if (!file) return
  if (file.size > MAX_BYTES) {
    failure.value = `Файл слишком большой: ${Math.ceil(file.size / 1024)} КиБ, не больше 1000 КиБ.`
    return
  }
  busy.value = true
  try {
    try {
      parsed.value = JSON.parse(await file.text())
    } catch {
      failure.value = 'Файл не разобран как JSON. Загружается файл, скачанный кнопкой «Скачать JSON» (не Markdown).'
      return
    }
    checked.value = await teamApi.importDocument(parsed.value, true)
  } catch (err) {
    fail(err)
  } finally {
    busy.value = false
  }
}

async function create() {
  if (!checked.value || parsed.value === null) return
  failure.value = ''
  problems.value = []
  busy.value = true
  try {
    const res = await teamApi.importDocument(parsed.value, false)
    open.value = false
    if (res.document) await router.push({ name: 'team-document', params: { id: res.document.id } })
  } catch (err) {
    checked.value = null
    fail(err)
  } finally {
    busy.value = false
  }
}

const statusNote = computed(() => (checked.value?.status_ignored ? `В файле статус «${checked.value.status_ignored}» — он не берётся: документ будет создан черновиком.` : ''))
</script>

<template>
  <UiModal v-model:open="open" title="Загрузить документ из файла" size="lg" testid="import-dialog">
    <form novalidate @submit.prevent="create">
      <p class="lead">Файл JSON, скачанный кнопкой «Скачать JSON» у документа (или собранный по описанию формата). Документ появится черновиком: его можно править и отправить на проверку.</p>

      <div class="pick">
        <label for="import-file" class="pick__label">Файл</label>
        <input id="import-file" ref="input" type="file" accept=".json,application/json" data-testid="import-file" :disabled="busy" @change="pick">
      </div>

      <p class="visually-hidden" role="status">{{ busy ? 'Проверяем файл…' : checked ? 'Файл проверен: ошибок нет.' : '' }}</p>
      <UiAlert v-if="failure" tone="danger" data-testid="import-failure">{{ failure }}</UiAlert>
      <div v-if="problems.length" class="problems" data-testid="import-problems">
        <p class="problems__title">Файл не прошёл проверку ({{ problems.length }}). Исправьте и загрузите заново:</p>
        <ul>
          <li v-for="p in problems" :key="`${p.path}|${p.message}`"><code>{{ p.path }}</code>: {{ p.message }}</li>
        </ul>
      </div>

      <dl v-if="checked" class="summary" data-testid="import-summary">
        <div><dt>Название</dt><dd>{{ checked.title }}</dd></div>
        <div><dt>Тип</dt><dd>{{ checked.type_name }}</dd></div>
        <div><dt>Шифр</dt><dd>{{ checked.code || 'номер присвоится при публикации' }}</dd></div>
        <div><dt>Блоков</dt><dd>{{ checked.blocks }}</dd></div>
      </dl>
      <UiAlert v-if="statusNote" tone="info" data-testid="import-status-note">{{ statusNote }}</UiAlert>

      <div class="actions">
        <UiButton type="submit" variant="primary" icon="upload" :disabled="!checked" :loading="busy && !!checked" data-testid="import-create">Создать черновик</UiButton>
        <UiButton variant="link" @click="open = false">Отмена</UiButton>
      </div>
    </form>
  </UiModal>
</template>

<style scoped>
.lead {
  margin-top: 0;
  color: var(--text-muted);
}
.pick {
  display: grid;
  gap: var(--space-1);
  margin-bottom: var(--space-4);
}
.pick__label {
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.pick input {
  max-width: 100%;
  padding: var(--space-2);
  border: 2px solid var(--border-strong);
  border-radius: var(--radius-2);
  background: var(--surface-raised);
}
.problems {
  margin: 0 0 var(--space-4);
  padding: var(--space-3) var(--space-4);
  border: 2px solid var(--red-700);
  border-radius: var(--radius-2);
  background: #f8e6e1;
  color: var(--red-800);
}
.problems__title {
  margin: 0 0 var(--space-2);
  font-weight: 700;
}
.problems ul {
  margin: 0;
  padding-left: var(--space-5);
  max-height: 14rem;
  overflow: auto;
}
.problems code {
  overflow-wrap: anywhere;
}
.summary {
  display: grid;
  gap: var(--space-1);
  margin: 0 0 var(--space-3);
  padding: var(--space-3);
  border-left: 4px solid var(--success);
  background: var(--surface-sunken);
}
.summary div {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.summary dt {
  color: var(--text-muted);
}
.summary dt::after {
  content: ':';
}
.summary dd {
  margin: 0;
  font-weight: 700;
  overflow-wrap: anywhere;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3);
  margin-top: var(--space-3);
}
</style>
