<script setup>
import Stamp from '../components/Stamp.vue'

defineProps({
  requestId: { type: String, default: '' },
  retrying: { type: Boolean, default: false },
})
defineEmits(['retry'])
</script>

<template>
  <article class="notice" role="alert">
    <Stamp text="Сбой" />
    <h1>Сбой архива</h1>
    <p>Архив временно недоступен. Повторите попытку позже.</p>
    <p>
      <button type="button" class="btn" :disabled="retrying" @click="$emit('retry')">
        {{ retrying ? 'Проверка…' : 'Повторить' }}
      </button>
    </p>
    <p v-if="requestId" class="ref">Номер обращения: {{ requestId }}</p>
  </article>
</template>

<style scoped>
.notice h1 { margin-top: var(--space-4); }
.ref { font-size: 0.85rem; color: var(--ink-soft); }
</style>
