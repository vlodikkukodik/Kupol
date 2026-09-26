<script setup lang="ts">
// Очередь ходатайств о допуске (шаг 5.8): решают члены Особого Совета и Директорат; своё ходатайство — не решают.
import { computed, ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { ApiError, isApiError } from '@/api/client'
import { petitionsApi } from '@/api/endpoints'
import type { Item } from '@/api/generated/petitions'
import { keys } from '@/api/query'
import { describeApiError } from '@/composables/useForm'
import { formatDateTime } from '@/lib/format'
import { levelName } from '@/lib/levels'
import { t } from '@/i18n'
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

const client = useQueryClient()
const invitations = useQuery({ queryKey: keys.councilInvitations, queryFn: ({ signal }) => petitionsApi.councilInvitations({ signal }), staleTime: 0 })
const inviteLogin = ref('')
const inviteMessage = ref('')
const inviteErrors = ref<Record<string, string>>({})
const inviteBusy = ref(false)
async function invite() {
  inviteErrors.value = {}
  notice.value = ''
  failure.value = ''
  if (!inviteLogin.value.trim()) {
    inviteErrors.value.login = t('teamPetitions.enterLogin')
    return
  }
  inviteBusy.value = true
  try {
    await petitionsApi.invite({ login: inviteLogin.value.trim(), message: inviteMessage.value })
    notice.value = t('teamPetitions.invited', { who: inviteLogin.value.trim() })
    inviteLogin.value = ''
    inviteMessage.value = ''
    await client.invalidateQueries({ queryKey: keys.councilInvitations })
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    inviteErrors.value = { ...err.fields }
    if (!Object.keys(inviteErrors.value).length) failure.value = describeApiError(err)
  } finally {
    inviteBusy.value = false
  }
}
async function withdraw(id: number) {
  try {
    await petitionsApi.withdraw(id)
    await client.invalidateQueries({ queryKey: keys.councilInvitations })
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    failure.value = describeApiError(err)
  }
}
const list = useQuery({ queryKey: keys.teamPetitions, queryFn: ({ signal }) => petitionsApi.queue({ signal }), staleTime: 0 })
const requestId = computed(() => (isApiError(list.error.value) ? list.error.value.requestId : ''))
const forbidden = computed(() => isApiError(list.error.value) && list.error.value.code === 'forbidden')

const deciding = ref<{ item: Item; verdict: 'approved' | 'rejected' } | null>(null)
const open = computed({ get: () => deciding.value !== null, set: (v) => { if (!v) deciding.value = null } })
const comment = ref('')
const failure = ref('')
const notice = ref('')
const busy = ref(false)

function start(item: Item, verdict: 'approved' | 'rejected') {
  deciding.value = { item, verdict }
  comment.value = ''
  failure.value = ''
}
async function confirm() {
  const d = deciding.value
  if (!d) return
  busy.value = true
  failure.value = ''
  try {
    await petitionsApi.decide(d.item.id, { verdict: d.verdict, comment: comment.value })
    deciding.value = null
    notice.value = t(d.verdict === 'approved' ? 'teamPetitions.approvedNotice' : 'teamPetitions.rejectedNotice', { who: d.item.author ?? '' })
    await client.invalidateQueries({ queryKey: keys.teamPetitions })
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    failure.value = describeApiError(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <UiSheet v-if="forbidden" as="section" aria-labelledby="petitions-title">
    <h2 id="petitions-title">{{ $t('teamPetitions.title') }}</h2>
    <p>{{ $t('teamPetitions.councilOnly') }}</p>
  </UiSheet>
  <ErrorView v-else-if="list.isError.value" :request-id="requestId" :retrying="list.isFetching.value" @retry="list.refetch()" />
  <UiSheet v-else as="section" aria-labelledby="petitions-title" data-testid="team-petitions">
    <h2 id="petitions-title">{{ $t('teamPetitions.title') }}</h2>
    <p class="lead">{{ $t('teamPetitions.lead') }}</p>
    <p class="visually-hidden" role="status">{{ notice }}</p>
    <UiAlert v-if="notice" tone="success" :live="false">{{ notice }}</UiAlert>
    <UiSkeleton v-if="list.isPending.value" :lines="3" :label="$t('teamPetitions.loading')" />
    <UiEmpty v-else-if="!list.data.value?.length" icon="stamp" :title="$t('teamPetitions.empty')" />
    <ul v-else class="queue">
      <li v-for="p in list.data.value" :key="p.id" :data-petition="p.id">
        <div class="queue__head">
          <strong>{{ p.author }}</strong>
          <span>{{ $t('teamPetitions.wants', { from: p.author_level ?? 0, level: p.target_level, name: levelName(p.target_level) }) }}</span>
          <time :datetime="p.created_at">{{ formatDateTime(p.created_at) }}</time>
        </div>
        <p class="queue__text">{{ p.text }}</p>
        <div v-if="p.status === 'pending'" class="queue__actions">
          <UiButton variant="primary" size="sm" data-testid="petition-approve" @click="start(p, 'approved')">{{ $t('teamPetitions.approve') }}</UiButton>
          <UiButton size="sm" data-testid="petition-reject" @click="start(p, 'rejected')">{{ $t('teamPetitions.reject') }}</UiButton>
        </div>
        <p v-else class="queue__done">{{ $t(`teamPetitions.done.${p.status}`, { who: p.decided_by ?? '' }) }}<span v-if="p.comment"> — {{ p.comment }}</span></p>
      </li>
    </ul>

    <h3>{{ $t('teamPetitions.inviteTitle') }}</h3>
    <form novalidate class="invite" @submit.prevent="invite">
      <UiField :label="$t('teamPetitions.inviteLogin')" :hint="$t('teamPetitions.inviteHint')" :error="inviteErrors.login"><UiInput v-model="inviteLogin" autocomplete="off" /></UiField>
      <UiField :label="$t('teamPetitions.comment')" :error="inviteErrors.message"><UiTextarea v-model="inviteMessage" :rows="2" :maxlength="2000" /></UiField>
      <UiButton type="submit" variant="primary" :loading="inviteBusy" data-testid="invite-send">{{ $t('teamPetitions.inviteSend') }}</UiButton>
    </form>
    <ul v-if="invitations.data.value?.length" class="queue" data-testid="invitations-list">
      <li v-for="i in invitations.data.value" :key="i.id">
        <div class="queue__head">
          <strong>{{ i.user }}</strong>
          <span>{{ $t('teamPetitions.inviteLevel', { level: i.target_level, name: levelName(i.target_level) }) }}</span>
          <span>{{ $t(`teamPetitions.inviteStatus.${i.status}`) }}</span>
          <time :datetime="i.created_at">{{ formatDateTime(i.created_at) }}</time>
        </div>
        <UiButton v-if="i.status === 'pending'" size="sm" variant="ghost" @click="withdraw(i.id)">{{ $t('teamPetitions.withdraw') }}</UiButton>
      </li>
    </ul>

    <UiModal v-model:open="open" :title="deciding?.verdict === 'approved' ? $t('teamPetitions.approveTitle') : $t('teamPetitions.rejectTitle')" testid="petition-dialog">
      <form novalidate @submit.prevent="confirm">
        <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>
        <UiField :label="$t('teamPetitions.comment')" :hint="$t('teamPetitions.commentHint')">
          <UiTextarea v-model="comment" :rows="3" :maxlength="2000" />
        </UiField>
        <div class="actions">
          <UiButton type="submit" variant="primary" :loading="busy" data-testid="petition-confirm">{{ $t('teamPetitions.confirm') }}</UiButton>
          <UiButton variant="link" @click="deciding = null">{{ $t('teamPetitions.cancel') }}</UiButton>
        </div>
      </form>
    </UiModal>
  </UiSheet>
</template>

<style scoped>
.lead {
  max-width: 44rem;
  color: var(--text-muted);
}
.queue {
  margin: 0;
  padding: 0;
  list-style: none;
}
.queue li {
  padding: var(--space-3) 0;
  border-top: 1px dashed var(--border-strong);
}
.queue li:first-child {
  border-top: 0;
}
.queue__head {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1) var(--space-4);
}
.queue__head time {
  margin-left: auto;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.queue__text {
  margin: var(--space-1) 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.queue__done {
  margin: 0;
  color: var(--text-muted);
}
.invite {
  display: grid;
  gap: var(--space-2);
  max-width: 34rem;
  margin-bottom: var(--space-4);
}
.queue__actions,
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin-top: var(--space-2);
}
</style>
