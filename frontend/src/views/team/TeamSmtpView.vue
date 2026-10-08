<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { ApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import type { Input as SmtpInput } from '@/api/generated/mailsettings'
import { keys } from '@/api/query'
import { describeApiError } from '@/composables/useForm'
import { formatDateTime } from '@/lib/format'
import { t } from '@/i18n'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiCheckbox from '@/ui/UiCheckbox.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import ErrorView from '../ErrorView.vue'

// Почтовый сервер, с которого уходят письма-уведомления (шаг 5.1.1). Читают настройки члены команды,
// правит и проверяет письмом только Директорат. Пока настройки не сохраняли, действует окружение (source = env).
const client = useQueryClient()
const smtp = useQuery({ queryKey: keys.teamSmtp, queryFn: ({ signal }) => teamApi.smtp({ signal }), staleTime: 0 })
const requestId = computed(() => (smtp.error.value instanceof ApiError ? smtp.error.value.requestId : ''))

const form = ref<SmtpInput>({ enabled: false, host: '', port: '587', username: '', password: '', from: '', from_name: '' })
const error = ref('')
const notice = ref('')
const busy = ref(false)

watch(
  () => smtp.data.value,
  (s) => {
    if (s && !busy.value) {
      form.value = { enabled: s.enabled, host: s.host, port: s.port, username: s.username, password: '', from: s.from, from_name: s.from_name }
    }
  },
  { immediate: true },
)

const canEdit = computed(() => Boolean(smtp.data.value?.can_edit))
const dirty = computed(() => {
  const s = smtp.data.value
  if (!s) return false
  const f = form.value
  if (f.enabled !== s.enabled || f.host !== s.host || f.port !== s.port || f.username !== s.username || f.from !== s.from || f.from_name !== s.from_name) return true
  return f.password !== '' // пустое поле не затирает сохранённый пароль
})

/** Первая ошибка полей формы сервера (сервер уже перевёл её на язык запроса). */
function fieldError(err: ApiError): string {
  const first = Object.values(err.fields)[0]
  return first ?? describeApiError(err)
}

async function save() {
  error.value = ''
  notice.value = ''
  busy.value = true
  try {
    // пустой пароль не шлём вовсе: сервер по отсутствию ключа оставляет сохранённый (mailsettings.Input)
    const { password, ...withoutPassword } = form.value
    await teamApi.updateSmtp(password === '' ? withoutPassword : form.value)
    form.value = { ...form.value, password: '' }
    notice.value = t('smtp.saved')
    await client.invalidateQueries({ queryKey: keys.teamSmtp })
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    error.value = fieldError(err)
  } finally {
    busy.value = false
  }
}

const testTo = ref('')
const testError = ref('')
const testNotice = ref('')
const testBusy = ref(false)

async function sendTest() {
  testError.value = ''
  testNotice.value = ''
  if (!testTo.value.trim()) {
    testError.value = t('smtp.testEnter')
    return
  }
  testBusy.value = true
  try {
    await teamApi.testSmtp(testTo.value.trim())
    testNotice.value = t('smtp.testSent', { to: testTo.value.trim() })
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    testError.value = fieldError(err)
  } finally {
    testBusy.value = false
  }
}
</script>

<template>
  <ErrorView v-if="smtp.isError.value" :request-id="requestId" :retrying="smtp.isFetching.value" @retry="smtp.refetch()" />
  <UiSheet v-else as="section" aria-labelledby="smtp-title" data-testid="team-smtp">
    <h2 id="smtp-title">{{ $t('smtp.title') }}</h2>
    <p class="lead">{{ $t('smtp.lead') }}</p>
    <UiSkeleton v-if="smtp.isPending.value" :lines="4" :label="$t('smtp.loading')" />
    <template v-else-if="smtp.data.value">
      <p v-if="!canEdit" class="readonly" data-testid="smtp-readonly">{{ $t('smtp.readonly') }}</p>
      <p class="source" data-testid="smtp-source">
        {{ smtp.data.value.source === 'env' ? $t('smtp.sourceEnv') : $t('smtp.sourceDb') }}
      </p>

      <form novalidate @submit.prevent="save">
        <UiAlert v-if="error" tone="danger" data-testid="smtp-error">{{ error }}</UiAlert>
        <UiAlert v-if="notice" tone="success" data-testid="smtp-notice">{{ notice }}</UiAlert>

        <UiCheckbox
          :model-value="form.enabled"
          :label="$t('smtp.enabled')"
          :disabled="!canEdit"
          data-testid="smtp-enabled"
          @update:model-value="(v) => (form.enabled = v)"
        />

        <div class="grid">
          <UiField :label="$t('smtp.host')" :hint="$t('smtp.hostHint')">
            <UiInput v-model="form.host" name="host" autocomplete="off" :disabled="!canEdit" data-testid="smtp-host" />
          </UiField>
          <UiField :label="$t('smtp.port')">
            <UiInput v-model="form.port" name="port" inputmode="numeric" :disabled="!canEdit" data-testid="smtp-port" />
          </UiField>
        </div>

        <UiField :label="$t('smtp.username')" :hint="$t('smtp.usernameHint')">
          <UiInput v-model="form.username" name="username" autocomplete="off" :disabled="!canEdit" data-testid="smtp-username" />
        </UiField>
        <UiField :label="$t('smtp.password')" :hint="smtp.data.value.password_set ? $t('smtp.passwordSet') : $t('smtp.passwordHint')">
          <UiInput
            v-model="form.password"
            name="password"
            type="password"
            autocomplete="new-password"
            reveal
            :disabled="!canEdit"
            data-testid="smtp-password"
          />
        </UiField>

        <div class="grid">
          <UiField :label="$t('smtp.from')" :hint="$t('smtp.fromHint')">
            <UiInput v-model="form.from" name="from" type="email" autocomplete="off" :disabled="!canEdit" data-testid="smtp-from" />
          </UiField>
          <UiField :label="$t('smtp.fromName')">
            <UiInput v-model="form.from_name" name="from_name" autocomplete="off" :disabled="!canEdit" data-testid="smtp-from-name" />
          </UiField>
        </div>

        <div class="actions">
          <UiButton v-if="canEdit" type="submit" variant="primary" :loading="busy" :disabled="!dirty" data-testid="smtp-save">
            {{ $t('smtp.save') }}
          </UiButton>
          <span v-if="smtp.data.value.updated_at" class="meta">
            {{
              smtp.data.value.updated_by
                ? $t('smtp.changedBy', { when: formatDateTime(smtp.data.value.updated_at), who: smtp.data.value.updated_by })
                : $t('smtp.changed', { when: formatDateTime(smtp.data.value.updated_at) })
            }}
          </span>
        </div>
      </form>

      <section class="test" aria-labelledby="smtp-test-title" data-testid="smtp-test">
        <h3 id="smtp-test-title">{{ $t('smtp.testTitle') }}</h3>
        <p class="lead">{{ $t('smtp.testHint') }}</p>
        <UiAlert v-if="testError" tone="danger" data-testid="smtp-test-error">{{ testError }}</UiAlert>
        <UiAlert v-if="testNotice" tone="success" data-testid="smtp-test-notice">{{ testNotice }}</UiAlert>
        <div class="test-row">
          <UiField :label="$t('smtp.testTo')">
            <UiInput v-model="testTo" type="email" autocomplete="off" name="to" data-testid="smtp-test-to" />
          </UiField>
          <UiButton
            variant="primary"
            :loading="testBusy"
            :disabled="!canEdit || !smtp.data.value.enabled"
            data-testid="smtp-test-send"
            @click="sendTest"
          >
            {{ $t('smtp.testSend') }}
          </UiButton>
        </div>
        <p v-if="!smtp.data.value.enabled" class="state">{{ $t('smtp.testDisabled') }}</p>
      </section>
    </template>
  </UiSheet>
</template>

<style scoped>
.lead {
  max-width: 44rem;
  color: var(--text-muted);
}
.readonly,
.source {
  padding: var(--space-2) var(--space-3);
  border-left: 4px solid var(--border-strong);
  background: var(--surface-sunken);
}
.source {
  border-left-color: var(--ink-900);
}
.grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(12rem, 1fr));
  gap: var(--space-3);
}
.actions,
.test-row {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: var(--space-3);
}
.test-row > :first-child {
  flex: 1 1 18rem;
}
.meta,
.state {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.test {
  margin-top: var(--space-4);
  padding-top: var(--space-3);
  border-top: 1px dashed var(--border);
}
.test h3 {
  font-size: var(--text-md);
}
</style>
