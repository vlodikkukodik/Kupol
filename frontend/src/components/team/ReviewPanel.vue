<script setup lang="ts">
import { computed, ref } from 'vue'
import { ApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { CommentOut, InputBlock, TeamDocument } from '@/api/generated/documents'
import { describeApiError } from '@/composables/useForm'
import { useReview } from '@/composables/useReview'
import { formatDateTime } from '@/lib/format'
import { blockPreview } from '@/lib/teamdoc'
import { t } from '@/i18n'
import UiAlert from '@/ui/UiAlert.vue'
import UiBadge from '@/ui/UiBadge.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiSelect from '@/ui/UiSelect.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import UiTextarea from '@/ui/UiTextarea.vue'

// Вкладка «Рецензия»: проверка канона, комментарии к блокам и ход рецензии (кто и когда отправил, что решил и почему).
const props = defineProps<{
  doc: TeamDocument
  /** Блоки в редакторе сейчас: по ним комментарий находит свой блок и подписывается «Блок 3 — Абзац» */
  blocks: InputBlock[]
  dirty: boolean
  kindName: (type: string) => string
}>()
const emit = defineEmits<{ 'go-to-block': [id: string] }>()

const docRef = computed(() => props.doc)
const { review, lint } = useReview(docRef)

const info = computed(() => review.data.value)
const report = computed(() => lint.data.value)
const failure = ref('')
const notice = ref('')

// ———— блоки: подписи ————
const blockLabels = computed(() => {
  const map = new Map<string, { number: number; kind: string; preview: string }>()
  props.blocks.forEach((b, i) => map.set(b.id, { number: i + 1, kind: props.kindName(b.type), preview: blockPreview(b, 60) }))
  return map
})
const target = ref('')
const targetOptions = computed(() => [
  { value: '', label: t('review.wholeDoc') },
  ...props.blocks.map((b, i) => {
    const preview = blockPreview(b, 40)
    const kind = props.kindName(b.type)
    return { value: b.id, label: preview ? t('review.blockOptionPreview', { n: i + 1, kind, preview }) : t('review.blockOption', { n: i + 1, kind }) }
  }),
])

function placeOf(c: CommentOut): string {
  if (!c.block_id) return t('review.placeDoc')
  const l = blockLabels.value.get(c.block_id)
  return l ? t('review.placeBlock', { n: l.number, kind: l.kind }) : t('review.placeGone')
}

// ———— новый комментарий ————
const body = ref('')
const bodyError = ref('')
const adding = ref(false)

async function addComment() {
  bodyError.value = ''
  failure.value = ''
  const text = body.value.trim()
  if (!text) {
    bodyError.value = t('review.writeComment')
    return
  }
  adding.value = true
  try {
    await teamApi.addComment(props.doc.id, { body: text, ...(target.value ? { block_id: target.value } : {}) })
    body.value = ''
    notice.value = t('review.added')
    await review.refetch()
  } catch (err) {
    fail(err)
  } finally {
    adding.value = false
  }
}

function fail(err: unknown) {
  if (!(err instanceof ApiError)) throw err
  failure.value = describeApiError(err)
}

async function toggle(c: CommentOut) {
  failure.value = ''
  try {
    await teamApi.resolveComment(props.doc.id, c.id, !c.resolved)
    notice.value = c.resolved ? t('review.reopened') : t('review.marked')
    await review.refetch()
  } catch (err) {
    fail(err)
  }
}

async function remove(c: CommentOut) {
  failure.value = ''
  try {
    await teamApi.deleteComment(props.doc.id, c.id)
    notice.value = t('review.removed')
    await review.refetch()
  } catch (err) {
    fail(err)
  }
}

const canComment = computed(() => props.doc.workflow.comment)
const openFirst = computed(() => [...(info.value?.comments ?? [])].sort((a, b) => Number(a.resolved) - Number(b.resolved) || a.id - b.id))
const events = computed(() => info.value?.events ?? [])

const lintTone = computed(() => (report.value?.errors ? 'danger' : report.value?.warnings ? 'warning' : 'success'))
const lintSummary = computed(() => {
  const r = report.value
  if (!r) return ''
  if (r.errors) return r.warnings ? t('review.lintErrorsWarnings', { n: r.errors, w: r.warnings }) : t('review.lintErrors', { n: r.errors })
  if (r.warnings) return t('review.lintWarnings', { n: r.warnings })
  return t('review.lintOk')
})
const eventTone = (kind: string) => (kind === 'approve' ? 'published' : kind === 'reject' ? 'archived' : kind === 'return' ? 'review' : 'draft')
</script>

<template>
  <section class="review" aria-labelledby="review-title" data-testid="review-panel">
    <h3 id="review-title" class="visually-hidden">{{ $t('review.title') }}</h3>
    <p class="visually-hidden" role="status">{{ notice }}</p>
    <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>

    <!-- проверка канона -->
    <section class="block" aria-labelledby="lint-title" data-testid="lint">
      <div class="block__head">
        <h4 id="lint-title">{{ $t('review.lintTitle') }}</h4>
        <UiButton size="sm" icon="refresh" :loading="lint.isFetching.value" data-testid="lint-refresh" @click="lint.refetch()">{{ $t('review.lintAgain') }}</UiButton>
      </div>
      <p v-if="dirty" class="muted">{{ $t('review.lintDirty', { rev: doc.revision }) }}</p>
      <UiSkeleton v-if="!report && lint.isPending.value" :lines="2" :label="$t('review.lintChecking')" />
      <UiAlert v-else-if="lint.isError.value" tone="danger">{{ $t('review.lintFailed') }}</UiAlert>
      <template v-else-if="report">
        <UiAlert :tone="lintTone" :live="false" data-testid="lint-summary">{{ lintSummary }}</UiAlert>
        <ul v-if="report.issues.length" class="issues" data-testid="lint-issues">
          <li v-for="i in report.issues" :key="`${i.code}|${i.block_id ?? ''}|${i.message}`" :data-severity="i.severity">
            <UiBadge :tone="i.severity === 'error' ? 'danger' : 'review'">{{ i.severity === 'error' ? $t('review.error') : $t('review.warning') }}</UiBadge>
            <span class="issues__text">{{ i.message }}</span>
            <button v-if="i.block_id && blockLabels.has(i.block_id)" type="button" class="link" @click="emit('go-to-block', i.block_id)">
              {{ $t('review.toBlock', { n: blockLabels.get(i.block_id)?.number }) }}
            </button>
          </li>
        </ul>
      </template>
    </section>

    <!-- комментарии -->
    <section class="block" aria-labelledby="comments-title" data-testid="comments">
      <div class="block__head">
        <h4 id="comments-title">{{ $t('review.commentsTitle') }} <span v-if="info" class="count">{{ info.comments.length }}</span></h4>
        <p v-if="info?.open" class="muted" data-testid="comments-open">{{ $t('review.openCount', { n: info.open }) }}</p>
      </div>
      <UiSkeleton v-if="!info && review.isPending.value" :lines="3" :label="$t('review.loading')" />
      <UiAlert v-else-if="review.isError.value" tone="danger">{{ $t('review.loadFailed') }}</UiAlert>
      <template v-else>
        <p v-if="!openFirst.length" class="muted" data-testid="comments-empty">
          {{ $t('review.noComments') }}{{ canComment ? '' : $t('review.noCommentsWait') }}
        </p>
        <ul v-else class="comments">
          <li v-for="c in openFirst" :key="c.id" class="comment" :class="{ 'is-resolved': c.resolved }" :data-comment="c.id">
            <div class="comment__meta">
              <strong>{{ c.author ?? $t('review.deletedAccount') }}</strong>
              <span>{{ formatDateTime(c.created_at) }}</span>
              <span>{{ $t('review.forRevision', { rev: c.revision }) }}</span>
              <UiBadge v-if="c.resolved" tone="published">{{ $t('review.fixed') }}</UiBadge>
            </div>
            <p class="comment__place">
              <button v-if="c.block_id && blockLabels.has(c.block_id)" type="button" class="link" @click="emit('go-to-block', c.block_id)">{{ placeOf(c) }}</button>
              <template v-else>{{ placeOf(c) }}</template>
            </p>
            <p class="comment__body">{{ c.body }}</p>
            <div v-if="c.can_resolve || c.can_delete" class="comment__actions">
              <UiButton v-if="c.can_resolve" size="sm" :data-testid="c.resolved ? 'comment-reopen' : 'comment-resolve'" @click="toggle(c)">
                {{ c.resolved ? $t('review.reopen') : $t('review.resolve') }}
              </UiButton>
              <UiButton v-if="c.can_delete" size="sm" variant="ghost" icon="trash" data-testid="comment-delete" @click="remove(c)">{{ $t('review.remove') }}</UiButton>
            </div>
          </li>
        </ul>

        <form v-if="canComment" class="new-comment" novalidate :aria-label="$t('review.newComment')" @submit.prevent="addComment">
          <UiField :label="$t('review.target')">
            <UiSelect v-model="target" :options="targetOptions" name="block" />
          </UiField>
          <UiField :label="$t('review.comment')" :error="bodyError">
            <UiTextarea v-model="body" :rows="3" :maxlength="2000" name="body" />
          </UiField>
          <UiButton type="submit" variant="primary" icon="plus" :loading="adding" data-testid="comment-add">{{ $t('review.addComment') }}</UiButton>
        </form>
      </template>
    </section>

    <!-- ход рецензии -->
    <section class="block" aria-labelledby="events-title" data-testid="events">
      <h4 id="events-title">{{ $t('review.flowTitle') }}</h4>
      <p v-if="info && !events.length" class="muted">{{ $t('review.noFlow') }}</p>
      <ol v-else class="events">
        <li v-for="e in events" :key="e.id" :data-kind="e.kind">
          <div class="events__head">
            <UiBadge :tone="eventTone(e.kind)">{{ e.kind_name }}</UiBadge>
            <span>{{ e.actor ?? $t('review.deletedAccount') }}</span>
            <span class="muted">{{ $t('review.flowMeta', { when: formatDateTime(e.created_at), rev: e.revision }) }}</span>
          </div>
          <p v-if="e.comment" class="events__comment">{{ e.comment }}</p>
        </li>
      </ol>
    </section>
  </section>
</template>

<style scoped>
.block {
  margin-bottom: var(--space-6);
}
.block__head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2) var(--space-4);
  margin-bottom: var(--space-2);
}
.block h4 {
  margin: 0;
}
.count {
  color: var(--text-muted);
  font-weight: 400;
}
.muted {
  margin: 0;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.issues,
.comments,
.events {
  margin: var(--space-3) 0 0;
  padding: 0;
  list-style: none;
}
.issues li {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-1) var(--space-3);
  padding: var(--space-2) 0;
  border-bottom: 1px dashed var(--border-strong);
}
.issues__text {
  flex: 1 1 16rem;
  overflow-wrap: anywhere;
}
.link {
  padding: 0;
  border: 0;
  background: none;
  color: var(--link);
  font: inherit;
  font-weight: 700;
  text-decoration: underline;
  cursor: pointer;
}
.comment {
  margin-bottom: var(--space-3);
  padding: var(--space-3) var(--space-4);
  border: 2px solid var(--border-strong);
  border-left-width: 6px;
  border-radius: var(--radius-2);
  background: var(--surface-raised);
}
.comment.is-resolved {
  border-left-color: var(--success);
  background: var(--surface-sunken);
}
.comment__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-1) var(--space-3);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.comment__meta strong {
  color: var(--text);
}
.comment__place {
  margin: var(--space-1) 0;
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.comment__body {
  margin: 0 0 var(--space-2);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.comment__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.new-comment {
  margin-top: var(--space-4);
  padding-top: var(--space-4);
  border-top: 2px solid var(--ink-900);
}
.events li {
  padding: var(--space-2) 0 var(--space-2) var(--space-4);
  border-left: 3px solid var(--border-strong);
}
.events__head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-1) var(--space-3);
}
.events__comment {
  margin: var(--space-1) 0 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
