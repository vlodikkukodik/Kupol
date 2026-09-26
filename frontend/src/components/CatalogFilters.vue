<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { LocationQuery } from 'vue-router'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiSelect from '@/ui/UiSelect.vue'
import type { Summary } from '@/api/generated/documents'
import { categoryOptions, containmentOptions, hasFilters } from '@/lib/catalog'

const props = withDefaults(
  defineProps<{
    /** query адреса */
    query: LocationQuery
    /** /documents/summary: какие типы, отделы и классы есть */
    summary?: Summary | null
    /** Название группы для скринридера */
    label?: string
  }>(),
  { summary: null, label: undefined },
)
const emit = defineEmits<{ change: [changes: Record<string, string>]; reset: [] }>()

const one = (v: LocationQuery[string] | undefined): string => (Array.isArray(v) ? (v[0] ?? '') : (v ?? '')) || ''
const active = computed(() => hasFilters(props.query))

// Годы держим в локальных полях: Vue при каждой перерисовке заново выставляет `value` из шаблона, и, пока человек
// печатает год, а данные каталога догружаются, ещё не подтверждённое значение стиралось бы из поля.
// Значение из адреса подтягивается, только когда сам адрес изменился (кнопка «назад», сброс фильтров).
const from = ref(one(props.query.from))
const to = ref(one(props.query.to))
watch(() => one(props.query.from), (v) => (from.value = v))
watch(() => one(props.query.to), (v) => (to.value = v))

const typeOptions = computed(() => (props.summary?.types ?? []).map((t) => ({ value: t.type, label: `${t.name} (${t.count})` })))
const classOptions = computed(() => (props.summary?.classes ?? []).map((c) => ({ value: String(c.class), label: `${c.class} (${c.count})` })))
const deptOptions = computed(() => (props.summary?.departments ?? []).map((d) => ({ value: d.code, label: `${d.code} (${d.count})` })))
const categories = computed(() => categoryOptions())
const containments = computed(() => containmentOptions())
</script>

<template>
  <form class="filters" :aria-label="label ?? $t('catalog.filters.label')" @submit.prevent>
    <UiField id="f-type" :label="$t('catalog.filters.type')">
      <UiSelect :model-value="one(query.type)" :options="typeOptions" :placeholder="$t('catalog.filters.allTypes')" @update:model-value="emit('change', { type: $event })" />
    </UiField>
    <UiField id="f-class" :label="$t('catalog.filters.dangerClass')">
      <UiSelect :model-value="one(query.class)" :options="classOptions" :placeholder="$t('catalog.filters.any')" @update:model-value="emit('change', { class: $event })" />
    </UiField>
    <UiField id="f-dept" :label="$t('catalog.filters.department')">
      <UiSelect :model-value="one(query.dept)" :options="deptOptions" :placeholder="$t('catalog.filters.any')" @update:model-value="emit('change', { dept: $event })" />
    </UiField>
    <UiField id="f-category" :label="$t('catalog.filters.category')">
      <UiSelect :model-value="one(query.category)" :options="categories" :placeholder="$t('catalog.filters.anyF')" @update:model-value="emit('change', { category: $event })" />
    </UiField>
    <UiField id="f-containment" :label="$t('catalog.filters.containment')">
      <UiSelect :model-value="one(query.containment)" :options="containments" :placeholder="$t('catalog.filters.any')" @update:model-value="emit('change', { containment: $event })" />
    </UiField>
    <fieldset class="years">
      <legend>{{ $t('catalog.filters.period') }}</legend>
      <div class="years__row">
        <UiField id="f-from" :label="$t('catalog.filters.yearFrom')" hide-label>
          <UiInput v-model="from" type="number" :min="1900" :max="2099" inputmode="numeric" :placeholder="$t('catalog.filters.from')" @change="emit('change', { from })" />
        </UiField>
        <span aria-hidden="true">—</span>
        <UiField id="f-to" :label="$t('catalog.filters.yearTo')" hide-label>
          <UiInput v-model="to" type="number" :min="1900" :max="2099" inputmode="numeric" :placeholder="$t('catalog.filters.to')" @change="emit('change', { to })" />
        </UiField>
      </div>
    </fieldset>
    <div v-if="active" class="reset">
      <UiButton variant="link" icon="refresh" @click="emit('reset')">{{ $t('catalog.filters.reset') }}</UiButton>
    </div>
  </form>
</template>

<style scoped>
.filters {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr));
  gap: 0 var(--space-4);
  align-items: end;
  margin-bottom: var(--space-5);
  padding-bottom: var(--space-2);
  border-bottom: 2px solid var(--ink-900);
}
.years {
  min-width: 0;
  margin: 0 0 var(--space-4);
  padding: 0;
  border: 0;
}
.years legend {
  margin-bottom: var(--space-1);
  padding: 0;
  font-family: var(--font-head);
  font-size: var(--text-sm);
  font-weight: 500;
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.years__row {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
.years__row :deep(.ui-field) {
  flex: 1;
  margin-bottom: 0;
}
.reset {
  margin-bottom: var(--space-4);
}
</style>
