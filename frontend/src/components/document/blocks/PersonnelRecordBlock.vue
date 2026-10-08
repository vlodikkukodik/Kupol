<script setup lang="ts">
import { computed } from 'vue'
import type { OutPersonnelRecord } from '@/api/generated/documents'
import { t } from '@/i18n'

const props = defineProps<{ data: OutPersonnelRecord }>()
const STATUSES = ['active', 'transferred', 'deceased', 'missing', 'unknown']
const statusName = computed(() => {
  const s = props.data.status ?? ''
  return t(`doc.block.personnel.status.${STATUSES.includes(s) ? s : 'unknown'}`)
})
</script>

<template>
  <dl class="record">
    <div v-if="data.rank"><dt>{{ $t('doc.block.personnel.rank') }}</dt><dd>{{ data.rank }}</dd></div>
    <div v-if="data.clearance"><dt>{{ $t('doc.block.personnel.clearance') }}</dt><dd>{{ data.clearance }}</dd></div>
    <div><dt>{{ $t('doc.block.personnel.statusLabel') }}</dt><dd>{{ statusName }}</dd></div>
  </dl>
</template>

<style scoped>
.record {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2) var(--space-6);
  margin: var(--space-5) 0;
  padding: var(--space-3) var(--space-4);
  border: 1px solid var(--border-strong);
}
.record dt {
  color: var(--text-muted);
  font-size: var(--text-sm);
}
.record dd {
  margin: 0;
  font-weight: 700;
}
</style>
