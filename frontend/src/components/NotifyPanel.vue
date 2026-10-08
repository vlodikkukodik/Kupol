<script setup lang="ts">
// Письма-уведомления в личном деле: копии записок внутренней почты, которые читатель хочет получать
// на указанный адрес. Письмо — это та же записка, поэтому виды настроек совпадают с видами записок.
// По умолчанию всё включено (opt-out): когда Директорат включает почту, известные события сразу доходят.
import { onMounted, ref } from 'vue'
import { ApiError } from '@/api/client'
import { authApi } from '@/api/endpoints'
import type { EmailPrefs } from '@/api/generated/inbox'
import { describeApiError } from '@/composables/useForm'
import { t } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiCheckbox from '@/ui/UiCheckbox.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'

type Kind = Exclude<keyof EmailPrefs, 'enabled'>

const KINDS: Kind[] = [
  'note',
  'level_up',
  'achievement',
  'suggestion',
  'remark_reply',
  'petition',
  'invitation',
  'invitation_answer',
  'sanction',
]

const auth = useAuthStore()
const prefs = ref<EmailPrefs | null>(null)
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const notice = ref('')

onMounted(async () => {
  try {
    prefs.value = await authApi.emailPrefs()
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    error.value = describeApiError(err)
  } finally {
    loading.value = false
  }
})

/** Переключить один вид (или общий тумблер) и сохранить сразу; при сбое прежнее значение возвращается. */
async function toggle(patch: Partial<EmailPrefs>) {
  const before = prefs.value
  if (!before) return
  const next = { ...before, ...patch }
  prefs.value = next
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    await authApi.setEmailPrefs(next)
    notice.value = t('notify.saved')
  } catch (err) {
    prefs.value = before
    if (!(err instanceof ApiError)) throw err
    error.value = describeApiError(err)
  } finally {
    saving.value = false
  }
}

/** Общий тумблер: выключает все письма разом, включая новые виды записок, появившиеся потом. */
function toggleEnabled(next: boolean) {
  void toggle({ enabled: next })
}
</script>

<template>
  <div class="notify" data-testid="notify">
    <p class="lead">{{ $t('notify.lead') }}</p>
    <UiSkeleton v-if="loading" :lines="4" :label="$t('notify.loading')" />
    <template v-else-if="prefs">
      <UiAlert v-if="error" tone="danger" data-testid="notify-error">{{ error }}</UiAlert>
      <p class="visually-hidden" role="status">{{ notice }}</p>

      <UiCheckbox
        :model-value="prefs.enabled"
        :label="$t('notify.master')"
        data-testid="notify-enabled"
        @update:model-value="toggleEnabled"
      />
      <p v-if="!prefs.enabled" class="state" data-testid="notify-off">{{ $t('notify.off') }}</p>

      <ul class="kinds" data-testid="notify-kinds">
        <li v-for="kind in KINDS" :key="kind">
          <UiCheckbox
            :model-value="prefs[kind]"
            :label="$t(`notify.kinds.${kind}`)"
            :disabled="!prefs.enabled || saving"
            :data-testid="`notify-kind-${kind}`"
            @update:model-value="(v) => toggle({ [kind]: v } as Partial<EmailPrefs>)"
          />
        </li>
      </ul>

      <p v-if="!auth.user?.email" class="state state--muted" data-testid="notify-noemail">{{ $t('notify.noEmail') }}</p>
      <UiButton v-if="saving" variant="link" disabled data-testid="notify-saving">{{ $t('notify.saving') }}</UiButton>
    </template>
  </div>
</template>

<style scoped>
.lead {
  max-width: 44rem;
  color: var(--text-muted);
}
.state {
  margin: var(--space-3) 0 0;
}
.state--muted {
  color: var(--text-muted);
}
.kinds {
  display: grid;
  gap: var(--space-2);
  margin: var(--space-3) 0 0;
  padding: var(--space-3) 0 0;
  list-style: none;
  border-top: 1px dashed var(--border);
}
</style>
