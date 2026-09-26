<script setup lang="ts">
import { computed, ref } from 'vue'
import { t } from '@/i18n'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'

// Коды, которые сервер показывает ОДИН раз (одноразовые коды, резервный код): список, «Скопировать», «Скачать файлом».
// Сами по себе они ничего не значат для сайта, но для человека — единственный способ вернуться, поэтому сохранить их
// нужно до того, как окно закроется.
const props = defineProps<{
  codes: string[]
  /** Что это за коды — в файле и подсказках */
  what: string
  /** Логин — в файле */
  login: string
  /** Имя файла для «Скачать» */
  filename: string
  /** Пояснение в файле: как ими пользоваться */
  note: string
}>()

const copied = ref('')

const fileText = computed(() => `${t('codes.fileHead', { what: props.what })}\n\n${t('codes.fileLogin', { login: props.login })}\n\n${props.codes.join('\n')}\n\n${props.note}\n`)

async function copy() {
  try {
    await navigator.clipboard.writeText(props.codes.join('\n'))
    copied.value = props.codes.length > 1 ? t('codes.copiedMany') : t('codes.copiedOne')
  } catch {
    copied.value = t('codes.copyFailed')
  }
}

function download() {
  const url = URL.createObjectURL(new Blob([fileText.value], { type: 'text/plain;charset=utf-8' }))
  const a = document.createElement('a')
  a.href = url
  a.download = props.filename
  a.click()
  URL.revokeObjectURL(url)
}
</script>

<template>
  <div class="codes">
    <ul class="codes__list" data-testid="secret-codes">
      <li v-for="c in codes" :key="c">{{ c }}</li>
    </ul>
    <div class="codes__actions">
      <UiButton icon="cards" data-testid="codes-copy" @click="copy">{{ $t('common.copy') }}</UiButton>
      <UiButton icon="download" data-testid="codes-download" @click="download">{{ $t('common.downloadFile') }}</UiButton>
    </div>
    <UiAlert v-if="copied" tone="info" class="codes__copied">{{ copied }}</UiAlert>
  </div>
</template>

<style scoped>
.codes__list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(9.5rem, 1fr));
  gap: var(--space-2);
  margin: 0 0 var(--space-3);
  padding: var(--space-3);
  border: 2px dashed var(--ink-900);
  border-radius: var(--radius-2);
  background: var(--surface-sunken);
  list-style: none;
  font-family: var(--font-doc);
  font-size: var(--text-lg);
  font-weight: 700;
  letter-spacing: 0.04em;
  overflow-wrap: anywhere;
  user-select: all;
}
.codes__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.codes__copied {
  margin-top: var(--space-3);
}
</style>
