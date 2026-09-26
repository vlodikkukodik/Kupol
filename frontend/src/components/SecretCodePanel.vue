<script setup lang="ts">
// Код доступа в личном деле (пасхалки, шаг 5.5): верный код открывает тайное дело аккаунту и даёт XP
// (и, возможно, грамоту «Нашёл скрытый код» — шаг 5.6).
import { ref } from 'vue'
import { useQueryClient } from '@tanstack/vue-query'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import { secretCodesApi } from '@/api/endpoints'
import type { RedeemResult } from '@/api/generated/documents'
import { keys } from '@/api/query'
import { useForm } from '@/composables/useForm'
import { achievementName } from '@/lib/achievements'
import { t } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const client = useQueryClient()
const form = useForm()
const code = ref('')
const result = ref<RedeemResult | null>(null)

async function submit() {
  form.clear()
  result.value = null
  if (!code.value.trim()) {
    form.errors.code = t('secretCode.enterCode')
    return
  }
  const ok = await form.submit(async () => {
    result.value = await secretCodesApi.redeem(code.value.trim())
  })
  if (ok) {
    code.value = ''
    await auth.load(true) // подтянуть новый XP/уровень в «пропуск»
    if ((result.value as RedeemResult | null)?.new_achievements?.length) await client.invalidateQueries({ queryKey: keys.myAchievements })
  }
}
</script>

<template>
  <form class="secret-code" novalidate @submit.prevent="submit">
    <p>{{ $t('secretCode.intro') }}</p>
    <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
    <UiField id="secret-code-input" :label="$t('secretCode.label')" :error="form.errors.code">
      <UiInput v-model="code" autocomplete="off" data-testid="secret-code-input" />
    </UiField>
    <UiButton type="submit" variant="primary" :loading="form.submitting.value" data-testid="secret-code-submit">{{ $t('secretCode.submit') }}</UiButton>

    <p class="visually-hidden" role="status">{{ result ? $t('secretCode.success', { xp: result.awarded_xp }) : '' }}</p>
    <UiAlert v-if="result" tone="success" :live="false" data-testid="secret-code-result">
      {{ $t('secretCode.success', { xp: result.awarded_xp }) }}
      <RouterLink :to="{ name: 'document', params: { ref: result.slug } }">{{ $t('secretCode.openDocument', { ref: result.document_ref }) }}</RouterLink>
      <span v-for="kind in result.new_achievements" :key="kind" class="new-achievement">{{ $t('secretCode.newAchievement', { name: achievementName(kind) }) }}</span>
    </UiAlert>
  </form>
</template>

<style scoped>
.secret-code {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}
.new-achievement {
  display: block;
  margin-top: var(--space-1);
  font-weight: 700;
}
</style>
