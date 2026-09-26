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
  email_verified_at: z.string().nullable(), // null — адрес почты ещё не подтверждён
  lang: z.enum(['ru', 'it']), // язык писем
  notify_email: z.boolean(), // писать ли о проблемах (сертификат, диск)
})
export type User = z.infer<typeof userSchema>

export const sessionSchema = z.object({
  access_token: z.string(),
  expires_in: z.number(),
  user: userSchema,
  mail_enabled: z.boolean().default(false), // на сервере настроена отправка почты
  databases_enabled: z.boolean().default(false), // включён раздел «Базы данных»
  shell_enabled: z.boolean().default(false), // включены SSH-ключи и веб-терминал
  mailhost_enabled: z.boolean().default(false), // включена почта на своих доменах
  dns_enabled: z.boolean().default(false), // включён собственный DNS (ns.vladinc.ru)
})

export const meSchema = z.object({ user: userSchema, mail_enabled: z.boolean().default(false), databases_enabled: z.boolean().default(false), shell_enabled: z.boolean().default(false), mailhost_enabled: z.boolean().default(false), dns_enabled: z.boolean().default(false) })

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

// Сведения о выпущенном сертификате; null, пока сертификата нет (или он выпущен до появления сведений).
export const certInfoSchema = z
  .object({
    issuer: z.string(),
    not_before: z.string(),
    not_after: z.string(),
    names: z.array(z.string()),
  })
  .nullable()
export type CertInfo = NonNullable<z.infer<typeof certInfoSchema>>

export const domainSchema = z.object({
  id: z.number(),
  host: z.string(),
  kind: z.enum(['custom', 'sub']), // custom — свой домен (A-запись), sub — поддомен сайта на нашем домене
  dir: z.string(), // папка сайта, которую отдаёт это имя; '' — весь сайт
  status: z.enum(['pending_dns', 'pending_cert', 'active', 'failed']),
  problem: z.string(), // '' | no_a | wrong_ip | has_aaaa | lookup
  found: z.array(z.string()),
  error: z.string(),
  verified_at: z.string().nullable(),
  created_at: z.string(),
  cert: certInfoSchema,
  cert_renew_at: z.string().nullable(), // когда можно перевыпустить вручную; null — сейчас
})
export type Domain = z.infer<typeof domainSchema>
export const domainResponseSchema = z.object({ domain: domainSchema })

// Дополнительный FTP-аккаунт сайта. Пароль в списках не приходит: он есть только в ответе на выдачу.
export const ftpAccountSchema = z.object({
  id: z.number(),
  name: z.string(),
  username: z.string(), // полный логин для FTP-клиента
  dir: z.string(), // папка сайта, которой ограничен аккаунт; '' — весь сайт
  read_only: z.boolean(),
  enabled: z.boolean(),
  last_login_at: z.string().nullable(),
  created_at: z.string(),
})
export type FtpAccount = z.infer<typeof ftpAccountSchema>
export const ftpAccountResponseSchema = z.object({ account: ftpAccountSchema })
export const ftpAccountGrantSchema = z.object({ account: ftpAccountSchema, password: z.string() })

export const ftpAccountForm = z.object({
  name: z
    .string()
    .trim()
    .toLowerCase()
    .regex(/^[a-z0-9]([a-z0-9-]{0,22}[a-z0-9])?$/, key('ftpAccounts.nameInvalid')),
})

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
  cert: certInfoSchema,
  cert_renew_at: z.string().nullable(),
  domains: z.array(domainSchema),
  ftp: z.object({
    available: z.boolean(), // FTP включён на сервере
    allow_plain: z.boolean(), // принимается ли и обычный FTP без шифрования
    enabled: z.boolean(), // у сайта выдан доступ
    host: z.string().optional(),
    port: z.number().optional(),
    username: z.string().optional(),
    accounts: z.array(ftpAccountSchema), // в ответах на одиночные действия пуст: полный список отдаёт GET /sites
    accounts_limit: z.number(),
  }),
})
export type Site = z.infer<typeof siteSchema>

export const sitesSchema = z.object({
  sites: z.array(siteSchema),
  limits: z.object({ max_sites: z.number(), disk_quota_bytes: z.number() }),
  domain_config: z.object({ available: z.boolean(), server_ips: z.array(z.string()), per_site: z.number(), per_site_sub: z.number() }),
  logs_available: z.boolean(),
  backups_available: z.boolean(),
  runtime_available: z.boolean().default(false), // на сервере установлена хотя бы одна среда выполнения
  cms_available: z.boolean().default(false), // доступна установка приложений (WordPress)
  shell_available: z.boolean().default(false), // доступны SSH и веб-терминал
})

// Ежедневный снимок файлов сайта.
export const backupSchema = z.object({
  id: z.number(),
  day: z.string(), // YYYY-MM-DD
  bytes: z.number(),
  files: z.number(),
  taken_at: z.string(),
})
export type Backup = z.infer<typeof backupSchema>
export const backupsSchema = z.object({ backups: z.array(backupSchema) })
export const backupResponseSchema = z.object({ backup: backupSchema })

export const logKinds = ['access', 'error'] as const
export type LogKind = (typeof logKinds)[number]
export const logStatusClasses = ['', '2xx', '3xx', '4xx', '5xx'] as const
export type LogStatusClass = (typeof logStatusClasses)[number]

// Событие журнала: у доступа заполнены m/s/b/ms, у ошибок — code/d.
export const logEntrySchema = z.object({
  t: z.string(),
  ip: z.string(),
  host: z.string(),
  m: z.string().optional(),
  p: z.string(),
  s: z.number().optional(),
  b: z.number().optional(),
  ms: z.number().optional(),
  ref: z.string().optional(),
  ua: z.string().optional(),
  code: z.string().optional(),
  d: z.string().optional(),
})
export type LogEntry = z.infer<typeof logEntrySchema>
export const logPageSchema = z.object({ entries: z.array(logEntrySchema), has_more: z.boolean() })
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

export const subdomainForm = z.object({
  label: z
    .string()
    .trim()
    .toLowerCase()
    .regex(/^[a-z0-9]([a-z0-9-]{0,30}[a-z0-9])?$/, key('subdomains.labelInvalid'))
    .refine((v) => !v.includes('--'), key('subdomains.labelInvalid')),
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

export const forgotForm = z.object({
  email: z.string().trim().min(1, key('auth.forgot.emailRequired')).email(key('validation.email')),
})

export const resetForm = z
  .object({
    password: z
      .string()
      .min(8, key('validation.passwordShort'))
      .refine((v) => new TextEncoder().encode(v).length <= 72, key('validation.passwordLong')),
    repeat: z.string(),
  })
  .refine((v) => v.password === v.repeat, { path: ['repeat'], message: key('settings.password.mismatch') })

/** Первая ошибка по каждому полю формы. */
export function fieldErrors(err: z.ZodError): Record<string, string> {
  const out: Record<string, string> = {}
  for (const issue of err.issues) {
    const key = String(issue.path[0] ?? '')
    if (key && !(key in out)) out[key] = issue.message
  }
  return out
}

export const statsPeriods = [7, 30, 90] as const
export type StatsPeriod = (typeof statsPeriods)[number]

const statsPointSchema = z.object({
  date: z.string(), // YYYY-MM-DD по UTC
  hits: z.number(),
  pages: z.number(),
  visitors: z.number(),
  bots: z.number(),
  bytes: z.number(),
  s2: z.number(),
  s3: z.number(),
  s4: z.number(),
  s5: z.number(),
})
export type StatsPoint = z.infer<typeof statsPointSchema>
const statsItemSchema = z.object({ key: z.string(), count: z.number() })
export type StatsItem = z.infer<typeof statsItemSchema>
export const statsSchema = z.object({
  days: z.array(statsPointSchema),
  total: statsPointSchema.omit({ date: true }),
  top_pages: z.array(statsItemSchema),
  top_refs: z.array(statsItemSchema),
})
export type Stats = z.infer<typeof statsSchema>

// Настройки сайта, которые применяет веб-шлюз (корневая папка, индексные файлы, листинг, страницы ошибок, www, HSTS).
export const siteSettingsSchema = z.object({
  root_dir: z.string(),
  index: z.array(z.string()),
  autoindex: z.boolean(),
  error_pages: z.record(z.string(), z.string()), // код ответа → файл в корне сайта
  www: z.enum(['', 'add', 'remove']),
  hsts: z.boolean(),
})
export type SiteSettings = z.infer<typeof siteSettingsSchema>
export const siteSettingsResponseSchema = z.object({
  settings: siteSettingsSchema,
  default_index: z.array(z.string()),
  error_statuses: z.array(z.number()),
  max_index: z.number(),
})

// Базы данных пользователя (PostgreSQL и MariaDB).
export const dbEngines = ['postgres', 'mariadb'] as const
export type DbEngine = (typeof dbEngines)[number]

export const databaseSchema = z.object({
  id: z.number(),
  engine: z.enum(dbEngines),
  name: z.string(), // полное имя: {пользователь}_{имя}; оно же логин
  status: z.enum(['active', 'frozen']), // frozen — превышен лимит: чтение и удаление данных, запись отключена
  size_bytes: z.number(),
  size_checked_at: z.string().nullable(),
  frozen_at: z.string().nullable(),
  created_at: z.string(),
  addrs: z.array(z.string()), // адреса внешнего доступа
})
export type Database = z.infer<typeof databaseSchema>

export const dbInfoSchema = z.object({
  engines: z.array(z.enum(dbEngines)),
  per_engine: z.number(),
  size_limit: z.number(),
  host: z.string(),
  ports: z.record(z.string(), z.number()),
  external: z.record(z.string(), z.boolean()),
  web_client: z.boolean(),
  max_addrs: z.number(),
})
export type DbInfo = z.infer<typeof dbInfoSchema>
export const databasesSchema = z.object({ databases: z.array(databaseSchema), info: dbInfoSchema })
export const databaseResponseSchema = z.object({ database: databaseSchema })
// Пароль есть только в ответе на создание и на смену пароля.
export const databaseCreatedSchema = z.object({ database: databaseSchema, password: z.string() })
export const webClientSchema = z.object({ url: z.string() })

export const databaseForm = z.object({
  name: z.string().trim().toLowerCase().regex(/^[a-z0-9]{1,20}$/, key('databases.nameInvalid')),
})

// Планировщик задач.
export const cronKinds = ['http', 'command'] as const
export type CronKind = (typeof cronKinds)[number]
export const cronRunStatuses = ['running', 'ok', 'failed', 'timeout', 'skipped'] as const
export type CronRunStatus = (typeof cronRunStatuses)[number]

export const cronJobSchema = z.object({
  id: z.number(),
  name: z.string(),
  kind: z.enum(cronKinds),
  schedule: z.string(), // пять полей cron, по UTC
  url: z.string(),
  command: z.string(),
  site_id: z.number().nullable(),
  enabled: z.boolean(),
  next_run_at: z.string().nullable(),
  last_run_at: z.string().nullable(),
  last_status: z.string(),
  fail_streak: z.number(),
  created_at: z.string(),
})
export type CronJob = z.infer<typeof cronJobSchema>

export const cronInfoSchema = z.object({
  max_jobs: z.number(),
  min_interval_min: z.number(),
  timeout_sec: z.number(),
  keep_runs: z.number(),
  commands_enabled: z.boolean(),
})
export type CronInfo = z.infer<typeof cronInfoSchema>

export const cronListSchema = z.object({ jobs: z.array(cronJobSchema), info: cronInfoSchema })
export const cronJobResponseSchema = z.object({ job: cronJobSchema })

export const cronRunSchema = z.object({
  id: z.number(),
  job_id: z.number(),
  started_at: z.string(),
  finished_at: z.string().nullable(),
  status: z.enum(cronRunStatuses),
  code: z.number(),
  duration_ms: z.number(),
  reason: z.string(),
  output: z.string(),
})
export type CronRun = z.infer<typeof cronRunSchema>
export const cronRunsSchema = z.object({ runs: z.array(cronRunSchema) })
export const cronRunStartedSchema = z.object({ run: cronRunSchema })

export const cronForm = z.object({
  name: z.string().trim().min(1, key('cron.nameInvalid')).max(60, key('cron.nameInvalid')),
})

// Среда выполнения сайта: статика, PHP, Node.js, Python.
export const runtimeKinds = ['static', 'php', 'node', 'python'] as const
export type RuntimeKind = (typeof runtimeKinds)[number]

export const runtimeSchema = z.object({
  runtime: z.enum(runtimeKinds),
  version: z.string(),
  command: z.string(),
  port: z.number(),
  state: z.string(), // active, failed, inactive, unknown; у статики пусто
  caps: z.object({ php: z.array(z.string()), node: z.string(), python: z.string() }),
})
export type SiteRuntime = z.infer<typeof runtimeSchema>
export const runtimeResponseSchema = z.object({ runtime: runtimeSchema })
export const runtimeLogsSchema = z.object({ logs: z.string() })

// Установка приложений «в один клик» (WordPress).
export const cmsStatusSchema = z.object({
  available: z.boolean(),
  catalog: z.array(z.object({ id: z.string(), name: z.string(), version: z.string() })),
  locales: z.array(z.string()),
  installed: z
    .object({ cms: z.string(), name: z.string(), version: z.string(), db_name: z.string(), installed_at: z.string(), url: z.string(), admin_url: z.string() })
    .nullable(),
  requirements: z.object({ php: z.boolean(), database: z.boolean(), empty: z.boolean() }),
  job: z.unknown().optional(),
})
export type CmsStatus = z.infer<typeof cmsStatusSchema>
export const cmsStatusResponseSchema = z.object({ cms: cmsStatusSchema })

export const cmsJobSchema = z.object({
  status: z.enum(['running', 'done', 'failed']),
  step: z.string(),
  steps: z.array(z.string()),
  failure: z.object({ code: z.string(), message: z.string(), step: z.string(), detail: z.string().optional() }).optional(),
  result: z
    .object({ url: z.string(), admin_url: z.string(), admin_user: z.string(), admin_password: z.string(), db_name: z.string(), db_password: z.string() })
    .optional(),
})
export type CmsJob = z.infer<typeof cmsJobSchema>
export const cmsJobResponseSchema = z.object({ job: cmsJobSchema.nullable() })

export const cmsForm = z.object({
  title: z.string().trim().min(1, key('cms.titleInvalid')).max(80, key('cms.titleInvalid')),
  admin_user: z.string().trim().regex(/^[A-Za-z0-9][A-Za-z0-9_.@-]{2,59}$/, key('cms.adminUserInvalid')),
  admin_email: z.string().trim().pipe(z.email(key('validation.email'))),
})

// SSH и веб-терминал.
export const sshKeySchema = z.object({
  id: z.number(),
  name: z.string(),
  algorithm: z.string(),
  public_key: z.string(),
  fingerprint: z.string(),
  created_at: z.string(),
  last_used_at: z.string().nullable(),
})
export type SshKey = z.infer<typeof sshKeySchema>
export const sshKeysSchema = z.object({ keys: z.array(sshKeySchema), max_keys: z.number(), host_fingerprint: z.string() })
export const sshKeyResponseSchema = z.object({ key: sshKeySchema })
// Закрытая часть сгенерированной пары приходит один раз.
export const sshKeyGeneratedSchema = z.object({ key: sshKeySchema, private_key: z.string() })

export const shellStatusSchema = z.object({
  enabled: z.boolean(),
  enabled_at: z.string().nullable(),
  login: z.string(),
  host: z.string(),
  port: z.number(),
  host_fingerprint: z.string(),
  keys: z.number(),
})
export type ShellStatus = z.infer<typeof shellStatusSchema>
export const shellResponseSchema = z.object({ shell: shellStatusSchema })
export const terminalTicketSchema = z.object({ ticket: z.string() })

export const sshKeyForm = z.object({
  name: z.string().trim().min(1, key('ssh.nameInvalid')).max(60, key('ssh.nameInvalid')),
  public_key: z.string().trim().min(1, key('ssh.publicKeyRequired')),
})

// Почта на своих доменах.
export const mailboxSchema = z.object({
  id: z.number(),
  domain_id: z.number(),
  local: z.string(),
  quota_mb: z.number(),
  enabled: z.boolean(),
  used_bytes: z.number().default(0),
  created_at: z.string(),
  autoreply: z.object({ enabled: z.boolean(), subject: z.string(), body: z.string(), from: z.string(), to: z.string(), days: z.number() }),
  forward: z.object({ to: z.array(z.string()), keep_copy: z.boolean() }),
})
export type Mailbox = z.infer<typeof mailboxSchema>
export const mailAliasSchema = z.object({ id: z.number(), domain_id: z.number(), local: z.string(), to: z.array(z.string()), created_at: z.string() })
export type MailAlias = z.infer<typeof mailAliasSchema>
export const mailDomainSchema = z.object({
  id: z.number(),
  domain: z.string(),
  enabled: z.boolean(),
  dkim_selector: z.string(),
  created_at: z.string(),
  mailboxes: z.array(mailboxSchema),
  aliases: z.array(mailAliasSchema),
})
export type MailDomain = z.infer<typeof mailDomainSchema>
export const mailInfoSchema = z.object({
  host: z.string(),
  imap_port: z.number(),
  pop3_port: z.number(),
  smtp_ports: z.array(z.number()),
  max_domains: z.number(),
  max_mailboxes: z.number(),
  max_aliases: z.number(),
  min_quota_mb: z.number(),
  max_quota_mb: z.number(),
  default_quota_mb: z.number(),
  min_password: z.number(),
  eligible_domains: z.array(z.string()).nullable().transform((v) => v ?? []),
  webmail_url: z.string().default(''),
})
export const mailOverviewSchema = z.object({ info: mailInfoSchema, domains: z.array(mailDomainSchema) })
export const mailDomainResponseSchema = z.object({ domain: z.object({ id: z.number(), domain: z.string() }) })
export const mailRecordSchema = z.object({
  kind: z.enum(['mx', 'spf', 'dkim', 'dmarc']),
  type: z.string(),
  name: z.string(),
  value: z.string(),
  state: z.enum(['ok', 'missing', 'mismatch']),
  detail: z.string().optional(),
})
export type MailRecord = z.infer<typeof mailRecordSchema>
export const mailAutoSchema = z.object({
  available: z.boolean(),
  zone: z.boolean(),
  delegated: z.boolean(),
  state: z.string(),
  found: z.array(z.string()),
  expected: z.array(z.string()),
})
export type MailAuto = z.infer<typeof mailAutoSchema>
export const mailRecordsSchema = z.object({ records: z.array(mailRecordSchema), auto: mailAutoSchema })
// Пароль сгенерированного ящика приходит один раз; пустая строка — пароль задал сам пользователь.
export const mailboxCreatedSchema = z.object({ mailbox: z.object({ id: z.number(), local: z.string() }), password: z.string() })
export const mailPasswordSchema = z.object({ password: z.string() })

export const mailDomainForm = z.object({
  domain: z.string().trim().toLowerCase().min(1, key('mailhost.domainRequired')),
})
export const mailboxForm = z.object({
  local: z.string().trim().toLowerCase().min(1, key('mailhost.localRequired')).max(64, key('mailhost.localRequired')),
  password: z.string().max(128, key('mailhost.passwordInvalid')),
  quota_mb: z.number({ error: key('mailhost.quotaInvalid') }).int(key('mailhost.quotaInvalid')).min(50, key('mailhost.quotaInvalid')).max(2000, key('mailhost.quotaInvalid')),
})
export const mailAliasForm = z.object({
  local: z.string().trim().toLowerCase().min(1, key('mailhost.localRequired')).max(64, key('mailhost.localRequired')),
  to: z.string().trim().min(1, key('mailhost.destRequired')),
})
// Проверяется только при включённом автоответчике.
export const mailAutoReplyForm = z.object({
  subject: z.string().trim().min(1, key('mailhost.rules.subjectInvalid')).max(200, key('mailhost.rules.subjectInvalid')),
  body: z.string().trim().min(1, key('mailhost.rules.bodyInvalid')).max(2000, key('mailhost.rules.bodyInvalid')),
  days: z.number({ error: key('mailhost.rules.daysInvalid') }).int(key('mailhost.rules.daysInvalid')).min(1, key('mailhost.rules.daysInvalid')).max(30, key('mailhost.rules.daysInvalid')),
})

export const mailJournalSchema = z.object({
  events: z.array(
    z.object({
      t: z.string(),
      kind: z.enum(['received', 'delivered', 'failed', 'deferred', 'rejected']),
      id: z.string(),
      from: z.string(),
      to: z.string(),
      host: z.string(),
      via: z.string(),
      detail: z.string(),
    }),
  ),
  queue: z.array(z.object({ id: z.string(), age: z.string(), size: z.string(), from: z.string(), to: z.array(z.string()), frozen: z.boolean() })),
})
export type MailJournal = z.infer<typeof mailJournalSchema>

// Собственный DNS.
export const dnsRecordSchema = z.object({
  id: z.number(),
  zone_id: z.number(),
  name: z.string(),
  type: z.string(),
  value: z.string(),
  priority: z.number(),
  ttl: z.number(),
  managed: z.string(),
  created_at: z.string(),
})
export type DnsRecord = z.infer<typeof dnsRecordSchema>
export const dnsZoneSchema = z.object({ id: z.number(), domain: z.string(), created_at: z.string(), records: z.array(dnsRecordSchema) })
export type DnsZone = z.infer<typeof dnsZoneSchema>
export const dnsOverviewSchema = z.object({
  info: z.object({
    ns: z.array(z.string()),
    server_ips: z.array(z.string()),
    max_zones: z.number(),
    max_records: z.number(),
    types: z.array(z.string()),
    ttls: z.array(z.number()),
    eligible_domains: z.array(z.string()).nullable().transform((v) => v ?? []),
  }),
  zones: z.array(dnsZoneSchema),
})
export const dnsDelegationSchema = z.object({
  delegation: z.object({ state: z.enum(['ok', 'partial', 'mixed', 'none', 'unknown']), found: z.array(z.string()), expected: z.array(z.string()) }),
})
export type DnsDelegation = z.infer<typeof dnsDelegationSchema>['delegation']
export const dnsZoneResponseSchema = z.object({ zone: z.object({ id: z.number(), domain: z.string() }) })
export const dnsRecordResponseSchema = z.object({ record: dnsRecordSchema })

export const dnsDomainForm = z.object({ domain: z.string().trim().toLowerCase().min(1, key('dns.domainRequired')) })
export const dnsRecordForm = z.object({
  name: z.string().trim().toLowerCase(),
  type: z.string().min(1, key('dns.typeRequired')),
  value: z.string().trim().min(1, key('dns.valueRequired')).max(2048, key('dns.valueRequired')),
  priority: z.number({ error: key('dns.priorityInvalid') }).int(key('dns.priorityInvalid')).min(0, key('dns.priorityInvalid')).max(65535, key('dns.priorityInvalid')),
  ttl: z.number({ error: key('dns.ttlInvalid') }),
})

// Журнал действий аккаунта.
export const activityEventSchema = z.object({
  id: z.number(),
  kind: z.string(),
  category: z.string(),
  target: z.string(),
  count: z.number(),
  ip: z.string(),
  user_agent: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
})
export type ActivityEvent = z.infer<typeof activityEventSchema>
export const activityListSchema = z.object({ events: z.array(activityEventSchema), next: z.number(), categories: z.array(z.string()) })

// Обращения в поддержку.
export const ticketStatusSchema = z.enum(['open', 'answered', 'closed'])
export type TicketStatus = z.infer<typeof ticketStatusSchema>
export const ticketItemSchema = z.object({
  id: z.number(),
  subject: z.string(),
  category: z.string(),
  status: ticketStatusSchema,
  created_at: z.string(),
  updated_at: z.string(),
  closed_at: z.string().nullable(),
  username: z.string(),
  messages: z.number(),
})
export type TicketItem = z.infer<typeof ticketItemSchema>
export const ticketListSchema = z.object({
  tickets: z.array(ticketItemSchema),
  categories: z.array(z.string()),
  max_open: z.number(),
  max_subject: z.number(),
  max_body: z.number(),
})
export const ticketQueueSchema = z.object({ tickets: z.array(ticketItemSchema) })
export const ticketViewSchema = z.object({
  ticket: ticketItemSchema.extend({
    email: z.string().optional(),
    can_reply: z.boolean(),
    thread: z.array(z.object({ id: z.number(), staff: z.boolean(), author: z.string(), body: z.string(), created_at: z.string() })),
  }),
})
export type TicketView = z.infer<typeof ticketViewSchema>['ticket']
export const ticketCreatedSchema = z.object({ ticket: z.object({ id: z.number() }) })
export const ticketSummarySchema = z.object({ waiting: z.number() })

export const ticketForm = z.object({
  subject: z.string().trim().min(3, key('support.subjectInvalid')).max(120, key('support.subjectInvalid')),
  category: z.string().min(1, key('support.categoryRequired')),
  message: z.string().trim().min(1, key('support.messageRequired')).max(5000, key('support.messageTooLong')),
})
export const ticketReplyForm = z.object({
  message: z.string().trim().min(1, key('support.messageRequired')).max(5000, key('support.messageTooLong')),
})
