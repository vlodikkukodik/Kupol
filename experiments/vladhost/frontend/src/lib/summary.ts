import type { Site } from '@/api/schemas'

// Данные для «Сводки» аккаунта, вычисляемые из уже загруженных сайтов (отдельных запросов не нужно).

/** Порог заполнения диска, после которого сводка предупреждает. */
export const DISK_WARN_PERCENT = 80

export type AttentionKind = 'cert_failed' | 'domain_failed' | 'domain_dns' | 'empty_site' | 'disk_full'

export interface Attention {
  kind: AttentionKind
  /** Сайт, к которому относится пункт; у «диск заполнен» его нет. */
  site?: Site
  /** Имя домена для пунктов про домены. */
  host?: string
  /** Раздел кабинета сайта, где проблема решается. */
  route: 'site-overview' | 'site-domains' | 'sites'
}

/** Что требует действий пользователя, от срочного к менее срочному. */
export function attentionItems(sites: readonly Site[], usedBytes: number, quotaBytes: number): Attention[] {
  const items: Attention[] = []
  for (const s of sites) {
    if (s.cert_status === 'failed') items.push({ kind: 'cert_failed', site: s, route: 'site-overview' })
  }
  for (const s of sites) {
    for (const d of s.domains) {
      if (d.status === 'failed') items.push({ kind: 'domain_failed', site: s, host: d.host, route: 'site-domains' })
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
