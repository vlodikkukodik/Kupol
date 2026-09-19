<script setup>
import { blockPreview, describeValue } from '../../lib/teamdoc.js'

defineProps({
  diff: { type: Object, required: true }, // ответ /versions/:vid/diff
  blockKindName: { type: Function, required: true },
})

const CHANGE_NAMES = { added: 'Добавлен', removed: 'Удалён', changed: 'Изменён', moved: 'Перенесён' }
const levelOf = (b) => b?.level ?? 0
</script>

<template>
  <div class="diff" data-testid="version-diff">
    <p v-if="diff.same" class="same">Отличий нет.</p>
    <template v-else>
      <table v-if="diff.fields.length" class="fields">
        <caption>Свойства документа</caption>
        <thead>
          <tr><th scope="col">Поле</th><th scope="col">Было</th><th scope="col">Стало</th></tr>
        </thead>
        <tbody>
          <tr v-for="f in diff.fields" :key="f.field" :data-field="f.field">
            <th scope="row">{{ f.label }}</th>
            <td class="before">{{ describeValue(f.before) }}</td>
            <td class="after">{{ describeValue(f.after) }}</td>
          </tr>
        </tbody>
      </table>

      <template v-if="diff.blocks.length">
        <h4 class="sub">Блоки</h4>
        <ul class="blocks">
          <li v-for="b in diff.blocks" :key="`${b.change}-${b.id}`" :class="`change change--${b.change}`" :data-change="b.change" :data-block="b.id">
            <span class="what">{{ CHANGE_NAMES[b.change] }}</span>
            <span class="kind">{{ blockKindName(b.type) }} <code>{{ b.id }}</code></span>
            <span v-if="b.change === 'changed' && levelOf(b.before) !== levelOf(b.after)" class="level">
              допуск {{ levelOf(b.before) }} → {{ levelOf(b.after) }}
            </span>
            <span v-if="b.before && b.change !== 'added'" class="text text--before">
              <span class="visually-hidden">Было:</span>{{ blockPreview(b.before) || '(без текста)' }}
            </span>
            <span v-if="b.after" class="text text--after">
              <span class="visually-hidden">Стало:</span>{{ blockPreview(b.after) || '(без текста)' }}
            </span>
          </li>
        </ul>
      </template>
      <p class="unchanged">Блоков без изменений: {{ diff.unchanged_blocks }}.</p>
    </template>
  </div>
</template>

<style scoped>
.same, .unchanged { color: var(--ink-soft); }
.fields { width: 100%; margin-bottom: var(--space-3); border-collapse: collapse; }
.fields caption, .sub { margin: 0 0 var(--space-2); font-family: var(--font-head); font-size: 1rem; letter-spacing: 0.08em; text-align: left; text-transform: uppercase; }
.fields th, .fields td { padding: 0.4rem 0.6rem; border-bottom: 1px solid var(--rule); text-align: left; vertical-align: top; overflow-wrap: anywhere; }
.fields thead th { border-bottom: 2px solid var(--ink); }
.before { text-decoration: line-through; color: var(--ink-soft); }
.after { font-weight: 700; }
.blocks { margin: 0 0 var(--space-3); padding: 0; list-style: none; }
.change { display: grid; gap: 0.15rem; margin-bottom: var(--space-2); padding: var(--space-2) var(--space-3); border-left: 4px solid var(--rule); background: var(--paper-shade); }
.change--added { border-left-color: #2f6b34; }
.change--removed { border-left-color: var(--stamp-red); }
.change--changed { border-left-color: var(--ink); }
.what { font-family: var(--font-head); letter-spacing: 0.08em; text-transform: uppercase; }
.kind, .level { font-size: 0.85rem; color: var(--ink-soft); }
.text { overflow-wrap: anywhere; }
.text--before { text-decoration: line-through; color: var(--ink-soft); }
</style>
