import { z } from 'zod'
import type { MessageKey } from '@/i18n'

// Тексты проверок форм — ключи каталога i18n (тип не даст указать несуществующий).
const key = (k: MessageKey): MessageKey => k

export const userSchema = z.object({
  id: z.number(),
  email: z.string(),
  username: z.string(),
  role: z.enum(['admin', 'user']),
  created_at: z.string(),
})
export type User = z.infer<typeof userSchema>

export const sessionSchema = z.object({
  access_token: z.string(),
  expires_in: z.number(),
  user: userSchema,
})

export const meSchema = z.object({ user: userSchema })

export const inviteSchema = z.object({
  id: z.number(),
  code: z.string(),
  used_by: z.number().nullable(),
  used_by_username: z.string().nullable().optional(),
  used_at: z.string().nullable(),
  expires_at: z.string(),
  created_at: z.string(),
})
export type Invite = z.infer<typeof inviteSchema>

export const invitesSchema = z.object({ invites: z.array(inviteSchema) })
export const createdInviteSchema = z.object({ invite: inviteSchema })

export const domainSchema = z.object({
  id: z.number(),
  host: z.string(),
  status: z.enum(['pending_dns', 'pending_cert', 'active', 'failed']),
  problem: z.string(), // '' | no_a | wrong_ip | has_aaaa | lookup
  found: z.array(z.string()),
  error: z.string(),
  verified_at: z.string().nullable(),
  created_at: z.string(),
})
export type Domain = z.infer<typeof domainSchema>
export const domainResponseSchema = z.object({ domain: domainSchema })

export const siteSchema = z.object({
  id: z.number(),
  slug: z.string(),
  host: z.string(),
  url: z.string(),
  disk_bytes: z.number(),
  status: z.enum(['empty', 'live']),
  deployed_at: z.string().nullable(),
  created_at: z.string(),
  // none — выпуск сертификатов выключен (локальная разработка).
  cert_status: z.enum(['none', 'pending', 'active', 'failed']),
  cert_error: z.string(),
  domains: z.array(domainSchema),
  ftp: z.object({
    available: z.boolean(), // FTP включён на сервере
    allow_plain: z.boolean(), // принимается ли и обычный FTP без шифрования
    enabled: z.boolean(), // у сайта выдан доступ
    host: z.string().optional(),
    port: z.number().optional(),
    username: z.string().optional(),
  }),
})
export type Site = z.infer<typeof siteSchema>

export const sitesSchema = z.object({
  sites: z.array(siteSchema),
  limits: z.object({ max_sites: z.number(), disk_quota_bytes: z.number() }),
  domain_config: z.object({ available: z.boolean(), server_ips: z.array(z.string()), per_site: z.number() }),
})
export const siteResponseSchema = z.object({ site: siteSchema })
// Ответ выдачи FTP: пароль есть только здесь и показывается один раз.
export const ftpGrantSchema = z.object({ site: siteSchema, password: z.string() })

export const domainForm = z.object({
  host: z
    .string()
    .trim()
    .toLowerCase()
    .min(1, key('sites.domains.required'))
    .regex(/^([a-z0-9-]{1,63}\.)+[a-z0-9-]{2,63}$/, key('sites.domains.invalid')),
})

export const siteForm = z.object({
  slug: z
    .string()
    .trim()
    .toLowerCase()
    .regex(/^[a-z0-9][a-z0-9-]{0,30}[a-z0-9]$/, key('validation.slug'))
    .refine((v) => !v.includes('--'), key('validation.slug')),
})

export const apiErrorSchema = z.object({
  error: z.object({ code: z.string(), message: z.string(), field: z.string().optional() }),
})

// Проверки форм повторяют серверные правила, чтобы ошибки были видны до отправки.
// Источник истины — сервер (backend/internal/auth/validate.go). Сообщения — ключи каталога i18n:
// в интерфейсе их переводит resolveMessage, поэтому язык меняется без пересоздания схем.
export const loginForm = z.object({
  login: z.string().trim().min(1, key('validation.identityRequired')),
  password: z.string().min(1, key('validation.passwordRequired')),
})

export const registerForm = z.object({
  invite: z.string().trim().min(1, key('validation.inviteRequired')),
  email: z.string().trim().pipe(z.email(key('validation.email'))),
  username: z
    .string()
    .trim()
    .toLowerCase()
    .regex(/^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$/, key('validation.usernameFormat'))
    .refine((v) => !v.includes('--'), key('validation.usernameDashes')),
  password: z
    .string()
    .min(8, key('validation.passwordShort'))
    .refine((v) => new TextEncoder().encode(v).length <= 72, key('validation.passwordLong')),
})

export const passwordForm = z
  .object({
    current: z.string().min(1, key('settings.password.currentRequired')),
    next: z
      .string()
      .min(8, key('validation.passwordShort'))
      .refine((v) => new TextEncoder().encode(v).length <= 72, key('validation.passwordLong')),
    repeat: z.string(),
  })
  .refine((v) => v.next === v.repeat, { path: ['repeat'], message: key('settings.password.mismatch') })
  .refine((v) => v.next !== v.current, { path: ['next'], message: key('settings.password.same') })

/** Первая ошибка по каждому полю формы. */
export function fieldErrors(err: z.ZodError): Record<string, string> {
  const out: Record<string, string> = {}
  for (const issue of err.issues) {
    const key = String(issue.path[0] ?? '')
    if (key && !(key in out)) out[key] = issue.message
  }
  return out
}
