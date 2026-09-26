<script setup lang="ts">
// Записки Директората (шаг 5.7): одному читателю по логину или всем сразу. Видна и открывается только Директорату.
import { ref } from 'vue'
import { ApiError } from '@/api/client'
import { teamApi } from '@/api/endpoints'
import { describeApiError } from '@/composables/useForm'
import { t } from '@/i18n'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiSheet from '@/ui/UiSheet.vue'
import UiTextarea from '@/ui/UiTextarea.vue'

const login = ref('')
const title = ref('')
const body = ref('')
const errors = ref<Record<string, string>>({})
const failure = ref('')
const notice = ref('')
const busy = ref(false)

async function send() {
  errors.value = {}
  failure.value = ''
  notice.value = ''
  if (!title.value.trim()) errors.value.title = t('teamInbox.enterTitle')
  if (!body.value.trim()) errors.value.body = t('teamInbox.enterBody')
  if (Object.keys(errors.value).length) return
  busy.value = true
  try {
    const res = await teamApi.sendNote({ login: login.value.trim(), title: title.value, body: body.value })
    notice.value = t('teamInbox.sent', { n: res.recipients })
    title.value = ''
    body.value = ''
  } catch (err) {
    if (!(err instanceof ApiError)) throw err
    errors.value = { ...err.fields }
    failure.value = Object.keys(errors.value).length ? '' : describeApiError(err)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <UiSheet as="section" aria-labelledby="team-inbox-title" data-testid="team-inbox">
    <h2 id="team-inbox-title">{{ $t('teamInbox.title') }}</h2>
    <p class="lead">{{ $t('teamInbox.lead') }}</p>
    <p class="visually-hidden" role="status">{{ notice }}</p>
    <UiAlert v-if="notice" tone="success" :live="false">{{ notice }}</UiAlert>
    <UiAlert v-if="failure" tone="danger">{{ failure }}</UiAlert>
    <form novalidate @submit.prevent="send">
      <UiField :label="$t('teamInbox.to')" :hint="$t('teamInbox.toHint')" :error="errors.login">
        <UiInput v-model="login" autocomplete="off" />
      </UiField>
      <UiField :label="$t('teamInbox.noteTitle')" required :error="errors.title">
        <UiInput v-model="title" :maxlength="200" />
      </UiField>
      <UiField :label="$t('teamInbox.noteBody')" required :error="errors.body">
        <UiTextarea v-model="body" :rows="6" :maxlength="4000" />
      </UiField>
      <UiButton type="submit" variant="primary" icon="mail" :loading="busy" data-testid="note-send">{{ $t('teamInbox.send') }}</UiButton>
    </form>
  </UiSheet>
</template>

<style scoped>
.lead {
  max-width: 44rem;
  color: var(--text-muted);
}
</style>
