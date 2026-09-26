<script setup lang="ts">
import { computed } from 'vue'
import UiStamp from '@/ui/UiStamp.vue'
import { formatComposed } from '@/lib/format'
import { levelName } from '@/lib/levels'
import { t } from '@/i18n'
import { useDocument } from '../context'

// Шапка досье строится из свойств самого документа (их отдаёт сервер вместе с документом).
const doc = useDocument()

const rows = computed<[string, string][]>(() => {
  const d = doc.value
  if (!d) return []
  const out: [string, string][] = [
    [t('doc.header.code'), d.code],
    [t('doc.header.type'), d.type_name],
    [t('doc.header.composed'), formatComposed(d.composed)],
  ]
  if (d.danger_class) out.push([t('doc.header.dangerClass'), String(d.danger_class)])
  if (d.deviation_points != null) out.push([t('doc.header.deviation'), t('doc.header.deviationValue', { n: d.deviation_points })])
  if (d.category_name) out.push([t('doc.header.category'), d.category_name])
  if (d.containment_name) out.push([t('doc.header.containment'), d.containment_name])
  if (d.department) out.push([t('doc.header.department'), d.department])
  if (d.discovery_place) out.push([t('doc.header.place'), d.discovery_place])
  out.push([t('doc.header.access'), d.level === 0 ? t('doc.header.accessOpen') : t('doc.header.accessLevel', { level: d.level, name: levelName(d.level) })])
  return out
})
</script>

<template>
  <section v-if="doc" class="header" :aria-label="$t('doc.header.label')" data-testid="dossier-header">
    <div class="header__stamp">
      <UiStamp :text="doc.grif" tone="ink" :tilt="-1.5" :animate="false" size="sm" />
    </div>
    <dl class="fields">
      <div v-for="[name, value] in rows" :key="name">
        <dt>{{ name }}</dt>
        <dd>{{ value }}</dd>
      </div>
    </dl>
  </section>
</template>

<style scoped>
.header {
  margin: 0 0 var(--space-6);
  padding: var(--space-3) var(--space-4) var(--space-2);
  border: 2px solid var(--ink-900);
  background: var(--surface-sunken);
}
.header__stamp {
  display: flex;
  justify-content: flex-end;
}
.fields {
  margin: 0;
  padding: 0;
}
.fields div {
  display: grid;
  grid-template-columns: minmax(9rem, 13rem) 1fr;
  gap: var(--space-3);
  padding: var(--space-1) 0;
  border-bottom: 1px dashed var(--border-strong);
}
.fields div:last-child {
  border-bottom: 0;
}
dt {
  color: var(--text-muted);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
dd {
  margin: 0;
  font-weight: 700;
  overflow-wrap: anywhere;
}
@media (max-width: 34rem) {
  .fields div {
    grid-template-columns: 1fr;
    gap: 0;
  }
}
</style>
