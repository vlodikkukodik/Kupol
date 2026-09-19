import { defineStore } from 'pinia'
import { computed, ref } from 'vue'

/**
 * Что сейчас открыто поверх страницы: боковое меню и окно входа. Одновременно открыто не больше одного:
 * окно входа открывается из меню, и меню при этом закрывается.
 */
export const useUiStore = defineStore('ui', () => {
  const menuOpen = ref(false)
  const authOpen = ref(false)
  /** Куда вернуть после входа из окна (например, на закрытый документ, с которого нажали «Войти»). */
  const authNext = ref<string | null>(null)

  const overlayOpen = computed(() => menuOpen.value || authOpen.value)

  function openMenu() {
    authOpen.value = false
    menuOpen.value = true
  }
  function closeMenu() {
    menuOpen.value = false
  }
  function toggleMenu() {
    if (menuOpen.value) closeMenu()
    else openMenu()
  }
  function openAuth({ next = null }: { next?: string | null } = {}) {
    menuOpen.value = false
    authNext.value = next
    authOpen.value = true
  }
  function closeAuth() {
    authOpen.value = false
    authNext.value = null
  }
  function closeAll() {
    menuOpen.value = false
    closeAuth()
  }

  return { menuOpen, authOpen, authNext, overlayOpen, openMenu, closeMenu, toggleMenu, openAuth, closeAuth, closeAll }
})
