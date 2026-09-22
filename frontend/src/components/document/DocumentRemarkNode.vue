<script setup lang="ts">
// Одна пометка на полях и её ответы (рекурсивно — ответ может быть на ответ). auth.isAuthenticated решает,
// показывать ли «Ответить»/«Жалоба»: писать и жаловаться может только вошедший (с уровня 1).
import { computed, ref } from 'vue'
import { useQueryClient } from '@tanstack/vue-query'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiModal from '@/ui/UiModal.vue'
import UiTextarea from '@/ui/UiTextarea.vue'
import type { RemarkOut } from '@/api/generated/documents'
import { ApiError } from '@/api/client'
import { remarksApi } from '@/api/endpoints'
import { keys } from '@/api/query'
import { describeApiError, useForm } from '@/composables/useForm'
import { formatDateTime } from '@/lib/format'
import { t } from '@/i18n'
import { useAuthStore } from '@/stores/auth'

defineOptions({ name: 'DocumentRemarkNode' })

const props = defineProps<{
  remark: RemarkOut
  byParent: Map<number, RemarkOut[]>
  docRef: string
}>()

const auth = useAuthStore()
const client = useQueryClient()
const kids = computed(() => props.byParent.get(props.remark.id) ?? [])

const replying = ref(false)
const replyText = ref('')
const replyForm = useForm()

const reported = ref(false)
const reportError = ref('')

const removing = ref(false)
const removeBusy = ref(false)
const removeError = ref('')

async function refresh() {
  await client.invalidateQueries({ queryKey: keys.remarks(props.docRef) })
}

async function submitReply() {
  replyForm.clear()
  if (!replyText.value.trim()) {
    replyForm.errors.text = t('doc.remarks.replyPlaceholder')
    return
  }
  const ok = await replyForm.submit(async () => {
    await remarksApi.create(props.docRef, { parent_id: props.remark.id, text: replyText.value.trim() })
  })
  if (ok) {
    replyText.value = ''
    replying.value = false
    await refresh()
  }
}

async function report() {
  reportError.value = ''
  try {
    await remarksApi.report(props.remark.id)
    reported.value = true
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    reportError.value = describeApiError(err)
  }
}

async function confirmRemove() {
  removeBusy.value = true
  removeError.value = ''
  try {
    await remarksApi.remove(props.remark.id)
    removing.value = false
    await refresh()
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    removeError.value = describeApiError(err)
  } finally {
    removeBusy.value = false
  }
}
</script>

<template>
  <li class="remark" :data-remark="remark.id">
    <p class="remark__meta">
      <strong>{{ remark.author }}</strong> · <time :datetime="remark.created_at">{{ formatDateTime(remark.created_at) }}</time>
    </p>
    <p class="remark__text">{{ remark.text }}</p>
    <div class="remark__actions">
      <UiButton v-if="auth.isAuthenticated" size="sm" variant="ghost" data-testid="remark-reply" @click="replying = !replying">{{ $t('doc.remarks.reply') }}</UiButton>
      <UiButton v-if="auth.isAuthenticated && !reported" size="sm" variant="ghost" data-testid="remark-report" @click="report">{{ $t('doc.remarks.report') }}</UiButton>
      <span v-else-if="reported" class="remark__reported" data-testid="remark-reported">{{ $t('doc.remarks.reported') }}</span>
      <UiButton v-if="remark.can_delete" size="sm" variant="ghost" data-testid="remark-delete" @click="removing = true">{{ $t('doc.remarks.remove') }}</UiButton>
    </div>
    <UiAlert v-if="reportError" tone="danger">{{ reportError }}</UiAlert>

    <form v-if="replying" novalidate class="remark__reply-form" @submit.prevent="submitReply">
      <UiAlert v-if="replyForm.formError.value" tone="danger">{{ replyForm.formError.value }}</UiAlert>
      <UiField :id="`remark-reply-${remark.id}`" :label="$t('doc.remarks.replyPlaceholder')" hide-label :error="replyForm.errors.text">
        <UiTextarea v-model="replyText" :rows="2" :maxlength="2000" :placeholder="$t('doc.remarks.replyPlaceholder')" />
      </UiField>
      <div class="remark__reply-actions">
        <UiButton type="submit" size="sm" variant="primary" :loading="replyForm.submitting.value" data-testid="remark-reply-submit">{{ $t('doc.remarks.replySubmit') }}</UiButton>
        <UiButton size="sm" variant="link" @click="replying = false">{{ $t('doc.remarks.cancel') }}</UiButton>
      </div>
    </form>

    <ul v-if="kids.length" class="remark__children">
      <DocumentRemarkNode v-for="child in kids" :key="child.id" :remark="child" :by-parent="byParent" :doc-ref="docRef" />
    </ul>

    <UiModal v-model:open="removing" :title="$t('doc.remarks.remove')" size="sm" testid="remark-delete-dialog">
      <p>{{ $t('doc.remarks.removeConfirm') }}</p>
      <UiAlert v-if="removeError" tone="danger">{{ removeError }}</UiAlert>
      <div class="remark__reply-actions">
        <UiButton variant="danger" :loading="removeBusy" data-testid="remark-confirm-delete" @click="confirmRemove">{{ $t('doc.remarks.remove') }}</UiButton>
        <UiButton variant="link" @click="removing = false">{{ $t('doc.remarks.cancel') }}</UiButton>
      </div>
    </UiModal>
  </li>
</template>

<style scoped>
.remark {
  padding: var(--space-3) 0;
  border-top: 1px dashed var(--border-strong);
}
.remark__meta {
  margin: 0 0 var(--space-1);
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.remark__text {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.remark__actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-2);
}
.remark__reported {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.remark__reply-form {
  margin-top: var(--space-2);
}
.remark__reply-actions {
  display: flex;
  gap: var(--space-2);
  margin-top: var(--space-2);
}
.remark__children {
  margin: var(--space-2) 0 0;
  padding-left: var(--space-5);
  list-style: none;
}
</style>
