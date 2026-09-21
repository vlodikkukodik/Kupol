<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import BlockEditor from '@/components/editor/BlockEditor.vue'
import DocumentPreview from '@/components/team/DocumentPreview.vue'
import ExportButtons from '@/components/team/ExportButtons.vue'
import ReviewPanel from '@/components/team/ReviewPanel.vue'
import SaveTemplateDialog from '@/components/team/SaveTemplateDialog.vue'
import WorkflowBar from '@/components/team/WorkflowBar.vue'
import DocumentPropsForm from '@/components/team/DocumentPropsForm.vue'
import VersionHistory from '@/components/team/VersionHistory.vue'
import UiAlert from '@/ui/UiAlert.vue'
import UiBadge from '@/ui/UiBadge.vue'
import UiButton from '@/ui/UiButton.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import { ApiError, isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { Content, InputBlock, LintReport, Problem, SaveResult, TeamDocument, TemplateFull } from '@/api/generated/documents'
import { useAutosave } from '@/composables/useAutosave'
import { useDocumentMeta } from '@/composables/useDocumentMeta'
import { describeApiError } from '@/composables/useForm'
import { useReview } from '@/composables/useReview'
import { blockIndexes } from '@/editor/problems'
import { formatDateTime, formatTime } from '@/lib/format'
import { locale, t } from '@/i18n'
import { canonicalContent, contentFromForm, formFromContent, problemsToFields, sameContent, type DocForm } from '@/lib/teamdoc'
import { useAuthStore } from '@/stores/auth'
import ErrorView from '../ErrorView.vue'
import NotFoundView from '../NotFoundView.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { meta, query: metaQuery, statusName, blockKindName } = useDocumentMeta()

const id = computed(() => Number(route.params.id))
type Tab = 'document' | 'preview' | 'review' | 'history'
const TABS: Tab[] = ['preview', 'review', 'history']
const tab = computed<Tab>(() => TABS.find((x) => x === route.query.tab) ?? 'document')
// Уровень читателя в предпросмотре живёт в адресе: страницу можно переслать и обновить, не теряя выбор
const previewLevel = computed(() => {
  const n = Number(route.query.level)
  return Number.isInteger(n) && n >= 0 && n <= 7 ? n : 0
})

const doc = ref<TeamDocument | null>(null)
const loadError = ref<ApiError | null>(null)
const loading = ref(false)

const form = ref<DocForm | null>(null) // поля формы (строки)
const baseline = ref<Content | null>(null) // сохранённое содержимое: с ним сравнивается форма
const errors = reactive<Record<string, string>>({}) // путь замечания → текст
const otherProblems = ref<Problem[]>([]) // замечания без поля в форме (блоки и прочее)
const sentBlocks = ref<InputBlock[] | null>(null) // блоки последнего сохранения: по ним замечания «blocks[3]…» находят свои блоки
const editorKey = ref(0) // новое значение пересоздаёт редактор блоков (подмена содержимого целиком)
const blockEditor = ref<InstanceType<typeof BlockEditor> | null>(null)
const notice = ref('') // объявление об успехе (role=status)
const failure = ref('') // общая ошибка (role=alert)
const conflict = ref(0) // редакция, до которой документ успели изменить другие
const saving = ref(false)
const lockBusy = ref(false)
const draftDismissed = ref(false)

const { open: openComments } = useReview(doc, { withLint: false }) // счётчик на вкладке «Рецензия»

const lockedBy = computed(() => (doc.value?.lock && !doc.value.lock.mine ? doc.value.lock : null))
const mineLock = computed(() => Boolean(doc.value?.lock?.mine))
const editable = computed(() => Boolean(doc.value?.can_edit) && !lockedBy.value && !conflict.value)
const current = computed(() => (form.value ? contentFromForm(form.value, { strict: false }).content : null))
const dirty = computed(() => Boolean(form.value && baseline.value) && !sameContent(current.value, baseline.value))
const offeredDraft = computed(() => (doc.value?.draft && doc.value.can_edit && !draftDismissed.value && !dirty.value ? doc.value.draft : null))

function clearProblems() {
  for (const k of Object.keys(errors)) delete errors[k]
  otherProblems.value = []
  sentBlocks.value = null
  failure.value = ''
}

function showProblems(problems: Problem[], sent: InputBlock[] | null = null) {
  const { byPath, other } = problemsToFields(problems)
  Object.assign(errors, byPath)
  otherProblems.value = other
  sentBlocks.value = sent
  failure.value = t('tdoc.invalid')
}

/** Замечание сервера к блоку: какой это блок (номер и вид) и его идентификатор в редакторе. */
function blockOf(problem: Problem): { number: number; kind: string; id: string } | null {
  const index = blockIndexes([problem.path])[0]
  const block = index === undefined ? undefined : sentBlocks.value?.[index]
  return index === undefined || !block ? null : { number: index + 1, kind: blockKindName(block.type), id: block.id }
}
const problemRows = computed(() => otherProblems.value.map((p) => ({ ...p, block: blockOf(p) })))
const problemBlockIds = computed(() => problemRows.value.map((p) => p.block?.id).filter((id): id is string => Boolean(id)))
const fieldPath = (path: string) => path.replace(/^blocks\[\d+\]\.?/, '')

function goToBlock(id: string) {
  blockEditor.value?.focusBlock(id)
}

/** Показать документ с сервера: форма и «сохранённое» — из его содержимого. remount — пересоздать редактор блоков. */
function applyDoc(d: TeamDocument, { remount = true } = {}) {
  doc.value = d
  baseline.value = canonicalContent(d.content)
  form.value = formFromContent(d.content, d.type)
  if (remount) editorKey.value++
  syncTitle()
}

/** Заголовок вкладки; пересчитывается и при смене языка интерфейса (роутер этот маршрут не трогает) */
function syncTitle() {
  const d = doc.value
  if (d) document.title = t('tdoc.tabTitle', { code: d.code ?? t('tdoc.noCode'), title: d.content.title })
}
watch(locale, syncTitle)

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
  if (doc.value) doc.value.lock = { holder: err.lock?.holder ?? t('tdoc.takenBy'), mine: false, acquired_at: '', expires_at: err.lock?.expiresAt ?? '' }
  autosave.stop()
  failure.value = t('tdoc.lostLock', { message: describeApiError(err) })
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
    if (!editable.value || !dirty.value) return
    void ensureLock() // правка блоков (кнопка панели, набор текста) не всегда приходит событием ввода формы
    autosave.schedule()
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
  const field = document.querySelector<HTMLElement>('.editor input[aria-invalid="true"], .editor select[aria-invalid="true"], .editor textarea[aria-invalid="true"]')
  if (field) field.focus()
  else if (problemBlockIds.value[0]) goToBlock(problemBlockIds.value[0])
}

async function save() {
  if (!form.value || !doc.value) return
  clearProblems()
  notice.value = ''
  const { content, errors: local } = contentFromForm(form.value)
  if (Object.keys(local).length > 0) {
    Object.assign(errors, local)
    failure.value = t('tdoc.fixFields')
    return focusFirstInvalid()
  }
  saving.value = true
  try {
    const res = await teamApi.save(id.value, { base_revision: doc.value.revision, content })
    autosave.cancel()
    // Редактор пересоздаётся, только если сервер привёл содержимое к другому виду: иначе после каждого «Сохранить»
    // пропадали бы положение курсора и история отмены.
    applyDoc(res.document, { remount: !sameContent(canonicalContent(res.document.content), content) })
    notice.value = res.changed ? t('tdoc.saved', { rev: res.document.revision }) : t('tdoc.nothingToSave')
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    if (err.code === 'validation') {
      showProblems(err.problems, content.blocks)
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
  editorKey.value++
  clearProblems()
  notice.value = t('tdoc.reverted')
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
  editorKey.value++
  draftDismissed.value = true
  notice.value = t('tdoc.draftOpened')
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
    notice.value = lockedBy.value ? t('tdoc.lockReleasedBy', { holder: lockedBy.value.holder }) : t('tdoc.lockReleased')
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
  notice.value = res.changed ? t('tdoc.restored', { rev: res.document.revision }) : t('tdoc.alreadySame')
}

/** Ctrl+S / ⌘S в редакторе — «Сохранить», а не сохранение страницы браузером. */
function onEditorKeydown(event: KeyboardEvent) {
  if ((event.ctrlKey || event.metaKey) && !event.altKey && event.key.toLowerCase() === 's') {
    event.preventDefault()
    if (editable.value && !saving.value) void save()
  }
}

function setTab(next: Tab) {
  const query = { ...route.query }
  if (next === 'document') delete query.tab
  else query.tab = next
  if (next !== 'preview') delete query.level
  return router.replace({ query })
}

function setPreviewLevel(level: number) {
  void router.replace({ query: { ...route.query, tab: 'preview', level: String(level) } })
}

// ——— «Сохранить как шаблон» (право вести шаблоны) ———
const templateOpen = ref(false)
function onTemplateSaved(tpl: TemplateFull) {
  const what = tpl.kind === 'document' ? t('tdoc.templateWhatDocument') : t('tdoc.templateWhatSet', { n: tpl.blocks })
  notice.value = t('tdoc.templateSaved', { name: tpl.name, what })
}

/** Документ перевели (отправили, вернули, опубликовали…): показать его новым и объявить итог. */
function onFlowChanged(next: TeamDocument, message: string) {
  autosave.cancel()
  clearProblems()
  applyDoc(next)
  notice.value = message
}

/** Канон не пропустил: причины — на вкладке «Рецензия», в самом блоке проверки. */
function onLintFailed(report: LintReport | null) {
  notice.value = ''
  failure.value = report?.errors ? t('tdoc.lintFailedN', { n: report.errors }) : t('tdoc.lintFailed')
  void setTab('review')
}

function onFlowConflict(current: number) {
  conflict.value = current
  autosave.stop()
}

/** Из предпросмотра и рецензии — к блоку: вкладка «Документ» и фокус на блоке. */
async function goToBlockFromPreview(id: string) {
  await setTab('document') // вкладка показывается после смены адреса: до этого поля блока скрыты и фокус в них не встанет
  await nextTick()
  goToBlock(id)
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
      return t('tdoc.autosave.pending')
    case 'saving':
      return t('tdoc.autosave.saving')
    case 'saved':
      return t('tdoc.autosave.saved', { time: autosave.savedAt.value ? formatTime(autosave.savedAt.value) : '' })
    case 'error':
      return isApiError(autosave.error.value) && autosave.error.value.code === 'locked' ? t('tdoc.autosave.lockedError') : t('tdoc.autosave.error')
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
  <UiSheet v-else-if="!doc || !meta || !form" wide><UiSkeleton :lines="6" :label="$t('tdoc.loading')" /></UiSheet>

  <UiSheet v-else as="section" wide class="editor" aria-labelledby="doc-title" :data-doc-id="doc.id">
    <header class="doc-head">
      <p class="kicker">
        {{ doc.type_name }} ·
        <span data-testid="doc-code">{{ doc.code ?? $t('tdoc.noCodeYet') }}</span>
      </p>
      <h2 id="doc-title">{{ doc.content.title }}</h2>
      <dl class="facts">
        <div>
          <dt>{{ $t('tdoc.status') }}</dt>
          <dd data-testid="doc-status"><UiBadge :tone="statusTone(doc.status)">{{ statusName(doc.status) }}</UiBadge></dd>
        </div>
        <div><dt>{{ $t('tdoc.revision') }}</dt><dd data-testid="doc-revision">{{ doc.revision }}</dd></div>
        <div v-if="doc.author"><dt>{{ $t('tdoc.author') }}</dt><dd>{{ doc.author }}</dd></div>
        <div><dt>{{ $t('tdoc.changed') }}</dt><dd>{{ formatDateTime(doc.updated_at) }}</dd></div>
      </dl>
      <p v-if="doc.status === 'published' && doc.slug" class="public">
        <RouterLink :to="{ name: 'document', params: { ref: doc.slug } }">{{ $t('tdoc.openAsReader') }}</RouterLink>
      </p>
    </header>

    <WorkflowBar :doc="doc" :dirty="dirty" @changed="onFlowChanged" @lint-failed="onLintFailed" @conflict="onFlowConflict" @failure="failure = $event" />

    <div class="views" role="group" :aria-label="$t('tdoc.sectionsLabel')">
      <button type="button" class="view" :aria-pressed="tab === 'document' ? 'true' : 'false'" @click="void setTab('document')">{{ $t('tdoc.tabDocument') }}</button>
      <button type="button" class="view" :aria-pressed="tab === 'preview' ? 'true' : 'false'" data-testid="tab-preview" @click="void setTab('preview')">{{ $t('tdoc.tabPreview') }}</button>
      <button type="button" class="view" :aria-pressed="tab === 'review' ? 'true' : 'false'" data-testid="tab-review" @click="void setTab('review')">
        {{ $t('tdoc.tabReview') }}<span v-if="openComments" class="view__count" data-testid="tab-review-count">{{ openComments }}</span>
      </button>
      <button type="button" class="view" :aria-pressed="tab === 'history' ? 'true' : 'false'" @click="void setTab('history')">{{ $t('tdoc.tabHistory') }}</button>
    </div>

    <!-- Объявления: об успехе — вежливо, об ошибке — сразу -->
    <p class="visually-hidden" role="status">{{ notice }}</p>
    <UiAlert v-if="notice" tone="success" :live="false">{{ notice }}</UiAlert>
    <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>
    <ul v-if="problemRows.length" class="problems" data-testid="problems">
      <li v-for="p in problemRows" :key="`${p.path}|${p.message}`">
        <template v-if="p.block">
          <button type="button" class="problem-link" @click="goToBlock(p.block.id)">{{ $t('tdoc.problemBlock', { n: p.block.number, kind: p.block.kind }) }}</button>
          <template v-if="fieldPath(p.path)"> (<code>{{ fieldPath(p.path) }}</code>)</template>: {{ p.message }}
        </template>
        <template v-else><code>{{ p.path }}</code>: {{ p.message }}</template>
      </li>
    </ul>

    <UiAlert v-if="lockedBy" tone="warning" data-testid="lock-banner">
      <p>
        <strong>{{ $t('tdoc.editingBy', { holder: lockedBy.holder }) }}</strong>
        <template v-if="lockedBy.expires_at">{{ $t('tdoc.editingUntil', { time: formatTime(lockedBy.expires_at) }) }}</template>
        {{ $t('tdoc.readOnlyWhileLocked') }}
      </p>
      <UiButton v-if="doc.can_break_lock" :loading="lockBusy" @click="releaseLock">{{ $t('tdoc.breakLock') }}</UiButton>
    </UiAlert>
    <UiAlert v-else-if="!doc.can_edit" tone="info" data-testid="readonly-banner">
      <p v-if="doc.status === 'published' || doc.status === 'archived'">{{ $t('tdoc.readonlyPublished') }}</p>
      <p v-else>{{ $t('tdoc.readonlyNoRight') }}</p>
    </UiAlert>

    <UiAlert v-if="conflict" tone="warning" data-testid="conflict-banner">
      <p>{{ $t('tdoc.conflict', { rev: conflict }) }}</p>
      <UiButton @click="load">{{ $t('tdoc.openCurrent') }}</UiButton>
    </UiAlert>

    <UiAlert v-if="offeredDraft" tone="info" data-testid="draft-banner">
      <p>
        {{ $t('tdoc.draftFound') }}<template v-if="offeredDraft.author">{{ $t('tdoc.draftBy', { author: offeredDraft.author, when: formatDateTime(offeredDraft.saved_at) }) }}</template><template v-else>{{ $t('tdoc.draftAt', { when: formatDateTime(offeredDraft.saved_at) }) }}</template>.
      </p>
      <div class="banner-actions">
        <UiButton :disabled="!editable" @click="useDraft">{{ $t('tdoc.draftContinue') }}</UiButton>
        <UiButton variant="link" @click="dismissDraft">{{ $t('tdoc.draftDismiss') }}</UiButton>
      </div>
    </UiAlert>

    <!-- Вкладка «Документ» только скрывается: редактор остаётся в памяти, не теряются курсор и история отмены -->
    <div v-show="tab === 'document'">
      <form id="doc-form" novalidate :aria-label="$t('tdoc.propsForm')" @submit.prevent="save" @input.capture="ensureLock" @change.capture="ensureLock">
        <DocumentPropsForm v-model="form" :meta="meta" :errors="errors" :disabled="!editable" />
      </form>

      <!-- Редактор — вне формы: Enter в его полях не должен отправлять форму (сохранение — кнопкой или Ctrl+S) -->
      <section class="blocks" aria-labelledby="blocks-title" @keydown="onEditorKeydown">
        <h3 id="blocks-title">{{ $t('tdoc.content') }}</h3>
        <BlockEditor ref="blockEditor" :key="editorKey" v-model:blocks="form.blocks" :editable="editable" :problem-blocks="problemBlockIds" />
      </section>

      <div class="save-bar" data-testid="save-bar">
        <UiButton type="submit" form="doc-form" variant="primary" icon="check" :disabled="!editable" :loading="saving">{{ saving ? $t('tdoc.saving') : $t('tdoc.save') }}</UiButton>
        <UiButton v-if="dirty && editable" variant="link" @click="revert">{{ $t('tdoc.revert') }}</UiButton>
        <UiButton v-if="auth.can('manage_templates')" variant="link" icon="layers" data-testid="save-as-template" @click="templateOpen = true">{{ $t('tdoc.saveAsTemplate') }}</UiButton>
        <ExportButtons :doc-id="doc.id" :dirty="dirty" />
        <UiButton
          v-if="mineLock && !lockedBy"
          variant="link"
          :disabled="dirty || lockBusy"
          :aria-describedby="dirty ? 'finish-hint' : undefined"
          @click="releaseLock"
        >
          {{ $t('tdoc.finish') }}
        </UiButton>
        <span v-if="dirty && mineLock" id="finish-hint" class="hint">{{ $t('tdoc.finishHint') }}</span>
        <span class="autosave" data-testid="autosave-state" :data-state="autosave.state.value">{{ autosaveText }}</span>
      </div>
    </div>

    <SaveTemplateDialog
      v-if="current"
      v-model:open="templateOpen"
      :doc-type="doc.type"
      :doc-type-name="doc.type_name"
      :content="current"
      :blocks="form.blocks"
      :kind-name="blockKindName"
      @saved="onTemplateSaved"
    />

    <DocumentPreview
      v-if="tab === 'preview'"
      :doc-id="doc.id"
      :content="current"
      :dirty="dirty"
      :kind-name="blockKindName"
      :level="previewLevel"
      @update:level="setPreviewLevel"
      @go-to-block="goToBlockFromPreview"
    />

    <ReviewPanel
      v-else-if="tab === 'review'"
      :doc="doc"
      :blocks="form.blocks"
      :dirty="dirty"
      :kind-name="blockKindName"
      @go-to-block="goToBlockFromPreview"
    />

    <VersionHistory
      v-else-if="tab === 'history'"
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
  display: flex;
  max-width: 100%;
  width: fit-content;
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
@media (max-width: 40rem) {
  /* Три вкладки на телефоне: делят ширину, а не раздвигают страницу */
  .views {
    width: 100%;
  }
  .view {
    flex: 1 1 auto;
    min-width: 0;
    padding: 0 var(--space-2);
    font-size: var(--text-sm);
  }
}
.view__count {
  display: inline-block;
  min-width: 1.4em;
  margin-left: var(--space-2);
  padding: 0 0.35em;
  border-radius: 999px;
  background: var(--red-700);
  color: var(--paper-50);
  font-size: var(--text-xs);
  line-height: 1.5;
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
.problem-link {
  padding: 0;
  border: 0;
  background: none;
  color: inherit;
  font: inherit;
  font-weight: 700;
  text-decoration: underline;
  cursor: pointer;
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
