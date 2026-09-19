<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import DocumentPropsForm from '@/components/team/DocumentPropsForm.vue'
import VersionHistory from '@/components/team/VersionHistory.vue'
import UiAlert from '@/ui/UiAlert.vue'
import UiBadge from '@/ui/UiBadge.vue'
import UiButton from '@/ui/UiButton.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import { ApiError, isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { Content, Problem, SaveResult, TeamDocument } from '@/api/generated/documents'
import { useAutosave } from '@/composables/useAutosave'
import { useDocumentMeta } from '@/composables/useDocumentMeta'
import { describeApiError } from '@/composables/useForm'
import { formatDateTime, formatTime } from '@/lib/format'
import { blockPreview, contentFromForm, formFromContent, problemsToFields, sameContent, type DocForm } from '@/lib/teamdoc'
import ErrorView from '../ErrorView.vue'
import NotFoundView from '../NotFoundView.vue'

const route = useRoute()
const router = useRouter()
const { meta, query: metaQuery, statusName, blockKindName } = useDocumentMeta()

const id = computed(() => Number(route.params.id))
const tab = computed(() => (route.query.tab === 'history' ? 'history' : 'document'))

const doc = ref<TeamDocument | null>(null)
const loadError = ref<ApiError | null>(null)
const loading = ref(false)

const form = ref<DocForm | null>(null) // поля формы (строки)
const baseline = ref<Content | null>(null) // сохранённое содержимое: с ним сравнивается форма
const errors = reactive<Record<string, string>>({}) // путь замечания → текст
const otherProblems = ref<Problem[]>([]) // замечания без поля в форме (блоки и прочее)
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

function showProblems(problems: Problem[]) {
  const { byPath, other } = problemsToFields(problems)
  Object.assign(errors, byPath)
  otherProblems.value = other
  failure.value = 'Документ не прошёл проверку: исправьте отмеченное.'
}

/** Показать документ с сервера: форма и «сохранённое» — из его содержимого. */
function applyDoc(d: TeamDocument) {
  doc.value = d
  baseline.value = d.content
  form.value = formFromContent(d.content, d.type)
  document.title = `${d.code ?? 'Документ без шифра'} — ${d.content.title} — Панель команды — КУПОЛ`
}

async function load() {
  loading.value = true
  loadError.value = null
  try {
    const res = await teamApi.document(id.value)
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
function lostLock(err: ApiError) {
  if (doc.value) doc.value.lock = { holder: err.lock?.holder ?? 'другой сотрудник', mine: false, acquired_at: '', expires_at: err.lock?.expiresAt ?? '' }
  autosave.stop()
  failure.value = `${describeApiError(err)} Ваши последние правки остались в истории версий как автосохранение.`
}

const autosave = useAutosave(async () => {
  if (!current.value) return
  try {
    const res = await teamApi.autosave(id.value, current.value)
    if (doc.value) doc.value.lock = res.lock
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
  if (!editable.value || mineLock.value || lockBusy.value || !doc.value) return
  lockBusy.value = true
  try {
    doc.value.lock = (await teamApi.takeLock(id.value)).lock
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    if (err.code === 'locked') lostLock(err)
    else failure.value = describeApiError(err)
  } finally {
    lockBusy.value = false
  }
}

const focusFirstInvalid = async () => {
  await nextTick()
  document.querySelector<HTMLElement>('.editor [aria-invalid="true"]')?.focus()
}

async function save() {
  if (!form.value || !doc.value) return
  clearProblems()
  notice.value = ''
  const { content, errors: local } = contentFromForm(form.value)
  if (Object.keys(local).length > 0) {
    Object.assign(errors, local)
    failure.value = 'Исправьте отмеченные поля.'
    return focusFirstInvalid()
  }
  saving.value = true
  try {
    const res = await teamApi.save(id.value, { base_revision: doc.value.revision, content })
    autosave.cancel()
    applyDoc(res.document)
    notice.value = res.changed ? `Сохранено: редакция ${res.document.revision}.` : 'Изменений нет — сохранять нечего.'
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    if (err.code === 'validation') {
      showProblems(err.problems)
      await focusFirstInvalid()
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
  return undefined
}

/** Отменить правки: форма возвращается к сохранённому, а автосохранение фиксирует это, чтобы старые правки не предлагались снова. */
async function revert() {
  if (!doc.value || !baseline.value) return
  autosave.cancel()
  form.value = formFromContent(baseline.value, doc.value.type)
  clearProblems()
  notice.value = 'Правки отменены.'
  if (mineLock.value) {
    try {
      await teamApi.autosave(id.value, baseline.value)
    } catch (err) {
      if (!(err instanceof ApiError)) throw err
    }
  }
}

function useDraft() {
  if (!doc.value?.draft) return
  form.value = formFromContent(doc.value.draft.content, doc.value.type)
  draftDismissed.value = true
  notice.value = 'Несохранённые правки открыты. Проверьте и сохраните.'
  void ensureLock()
}

async function dismissDraft() {
  draftDismissed.value = true
  if (!editable.value) return
  await ensureLock()
  if (!mineLock.value || !baseline.value) return
  try {
    await teamApi.autosave(id.value, baseline.value)
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
  }
}

async function releaseLock() {
  lockBusy.value = true
  failure.value = ''
  try {
    await teamApi.releaseLock(id.value)
    notice.value = lockedBy.value ? `Замок снят: ${lockedBy.value.holder} больше не правит документ.` : 'Работа завершена: документ свободен.'
    await load()
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    failure.value = describeApiError(err)
  } finally {
    lockBusy.value = false
  }
}

function onRestored(res: SaveResult) {
  autosave.cancel()
  clearProblems()
  applyDoc(res.document)
  notice.value = res.changed ? `Версия возвращена: редакция ${res.document.revision}.` : 'Документ уже совпадает с этой версией.'
}

function setTab(next: 'document' | 'history') {
  const query = { ...route.query }
  if (next === 'history') query.tab = 'history'
  else delete query.tab
  void router.replace({ query })
}

// Уход со страницы: несохранённое дописать в автосохранение; если правок нет — отпустить документ.
onBeforeRouteLeave(async () => {
  await autosave.flush()
  if (doc.value && mineLock.value && !dirty.value) {
    try {
      await teamApi.releaseLock(id.value)
    } catch (err) {
      if (!(err instanceof ApiError)) throw err
    }
  }
  return true
})

// Закрытие вкладки, пока правки ещё не дошли до автосохранения, — браузер переспросит.
function beforeUnload(event: BeforeUnloadEvent) {
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
      return `Правки автосохранены в ${autosave.savedAt.value ? formatTime(autosave.savedAt.value) : ''}`
    case 'error':
      return isApiError(autosave.error.value) && autosave.error.value.code === 'locked'
        ? 'Автосохранение остановлено: документ взял другой сотрудник.'
        : 'Автосохранение не удалось — повторим при следующей правке.'
    default:
      return ''
  }
})

const loadRequestId = computed(() => loadError.value?.requestId ?? '')
const metaRequestId = computed(() => (isApiError(metaQuery.error.value) ? metaQuery.error.value.requestId : ''))
const statusTone = (s: string) => (s === 'published' ? 'published' : s === 'review' ? 'review' : s === 'archived' ? 'archived' : 'draft')
</script>

<template>
  <NotFoundView v-if="loadError && loadError.status === 404" />
  <ErrorView v-else-if="loadError" :request-id="loadRequestId" :retrying="loading" @retry="load" />
  <ErrorView v-else-if="metaQuery.isError.value" :request-id="metaRequestId" @retry="metaQuery.refetch()" />
  <UiSheet v-else-if="!doc || !meta || !form" wide><UiSkeleton :lines="6" label="Загрузка документа…" /></UiSheet>

  <UiSheet v-else as="section" wide class="editor" aria-labelledby="doc-title" :data-doc-id="doc.id">
    <header class="doc-head">
      <p class="kicker">
        {{ doc.type_name }} ·
        <span data-testid="doc-code">{{ doc.code ?? 'без шифра (номер присвоится при публикации)' }}</span>
      </p>
      <h2 id="doc-title">{{ doc.content.title }}</h2>
      <dl class="facts">
        <div>
          <dt>Статус</dt>
          <dd data-testid="doc-status"><UiBadge :tone="statusTone(doc.status)">{{ statusName(doc.status) }}</UiBadge></dd>
        </div>
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
    <UiAlert v-if="notice" tone="success" :live="false">{{ notice }}</UiAlert>
    <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>
    <ul v-if="otherProblems.length" class="problems" data-testid="problems">
      <li v-for="p in otherProblems" :key="p.path"><code>{{ p.path }}</code>: {{ p.message }}</li>
    </ul>

    <UiAlert v-if="lockedBy" tone="warning" data-testid="lock-banner">
      <p>
        <strong>Редактирует: {{ lockedBy.holder }}</strong>
        <template v-if="lockedBy.expires_at"> — до {{ formatTime(lockedBy.expires_at) }}, если не продлит.</template>
        Пока документ в работе, для вас он только для чтения.
      </p>
      <UiButton v-if="doc.can_break_lock" :loading="lockBusy" @click="releaseLock">Снять замок</UiButton>
    </UiAlert>
    <UiAlert v-else-if="!doc.can_edit" tone="info" data-testid="readonly-banner">
      <p v-if="doc.status === 'published' || doc.status === 'archived'">Опубликованный документ правят только Редактор и Директорат.</p>
      <p v-else>У вас нет права править этот документ.</p>
    </UiAlert>

    <UiAlert v-if="conflict" tone="warning" data-testid="conflict-banner">
      <p>
        Документ изменён после того, как вы его открыли (сейчас редакция {{ conflict }}). Ваши правки не потеряны: они
        в истории версий как автосохранение — их можно сравнить с новой редакцией.
      </p>
      <UiButton @click="load">Открыть актуальную редакцию</UiButton>
    </UiAlert>

    <UiAlert v-if="offeredDraft" tone="info" data-testid="draft-banner">
      <p>
        Есть несохранённые правки<template v-if="offeredDraft.author"> ({{ offeredDraft.author }}, {{ formatDateTime(offeredDraft.saved_at) }})</template><template v-else> ({{ formatDateTime(offeredDraft.saved_at) }})</template>.
      </p>
      <div class="banner-actions">
        <UiButton :disabled="!editable" @click="useDraft">Продолжить с ними</UiButton>
        <UiButton variant="link" @click="dismissDraft">Отбросить</UiButton>
      </div>
    </UiAlert>

    <template v-if="tab === 'document'">
      <form novalidate aria-label="Свойства документа" @submit.prevent="save" @input.capture="ensureLock" @change.capture="ensureLock">
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

        <div class="save-bar" data-testid="save-bar">
          <UiButton type="submit" variant="primary" icon="check" :disabled="!editable" :loading="saving">{{ saving ? 'Сохраняем…' : 'Сохранить' }}</UiButton>
          <UiButton v-if="dirty && editable" variant="link" @click="revert">Отменить правки</UiButton>
          <UiButton
            v-if="mineLock && !lockedBy"
            variant="link"
            :disabled="dirty || lockBusy"
            :aria-describedby="dirty ? 'finish-hint' : undefined"
            @click="releaseLock"
          >
            Завершить работу
          </UiButton>
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
  </UiSheet>
</template>

<style scoped>
.editor {
  width: 100%;
}
.kicker {
  margin: 0 0 var(--space-1);
  color: var(--text-muted);
  font-family: var(--font-head);
  letter-spacing: 0.14em;
  text-transform: uppercase;
}
.doc-head h2 {
  overflow-wrap: anywhere;
}
.facts {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-6);
  margin: 0 0 var(--space-3);
}
.facts dt {
  color: var(--text-muted);
  font-family: var(--font-head);
  font-size: var(--text-xs);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.facts dd {
  margin: 0;
  font-weight: 700;
}
.public {
  margin: 0 0 var(--space-3);
}
.views {
  display: inline-flex;
  margin: var(--space-2) 0 var(--space-4);
  border: 2px solid var(--ink-900);
  border-radius: var(--radius-2);
  overflow: hidden;
}
.view {
  min-height: var(--control-h);
  padding: 0 var(--space-5);
  border: 0;
  background: transparent;
  font-family: var(--font-head);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
  cursor: pointer;
}
.view + .view {
  border-left: 2px solid var(--ink-900);
}
.view:hover {
  background: var(--surface-strong);
}
.view[aria-pressed='true'] {
  background: var(--ink-900);
  color: var(--paper-50);
  font-weight: 700;
}
.problems {
  margin: 0 0 var(--space-4);
  padding: var(--space-3) var(--space-4) var(--space-3) var(--space-6);
  border: 2px solid var(--red-700);
  border-radius: var(--radius-2);
  background: #f8e6e1;
  color: var(--red-800);
}
.banner-actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
}
.blocks {
  margin-top: var(--space-5);
}
.block-list {
  margin: 0;
  padding: 0;
  list-style: none;
}
.block-list li {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-1) var(--space-3);
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px dashed var(--border-strong);
}
.block-kind {
  font-family: var(--font-head);
  font-weight: 700;
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.block-id,
.block-level {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.block-text {
  flex-basis: 100%;
  overflow-wrap: anywhere;
}
.save-bar {
  position: sticky;
  bottom: 0;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3) var(--space-5);
  margin-top: var(--space-5);
  padding: var(--space-3) 0;
  border-top: 2px solid var(--ink-900);
  background: var(--surface);
}
.hint {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.autosave {
  margin-left: auto;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.autosave[data-state='error'] {
  color: var(--danger);
  font-weight: 700;
}
.state {
  color: var(--text-muted);
}
</style>
