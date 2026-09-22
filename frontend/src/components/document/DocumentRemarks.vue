<script setup lang="ts">
// «Пометки на полях» под документом (шаг 5.2): ветки с ответами, с уровня 1, публикуются сразу.
// Видимость треда следует за видимостью документа — сервер уже проверил допуск, здесь фильтровать нечего.
import { computed, ref } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import DocumentRemarkNode from './DocumentRemarkNode.vue'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiSkeleton from '@/ui/UiSkeleton.vue'
import UiTextarea from '@/ui/UiTextarea.vue'
import type { RemarkOut } from '@/api/generated/documents'
import { remarksApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { useForm } from '@/composables/useForm'
import { t } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'

const props = defineProps<{ code: string }>()
const auth = useAuthStore()
const ui = useUiStore()
const client = useQueryClient()

const query = useQuery({
  queryKey: computed(() => keys.remarks(props.code)),
  queryFn: ({ signal }) => remarksApi.list(props.code, { signal }),
  staleTime: 0,
})

const byParent = computed(() => {
  const m = new Map<number, RemarkOut[]>()
  for (const r of query.data.value ?? []) {
    if (r.parent_id == null) continue
    const list = m.get(r.parent_id)
    if (list) list.push(r)
    else m.set(r.parent_id, [r])
  }
  return m
})
const roots = computed(() => (query.data.value ?? []).filter((r) => r.parent_id == null))

const text = ref('')
const form = useForm()

async function submit() {
  form.clear()
  if (!text.value.trim()) {
    form.errors.text = t('doc.remarks.placeholder')
    return
  }
  const ok = await form.submit(async () => {
    await remarksApi.create(props.code, { text: text.value.trim() })
  })
  if (ok) {
    text.value = ''
    await client.invalidateQueries({ queryKey: keys.remarks(props.code) })
  }
}
</script>

<template>
  <UiSheet as="section" class="remarks" aria-labelledby="remarks-title">
    <h2 id="remarks-title">{{ $t('doc.remarks.title') }}</h2>

    <UiSkeleton v-if="query.isPending.value" :lines="3" :label="$t('doc.remarks.loading')" />
    <template v-else>
      <p v-if="!roots.length" class="remarks__empty">{{ $t('doc.remarks.empty') }}</p>
      <ul v-else class="remarks__list" data-testid="remarks-list">
        <DocumentRemarkNode v-for="r in roots" :key="r.id" :remark="r" :by-parent="byParent" :doc-ref="code" />
      </ul>
    </template>

    <form v-if="auth.isAuthenticated" novalidate class="remarks__form" @submit.prevent="submit">
      <UiAlert v-if="form.formError.value" tone="danger">{{ form.formError.value }}</UiAlert>
      <UiField id="remark-new" :label="$t('doc.remarks.placeholder')" hide-label :error="form.errors.text">
        <UiTextarea v-model="text" :rows="3" :maxlength="2000" :placeholder="$t('doc.remarks.placeholder')" />
      </UiField>
      <UiButton type="submit" variant="primary" :loading="form.submitting.value" data-testid="remark-submit">{{ $t('doc.remarks.submit') }}</UiButton>
    </form>
    <p v-else class="remarks__login">
      {{ $t('doc.remarks.needLogin') }}
      <UiButton variant="link" @click="ui.openAuth()">{{ $t('doc.remarks.signIn') }}</UiButton>
    </p>
  </UiSheet>
</template>

<style scoped>
.remarks {
  margin-top: var(--space-5);
}
.remarks__list {
  margin: 0;
  padding: 0;
  list-style: none;
}
.remarks__empty {
  color: var(--text-muted);
}
.remarks__form {
  margin-top: var(--space-4);
}
.remarks__form .ui-button {
  margin-top: var(--space-2);
}
.remarks__login {
  margin-top: var(--space-4);
  color: var(--text-muted);
}
</style>
