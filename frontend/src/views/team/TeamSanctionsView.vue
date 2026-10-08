<script setup lang="ts">
// Наказания (шаг 5.9): предупреждение → временная блокировка комментариев → бан. Накладывают Модератор и Директорат;
// причина обязательна и остаётся в журнале. Бан закрывает вход, блокировка запрещает писать пометки; всё снимается.
import { computed, ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { ApiError, isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { Item } from '@/api/generated/sanctions'
import { keys } from '@/api/query'
import { describeApiError } from '@/composables/useForm'
import { formatDateTime } from '@/lib/format'
import { t } from '@/i18n'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiEmpty from '@/ui/UiEmpty.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiSelect from '@/ui/UiSelect.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import UiTextarea from '@/ui/UiTextarea.vue'
import ErrorView from '../ErrorView.vue'

const client = useQueryClient()
const filter = ref('')
const applied = ref('')
const list = useQuery({ queryKey: computed(() => keys.teamSanctions(applied.value)), queryFn: ({ signal }) => teamApi.sanctions(applied.value, { signal }), staleTime: 0 })
const requestId = computed(() => (isApiError(list.error.value) ? list.error.value.requestId : ''))

const login = ref('')
const kind = ref<'warning' | 'comment_ban' | 'ban'>('warning')
const days = ref('7')
const reason = ref('')
const errors = ref<Record<string, string>>({})
const failure = ref('')
const notice = ref('')
const busy = ref(false)
const kinds = computed(() => [
  { value: 'warning', label: t('teamSanctions.kind.warning') },
  { value: 'comment_ban', label: t('teamSanctions.kind.comment_ban') },
  { value: 'ban', label: t('teamSanctions.kind.ban') },
])

function fail(err: unknown) {
  if (!(err instanceof ApiError)) throw err
  errors.value = { ...err.fields }
  failure.value = Object.keys(errors.value).length ? '' : describeApiError(err)
}

async function issue() {
  errors.value = {}
  failure.value = ''
  notice.value = ''
  if (!login.value.trim()) errors.value.login = t('teamSanctions.enterLogin')
  if (!reason.value.trim()) errors.value.reason = t('teamSanctions.enterReason')
  if (Object.keys(errors.value).length) return
  busy.value = true
  try {
    await teamApi.issueSanction({ login: login.value.trim(), kind: kind.value, reason: reason.value, days: kind.value === 'comment_ban' ? Number(days.value) || 0 : 0 })
    notice.value = t('teamSanctions.issued', { login: login.value.trim() })
    reason.value = ''
    await client.invalidateQueries({ queryKey: ['team', 'sanctions'] })
  } catch (err) {
    fail(err)
  } finally {
    busy.value = false
  }
}

async function revoke(item: Item) {
  failure.value = ''
  try {
    await teamApi.revokeSanction(item.id)
    notice.value = t('teamSanctions.revoked', { login: item.user })
    await client.invalidateQueries({ queryKey: ['team', 'sanctions'] })
  } catch (err) {
    fail(err)
  }
}
</script>

<template>
  <ErrorView v-if="list.isError.value" :request-id="requestId" :retrying="list.isFetching.value" @retry="list.refetch()" />
  <UiSheet v-else as="section" aria-labelledby="sanctions-title" data-testid="team-sanctions">
    <h2 id="sanctions-title">{{ $t('teamSanctions.title') }}</h2>
    <p class="lead">{{ $t('teamSanctions.lead') }}</p>
    <p class="visually-hidden" role="status">{{ notice }}</p>
    <UiAlert v-if="notice" tone="success" :live="false">{{ notice }}</UiAlert>
    <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>

    <form novalidate class="issue" @submit.prevent="issue">
      <UiField :label="$t('teamSanctions.login')" required :error="errors.login"><UiInput v-model="login" autocomplete="off" /></UiField>
      <UiField :label="$t('teamSanctions.kindLabel')" :error="errors.kind"><UiSelect v-model="kind" :options="kinds" /></UiField>
      <UiField v-if="kind === 'comment_ban'" :label="$t('teamSanctions.days')" :hint="$t('teamSanctions.daysHint')" :error="errors.days">
        <UiInput v-model="days" type="number" min="1" max="365" />
      </UiField>
      <UiField :label="$t('teamSanctions.reason')" required :error="errors.reason"><UiTextarea v-model="reason" :rows="3" :maxlength="1000" /></UiField>
      <UiButton type="submit" variant="primary" :loading="busy" data-testid="sanction-issue">{{ $t('teamSanctions.issue') }}</UiButton>
    </form>

    <h3>{{ $t('teamSanctions.journal') }}</h3>
    <form class="filter" @submit.prevent="applied = filter.trim()">
      <UiField :label="$t('teamSanctions.filter')"><UiInput v-model="filter" autocomplete="off" /></UiField>
      <UiButton type="submit" icon="search">{{ $t('teamSanctions.find') }}</UiButton>
    </form>
    <UiSkeleton v-if="list.isPending.value" :lines="3" :label="$t('teamSanctions.loading')" />
    <UiEmpty v-else-if="!list.data.value?.length" icon="stamp" :title="$t('teamSanctions.empty')" />
    <ul v-else class="journal" data-testid="sanctions-list">
      <li v-for="s in list.data.value" :key="s.id" :class="{ 'is-active': s.active }">
        <div class="journal__head">
          <strong>{{ s.user }}</strong>
          <span>{{ $t(`teamSanctions.kind.${s.kind}`) }}</span>
          <span v-if="s.expires_at">{{ $t('teamSanctions.until', { when: formatDateTime(s.expires_at) }) }}</span>
          <time :datetime="s.created_at">{{ formatDateTime(s.created_at) }}</time>
        </div>
        <p class="journal__reason">{{ s.reason }}</p>
        <p class="journal__meta">
          <span v-if="s.issued_by">{{ $t('teamSanctions.by', { who: s.issued_by }) }}</span>
          <span v-if="s.revoked_at"> · {{ $t('teamSanctions.revokedAt', { when: formatDateTime(s.revoked_at) }) }}</span>
          <span v-else-if="s.active"> · {{ $t('teamSanctions.active') }}</span>
        </p>
        <UiButton v-if="!s.revoked_at && s.kind !== 'warning'" size="sm" variant="ghost" @click="revoke(s)">{{ $t('teamSanctions.revoke') }}</UiButton>
      </li>
    </ul>
  </UiSheet>
</template>

<style scoped>
.lead {
  max-width: 44rem;
  color: var(--text-muted);
}
.issue,
.filter {
  display: grid;
  gap: var(--space-2);
  max-width: 34rem;
  margin-bottom: var(--space-4);
}
.journal {
  margin: 0;
  padding: 0;
  list-style: none;
}
.journal li {
  padding: var(--space-3) 0;
  border-top: 1px dashed var(--border-strong);
}
.journal li.is-active {
  border-left: 4px solid var(--red-700);
  padding-left: var(--space-3);
}
.journal__head {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-4);
}
.journal__head time {
  margin-left: auto;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.journal__reason {
  margin: var(--space-1) 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.journal__meta {
  margin: 0 0 var(--space-1);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
</style>
