import { z } from 'zod'

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
  ftp: z.object({
    available: z.boolean(), // FTP включён на сервере
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
})
export const siteResponseSchema = z.object({ site: siteSchema })
// Ответ выдачи FTP: пароль есть только здесь и показывается один раз.
export const ftpGrantSchema = z.object({ site: siteSchema, password: z.string() })

export const siteForm = z.object({
  slug: z
    .string()
    .trim()
    .toLowerCase()
    .regex(/^[a-z0-9][a-z0-9-]{0,30}[a-z0-9]$/, '2–32 символа: латиница, цифры и дефис, не с дефиса и не на дефис')
    .refine((v) => !v.includes('--'), 'Два дефиса подряд недопустимы'),
})

export const apiErrorSchema = z.object({
  error: z.object({ code: z.string(), message: z.string(), field: z.string().optional() }),
})

// Проверки форм повторяют серверные правила, чтобы ошибки видны до отправки.
// Источник истины — сервер (backend/internal/auth/validate.go).
export const loginForm = z.object({
  login: z.string().trim().min(1, 'Введите email или имя'),
  password: z.string().min(1, 'Введите пароль'),
})

export const registerForm = z.object({
  invite: z.string().trim().min(1, 'Введите код инвайта'),
  email: z.string().trim().pipe(z.email('Некорректный email')),
  username: z
    .string()
    .trim()
    .toLowerCase()
    .regex(/^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$/, '3–32 символа: латиница, цифры и дефис, не с дефиса и не на дефис')
    .refine((v) => !v.includes('--'), 'Два дефиса подряд недопустимы'),
  password: z
    .string()
    .min(8, 'Пароль короче 8 символов')
    .refine((v) => new TextEncoder().encode(v).length <= 72, 'Пароль длиннее 72 байт'),
})

/** Первая ошибка по каждому полю формы. */
export function fieldErrors(err: z.ZodError): Record<string, string> {
  const out: Record<string, string> = {}
  for (const issue of err.issues) {
    const key = String(issue.path[0] ?? '')
    if (key && !(key in out)) out[key] = issue.message
  }
  return out
}
