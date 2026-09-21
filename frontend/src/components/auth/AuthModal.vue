<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { t } from '@/i18n'
import UiModal from '@/ui/UiModal.vue'
import UiTabs from '@/ui/UiTabs.vue'
import LoginForm from './LoginForm.vue'
import RegisterForm from './RegisterForm.vue'
import { useUiStore } from '@/stores/ui'

// Единое окно входа и регистрации: открывается из меню, шапки и с закрытых страниц.
const ui = useUiStore()
const tab = ref('login')
const tabs = computed(() => [
  { id: 'login', label: t('auth.tabLogin') },
  { id: 'register', label: t('auth.tabRegister') },
])

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
    :title="$t('auth.modalTitle')"
    testid="auth-modal"
    scrim-testid="auth-scrim"
    :close-label="$t('auth.modalClose')"
    :initial-focus="firstField"
    :fallback-focus="backToMenuButton"
    @update:open="onOpen"
  >
    <UiTabs v-model="tab" :tabs="tabs" :label="$t('auth.tabsLabel')">
      <template #login><LoginForm /></template>
      <template #register><RegisterForm /></template>
    </UiTabs>
  </UiModal>
</template>
