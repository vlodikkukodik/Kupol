<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { api } from '../../api/index.js'
import { ApiError } from '../../api/client.js'
import DocumentPropsForm from '../../components/team/DocumentPropsForm.vue'
import VersionHistory from '../../components/team/VersionHistory.vue'
import { useAutosave } from '../../composables/useAutosave.js'
import { useDocumentMeta } from '../../composables/useDocumentMeta.js'
import { describeApiError } from '../../composables/useForm.js'
import { formatDateTime, formatTime } from '../../lib/format.js'
import { blockPreview, contentFromForm, formFromContent, problemsToFields, sameContent } from '../../lib/teamdoc.js'
import ErrorView from '../ErrorView.vue'
import NotFoundView from '../NotFoundView.vue'

const route = useRoute()
const router = useRouter()
const { meta, error: metaError, statusName, blockKindName } = useDocumentMeta()

const id = computed(() => Number(route.params.id))
const tab = computed(() => (route.query.tab === 'history' ? 'history' : 'document'))

const doc = ref(null)
const loadError = ref(null)
const loading = ref(false)

const form = ref(null) // поля формы (строки)
const baseline = ref(null) // сохранённое содержимое: с ним сравнивается форма
const errors = reactive({}) // путь замечания -> текст
const otherProblems = ref([]) // замечания без поля в форме (блоки и прочее)
const notice = ref('') // объявление об успехе (role=status)
const failure = ref('') // общая ошибка (role=alert)
const conflict = ref(0) // редакция, до которой документ успели изменить другие
const saving = ref(false)
const lockBusy = ref(false)
const draftDismissed = ref(false)

const lockedBy = computed(() => (doc.value?.lock && !doc.value.lock.mine ? doc.value.lock : null))
const mineLock = computed(() => Boolean(doc.value?.lock?.mine))
const editable = computed(() => Boolean(doc.value?.can_edit) && !lockedBy.value && !conflict.value)
const current = computed(() => (form.value ? contentFromForm(form.value, { strict: false }).content : null))
const dirty = computed(() => Boolean(form.value && baseline.value) && !sameContent(current.value, baseline.value))
const offeredDraft = computed(() => (doc.value?.draft && doc.value.can_edit && !draftDismissed.value && !dirty.value ? doc.value.draft : null))

function clearProblems() {
  for (const k of Object.keys(errors)) delete errors[k]
  otherProblems.value = []
  failure.value = ''
}

function showProblems(problems) {
  const { byPath, other } = problemsToFields(problems)
  Object.assign(errors, byPath)
  otherProblems.value = other
  failure.value = 'Документ не прошёл проверку: исправьте отмеченное.'
}

/** Показать документ с сервера: форма и «сохранённое» — из его содержимого. */
function applyDoc(d) {
  doc.value = d
  baseline.value = d.content
  form.value = formFromContent(d.content, d.type)
  document.title = `${d.code ?? 'Документ без шифра'} — ${d.content.title} — Панель команды — КУПОЛ`
}

async function load() {
  loading.value = true
  loadError.value = null
  try {
    const res = await api.get(`/team/documents/${id.value}`)
    conflict.value = 0
    draftDismissed.value = false
    clearProblems()
    applyDoc(res.document)
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    loadError.value = err
  } finally {
    loading.value = false
  }
}
watch(id, load, { immediate: true })

/** Кто-то другой взял документ: форма становится только для чтения. */
function lostLock(err) {
  doc.value.lock = { holder: err.lock?.holder ?? 'другой сотрудник', mine: false, expires_at: err.lock?.expiresAt ?? '' }
  autosave.stop()
  failure.value = `${describeApiError(err)} Ваши последние правки остались в истории версий как автосохранение.`
}

const autosave = useAutosave(async () => {
  try {
    const res = await api.put(`/team/documents/${id.value}/draft`, current.value)
    doc.value.lock = res.lock
  } catch (err) {
    if (err instanceof ApiError && err.code === 'locked') lostLock(err)
    throw err
  }
})

watch(
  form,
  () => {
    if (editable.value && dirty.value) autosave.schedule()
  },
  { deep: true },
)

/** Взять документ в работу при первой правке, а не при открытии: чтение никого не блокирует. */
async function ensureLock() {
  if (!editable.value || mineLock.value || lockBusy.value) return
  lockBusy.value = true
  try {
    doc.value.lock = (await api.post(`/team/documents/${id.value}/lock`)).lock
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    if (err.code === 'locked') lostLock(err)
    else failure.value = describeApiError(err)
  } finally {
    lockBusy.value = false
  }
}

async function save() {
  clearProblems()
  notice.value = ''
  const { content, errors: local } = contentFromForm(form.value)
  if (Object.keys(local).length > 0) {
    Object.assign(errors, local)
    failure.value = 'Исправьте отмеченные поля.'
    await nextTick()
    document.querySelector('.editor [aria-invalid="true"]')?.focus()
    return
  }
  saving.value = true
  try {
    const res = await api.put(`/team/documents/${id.value}`, { base_revision: doc.value.revision, content })
    autosave.cancel()
    applyDoc(res.document)
    notice.value = res.changed ? `Сохранено: редакция ${res.document.revision}.` : 'Изменений нет — сохранять нечего.'
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    if (err.code === 'validation') {
      showProblems(err.problems)
      await nextTick()
      document.querySelector('.editor [aria-invalid="true"]')?.focus()
    } else if (err.code === 'conflict') {
      conflict.value = err.currentRevision
      autosave.stop()
    } else if (err.code === 'locked') {
      lostLock(err)
    } else {
      failure.value = describeApiError(err)
    }
  } finally {
    saving.value = false
  }
}

/** Отменить правки: форма возвращается к сохранённому, а автосохранение фиксирует это, чтобы старые правки не предлагались снова. */
async function revert() {
  autosave.cancel()
  form.value = formFromContent(baseline.value, doc.value.type)
  clearProblems()
  notice.value = 'Правки отменены.'
  if (mineLock.value) {
    try {
      await api.put(`/team/documents/${id.value}/draft`, baseline.value)
    } catch (err) {
      if (!(err instanceof ApiError)) throw err
    }
  }
}

function useDraft() {
  form.value = formFromContent(doc.value.draft.content, doc.value.type)
  draftDismissed.value = true
  notice.value = 'Несохранённые правки открыты. Проверьте и сохраните.'
  ensureLock()
}

async function dismissDraft() {
  draftDismissed.value = true
  if (!editable.value) return
  await ensureLock()
  if (!mineLock.value) return
  try {
    await api.put(`/team/documents/${id.value}/draft`, baseline.value)
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
  }
}

async function releaseLock() {
  lockBusy.value = true
  failure.value = ''
  try {
    await api.delete(`/team/documents/${id.value}/lock`)
    notice.value = lockedBy.value ? `Замок снят: ${lockedBy.value.holder} больше не правит документ.` : 'Работа завершена: документ свободен.'
    await load()
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    failure.value = describeApiError(err)
  } finally {
    lockBusy.value = false
  }
}

function onRestored(res) {
  autosave.cancel()
  clearProblems()
  applyDoc(res.document)
  notice.value = res.changed ? `Версия возвращена: редакция ${res.document.revision}.` : 'Документ уже совпадает с этой версией.'
}

function setTab(next) {
  const query = { ...route.query }
  if (next === 'history') query.tab = 'history'
  else delete query.tab
  router.replace({ query })
}

// Уход со страницы: несохранённое дописать в автосохранение; если правок нет — отпустить документ.
onBeforeRouteLeave(async () => {
  await autosave.flush()
  if (doc.value && mineLock.value && !dirty.value) {
    try {
      await api.delete(`/team/documents/${id.value}/lock`)
    } catch (err) {
      if (!(err instanceof ApiError)) throw err
    }
  }
  return true
})

// Закрытие вкладки, пока правки ещё не дошли до автосохранения, — браузер переспросит.
function beforeUnload(event) {
  if (autosave.state.value === 'pending' || autosave.state.value === 'saving') event.preventDefault()
}
onMounted(() => window.addEventListener('beforeunload', beforeUnload))
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))

const autosaveText = computed(() => {
  switch (autosave.state.value) {
    case 'pending':
      return 'Есть несохранённые правки…'
    case 'saving':
      return 'Автосохранение…'
    case 'saved':
      return `Правки автосохранены в ${formatTime(autosave.savedAt.value)}`
    case 'error':
      return autosave.error.value instanceof ApiError && autosave.error.value.code === 'locked'
        ? 'Автосохранение остановлено: документ взял другой сотрудник.'
        : 'Автосохранение не удалось — повторим при следующей правке.'
    default:
      return ''
  }
})
</script>

<template>
  <NotFoundView v-if="loadError && loadError.status === 404" />
  <ErrorView v-else-if="loadError" :request-id="loadError.requestId" :retrying="loading" @retry="load" />
  <ErrorView v-else-if="metaError" :request-id="metaError.requestId" @retry="load" />
  <p v-else-if="!doc || !meta" class="state" role="status">Загрузка документа…</p>

  <section v-else class="editor" :aria-labelledby="'doc-title'" :data-doc-id="doc.id">
    <header class="doc-head">
      <p class="kicker">
        {{ doc.type_name }} ·
        <span data-testid="doc-code">{{ doc.code ?? 'без шифра (номер присвоится при публикации)' }}</span>
      </p>
      <h2 id="doc-title">{{ doc.content.title }}</h2>
      <dl class="facts">
        <div><dt>Статус</dt><dd data-testid="doc-status">{{ statusName(doc.status) }}</dd></div>
        <div><dt>Редакция</dt><dd data-testid="doc-revision">{{ doc.revision }}</dd></div>
        <div v-if="doc.author"><dt>Автор</dt><dd>{{ doc.author }}</dd></div>
        <div><dt>Изменён</dt><dd>{{ formatDateTime(doc.updated_at) }}</dd></div>
      </dl>
      <p v-if="doc.status === 'published' && doc.slug" class="public">
        <RouterLink :to="{ name: 'document', params: { ref: doc.slug } }">Открыть как читатель</RouterLink>
      </p>
    </header>

    <div class="views" role="group" aria-label="Раздел документа">
      <button type="button" class="view" :aria-pressed="tab === 'document' ? 'true' : 'false'" @click="setTab('document')">Документ</button>
      <button type="button" class="view" :aria-pressed="tab === 'history' ? 'true' : 'false'" @click="setTab('history')">История</button>
    </div>

    <!-- Объявления: об успехе — вежливо, об ошибке — сразу -->
    <p class="visually-hidden" role="status">{{ notice }}</p>
    <p v-if="notice" class="form-ok" aria-hidden="true">{{ notice }}</p>
    <p v-if="failure" class="form-error" role="alert">{{ failure }}</p>
    <ul v-if="otherProblems.length" class="problems" data-testid="problems">
      <li v-for="p in otherProblems" :key="p.path"><code>{{ p.path }}</code>: {{ p.message }}</li>
    </ul>

    <div v-if="lockedBy" class="banner banner--lock" data-testid="lock-banner">
      <p>
        <strong>Редактирует: {{ lockedBy.holder }}</strong>
        <template v-if="lockedBy.expires_at"> — до {{ formatTime(lockedBy.expires_at) }}, если не продлит.</template>
        Пока документ в работе, для вас он только для чтения.
      </p>
      <button v-if="doc.can_break_lock" type="button" class="btn" :disabled="lockBusy" @click="releaseLock">Снять замок</button>
    </div>
    <div v-else-if="!doc.can_edit" class="banner" data-testid="readonly-banner">
      <p v-if="doc.status === 'published' || doc.status === 'archived'">Опубликованный документ правят только Редактор и Директорат.</p>
      <p v-else>У вас нет права править этот документ.</p>
    </div>

    <div v-if="conflict" class="banner banner--lock" role="alert" data-testid="conflict-banner">
      <p>
        Документ изменён после того, как вы его открыли (сейчас редакция {{ conflict }}). Ваши правки не потеряны: они
        в истории версий как автосохранение — их можно сравнить с новой редакцией.
      </p>
      <button type="button" class="btn" @click="load">Открыть актуальную редакцию</button>
    </div>

    <div v-if="offeredDraft" class="banner" data-testid="draft-banner">
      <p>
        Есть несохранённые правки<template v-if="offeredDraft.author"> ({{ offeredDraft.author }}</template><template v-else> (</template>,
        {{ formatDateTime(offeredDraft.saved_at) }}).
      </p>
      <button type="button" class="btn" :disabled="!editable" @click="useDraft">Продолжить с ними</button>
      <button type="button" class="form-link" @click="dismissDraft">Отбросить</button>
    </div>

    <template v-if="tab === 'document'">
      <form class="doc-form" novalidate aria-label="Свойства документа" @submit.prevent="save" @input.capture="ensureLock" @change.capture="ensureLock">
        <DocumentPropsForm v-model="form" :meta="meta" :errors="errors" :disabled="!editable" />

        <section class="blocks" aria-labelledby="blocks-title">
          <h3 id="blocks-title">Блоки ({{ form.blocks.length }})</h3>
          <p v-if="!form.blocks.length" class="state">В документе пока нет блоков.</p>
          <ol v-else class="block-list" data-testid="block-list">
            <li v-for="b in form.blocks" :key="b.id" :data-block="b.id">
              <span class="block-kind">{{ blockKindName(b.type) }}</span>
              <code class="block-id">{{ b.id }}</code>
              <span v-if="b.level" class="block-level">допуск {{ b.level }}</span>
              <span class="block-text">{{ blockPreview(b) }}</span>
            </li>
          </ol>
        </section>

        <div class="form-actions sticky" data-testid="save-bar">
          <button type="submit" class="btn" :disabled="!editable || saving">{{ saving ? 'Сохраняем…' : 'Сохранить' }}</button>
          <button v-if="dirty && editable" type="button" class="form-link" @click="revert">Отменить правки</button>
          <button
            v-if="mineLock && !lockedBy"
            type="button"
            class="form-link"
            :disabled="dirty || lockBusy"
            :aria-describedby="dirty ? 'finish-hint' : undefined"
            @click="releaseLock"
          >
            Завершить работу
          </button>
          <span v-if="dirty && mineLock" id="finish-hint" class="hint">Сохраните или отмените правки, чтобы отпустить документ.</span>
          <span class="autosave" data-testid="autosave-state" :data-state="autosave.state.value">{{ autosaveText }}</span>
        </div>
      </form>
    </template>

    <VersionHistory
      v-else
      :doc-id="doc.id"
      :revision="doc.revision"
      :can-restore="editable"
      :status-name="statusName"
      :block-kind-name="blockKindName"
      @restored="onRestored"
      @problems="showProblems"
    />
  </section>
</template>

<style scoped>
.state, .hint, .autosave { color: var(--ink-soft); }
.kicker { margin: 0 0 var(--space-1); font-family: var(--font-head); letter-spacing: 0.12em; text-transform: uppercase; color: var(--ink-soft); }
.doc-head h2 { margin-bottom: var(--space-2); overflow-wrap: anywhere; }
.facts { display: flex; flex-wrap: wrap; gap: var(--space-2) var(--space-4); margin: 0 0 var(--space-3); }
.facts div { display: flex; gap: var(--space-1); }
.facts dt { font-family: var(--font-head); letter-spacing: 0.06em; text-transform: uppercase; }
.facts dt::after { content: ':'; }
.facts dd { margin: 0; }
.public { margin-top: 0; }
.views { display: inline-flex; margin-bottom: var(--space-3); border: 2px solid var(--ink); }
.view {
  padding: 0.4rem 1.1rem;
  border: 0;
  background: transparent;
  color: var(--ink);
  font-family: var(--font-head);
  letter-spacing: 0.1em;
  text-transform: uppercase;
  cursor: pointer;
}
.view + .view { border-left: 2px solid var(--ink); }
.view[aria-pressed='true'] { background: var(--ink); color: var(--paper); }
.banner {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2) var(--space-3);
  margin-bottom: var(--space-3);
  padding: var(--space-2) var(--space-3);
  border: 2px solid var(--ink);
  background: var(--paper-shade);
}
.banner p { flex: 1 1 18rem; margin: 0; }
.banner--lock { border-color: var(--stamp-red); }
.problems { margin: 0 0 var(--space-3); padding-left: 1.2rem; color: var(--stamp-red); }
.blocks { margin-top: var(--space-4); }
.block-list { margin: 0; padding-left: 1.6rem; }
.block-list li { padding: 0.3rem 0; border-bottom: 1px solid var(--rule); overflow-wrap: anywhere; }
.block-kind { font-family: var(--font-head); letter-spacing: 0.06em; text-transform: uppercase; margin-right: var(--space-2); }
.block-id { font-size: 0.8rem; color: var(--ink-soft); margin-right: var(--space-2); }
.block-level { font-size: 0.8rem; font-weight: 700; color: var(--stamp-red); margin-right: var(--space-2); }
.block-text { display: block; font-size: 0.9rem; color: var(--ink-soft); }
.sticky {
  position: sticky;
  bottom: 0;
  z-index: 1;
  margin-top: var(--space-4);
  padding: var(--space-3) 0;
  border-top: 2px solid var(--ink);
  background: var(--paper);
}
.autosave { margin-left: auto; font-size: 0.85rem; }
</style>
