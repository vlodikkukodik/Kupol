import type { Site } from '@/api/schemas'

// Данные для «Сводки» аккаунта, вычисляемые из уже загруженных сайтов (отдельных запросов не нужно).

/** Порог заполнения диска, после которого сводка предупреждает. */
export const DISK_WARN_PERCENT = 80

/** За сколько суток до окончания сертификата сводка предупреждает: автопродление к этому сроку уже должно было сработать. */
export const CERT_WARN_DAYS = 14

/** Сколько целых суток осталось до момента; отрицательно, если он уже прошёл. */
export function daysUntil(iso: string, now: Date = new Date()): number {
  return Math.floor((Date.parse(iso) - now.getTime()) / 86_400_000)
}

export type AttentionKind = 'cert_failed' | 'domain_failed' | 'cert_expiring' | 'domain_dns' | 'empty_site' | 'disk_full'

export interface Attention {
  kind: AttentionKind
  /** Сайт, к которому относится пункт; у «диск заполнен» его нет. */
  site?: Site
  /** Имя домена (или адрес сайта) для пунктов про домены и сертификаты. */
  host?: string
  /** Сколько суток осталось у сертификата (для «сертификат скоро истекает»). */
  days?: number
  /** Раздел кабинета сайта, где проблема решается. */
  route: 'site-overview' | 'site-domains' | 'site-ssl' | 'sites'
}

/** Что требует действий пользователя, от срочного к менее срочному. */
export function attentionItems(sites: readonly Site[], usedBytes: number, quotaBytes: number, now: Date = new Date()): Attention[] {
  const items: Attention[] = []
  for (const s of sites) {
    if (s.cert_status === 'failed') items.push({ kind: 'cert_failed', site: s, route: 'site-overview' })
  }
  for (const s of sites) {
    for (const d of s.domains) {
      if (d.status === 'failed') items.push({ kind: 'domain_failed', site: s, host: d.host, route: 'site-domains' })
    }
  }
  // Работающий сертификат подходит к концу — значит, автопродление не сработало (лимит, DNS, сбой выпускателя).
  for (const s of sites) {
    const names = [
      ...(s.cert_status === 'active' && s.cert ? [{ host: s.host, cert: s.cert }] : []),
      ...s.domains.filter((d) => d.status === 'active' && d.cert).map((d) => ({ host: d.host, cert: d.cert! })),
    ]
    for (const n of names) {
      const days = daysUntil(n.cert.not_after, now)
      if (days <= CERT_WARN_DAYS) items.push({ kind: 'cert_expiring', site: s, host: n.host, days, route: 'site-ssl' })
    }
  }
  if (quotaBytes > 0 && (usedBytes / quotaBytes) * 100 >= DISK_WARN_PERCENT) items.push({ kind: 'disk_full', route: 'sites' })
  for (const s of sites) {
    for (const d of s.domains) {
      if (d.status === 'pending_dns') items.push({ kind: 'domain_dns', site: s, host: d.host, route: 'site-domains' })
    }
  }
  for (const s of sites) {
    if (s.status === 'empty') items.push({ kind: 'empty_site', site: s, route: 'site-overview' })
  }
  return items
}

export type EventKind = 'site_created' | 'site_deployed' | 'domain_added' | 'domain_active'

export interface ActivityEvent {
  kind: EventKind
  at: string
  site: Site
  host?: string
}

/**
 * Последние события по меткам времени сайтов и доменов, новые сверху.
 * Это не журнал действий: удаления и входы здесь не видны, только то, что сохранено в самих записях.
 */
export function recentEvents(sites: readonly Site[], limit = 8): ActivityEvent[] {
  const events: ActivityEvent[] = []
  for (const s of sites) {
    events.push({ kind: 'site_created', at: s.created_at, site: s })
    if (s.deployed_at) events.push({ kind: 'site_deployed', at: s.deployed_at, site: s })
    for (const d of s.domains) {
      events.push({ kind: 'domain_added', at: d.created_at, site: s, host: d.host })
      if (d.verified_at) events.push({ kind: 'domain_active', at: d.verified_at, site: s, host: d.host })
    }
  }
  return events.sort((a, b) => Date.parse(b.at) - Date.parse(a.at)).slice(0, limit)
}
