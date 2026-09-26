<script setup lang="ts">
// «Предложения» (шаг 5.4, спецификация §8): одна форма (идея или замечание) для вошедших читателей и
// собственный список: статусы получено → рассмотрено → принято/отклонено; «записка» автору — пояснение
// Редактора рядом со статусом (отдельными записками внутренней почты — шаг 5.7).
// Очередь разбирают Редакторы: /team/suggestions (право review).
import { computed, ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { isApiError } from '@/api/client'
import { suggestionsApi } from '@/api/endpoints'
import type { Status } from '@/api/generated/suggestions'
import { keys } from '@/api/query'
import { useForm } from '@/composables/useForm'
import { formatDateTime } from '@/lib/format'
import { t } from '@/i18n'
import UiAlert from '@/ui/UiAlert.vue'
import UiBadge from '@/ui/UiBadge.vue'
import UiButton from '@/ui/UiButton.vue'
import UiEmpty from '@/ui/UiEmpty.vue'
import UiField from '@/ui/UiField.vue'
import UiPageHeader from '@/ui/UiPageHeader.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import UiTextarea from '@/ui/UiTextarea.vue'
import ErrorView from './ErrorView.vue'

const MAX_TEXT = 2000

/** Статус → цвет метки: полученное ещё в работе, отклонённое — красное. */
const TONES: Record<Status, 'draft' | 'review' | 'published' | 'danger'> = {
  received: 'draft',
  reviewed: 'review',
  accepted: 'published',
  rejected: 'danger',
}

const client = useQueryClient()
const list = useQuery({
  queryKey: keys.suggestionsMine,
  queryFn: ({ signal }) => suggestionsApi.mine({ signal }),
  staleTime: 0,
})
const items = computed(() => list.data.value ?? [])
const requestId = computed(() => (isApiError(list.error.value) ? list.error.value.requestId : ''))

const text = ref('')
const notice = ref('')
const form = useForm()
const left = computed(() => MAX_TEXT - [...text.value].length)

async function submit() {
  notice.value = ''
  const ok = await form.submit(() => suggestionsApi.create({ text: text.value }))
  if (!ok) return
  text.value = ''
  notice.value = t('suggest.form.sent')
  await client.invalidateQueries({ queryKey: keys.suggestionsMine })
}
</script>

<template>
  <UiPageHeader :title="$t('suggest.title')" :kicker="$t('suggest.kicker')">
    <p>{{ $t('suggest.lead') }}</p>
    <p>{{ $t('suggest.xpHint') }}</p>
  </UiPageHeader>

  <UiSheet as="section" class="block" aria-labelledby="suggest-form-title" data-testid="suggest-form">
    <h2 id="suggest-form-title">{{ $t('suggest.form.title') }}</h2>
    <form novalidate @submit.prevent="submit">
      <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
      <UiAlert v-if="notice" tone="success" data-testid="suggest-sent">{{ notice }}</UiAlert>
      <UiField :label="$t('suggest.form.label')" :hint="$t('suggest.form.hint')" :error="form.errors.text">
        <UiTextarea v-model="text" name="text" :rows="6" :maxlength="MAX_TEXT" data-testid="suggest-text" />
      </UiField>
      <div class="actions">
        <UiButton
          type="submit"
          variant="primary"
          :loading="form.submitting.value"
          :disabled="!text.trim()"
          data-testid="suggest-submit"
        >
          {{ $t('suggest.form.submit') }}
        </UiButton>
        <span class="counter">{{ $t('suggest.form.left', { n: left }) }}</span>
      </div>
    </form>
  </UiSheet>

  <section class="block" aria-labelledby="suggest-mine-title">
    <h2 id="suggest-mine-title">{{ $t('suggest.mine.title') }}</h2>

    <UiSkeleton v-if="list.isPending.value" :lines="3" :label="$t('suggest.mine.loading')" />
    <ErrorView v-else-if="list.isError.value" :request-id="requestId" :retrying="list.isFetching.value" @retry="list.refetch()" />
    <UiEmpty v-else-if="!items.length" :title="$t('suggest.mine.empty')" />
    <ul v-else class="items" data-testid="suggest-list">
      <li v-for="item in items" :key="item.id">
        <UiSheet as="article" class="item">
          <p class="item__meta">
            <UiBadge :tone="TONES[item.status] ?? 'neutral'">{{ $t(`suggest.status.${item.status}`) }}</UiBadge>
            <span> · {{ $t('suggest.mine.sentAt', { when: formatDateTime(item.created_at) }) }}</span>
          </p>
          <p class="item__text">{{ item.text }}</p>
          <div v-if="item.handled_at || item.comment" class="item__decision" data-testid="suggest-decision">
            <p v-if="item.handled_at" class="item__when">
              {{ $t('suggest.mine.decided', { when: formatDateTime(item.handled_at), who: item.handled_by ?? '' }) }}
            </p>
            <blockquote v-if="item.comment" class="item__note">
              <span class="item__note-label">{{ $t('suggest.mine.note') }}:</span>
              {{ item.comment }}
            </blockquote>
          </div>
        </UiSheet>
      </li>
    </ul>
  </section>
</template>

<style scoped>
.block {
  margin-bottom: var(--space-5);
}
.actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3);
}
.counter {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.items {
  margin: 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: var(--space-3);
}
.item {
  margin: 0;
  max-width: none;
}
.item__meta {
  margin: 0 0 var(--space-2);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.item__text {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.item__decision {
  margin-top: var(--space-3);
  padding-top: var(--space-2);
  border-top: 1px dashed var(--border);
}
.item__when {
  margin: 0 0 var(--space-1);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.item__note {
  margin: 0;
  padding-left: var(--space-3);
  border-left: 3px solid var(--border-strong);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.item__note-label {
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
</style>
