<script setup>
import { computed, nextTick, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../../api/index.js'
import FormField from '../../components/FormField.vue'
import { useDocumentMeta } from '../../composables/useDocumentMeta.js'
import { useForm } from '../../composables/useForm.js'
import { contentFromForm, formFromContent, problemsToFields } from '../../lib/teamdoc.js'
import ErrorView from '../ErrorView.vue'

const router = useRouter()
const { meta, error: metaError, loading: metaLoading, reload: reloadMeta } = useDocumentMeta()
const form = useForm()

const type = ref('')
const code = ref('')
const fields = ref(formFromContent({ level: 0 }, ''))
const otherProblems = ref([])

const typeInfo = computed(() => meta.value?.types.find((t) => t.id === type.value) ?? null)
const codeHint = computed(() => {
  if (!typeInfo.value) return 'Сначала выберите тип документа.'
  const example = `Например, ${typeInfo.value.code_example}.`
  return typeInfo.value.code_optional ? `${example} Можно не указывать: номер присвоится при публикации.` : example
})

async function focusFirstError() {
  await nextTick()
  document.querySelector('.new-doc [aria-invalid="true"]')?.focus()
}

async function onSubmit() {
  form.clear()
  otherProblems.value = []
  if (!type.value) form.errors.type = 'Выберите тип документа'
  const { content, errors } = contentFromForm({ ...fields.value, props: null, blocks: [] })
  Object.assign(form.errors, errors)
  if (Object.keys(form.errors).length > 0) return focusFirstError()

  let created = null
  const ok = await form.submit(async () => {
    const res = await api.post('/team/documents', { type: type.value, code: code.value.trim(), ...content })
    created = res.document
  })
  if (ok) {
    await router.push({ name: 'team-document', params: { id: created.id } })
    return undefined
  }
  const err = form.lastError.value
  if (err?.problems?.length) {
    const { byPath, other } = problemsToFields(err.problems)
    Object.assign(form.errors, byPath)
    otherProblems.value = other
    form.formError.value = 'Проверьте поля бланка.'
  }
  return focusFirstError()
}
</script>

<template>
  <section class="new-doc" aria-labelledby="new-title">
    <h2 id="new-title">Новый документ</h2>
    <p class="note">Документ появится черновиком: его видите только вы и Директорат, пока не отправите на проверку.</p>

    <ErrorView v-if="metaError" :request-id="metaError.requestId" :retrying="metaLoading" @retry="reloadMeta" />
    <p v-else-if="!meta" class="state" role="status">Загрузка…</p>

    <form v-else class="form" novalidate aria-label="Новый документ" @submit.prevent="onSubmit">
      <p v-if="form.formError.value" class="form-error" role="alert">{{ form.formError.value }}</p>
      <ul v-if="otherProblems.length" class="problems">
        <li v-for="p in otherProblems" :key="p.path"><code>{{ p.path }}</code>: {{ p.message }}</li>
      </ul>

      <div class="field" :class="{ 'field--invalid': form.errors.type }">
        <label for="nd-type">Тип документа</label>
        <select
          id="nd-type"
          v-model="type"
          :aria-invalid="form.errors.type ? 'true' : undefined"
          :aria-describedby="form.errors.type ? 'nd-type-error' : undefined"
        >
          <option value="" disabled>Выберите…</option>
          <option v-for="t in meta.types" :key="t.id" :value="t.id">{{ t.name }}</option>
        </select>
        <p v-if="form.errors.type" id="nd-type-error" class="error">{{ form.errors.type }}</p>
      </div>

      <FormField id="nd-code" v-model="code" label="Шифр" :hint="codeHint" :error="form.errors.code" :maxlength="40" />
      <FormField id="nd-title" v-model="fields.title" label="Название" :error="form.errors.title" :maxlength="300" />

      <fieldset class="date" :aria-describedby="form.errors.composed ? 'nd-composed-error' : undefined">
        <legend>Дата составления (внутри вселенной)</legend>
        <div class="date-row">
          <FormField id="nd-year" v-model="fields.year" label="Год" inputmode="numeric" :maxlength="4" :error="form.errors['composed.year']" />
          <FormField id="nd-month" v-model="fields.month" label="Месяц" inputmode="numeric" :maxlength="2" :error="form.errors['composed.month']" />
          <FormField id="nd-day" v-model="fields.day" label="День" inputmode="numeric" :maxlength="2" :error="form.errors['composed.day']" />
        </div>
        <p class="hint">Месяц и день можно не указывать.</p>
        <p v-if="form.errors.composed" id="nd-composed-error" class="error">{{ form.errors.composed }}</p>
      </fieldset>

      <div class="form-actions">
        <button type="submit" class="btn" :disabled="form.submitting.value">
          {{ form.submitting.value ? 'Заводим…' : 'Завести черновик' }}
        </button>
        <RouterLink class="form-link" :to="{ name: 'team-documents' }">Отмена</RouterLink>
      </div>
    </form>
  </section>
</template>

<style scoped>
.note, .state { color: var(--ink-soft); }
.field { margin-bottom: var(--space-3); }
label, legend {
  display: block;
  margin-bottom: var(--space-1);
  font-family: var(--font-head);
  font-size: 0.95rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
select {
  width: 100%;
  padding: 0.55rem 0.7rem;
  border: 2px solid var(--ink);
  border-radius: var(--radius);
  background: #f4eedc;
  color: var(--ink);
  font: inherit;
}
.field--invalid select { border-color: var(--stamp-red); }
.error { margin: var(--space-1) 0 0; color: var(--stamp-red); font-weight: 700; }
.hint { margin: 0; font-size: 0.85rem; color: var(--ink-soft); }
.date { margin: 0 0 var(--space-3); padding: var(--space-3); border: 1px solid var(--rule); }
.date-row { display: grid; grid-template-columns: 2fr 1fr 1fr; gap: var(--space-3); }
.date-row :deep(.field) { margin-bottom: var(--space-2); }
.problems { margin: 0 0 var(--space-3); padding-left: 1.2rem; color: var(--stamp-red); }
</style>
