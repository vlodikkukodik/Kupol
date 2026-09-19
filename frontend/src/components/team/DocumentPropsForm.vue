<script setup lang="ts">
import { computed } from 'vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiSelect from '@/ui/UiSelect.vue'
import type { Meta } from '@/api/generated/documents'
import { LEVEL_NAMES } from '@/lib/levels'
import type { DocForm } from '@/lib/teamdoc'

// Свойства документа: название, допуск, режим прямой ссылки, гриф, дата составления, свойства Объекта.
// Поля — строки (как в полях ввода); перевод в содержимое для сервера — lib/teamdoc.ts.
const form = defineModel<DocForm>({ required: true })
const props = withDefaults(
  defineProps<{
    /** Справочник /team/document-types */
    meta: Meta
    /** путь замечания → текст */
    errors?: Record<string, string>
    disabled?: boolean
  }>(),
  { errors: () => ({}), disabled: false },
)

const levelLabel = (n: number) => (n === 0 ? '0 — открыт всем' : n === 7 ? '7 — только Директорат' : `${n} — ${LEVEL_NAMES[n]}`)
const levelOptions = computed(() => Array.from({ length: props.meta.max_level + 1 }, (_, n) => ({ value: String(n), label: levelLabel(n) })))
const directOptions = computed(() => props.meta.direct_links.map((o) => ({ value: o.id, label: o.name })))
const classOptions = [1, 2, 3, 4, 5].map((n) => ({ value: String(n), label: String(n) }))
const categoryOptions = computed(() => props.meta.categories.map((o) => ({ value: o.id, label: o.name })))
const containmentOptions = computed(() => props.meta.containment.map((o) => ({ value: o.id, label: o.name })))
</script>

<template>
  <fieldset class="props" :disabled="disabled">
    <legend class="visually-hidden">Свойства документа</legend>

    <UiField id="dp-title" label="Название" :error="errors.title">
      <UiInput v-model="form.title" :maxlength="300" :disabled="disabled" />
    </UiField>

    <div class="row">
      <UiField id="dp-level" label="Допуск к документу" hint="Читатель ниже этого уровня не видит документ в каталоге." :error="errors.level">
        <UiSelect v-model="form.level" :options="levelOptions" :disabled="disabled" />
      </UiField>
      <UiField id="dp-direct" label="По прямой ссылке без допуска" :error="errors.direct_link">
        <UiSelect v-model="form.direct_link" :options="directOptions" :disabled="disabled" />
      </UiField>
    </div>

    <UiField id="dp-grif" label="Гриф" :hint="`По умолчанию — «${meta.default_grif}».`" :error="errors.grif">
      <UiInput v-model="form.grif" :maxlength="100" :disabled="disabled" />
    </UiField>

    <fieldset class="group">
      <legend>Дата составления (внутри вселенной)</legend>
      <div class="date-row">
        <UiField id="dp-year" label="Год" :error="errors['composed.year']">
          <UiInput v-model="form.year" inputmode="numeric" :maxlength="4" :disabled="disabled" />
        </UiField>
        <UiField id="dp-month" label="Месяц" :error="errors['composed.month']">
          <UiInput v-model="form.month" inputmode="numeric" :maxlength="2" :disabled="disabled" />
        </UiField>
        <UiField id="dp-day" label="День" :error="errors['composed.day']">
          <UiInput v-model="form.day" inputmode="numeric" :maxlength="2" :disabled="disabled" />
        </UiField>
      </div>
      <p v-if="errors.composed" class="error">{{ errors.composed }}</p>
    </fieldset>

    <fieldset v-if="form.props" class="group" data-testid="object-props">
      <legend>Свойства Объекта</legend>
      <p v-if="errors.props" class="error">{{ errors.props }}</p>
      <div class="row">
        <UiField id="dp-class" label="Класс опасности" :error="errors['props.danger_class']">
          <UiSelect v-model="form.props.danger_class" :options="classOptions" placeholder="не указан" :disabled="disabled" />
        </UiField>
        <UiField id="dp-deviation" label="Пункты отклонения" :error="errors['props.deviation_points']">
          <UiInput v-model="form.props.deviation_points" inputmode="numeric" :maxlength="9" :disabled="disabled" />
        </UiField>
      </div>
      <div class="row">
        <UiField id="dp-category" label="Категория" :error="errors['props.category']">
          <UiSelect v-model="form.props.category" :options="categoryOptions" placeholder="не указана" :disabled="disabled" />
        </UiField>
        <UiField id="dp-containment" label="Статус содержания" :error="errors['props.containment_status']">
          <UiSelect v-model="form.props.containment_status" :options="containmentOptions" placeholder="не указан" :disabled="disabled" />
        </UiField>
      </div>
      <UiField id="dp-dept" label="Отдел" hint="Шифр отдела или филиала: ОТД-2, ОБ-14." :error="errors['props.department']">
        <UiInput v-model="form.props.department" :maxlength="20" :disabled="disabled" />
      </UiField>
      <UiField id="dp-place" label="Место обнаружения" :error="errors['props.discovery_place']">
        <UiInput v-model="form.props.discovery_place" :maxlength="500" :disabled="disabled" />
      </UiField>
    </fieldset>
  </fieldset>
</template>

<style scoped>
.props {
  min-width: 0;
  margin: 0;
  padding: 0;
  border: 0;
}
.row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
  gap: 0 var(--space-4);
}
.group {
  min-width: 0;
  margin: 0 0 var(--space-4);
  padding: var(--space-3) var(--space-4) var(--space-1);
  border: 2px dashed var(--border-strong);
  border-radius: var(--radius-2);
}
.group legend {
  padding: 0 var(--space-2);
  font-family: var(--font-head);
  font-size: var(--text-sm);
  letter-spacing: var(--tracking-caps);
  text-transform: uppercase;
}
.date-row {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0 var(--space-3);
}
.error {
  margin: 0 0 var(--space-2);
  color: var(--danger);
  font-size: var(--text-sm);
  font-weight: 700;
}
</style>
