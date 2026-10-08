// Помощники формы редактора для перевода дела (translations_test.go — зеркало на бэке).
import { describe, expect, it } from 'vitest'
import type { Content } from '@/api/generated/documents'
import {
  canonicalContent,
  contentFromForm,
  formFromContent,
  sameContent,
  translationFormFromContent,
  translationFromForm,
} from '@/lib/teamdoc'

const baseContent = (): Content => ({
  title: 'Гамма',
  composed: { year: 1981 },
  blocks: [{ id: 'p1', type: 'paragraph', level: undefined, data: { text: 'Текст' } }],
})

describe('перевод дела в форме редактора', () => {
  it('translationFormFromContent: пусто, если content.it нет', () => {
    expect(translationFormFromContent(baseContent())).toEqual({ title: '', blocks: [] })
  })

  it('translationFormFromContent: читает заголовок и блоки перевода', () => {
    const content: Content = { ...baseContent(), it: { title: 'Gamma', blocks: [{ id: 'p1', type: 'paragraph', level: undefined, data: { text: 'Testo' } }] } }
    const form = translationFormFromContent(content)
    expect(form.title).toBe('Gamma')
    expect(form.blocks).toHaveLength(1)
  })

  it('translationFromForm: пустая вкладка (нет заголовка и блоков) — перевода нет', () => {
    expect(translationFromForm({ title: '', blocks: [] })).toBeUndefined()
  })

  it('translationFromForm: заполненная вкладка идёт как есть', () => {
    const t = translationFromForm({ title: 'Gamma', blocks: [{ id: 'p1', type: 'paragraph', level: undefined, data: { text: 'Testo' } }] })
    expect(t?.title).toBe('Gamma')
    expect(t?.blocks).toHaveLength(1)
  })

  it('contentFromForm: перевод попадает в content.it, только если вкладка заполнена', () => {
    const form = formFromContent(baseContent(), 'memo')
    const empty = contentFromForm(form, { it: { title: '', blocks: [] } })
    expect(empty.content.it).toBeUndefined()

    const filled = contentFromForm(form, { it: { title: 'Gamma', blocks: [{ id: 'p1', type: 'paragraph', level: undefined, data: { text: 'Testo' } }] } })
    expect(filled.content.it?.title).toBe('Gamma')
  })

  it('sameContent и canonicalContent учитывают it наравне с основным содержимым', () => {
    const a: Content = { ...baseContent(), it: { title: 'Gamma', blocks: [{ id: 'p1', type: 'paragraph', level: undefined, data: { text: 'Testo' } }] } }
    const b: Content = { ...baseContent(), it: { title: 'Gamma', blocks: [{ id: 'p1', type: 'paragraph', level: undefined, data: { text: 'Testo' } }] } }
    expect(sameContent(canonicalContent(a), canonicalContent(b))).toBe(true)

    const c: Content = { ...baseContent(), it: { title: 'Altro', blocks: [] } }
    expect(sameContent(canonicalContent(a), canonicalContent(c))).toBe(false)
  })
})
