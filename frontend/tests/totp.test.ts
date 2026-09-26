import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import jsQR from 'jsqr'
import QrCode from '@/components/QrCode.vue'
import { groupSecret } from '@/lib/totp'

describe('groupSecret', () => {
  it('делит секрет на группы по 4 знака и не оставляет пробела в конце', () => {
    expect(groupSecret('GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ')).toBe('GEZD GNBV GY3T QOJQ GEZD GNBV GY3T QOJQ')
    expect(groupSecret('ABCDEFGHIJ')).toBe('ABCD EFGH IJ')
    expect(groupSecret('ABCD')).toBe('ABCD')
    expect(groupSecret('')).toBe('')
  })

  it('уже разбитый секрет не разъезжается', () => {
    expect(groupSecret('ABCD EFGH IJ')).toBe('ABCD EFGH IJ')
  })
})

/** Растр QR из нарисованного пути: каждый чёрный отрезок «M x y h w v1 h-w z» — w клеток в ряду y. Масштаб — scale пикселей на клетку. */
function rasterize(path: string, size: number, scale: number): { data: Uint8ClampedArray; width: number } {
  const width = size * scale
  const data = new Uint8ClampedArray(width * width * 4).fill(255) // белый фон, непрозрачный
  for (const m of path.matchAll(/M(\d+) (\d+)h(\d+)v1h-\d+z/g)) {
    const [x, y, w] = [Number(m[1]), Number(m[2]), Number(m[3])]
    for (let py = y * scale; py < (y + 1) * scale; py++) {
      for (let px = x * scale; px < (x + w) * scale; px++) {
        const i = (py * width + px) * 4
        data[i] = data[i + 1] = data[i + 2] = 0
      }
    }
  }
  return { data, width }
}

/** Сторона квадрата из viewBox «0 0 N N». */
const viewSize = (viewBox: string): number => Number(viewBox.split(' ')[2])

describe('QrCode', () => {
  const uri = 'otpauth://totp/%D0%9A%D0%A3%D0%9F%D0%9E%D0%9B:Vera?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&issuer=%D0%9A%D0%A3%D0%9F%D0%9E%D0%9B&algorithm=SHA1&digits=6&period=30'

  it('нарисованный код читается сканером и возвращает ту же ссылку', () => {
    const w = mount(QrCode, { props: { value: uri, label: 'QR-код для приложения' } })
    const size = viewSize(w.get('svg').attributes('viewBox')!)
    const { data, width } = rasterize(w.get('path').attributes('d')!, size, 6)
    const decoded = jsQR(data, width, width)
    expect(decoded?.data).toBe(uri)
  })

  it('чёрное на белом, с полем в 4 клетки вокруг; подпись для скринридера', () => {
    const w = mount(QrCode, { props: { value: 'HELLO', label: 'QR-код для приложения' } })
    const svg = w.get('svg')
    expect(svg.attributes('role')).toBe('img')
    expect(svg.attributes('aria-label')).toBe('QR-код для приложения')
    expect(w.get('rect').attributes('fill')).toBe('#fff') // белый в любой теме: иначе в тёмной сканер не прочтёт
    expect(w.get('path').attributes('fill')).toBe('#000')
    const size = viewSize(svg.attributes('viewBox')!)
    expect(size).toBe(21 + 8) // версия 1 — 21 клетка плюс поле
    for (const m of w.get('path').attributes('d')!.matchAll(/M(\d+) (\d+)h(\d+)v1h-\d+z/g)) {
      const [x, y, len] = [Number(m[1]), Number(m[2]), Number(m[3])]
      expect(x).toBeGreaterThanOrEqual(4)
      expect(y).toBeGreaterThanOrEqual(4)
      expect(x + len).toBeLessThanOrEqual(size - 4)
      expect(y).toBeLessThan(size - 4)
    }
  })

  it('другое содержимое — другой рисунок', () => {
    const a = mount(QrCode, { props: { value: 'один', label: 'q' } }).get('path').attributes('d')
    const b = mount(QrCode, { props: { value: 'два', label: 'q' } }).get('path').attributes('d')
    expect(a).not.toBe(b)
  })
})
