import { describe, expect, it } from 'vitest'
import { baseName, breadcrumbs, joinPath, parentPath, validateName } from './paths'

describe('paths', () => {
  it('joinPath / parentPath / baseName', () => {
    expect(joinPath('', 'a')).toBe('a')
    expect(joinPath('a/b', 'c.txt')).toBe('a/b/c.txt')
    expect(parentPath('a/b/c.txt')).toBe('a/b')
    expect(parentPath('a')).toBe('')
    expect(baseName('a/b/c.txt')).toBe('c.txt')
    expect(baseName('c.txt')).toBe('c.txt')
  })

  it('breadcrumbs', () => {
    expect(breadcrumbs('', 'site')).toEqual([{ name: 'site', path: '' }])
    expect(breadcrumbs('a/b', 'site')).toEqual([
      { name: 'site', path: '' },
      { name: 'a', path: 'a' },
      { name: 'b', path: 'a/b' },
    ])
  })

  it.each([
    ['ok.html', null],
    ['  ', 'validation.nameRequired'],
    ['..', 'validation.nameInvalid'],
    ['a/b', 'validation.nameSlash'],
    ['a\\b', 'validation.nameSlash'],
    ['x'.repeat(256), 'validation.nameTooLong'],
  ])('validateName(%j)', (name, want) => expect(validateName(name)).toBe(want))
})
