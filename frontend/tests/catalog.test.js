import { describe, expect, it } from 'vitest'
import { PER_PAGE, ariaSort, hasFilters, nextSort, pageWindow, toApiParams, withFilters } from '../src/lib/catalog.js'

describe('toApiParams', () => {
  it('переводит имена адреса в имена API и всегда просит 25 на странице', () => {
    const p = toApiParams({ type: 'object', class: '3', dept: 'ОТД-2', category: 'entity', containment: 'lost', from: '1970', to: '1990' })
    expect(Object.fromEntries(p)).toEqual({
      type: 'object',
      class: '3',
      department: 'ОТД-2',
      category: 'entity',
      containment: 'lost',
      year_from: '1970',
      year_to: '1990',
      per_page: String(PER_PAGE),
    })
  })

  it('пустое, неизвестное и лишнее отбрасывается', () => {
    const p = toApiParams({ type: '', class: undefined, view: 'folders', sort: 'drop table', order: 'sideways', page: 'abc', evil: 'x' })
    expect(Object.fromEntries(p)).toEqual({ per_page: String(PER_PAGE) })
  })

  it('сортировка и страница передаются; порядок — только desc; первая страница не пишется', () => {
    expect(Object.fromEntries(toApiParams({ sort: 'year', order: 'desc', page: '3' }))).toMatchObject({ sort: 'year', order: 'desc', page: '3' })
    expect(Object.fromEntries(toApiParams({ sort: 'year', order: 'asc', page: '1' }))).not.toHaveProperty('order')
    expect(Object.fromEntries(toApiParams({ page: '1' }))).not.toHaveProperty('page')
    expect(Object.fromEntries(toApiParams({ page: '-2' }))).not.toHaveProperty('page')
  })

  it('повторяющийся параметр в адресе (массив) — берётся первое значение', () => {
    expect(Object.fromEntries(toApiParams({ type: ['order', 'object'] })).type).toBe('order')
  })

  it('значения кодируются: кириллица и спецсимволы не ломают строку запроса', () => {
    const s = toApiParams({ dept: 'ОТД-2&x=1' }).toString()
    expect(s).not.toContain('&x=1')
    expect(new URLSearchParams(s).get('department')).toBe('ОТД-2&x=1')
  })
})

describe('hasFilters', () => {
  it('фильтры — это условия отбора; вид, сортировка и страница не считаются', () => {
    expect(hasFilters({})).toBe(false)
    expect(hasFilters({ view: 'folders', sort: 'title', order: 'desc', page: '2' })).toBe(false)
    expect(hasFilters({ type: 'order' })).toBe(true)
    expect(hasFilters({ from: '1970' })).toBe(true)
    expect(hasFilters({ type: '' })).toBe(false)
  })
})

describe('withFilters', () => {
  it('добавляет условия, убирает пустые и сбрасывает страницу, сохраняя вид и сортировку', () => {
    expect(withFilters({ view: 'x', sort: 'year', page: '4', type: 'order' }, { class: '3', type: '' })).toEqual({ view: 'x', sort: 'year', class: '3' })
  })
  it('не меняет исходный объект', () => {
    const q = { page: '2', type: 'order' }
    withFilters(q, { type: '' })
    expect(q).toEqual({ page: '2', type: 'order' })
  })
})

describe('сортировка по заголовкам', () => {
  it('по умолчанию сортировка по шифру по возрастанию', () => {
    expect(ariaSort({}, 'code')).toBe('ascending')
    expect(ariaSort({}, 'title')).toBe('none')
  })
  it('aria-sort показывает направление только у выбранного столбца', () => {
    expect(ariaSort({ sort: 'year', order: 'desc' }, 'year')).toBe('descending')
    expect(ariaSort({ sort: 'year' }, 'year')).toBe('ascending')
    expect(ariaSort({ sort: 'year', order: 'desc' }, 'code')).toBe('none')
  })
  it('неизвестная сортировка в адресе считается сортировкой по шифру', () => {
    expect(ariaSort({ sort: 'zzz' }, 'code')).toBe('ascending')
  })
  it('повторный клик по тому же столбцу меняет направление, по другому — сортирует по возрастанию; страница сбрасывается', () => {
    expect(nextSort({}, 'code')).toEqual({ sort: 'code', order: 'desc' })
    expect(nextSort({ sort: 'code', order: 'desc', page: '3' }, 'code')).toEqual({ sort: 'code' })
    expect(nextSort({ sort: 'code', order: 'desc', page: '3', type: 'order' }, 'year')).toEqual({ sort: 'year', type: 'order' })
    expect(nextSort({ sort: 'title' }, 'title')).toEqual({ sort: 'title', order: 'desc' })
  })
})

describe('pageWindow', () => {
  it('одна страница — навигация не нужна', () => {
    expect(pageWindow(1, 1)).toEqual([])
    expect(pageWindow(1, 0)).toEqual([])
  })
  it('мало страниц — все подряд', () => {
    expect(pageWindow(2, 4)).toEqual([1, 2, 3, 4])
  })
  it('много страниц: первая, последняя, окно вокруг текущей и разрывы', () => {
    expect(pageWindow(1, 20)).toEqual([1, 2, 3, null, 20])
    expect(pageWindow(10, 20)).toEqual([1, null, 8, 9, 10, 11, 12, null, 20])
    expect(pageWindow(20, 20)).toEqual([1, null, 18, 19, 20])
  })
  it('разрыв в одну страницу не рисуется: показывается сама страница', () => {
    expect(pageWindow(5, 9)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9])
    expect(pageWindow(6, 10)).toEqual([1, null, 4, 5, 6, 7, 8, 9, 10])
  })
})
