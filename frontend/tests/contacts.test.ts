import { describe, expect, it } from 'vitest'
import { linkifyContact } from '@/lib/contacts'

describe('linkifyContact', () => {
  it('адрес сайта становится ссылкой; почта с кириллицей до «@» — нет (в ссылку идут только латинские адреса)', () => {
    expect(linkifyContact('Почта: автор@example.org, сайт https://example.org/about.')).toEqual([
      { text: 'Почта: автор@example.org, сайт ' },
      { text: 'https://example.org/about', href: 'https://example.org/about' },
      { text: '.' },
    ])
  })

  it('латинская почта → mailto, хвостовая пунктуация не входит в ссылку', () => {
    expect(linkifyContact('write (me@kupol.example).')).toEqual([
      { text: 'write (' },
      { text: 'me@kupol.example', href: 'mailto:me@kupol.example' },
      { text: ').' },
    ])
    expect(linkifyContact('см. https://t.me/kupol, и всё')).toEqual([
      { text: 'см. ' },
      { text: 'https://t.me/kupol', href: 'https://t.me/kupol' },
      { text: ', и всё' },
    ])
  })

  it('опасные схемы ссылкой не становятся', () => {
    for (const bad of ['javascript:alert(1)', 'data:text/html,<b>', 'ftp://x.example', 'vbscript:x']) {
      expect(linkifyContact(bad).every((p) => !p.href), bad).toBe(true)
    }
    // http внутри текста после «javascript:» — обычная ссылка, а не выполнение
    const parts = linkifyContact('javascript:https://x.example')
    expect(parts.filter((p) => p.href).map((p) => p.href)).toEqual(['https://x.example/'])
    expect(parts[0]?.text).toBe('javascript:')
  })

  it('обычный текст, пустая строка и юникод целы', () => {
    expect(linkifyContact('Telegram: @kupol')).toEqual([{ text: 'Telegram: @kupol' }])
    expect(linkifyContact('')).toEqual([])
    const text = 'Пишите в личные сообщения — ответ в течение недели'
    expect(linkifyContact(text)).toEqual([{ text }])
  })

  it('склейка кусков даёт исходную строку', () => {
    const line = 'a@b.co и https://x.example/y?z=1#f (запасной: c@d.org)'
    expect(linkifyContact(line).map((p) => p.text).join('')).toBe(line)
  })
})
