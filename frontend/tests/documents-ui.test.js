import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import BlockRenderer from '../src/components/BlockRenderer.vue'
import RichText from '../src/components/RichText.vue'
import { formatComposed } from '../src/lib/format.js'
import { LEVEL_NAMES, MAX_LEVEL, levelName, requiredAccess } from '../src/lib/levels.js'

describe('уровни допуска', () => {
  it('восемь уровней от Гражданина до Директората', () => {
    expect(LEVEL_NAMES).toHaveLength(8)
    expect(MAX_LEVEL).toBe(7)
    expect(levelName(0)).toBe('Гражданин')
    expect(levelName(4)).toBe('Надзиратель')
    expect(levelName(7)).toBe('Директорат')
    expect(levelName(99)).toBe('')
  })
  it('требуемый допуск: «не ниже уровня N», а для 7 — только Директорат', () => {
    expect(requiredAccess(2)).toBe('допуск не ниже уровня 2 (Стажёр)')
    expect(requiredAccess(6)).toBe('допуск не ниже уровня 6 (Особый Совет)')
    expect(requiredAccess(7)).toBe('только Директорат')
  })
})

describe('formatComposed', () => {
  it('частичные даты: год, месяц и год, полная', () => {
    expect(formatComposed({ year: 1979 })).toBe('1979 г.')
    expect(formatComposed({ year: 1979, month: 3 })).toBe('март 1979 г.')
    expect(formatComposed({ year: 1979, month: 3, day: 14 })).toBe('14 марта 1979 г.')
    expect(formatComposed({ year: 1980, month: 8, day: 1 })).toBe('1 августа 1980 г.')
  })
  it('пусто или мусор — пустая строка', () => {
    expect(formatComposed(null)).toBe('')
    expect(formatComposed({})).toBe('')
  })
})

// RichText — компонент из нескольких корневых узлов; чтобы видеть текст как есть (с пробелами),
// его монтируют внутри абзаца.
const inParagraph = (runs) => mount({ render: () => h('p', [h(RichText, { runs })]) })

describe('RichText', () => {
  const html = (runs) => inParagraph(runs).html()

  it('обычный, жирный, курсивный и жирный курсивный текст', () => {
    const w = inParagraph([{ text: 'а ' }, { text: 'б ', bold: true }, { text: 'в ', italic: true }, { text: 'г', bold: true, italic: true }])
    expect(w.text()).toBe('а б в г')
    expect(w.findAll('strong')).toHaveLength(2)
    expect(w.findAll('em')).toHaveLength(2)
    expect(w.find('strong em').text()).toBe('г')
  })

  it('закрытый фрагмент — чёрная полоса с уровнем и без текста', () => {
    const w = inParagraph([{ text: 'до ' }, { redacted: true, level: 4 }, { text: ' после' }])
    const bar = w.find('.redacted')
    expect(bar.exists()).toBe(true)
    expect(bar.attributes('data-level')).toBe('4')
    expect(bar.attributes('role')).toBe('img')
    expect(bar.attributes('aria-label')).toBe('Засекреченный фрагмент: допуск не ниже уровня 4 (Надзиратель)')
    expect(bar.text()).toBe('')
    expect(w.text()).toBe('до  после')
  })

  it('текст выводится как текст: разметка из документа не исполняется', () => {
    const out = html([{ text: '<img src=x onerror=alert(1)><b>x</b>' }])
    expect(out).not.toContain('<img')
    expect(out).toContain('&lt;img')
  })

  it('пустой список — пусто', () => {
    expect(inParagraph([]).text()).toBe('')
  })
})

describe('BlockRenderer', () => {
  const render = (type, data = {}) => mount(BlockRenderer, { props: { block: { id: 'x', type, data } }, global: { provide: { document: { value: {} } }, stubs: { RouterLink: { props: ['to'], template: '<a><slot /></a>' } } } })
  const run = (text) => [{ text }]

  it('заголовок: h1 занят названием документа, поэтому глубина 1 — h2, 2 — h3, 3 — h4', () => {
    expect(render('heading', { depth: 1, text: 'Один' }).find('h2').text()).toBe('Один')
    expect(render('heading', { depth: 2, text: 'Два' }).find('h3').text()).toBe('Два')
    expect(render('heading', { depth: 3, text: 'Три' }).find('h4').text()).toBe('Три')
    expect(render('heading', { depth: 1, text: 'Один' }).find('h1').exists()).toBe(false)
  })

  it('абзац, список, цитата', () => {
    expect(render('paragraph', { text: run('Абзац') }).text()).toBe('Абзац')
    const list = render('list', { ordered: true, items: [run('раз'), run('два')] })
    expect(list.find('ol').exists()).toBe(true)
    expect(list.findAll('li').map((li) => li.text())).toEqual(['раз', 'два'])
    expect(render('list', { ordered: false, items: [run('раз')] }).find('ul').exists()).toBe(true)
    const quote = render('quote', { text: run('Цитата'), source: 'источник' })
    expect(quote.text()).toContain('Цитата')
    expect(quote.text()).toContain('источник')
  })

  it('штамп, разделитель, страница, сноска, приложение', () => {
    expect(render('stamp', { text: 'Изъято', tone: 'red', tilt: 0 }).text()).toBe('Изъято')
    expect(render('divider', { style: 'stars' }).text()).toContain('*')
    expect(render('divider', { style: 'line' }).find('hr').exists()).toBe(true)
    expect(render('page', { number: '7' }).text()).toContain('7')
    const fn = render('footnote', { mark: '*', text: run('Сноска') })
    expect(fn.text()).toContain('*')
    expect(fn.text()).toContain('Сноска')
    const app = render('appendix', { number: '2', title: 'Схема' })
    expect(app.text()).toContain('Приложение')
    expect(app.text()).toContain('Схема')
  })

  it('служебная записка, вырезка, расшифровка записи, журнал испытаний, таблица', () => {
    const memo = render('memo', { kind: 'order', number: 'ПРИКАЗ-1', from: 'Комитет', to: ['А', 'Б'], subject: 'Тема', body: [run('Текст')], signature: 'Подпись' })
    for (const t of ['Комитет', 'А', 'Б', 'Тема', 'Текст', 'Подпись']) expect(memo.text()).toContain(t)

    const news = render('clipping', { kind: 'newspaper', title: 'Заголовок', source: 'Газета', paragraphs: [run('Новость')] })
    for (const t of ['Заголовок', 'Газета', 'Новость']) expect(news.text()).toContain(t)

    const tr = render('clipping', { kind: 'transcript', lines: [{ speaker: 'Он', text: run('Реплика') }] })
    expect(tr.text()).toContain('Он')
    expect(tr.text()).toContain('Реплика')

    const log = render('experiment_log', { title: 'Журнал', entries: [{ date: '1.1.1979', participants: ['Ж'], text: run('Запись') }] })
    for (const t of ['Журнал', '1.1.1979', 'Ж', 'Запись']) expect(log.text()).toContain(t)

    const table = render('table', { caption: 'Подпись', columns: ['К1', 'К2'], rows: [[run('а'), run('б')]] })
    expect(table.findAll('th').map((th) => th.text())).toEqual(['К1', 'К2'])
    expect(table.findAll('td').map((td) => td.text())).toEqual(['а', 'б'])
  })

  it('таблица: закрытая ячейка — чёрная полоса, а не текст', () => {
    const table = render('table', { columns: ['К'], rows: [[[{ redacted: true, level: 3 }]]] })
    expect(table.find('td .redacted').attributes('data-level')).toBe('3')
  })

  it('метка закрытого блока — плашка с нужным допуском', () => {
    const w = mount(BlockRenderer, { props: { block: { type: 'redacted', data: { level: 6 } } } })
    expect(w.find('.plate').attributes('data-level')).toBe('6')
    expect(w.text()).toContain('Данные удалены')
    expect(w.text()).toContain('Особый Совет')
  })

  it('ссылка на доступный документ — ссылка с шифром и названием; на закрытый — «Засекречен» без подробностей', () => {
    const open = render('doc_link', { available: true, code: 'О-041', slug: 'O-041', title: 'Название', type_name: 'Объект', note: 'см. также' })
    for (const t of ['О-041', 'Название', 'Объект', 'см. также']) expect(open.text()).toContain(t)
    const closed = render('doc_link', { available: false })
    expect(closed.text()).toContain('Засекречен')
    expect(closed.find('a').exists()).toBe(false)
  })

  it('неизвестный тип блока не ломает страницу; «constructor» и прочие имена из прототипа — тоже', () => {
    for (const type of ['zzz', 'constructor', '__proto__', 'toString', 'hasOwnProperty']) {
      const w = mount(BlockRenderer, { props: { block: { type, data: {} } } })
      expect(w.text()).toContain('не может быть показан')
    }
  })
})
