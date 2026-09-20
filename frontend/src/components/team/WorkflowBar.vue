<script setup lang="ts">
import { computed, ref } from 'vue'
import { ApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { LintReport, TeamDocument } from '@/api/generated/documents'
import { describeApiError } from '@/composables/useForm'
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

const STATUS_HINT: Record<string, string> = {
  draft: 'Черновик: его видит и правит только автор. Когда текст готов — отправьте на проверку.',
  review: 'На проверке: Редактор читает документ и выносит вердикт. Пока вердикта нет, автор может забрать документ обратно.',
  published: 'Опубликован: читатели видят его по своему допуску. Править «на месте» вправе Редактор и Директорат.',
  archived: 'В архиве: читателям не виден. Вернуть в опубликованные вправе Редактор и Директорат.',
}
const hint = computed(() => STATUS_HINT[props.doc.status] ?? '')

const VERDICTS: { value: VerdictChoice; label: string; help: string }[] = [
  { value: 'approve', label: 'Принять и опубликовать', help: 'Документ станет виден читателям сразу; объекту без шифра присвоится номер О-№.' },
  { value: 'return', label: 'Вернуть на доработку', help: 'Документ снова станет черновиком автора; причина обязательна.' },
  { value: 'reject', label: 'Отклонить', help: 'Документ уйдёт в архив; причина обязательна.' },
]
const needsReason = computed(() => mode.value === 'verdict' && verdict.value !== 'approve')

const DIALOG_TITLES: Record<Mode, string> = {
  verdict: 'Вердикт по документу',
  withdraw: 'Забрать документ с проверки',
  archive: 'Убрать документ в архив',
  unarchive: 'Вернуть документ из архива',
}

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

const submit = () =>
  run(() => teamApi.submit(props.doc.id, props.doc.revision), () => 'Документ отправлен на проверку. Редактор увидит его в списке «На проверке».')

async function confirm() {
  commentError.value = ''
  const text = comment.value.trim()
  if (needsReason.value && text === '') {
    commentError.value = 'Укажите причину: автору нужно знать, что исправить.'
    return
  }
  const id = props.doc.id
  switch (mode.value) {
    case 'verdict': {
      const choice = verdict.value
      const rev = props.doc.revision
      return run(
        () => teamApi.verdict(id, { verdict: choice, comment: text, base_revision: rev }),
        (d) =>
          choice === 'approve'
            ? `Документ опубликован${d.code ? ` как ${d.code}` : ''}.`
            : choice === 'return'
              ? 'Документ возвращён автору на доработку.'
              : 'Документ отклонён и убран в архив.',
      )
    }
    case 'withdraw':
      return run(() => teamApi.withdraw(id, text), () => 'Документ снова черновик: его можно править и отправить заново.')
    case 'archive':
      return run(() => teamApi.archive(id, text), () => 'Документ убран в архив: читатели его больше не видят.')
    case 'unarchive':
      return run(() => teamApi.unarchive(id, text), () => 'Документ снова опубликован.')
    default:
      return undefined
  }
}

const anyAction = computed(() => wf.value.submit || wf.value.withdraw || wf.value.review || wf.value.archive || wf.value.unarchive)
</script>

<template>
  <section class="flow" aria-label="Ход документа" data-testid="workflow">
    <p class="flow__hint" data-testid="workflow-hint">{{ hint }}</p>

    <div v-if="anyAction" class="flow__actions">
      <UiButton v-if="wf.submit" variant="primary" icon="arrow-right" :loading="busy" :disabled="dirty" data-testid="wf-submit" @click="submit">
        Отправить на проверку
      </UiButton>
      <UiButton v-if="wf.review" variant="primary" icon="check" :disabled="busy" data-testid="wf-verdict" @click="openDialog('verdict')">Вынести вердикт</UiButton>
      <UiButton v-if="wf.withdraw" :disabled="busy" data-testid="wf-withdraw" @click="openDialog('withdraw')">Забрать на доработку</UiButton>
      <UiButton v-if="wf.archive" :disabled="busy" data-testid="wf-archive" @click="openDialog('archive')">В архив</UiButton>
      <UiButton v-if="wf.unarchive" :disabled="busy" data-testid="wf-unarchive" @click="openDialog('unarchive')">Вернуть из архива</UiButton>
      <span v-if="wf.submit && dirty" class="flow__note" data-testid="wf-dirty-note">Сначала сохраните правки: на проверку уходит сохранённая редакция.</span>
    </div>

    <UiModal v-model:open="dialogOpen" :title="mode ? DIALOG_TITLES[mode] : ''" testid="wf-dialog">
      <form v-if="mode" class="dialog" novalidate @submit.prevent="confirm">
        <fieldset v-if="mode === 'verdict'" class="verdicts">
          <legend class="verdicts__legend">Что решаете</legend>
          <label v-for="v in VERDICTS" :key="v.value" class="verdicts__item" :class="{ 'is-active': verdict === v.value }">
            <input v-model="verdict" type="radio" name="verdict" :value="v.value" :data-testid="`verdict-${v.value}`">
            <span class="verdicts__label">{{ v.label }}</span>
            <span class="verdicts__help">{{ v.help }}</span>
          </label>
        </fieldset>
        <p v-else-if="mode === 'withdraw'" class="dialog__lead">Документ вернётся в черновики: вы сможете его править и отправить заново. Рецензент увидит, что вы его забрали.</p>
        <p v-else-if="mode === 'archive'" class="dialog__lead">Опубликованный документ пропадёт из каталога для читателей. Его можно вернуть из архива.</p>
        <p v-else class="dialog__lead">Документ снова станет виден читателям по своему допуску.</p>

        <UiField :label="needsReason ? 'Причина' : 'Пояснение (необязательно)'" :required="needsReason" :error="commentError">
          <UiTextarea v-model="comment" :rows="4" :maxlength="2000" name="comment" />
        </UiField>

        <UiAlert v-if="mode === 'verdict' && verdict === 'approve' && !doc.code" tone="info">
          У объекта ещё нет шифра: номер О-№ присвоится при публикации — следующий по порядку.
        </UiAlert>

        <div class="dialog__actions">
          <UiButton type="submit" variant="primary" :loading="busy" data-testid="wf-confirm">Подтвердить</UiButton>
          <UiButton variant="link" @click="mode = null">Отмена</UiButton>
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
