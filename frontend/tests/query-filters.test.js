import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { defineComponent, h } from 'vue'
import { useQueryFilters } from '../src/composables/useQueryFilters.js'

let router
let filters
let wrapper

// Переход завершается не сразу — как с охраной страниц, которая ходит за сессией на сервер.
const SLOW = 40

beforeEach(async () => {
  router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/list', component: { template: '<p />' } }] })
  router.beforeEach(() => new Promise((resolve) => setTimeout(resolve, SLOW)))
  await router.push('/list')
  const Host = defineComponent({
    setup() {
      filters = useQueryFilters()
      return () => h('div')
    },
  })
  wrapper = mount(Host, { global: { plugins: [router] } })
})
afterEach(() => wrapper.unmount())

const query = () => router.currentRoute.value.query

describe('useQueryFilters', () => {
  it('одно изменение попадает в адрес; пустое значение убирает условие; страница сбрасывается', async () => {
    await router.push({ path: '/list', query: { page: '3', type: 'order' } })
    await filters.change({ from: '1970' })
    expect(query()).toEqual({ type: 'order', from: '1970' })
    await filters.change({ type: '' })
    expect(query()).toEqual({ from: '1970' })
  })

  it('быстрые изменения подряд не теряют друг друга (год «с», затем «по» до конца первого перехода)', async () => {
    const first = filters.change({ from: '1902' })
    const second = filters.change({ to: '1902' }) // первый переход ещё идёт
    await Promise.allSettled([first, second])
    expect(query()).toEqual({ from: '1902', to: '1902' })
  })

  it('три изменения подряд, включая сброс одного из них', async () => {
    const all = [filters.change({ type: 'order' }), filters.change({ class: '3' }), filters.change({ type: '' })]
    await Promise.allSettled(all)
    expect(query()).toEqual({ class: '3' })
  })

  it('после завершения перехода следующее изменение строится от актуального адреса, а не от старых накоплений', async () => {
    await filters.change({ from: '1970' })
    // адрес изменили иначе (кнопка «назад», ссылка, «Сбросить фильтры»)
    await router.push({ path: '/list', query: { type: 'memo' } })
    await filters.change({ class: '2' })
    expect(query()).toEqual({ type: 'memo', class: '2' })
  })
})
