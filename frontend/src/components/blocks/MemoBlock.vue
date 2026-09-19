<script setup>
import { computed } from 'vue'
import RichText from '../RichText.vue'

const props = defineProps({ data: { type: Object, required: true } })
// { kind: memo | order | letter, number, date, from, to[], subject, body: [runs], signature }

const heading = computed(
  () => ({ memo: 'Служебная записка', order: 'Приказ', letter: 'Письмо' })[props.data.kind] || 'Документ',
)
</script>

<template>
  <section class="blank" :class="`blank--${data.kind}`" :aria-label="heading">
    <header class="top">
      <span class="kind">{{ heading }}</span>
      <span v-if="data.number" class="number">№ {{ data.number }}</span>
      <span v-if="data.date" class="date">{{ data.date }}</span>
    </header>
    <dl v-if="data.from || (data.to && data.to.length) || data.subject" class="route">
      <div v-if="data.from"><dt>От</dt><dd>{{ data.from }}</dd></div>
      <div v-if="data.to && data.to.length"><dt>Кому</dt><dd>{{ data.to.join('; ') }}</dd></div>
      <div v-if="data.subject"><dt>Тема</dt><dd>{{ data.subject }}</dd></div>
    </dl>
    <p v-for="(para, i) in data.body" :key="i" class="para"><RichText :runs="para" /></p>
    <p v-if="data.signature" class="signature">{{ data.signature }}</p>
  </section>
</template>

<style scoped>
.blank {
  margin: var(--space-4) 0;
  padding: var(--space-4);
  border: 1px solid var(--ink);
  background: #f4eedc;
  box-shadow: 3px 3px 0 var(--rule);
}
.top { display: flex; flex-wrap: wrap; gap: var(--space-1) var(--space-4); align-items: baseline; margin-bottom: var(--space-3); padding-bottom: var(--space-2); border-bottom: 2px solid var(--ink); }
.kind { font-family: var(--font-head); font-size: 1.15rem; font-weight: 700; letter-spacing: 0.12em; text-transform: uppercase; }
.number, .date { font-size: 0.95rem; }
.date { margin-left: auto; }
.route { margin: 0 0 var(--space-3); }
.route div { display: flex; gap: var(--space-3); padding: 0.1rem 0; }
.route dt { flex: none; width: 4rem; color: var(--ink-soft); }
.route dd { margin: 0; font-weight: 700; overflow-wrap: anywhere; }
.para { white-space: pre-line; overflow-wrap: anywhere; }
.signature { margin: var(--space-4) 0 0; text-align: right; font-style: italic; }
</style>
