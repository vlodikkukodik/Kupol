<script setup lang="ts">
import { ref, watch } from 'vue'
import UiModal from '@/ui/UiModal.vue'
import UiTabs from '@/ui/UiTabs.vue'
import LoginForm from './LoginForm.vue'
import RegisterForm from './RegisterForm.vue'
import { useUiStore } from '@/stores/ui'

// Единое окно входа и регистрации: открывается из меню, шапки и с закрытых страниц.
const ui = useUiStore()
const tab = ref('login')
const tabs = [
  { id: 'login', label: 'Вход' },
  { id: 'register', label: 'Регистрация' },
]

// Окно всегда открывается на «Вход»: прежняя вкладка (например, «Регистрация») не запоминается между открытиями.
watch(
  () => ui.authOpen,
  (open) => {
    if (open) tab.value = 'login'
  },
)

const onOpen = (value: boolean) => {
  if (!value) ui.closeAuth()
}

// Открыли окно, чтобы войти, — фокус сразу в первое поле.
const firstField = () => document.querySelector<HTMLElement>('[data-testid="auth-modal"] input')
const backToMenuButton = () => document.getElementById('menu-toggle')
</script>

<template>
  <UiModal
    :open="ui.authOpen"
    title="Допуск в архив"
    testid="auth-modal"
    scrim-testid="auth-scrim"
    close-label="Закрыть окно входа"
    :initial-focus="firstField"
    :fallback-focus="backToMenuButton"
    @update:open="onOpen"
  >
    <UiTabs v-model="tab" :tabs="tabs" label="Вход или регистрация">
      <template #login><LoginForm /></template>
      <template #register><RegisterForm /></template>
    </UiTabs>
  </UiModal>
</template>
