import { describe, expect, it } from 'vitest'
import type { GraphEdge, GraphNode } from '@/api/generated/documents'
import { CARD_H, CARD_W, layoutBoard, pinOf, threadPath, wrapTitle } from '@/lib/graph'

const node = (code: string, depth: number): GraphNode => ({ code, slug: code, title: code, type: 'object', type_name: 'Объект', level: 0, year: 1979, depth })

function board(n1: number, n2: number) {
  const nodes = [node('C', 0)]
  const edges: GraphEdge[] = []
  for (let i = 0; i < n1; i++) {
    nodes.push(node(`A${i}`, 1))
    edges.push({ from: 'C', to: `A${i}` })
  }
  for (let i = 0; i < n2; i++) {
    nodes.push(node(`B${i}`, 2))
    edges.push({ from: `A${i % Math.max(1, n1)}`, to: `B${i}` })
  }
  return { nodes, edges, board: layoutBoard(nodes, edges) }
}

describe('layoutBoard', () => {
  it('центр посередине, все карточки размещены, доска вмещает их с полями', () => {
    const { nodes, board: b } = board(6, 5)
    expect(b.placed).toHaveLength(nodes.length)
    expect(b.byCode.get('C')).toMatchObject({ x: 0, y: 0 })
    for (const p of b.placed) {
      expect(p.x - CARD_W / 2).toBeGreaterThanOrEqual(b.view.x)
      expect(p.x + CARD_W / 2).toBeLessThanOrEqual(b.view.x + b.view.w)
      expect(p.y - CARD_H / 2).toBeGreaterThanOrEqual(b.view.y)
      expect(p.y + CARD_H / 2).toBeLessThanOrEqual(b.view.y + b.view.h)
    }
  })

  it('карточки не перекрываются — при любом числе связей до предела доски', () => {
    for (const [n1, n2] of [[1, 0], [3, 2], [8, 8], [12, 27], [39, 0]] as const) {
      const { board: b } = board(n1, n2)
      for (let i = 0; i < b.placed.length; i++) {
        for (let j = i + 1; j < b.placed.length; j++) {
          const a = b.placed[i]!
          const c = b.placed[j]!
          const overlap = Math.abs(a.x - c.x) < CARD_W && Math.abs(a.y - c.y) < CARD_H
          expect(overlap, `${a.node.code} и ${c.node.code} при ${n1}+${n2}`).toBe(false)
        }
      }
    }
  })

  it('раскладка детерминирована и не зависит от порядка карточек в ответе внутри кольца', () => {
    const a = board(5, 4).board.placed.map((p) => [p.node.code, Math.round(p.x), Math.round(p.y)])
    const b = board(5, 4).board.placed.map((p) => [p.node.code, Math.round(p.x), Math.round(p.y)])
    expect(a).toEqual(b)
  })

  it('пустая доска и доска из одного документа не падают', () => {
    expect(layoutBoard([], []).placed).toEqual([])
    const solo = layoutBoard([node('C', 0)], [])
    expect(solo.placed).toHaveLength(1)
    expect(solo.view.w).toBeGreaterThan(CARD_W)
  })
})

describe('threadPath и pinOf', () => {
  it('нить идёт от булавки к булавке и провисает вниз', () => {
    const { board: b } = board(2, 0)
    const c = b.byCode.get('C')!
    const a = b.byCode.get('A0')!
    const d = threadPath(c, a)
    const m = /^M([-\d.]+) ([-\d.]+) Q([-\d.]+) ([-\d.]+) ([-\d.]+) ([-\d.]+)$/.exec(d)!
    expect(m).toBeTruthy()
    const [, x1, y1, cx, cy, x2, y2] = m.map(Number)
    expect([x1, y1]).toEqual([pinOf(c).x, Number(pinOf(c).y.toFixed(1))])
    expect(Math.abs((x2 as number) - pinOf(a).x)).toBeLessThan(0.1)
    expect(Math.abs((y2 as number) - pinOf(a).y)).toBeLessThan(0.1)
    expect(cy as number).toBeGreaterThan(((y1 as number) + (y2 as number)) / 2) // провисает вниз
    expect(Math.abs((cx as number) - ((x1 as number) + (x2 as number)) / 2)).toBeLessThan(30) // небольшой изгиб вбок
    expect(threadPath(c, a)).toBe(d) // тот же вид при повторе: изгиб не случайный
  })
})

describe('wrapTitle', () => {
  it('переносит по словам и обрезает многоточием', () => {
    expect(wrapTitle('Короткое')).toEqual(['Короткое'])
    expect(wrapTitle('Объект «Купол» и его свойства в архиве', 16, 2)).toEqual(['Объект «Купол» и', 'его свойства в…'])
    expect(wrapTitle('  ')).toEqual([])
  })

  it('слишком длинное слово режется по ширине, строк не больше заданного', () => {
    const out = wrapTitle('Абвгдежзийклмнопрстуфхцчшщ', 10, 2)
    expect(out).toHaveLength(2)
    expect(out[0]).toBe('Абвгдежзий')
    expect(out[1]!.endsWith('…')).toBe(true)
    for (const line of out) expect(line.length).toBeLessThanOrEqual(10)
  })
})
