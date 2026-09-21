<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { Content, InputBlock, Problem, TemplateFull } from '@/api/generated/documents'
import { describeApiError } from '@/composables/useForm'
import { blockPreview } from '@/lib/teamdoc'
import { t } from '@/i18n'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiModal from '@/ui/UiModal.vue'
import UiSelect from '@/ui/UiSelect.vue'
import UiTextarea from '@/ui/UiTextarea.vue'

// «Сохранить как шаблон»: из открытого документа — шаблон документа (тип, название, допуск, гриф, блоки) или набор блоков
// (выбранный диапазон блоков). Берётся то, что сейчас в редакторе, — с несохранёнными правками. Блоки проверяет сервер так же
// строго, как в документе, поэтому шаблон, который нельзя вставить, не сохранится: причины показываются списком.
const open = defineModel<boolean>('open', { required: true })
const props = defineProps<{ docType: string; docTypeName: string; content: Content; blocks: InputBlock[]; kindName: (type: string) => string }>()
const emit = defineEmits<{ saved: [template: TemplateFull] }>()

type Kind = 'document' | 'blockset'
const kind = ref<Kind>('document')
const name = ref('')
const description = ref('')
const from = ref('1')
const to = ref('1')
const errors = ref<Record<string, string>>({})
const problems = ref<Problem[]>([])
const failure = ref('')
const busy = ref(false)

watch(open, (isOpen) => {
  if (!isOpen) return
  kind.value = 'document'
  name.value = props.content.title
  description.value = ''
  from.value = '1'
  to.value = String(Math.max(1, props.blocks.length))
  errors.value = {}
  problems.value = []
  failure.value = ''
})

const blockOptions = computed(() =>
  props.blocks.map((b, i) => {
    const preview = blockPreview(b, 40)
    return { value: String(i + 1), label: preview ? t('saveTpl.blockOptionPreview', { n: i + 1, kind: props.kindName(b.type), preview }) : t('saveTpl.blockOption', { n: i + 1, kind: props.kindName(b.type) }) }
  }),
)
const range = computed(() => {
  const a = Number(from.value)
  const b = Number(to.value)
  return { a: Math.min(a, b), b: Math.max(a, b) }
})
const chosen = computed(() => (kind.value === 'document' ? props.blocks : props.blocks.slice(range.value.a - 1, range.value.b)))

async function save() {
  errors.value = {}
  problems.value = []
  failure.value = ''
  if (!name.value.trim()) {
    errors.value = { name: t('saveTpl.enterName') }
    return
  }
  if (!chosen.value.length) {
    failure.value = t('saveTpl.empty')
    return
  }
  const c = props.content
  busy.value = true
  try {
    const created = await teamApi.createTemplate(
      kind.value === 'document'
        ? {
            kind: 'document',
            name: name.value,
            description: description.value,
            doc_type: props.docType,
            content: {
              blocks: chosen.value,
              ...(c.title ? { title: c.title } : {}),
              ...(typeof c.level === 'number' ? { level: c.level } : {}),
              ...(c.direct_link ? { direct_link: c.direct_link } : {}),
              ...(c.grif ? { grif: c.grif } : {}),
            },
          }
        : { kind: 'blockset', name: name.value, description: description.value, doc_type: '', content: { blocks: chosen.value } },
    )
    open.value = false
    emit('saved', created)
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    errors.value = { ...err.fields }
    problems.value = err.problems
    failure.value = err.problems.length ? t('saveTpl.failed') : Object.keys(err.fields).length ? '' : describeApiError(err)
  } finally {
    busy.value = false
  }
}

/** «content.blocks[2].data.text» → «Блок 3 · data.text» (номера — в выбранном диапазоне, для набора блоков — со смещением). */
function where(path: string): string {
  const m = /^content\.blocks\[(\d+)\]\.?(.*)$/.exec(path)
  if (!m) return path
  const n = Number(m[1]) + (kind.value === 'blockset' ? range.value.a : 1)
  return m[2] ? t('saveTpl.whereField', { n, field: m[2] }) : t('saveTpl.where', { n })
}
</script>

<template>
  <UiModal v-model:open="open" :title="$t('saveTpl.title')" size="lg" testid="tpl-save-dialog">
    <form novalidate @submit.prevent="save">
      <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>
      <ul v-if="problems.length" class="problems" data-testid="tpl-problems">
        <li v-for="p in problems" :key="`${p.path}|${p.message}`"><strong>{{ where(p.path) }}</strong>: {{ p.message }}</li>
      </ul>

      <fieldset class="kinds">
        <legend>{{ $t('saveTpl.what') }}</legend>
        <label class="kind" :class="{ 'is-active': kind === 'document' }">
          <input v-model="kind" type="radio" name="kind" value="document" data-testid="tpl-kind-document">
          <span class="kind__label">{{ $t('saveTpl.document') }}</span>
          <span class="kind__help">{{ $t('saveTpl.documentHelp', { type: docTypeName, n: blocks.length }) }}</span>
        </label>
        <label class="kind" :class="{ 'is-active': kind === 'blockset' }">
          <input v-model="kind" type="radio" name="kind" value="blockset" data-testid="tpl-kind-blockset">
          <span class="kind__label">{{ $t('saveTpl.blockset') }}</span>
          <span class="kind__help">{{ $t('saveTpl.blocksetHelp') }}</span>
        </label>
      </fieldset>

      <div v-if="kind === 'blockset'" class="range">
        <UiField :label="$t('saveTpl.from')">
          <UiSelect v-model="from" :options="blockOptions" name="from" />
        </UiField>
        <UiField :label="$t('saveTpl.to')">
          <UiSelect v-model="to" :options="blockOptions" name="to" />
        </UiField>
      </div>
      <p v-if="kind === 'blockset'" class="count" data-testid="tpl-count">{{ $t('saveTpl.count', { n: chosen.length }) }}</p>

      <UiField :label="$t('saveTpl.name')" required :error="errors.name">
        <UiInput v-model="name" :maxlength="100" name="name" data-testid="tpl-name" />
      </UiField>
      <UiField :label="$t('saveTpl.description')" :error="errors.description">
        <UiTextarea v-model="description" :rows="3" :maxlength="500" name="description" />
      </UiField>

      <div class="actions">
        <UiButton type="submit" variant="primary" :loading="busy" data-testid="tpl-submit">{{ $t('saveTpl.submit') }}</UiButton>
        <UiButton variant="link" @click="open = false">{{ $t('saveTpl.cancel') }}</UiButton>
      </div>
    </form>
  </UiModal>
</template>

<style scoped>
.problems {
  margin: 0 0 var(--space-4);
  padding: var(--space-3) var(--space-4) var(--space-3) var(--space-6);
  border: 2px solid var(--red-700);
  border-radius: var(--radius-2);
  background: #f8e6e1;
  color: var(--red-800);
}
.kinds {
  margin: 0 0 var(--space-4);
  padding: 0;
  border: 0;
}
.kinds legend {
  padding: 0;
  margin-bottom: var(--space-2);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.kind {
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
.kind.is-active {
  border-color: var(--ink-950);
  background: var(--paper-100);
}
.kind:has(input:focus-visible) {
  box-shadow:
    0 0 0 2px var(--focus-inner),
    0 0 0 5px var(--focus);
}
.kind input {
  grid-row: 1 / span 2;
  width: 1.25rem;
  height: 1.25rem;
  accent-color: var(--ink-900);
}
.kind__label {
  font-weight: 700;
}
.kind__help {
  grid-column: 2;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.range {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
  gap: 0 var(--space-3);
}
.count {
  margin: calc(-1 * var(--space-2)) 0 var(--space-4);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3);
  margin-top: var(--space-3);
}
</style>
