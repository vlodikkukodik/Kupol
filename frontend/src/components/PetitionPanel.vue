<script setup lang="ts">
// Ходатайство о допуске (шаг 5.8): с уровня 3 можно просить следующий (4–6); решает Особый Совет.
import { computed, ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { petitionsApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { ApiError } from '@/api/client'
import { describeApiError, useForm } from '@/composables/useForm'
import { formatDate } from '@/lib/format'
import { levelName } from '@/lib/levels'
import { t } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiTextarea from '@/ui/UiTextarea.vue'

const auth = useAuthStore()
const invites = useQuery({ queryKey: keys.myInvitations, queryFn: ({ signal }) => petitionsApi.invitations({ signal }), staleTime: 0 })
const pendingInvite = computed(() => invites.data.value?.find((i) => i.status === 'pending'))
const inviteError = ref('')
async function answer(id: number, a: 'accept' | 'decline') {
  inviteError.value = ''
  try {
    await petitionsApi.respond(id, a)
    await Promise.all([auth.load(true), client.invalidateQueries({ queryKey: ['me'] })])
  } catch (err) {
    inviteError.value = err instanceof ApiError ? describeApiError(err) : String(err)
  }
}
const client = useQueryClient()
const form = useForm()
const text = ref('')
const list = useQuery({ queryKey: keys.myPetitions, queryFn: ({ signal }) => petitionsApi.mine({ signal }), staleTime: 0 })
const pending = computed(() => list.data.value?.find((p) => p.status === 'pending'))
const target = computed(() => (auth.user ? auth.user.level + 1 : 0))

async function submit() {
  form.clear()
  if (!text.value.trim()) {
    form.errors.text = t('petition.enterText')
    return
  }
  const ok = await form.submit(async () => {
    await petitionsApi.create(text.value)
  })
  if (ok) {
    text.value = ''
    await client.invalidateQueries({ queryKey: keys.myPetitions })
  }
}
</script>

<template>
  <div class="petition">
    <UiAlert v-if="pendingInvite" tone="info" data-testid="invitation">
      <p>{{ $t('petition.invited', { level: pendingInvite.target_level, name: levelName(pendingInvite.target_level) }) }}<span v-if="pendingInvite.message"> {{ pendingInvite.message }}</span></p>
      <div class="invite-actions">
        <UiButton variant="primary" data-testid="invitation-accept" @click="answer(pendingInvite.id, 'accept')">{{ $t('petition.accept') }}</UiButton>
        <UiButton @click="answer(pendingInvite.id, 'decline')">{{ $t('petition.decline') }}</UiButton>
      </div>
    </UiAlert>
    <UiAlert v-if="inviteError" tone="danger">{{ inviteError }}</UiAlert>
    <p v-if="pending" data-testid="petition-pending">{{ $t('petition.pending', { level: pending.target_level, name: levelName(pending.target_level) }) }}</p>
    <form v-else-if="(auth.user?.level ?? 0) >= 3" novalidate @submit.prevent="submit">
      <p>{{ $t('petition.intro', { level: target, name: levelName(target) }) }}</p>
      <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
      <UiField id="petition-text" :label="$t('petition.label')" :error="form.errors.text">
        <UiTextarea v-model="text" :rows="4" :maxlength="2000" />
      </UiField>
      <UiButton type="submit" variant="primary" :loading="form.submitting.value" data-testid="petition-submit">{{ $t('petition.submit') }}</UiButton>
    </form>
    <ul v-if="list.data.value?.some((p) => p.status !== 'pending')" class="history">
      <li v-for="p in list.data.value.filter((x) => x.status !== 'pending')" :key="p.id">
        {{ $t(`petition.status.${p.status}`, { level: p.target_level, when: formatDate(p.decided_at ?? p.created_at) }) }}
        <span v-if="p.comment" class="comment">— {{ p.comment }}</span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.invite-actions {
  display: flex;
  gap: var(--space-2);
  margin-top: var(--space-2);
}
.history {
  margin: var(--space-3) 0 0;
  padding-left: 1.2em;
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.comment {
  overflow-wrap: anywhere;
}
</style>
