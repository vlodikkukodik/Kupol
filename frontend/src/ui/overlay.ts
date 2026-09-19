import { nextTick, onBeforeUnmount, watch, type Ref } from 'vue'

const FOCUSABLE = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

// Открытых окон бывает больше одного за раз только на миг (меню закрывается, окно входа открывается),
// поэтому блокировку прокрутки страницы и число открытых окон ведём счётчиками.
let openOverlays = 0
let scrollLocks = 0
function lockScroll() {
  if (scrollLocks++ === 0) document.documentElement.classList.add('scroll-locked')
}
function unlockScroll() {
  if (--scrollLocks <= 0) {
    scrollLocks = 0
    document.documentElement.classList.remove('scroll-locked')
  }
}

/** Управляемые элементы окна: без скрытых (`hidden`), в порядке обхода. */
export function focusableIn(root: HTMLElement): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((el) => !el.closest('[hidden]'))
}

export interface OverlayOptions {
  /** Открыто ли окно */
  active: () => boolean
  /** Корневой элемент окна */
  container: Ref<HTMLElement | null>
  onClose: () => void
  /** Что сфокусировать при открытии (по умолчанию — первый управляемый элемент) */
  initialFocus?: () => HTMLElement | null | undefined
  /** Куда вернуть фокус, если элемента, из которого открыли, уже нет на странице */
  fallbackFocus?: () => HTMLElement | null
}

/**
 * Поведение окна поверх страницы (меню, окно входа, диалог): фокус уходит внутрь и не выходит по Tab, Esc закрывает,
 * страница под окном не прокручивается, а по закрытии фокус возвращается туда, откуда окно открыли.
 */
export function useOverlay({ active, container, onClose, initialFocus, fallbackFocus }: OverlayOptions): void {
  let opener: HTMLElement | null = null
  let listening = false

  function onKeydown(event: KeyboardEvent) {
    const root = container.value
    if (!root) return
    if (event.key === 'Escape') {
      event.preventDefault()
      onClose()
      return
    }
    if (event.key !== 'Tab') return
    const items = focusableIn(root)
    const first = items[0]
    const last = items.at(-1)
    if (!first || !last) {
      event.preventDefault()
      root.focus()
      return
    }
    const current = document.activeElement
    if (!root.contains(current)) {
      event.preventDefault()
      first.focus()
    } else if (event.shiftKey && current === first) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && current === last) {
      event.preventDefault()
      first.focus()
    }
  }

  async function open() {
    // Запоминаем только элемент самой страницы. Фокус на <body> — элемент, из которого открывали, уже убран;
    // фокус внутри другого окна (окно входа открыли из меню) — то окно закроется и уйдёт вместе с кнопкой.
    const focused = document.activeElement
    opener = focused instanceof HTMLElement && focused !== document.body && !focused.closest('[role="dialog"]') ? focused : null
    openOverlays++
    lockScroll()
    if (!listening) {
      document.addEventListener('keydown', onKeydown)
      listening = true
    }
    await nextTick()
    const root = container.value
    if (!root) return
    const target = initialFocus?.() || focusableIn(root)[0] || root
    target.focus()
  }

  async function close() {
    if (!listening) return
    document.removeEventListener('keydown', onKeydown)
    listening = false
    openOverlays--
    unlockScroll()
    await nextTick()
    // Меню закрылось, потому что из него открыли окно входа: фокус уже в окне, забирать его нельзя.
    if (openOverlays > 0) return
    const back = opener && document.contains(opener) && !opener.closest('[inert]') ? opener : fallbackFocus?.()
    opener = null
    back?.focus()
  }

  watch(active, (isOpen) => (isOpen ? open() : close()), { immediate: true, flush: 'post' })

  onBeforeUnmount(() => {
    if (listening) {
      document.removeEventListener('keydown', onKeydown)
      listening = false
      openOverlays--
      unlockScroll()
    }
  })
}
