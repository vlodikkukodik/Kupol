<script setup lang="ts">
import { nextTick, ref, useId } from 'vue'

// Вкладки по шаблону ARIA Authoring Practices: стрелки, Home и End переключают; активная вкладка — единственная в Tab.
// Содержимое вкладки — слот с именем панели (`#login`, `#register`…): панели не активных вкладок не рисуются.
export interface TabItem {
  id: string
  label: string
}
const model = defineModel<string>({ required: true })
const props = defineProps<{ tabs: TabItem[]; label: string }>()

const base = useId()
const els = ref<Record<string, HTMLElement | null>>({})

async function onKeydown(event: KeyboardEvent) {
  const idx = props.tabs.findIndex((t) => t.id === model.value)
  let next = idx
  if (event.key === 'ArrowRight') next = (idx + 1) % props.tabs.length
  else if (event.key === 'ArrowLeft') next = (idx - 1 + props.tabs.length) % props.tabs.length
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = props.tabs.length - 1
  else return
  event.preventDefault()
  const tab = props.tabs[next]
  if (!tab) return
  model.value = tab.id
  await nextTick()
  els.value[tab.id]?.focus()
}
</script>

<template>
  <div class="ui-tabs">
    <div class="ui-tabs__list" role="tablist" :aria-label="label" @keydown="onKeydown">
      <button
        v-for="t in tabs"
        :id="`${base}-tab-${t.id}`"
        :key="t.id"
        :ref="(el) => (els[t.id] = el as HTMLElement | null)"
        type="button"
        role="tab"
        class="ui-tabs__tab"
        :aria-selected="model === t.id ? 'true' : 'false'"
        :aria-controls="`${base}-panel-${t.id}`"
        :tabindex="model === t.id ? 0 : -1"
        @click="model = t.id"
      >
        {{ t.label }}
      </button>
    </div>
    <template v-for="t in tabs" :key="t.id">
      <div
        :id="`${base}-panel-${t.id}`"
        class="ui-tabs__panel"
        role="tabpanel"
        :aria-labelledby="`${base}-tab-${t.id}`"
        :hidden="model !== t.id"
      >
        <slot v-if="model === t.id" :name="t.id" />
      </div>
    </template>
  </div>
</template>

<style scoped>
.ui-tabs__list {
  display: flex;
  gap: var(--space-1);
  margin-bottom: var(--space-4);
  border-bottom: 2px solid var(--ink-900);
}
.ui-tabs__tab {
  margin-bottom: -2px;
  padding: 0.6rem var(--space-5);
  border: 2px solid transparent;
  border-bottom: 0;
  border-radius: var(--radius-2) var(--radius-2) 0 0;
  background: transparent;
  color: var(--text);
  font-family: var(--font-head);
  font-size: var(--text-md);
  font-weight: 500;
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
  cursor: pointer;
}
.ui-tabs__tab:hover:not([aria-selected='true']) {
  background: rgb(0 0 0 / 0.06);
}
.ui-tabs__tab[aria-selected='true'] {
  border-color: var(--ink-900);
  background: var(--surface);
  font-weight: 700;
}
@media (max-width: 34rem) {
  .ui-tabs__tab {
    flex: 1;
    padding-inline: var(--space-2);
  }
}
</style>
