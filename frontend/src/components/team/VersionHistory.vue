<script setup>
import { computed, ref, watch } from 'vue'
import { api } from '../../api/index.js'
import { ApiError } from '../../api/client.js'
import { describeApiError } from '../../composables/useForm.js'
import { useResource } from '../../composables/useResource.js'
import { formatDateTime } from '../../lib/format.js'
import ErrorView from '../../views/ErrorView.vue'
import VersionDiff from './VersionDiff.vue'

const props = defineProps({
  docId: { type: Number, required: true },
  revision: { type: Number, required: true }, // текущая редакция: при её смене история перечитывается
  canRestore: { type: Boolean, default: false },
  statusName: { type: Function, required: true },
  blockKindName: { type: Function, required: true },
})
const emit = defineEmits(['restored', 'problems'])

const PER_PAGE = 20
const page = ref(1)
const key = computed(() => `${props.docId}|${props.revision}|${page.value}`)
const list = useResource(
  (signal) => api.get(`/team/documents/${props.docId}/versions?page=${page.value}&per_page=${PER_PAGE}`, { signal }),
  () => key.value,
)

// Открытое сравнение: с текущим содержимым или с предыдущей по списку версией.
const open = ref(null) // { id, against: 'live' | 'prev' }
const diff = ref(null)
const diffError = ref('')
const loadingDiff = ref(false)
const confirming = ref(null) // id версии, откат к которой подтверждается
const restoring = ref(false)
const restoreError = ref('')
watch(key, () => {
  open.value = null
  diff.value = null
  confirming.value = null
})

function previousOf(id) {
  const items = list.data.value?.items ?? []
  const i = items.findIndex((v) => v.id === id)
  return i >= 0 && i + 1 < items.length ? items[i + 1] : null
}

async function showDiff(v, against) {
  if (open.value?.id === v.id && open.value.against === against) {
    open.value = null
    return
  }
  open.value = { id: v.id, against }
  diff.value = null
  diffError.value = ''
  loadingDiff.value = true
  try {
    // «что изменилось в этой версии» = разница от предыдущей к этой; «с текущим» = от этой к документу
    const url =
      against === 'live'
        ? `/team/documents/${props.docId}/versions/${v.id}/diff?against=live`
        : `/team/documents/${props.docId}/versions/${previousOf(v.id).id}/diff?against=${v.id}`
    diff.value = (await api.get(url)).diff
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    diffError.value = describeApiError(err)
  } finally {
    loadingDiff.value = false
  }
}

async function restore(v) {
  restoring.value = true
  restoreError.value = ''
  try {
    const res = await api.post(`/team/documents/${props.docId}/versions/${v.id}/restore`)
    confirming.value = null
    emit('restored', res)
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    if (err.problems.length) {
      restoreError.value = 'Эту версию нельзя вернуть как есть: она не проходит проверку.'
      emit('problems', err.problems)
    } else {
      restoreError.value = describeApiError(err)
    }
  } finally {
    restoring.value = false
  }
}

const pages = computed(() => list.data.value?.pages ?? 1)
</script>

<template>
  <section class="history" aria-labelledby="history-title">
    <h3 id="history-title">История версий</h3>
    <p class="note">
      Снимки при создании, смене статуса, правке опубликованного и откате хранятся всегда; обычные сохранения и
      автосохранения — последние 30.
    </p>
    <p v-if="restoreError" class="form-error" role="alert">{{ restoreError }}</p>

    <ErrorView v-if="list.error.value" :request-id="list.error.value.requestId" :retrying="list.loading.value" @retry="list.reload" />
    <p v-else-if="list.loading.value && !list.data.value" class="state" role="status">Загрузка истории…</p>

    <template v-else-if="list.data.value">
      <ol class="versions" data-testid="versions">
        <li v-for="v in list.data.value.items" :key="v.id" :data-version="v.id" :data-kind="v.kind">
          <div class="line">
            <span class="kind">{{ v.kind_name }}</span>
            <span class="meta">
              {{ formatDateTime(v.created_at) }} · редакция {{ v.revision }} · {{ statusName(v.status) }}<template v-if="v.author"> · {{ v.author }}</template>
            </span>
            <span v-if="v.note" class="note-line">{{ v.note }}</span>
          </div>
          <div class="actions">
            <button type="button" class="form-link" :aria-expanded="open?.id === v.id && open.against === 'live' ? 'true' : 'false'" @click="showDiff(v, 'live')">
              Сравнить с текущей
            </button>
            <button v-if="previousOf(v.id)" type="button" class="form-link" :aria-expanded="open?.id === v.id && open.against === 'prev' ? 'true' : 'false'" @click="showDiff(v, 'prev')">
              Что изменилось
            </button>
            <button v-if="canRestore && confirming !== v.id" type="button" class="form-link" @click="confirming = v.id">Вернуть эту версию</button>
          </div>
          <div v-if="confirming === v.id" class="confirm" role="group" :aria-label="`Подтверждение отката к версии ${v.id}`">
            <p>Документ станет таким, как в этой версии. Это будет новая редакция; нынешняя останется в истории.</p>
            <button type="button" class="btn" :disabled="restoring" @click="restore(v)">{{ restoring ? 'Возвращаем…' : 'Да, вернуть' }}</button>
            <button type="button" class="form-link" @click="confirming = null">Отмена</button>
          </div>
          <div v-if="open?.id === v.id" class="diff-box">
            <p v-if="loadingDiff" class="state" role="status">Сравниваем…</p>
            <p v-else-if="diffError" class="form-error" role="alert">{{ diffError }}</p>
            <VersionDiff v-else-if="diff" :diff="diff" :block-kind-name="blockKindName" />
          </div>
        </li>
      </ol>
      <nav v-if="pages > 1" class="pager" aria-label="Страницы истории">
        <button type="button" class="btn" :disabled="page <= 1" @click="page--">← Новее</button>
        <span>Страница {{ page }} из {{ pages }}</span>
        <button type="button" class="btn" :disabled="page >= pages" @click="page++">Старше →</button>
      </nav>
    </template>
  </section>
</template>

<style scoped>
.note, .state { color: var(--ink-soft); font-size: 0.9rem; }
.versions { margin: 0; padding: 0; list-style: none; border-top: 2px solid var(--ink); }
.versions > li { padding: var(--space-2) 0; border-bottom: 1px solid var(--rule); }
.line { display: grid; gap: 0.1rem; }
.kind { font-family: var(--font-head); letter-spacing: 0.08em; text-transform: uppercase; }
.meta, .note-line { font-size: 0.85rem; color: var(--ink-soft); overflow-wrap: anywhere; }
.actions { display: flex; flex-wrap: wrap; gap: var(--space-1) var(--space-3); margin-top: var(--space-1); }
.confirm { margin-top: var(--space-2); padding: var(--space-2) var(--space-3); border: 2px solid var(--ink); background: var(--paper-shade); }
.confirm p { margin-top: 0; }
.confirm .btn { margin-right: var(--space-3); }
.diff-box { margin-top: var(--space-2); }
.pager { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-3); margin-top: var(--space-3); }
</style>
