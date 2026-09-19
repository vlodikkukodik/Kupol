<script setup>
import FormField from '../FormField.vue'
import { LEVEL_NAMES } from '../../lib/levels.js'

// Свойства документа: название, допуск, режим прямой ссылки, гриф, дата составления, свойства Объекта.
// Поля — строки (как в полях ввода); перевод в содержимое для сервера — lib/teamdoc.js.
const form = defineModel({ type: Object, required: true })
defineProps({
  meta: { type: Object, required: true }, // справочник /team/document-types
  errors: { type: Object, default: () => ({}) }, // путь замечания -> текст
  disabled: { type: Boolean, default: false },
})

const levelLabel = (n) => (n === 0 ? '0 — открыт всем' : n === 7 ? '7 — только Директорат' : `${n} — ${LEVEL_NAMES[n]}`)
</script>

<template>
  <fieldset class="props" :disabled="disabled">
    <legend class="visually-hidden">Свойства документа</legend>

    <FormField id="dp-title" v-model="form.title" label="Название" :error="errors.title" :maxlength="300" :disabled="disabled" />

    <div class="row">
      <div class="field" :class="{ 'field--invalid': errors.level }">
        <label for="dp-level">Допуск к документу</label>
        <select id="dp-level" v-model="form.level" :aria-invalid="errors.level ? 'true' : undefined" :aria-describedby="errors.level ? 'dp-level-error' : 'dp-level-hint'">
          <option v-for="n in meta.max_level + 1" :key="n - 1" :value="String(n - 1)">{{ levelLabel(n - 1) }}</option>
        </select>
        <p id="dp-level-hint" class="hint">Читатель ниже этого уровня не видит документ в каталоге.</p>
        <p v-if="errors.level" id="dp-level-error" class="error">{{ errors.level }}</p>
      </div>
      <div class="field" :class="{ 'field--invalid': errors.direct_link }">
        <label for="dp-direct">По прямой ссылке без допуска</label>
        <select id="dp-direct" v-model="form.direct_link" :aria-invalid="errors.direct_link ? 'true' : undefined">
          <option v-for="o in meta.direct_links" :key="o.id" :value="o.id">{{ o.name }}</option>
        </select>
        <p v-if="errors.direct_link" class="error">{{ errors.direct_link }}</p>
      </div>
    </div>

    <FormField id="dp-grif" v-model="form.grif" label="Гриф" :hint="`По умолчанию — «${meta.default_grif}».`" :error="errors.grif" :maxlength="100" :disabled="disabled" />

    <fieldset class="group">
      <legend>Дата составления (внутри вселенной)</legend>
      <div class="date-row">
        <FormField id="dp-year" v-model="form.year" label="Год" inputmode="numeric" :maxlength="4" :error="errors['composed.year']" :disabled="disabled" />
        <FormField id="dp-month" v-model="form.month" label="Месяц" inputmode="numeric" :maxlength="2" :error="errors['composed.month']" :disabled="disabled" />
        <FormField id="dp-day" v-model="form.day" label="День" inputmode="numeric" :maxlength="2" :error="errors['composed.day']" :disabled="disabled" />
      </div>
      <p v-if="errors.composed" class="error">{{ errors.composed }}</p>
    </fieldset>

    <fieldset v-if="form.props" class="group" data-testid="object-props">
      <legend>Свойства Объекта</legend>
      <p v-if="errors.props" class="error">{{ errors.props }}</p>
      <div class="row">
        <div class="field" :class="{ 'field--invalid': errors['props.danger_class'] }">
          <label for="dp-class">Класс опасности</label>
          <select id="dp-class" v-model="form.props.danger_class" :aria-invalid="errors['props.danger_class'] ? 'true' : undefined">
            <option value="">не указан</option>
            <option v-for="n in 5" :key="n" :value="String(n)">{{ n }}</option>
          </select>
          <p v-if="errors['props.danger_class']" class="error">{{ errors['props.danger_class'] }}</p>
        </div>
        <FormField id="dp-deviation" v-model="form.props.deviation_points" label="Пункты отклонения" inputmode="numeric" :maxlength="9" :error="errors['props.deviation_points']" :disabled="disabled" />
      </div>
      <div class="row">
        <div class="field" :class="{ 'field--invalid': errors['props.category'] }">
          <label for="dp-category">Категория</label>
          <select id="dp-category" v-model="form.props.category" :aria-invalid="errors['props.category'] ? 'true' : undefined">
            <option value="">не указана</option>
            <option v-for="o in meta.categories" :key="o.id" :value="o.id">{{ o.name }}</option>
          </select>
          <p v-if="errors['props.category']" class="error">{{ errors['props.category'] }}</p>
        </div>
        <div class="field" :class="{ 'field--invalid': errors['props.containment_status'] }">
          <label for="dp-containment">Статус содержания</label>
          <select id="dp-containment" v-model="form.props.containment_status" :aria-invalid="errors['props.containment_status'] ? 'true' : undefined">
            <option value="">не указан</option>
            <option v-for="o in meta.containment" :key="o.id" :value="o.id">{{ o.name }}</option>
          </select>
          <p v-if="errors['props.containment_status']" class="error">{{ errors['props.containment_status'] }}</p>
        </div>
      </div>
      <FormField id="dp-dept" v-model="form.props.department" label="Отдел" hint="Шифр отдела или филиала: ОТД-2, ОБ-14." :error="errors['props.department']" :maxlength="20" :disabled="disabled" />
      <FormField id="dp-place" v-model="form.props.discovery_place" label="Место обнаружения" :error="errors['props.discovery_place']" :maxlength="500" :disabled="disabled" />
    </fieldset>
  </fieldset>
</template>

<style scoped>
.props { margin: 0; padding: 0; border: 0; min-width: 0; }
.group { margin: 0 0 var(--space-3); padding: var(--space-3); border: 1px solid var(--rule); min-width: 0; }
.row { display: grid; grid-template-columns: repeat(auto-fit, minmax(13rem, 1fr)); gap: 0 var(--space-3); }
.date-row { display: grid; grid-template-columns: 2fr 1fr 1fr; gap: var(--space-3); }
.date-row :deep(.field) { margin-bottom: 0; }
.field { margin-bottom: var(--space-3); }
label, legend {
  display: block;
  margin-bottom: var(--space-1);
  font-family: var(--font-head);
  font-size: 0.95rem;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
select {
  width: 100%;
  min-width: 0;
  padding: 0.55rem 0.7rem;
  border: 2px solid var(--ink);
  border-radius: var(--radius);
  background: #f4eedc;
  color: var(--ink);
  font: inherit;
}
select:disabled { opacity: 0.7; }
.field--invalid select { border-color: var(--stamp-red); }
.hint { margin: var(--space-1) 0 0; font-size: 0.85rem; color: var(--ink-soft); }
.error { margin: var(--space-1) 0 0; color: var(--stamp-red); font-weight: 700; }
</style>
