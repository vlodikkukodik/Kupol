<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/vue-query'
import { ApiError, isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { TermOut } from '@/api/generated/documents'
import { keys } from '@/api/query'
import { describeApiError } from '@/composables/useForm'
import { useQueryFilters } from '@/composables/useQueryFilters'
import { useAuthStore } from '@/stores/auth'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiEmpty from '@/ui/UiEmpty.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiModal from '@/ui/UiModal.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import UiTextarea from '@/ui/UiTextarea.vue'
import ErrorView from '../ErrorView.vue'

// Глоссарий канона: как пишется термин, что он значит, какие есть другие написания. Читают все члены команды; заводят и правят
// те, у кого право вести глоссарий. Поиск — по термину, написаниям и определению; запрос живёт в адресе (?q=).
const route = useRoute()
const filters = useQueryFilters()
const client = useQueryClient()
const auth = useAuthStore()

const q = computed(() => (typeof route.query.q === 'string' ? route.query.q : ''))
const search = ref(q.value)
const list = useQuery({
  queryKey: computed(() => keys.teamGlossary(q.value)),
  queryFn: ({ signal }) => teamApi.glossary(q.value, { signal }),
  placeholderData: keepPreviousData,
  staleTime: 0,
})
const requestId = computed(() => (isApiError(list.error.value) ? list.error.value.requestId : ''))
const canManage = computed(() => auth.can('manage_glossary'))
const notice = ref('')

function find() {
  void filters.change({ q: search.value.trim() })
}
function clear() {
  search.value = ''
  void filters.change({ q: '' })
}

// ——— форма термина (создание и правка) ———
const editing = ref<TermOut | 'new' | null>(null)
const dialogOpen = computed({ get: () => editing.value !== null, set: (v) => { if (!v) editing.value = null } })
const term = ref('')
const definition = ref('')
const aliases = ref('')
const errors = ref<Record<string, string>>({})
const failure = ref('')
const busy = ref(false)

function openForm(t: TermOut | 'new') {
  editing.value = t
  term.value = t === 'new' ? '' : t.term
  definition.value = t === 'new' ? '' : t.definition
  aliases.value = t === 'new' ? '' : t.aliases.join('\n')
  errors.value = {}
  failure.value = ''
}

function fail(err: unknown) {
  if (!(err instanceof ApiError)) throw err
  errors.value = { ...err.fields }
  for (const p of err.problems) errors.value[p.path] = errors.value[p.path] ? `${errors.value[p.path]}; ${p.message}` : p.message
  failure.value = Object.keys(errors.value).length ? '' : describeApiError(err)
}

async function refresh(message: string) {
  notice.value = message
  await client.invalidateQueries({ queryKey: ['team', 'glossary'] })
}

async function save() {
  errors.value = {}
  failure.value = ''
  if (!term.value.trim()) errors.value.term = 'Введите термин'
  if (!definition.value.trim()) errors.value.definition = 'Напишите определение'
  if (Object.keys(errors.value).length) return
  const body = { term: term.value, definition: definition.value, aliases: aliases.value.split('\n') }
  busy.value = true
  try {
    const current = editing.value
    if (current === 'new') await teamApi.createTerm(body)
    else if (current) await teamApi.updateTerm(current.id, body)
    editing.value = null
    await refresh(current === 'new' ? `Термин «${body.term.trim()}» добавлен.` : `Термин «${body.term.trim()}» сохранён.`)
  } catch (err) {
    fail(err)
  } finally {
    busy.value = false
  }
}

const removing = ref<TermOut | null>(null)
const removeOpen = computed({ get: () => removing.value !== null, set: (v) => { if (!v) removing.value = null } })
async function confirmRemove() {
  const t = removing.value
  if (!t) return
  failure.value = ''
  busy.value = true
  try {
    await teamApi.deleteTerm(t.id)
    removing.value = null
    await refresh(`Термин «${t.term}» удалён.`)
  } catch (err) {
    fail(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ErrorView v-if="list.isError.value" :request-id="requestId" :retrying="list.isFetching.value" @retry="list.refetch()" />
  <UiSheet v-else as="section" aria-labelledby="glossary-title" data-testid="glossary">
    <div class="head">
      <h2 id="glossary-title">Глоссарий канона</h2>
      <UiButton v-if="canManage" variant="primary" icon="plus" data-testid="term-new" @click="openForm('new')">Добавить термин</UiButton>
    </div>
    <p class="lead">Справочник терминов вселенной: как пишется, что значит, какие бывают другие написания. Сверяйтесь с ним, когда пишете документ.</p>

    <form class="search" role="search" aria-label="Поиск по глоссарию" @submit.prevent="find">
      <UiField label="Найти" hint="По термину, другому написанию или определению">
        <UiInput v-model="search" type="search" :maxlength="100" name="q" />
      </UiField>
      <div class="search__actions">
        <UiButton type="submit" icon="search">Найти</UiButton>
        <UiButton v-if="q" variant="link" @click="clear">Сбросить</UiButton>
      </div>
    </form>

    <p class="visually-hidden" role="status">{{ notice }}</p>
    <p v-if="notice" class="notice" data-testid="glossary-notice">{{ notice }}</p>
    <p v-if="list.data.value" class="total" data-testid="glossary-total">Найдено: {{ list.data.value.length }}</p>

    <UiSkeleton v-if="list.isPending.value" :lines="4" label="Загружаем глоссарий…" />
    <UiEmpty v-else-if="!list.data.value?.length" icon="book" :title="q ? 'Ничего не найдено' : 'Глоссарий пока пуст'">
      <template v-if="q">По запросу «{{ q }}» терминов нет: проверьте написание или сбросьте поиск.</template>
      <template v-else-if="canManage">Добавьте первый термин кнопкой выше.</template>
      <template v-else>Термины добавляют Редактор и Архивариус.</template>
    </UiEmpty>
    <dl v-else class="terms" data-testid="glossary-list">
      <div v-for="t in list.data.value" :key="t.id" class="term" :data-term="t.id">
        <dt class="term__name">{{ t.term }}</dt>
        <dd class="term__body">
          <p class="term__def">{{ t.definition }}</p>
          <p v-if="t.aliases.length" class="term__aliases">Другие написания: {{ t.aliases.join(' · ') }}</p>
          <div v-if="t.can_edit" class="term__actions">
            <UiButton size="sm" icon="edit" data-testid="term-edit" @click="openForm(t)">Изменить</UiButton>
            <UiButton size="sm" variant="ghost" icon="trash" data-testid="term-delete" @click="removing = t">Удалить</UiButton>
          </div>
        </dd>
      </div>
    </dl>

    <UiModal v-model:open="dialogOpen" :title="editing === 'new' ? 'Новый термин' : 'Изменить термин'" testid="term-dialog">
      <form novalidate @submit.prevent="save">
        <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>
        <UiField label="Термин" required :error="errors.term">
          <UiInput v-model="term" :maxlength="100" name="term" />
        </UiField>
        <UiField label="Определение" required :error="errors.definition">
          <UiTextarea v-model="definition" :rows="5" :maxlength="2000" name="definition" />
        </UiField>
        <UiField label="Другие написания" hint="По одному на строке, не больше десяти. Ищутся так же, как сам термин." :error="errors.aliases">
          <UiTextarea v-model="aliases" :rows="3" name="aliases" />
        </UiField>
        <div class="dlg-actions">
          <UiButton type="submit" variant="primary" :loading="busy" data-testid="term-save">Сохранить</UiButton>
          <UiButton variant="link" @click="editing = null">Отмена</UiButton>
        </div>
      </form>
    </UiModal>

    <UiModal v-model:open="removeOpen" title="Удалить термин?" testid="term-delete-dialog">
      <p>Термин «{{ removing?.term }}» будет удалён из глоссария.</p>
      <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>
      <div class="dlg-actions">
        <UiButton variant="danger" icon="trash" :loading="busy" data-testid="term-confirm-delete" @click="confirmRemove">Удалить</UiButton>
        <UiButton variant="link" @click="removing = null">Отмена</UiButton>
      </div>
    </UiModal>
  </UiSheet>
</template>

<style scoped>
.head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
}
.head h2 {
  margin: 0;
}
.lead {
  max-width: 44rem;
  color: var(--text-muted);
}
.search {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: var(--space-2) var(--space-3);
  margin: var(--space-3) 0;
}
.search :deep(.ui-field) {
  flex: 1 1 18rem;
  margin-bottom: 0;
}
.search__actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
.notice {
  margin: 0 0 var(--space-3);
  padding: var(--space-2) var(--space-3);
  border-left: 4px solid var(--success);
  background: var(--surface-sunken);
}
.total {
  margin: 0 0 var(--space-3);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.terms {
  margin: 0;
}
.term {
  padding: var(--space-3) 0;
  border-bottom: 1px dashed var(--border-strong);
}
.term:last-child {
  border-bottom: 0;
}
.term__name {
  font-family: var(--font-head);
  font-size: var(--text-lg);
  font-weight: 700;
  letter-spacing: 0.04em;
  overflow-wrap: anywhere;
}
.term__body {
  margin: var(--space-1) 0 0;
}
.term__def {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.term__aliases {
  margin: var(--space-1) 0 0;
  color: var(--text-muted);
  font-size: var(--text-sm);
  overflow-wrap: anywhere;
}
.term__actions,
.dlg-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-2);
}
</style>
