import { describe, expect, it } from 'vitest'
import type { Domain, Site } from '@/api/schemas'
import { attentionItems, recentEvents } from './summary'

const domain = (over: Partial<Domain>): Domain => ({
  id: 1,
  host: 'example.com',
  status: 'active',
  problem: '',
  found: [],
  error: '',
  verified_at: null,
  created_at: '2026-01-01T00:00:00Z',
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
  domains: [],
  ftp: { available: true, allow_plain: false, enabled: false },
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
