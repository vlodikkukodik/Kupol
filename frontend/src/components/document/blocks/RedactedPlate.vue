<script setup lang="ts">
// Закрытый блок: сервер прислал только нужный уровень допуска — ни текста, ни длины, ни вида блока.
// «Поле на плашках» (шаг 5.5): у каждой плашки можно попробовать код доступа — не обязательно тот,
// что открывает именно этот блок (сервер сам решает, что откроется), это просто удобное место попробовать.
import { ref } from 'vue'
import type { RedactedData } from '@/api/blocks'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiInput from '@/ui/UiInput.vue'
import { secretCodesApi } from '@/api/endpoints'
import type { RedeemResult } from '@/api/generated/documents'
import { useForm } from '@/composables/useForm'
import { achievementName } from '@/lib/achievements'
import { requiredAccess } from '@/lib/levels'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'

defineProps<{ data: RedactedData }>()

const auth = useAuthStore()
const ui = useUiStore()
const form = useForm()
const showForm = ref(false)
const code = ref('')
const result = ref<RedeemResult | null>(null)

async function submit() {
  form.clear()
  result.value = null
  const ok = await form.submit(async () => {
    result.value = await secretCodesApi.redeem(code.value.trim())
  })
  if (ok) {
    code.value = ''
    await auth.load(true)
  }
}
</script>

<template>
  <div class="plate" role="img" :aria-label="$t('doc.redactedRun', { access: requiredAccess(data.level) })" :data-level="data.level">
    <span class="plate__title">{{ $t('doc.redactedBlock') }}</span>
    <span class="plate__need">{{ requiredAccess(data.level) }}</span>
  </div>

  <p v-if="!auth.isAuthenticated" class="code-hint">
    {{ $t('doc.secretCode.needLogin') }}
    <UiButton variant="link" @click="ui.openAuth()">{{ $t('doc.secretCode.signIn') }}</UiButton>
  </p>
  <div v-else class="code-toggle">
    <UiButton v-if="!showForm" variant="link" @click="showForm = true">{{ $t('doc.secretCode.tryCode') }}</UiButton>
    <form v-else novalidate class="code-form" @submit.prevent="submit">
      <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
      <UiInput v-model="code" :aria-label="$t('doc.secretCode.label')" :placeholder="$t('doc.secretCode.label')" autocomplete="off" />
      <UiButton type="submit" variant="primary" :loading="form.submitting.value">{{ $t('doc.secretCode.submitShort') }}</UiButton>
    </form>
    <p class="visually-hidden" role="status">{{ result ? $t('doc.secretCode.successShort', { xp: result.awarded_xp }) : '' }}</p>
    <UiAlert v-if="result" tone="success" :live="false">
      {{ $t('doc.secretCode.successShort', { xp: result.awarded_xp }) }}
      <RouterLink :to="{ name: 'document', params: { ref: result.slug } }">{{ $t('doc.secretCode.openShort', { ref: result.document_ref }) }}</RouterLink>
      <span v-for="kind in result.new_achievements" :key="kind" class="new-achievement">{{ $t('secretCode.newAchievement', { name: achievementName(kind) }) }}</span>
    </UiAlert>
  </div>
</template>

<style scoped>
.plate {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--space-2) var(--space-4);
  margin: var(--space-4) 0 0;
  padding: var(--space-3) var(--space-4);
  background: var(--redaction);
  color: var(--paper-100);
  font-family: var(--font-head);
  letter-spacing: 0.1em;
  text-transform: uppercase;
  user-select: none;
  /* сплошная чёрная полоса: надпись на ней должна читаться, а рваный край даёт тень */
  box-shadow: 0 0 0 1px var(--redaction), 1px 1px 0 1px rgb(0 0 0 / 0.35);
}
.plate__title {
  font-size: var(--text-lg);
  font-weight: 700;
}
.plate__need {
  font-size: var(--text-sm);
  opacity: 0.9;
}
.code-hint,
.code-toggle {
  margin: var(--space-1) 0 var(--space-4);
  font-size: var(--text-sm);
}
.new-achievement {
  display: block;
  font-weight: 700;
}
.code-form {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-1);
}
</style>
