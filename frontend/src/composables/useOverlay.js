import { nextTick, onBeforeUnmount, watch } from 'vue'

const FOCUSABLE = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

// Открытых окон может быть больше одного за раз только на миг (меню закрывается, окно входа открывается),
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
export function focusableIn(root) {
  return [...root.querySelectorAll(FOCUSABLE)].filter((el) => !el.closest('[hidden]'))
}

/**
 * Поведение окна поверх страницы (боковое меню, окно входа): фокус уходит внутрь и не выходит по Tab,
 * Esc закрывает, страница под окном не прокручивается, а по закрытии фокус возвращается туда, откуда окно открыли.
 *
 * @param {object} p
 * @param {import('vue').Ref<boolean>|(() => boolean)} p.active открыто ли окно
 * @param {import('vue').Ref<HTMLElement|null>} p.container корневой элемент окна
 * @param {() => void} p.onClose закрыть окно
 * @param {() => HTMLElement|null} [p.initialFocus] что сфокусировать при открытии (по умолчанию — первый элемент)
 * @param {() => HTMLElement|null} [p.fallbackFocus] куда вернуть фокус, если прежнего элемента уже нет на странице
 */
export function useOverlay({ active, container, onClose, initialFocus, fallbackFocus }) {
  let opener = null
  let listening = false

  function onKeydown(event) {
    const root = container.value
    if (!root) return
    if (event.key === 'Escape') {
      event.preventDefault()
      onClose()
      return
    }
    if (event.key !== 'Tab') return
    const items = focusableIn(root)
    if (items.length === 0) {
      event.preventDefault()
      root.focus()
      return
    }
    const first = items[0]
    const last = items.at(-1)
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
    // фокус внутри другого окна (окно входа открыли из меню) — то окно закрывается и уходит вместе с кнопкой,
    // вернуть фокус туда будет нельзя.
    const focused = document.activeElement
    opener =
      focused instanceof HTMLElement && focused !== document.body && !focused.closest('[role="dialog"]') ? focused : null
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
    // Вернуть фокус туда, откуда открыли; если того элемента больше нет — на запасной (кнопку меню).
    const back = opener && document.contains(opener) && !opener.closest('[inert]') ? opener : fallbackFocus?.()
    opener = null
    back?.focus()
  }

  watch(
    () => (typeof active === 'function' ? active() : active.value),
    (isOpen) => (isOpen ? open() : close()),
    { immediate: true, flush: 'post' },
  )

  onBeforeUnmount(() => {
    if (listening) {
      document.removeEventListener('keydown', onKeydown)
      listening = false
      openOverlays--
      unlockScroll()
    }
  })
}
