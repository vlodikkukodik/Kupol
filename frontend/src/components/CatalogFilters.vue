<script setup>
import { computed, ref, watch } from 'vue'
import { CATEGORY_NAMES, CONTAINMENT_NAMES, hasFilters } from '../lib/catalog.js'

const props = defineProps({
  query: { type: Object, required: true }, // query адреса
  summary: { type: Object, default: null }, // /documents/summary: какие типы, отделы и классы есть
})
const emit = defineEmits(['change', 'reset'])

const one = (v) => (Array.isArray(v) ? v[0] : v) || ''
const active = computed(() => hasFilters(props.query))

// Годы держим в локальных полях: Vue при каждой перерисовке заново выставляет `value` из шаблона, и, пока человек
// печатает год, а данные каталога догружаются, ещё не подтверждённое значение стиралось бы из поля.
// Значение из адреса подтягивается, только когда сам адрес изменился (кнопка «назад», сброс фильтров).
const from = ref(one(props.query.from))
const to = ref(one(props.query.to))
watch(() => one(props.query.from), (v) => (from.value = v))
watch(() => one(props.query.to), (v) => (to.value = v))

function change(name, event) {
  emit('change', { [name]: event.target.value })
}
</script>

<template>
  <form class="filters" aria-label="Фильтры каталога" @submit.prevent>
    <div class="field">
      <label for="f-type">Тип</label>
      <select id="f-type" :value="one(query.type)" @change="change('type', $event)">
        <option value="">Все типы</option>
        <option v-for="t in summary?.types ?? []" :key="t.type" :value="t.type">{{ t.name }} ({{ t.count }})</option>
      </select>
    </div>
    <div class="field">
      <label for="f-class">Класс опасности</label>
      <select id="f-class" :value="one(query.class)" @change="change('class', $event)">
        <option value="">Любой</option>
        <option v-for="c in summary?.classes ?? []" :key="c.class" :value="c.class">{{ c.class }} ({{ c.count }})</option>
      </select>
    </div>
    <div class="field">
      <label for="f-dept">Отдел</label>
      <select id="f-dept" :value="one(query.dept)" @change="change('dept', $event)">
        <option value="">Любой</option>
        <option v-for="d in summary?.departments ?? []" :key="d.code" :value="d.code">{{ d.code }} ({{ d.count }})</option>
      </select>
    </div>
    <div class="field">
      <label for="f-category">Категория</label>
      <select id="f-category" :value="one(query.category)" @change="change('category', $event)">
        <option value="">Любая</option>
        <option v-for="(name, key) in CATEGORY_NAMES" :key="key" :value="key">{{ name }}</option>
      </select>
    </div>
    <div class="field">
      <label for="f-containment">Статус содержания</label>
      <select id="f-containment" :value="one(query.containment)" @change="change('containment', $event)">
        <option value="">Любой</option>
        <option v-for="(name, key) in CONTAINMENT_NAMES" :key="key" :value="key">{{ name }}</option>
      </select>
    </div>
    <div class="field field--year">
      <label for="f-from">Период, год</label>
      <div class="range">
        <input id="f-from" v-model="from" type="number" min="1900" max="2099" inputmode="numeric" placeholder="с" aria-label="Год не ранее" @change="change('from', $event)">
        <span aria-hidden="true">—</span>
        <input id="f-to" v-model="to" type="number" min="1900" max="2099" inputmode="numeric" placeholder="по" aria-label="Год не позднее" @change="change('to', $event)">
      </div>
    </div>
    <div v-if="active" class="field field--reset">
      <button type="button" class="form-link" @click="emit('reset')">Сбросить фильтры</button>
    </div>
  </form>
</template>

<style scoped>
.filters {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr));
  gap: var(--space-3);
  margin-bottom: var(--space-4);
  padding: var(--space-3);
  border: 1px solid var(--ink);
  background: var(--paper-shade);
}
label {
  display: block;
  margin-bottom: 0.2rem;
  font-family: var(--font-head);
  font-size: 0.85rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
select, input {
  width: 100%;
  min-width: 0;
  padding: 0.4rem 0.5rem;
  border: 2px solid var(--ink);
  border-radius: var(--radius);
  background: #f4eedc;
  color: var(--ink);
  font: inherit;
}
.range { display: flex; align-items: center; gap: var(--space-2); }
.field--reset { align-self: end; }
</style>
