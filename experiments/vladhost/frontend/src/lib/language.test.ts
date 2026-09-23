import { describe, expect, it } from 'vitest'
import { langFor } from './language'

describe('langFor', () => {
  it.each([
    ['index.html', 'html'],
    ['INDEX.HTM', 'html'],
    ['a/b.css', 'css'],
    ['app.mjs', 'js'],
    ['main.tsx', 'ts'],
    ['data.json', 'json'],
    ['README.md', 'markdown'],
    ['Makefile', 'text'],
    ['.gitignore', 'text'],
    ['photo.png', 'text'],
  ])('%s → %s', (name, want) => expect(langFor(name)).toBe(want))
})
