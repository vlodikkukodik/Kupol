<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import UiButton from '@/ui/UiButton.vue'
import UiNotice from '@/ui/UiNotice.vue'
import { levelName, requiredAccess } from '@/lib/levels'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'

/** Нужный уровень допуска из ответа сервера */
const props = defineProps<{ level: number }>()

const auth = useAuthStore()
const ui = useUiStore()
const route = useRoute()
const mine = computed(() => (auth.user ? (auth.user.directorate ? 7 : auth.user.level) : 0))
</script>

<template>
  <UiNotice stamp="Доступ запрещён" title="Доступ запрещён" data-testid="access-denied">
    <p>Для этого дела нужен {{ requiredAccess(props.level) }}.</p>
    <p v-if="!auth.user">Вы не вошли в архив (уровень 0, {{ levelName(0) }}). Возможно, после входа дело откроется.</p>
    <p v-else>Ваш допуск: уровень {{ mine }} ({{ auth.user.directorate ? 'Директорат' : levelName(mine) }}).</p>
    <template #actions>
      <!-- окно входа поверх документа; после входа читатель вернётся на этот же адрес (уровень мог вырасти) -->
      <UiButton v-if="!auth.user" variant="primary" icon="user" @click="ui.openAuth({ next: route.fullPath })">Войти или зарегистрироваться</UiButton>
      <UiButton to="/catalog" icon="book">В каталог</UiButton>
    </template>
  </UiNotice>
</template>
