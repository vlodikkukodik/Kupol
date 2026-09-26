import { describe, expect, it } from 'vitest'
import type { Domain, Site } from '@/api/schemas'
import { attentionItems, CERT_WARN_DAYS, daysUntil, recentEvents } from './summary'

const domain = (over: Partial<Domain>): Domain => ({
  id: 1,
  host: 'example.com',
  kind: 'custom',
  dir: '',
  status: 'active',
  problem: '',
  found: [],
  error: '',
  verified_at: null,
  created_at: '2026-01-01T00:00:00Z',
  cert: null,
  cert_renew_at: null,
  ...over,
})

const site = (over: Partial<Site>): Site => ({
  id: 1,
  slug: 'blog',
  host: 'blog.u.vladinc.ru',
  url: 'https://blog.u.vladinc.ru',
  disk_bytes: 0,
  status: 'live',
  deployed_at: null,
  created_at: '2026-01-01T00:00:00Z',
  cert_status: 'active',
  cert_error: '',
  cert: null,
  cert_renew_at: null,
  domains: [],
  ftp: { available: true, allow_plain: false, enabled: false, accounts: [], accounts_limit: 5 },
  ...over,
})

describe('attentionItems', () => {
  it('здоровый аккаунт ничего не требует', () => {
    expect(attentionItems([site({})], 10, 1000)).toEqual([])
  })

  it('порядок: сертификат, домен с ошибкой, диск, ожидание DNS, пустой сайт', () => {
    const s = site({
      status: 'empty',
      cert_status: 'failed',
      domains: [domain({ id: 1, host: 'a.com', status: 'pending_dns' }), domain({ id: 2, host: 'b.com', status: 'failed' })],
    })
    expect(attentionItems([s], 900, 1000).map((i) => i.kind)).toEqual([
      'cert_failed',
      'domain_failed',
      'disk_full',
      'domain_dns',
      'empty_site',
    ])
  })

  it('диск предупреждает ровно с порога и не делит на ноль при неограниченной квоте', () => {
    expect(attentionItems([], 799, 1000)).toEqual([])
    expect(attentionItems([], 800, 1000).map((i) => i.kind)).toEqual(['disk_full'])
    expect(attentionItems([], 999, 0)).toEqual([])
  })

  it('пункты про домены знают имя домена и ведут в раздел доменов', () => {
    const s = site({ domains: [domain({ host: 'x.org', status: 'failed' })] })
    const [item] = attentionItems([s], 0, 1000)
    expect(item).toMatchObject({ kind: 'domain_failed', host: 'x.org', route: 'site-domains' })
  })
})

describe('сертификат подходит к концу', () => {
  const now = new Date('2026-09-24T12:00:00Z')
  const cert = (days: number) => ({
    issuer: "Let's Encrypt",
    not_before: '2026-06-01T00:00:00Z',
    not_after: new Date(now.getTime() + days * 86_400_000).toISOString(),
    names: [],
  })

  it('daysUntil считает целые сутки вниз, в прошлом — отрицательные', () => {
    expect(daysUntil('2026-09-25T12:00:00Z', now)).toBe(1)
    expect(daysUntil('2026-09-25T11:00:00Z', now)).toBe(0)
    expect(daysUntil('2026-09-24T11:00:00Z', now)).toBe(-1)
    expect(daysUntil('2026-08-25T12:00:00Z', now)).toBe(-30)
  })

  it('предупреждает с порога и не раньше', () => {
    expect(attentionItems([site({ cert: cert(CERT_WARN_DAYS) })], 0, 1000, now).map((i) => i.kind)).toEqual(['cert_expiring'])
    expect(attentionItems([site({ cert: cert(CERT_WARN_DAYS + 1) })], 0, 1000, now)).toEqual([])
    expect(attentionItems([site({ cert: cert(60) })], 0, 1000, now)).toEqual([])
  })

  it('истёкший сертификат тоже виден, с отрицательным числом суток', () => {
    const [item] = attentionItems([site({ cert: cert(-3) })], 0, 1000, now)
    expect(item).toMatchObject({ kind: 'cert_expiring', host: 'blog.u.vladinc.ru', route: 'site-ssl' })
    expect(item?.days).toBeLessThan(0)
  })

  it('смотрит и на сертификаты доменов, но только у работающих', () => {
    const s = site({
      cert: cert(90),
      domains: [
        domain({ id: 1, host: 'a.com', status: 'active', cert: cert(3) }),
        domain({ id: 2, host: 'b.com', status: 'pending_cert', cert: cert(3) }),
        domain({ id: 3, host: 'c.com', status: 'active', cert: null }),
      ],
    })
    const items = attentionItems([s], 0, 1000, now)
    expect(items.map((i) => i.host)).toEqual(['a.com'])
  })

  it('неработающий сертификат сайта не считается «скоро истекающим»', () => {
    expect(attentionItems([site({ cert_status: 'pending', cert: cert(1) })], 0, 1000, now)).toEqual([])
  })

  it('стоит после ошибок выпуска и перед диском', () => {
    const s = site({ cert_status: 'active', cert: cert(2), domains: [domain({ id: 1, host: 'x.com', status: 'failed' })] })
    expect(attentionItems([s], 900, 1000, now).map((i) => i.kind)).toEqual(['domain_failed', 'cert_expiring', 'disk_full'])
  })
})

describe('recentEvents', () => {
  it('собирает события и сортирует от новых к старым', () => {
    const s = site({
      created_at: '2026-01-01T00:00:00Z',
      deployed_at: '2026-01-03T00:00:00Z',
      domains: [domain({ created_at: '2026-01-02T00:00:00Z', verified_at: '2026-01-04T00:00:00Z' })],
    })
    expect(recentEvents([s]).map((e) => e.kind)).toEqual(['domain_active', 'site_deployed', 'domain_added', 'site_created'])
  })

  it('не выдумывает события: без деплоя и проверки домена их нет', () => {
    const s = site({ domains: [domain({ verified_at: null })] })
    expect(recentEvents([s]).map((e) => e.kind).sort()).toEqual(['domain_added', 'site_created'])
  })

  it('ограничивает число событий и смешивает разные сайты', () => {
    const a = site({ id: 1, created_at: '2026-01-01T00:00:00Z' })
    const b = site({ id: 2, created_at: '2026-02-01T00:00:00Z', deployed_at: '2026-03-01T00:00:00Z' })
    const list = recentEvents([a, b], 2)
    expect(list).toHaveLength(2)
    expect(list[0]).toMatchObject({ kind: 'site_deployed', site: { id: 2 } })
  })
})
