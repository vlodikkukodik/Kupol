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
  <UiNotice :stamp="$t('notice.denied.stamp')" :title="$t('notice.denied.title')" data-testid="access-denied">
    <p>{{ $t('notice.denied.need', { access: requiredAccess(props.level) }) }}</p>
    <p v-if="!auth.user">{{ $t('notice.denied.guest', { name: levelName(0) }) }}</p>
    <p v-else>{{ $t('notice.denied.mine', { level: mine, name: auth.user.directorate ? levelName(7) : levelName(mine) }) }}</p>
    <template #actions>
      <!-- окно входа поверх документа; после входа читатель вернётся на этот же адрес (уровень мог вырасти) -->
      <UiButton v-if="!auth.user" variant="primary" icon="user" @click="ui.openAuth({ next: route.fullPath })">{{ $t('notice.denied.signIn') }}</UiButton>
      <UiButton to="/catalog" icon="book">{{ $t('notice.denied.catalog') }}</UiButton>
    </template>
  </UiNotice>
</template>
