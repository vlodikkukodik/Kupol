<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import Stamp from '../components/Stamp.vue'
import { levelName, requiredAccess } from '../lib/levels.js'
import { useAuthStore } from '../stores/auth.js'
import { useUiStore } from '../stores/ui.js'

const props = defineProps({
  /** Нужный уровень допуска из ответа сервера */
  level: { type: Number, required: true },
})

const auth = useAuthStore()
const ui = useUiStore()
const route = useRoute()
const mine = computed(() => (auth.user ? (auth.user.directorate ? 7 : auth.user.level) : 0))
</script>

<template>
  <article class="notice" data-testid="access-denied">
    <Stamp text="Доступ запрещён" />
    <h1>Доступ запрещён</h1>
    <p>Для этого дела нужен {{ requiredAccess(props.level) }}.</p>
    <p v-if="!auth.user">
      Вы не вошли в архив (уровень 0, {{ levelName(0) }}).
      <!-- окно входа поверх документа; после входа читатель вернётся на этот же адрес (уровень мог вырасти) -->
      <button type="button" class="form-link" @click="ui.openAuth({ next: route.fullPath })">Войти или зарегистрироваться</button>
    </p>
    <p v-else>Ваш допуск: уровень {{ mine }} ({{ auth.user.directorate ? 'Директорат' : levelName(mine) }}).</p>
    <p><RouterLink class="btn" to="/catalog">В каталог</RouterLink></p>
  </article>
</template>

<style scoped>
.notice h1 { margin-top: var(--space-4); }
</style>
