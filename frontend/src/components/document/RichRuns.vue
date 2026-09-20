<script setup lang="ts">
import { computed } from 'vue'
import type { OutRun } from '@/api/generated/documents'
import { requiredAccess } from '@/lib/levels'
import { redactionWidth } from '@/lib/realism'
import { useDocumentOptional } from './context'

// Фрагменты текста из ответа сервера: {text, bold, italic} или {redacted: true, level}.
const props = withDefaults(defineProps<{ runs?: OutRun[] }>(), { runs: () => [] })
const doc = useDocumentOptional()

// Ширина полосы закрытого фрагмента зависит от положения (шифр документа, номер фрагмента, видимый текст до него), а не от скрытого текста.
const widths = computed(() => {
  let before = 0
  return props.runs.map((run, i) => {
    const w = run.redacted ? redactionWidth(`${doc.value?.code ?? ''}|${i}|${before}|${run.level ?? 0}`) : 0
    before += run.text?.length ?? 0
    return w
  })
})
</script>

<template>
  <template v-for="(run, i) in runs" :key="i">
    <span
      v-if="run.redacted"
      class="redacted"
      role="img"
      :aria-label="`Засекреченный фрагмент: ${requiredAccess(run.level ?? 0)}`"
      :title="`Данные удалены — ${requiredAccess(run.level ?? 0)}`"
      :data-level="run.level"
      :style="{ width: `${widths[i]}ch` }"
    />
    <strong v-else-if="run.bold"><em v-if="run.italic">{{ run.text }}</em><template v-else>{{ run.text }}</template></strong>
    <em v-else-if="run.italic">{{ run.text }}</em>
    <template v-else>{{ run.text }}</template>
  </template>
</template>

<style scoped>
/* Зачернённый фрагмент: ширина «живая», но не зависит от скрытого текста — длина скрытого не раскрывается. */
.redacted {
  display: inline-block;
  min-width: 3.5ch;
  height: 1.05em;
  vertical-align: text-bottom;
  background: var(--redaction);
  border-radius: 1px;
}
</style>
