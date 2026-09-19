import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import Stamp from '../src/components/Stamp.vue'
import SealMark from '../src/components/SealMark.vue'

describe('Stamp', () => {
  it('выводит текст, красный тон и наклон по умолчанию, с анимацией', () => {
    const w = mount(Stamp, { props: { text: 'Изъято' } })
    expect(w.text()).toBe('Изъято')
    expect(w.classes()).toContain('stamp--red')
    expect(w.classes()).toContain('stamp--animate')
    expect(w.attributes('style')).toContain('--tilt: -6deg')
  })

  it('чёрный тон, свой наклон, без анимации', () => {
    const w = mount(Stamp, { props: { text: 'Закрыто', tone: 'ink', tilt: 3, animate: false } })
    expect(w.classes()).toContain('stamp--ink')
    expect(w.classes()).not.toContain('stamp--animate')
    expect(w.attributes('style')).toContain('--tilt: 3deg')
  })
})

describe('SealMark', () => {
  it('по умолчанию — изображение с подписью для скринридера', () => {
    const w = mount(SealMark)
    const svg = w.find('svg')
    expect(svg.attributes('role')).toBe('img')
    expect(svg.attributes('aria-label')).toBe('Печать КУПОЛ')
    expect(svg.attributes('aria-hidden')).toBeUndefined()
    expect(svg.attributes('width')).toBe('120')
    expect(w.text()).toContain('КОМИТЕТ УПРАВЛЕНИЯ ПАРАНОРМАЛЬНЫМИ ОБЪЕКТАМИ И ЛОКАЦИЯМИ')
  })

  it('декоративная печать скрыта от скринридера', () => {
    const svg = mount(SealMark, { props: { decorative: true, size: 40 } }).find('svg')
    expect(svg.attributes('aria-hidden')).toBe('true')
    expect(svg.attributes('role')).toBeUndefined()
    expect(svg.attributes('aria-label')).toBeUndefined()
  })
})
