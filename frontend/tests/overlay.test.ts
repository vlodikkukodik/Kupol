import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia, type Pinia } from 'pinia'
import { createMemoryHistory, createRouter, type RouteLocationNormalized, type Router } from 'vue-router'
import { defineComponent, h, nextTick, ref, type Component } from 'vue'
import { focusableIn, useOverlay } from '@/ui/overlay'
import AppMenu from '@/components/AppMenu.vue'
import AuthModal from '@/components/auth/AuthModal.vue'
import { authGuard } from '@/router'
import { useAuthStore } from '@/stores/auth'
import { useUiStore } from '@/stores/ui'
import type { UserDTO } from '@/api/generated/httpapi'

const press = (key: string, opts: KeyboardEventInit = {}) => document.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...opts }))
const byId = (id: string) => document.getElementById(id) as HTMLElement

describe('ui store', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('меню и окно входа открыты не одновременно: окно входа закрывает меню', () => {
    const ui = useUiStore()
    expect(ui.overlayOpen).toBe(false)
    ui.openMenu()
    expect(ui.menuOpen).toBe(true)
    expect(ui.overlayOpen).toBe(true)
    ui.openAuth()
    expect(ui.menuOpen).toBe(false)
    expect(ui.authOpen).toBe(true)
    ui.openMenu()
    expect(ui.authOpen).toBe(false)
    expect(ui.menuOpen).toBe(true)
  })

  it('toggleMenu открывает и закрывает', () => {
    const ui = useUiStore()
    ui.toggleMenu()
    expect(ui.menuOpen).toBe(true)
    ui.toggleMenu()
    expect(ui.menuOpen).toBe(false)
  })

  it('адрес возврата запоминается при открытии окна входа и сбрасывается при закрытии', () => {
    const ui = useUiStore()
    ui.openAuth({ next: '/doc/O-041' })
    expect(ui.authNext).toBe('/doc/O-041')
    ui.closeAuth()
    expect(ui.authNext).toBeNull()
    ui.openAuth()
    expect(ui.authNext).toBeNull()
  })

  it('closeAll закрывает всё', () => {
    const ui = useUiStore()
    ui.openAuth({ next: '/x' })
    ui.closeAll()
    expect(ui.overlayOpen).toBe(false)
    expect(ui.authNext).toBeNull()
  })
})

describe('useOverlay на настоящем DOM', () => {
  let wrapper: VueWrapper
  let closed = 0

  const Host = defineComponent({
    props: { open: Boolean },
    setup(props) {
      const box = ref<HTMLElement | null>(null)
      useOverlay({
        active: () => props.open,
        container: box,
        onClose: () => closed++,
        fallbackFocus: () => document.getElementById('fallback'),
      })
      return () =>
        h('div', [
          h('button', { id: 'opener' }, 'открыть'),
          h('button', { id: 'fallback' }, 'запасной'),
          props.open
            ? h('div', { ref: box, id: 'box', tabindex: -1 }, [
                h('button', { id: 'a' }, 'a'),
                h('input', { id: 'b' }),
                h('div', { hidden: true }, [h('button', { id: 'hidden' }, 'скрытая')]),
                h('button', { id: 'c', disabled: false }, 'c'),
                h('button', { id: 'off', disabled: true }, 'выключена'),
              ])
            : null,
        ])
    },
  })

  const active = () => document.activeElement?.id
  const open = async () => {
    byId('opener').focus()
    await wrapper.setProps({ open: true })
    await flushPromises()
  }

  beforeEach(() => {
    closed = 0
    wrapper = mount(Host, { attachTo: document.body, props: { open: false } })
  })
  afterEach(() => wrapper.unmount())

  it('фокусируемые элементы: без скрытых и выключенных, в порядке обхода', async () => {
    await open()
    expect(focusableIn(byId('box')).map((el) => el.id)).toEqual(['a', 'b', 'c'])
  })

  it('при открытии фокус уходит внутрь, при закрытии возвращается на открывший элемент', async () => {
    await open()
    expect(active()).toBe('a')
    await wrapper.setProps({ open: false })
    await flushPromises()
    expect(active()).toBe('opener')
  })

  it('если открывшего элемента больше нет на странице, фокус уходит на запасной', async () => {
    await open()
    byId('opener').remove()
    await wrapper.setProps({ open: false })
    await flushPromises()
    expect(active()).toBe('fallback')
  })

  it('Tab с последнего элемента идёт на первый, Shift+Tab с первого — на последний', async () => {
    await open()
    byId('c').focus()
    press('Tab')
    expect(active()).toBe('a')
    press('Tab', { shiftKey: true })
    expect(active()).toBe('c')
  })

  it('Tab в середине не перехватывается (браузер сам переходит к следующему)', async () => {
    await open()
    byId('b').focus()
    const event = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
    document.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(false)
  })

  it('если фокус оказался вне окна, Tab возвращает его внутрь', async () => {
    await open()
    byId('fallback').focus()
    press('Tab')
    expect(active()).toBe('a')
  })

  it('Esc закрывает; когда окно закрыто, Esc не обрабатывается', async () => {
    await open()
    press('Escape')
    expect(closed).toBe(1)
    await wrapper.setProps({ open: false })
    await flushPromises()
    press('Escape')
    expect(closed).toBe(1)
  })

  it('прокрутка страницы блокируется, пока окно открыто, и разблокируется после', async () => {
    const root = document.documentElement
    expect(root.classList.contains('scroll-locked')).toBe(false)
    await open()
    expect(root.classList.contains('scroll-locked')).toBe(true)
    await wrapper.setProps({ open: false })
    await flushPromises()
    expect(root.classList.contains('scroll-locked')).toBe(false)
  })

  it('размонтирование открытого окна снимает блокировку и обработчик клавиш', async () => {
    await open()
    wrapper.unmount()
    expect(document.documentElement.classList.contains('scroll-locked')).toBe(false)
    press('Escape')
    expect(closed).toBe(0)
    wrapper = mount(Host, { attachTo: document.body, props: { open: false } }) // для afterEach
  })
})

describe('боковое меню и окно входа', () => {
  let pinia: Pinia
  let router: Router

  beforeEach(async () => {
    pinia = createPinia()
    setActivePinia(pinia)
    router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: { template: '<p />' } },
        { path: '/catalog', component: { template: '<p />' } },
        { path: '/file', component: { template: '<p />' } },
      ],
    })
    void router.push('/')
    await router.isReady()
  })
  afterEach(() => {
    document.body.innerHTML = ''
    document.documentElement.classList.remove('scroll-locked')
  })

  const mountWith = (component: Component) => mount(component, { attachTo: document.body, global: { plugins: [pinia, router] } })
  const text = () => document.body.textContent ?? ''
  const q = <T extends Element>(sel: string) => document.querySelector<T>(sel)
  const userOf = (login: string): UserDTO => ({
    login,
    level: 1,
    level_name: 'Посетитель',
    directorate: false,
    roles: [],
    capabilities: [],
    created_at: '2026-09-19T00:00:00Z',
    totp_enabled: false,
    xp: 0,
    login_streak: 0,
    next_level_xp: 100,
  })

  it('закрытое меню не рисуется', () => {
    mountWith(AppMenu)
    expect(document.getElementById('site-menu')).toBeNull()
  })

  it('гость: ссылки на главную и каталог, кнопка входа; личного дела нет', async () => {
    useAuthStore().$patch({ status: 'ready', user: null })
    mountWith(AppMenu)
    useUiStore().openMenu()
    await nextTick()
    const menu = byId('site-menu')
    expect(menu.getAttribute('role')).toBe('dialog')
    expect(menu.getAttribute('aria-modal')).toBe('true')
    const links = [...menu.querySelectorAll('nav a')].map((a) => a.textContent?.trim())
    expect(links).toEqual(['Главная', 'Каталог', 'Поиск', 'О КУПОЛЕ'])
    const button = [...menu.querySelectorAll('button')].find((b) => b.textContent?.includes('Войти или зарегистрироваться'))
    expect(button).toBeTruthy()
  })

  it('кнопка входа закрывает меню и открывает окно входа', async () => {
    useAuthStore().$patch({ status: 'ready', user: null })
    mountWith(AppMenu)
    const ui = useUiStore()
    ui.openMenu()
    await nextTick()
    ;[...document.querySelectorAll<HTMLButtonElement>('#site-menu button')].find((b) => b.textContent?.includes('Войти'))?.click()
    expect(ui.menuOpen).toBe(false)
    expect(ui.authOpen).toBe(true)
  })

  it('вошедший: личное дело и его логин, кнопки входа нет', async () => {
    useAuthStore().$patch({ status: 'ready', user: userOf('куратор7') })
    mountWith(AppMenu)
    useUiStore().openMenu()
    await nextTick()
    const links = [...document.querySelectorAll('#site-menu nav a')].map((a) => a.textContent?.trim())
    expect(links).toEqual(['Главная', 'Каталог', 'Поиск', 'О КУПОЛЕ', 'Личное дело'])
    expect(text()).toContain('куратор7')
    expect(text()).not.toContain('Войти или зарегистрироваться')
  })

  it('клик по затемнению и по «Закрыть меню» закрывает меню', async () => {
    mountWith(AppMenu)
    const ui = useUiStore()
    ui.openMenu()
    await nextTick()
    q<HTMLElement>('[data-testid="menu-scrim"]')?.click()
    expect(ui.menuOpen).toBe(false)
    ui.openMenu()
    await nextTick()
    document.querySelector<HTMLElement>('#site-menu [aria-label="Закрыть меню"]')?.click()
    expect(ui.menuOpen).toBe(false)
  })

  it('окно входа: диалог с заголовком, вкладки, формы рисуются только пока окно открыто', async () => {
    mountWith(AuthModal)
    expect(q('[data-testid="auth-modal"]')).toBeNull()
    expect(q('input')).toBeNull()

    useUiStore().openAuth()
    await nextTick()
    await flushPromises()
    const modal = q<HTMLElement>('[data-testid="auth-modal"]')
    expect(modal?.getAttribute('role')).toBe('dialog')
    expect(modal?.getAttribute('aria-modal')).toBe('true')
    const title = modal?.getAttribute('aria-labelledby')
    expect(title && byId(title).textContent).toBe('Допуск в архив')
    expect([...(modal?.querySelectorAll('[role="tab"]') ?? [])].map((t) => t.textContent?.trim())).toEqual(['Вход', 'Регистрация'])
    expect(modal?.querySelector('input')).toBeTruthy()
    expect(document.activeElement?.tagName).toBe('INPUT') // фокус сразу в поле логина

    modal?.querySelector<HTMLElement>('[aria-label="Закрыть окно входа"]')?.click()
    expect(useUiStore().authOpen).toBe(false)
  })
})

describe('охрана страниц: гость на закрытой странице', () => {
  beforeEach(() => setActivePinia(createPinia()))

  const route = (fullPath: string, name: string) => {
    const meta = { requiresAuth: true }
    return { meta, matched: [{ meta }], fullPath, name } as unknown as RouteLocationNormalized
  }

  it('возвращается на главную с адресом возврата — в том числе с личного дела (по нему App открывает окно входа)', async () => {
    useAuthStore().$patch({ status: 'ready', user: null })
    expect(await authGuard(route('/file', 'file'))).toEqual({ name: 'home', query: { next: '/file' } })
    expect(await authGuard(route('/x?a=1', 'x'))).toEqual({ name: 'home', query: { next: '/x?a=1' } })
  })

  it('вошедшего пускает', async () => {
    useAuthStore().$patch({ status: 'ready', user: { login: 'a', level: 1, level_name: '', directorate: false, roles: [], capabilities: [], created_at: '' } })
    expect(await authGuard(route('/file', 'file'))).toBe(true)
  })
})
