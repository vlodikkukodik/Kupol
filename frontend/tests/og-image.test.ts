// Картинки превью — готовые файлы в public/ (см. scripts/gen-og-image.mjs), не результат сборки. Проверяем то, от чего
// зависят og:-теги и apple-touch-icon: файл существует, это PNG и у него правильные пиксельные размеры (значения
// совпадают с тем, что prerender.mjs и public/api/lib.php пишут в og:image:width/height).
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const PUBLIC = resolve(import.meta.dirname, '../public')

/** Ширина и высота из чанка IHDR (первого в PNG, сразу после 8-байтной сигнатуры и 4+4 байт длины/типа чанка). */
function pngSize(path: string): { width: number; height: number } {
  const buf = readFileSync(path)
  expect(buf.subarray(0, 8).toString('hex'), `${path}: не PNG`).toBe('89504e470d0a1a0a')
  return { width: buf.readUInt32BE(16), height: buf.readUInt32BE(20) }
}

describe('картинки превью в public/', () => {
  it.each([
    ['og-image.png', 1200, 630],
    ['og-image-it.png', 1200, 630],
    ['apple-touch-icon.png', 180, 180],
  ])('%s — PNG %dx%d', (file, width, height) => {
    expect(pngSize(resolve(PUBLIC, file))).toEqual({ width, height })
  })
})
