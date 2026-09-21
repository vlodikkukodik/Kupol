<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { ApiError, isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { describeApiError } from '@/composables/useForm'
import { formatDateTime } from '@/lib/format'
import { t } from '@/i18n'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import UiTextarea from '@/ui/UiTextarea.vue'
import ErrorView from '../ErrorView.vue'

// Настройки сайта: контакты автора, которые видны на странице «О КУПОЛЕ» (раздел «Об авторе и контакты»). Читают члены команды,
// правит только Директорат: публиковать ли свою почту, решает автор архива. Пустое поле убирает раздел контактов со страницы.
const client = useQueryClient()
const site = useQuery({ queryKey: keys.teamSite, queryFn: ({ signal }) => teamApi.site({ signal }), staleTime: 0 })
const requestId = computed(() => (isApiError(site.error.value) ? site.error.value.requestId : ''))

const contact = ref('')
const error = ref('')
const notice = ref('')
const busy = ref(false)
watch(
  () => site.data.value,
  (s) => {
    if (s && !busy.value) contact.value = s.contact
  },
  { immediate: true },
)
const dirty = computed(() => site.data.value !== undefined && contact.value !== site.data.value.contact)

async function save() {
  error.value = ''
  notice.value = ''
  busy.value = true
  try {
    const saved = await teamApi.updateSite(contact.value)
    contact.value = saved.contact
    notice.value = saved.contact ? t('site.savedWith') : t('site.savedEmpty')
    await Promise.all([client.invalidateQueries({ queryKey: keys.teamSite }), client.invalidateQueries({ queryKey: keys.site })])
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    error.value = err.problems[0]?.message ?? err.fields.contact ?? describeApiError(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ErrorView v-if="site.isError.value" :request-id="requestId" :retrying="site.isFetching.value" @retry="site.refetch()" />
  <UiSheet v-else as="section" aria-labelledby="site-title" data-testid="team-site">
    <h2 id="site-title">{{ $t('site.title') }}</h2>
    <p class="lead">
      <i18n-t keypath="site.lead" scope="global">
        <template #about><RouterLink to="/about#author">{{ $t('site.aboutLink') }}</RouterLink></template>
      </i18n-t>
    </p>
    <UiSkeleton v-if="site.isPending.value" :lines="3" :label="$t('site.loading')" />
    <template v-else-if="site.data.value">
      <p v-if="!site.data.value.can_edit" class="readonly" data-testid="site-readonly">{{ $t('site.readonly') }}</p>
      <form novalidate @submit.prevent="save">
        <UiAlert v-if="error" tone="danger" data-testid="site-error">{{ error }}</UiAlert>
        <UiAlert v-if="notice" tone="success" data-testid="site-notice">{{ notice }}</UiAlert>
        <UiField :label="$t('site.contacts')" :hint="$t('site.hint')">
          <UiTextarea v-model="contact" :rows="5" :maxlength="1000" name="contact" :disabled="!site.data.value.can_edit" />
        </UiField>
        <div class="actions">
          <UiButton v-if="site.data.value.can_edit" type="submit" variant="primary" :loading="busy" :disabled="!dirty" data-testid="site-save">{{ $t('site.save') }}</UiButton>
          <span v-if="site.data.value.updated_at" class="meta">
            {{ site.data.value.updated_by ? $t('site.changedBy', { when: formatDateTime(site.data.value.updated_at), who: site.data.value.updated_by }) : $t('site.changed', { when: formatDateTime(site.data.value.updated_at) }) }}
          </span>
        </div>
      </form>
    </template>
  </UiSheet>
</template>

<style scoped>
.lead {
  max-width: 44rem;
  color: var(--text-muted);
}
.readonly {
  padding: var(--space-2) var(--space-3);
  border-left: 4px solid var(--border-strong);
  background: var(--surface-sunken);
}
.actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3);
}
.meta {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
</style>
