<script setup lang="ts">
import { computed, ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { ApiError, isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { TimelineEventOut } from '@/api/generated/documents'
import { keys } from '@/api/query'
import { describeApiError } from '@/composables/useForm'
import { LEVEL_NAMES } from '@/lib/levels'
import { MONTHS_NOMINATIVE, formatComposed } from '@/lib/format'
import { useAuthStore } from '@/stores/auth'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiEmpty from '@/ui/UiEmpty.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiModal from '@/ui/UiModal.vue'
import UiSelect from '@/ui/UiSelect.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import UiTextarea from '@/ui/UiTextarea.vue'
import ErrorView from '../ErrorView.vue'

// Хронология «О КУПОЛЕ»: события вселенной 1974 — наши дни. Читают все члены команды (со всеми событиями), ведут Редактор,
// Архивариус и Директорат. Читателям на странице «О КУПОЛЕ» видны только события не выше их допуска.
const client = useQueryClient()
const auth = useAuthStore()
const list = useQuery({ queryKey: keys.teamTimeline, queryFn: ({ signal }) => teamApi.timeline({ signal }), staleTime: 0 })
const requestId = computed(() => (isApiError(list.error.value) ? list.error.value.requestId : ''))
const canManage = computed(() => auth.can('manage_timeline'))
const notice = ref('')

const monthOptions = MONTHS_NOMINATIVE.map((name, i) => ({ value: String(i + 1), label: name }))
const levelOptions = LEVEL_NAMES.map((name, i) => ({ value: String(i), label: i === 0 ? `0 — ${name}: видно всем` : `${i} — ${name}` }))

// ——— форма события ———
const editing = ref<TimelineEventOut | 'new' | null>(null)
const dialogOpen = computed({ get: () => editing.value !== null, set: (v) => { if (!v) editing.value = null } })
const year = ref('')
const month = ref('')
const day = ref('')
const title = ref('')
const body = ref('')
const level = ref('0')
const documentCode = ref('')
const errors = ref<Record<string, string>>({})
const failure = ref('')
const busy = ref(false)

function openForm(e: TimelineEventOut | 'new') {
  editing.value = e
  const src = e === 'new' ? null : e
  year.value = src ? String(src.year) : ''
  month.value = src?.month ? String(src.month) : ''
  day.value = src?.day ? String(src.day) : ''
  title.value = src?.title ?? ''
  body.value = src?.body ?? ''
  level.value = String(src?.level ?? 0)
  documentCode.value = src?.document_code ?? ''
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
  await Promise.all([client.invalidateQueries({ queryKey: keys.teamTimeline }), client.invalidateQueries({ queryKey: keys.timeline })])
}

async function save() {
  errors.value = {}
  failure.value = ''
  if (!year.value.trim()) errors.value.year = 'Введите год'
  if (!title.value.trim()) errors.value.title = 'Введите название события'
  if (Object.keys(errors.value).length) return
  const input = {
    year: Number(year.value),
    ...(month.value ? { month: Number(month.value) } : {}),
    ...(day.value ? { day: Number(day.value) } : {}),
    title: title.value,
    body: body.value,
    level: Number(level.value),
    document_code: documentCode.value,
  }
  busy.value = true
  try {
    const current = editing.value
    if (current === 'new') await teamApi.createEvent(input)
    else if (current) await teamApi.updateEvent(current.id, input)
    editing.value = null
    await refresh(current === 'new' ? `Событие «${input.title.trim()}» добавлено.` : `Событие «${input.title.trim()}» сохранено.`)
  } catch (err) {
    fail(err)
  } finally {
    busy.value = false
  }
}

const removing = ref<TimelineEventOut | null>(null)
const removeOpen = computed({ get: () => removing.value !== null, set: (v) => { if (!v) removing.value = null } })
async function confirmRemove() {
  const e = removing.value
  if (!e) return
  failure.value = ''
  busy.value = true
  try {
    await teamApi.deleteEvent(e.id)
    removing.value = null
    await refresh(`Событие «${e.title}» удалено.`)
  } catch (err) {
    fail(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ErrorView v-if="list.isError.value" :request-id="requestId" :retrying="list.isFetching.value" @retry="list.refetch()" />
  <UiSheet v-else as="section" aria-labelledby="timeline-title" data-testid="team-timeline">
    <div class="head">
      <h2 id="timeline-title">Хронология «О КУПОЛЕ»</h2>
      <UiButton v-if="canManage" variant="primary" icon="plus" data-testid="event-new" @click="openForm('new')">Добавить событие</UiButton>
    </div>
    <p class="lead">
      События вселенной, которые видят читатели на странице «О КУПОЛЕ». У каждого события свой допуск: читатель видит только события не выше своего уровня.
      Ссылка на документ показывается, только если читатель вправе открыть сам документ.
    </p>

    <p class="visually-hidden" role="status">{{ notice }}</p>
    <p v-if="notice" class="notice" data-testid="timeline-notice">{{ notice }}</p>

    <UiSkeleton v-if="list.isPending.value" :lines="4" label="Загружаем хронологию…" />
    <UiEmpty v-else-if="!list.data.value?.length" icon="clock" title="Хронология пока пуста">
      <template v-if="canManage">Добавьте первое событие кнопкой выше.</template>
      <template v-else>События добавляют Редактор и Архивариус.</template>
    </UiEmpty>
    <ol v-else class="events" data-testid="event-list">
      <li v-for="e in list.data.value" :key="e.id" class="event" :data-event="e.id">
        <p class="event__date">{{ formatComposed({ year: e.year, month: e.month, day: e.day }) }}</p>
        <div class="event__main">
          <h3 class="event__title">{{ e.title }}</h3>
          <p v-if="e.body" class="event__body">{{ e.body }}</p>
          <p class="event__meta">
            Допуск: {{ e.level }} ({{ LEVEL_NAMES[e.level] }})<template v-if="e.document_code"> · документ {{ e.document_code }}</template>
          </p>
          <div v-if="e.can_edit" class="event__actions">
            <UiButton size="sm" icon="edit" data-testid="event-edit" @click="openForm(e)">Изменить</UiButton>
            <UiButton size="sm" variant="ghost" icon="trash" data-testid="event-delete" @click="removing = e">Удалить</UiButton>
          </div>
        </div>
      </li>
    </ol>

    <UiModal v-model:open="dialogOpen" :title="editing === 'new' ? 'Новое событие' : 'Изменить событие'" testid="event-dialog">
      <form novalidate @submit.prevent="save">
        <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>
        <div class="date">
          <UiField label="Год" required :error="errors.year">
            <UiInput v-model="year" type="number" :min="1900" :max="2099" inputmode="numeric" name="year" />
          </UiField>
          <UiField label="Месяц" :error="errors.month">
            <UiSelect v-model="month" :options="monthOptions" placeholder="не указан" name="month" />
          </UiField>
          <UiField label="День" :error="errors.day">
            <UiInput v-model="day" type="number" :min="1" :max="31" inputmode="numeric" name="day" />
          </UiField>
        </div>
        <UiField label="Название" required :error="errors.title">
          <UiInput v-model="title" :maxlength="200" name="title" />
        </UiField>
        <UiField label="Описание" hint="Необязательно, до 2000 знаков." :error="errors.body">
          <UiTextarea v-model="body" :rows="4" :maxlength="2000" name="body" />
        </UiField>
        <UiField label="Допуск события" hint="Читатель ниже этого уровня события не увидит." :error="errors.level">
          <UiSelect v-model="level" :options="levelOptions" name="level" />
        </UiField>
        <UiField label="Документ" hint="Шифр, например О-041. Необязательно." :error="errors.document_code">
          <UiInput v-model="documentCode" :maxlength="40" name="document_code" />
        </UiField>
        <div class="dlg-actions">
          <UiButton type="submit" variant="primary" :loading="busy" data-testid="event-save">Сохранить</UiButton>
          <UiButton variant="link" @click="editing = null">Отмена</UiButton>
        </div>
      </form>
    </UiModal>

    <UiModal v-model:open="removeOpen" title="Удалить событие?" testid="event-delete-dialog">
      <p>Событие «{{ removing?.title }}» будет удалено из хронологии.</p>
      <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>
      <div class="dlg-actions">
        <UiButton variant="danger" icon="trash" :loading="busy" data-testid="event-confirm-delete" @click="confirmRemove">Удалить</UiButton>
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
.notice {
  margin: 0 0 var(--space-3);
  padding: var(--space-2) var(--space-3);
  border-left: 4px solid var(--success);
  background: var(--surface-sunken);
}
.events {
  margin: 0;
  padding: 0;
  list-style: none;
}
.event {
  display: grid;
  grid-template-columns: 10rem 1fr;
  gap: var(--space-4);
  padding: var(--space-3) 0;
  border-bottom: 1px dashed var(--border-strong);
}
.event:last-child {
  border-bottom: 0;
}
.event__date {
  margin: 0;
  font-family: var(--font-head);
  font-weight: 700;
  letter-spacing: 0.04em;
}
.event__title {
  margin: 0;
  font-size: var(--text-md);
  overflow-wrap: anywhere;
}
.event__body {
  margin: var(--space-1) 0 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.event__meta {
  margin: var(--space-1) 0 0;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.event__actions,
.dlg-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-2);
}
.date {
  display: grid;
  grid-template-columns: 1fr 1.5fr 1fr;
  gap: 0 var(--space-3);
}
@media (max-width: 40rem) {
  .event {
    grid-template-columns: 1fr;
    gap: var(--space-1);
  }
  .date {
    grid-template-columns: 1fr;
  }
}
</style>
