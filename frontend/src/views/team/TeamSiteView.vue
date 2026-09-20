<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { ApiError, isApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { describeApiError } from '@/composables/useForm'
import { formatDateTime } from '@/lib/format'
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
    notice.value = saved.contact ? 'Контакты сохранены: они видны на странице «О КУПОЛЕ».' : 'Контакты убраны: раздел на странице «О КУПОЛЕ» не показывается.'
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
    <h2 id="site-title">Сайт: контакты автора</h2>
    <p class="lead">
      Текст из этого поля показывается на странице <RouterLink to="/about#author">«О КУПОЛЕ»</RouterLink> в разделе «Об авторе и контакты». Адреса вида
      https://… и электронная почта становятся ссылками. Пустое поле — раздела контактов на странице нет.
    </p>
    <UiSkeleton v-if="site.isPending.value" :lines="3" label="Загружаем настройки…" />
    <template v-else-if="site.data.value">
      <p v-if="!site.data.value.can_edit" class="readonly" data-testid="site-readonly">Менять контакты может только Директорат. Сейчас на странице показано то, что ниже.</p>
      <form novalidate @submit.prevent="save">
        <UiAlert v-if="error" tone="danger" data-testid="site-error">{{ error }}</UiAlert>
        <UiAlert v-if="notice" tone="success" data-testid="site-notice">{{ notice }}</UiAlert>
        <UiField label="Контакты" hint="До 1000 знаков; можно в несколько строк.">
          <UiTextarea v-model="contact" :rows="5" :maxlength="1000" name="contact" :disabled="!site.data.value.can_edit" />
        </UiField>
        <div class="actions">
          <UiButton v-if="site.data.value.can_edit" type="submit" variant="primary" :loading="busy" :disabled="!dirty" data-testid="site-save">Сохранить</UiButton>
          <span v-if="site.data.value.updated_at" class="meta">
            Изменено {{ formatDateTime(site.data.value.updated_at) }}<template v-if="site.data.value.updated_by"> · {{ site.data.value.updated_by }}</template>
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
