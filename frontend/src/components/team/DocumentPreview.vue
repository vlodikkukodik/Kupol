<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from 'vue'
import { keepPreviousData, useQuery } from '@tanstack/vue-query'
import { isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { Content } from '@/api/generated/documents'
import DocumentPaper from '@/components/document/DocumentPaper.vue'
import { describeApiError } from '@/composables/useForm'
import { blockIndexes } from '@/editor/problems'
import { LEVEL_NAMES, levelName, requiredAccess } from '@/lib/levels'
import { describeRedactions, redactionStats } from '@/lib/preview'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'

// Предпросмотр «глазами уровня N». Документ строит сервер тем же кодом, что отдаёт его читателю (закрытое в ответ не
// попадает вовсе), поэтому здесь — только выбор уровня и показ ответа: ничего не скрывается на стороне браузера.
const props = defineProps<{
  docId: number
  /** Содержимое из редактора (с несохранёнными правками) */
  content: Content | null
  /** Есть ли несохранённые правки: без них серверу достаточно сохранённого */
  dirty: boolean
  /** Название вида блока по его типу (для ссылок на замечания) */
  kindName: (type: string) => string
}>()
const level = defineModel<number>('level', { default: 0 })
const emit = defineEmits<{ 'go-to-block': [id: string] }>()

const legendId = useId()

// Правки в редакторе идут по буквам; запрос — когда человек на мгновение остановился.
const settled = ref<Content | null>(props.content)
let timer: ReturnType<typeof setTimeout> | undefined
watch(
  () => props.content,
  (c) => {
    clearTimeout(timer)
    timer = setTimeout(() => (settled.value = c), 400)
  },
  { deep: true },
)
onBeforeUnmount(() => clearTimeout(timer))

const sent = computed(() => (props.dirty ? settled.value : null))
const query = useQuery({
  queryKey: computed(() => ['team', 'preview', props.docId, level.value, sent.value] as const),
  queryFn: ({ signal }) => teamApi.preview(props.docId, { level: level.value, ...(sent.value ? { content: sent.value } : {}) }, { signal }),
  placeholderData: keepPreviousData, // при смене уровня прежний лист остаётся, пока приходит новый
  retry: false,
  staleTime: 0,
  gcTime: 30_000,
})
const result = computed(() => query.data.value)
const error = computed(() => (isApiError(query.error.value) ? query.error.value : null))
const stale = computed(() => query.isPlaceholderData.value)

const who = computed(() => `уровня ${level.value} (${levelName(level.value)})`)
const stats = computed(() => redactionStats(result.value?.document))
const statsText = computed(() => {
  const what = describeRedactions(stats.value)
  return stats.value.blocks + stats.value.fragments === 0 ? `Для этого читателя ${what}.` : `Для этого читателя закрыто: ${what}.`
})
const source = computed(() => (props.dirty ? 'по несохранённым правкам' : 'по сохранённому документу'))

/** Замечания, привязанные к блокам: номер блока → его идентификатор и вид (по тому содержимому, что ушло на сервер). */
const problems = computed(() =>
  (result.value?.problems ?? []).map((p) => {
    const index = blockIndexes([p.path])[0]
    const block = index === undefined ? undefined : sent.value?.blocks[index] ?? props.content?.blocks[index]
    return { ...p, block: block ? { number: (index ?? 0) + 1, id: block.id, kind: props.kindName(block.type) } : null }
  }),
)

const announcement = computed(() => {
  const r = result.value
  if (!r || stale.value) return ''
  if (r.access === 'not_found') return `Читатель ${who.value} получит «Дело не найдено».`
  if (r.access === 'denied') return `Читатель ${who.value} увидит «Доступ запрещён».`
  return `Показан документ для читателя ${who.value}: ${describeRedactions(stats.value)}.`
})
</script>

<template>
  <section class="preview" aria-labelledby="preview-title" :aria-busy="query.isFetching.value ? 'true' : 'false'" data-testid="preview">
    <h3 id="preview-title" class="preview__title">Предпросмотр глазами читателя</h3>
    <p class="preview__lead">
      Так документ увидит человек с выбранным допуском. Собирает его сервер — так же, как при обычном чтении: закрытое до читателя не доходит.
      Показано {{ source }}; статус не учитывается — документ выглядит как опубликованный.
    </p>

    <fieldset class="levels" :aria-labelledby="legendId">
      <legend :id="legendId" class="levels__legend">Уровень читателя</legend>
      <label v-for="(name, n) in LEVEL_NAMES" :key="n" class="levels__item" :class="{ 'is-active': level === n }">
        <input v-model="level" type="radio" class="levels__radio" name="preview-level" :value="n" :data-testid="`preview-level-${n}`">
        <span class="levels__n">{{ n }}</span>
        <span class="levels__name">{{ name }}</span>
      </label>
    </fieldset>

    <p class="visually-hidden" role="status">{{ announcement }}</p>

    <UiAlert v-if="error" tone="danger">
      <p>{{ describeApiError(error) }}</p>
      <UiButton @click="query.refetch()">Повторить</UiButton>
    </UiAlert>

    <UiAlert v-if="problems.length" tone="warning" title="В предпросмотр не попали блоки с замечаниями" data-testid="preview-problems">
      <ul class="problems">
        <li v-for="p in problems" :key="`${p.path}|${p.message}`">
          <template v-if="p.block">
            <button type="button" class="problem-link" @click="emit('go-to-block', p.block.id)">Блок {{ p.block.number }} — {{ p.block.kind }}</button>: {{ p.message }}
          </template>
          <template v-else><code>{{ p.path }}</code>: {{ p.message }}</template>
        </li>
      </ul>
    </UiAlert>

    <UiSkeleton v-if="!result && !error" :lines="8" label="Собираем документ для читателя…" />

    <template v-else-if="result">
      <UiAlert v-if="result.access === 'not_found'" tone="warning" data-testid="preview-verdict" :data-access="result.access">
        Читатель {{ who }} получит «Дело не найдено»: документ закрыт ({{ requiredAccess(result.required_level) }}), а его существование
        закрытый режим не раскрывает.
      </UiAlert>
      <UiAlert v-else-if="result.access === 'denied'" tone="warning" data-testid="preview-verdict" :data-access="result.access">
        Читатель {{ who }} увидит «Доступ запрещён» и узнает, что нужен допуск: {{ requiredAccess(result.required_level) }}. Содержимое ему не отдаётся.
      </UiAlert>
      <template v-else-if="result.document">
        <p class="preview__stats" data-testid="preview-stats">{{ statsText }}</p>
        <div class="preview__sheet" :class="{ 'is-stale': stale }" data-testid="preview-sheet">
          <DocumentPaper :doc="result.document" />
        </div>
      </template>
    </template>
  </section>
</template>

<style scoped>
.preview__title {
  margin-bottom: var(--space-2);
}
.preview__lead {
  max-width: 44rem;
  color: var(--text-muted);
}
.levels {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(7.5rem, 1fr));
  gap: var(--space-2);
  margin: var(--space-4) 0;
  padding: 0;
  border: 0;
}
.levels__legend {
  grid-column: 1 / -1;
  padding: 0;
  margin-bottom: var(--space-1);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.levels__item {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 3.5rem;
  padding: var(--space-2);
  border: 2px solid var(--border-strong);
  border-radius: var(--radius-2);
  background: var(--surface-raised);
  text-align: center;
  cursor: pointer;
}
.levels__item:hover {
  border-color: var(--ink-900);
}
.levels__item.is-active {
  border-color: var(--ink-950);
  background: var(--ink-900);
  color: var(--paper-50);
}
/* Двойное кольцо фокуса — на подписи: сам радиокнопка скрыта, но остаётся настоящей (клавиши-стрелки, скринридер) */
.levels__item:has(.levels__radio:focus-visible) {
  box-shadow:
    0 0 0 2px var(--focus-inner),
    0 0 0 5px var(--focus);
}
.levels__radio {
  position: absolute;
  opacity: 0;
  pointer-events: none;
}
.levels__n {
  font-family: var(--font-head);
  font-size: var(--text-xl);
  font-weight: 700;
  line-height: 1;
}
.levels__name {
  font-size: var(--text-sm);
}
.problems {
  margin: 0;
  padding-left: var(--space-5);
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
.preview__stats {
  margin: var(--space-3) 0;
  font-weight: 700;
}
.preview__sheet {
  transition: opacity var(--dur-fast) var(--ease);
}
.preview__sheet.is-stale {
  opacity: 0.55;
}
@media (prefers-reduced-motion: reduce) {
  .preview__sheet {
    transition: none;
  }
}
</style>
