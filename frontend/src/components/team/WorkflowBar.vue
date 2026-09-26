<script setup lang="ts">
import { computed, ref } from 'vue'
import { ApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { LintReport, TeamDocument } from '@/api/generated/documents'
import { describeApiError } from '@/composables/useForm'
import { t } from '@/i18n'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiModal from '@/ui/UiModal.vue'
import UiTextarea from '@/ui/UiTextarea.vue'

// Ход документа: отправить на проверку, забрать, вынести вердикт, убрать в архив и вернуть. Что доступно сейчас —
// решает сервер (doc.workflow), здесь показывается только это. Каждое действие — один запрос; ответ — обновлённый документ.
const props = defineProps<{ doc: TeamDocument; dirty: boolean }>()
const emit = defineEmits<{
  changed: [doc: TeamDocument, message: string]
  'lint-failed': [report: LintReport | null]
  conflict: [currentRevision: number]
  failure: [message: string]
}>()

type Mode = 'verdict' | 'withdraw' | 'archive' | 'unarchive'
type VerdictChoice = 'approve' | 'return' | 'reject'

const wf = computed(() => props.doc.workflow)
const busy = ref(false)
const mode = ref<Mode | null>(null)
const dialogOpen = computed({ get: () => mode.value !== null, set: (v) => { if (!v) mode.value = null } })
const verdict = ref<VerdictChoice>('approve')
const comment = ref('')
const commentError = ref('')

const STATUSES_WITH_HINT = ['draft', 'review', 'published', 'archived']
const hint = computed(() => (STATUSES_WITH_HINT.includes(props.doc.status) ? t(`flow.hint.${props.doc.status}`) : ''))

const VERDICTS: VerdictChoice[] = ['approve', 'return', 'reject']
const needsReason = computed(() => mode.value === 'verdict' && verdict.value !== 'approve')

function openDialog(next: Mode) {
  mode.value = next
  verdict.value = 'approve'
  comment.value = ''
  commentError.value = ''
}

/** Разбор ошибки действия: канон, конфликт редакций и прочее — каждому свой путь. */
function onError(err: unknown) {
  if (!(err instanceof ApiError)) throw err
  mode.value = null
  if (err.code === 'lint_failed') emit('lint-failed', err.lint)
  else if (err.code === 'conflict') emit('conflict', err.currentRevision)
  else emit('failure', describeApiError(err))
}

async function run(action: () => Promise<{ document: TeamDocument }>, message: (d: TeamDocument) => string) {
  busy.value = true
  try {
    const res = await action()
    mode.value = null
    emit('changed', res.document, message(res.document))
  } catch (err) {
    onError(err)
  } finally {
    busy.value = false
  }
}

const submit = () => run(() => teamApi.submit(props.doc.id, props.doc.revision), () => t('flow.done.submit'))

async function confirm() {
  commentError.value = ''
  const text = comment.value.trim()
  if (needsReason.value && text === '') {
    commentError.value = t('flow.reasonNeeded')
    return
  }
  const id = props.doc.id
  switch (mode.value) {
    case 'verdict': {
      const choice = verdict.value
      const rev = props.doc.revision
      return run(
        () => teamApi.verdict(id, { verdict: choice, comment: text, base_revision: rev }),
        (d) => (choice === 'approve' ? (d.code ? t('flow.done.approveCode', { code: d.code }) : t('flow.done.approve')) : t(`flow.done.${choice}`)),
      )
    }
    case 'withdraw':
      return run(() => teamApi.withdraw(id, text), () => t('flow.done.withdraw'))
    case 'archive':
      return run(() => teamApi.archive(id, text), () => t('flow.done.archive'))
    case 'unarchive':
      return run(() => teamApi.unarchive(id, text), () => t('flow.done.unarchive'))
    default:
      return undefined
  }
}

const anyAction = computed(() => wf.value.submit || wf.value.withdraw || wf.value.review || wf.value.archive || wf.value.unarchive)
</script>

<template>
  <section class="flow" :aria-label="$t('flow.label')" data-testid="workflow">
    <p class="flow__hint" data-testid="workflow-hint">{{ hint }}</p>

    <div v-if="anyAction" class="flow__actions">
      <UiButton v-if="wf.submit" variant="primary" icon="arrow-right" :loading="busy" :disabled="dirty" data-testid="wf-submit" @click="submit">
        {{ $t('flow.submit') }}
      </UiButton>
      <UiButton v-if="wf.review" variant="primary" icon="check" :disabled="busy" data-testid="wf-verdict" @click="openDialog('verdict')">{{ $t('flow.verdict') }}</UiButton>
      <UiButton v-if="wf.withdraw" :disabled="busy" data-testid="wf-withdraw" @click="openDialog('withdraw')">{{ $t('flow.withdraw') }}</UiButton>
      <UiButton v-if="wf.archive" :disabled="busy" data-testid="wf-archive" @click="openDialog('archive')">{{ $t('flow.archive') }}</UiButton>
      <UiButton v-if="wf.unarchive" :disabled="busy" data-testid="wf-unarchive" @click="openDialog('unarchive')">{{ $t('flow.unarchive') }}</UiButton>
      <span v-if="wf.submit && dirty" class="flow__note" data-testid="wf-dirty-note">{{ $t('flow.saveFirst') }}</span>
    </div>

    <UiModal v-model:open="dialogOpen" :title="mode ? $t(`flow.dialog.${mode}`) : ''" testid="wf-dialog">
      <form v-if="mode" class="dialog" novalidate @submit.prevent="confirm">
        <fieldset v-if="mode === 'verdict'" class="verdicts">
          <legend class="verdicts__legend">{{ $t('flow.decide') }}</legend>
          <label v-for="v in VERDICTS" :key="v" class="verdicts__item" :class="{ 'is-active': verdict === v }">
            <input v-model="verdict" type="radio" name="verdict" :value="v" :data-testid="`verdict-${v}`">
            <span class="verdicts__label">{{ $t(`flow.verdicts.${v}.label`) }}</span>
            <span class="verdicts__help">{{ $t(`flow.verdicts.${v}.help`) }}</span>
          </label>
        </fieldset>
        <p v-else-if="mode === 'withdraw'" class="dialog__lead">{{ $t('flow.withdrawLead') }}</p>
        <p v-else-if="mode === 'archive'" class="dialog__lead">{{ $t('flow.archiveLead') }}</p>
        <p v-else class="dialog__lead">{{ $t('flow.unarchiveLead') }}</p>

        <UiField :label="needsReason ? $t('flow.reason') : $t('flow.note')" :required="needsReason" :error="commentError">
          <UiTextarea v-model="comment" :rows="4" :maxlength="2000" name="comment" />
        </UiField>

        <UiAlert v-if="mode === 'verdict' && verdict === 'approve' && !doc.code" tone="info">{{ $t('flow.autoCode') }}</UiAlert>

        <div class="dialog__actions">
          <UiButton type="submit" variant="primary" :loading="busy" data-testid="wf-confirm">{{ $t('flow.confirm') }}</UiButton>
          <UiButton variant="link" @click="mode = null">{{ $t('flow.cancel') }}</UiButton>
        </div>
      </form>
    </UiModal>
  </section>
</template>

<style scoped>
.flow {
  margin: 0 0 var(--space-4);
  padding: var(--space-3) var(--space-4);
  border: 2px solid var(--ink-900);
  border-radius: var(--radius-2);
  background: var(--surface-sunken);
}
.flow__hint {
  margin: 0;
  max-width: 46rem;
}
.flow__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3);
  margin-top: var(--space-3);
}
.flow__note {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.dialog__lead {
  margin: 0 0 var(--space-4);
}
.dialog__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3);
  margin-top: var(--space-3);
}
.verdicts {
  margin: 0 0 var(--space-4);
  padding: 0;
  border: 0;
}
.verdicts__legend {
  padding: 0;
  margin-bottom: var(--space-2);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.verdicts__item {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 0 var(--space-3);
  align-items: baseline;
  margin-bottom: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border: 2px solid var(--border-strong);
  border-radius: var(--radius-2);
  background: var(--surface-raised);
  cursor: pointer;
}
.verdicts__item.is-active {
  border-color: var(--ink-950);
  background: var(--paper-100);
}
.verdicts__item:has(input:focus-visible) {
  box-shadow:
    0 0 0 2px var(--focus-inner),
    0 0 0 5px var(--focus);
}
.verdicts__item input {
  grid-row: 1 / span 2;
  width: 1.25rem;
  height: 1.25rem;
  accent-color: var(--ink-900);
}
.verdicts__label {
  font-weight: 700;
}
.verdicts__help {
  grid-column: 2;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
</style>
