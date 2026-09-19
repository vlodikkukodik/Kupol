<script setup>
import Stamp from '../components/Stamp.vue'
</script>

<template>
  <article class="notice">
    <Stamp text="Изъято" />
    <h1>Дело не найдено</h1>
    <p>Запрошенный документ отсутствует в архиве или изъят. Проверьте адрес.</p>
    <p><RouterLink class="btn" to="/">На главную</RouterLink></p>
  </article>
</template>

<style scoped>
.notice h1 { margin-top: var(--space-4); }
</style>
