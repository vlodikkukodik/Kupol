<script setup lang="ts">
import { computed, ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { ApiError, isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { SecretCodeOut } from '@/api/generated/documents'
import { keys } from '@/api/query'
import { describeApiError } from '@/composables/useForm'
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

// Скрытые коды (пасхалки, шаг 5.5): заводит и удаляет только Директорат — остальным членам команды
// список не показывается вовсе (сервер отвечает 403), чтобы разгадку никто из команды не мог подсмотреть.
const client = useQueryClient()
const list = useQuery({ queryKey: keys.teamSecretCodes, queryFn: ({ signal }) => teamApi.secretCodes({ signal }), staleTime: 0 })
const requestId = computed(() => (isApiError(list.error.value) ? list.error.value.requestId : ''))
const forbidden = computed(() => isApiError(list.error.value) && list.error.value.code === 'forbidden')

const creating = ref(false)
const code = ref('')
const documentRef = ref('')
const blockId = ref('')
const rewardXp = ref('50')
const note = ref('')
const errors = ref<Record<string, string>>({})
const failure = ref('')
const busy = ref(false)
const notice = ref('')

function openCreate() {
  creating.value = true
  code.value = ''
  documentRef.value = ''
  blockId.value = ''
  rewardXp.value = '50'
  note.value = ''
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
  await client.invalidateQueries({ queryKey: keys.teamSecretCodes })
}

async function save() {
  errors.value = {}
  failure.value = ''
  const xp = Number(rewardXp.value)
  if (!code.value.trim()) errors.value.code = t('teamSecrets.enterCode')
  if (!documentRef.value.trim()) errors.value.document_ref = t('teamSecrets.enterRef')
  if (!Number.isInteger(xp) || xp < 0) errors.value.reward_xp = t('teamSecrets.badXp')
  if (Object.keys(errors.value).length) return
  busy.value = true
  try {
    const created = await teamApi.createSecretCode({
      code: code.value.trim(),
      document_ref: documentRef.value.trim(),
      block_id: blockId.value.trim() || undefined,
      reward_xp: xp,
      note: note.value.trim() || undefined,
    })
    creating.value = false
    await refresh(t('teamSecrets.created', { code: created.code }))
  } catch (err) {
    fail(err)
  } finally {
    busy.value = false
  }
}

const removing = ref<SecretCodeOut | null>(null)
const removeOpen = computed({ get: () => removing.value !== null, set: (v) => { if (!v) removing.value = null } })
async function confirmRemove() {
  const item = removing.value
  if (!item) return
  failure.value = ''
  busy.value = true
  try {
    await teamApi.deleteSecretCode(item.id)
    removing.value = null
    await refresh(t('teamSecrets.removed', { code: item.code }))
  } catch (err) {
    fail(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <UiSheet v-if="forbidden" as="section" aria-labelledby="secrets-title">
    <h2 id="secrets-title">{{ $t('teamSecrets.title') }}</h2>
    <p>{{ $t('teamSecrets.directorateOnly') }}</p>
  </UiSheet>
  <ErrorView v-else-if="list.isError.value" :request-id="requestId" :retrying="list.isFetching.value" @retry="list.refetch()" />
  <UiSheet v-else as="section" aria-labelledby="secrets-title" data-testid="team-secrets">
    <div class="head">
      <h2 id="secrets-title">{{ $t('teamSecrets.title') }}</h2>
      <UiButton variant="primary" icon="plus" data-testid="secret-new" @click="openCreate">{{ $t('teamSecrets.add') }}</UiButton>
    </div>
    <p class="lead">{{ $t('teamSecrets.lead') }}</p>

    <p class="visually-hidden" role="status">{{ notice }}</p>
    <p v-if="notice" class="notice">{{ notice }}</p>

    <UiSkeleton v-if="list.isPending.value" :lines="4" :label="$t('teamSecrets.loading')" />
    <UiEmpty v-else-if="!list.data.value?.length" icon="lock" :title="$t('teamSecrets.emptyTitle')">{{ $t('teamSecrets.emptyText') }}</UiEmpty>
    <table v-else class="codes" data-testid="secrets-list">
      <thead>
        <tr>
          <th>{{ $t('teamSecrets.code') }}</th>
          <th>{{ $t('teamSecrets.document') }}</th>
          <th>{{ $t('teamSecrets.reward') }}</th>
          <th>{{ $t('teamSecrets.note') }}</th>
          <th />
        </tr>
      </thead>
      <tbody>
        <tr v-for="item in list.data.value" :key="item.id">
          <td class="codes__code">{{ item.code }}</td>
          <td>{{ item.document_ref }} — {{ item.document_title }}<span v-if="item.block_id"> (#{{ item.block_id }})</span></td>
          <td>{{ item.reward_xp }}</td>
          <td class="codes__note">{{ item.note }}</td>
          <td><UiButton size="sm" variant="ghost" icon="trash" @click="removing = item">{{ $t('teamSecrets.remove') }}</UiButton></td>
        </tr>
      </tbody>
    </table>

    <UiModal v-model:open="creating" :title="$t('teamSecrets.newTitle')" testid="secret-dialog">
      <form novalidate @submit.prevent="save">
        <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>
        <UiField :label="$t('teamSecrets.code')" required :error="errors.code">
          <UiInput v-model="code" :maxlength="64" />
        </UiField>
        <UiField :label="$t('teamSecrets.document')" :hint="$t('teamSecrets.documentHint')" required :error="errors.document_ref">
          <UiInput v-model="documentRef" />
        </UiField>
        <UiField :label="$t('teamSecrets.blockId')" :hint="$t('teamSecrets.blockIdHint')" :error="errors.block_id">
          <UiInput v-model="blockId" />
        </UiField>
        <UiField :label="$t('teamSecrets.reward')" :error="errors.reward_xp">
          <UiInput v-model="rewardXp" type="number" min="0" max="500" />
        </UiField>
        <UiField :label="$t('teamSecrets.note')" :hint="$t('teamSecrets.noteHint')" :error="errors.note">
          <UiTextarea v-model="note" :rows="2" :maxlength="500" />
        </UiField>
        <div class="dlg-actions">
          <UiButton type="submit" variant="primary" :loading="busy" data-testid="secret-save">{{ $t('teamSecrets.save') }}</UiButton>
          <UiButton variant="link" @click="creating = false">{{ $t('teamSecrets.cancel') }}</UiButton>
        </div>
      </form>
    </UiModal>

    <UiModal v-model:open="removeOpen" :title="$t('teamSecrets.removeTitle')" testid="secret-delete-dialog">
      <p>{{ $t('teamSecrets.removeText', { code: removing?.code ?? '' }) }}</p>
      <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>
      <div class="dlg-actions">
        <UiButton variant="danger" icon="trash" :loading="busy" data-testid="secret-confirm-delete" @click="confirmRemove">{{ $t('teamSecrets.remove') }}</UiButton>
        <UiButton variant="link" @click="removing = null">{{ $t('teamSecrets.cancel') }}</UiButton>
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
.codes {
  width: 100%;
  border-collapse: collapse;
}
.codes th {
  padding: var(--space-2) var(--space-3);
  border-bottom: 2px solid var(--ink-900);
  text-align: left;
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.codes td {
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px dashed var(--border-strong);
  vertical-align: top;
}
.codes__code {
  font-weight: 700;
  white-space: nowrap;
}
.codes__note {
  color: var(--text-muted);
  max-width: 20rem;
  overflow-wrap: anywhere;
}
.dlg-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-2);
}
</style>
