<script setup lang="ts">
import type { OutRun } from '@/api/generated/documents'
import { requiredAccess } from '@/lib/levels'

// Фрагменты текста из ответа сервера: {text, bold, italic} или {redacted: true, level}.
withDefaults(defineProps<{ runs?: OutRun[] }>(), { runs: () => [] })
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
    />
    <strong v-else-if="run.bold"><em v-if="run.italic">{{ run.text }}</em><template v-else>{{ run.text }}</template></strong>
    <em v-else-if="run.italic">{{ run.text }}</em>
    <template v-else>{{ run.text }}</template>
  </template>
</template>

<style scoped>
/* Зачернённый фрагмент: ширина фиксированная — длина скрытого текста не раскрывается. */
.redacted {
  display: inline-block;
  width: 5.5ch;
  height: 1.05em;
  vertical-align: text-bottom;
  background: var(--redaction);
  border-radius: 1px;
}
</style>
