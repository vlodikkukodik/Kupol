import { defineStore } from 'pinia'

/**
 * Что сейчас открыто поверх страницы: боковое меню и окно входа. Одновременно открыто не больше одного:
 * окно входа открывается из меню, и меню при этом закрывается.
 */
export const useUiStore = defineStore('ui', {
  state: () => ({
    menuOpen: false,
    authOpen: false,
    /** Куда вернуть после входа из окна (например, на закрытый документ, с которого нажали «Войти»). */
    authNext: null,
  }),
  getters: {
    overlayOpen: (state) => state.menuOpen || state.authOpen,
  },
  actions: {
    openMenu() {
      this.authOpen = false
      this.menuOpen = true
    },
    closeMenu() {
      this.menuOpen = false
    },
    toggleMenu() {
      if (this.menuOpen) this.closeMenu()
      else this.openMenu()
    },
    /** @param {{next?: string|null}} [opts] */
    openAuth({ next = null } = {}) {
      this.menuOpen = false
      this.authNext = next
      this.authOpen = true
    },
    closeAuth() {
      this.authOpen = false
      this.authNext = null
    },
    closeAll() {
      this.menuOpen = false
      this.closeAuth()
    },
  },
})
