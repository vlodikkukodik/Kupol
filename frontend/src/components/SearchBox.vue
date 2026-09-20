<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'

// Строка поиска по архиву. Отправка ведёт на /search?q=…: запрос живёт в адресе, как и всё состояние списков.
// На самой странице поиска фильтры остаются (keep-filters), с других страниц — идут чистыми.
const props = withDefaults(defineProps<{ initial?: string; id?: string; keepFilters?: Record<string, string> }>(), {
  initial: '',
  id: 'search-q',
  keepFilters: () => ({}),
})
const router = useRouter()
const text = ref(props.initial)
watch(
  () => props.initial,
  (v) => (text.value = v),
)

function submit() {
  const q = text.value.trim()
  if (!q) return
  void router.push({ name: 'search', query: { ...props.keepFilters, q } })
}
</script>

<template>
  <form class="search" role="search" aria-label="Поиск по архиву" @submit.prevent="submit">
    <UiField :id="id" label="Найти в архиве" hint="Слова, «фраза в кавычках», -исключённое слово, шифр (О-41)">
      <UiInput v-model="text" type="search" :maxlength="200" name="q" autocomplete="off" />
    </UiField>
    <UiButton type="submit" variant="primary" icon="search">Найти</UiButton>
  </form>
</template>

<style scoped>
.search {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: var(--space-2) var(--space-3);
}
.search :deep(.ui-field) {
  flex: 1 1 18rem;
  margin-bottom: 0;
}
</style>
