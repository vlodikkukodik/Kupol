<script setup lang="ts">
// Жалобы на «пометки на полях» (шаг 5.2): очередь для модератора (право moderate_comments), по числу жалоб.
import { computed, ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { ApiError, isApiError } from '@/api/client'
import { remarksApi } from '@/api/endpoints'
import type { RemarkOut } from '@/api/generated/documents'
import { keys } from '@/api/query'
import { describeApiError } from '@/composables/useForm'
import { formatDateTime } from '@/lib/format'
import { tc } from '@/i18n'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiEmpty from '@/ui/UiEmpty.vue'
import UiModal from '@/ui/UiModal.vue'
import UiPageHeader from '@/ui/UiPageHeader.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import ErrorView from '../ErrorView.vue'

const client = useQueryClient()
const list = useQuery({
  queryKey: keys.reportedRemarks,
  queryFn: ({ signal }) => remarksApi.reported({ signal }),
  staleTime: 0,
})
const requestId = computed(() => (isApiError(list.error.value) ? list.error.value.requestId : ''))

const removing = ref<RemarkOut | null>(null)
const removeOpen = computed({ get: () => removing.value !== null, set: (v) => { if (!v) removing.value = null } })
const busy = ref(false)
const failure = ref('')

async function confirmRemove() {
  const item = removing.value
  if (!item) return
  busy.value = true
  failure.value = ''
  try {
    await remarksApi.remove(item.id)
    removing.value = null
    await client.invalidateQueries({ queryKey: keys.reportedRemarks })
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    failure.value = describeApiError(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="reports">
    <UiPageHeader :title="$t('reports.title')" :kicker="$t('reports.kicker')" />

    <UiSkeleton v-if="list.isPending.value" :lines="4" :label="$t('reports.loading')" />
    <ErrorView v-else-if="list.isError.value" :request-id="requestId" :retrying="list.isFetching.value" @retry="list.refetch()" />
    <UiEmpty v-else-if="!list.data.value?.length" :title="$t('reports.empty')" />
    <ul v-else class="reports__list" data-testid="reports-list">
      <li v-for="item in list.data.value" :key="item.id">
        <UiSheet as="article" class="reports__item">
          <p class="reports__meta">
            <RouterLink :to="`/doc/${item.document_slug}`">{{ item.document_code }}</RouterLink>
            · {{ $t('reports.by', { author: item.author }) }} · <time :datetime="item.created_at">{{ formatDateTime(item.created_at) }}</time>
            · {{ tc('reports.count', item.report_count ?? 0) }}
          </p>
          <p class="reports__text">{{ item.text }}</p>
          <UiButton size="sm" variant="danger" icon="trash" data-testid="report-delete" @click="removing = item">{{ $t('reports.remove') }}</UiButton>
        </UiSheet>
      </li>
    </ul>

    <UiModal v-model:open="removeOpen" :title="$t('reports.removeTitle')" testid="report-delete-dialog">
      <p>{{ $t('reports.removeText') }}</p>
      <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>
      <div class="reports__actions">
        <UiButton variant="danger" icon="trash" :loading="busy" data-testid="report-confirm-delete" @click="confirmRemove">{{ $t('reports.remove') }}</UiButton>
        <UiButton variant="link" @click="removing = null">{{ $t('reports.cancel') }}</UiButton>
      </div>
    </UiModal>
  </div>
</template>

<style scoped>
.reports__list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: var(--space-3);
}
.reports__item {
  margin: 0;
  max-width: none;
}
.reports__meta {
  margin: 0 0 var(--space-2);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.reports__text {
  margin: 0 0 var(--space-3);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.reports__actions {
  display: flex;
  gap: var(--space-2);
  margin-top: var(--space-3);
}
</style>
