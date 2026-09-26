// «Доска с нитками»: раскладка карточек и нитей. Чистые функции — их проверяет vitest, а компонент только рисует.
import type { GraphEdge, GraphNode } from '@/api/generated/documents'

export const CARD_W = 176
export const CARD_H = 88
const MARGIN = 40

export interface Placed {
  node: GraphNode
  x: number // центр карточки
  y: number
}

export interface Board {
  placed: Placed[]
  /** Границы доски: viewBox = «minX minY width height» */
  view: { x: number; y: number; w: number; h: number }
  byCode: Map<string, Placed>
}

/**
 * Карточки по кольцам: в центре — документ, вокруг него ближайшие связи (глубина 1), дальше — «через одного» (глубина 2).
 * Кольца — эллипсы (карточки широкие); внешние карточки стоят в порядке углов своих «родителей», чтобы нити не перекрещивались
 * без нужды. Раскладка детерминирована: тот же граф даёт ту же доску.
 */
export function layoutBoard(nodes: GraphNode[], edges: GraphEdge[]): Board {
  const byDepth = (d: number) => nodes.filter((n) => n.depth === d)
  const center = byDepth(0)[0]
  const ring1 = byDepth(1)
  const ring2 = byDepth(2)
  const placed: Placed[] = []
  const at = new Map<string, Placed>()
  const put = (node: GraphNode, x: number, y: number) => {
    const p = { node, x, y }
    placed.push(p)
    at.set(node.code, p)
  }

  if (center) put(center, 0, 0)

  // размеры колец растут вместе с числом карточек: рядом стоящие не перекрываются
  const rx1 = Math.max(CARD_W * 1.5, (ring1.length * (CARD_W + 24)) / (2 * Math.PI) * 1.15)
  const ry1 = Math.max(CARD_H * 2.4, (ring1.length * (CARD_H + 40)) / (2 * Math.PI) * 1.15)
  const angleOf = (i: number, n: number, start = -Math.PI / 2) => start + (2 * Math.PI * i) / Math.max(1, n)
  ring1.forEach((n, i) => {
    // со сдвигом: при одной-двух связях карточки не выстраиваются в вертикальную линию с центром
    const a = angleOf(i, ring1.length, -Math.PI / 2 + Math.PI / Math.max(2, ring1.length))
    put(n, Math.cos(a) * rx1, Math.sin(a) * ry1)
  })

  if (ring2.length) {
    // родитель карточки внешнего кольца — первая её связь с внутренним кольцом; по углу родителя внешние и упорядочиваются
    const parentAngle = (code: string): number => {
      for (const e of edges) {
        const other = e.from === code ? e.to : e.to === code ? e.from : null
        const p = other ? at.get(other) : undefined
        if (p && p.node.depth === 1) return Math.atan2(p.y / ry1, p.x / rx1)
      }
      return Number.POSITIVE_INFINITY
    }
    const sorted = [...ring2].sort((a, b) => parentAngle(a.code) - parentAngle(b.code) || a.code.localeCompare(b.code))
    const rx2 = rx1 + CARD_W * 1.25 + Math.max(0, (ring2.length * (CARD_W + 24)) / (2 * Math.PI) * 1.15 - rx1)
    const ry2 = ry1 + CARD_H * 1.9 + Math.max(0, (ring2.length * (CARD_H + 40)) / (2 * Math.PI) * 1.15 - ry1)
    sorted.forEach((n, i) => {
      const a = angleOf(i, sorted.length, -Math.PI / 2 + Math.PI / Math.max(1, sorted.length))
      put(n, Math.cos(a) * rx2, Math.sin(a) * ry2)
    })
  }

  let minX = 0
  let minY = 0
  let maxX = 0
  let maxY = 0
  for (const p of placed) {
    minX = Math.min(minX, p.x - CARD_W / 2)
    maxX = Math.max(maxX, p.x + CARD_W / 2)
    minY = Math.min(minY, p.y - CARD_H / 2)
    maxY = Math.max(maxY, p.y + CARD_H / 2 + 8)
  }
  return {
    placed,
    view: { x: minX - MARGIN, y: minY - MARGIN, w: maxX - minX + 2 * MARGIN, h: maxY - minY + 2 * MARGIN },
    byCode: at,
  }
}

/** Булавка на карточке: сюда крепится нить. */
export const pinOf = (p: Placed): { x: number; y: number } => ({ x: p.x, y: p.y - CARD_H / 2 + 10 })

/** Нить между двумя булавками: провисает вниз, как настоящая (сила тяжести), тем сильнее, чем длиннее. */
export function threadPath(a: Placed, b: Placed): string {
  const p = pinOf(a)
  const q = pinOf(b)
  const dist = Math.hypot(q.x - p.x, q.y - p.y) || 1
  // небольшой изгиб вбок (в сторону, зависящую от шифров, а не случайную): нити не сливаются в прямые линии
  let h = 0
  for (const ch of a.node.code + b.node.code) h = (h * 31 + ch.charCodeAt(0)) % 997
  const side = h % 2 === 0 ? 1 : -1
  const bulge = Math.min(26, dist * 0.07) * side
  const cx = (p.x + q.x) / 2 + (-(q.y - p.y) / dist) * bulge
  const cy = (p.y + q.y) / 2 + (((q.x - p.x) / dist) * bulge) + Math.min(90, dist * 0.16)
  return `M${p.x.toFixed(1)} ${p.y.toFixed(1)} Q${cx.toFixed(1)} ${cy.toFixed(1)} ${q.x.toFixed(1)} ${q.y.toFixed(1)}`
}

/** Название на карточке: не длиннее lines строк по width знаков, с многоточием. Слова не рвутся, кроме слишком длинных. */
export function wrapTitle(title: string, width = 22, lines = 2): string[] {
  const words = title.split(/\s+/).filter(Boolean)
  const out: string[] = []
  let cur = ''
  for (let w of words) {
    while (w.length > width) {
      // слишком длинное слово режем по ширине
      if (cur) {
        out.push(cur)
        cur = ''
      }
      out.push(w.slice(0, width))
      w = w.slice(width)
    }
    if (!cur) cur = w
    else if (cur.length + 1 + w.length <= width) cur += ` ${w}`
    else {
      out.push(cur)
      cur = w
    }
  }
  if (cur) out.push(cur)
  if (out.length <= lines) return out
  const kept = out.slice(0, lines)
  const last = kept[lines - 1] ?? ''
  kept[lines - 1] = `${last.slice(0, Math.max(0, width - 1)).trimEnd()}…`
  return kept
}
