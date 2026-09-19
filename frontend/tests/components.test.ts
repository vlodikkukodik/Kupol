import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import UiStamp from '@/ui/UiStamp.vue'
import UiSeal from '@/ui/UiSeal.vue'
import UiBadge from '@/ui/UiBadge.vue'
import UiAlert from '@/ui/UiAlert.vue'
import UiButton from '@/ui/UiButton.vue'
import UiField from '@/ui/UiField.vue'
import UiInput from '@/ui/UiInput.vue'
import UiTabs from '@/ui/UiTabs.vue'

describe('UiStamp', () => {
  it('выводит текст, красный тон и наклон по умолчанию, с анимацией', () => {
    const w = mount(UiStamp, { props: { text: 'Изъято' } })
    expect(w.text()).toBe('Изъято')
    expect(w.classes()).toContain('ui-stamp--red')
    expect(w.classes()).toContain('ui-stamp--animate')
    expect(w.attributes('style')).toContain('--tilt: -6deg')
  })

  it('чёрный тон, свой наклон, без анимации', () => {
    const w = mount(UiStamp, { props: { text: 'Закрыто', tone: 'ink', tilt: 3, animate: false } })
    expect(w.classes()).toContain('ui-stamp--ink')
    expect(w.classes()).not.toContain('ui-stamp--animate')
    expect(w.attributes('style')).toContain('--tilt: 3deg')
  })
})

describe('UiSeal', () => {
  it('по умолчанию — изображение с подписью для скринридера', () => {
    const svg = mount(UiSeal).find('svg')
    expect(svg.attributes('role')).toBe('img')
    expect(svg.attributes('aria-label')).toBe('Печать КУПОЛ')
    expect(svg.attributes('aria-hidden')).toBeUndefined()
    expect(svg.attributes('width')).toBe('120')
    expect(svg.text()).toContain('КОМИТЕТ УПРАВЛЕНИЯ ПАРАНОРМАЛЬНЫМИ ОБЪЕКТАМИ И ЛОКАЦИЯМИ')
  })

  it('декоративная печать скрыта от скринридера', () => {
    const svg = mount(UiSeal, { props: { decorative: true, size: 40 } }).find('svg')
    expect(svg.attributes('aria-hidden')).toBe('true')
    expect(svg.attributes('role')).toBeUndefined()
    expect(svg.attributes('aria-label')).toBeUndefined()
  })
})

describe('UiBadge и UiAlert', () => {
  it('метка передаёт смысл текстом, а тон — классом', () => {
    const w = mount(UiBadge, { props: { tone: 'review' }, slots: { default: 'На проверке' } })
    expect(w.text()).toBe('На проверке')
    expect(w.classes()).toContain('ui-badge--review')
  })

  it('ошибка объявляется сразу (alert), остальное — вежливо (status), а без live — тихо', () => {
    expect(mount(UiAlert, { props: { tone: 'danger' }, slots: { default: 'Сбой' } }).attributes('role')).toBe('alert')
    expect(mount(UiAlert, { props: { tone: 'success' }, slots: { default: 'Готово' } }).attributes('role')).toBe('status')
    expect(mount(UiAlert, { props: { tone: 'info', live: false }, slots: { default: 'Просто' } }).attributes('role')).toBeUndefined()
  })
})

describe('UiButton', () => {
  it('по умолчанию <button type="button">; submit и disabled передаются', () => {
    const w = mount(UiButton, { slots: { default: 'Нажать' } })
    expect(w.element.tagName).toBe('BUTTON')
    expect(w.attributes('type')).toBe('button')
    const s = mount(UiButton, { props: { type: 'submit', disabled: true }, slots: { default: 'Отправить' } })
    expect(s.attributes('type')).toBe('submit')
    expect(s.attributes('disabled')).toBeDefined()
  })

  it('загрузка блокирует повторное нажатие и сообщает aria-busy', () => {
    const w = mount(UiButton, { props: { loading: true }, slots: { default: 'Идёт' } })
    expect(w.attributes('disabled')).toBeDefined()
    expect(w.attributes('aria-busy')).toBe('true')
  })

  it('с href — обычная ссылка', () => {
    const w = mount(UiButton, { props: { href: '/x' }, slots: { default: 'Ссылка' } })
    expect(w.element.tagName).toBe('A')
    expect(w.attributes('href')).toBe('/x')
  })
})

describe('UiField + UiInput', () => {
  const field = (props: Record<string, unknown>) => mount(UiField, { props: { label: 'Логин', ...props }, slots: { default: UiInput } })

  it('подпись привязана к полю по for/id', () => {
    const w = field({ id: 'x-login' })
    expect(w.find('label').attributes('for')).toBe('x-login')
    expect(w.find('input').attributes('id')).toBe('x-login')
  })

  it('ошибка и подсказка связаны с полем через aria-describedby; поле помечено aria-invalid', () => {
    const w = field({ id: 'y', hint: 'подсказка', error: 'плохо' })
    const input = w.find('input')
    expect(input.attributes('aria-invalid')).toBe('true')
    expect(input.attributes('aria-describedby')).toBe('y-hint y-error')
    expect(w.find('#y-error').text()).toBe('плохо')
    expect(w.find('#y-hint').text()).toBe('подсказка')
  })

  it('без ошибки поле не помечается недопустимым', () => {
    const input = field({ id: 'z' }).find('input')
    expect(input.attributes('aria-invalid')).toBeUndefined()
    expect(input.attributes('aria-describedby')).toBeUndefined()
  })
})

describe('UiTabs', () => {
  const tabs = [
    { id: 'a', label: 'Первая' },
    { id: 'b', label: 'Вторая' },
    { id: 'c', label: 'Третья' },
  ]
  const make = () =>
    mount(UiTabs, {
      attachTo: document.body,
      props: { tabs, label: 'Разделы', modelValue: 'a', 'onUpdate:modelValue': (v: string) => w.setProps({ modelValue: v }) },
      slots: { a: 'содержимое А', b: 'содержимое Б', c: 'содержимое В' },
    })
  let w: ReturnType<typeof make>

  it('одна вкладка в порядке Tab, показана только её панель', () => {
    w = make()
    const tabEls = w.findAll('[role="tab"]')
    expect(tabEls.map((t) => t.attributes('tabindex'))).toEqual(['0', '-1', '-1'])
    expect(tabEls.map((t) => t.attributes('aria-selected'))).toEqual(['true', 'false', 'false'])
    expect(w.text()).toContain('содержимое А')
    expect(w.text()).not.toContain('содержимое Б')
    w.unmount()
  })

  it('стрелки, Home и End переключают вкладки по кругу и переносят фокус', async () => {
    w = make()
    const list = w.find('[role="tablist"]')
    await list.trigger('keydown', { key: 'ArrowRight' })
    expect(w.props('modelValue')).toBe('b')
    expect(document.activeElement?.textContent?.trim()).toBe('Вторая')
    await list.trigger('keydown', { key: 'End' })
    expect(w.props('modelValue')).toBe('c')
    await list.trigger('keydown', { key: 'ArrowRight' })
    expect(w.props('modelValue')).toBe('a')
    await list.trigger('keydown', { key: 'ArrowLeft' })
    expect(w.props('modelValue')).toBe('c')
    await list.trigger('keydown', { key: 'Home' })
    expect(w.props('modelValue')).toBe('a')
    w.unmount()
  })
})
