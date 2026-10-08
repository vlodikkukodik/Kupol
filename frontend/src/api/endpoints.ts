// Типизированные обращения к API: путь, параметры и форма ответа описаны в одном месте.
// Формы ответов — из generated/ (их порождает tygo из Go), поэтому расхождение с сервером ловит `make types-check`.
import { api } from './index'
import type { RequestOptions } from './client'
import type {
  Content,
  DocumentRatings,
  GraphResult,
  ImportResult,
  ListResult,
  Meta,
  PreviewResult,
  Rating,
  RedeemResult,
  RemarkOut,
  SearchResult,
  SecretCodeInput,
  TemplateContent,
  TemplateInput,
  TimelineInput,
  TermInput,
  Summary,
  TeamListResult,
  VersionsPage,
  SaveResult,
  AutosaveResult,
} from './generated/documents'
import type { Page as InboxPage } from './generated/inbox'
import type { Item as PetitionItem, Invitation } from './generated/petitions'
import type { Upload } from './generated/uploads'
import type { Item as SanctionItem, Input as SanctionInput } from './generated/sanctions'
import type {
  AchievementsResponse,
  UploadTicketResponse,
  UploadResponse,
  UploadsResponse,
  CreatePetitionRequest,
  DecidePetitionRequest,
  InvitationResponse,
  InvitationsResponse,
  InviteRequest,
  RespondInvitationRequest,
  UserCardResponse,
  PetitionResponse,
  SanctionResponse,
  SanctionsResponse,
  PetitionsResponse,
  InboxUnreadResponse,
  SendNoteRequest,
  SendNoteResponse,
  AddCommentRequest,
  CaptchaResponse,
  ChangePasswordRequest,
  CommentResponse,
  ConfirmEmailRequest,
  CreateRemarkRequest,
  CreateDocumentRequest,
  CreateSuggestionRequest,
  DashboardResponse,
  DiffResponse,
  DocumentResponse,
  GlossaryResponse,
  HealthResponse,
  LintResponse,
  LockResponse,
  LoginRequest,
  LoginResponse,
  MemberRoleResponse,
  PreviewRequest,
  RatingsResponse,
  RecentResponse,
  RedeemCodeRequest,
  RegisterRequest,
  RegisterResponse,
  RemarkResponse,
  RemarksResponse,
  RestoreRequest,
  RestoreResponse,
  ResolveCommentRequest,
  ReviewResponse,
  SaveDocumentRequest,
  SecretCodeResponse,
  SecretCodesResponse,
  SessionResponse,
  SetEmailRequest,
  SetRatingRequest,
  SiteResponse,
  SiteSettingsResponse,
  SubmitRequest,
  SuggestionResponse,
  SuggestionStatusRequest,
  SuggestionsListResponse,
  SuggestionsResponse,
  TeamDocumentResponse,
  TemplateResponse,
  TemplatesResponse,
  TeamMembersResponse,
  TeamRolesResponse,
  TermResponse,
  TimelineEventResponse,
  TimelineEventsResponse,
  TimelineResponse,
  TOTPCodesResponse,
  TOTPSetupResponse,
  TOTPStatusResponse,
  VerdictRequest,
  VersionResponse,
} from './generated/httpapi'

type Params = Record<string, string | number | boolean | undefined | null> | URLSearchParams

/** Строка запроса: пустые значения отбрасываются. */
export function qs(params?: Params): string {
  if (!params) return ''
  const sp = params instanceof URLSearchParams ? params : new URLSearchParams()
  if (!(params instanceof URLSearchParams)) {
    for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== null && v !== '' && v !== false) sp.set(k, String(v))
  }
  const s = sp.toString()
  return s ? `?${s}` : ''
}

export const authApi = {
  session: (o?: RequestOptions) => api.get<SessionResponse>('/auth/session', o),
  captcha: (o?: RequestOptions) => api.get<CaptchaResponse>('/auth/captcha', o),
  login: (body: LoginRequest) => api.post<LoginResponse>('/auth/login', body),
  register: (body: RegisterRequest) => api.post<RegisterResponse>('/auth/register', body),
  restore: (body: RestoreRequest) => api.post<RestoreResponse>('/auth/restore', body),
  logout: () => api.post<null>('/auth/logout'),
  changePassword: (body: ChangePasswordRequest) => api.post<null>('/me/password', body),
  deleteAccount: (password: string) => api.delete<null>('/me', { password }),
  setEmail: (body: SetEmailRequest) => api.put<null>('/me/email', body),
  removeEmail: (password: string) => api.delete<null>('/me/email', { password }),
  /** Переход по ссылке из письма подтверждения; входа не требует. */
  confirmEmail: (body: ConfirmEmailRequest) => api.post<null>('/auth/email/confirm', body),
  /** Грамоты пользователя (шаг 5.6), для личного дела */
  achievements: (o?: RequestOptions) => api.get<AchievementsResponse>('/me/achievements', o).then((r) => r.items),
}

/** Внутренняя почта (шаг 5.7): ящик читателя; записки Директората шлёт teamApi.sendNote. */
export const inboxApi = {
  list: (page: number, o?: RequestOptions) => api.get<InboxPage>(`/me/inbox${qs({ page: page > 1 ? page : undefined })}`, o),
  unread: (o?: RequestOptions) => api.get<InboxUnreadResponse>('/me/inbox/unread', o).then((r) => r.unread),
  markRead: (id: number) => api.post<null>(`/me/inbox/${id}/read`),
  markAllRead: () => api.post<null>('/me/inbox/read-all'),
}

/** Загрузки (этап 6.1): картинки и аудио. Файл уходит по одноразовому билету на отдельный путь. */
export const uploadsApi = {
  list: (o?: RequestOptions) => api.get<UploadsResponse>('/team/uploads', o).then((r) => r.items as Upload[]),
  /** Билет и загрузка одним действием; level — минимальный допуск читателя (0–6) */
  async upload(file: File, level: number): Promise<Upload> {
    const t = await api.post<UploadTicketResponse>('/team/uploads/ticket')
    const form = new FormData()
    form.append('level', String(level))
    form.append('file', file)
    const r = await api.post<UploadResponse>(t.path.replace(/^\/api/, ''), form)
    return r.upload as Upload
  },
  remove: (id: number) => api.delete<null>(`/team/uploads/${id}`),
}

/** Карточка пользователя (клик по нику, только вошедшим): ник, уровень, звание, грамоты */
export const usersApi = {
  card: (login: string, o?: RequestOptions) => api.get<UserCardResponse>(`/users/${encodeURIComponent(login)}`, o).then((r) => r.card),
}

/** Ходатайства о допуске 4–6 (шаг 5.8): подаёт вошедший; очередь и решение — Особый Совет и Директорат. */
export const petitionsApi = {
  mine: (o?: RequestOptions) => api.get<PetitionsResponse>('/petitions', o).then((r) => r.items as PetitionItem[]),
  create: (text: string) => api.post<PetitionResponse>('/petitions', { text } satisfies CreatePetitionRequest).then((r) => r.petition as PetitionItem),
  queue: (o?: RequestOptions) => api.get<PetitionsResponse>('/team/petitions', o).then((r) => r.items as PetitionItem[]),
  /** Приглашения Совета на следующий уровень: читатель принимает или отклоняет; Совет приглашает и отзывает */
  invitations: (o?: RequestOptions) => api.get<InvitationsResponse>('/invitations', o).then((r) => r.items as Invitation[]),
  respond: (id: number, answer: RespondInvitationRequest['answer']) => api.post<InvitationResponse>(`/invitations/${id}/respond`, { answer }).then((r) => r.invitation as Invitation),
  councilInvitations: (o?: RequestOptions) => api.get<InvitationsResponse>('/team/invitations', o).then((r) => r.items as Invitation[]),
  invite: (body: InviteRequest) => api.post<InvitationResponse>('/team/invitations', body).then((r) => r.invitation as Invitation),
  withdraw: (id: number) => api.post<InvitationResponse>(`/team/invitations/${id}/withdraw`).then((r) => r.invitation as Invitation),
  decide: (id: number, body: DecidePetitionRequest) => api.post<PetitionResponse>(`/team/petitions/${id}/decision`, body).then((r) => r.petition as PetitionItem),
}

/** Код из приложения (TOTP) в личном деле: включается по желанию, все действия подтверждаются паролем. */
export const totpApi = {
  status: (o?: RequestOptions) => api.get<TOTPStatusResponse>('/me/totp', o),
  /** Начать подключение: секрет и ссылка для QR. Защита включится только первым верным кодом (enable). */
  setup: (password: string) => api.post<TOTPSetupResponse>('/me/totp/setup', { password }),
  /** Подтвердить подключение первым кодом; в ответ — одноразовые коды (показываются один раз). */
  enable: (code: string) => api.post<TOTPCodesResponse>('/me/totp/enable', { code }),
  disable: (password: string, code: string) => api.post<null>('/me/totp/disable', { password, code }),
  renewRecoveryCodes: (password: string, code: string) => api.post<TOTPCodesResponse>('/me/totp/recovery-codes', { password, code }),
}

export const healthApi = {
  check: (o?: RequestOptions) => api.get<HealthResponse>('/health', o),
}

export const documentsApi = {
  list: (params: URLSearchParams, o?: RequestOptions) => api.get<ListResult>(`/documents${qs(params)}`, o),
  summary: (o?: RequestOptions) => api.get<Summary>('/documents/summary', o),
  recent: (limit = 10, o?: RequestOptions) => api.get<RecentResponse>(`/documents/recent${qs({ limit })}`, o),
  get: (ref: string, o?: RequestOptions) => api.get<DocumentResponse>(`/documents/${encodeURIComponent(ref)}`, o),
  /** «Доска с нитками»: документ, связанные с ним документы и ссылки между ними — только то, что читатель вправе видеть */
  graph: (ref: string, depth: 1 | 2, o?: RequestOptions) => api.get<GraphResult>(`/graph/${encodeURIComponent(ref)}${qs({ depth })}`, o),
  /** Публичные настройки сайта: контакты автора для страницы «О КУПОЛЕ» */
  site: (o?: RequestOptions) => api.get<SiteResponse>('/site', o).then((r) => r.site),
  /** Хронология «О КУПОЛЕ»: события не выше допуска читателя */
  timeline: (o?: RequestOptions) => api.get<TimelineResponse>('/timeline', o).then((r) => r.items),
  /** Поиск по названиям, шифрам и тексту блоков: в выдаче только то, что читатель вправе видеть */
  search: (params: URLSearchParams, o?: RequestOptions) => api.get<SearchResult>(`/search${qs(params)}`, o),
}

/** «Пометки на полях» под документом (шаг 5.2): читает кто угодно, пишет и жалуется только вошедший. */
export const remarksApi = {
  list: (ref: string, o?: RequestOptions) => api.get<RemarksResponse>(`/documents/${encodeURIComponent(ref)}/remarks`, o).then((r) => r.items as RemarkOut[]),
  create: (ref: string, body: CreateRemarkRequest) => api.post<RemarkResponse>(`/documents/${encodeURIComponent(ref)}/remarks`, body).then((r) => r.remark as RemarkOut),
  report: (id: number) => api.post<null>(`/remarks/${id}/report`),
  remove: (id: number) => api.delete<null>(`/remarks/${id}`),
  /** Очередь жалоб модератору (право moderate_comments) */
  reported: (o?: RequestOptions) => api.get<RemarksResponse>('/team/remarks/reported', o).then((r) => r.items as RemarkOut[]),
}

/** «Оценки» (шаг 5.3): «ознакомлен / одобряю / сомнительно» — канцелярские отметки читателей. */
export const ratingsApi = {
  get: (ref: string, o?: RequestOptions) => api.get<RatingsResponse>(`/documents/${encodeURIComponent(ref)}/ratings`, o).then((r) => r.ratings as DocumentRatings),
  set: (ref: string, rating: Rating) => api.post<RatingsResponse>(`/documents/${encodeURIComponent(ref)}/ratings`, { rating } satisfies SetRatingRequest).then((r) => r.ratings as DocumentRatings),
  remove: (ref: string) => api.delete<RatingsResponse>(`/documents/${encodeURIComponent(ref)}/ratings`).then((r) => r.ratings as DocumentRatings),
}

/** «Предложения» (шаг 5.4): одна форма для читателей, собственный список, очередь и решение — только с правом review. */
export const suggestionsApi = {
  create: (body: CreateSuggestionRequest) => api.post<SuggestionResponse>('/suggestions', body).then((r) => r.suggestion),
  mine: (o?: RequestOptions) => api.get<SuggestionsResponse>('/suggestions', o).then((r) => r.items),
  /** Очередь для команды: фильтр по статусу, страницы */
  queue: (params: { status?: string; page?: number; per_page?: number }, o?: RequestOptions) => api.get<SuggestionsListResponse>(`/team/suggestions${qs(params)}`, o),
  /** Перевод в новый статус с пояснением автору («записка»); начисляет XP при принятии */
  setStatus: (id: number, body: SuggestionStatusRequest) => api.post<SuggestionResponse>(`/team/suggestions/${id}/status`, body).then((r) => r.suggestion),
}

export interface TeamListParams {
  status?: string
  type?: string
  q?: string
  mine?: boolean
  page?: number
  per_page?: number
}

export const teamApi = {
  roles: (o?: RequestOptions) => api.get<TeamRolesResponse>('/team/roles', o),
  members: (params: { q?: string; role?: string; staff?: boolean; page?: number; per_page?: number }, o?: RequestOptions) =>
    api.get<TeamMembersResponse>(`/team/members${qs({ ...params, staff: params.staff ? '1' : undefined })}`, o),
  grant: (login: string, role: string) =>
    api.put<MemberRoleResponse>(`/team/members/${encodeURIComponent(login)}/roles/${encodeURIComponent(role)}`),
  revoke: (login: string, role: string) =>
    api.delete<MemberRoleResponse>(`/team/members/${encodeURIComponent(login)}/roles/${encodeURIComponent(role)}`),

  /** Рабочий стол: мои документы по состояниям, возвращённые на доработку, очередь на проверку (рецензентам) */
  dashboard: (o?: RequestOptions) => api.get<DashboardResponse>('/team/dashboard', o).then((r) => r.dashboard),
  meta: (o?: RequestOptions) => api.get<Meta>('/team/document-types', o),
  documents: (params: TeamListParams, o?: RequestOptions) => api.get<TeamListResult>(`/team/documents${qs({ ...params, mine: params.mine ? '1' : undefined })}`, o),
  create: (body: CreateDocumentRequest) => api.post<TeamDocumentResponse>('/team/documents', body),
  document: (id: number, o?: RequestOptions) => api.get<TeamDocumentResponse>(`/team/documents/${id}`, o),
  save: (id: number, body: SaveDocumentRequest) => api.put<SaveResult>(`/team/documents/${id}`, body),
  /** Автосохранение: тело — содержимое документа целиком (без base_revision) */
  autosave: (id: number, content: Content) => api.put<AutosaveResult>(`/team/documents/${id}/draft`, content),
  takeLock: (id: number) => api.post<LockResponse>(`/team/documents/${id}/lock`),
  releaseLock: (id: number) => api.delete<null>(`/team/documents/${id}/lock`),
  versions: (id: number, params: { page?: number; per_page?: number } = {}, o?: RequestOptions) =>
    api.get<VersionsPage>(`/team/documents/${id}/versions${qs(params)}`, o),
  version: (id: number, versionId: number, o?: RequestOptions) => api.get<VersionResponse>(`/team/documents/${id}/versions/${versionId}`, o),
  /** against: номер другой версии или 'live' (по умолчанию) — сравнить с текущим документом */
  diff: (id: number, versionId: number, against: number | 'live' = 'live', o?: RequestOptions) =>
    api.get<DiffResponse>(`/team/documents/${id}/versions/${versionId}/diff${qs({ against })}`, o),
  /** Документ глазами читателя уровня level (0–7): сервер фильтрует, как при чтении. content — несохранённые правки; без него — сохранённое */
  preview: (id: number, body: PreviewRequest, o?: RequestOptions) => api.post<PreviewResult>(`/team/documents/${id}/preview`, body, o),
  /** Шаблоны документов и наборы блоков: читают все члены команды, ведёт право manage_templates */
  templates: (kind: 'document' | 'blockset' | '', o?: RequestOptions) => api.get<TemplatesResponse>(`/team/templates${qs({ kind: kind || undefined })}`, o).then((r) => r.items),
  template: (id: number, o?: RequestOptions) => api.get<TemplateResponse>(`/team/templates/${id}`, o).then((r) => r.template),
  createTemplate: (body: TemplateInput) => api.post<TemplateResponse>('/team/templates', body).then((r) => r.template),
  updateTemplate: (id: number, body: { name: string; description: string; content?: TemplateContent }) =>
    api.put<TemplateResponse>(`/team/templates/${id}`, body).then((r) => r.template),
  deleteTemplate: (id: number) => api.delete<null>(`/team/templates/${id}`),
  /** Настройки сайта (контакты автора): читают члены команды, правит только Директорат */
  site: (o?: RequestOptions) => api.get<SiteSettingsResponse>('/team/site', o).then((r) => r.site),
  updateSite: (contact: string) => api.put<SiteSettingsResponse>('/team/site', { contact }).then((r) => r.site),
  /** Хронология «О КУПОЛЕ» для редактирования: все события, ведёт право manage_timeline */
  timeline: (o?: RequestOptions) => api.get<TimelineEventsResponse>('/team/timeline', o).then((r) => r.items),
  createEvent: (body: TimelineInput) => api.post<TimelineEventResponse>('/team/timeline', body).then((r) => r.event),
  updateEvent: (id: number, body: TimelineInput) => api.put<TimelineEventResponse>(`/team/timeline/${id}`, body).then((r) => r.event),
  deleteEvent: (id: number) => api.delete<null>(`/team/timeline/${id}`),
  /** Глоссарий канона: читают все члены команды, ведёт право manage_glossary */
  glossary: (q: string, o?: RequestOptions) => api.get<GlossaryResponse>(`/team/glossary${qs({ q: q || undefined })}`, o).then((r) => r.items),
  createTerm: (body: TermInput) => api.post<TermResponse>('/team/glossary', body).then((r) => r.term),
  updateTerm: (id: number, body: TermInput) => api.put<TermResponse>(`/team/glossary/${id}`, body).then((r) => r.term),
  deleteTerm: (id: number) => api.delete<null>(`/team/glossary/${id}`),
  /** Экспорт документа в файл: json — формат загрузки (круг экспорт → правка → импорт), md — для чтения и правки текста */
  exportDocument: (id: number, format: 'json' | 'md', o?: RequestOptions) => api.download(`/team/documents/${id}/export${qs({ format })}`, o),
  /**
   * Черновик из файла формата загрузки (содержимое файла, уже разобранное как JSON). С dryRun файл только проверяется:
   * те же замечания с путями, но ничего не создаётся.
   */
  importDocument: (file: unknown, dryRun = false) => api.post<ImportResult>(`/team/documents/import${qs({ dry_run: dryRun ? '1' : undefined })}`, file),
  /** Проверка канона по сохранённому документу */
  lint: (id: number, o?: RequestOptions) => api.get<LintResponse>(`/team/documents/${id}/lint`, o),
  /** Ход рецензии и комментарии */
  review: (id: number, o?: RequestOptions) => api.get<ReviewResponse>(`/team/documents/${id}/review`, o),
  submit: (id: number, baseRevision: number) => api.post<TeamDocumentResponse>(`/team/documents/${id}/submit`, { base_revision: baseRevision } satisfies SubmitRequest),
  withdraw: (id: number, comment = '') => api.post<TeamDocumentResponse>(`/team/documents/${id}/withdraw`, { comment }),
  verdict: (id: number, body: VerdictRequest) => api.post<TeamDocumentResponse>(`/team/documents/${id}/verdict`, body),
  archive: (id: number, comment = '') => api.post<TeamDocumentResponse>(`/team/documents/${id}/archive`, { comment }),
  unarchive: (id: number, comment = '') => api.post<TeamDocumentResponse>(`/team/documents/${id}/unarchive`, { comment }),
  addComment: (id: number, body: AddCommentRequest) => api.post<CommentResponse>(`/team/documents/${id}/comments`, body),
  resolveComment: (id: number, commentId: number, resolved: boolean) =>
    api.put<CommentResponse>(`/team/documents/${id}/comments/${commentId}`, { resolved } satisfies ResolveCommentRequest),
  deleteComment: (id: number, commentId: number) => api.delete<null>(`/team/documents/${id}/comments/${commentId}`),
  restore: (id: number, versionId: number) => api.post<SaveResult>(`/team/documents/${id}/versions/${versionId}/restore`),
  /** Наказания (шаг 5.9): Модератор и Директорат */
  sanctions: (login: string, o?: RequestOptions) => api.get<SanctionsResponse>(`/team/sanctions${qs({ login: login || undefined })}`, o).then((r) => r.items as SanctionItem[]),
  issueSanction: (body: SanctionInput) => api.post<SanctionResponse>('/team/sanctions', body).then((r) => r.sanction as SanctionItem),
  revokeSanction: (id: number) => api.post<SanctionResponse>(`/team/sanctions/${id}/revoke`).then((r) => r.sanction as SanctionItem),
  /** Записка Директората: одному (login) или всем (login пуст) */
  sendNote: (body: SendNoteRequest) => api.post<SendNoteResponse>('/team/inbox/send', body),
  /** Удалить документ безвозвратно (Редактор, Директорат); шифр освобождается сразу */
  deleteDocument: (id: number) => api.delete<null>(`/team/documents/${id}`),
  /** Скрытые коды (пасхалки, шаг 5.5): заводит и удаляет только Директорат */
  secretCodes: (o?: RequestOptions) => api.get<SecretCodesResponse>('/team/secret-codes', o).then((r) => r.codes),
  createSecretCode: (body: SecretCodeInput) => api.post<SecretCodeResponse>('/team/secret-codes', body).then((r) => r.code),
  deleteSecretCode: (id: number) => api.delete<null>(`/team/secret-codes/${id}`),
}

/** Погашение скрытого кода (шаг 5.5): требует входа, один раз на аккаунт и код. */
export const secretCodesApi = {
  redeem: (code: string) => api.post<RedeemResult>('/secret-codes/redeem', { code } satisfies RedeemCodeRequest),
}
