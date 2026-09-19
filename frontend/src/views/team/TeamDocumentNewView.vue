<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useRouter } from 'vue-router'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiSelect from '@/ui/UiSelect.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import { isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { Problem } from '@/api/generated/documents'
import { useDocumentMeta } from '@/composables/useDocumentMeta'
import { useForm } from '@/composables/useForm'
import { contentFromForm, formFromContent, problemsToFields } from '@/lib/teamdoc'
import ErrorView from '../ErrorView.vue'

const router = useRouter()
const { meta, query: metaQuery } = useDocumentMeta()
const form = useForm()

const type = ref('')
const code = ref('')
const fields = ref(formFromContent({ level: 0 }, ''))
const otherProblems = ref<Problem[]>([])

const typeInfo = computed(() => meta.value?.types.find((t) => t.id === type.value) ?? null)
const typeOptions = computed(() => (meta.value?.types ?? []).map((t) => ({ value: t.id, label: t.name })))
const codeHint = computed(() => {
  if (!typeInfo.value) return 'Сначала выберите тип документа.'
  const example = `Например, ${typeInfo.value.code_example}.`
  return typeInfo.value.code_optional ? `${example} Можно не указывать: номер присвоится при публикации.` : example
})
const metaRequestId = computed(() => (isApiError(metaQuery.error.value) ? metaQuery.error.value.requestId : ''))

async function focusFirstError() {
  await nextTick()
  document.querySelector<HTMLElement>('.new-doc [aria-invalid="true"]')?.focus()
}

async function onSubmit() {
  form.clear()
  otherProblems.value = []
  if (!type.value) form.errors.type = 'Выберите тип документа'
  const { content, errors } = contentFromForm({ ...fields.value, props: null, blocks: [] })
  Object.assign(form.errors, errors)
  if (Object.keys(form.errors).length > 0) return focusFirstError()

  let createdId = 0
  const ok = await form.submit(async () => {
    const res = await teamApi.create({ type: type.value, code: code.value.trim(), ...content })
    createdId = res.document.id
  })
  if (ok) {
    await router.push({ name: 'team-document', params: { id: createdId } })
    return undefined
  }
  const err = form.lastError.value
  if (err?.problems.length) {
    const { byPath, other } = problemsToFields(err.problems)
    Object.assign(form.errors, byPath)
    otherProblems.value = other
    form.formError.value = 'Проверьте поля бланка.'
  }
  return focusFirstError()
}
</script>

<template>
  <UiSheet as="section" class="new-doc" aria-labelledby="new-title">
    <h2 id="new-title">Новый документ</h2>
    <p class="note">Документ появится черновиком: его видите только вы и Директорат, пока не отправите на проверку.</p>

    <ErrorView v-if="metaQuery.isError.value" :request-id="metaRequestId" :retrying="metaQuery.isFetching.value" @retry="metaQuery.refetch()" />
    <UiSkeleton v-else-if="!meta" :lines="4" />

    <form v-else novalidate aria-label="Новый документ" @submit.prevent="onSubmit">
      <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
      <ul v-if="otherProblems.length" class="problems">
        <li v-for="p in otherProblems" :key="p.path"><code>{{ p.path }}</code>: {{ p.message }}</li>
      </ul>

      <UiField id="nd-type" label="Тип документа" :error="form.errors.type">
        <UiSelect v-model="type" :options="typeOptions" placeholder="Выберите…" />
      </UiField>
      <UiField id="nd-code" label="Шифр" :hint="codeHint" :error="form.errors.code">
        <UiInput v-model="code" :maxlength="40" />
      </UiField>
      <UiField id="nd-title" label="Название" :error="form.errors.title">
        <UiInput v-model="fields.title" :maxlength="300" />
      </UiField>

      <fieldset class="date">
        <legend>Дата составления (внутри вселенной)</legend>
        <div class="date__row">
          <UiField id="nd-year" label="Год" :error="form.errors['composed.year']">
            <UiInput v-model="fields.year" inputmode="numeric" :maxlength="4" />
          </UiField>
          <UiField id="nd-month" label="Месяц" :error="form.errors['composed.month']">
            <UiInput v-model="fields.month" inputmode="numeric" :maxlength="2" />
          </UiField>
          <UiField id="nd-day" label="День" :error="form.errors['composed.day']">
            <UiInput v-model="fields.day" inputmode="numeric" :maxlength="2" />
          </UiField>
        </div>
        <p class="hint">Месяц и день можно не указывать.</p>
        <p v-if="form.errors.composed" class="error">{{ form.errors.composed }}</p>
      </fieldset>

      <div class="actions">
        <UiButton type="submit" variant="primary" :loading="form.submitting.value">{{ form.submitting.value ? 'Заводим…' : 'Завести черновик' }}</UiButton>
        <UiButton :to="{ name: 'team-documents' }" variant="link">Отмена</UiButton>
      </div>
    </form>
  </UiSheet>
</template>

<style scoped>
.new-doc {
  max-width: 44rem;
  margin-inline: 0;
}
.note {
  color: var(--text-muted);
}
.problems {
  margin: 0 0 var(--space-4);
  padding: var(--space-3) var(--space-4) var(--space-3) var(--space-6);
  border: 2px solid var(--red-700);
  border-radius: var(--radius-2);
  background: #f8e6e1;
  color: var(--red-800);
}
.date {
  margin: 0 0 var(--space-4);
  padding: var(--space-3) var(--space-4) var(--space-1);
  border: 2px dashed var(--border-strong);
  border-radius: var(--radius-2);
}
.date legend {
  padding: 0 var(--space-2);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.date__row {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0 var(--space-3);
}
.hint {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.error {
  color: var(--danger);
  font-size: var(--text-sm);
  font-weight: 700;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3) var(--space-5);
}
</style>
