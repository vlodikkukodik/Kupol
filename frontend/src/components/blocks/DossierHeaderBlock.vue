<script setup>
import { computed, inject } from 'vue'
import Stamp from '../Stamp.vue'
import { formatComposed } from '../../lib/format.js'
import { levelName } from '../../lib/levels.js'

// Шапка досье строится из свойств самого документа (их отдаёт сервер вместе с документом).
defineProps({ data: { type: Object, default: () => ({}) } })
const doc = inject('document')

const rows = computed(() => {
  const d = doc.value
  const out = [
    ['Шифр', d.code],
    ['Тип', d.type_name],
    ['Дата составления', formatComposed(d.composed)],
  ]
  if (d.danger_class) out.push(['Класс опасности', String(d.danger_class)])
  if (d.deviation_points != null) out.push(['Пункты отклонения', `${d.deviation_points} п.о.`])
  if (d.category_name) out.push(['Категория', d.category_name])
  if (d.containment_name) out.push(['Статус содержания', d.containment_name])
  if (d.department) out.push(['Отдел', d.department])
  if (d.discovery_place) out.push(['Место обнаружения', d.discovery_place])
  out.push(['Допуск', d.level === 0 ? 'открытый' : `уровень ${d.level} — ${levelName(d.level)}`])
  return out
})
</script>

<template>
  <section class="header" aria-label="Шапка досье" data-testid="dossier-header">
    <div class="stamp-line">
      <Stamp :text="doc.grif" tone="ink" :tilt="-1.5" :animate="false" class="grif" />
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
  margin: 0 0 var(--space-5);
  padding: var(--space-3) var(--space-4) var(--space-2);
  border: 2px solid var(--ink);
  background: var(--paper-shade);
}
.stamp-line { display: flex; justify-content: flex-end; }
.grif { font-size: 0.95rem; }
.fields { margin: 0; padding: 0; }
.fields div {
  display: grid;
  grid-template-columns: minmax(9rem, 13rem) 1fr;
  gap: var(--space-3);
  padding: 0.3rem 0;
  border-bottom: 1px solid var(--rule);
}
.fields div:last-child { border-bottom: 0; }
dt { font-family: var(--font-head); letter-spacing: 0.08em; text-transform: uppercase; color: var(--ink-soft); font-size: 0.9rem; }
dd { margin: 0; font-weight: 700; overflow-wrap: anywhere; }
@media (max-width: 34rem) {
  .fields div { grid-template-columns: 1fr; gap: 0; }
}
</style>
