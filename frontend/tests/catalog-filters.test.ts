import { describe, expect, it } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import CatalogFilters from '@/components/CatalogFilters.vue'
import type { Summary } from '@/api/generated/documents'

const summary: Summary = { total: 3, types: [{ type: 'memo', name: 'Меморандум', count: 3 }], departments: [], classes: [] }

// Печать в поле: событие input на каждое нажатие; change — только когда человек уходит из поля.
// (VTU setValue вызывает и то и другое сразу, поэтому здесь — вручную.)
async function type(wrapper: VueWrapper, selector: string, value: string) {
  const input = wrapper.find<HTMLInputElement>(selector)
  input.element.value = value
  await input.trigger('input')
  return input
}

describe('CatalogFilters: годы', () => {
  it('напечатанный, но ещё не подтверждённый год не стирается, когда компонент перерисовывается', async () => {
    const w = mount(CatalogFilters, { props: { query: { type: 'memo' }, summary: null } })
    const from = await type(w, '#f-from', '1902') // печатает: событие input есть, change (уход из поля) ещё нет
    // приходят данные каталога — фильтры перерисовываются
    await w.setProps({ summary })
    await w.setProps({ query: { type: 'memo' } })
    expect(from.element.value).toBe('1902')
    expect(w.emitted('change')).toBeUndefined() // подтверждения не было — ничего не отправлено
  })

  it('подтверждение отправляет значение поля', async () => {
    const w = mount(CatalogFilters, { props: { query: {}, summary } })
    const to = await type(w, '#f-to', '1990')
    await to.trigger('change')
    expect(w.emitted('change')).toEqual([[{ to: '1990' }]])
  })

  it('когда адрес изменился (назад, сброс), поля показывают значения из адреса', async () => {
    const w = mount(CatalogFilters, { props: { query: { from: '1970', to: '1990' }, summary } })
    expect(w.find<HTMLInputElement>('#f-from').element.value).toBe('1970')
    expect(w.find<HTMLInputElement>('#f-to').element.value).toBe('1990')
    await w.setProps({ query: {} })
    expect(w.find<HTMLInputElement>('#f-from').element.value).toBe('')
    expect(w.find<HTMLInputElement>('#f-to').element.value).toBe('')
    await w.setProps({ query: { from: '1980' } })
    expect(w.find<HTMLInputElement>('#f-from').element.value).toBe('1980')
  })

  it('незавершённый ввод в одном поле не мешает другому', async () => {
    const w = mount(CatalogFilters, { props: { query: { from: '1970' }, summary } })
    await type(w, '#f-to', '19')
    await w.setProps({ query: { from: '1975' } }) // адрес изменился по «from»
    expect(w.find<HTMLInputElement>('#f-from').element.value).toBe('1975')
    expect(w.find<HTMLInputElement>('#f-to').element.value).toBe('19')
  })

  it('выбор типа сообщает изменение с именем условия из адреса', async () => {
    const w = mount(CatalogFilters, { props: { query: {}, summary } })
    await w.find('#f-type').setValue('memo')
    expect(w.emitted('change')).toEqual([[{ type: 'memo' }]])
  })
})
