<script setup>
import Stamp from '../components/Stamp.vue'

defineProps({ retrying: { type: Boolean, default: false } })
defineEmits(['retry'])
</script>

<template>
  <article class="notice" role="status">
    <Stamp text="Закрыто" tone="ink" />
    <h1>Архив закрыт на инвентаризацию</h1>
    <p>Доступ временно приостановлен. Загляните позже.</p>
    <p>
      <button type="button" class="btn" :disabled="retrying" @click="$emit('retry')">
        {{ retrying ? 'Проверка…' : 'Проверить снова' }}
      </button>
    </p>
  </article>
</template>

<style scoped>
.notice h1 { margin-top: var(--space-4); }
</style>
