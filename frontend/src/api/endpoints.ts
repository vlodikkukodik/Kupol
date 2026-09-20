// Типизированные обращения к API: путь, параметры и форма ответа описаны в одном месте.
// Формы ответов — из generated/ (их порождает tygo из Go), поэтому расхождение с сервером ловит `make types-check`.
import { api } from './index'
import type { RequestOptions } from './client'
import type {
  Content,
  ListResult,
  Meta,
  PreviewResult,
  TemplateContent,
  TemplateInput,
  Summary,
  TeamListResult,
  VersionsPage,
  SaveResult,
  AutosaveResult,
} from './generated/documents'
import type {
  AddCommentRequest,
  CaptchaResponse,
  ChangePasswordRequest,
  CommentResponse,
  CreateDocumentRequest,
  DashboardResponse,
  DiffResponse,
  DocumentResponse,
  HealthResponse,
  LintResponse,
  LockResponse,
  LoginRequest,
  LoginResponse,
  MemberRoleResponse,
  PreviewRequest,
  RecentResponse,
  RegisterRequest,
  RegisterResponse,
  RestoreRequest,
  ResolveCommentRequest,
  ReviewResponse,
  SaveDocumentRequest,
  SessionResponse,
  SubmitRequest,
  TeamDocumentResponse,
  TemplateResponse,
  TemplatesResponse,
  TeamMembersResponse,
  TeamRolesResponse,
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
  restore: (body: RestoreRequest) => api.post<RegisterResponse>('/auth/restore', body),
  logout: () => api.post<null>('/auth/logout'),
  changePassword: (body: ChangePasswordRequest) => api.post<null>('/me/password', body),
  deleteAccount: (password: string) => api.delete<null>('/me', { password }),
}

export const healthApi = {
  check: (o?: RequestOptions) => api.get<HealthResponse>('/health', o),
}

export const documentsApi = {
  list: (params: URLSearchParams, o?: RequestOptions) => api.get<ListResult>(`/documents${qs(params)}`, o),
  summary: (o?: RequestOptions) => api.get<Summary>('/documents/summary', o),
  recent: (limit = 10, o?: RequestOptions) => api.get<RecentResponse>(`/documents/recent${qs({ limit })}`, o),
  get: (ref: string, o?: RequestOptions) => api.get<DocumentResponse>(`/documents/${encodeURIComponent(ref)}`, o),
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
}
