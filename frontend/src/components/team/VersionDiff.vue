<script setup lang="ts">
import type { BlockChange, Diff } from '@/api/generated/documents'
import { blockPreview, describeValue } from '@/lib/teamdoc'
import { t } from '@/i18n'

defineProps<{
  /** ответ /versions/:vid/diff */
  diff: Diff
  blockKindName: (id: string) => string
}>()

const CHANGES = ['added', 'removed', 'changed', 'moved']
const changeName = (c: string): string => (CHANGES.includes(c) ? t(`diff.change.${c}`) : c)
const levelOf = (b: BlockChange['before']): number => b?.level ?? 0
</script>

<template>
  <div class="diff" data-testid="version-diff">
    <p v-if="diff.same" class="same">{{ $t('diff.same') }}</p>
    <template v-else>
      <table v-if="diff.fields.length" class="fields">
        <caption>{{ $t('diff.props') }}</caption>
        <thead>
          <tr><th scope="col">{{ $t('diff.field') }}</th><th scope="col">{{ $t('diff.before') }}</th><th scope="col">{{ $t('diff.after') }}</th></tr>
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
        <h4>{{ $t('diff.blocks') }}</h4>
        <ul class="blocks">
          <li v-for="b in diff.blocks" :key="`${b.change}-${b.id}`" :class="`change change--${b.change}`" :data-change="b.change" :data-block="b.id">
            <span class="what">{{ changeName(b.change) }}</span>
            <span class="kind">{{ blockKindName(b.type) }} <code>{{ b.id }}</code></span>
            <span v-if="b.change === 'changed' && levelOf(b.before) !== levelOf(b.after)" class="level">
              {{ $t('diff.level', { from: levelOf(b.before), to: levelOf(b.after) }) }}
            </span>
            <span v-if="b.before && b.change !== 'added'" class="text text--before">
              <span class="visually-hidden">{{ $t('diff.before') }}:</span>{{ blockPreview(b.before) || $t('diff.noText') }}
            </span>
            <span v-if="b.after" class="text text--after">
              <span class="visually-hidden">{{ $t('diff.after') }}:</span>{{ blockPreview(b.after) || $t('diff.noText') }}
            </span>
          </li>
        </ul>
      </template>
      <p class="unchanged">{{ $t('diff.unchanged', { n: diff.unchanged_blocks }) }}</p>
    </template>
  </div>
</template>

<style scoped>
.diff {
  padding: var(--space-3) 0;
}
.same,
.unchanged {
  color: var(--text-muted);
}
.fields {
  width: 100%;
  margin-bottom: var(--space-4);
  font-size: var(--text-sm);
}
.fields caption {
  margin-bottom: var(--space-1);
  text-align: left;
  font-family: var(--font-head);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.fields th,
.fields td {
  padding: var(--space-1) var(--space-3);
  border: 1px solid var(--border-strong);
  text-align: left;
  vertical-align: top;
}
.before {
  background: #f8e6e1;
  text-decoration: line-through;
  text-decoration-color: rgb(140 34 24 / 0.5);
}
.after {
  background: #e6efe2;
}
.blocks {
  margin: 0 0 var(--space-3);
  padding: 0;
  list-style: none;
}
.change {
  display: grid;
  gap: var(--space-1);
  margin-bottom: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border-left: 5px solid var(--border-strong);
  background: var(--surface-sunken);
  font-size: var(--text-sm);
}
.change--added {
  border-left-color: var(--green-700);
}
.change--removed {
  border-left-color: var(--red-700);
}
.change--changed {
  border-left-color: var(--amber-700);
}
.change--moved {
  border-left-color: var(--blue-700);
}
.what {
  font-family: var(--font-head);
  font-weight: 700;
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.text--before {
  color: var(--red-800);
  text-decoration: line-through;
  text-decoration-color: rgb(140 34 24 / 0.5);
}
.text--after {
  color: #1c4529;
}
</style>
