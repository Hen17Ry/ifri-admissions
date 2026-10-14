import { api, ApiError } from '@/lib/api'

export type Stage = 'MFA_ENROLLMENT' | 'MFA_VERIFICATION' | 'ACTIVE'
export type Role = 'SUPER_ADMIN' | 'REVIEWER' | 'SELECTION_MANAGER' | 'AUDITOR'
export interface Grant {
  role: Role
  program?: string
  programName?: string
}
export interface AdminSession {
  stage: Stage
  csrfToken: string
  email: string
  displayName: string
  permissions: string[]
  grants: Grant[]
  idleExpiresAt: string
  expiresAt: string
  mfaRequired: boolean
}
export interface StageResponse {
  stage: Stage
  csrfToken: string
  recoveryCodes?: string[]
}
export interface Enrollment {
  secret: string
  uri: string
  qrSvg: string
}
export interface Account {
  id: string
  email: string
  displayName: string
  status: 'PENDING_ACTIVATION' | 'ACTIVE' | 'DISABLED'
  mfaEnabled: boolean
  lastLoginAt: string | null
  createdAt: string
  activationExpiresAt: string | null
  grants: Grant[]
}
export interface Activation {
  account: Account
  activationUrl: string
  expiresAt: string
}
export interface Program {
  code: string
  name: string
}
export interface GrantInput {
  role: Role
  program: string
}
export interface AuditEntry {
  id: string
  eventType: string
  actorType: 'ADMIN' | 'CANDIDATE' | 'SYSTEM'
  actorId?: string
  actorLabel?: string
  targetType?: string
  targetId?: string
  targetLabel?: string
  metadata: Record<string, unknown>
  occurredAt: string
}
export interface AuditPage {
  events: AuditEntry[]
  nextCursor?: string
  eventTypes: string[]
}
export interface AuditFilter {
  eventType?: string
  actorType?: string
  from?: string
  to?: string
  cursor?: string
}

// The CSRF token comes from the session responses; it is kept in memory only.
let csrfToken = ''
export function setCsrfToken(token: string) {
  csrfToken = token
}
function request<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
  const mutation = method !== 'GET'
  return api<T>(`/admin${path}`, {
    method,
    body: mutation ? JSON.stringify(body ?? {}) : undefined,
    headers: mutation && csrfToken ? { 'X-CSRF-Token': csrfToken } : {},
  })
}
export const adminApi = {
  session: () => request<AdminSession>('/session'),
  login: (email: string, password: string) =>
    request<StageResponse>('/auth/login', 'POST', { email, password }),
  logout: () => request<{ loggedOut: boolean }>('/auth/logout', 'POST'),
  startEnrollment: () => request<Enrollment>('/auth/mfa/enrollment', 'POST'),
  confirmEnrollment: (code: string) =>
    request<StageResponse>('/auth/mfa/enrollment/confirm', 'POST', { code }),
  verify: (code: string) => request<StageResponse>('/auth/mfa/verify', 'POST', { code }),
  changePassword: (currentPassword: string, newPassword: string) =>
    request<{ reauthenticate: boolean }>('/auth/password', 'POST', {
      currentPassword,
      newPassword,
    }),
  regenerateRecoveryCodes: () =>
    request<{ recoveryCodes: string[] }>('/auth/recovery-codes', 'POST'),
  inspectActivation: (token: string) =>
    request<{ email: string; displayName: string }>('/activation/inspect', 'POST', { token }),
  activate: (token: string, password: string) =>
    request<{ activated: boolean }>('/activation', 'POST', { token, password }),
  accounts: () => request<{ accounts: Account[] }>('/accounts'),
  createAccount: (input: { email: string; displayName: string; grants: GrantInput[] }) =>
    request<Activation>('/accounts', 'POST', input),
  reissueActivation: (id: string) =>
    request<Activation>(`/accounts/${encodeURIComponent(id)}/activation-link`, 'POST'),
  setStatus: (id: string, enabled: boolean) =>
    request<Account>(
      `/accounts/${encodeURIComponent(id)}/${enabled ? 'enable' : 'disable'}`,
      'POST',
    ),
  setGrants: (id: string, grants: GrantInput[]) =>
    request<Account>(`/accounts/${encodeURIComponent(id)}/grants`, 'PUT', { grants }),
  resetMfa: (id: string) =>
    request<Account>(`/accounts/${encodeURIComponent(id)}/mfa/reset`, 'POST'),
  programs: () => request<{ programs: Program[] }>('/programs'),
  audit: (filter: AuditFilter) => {
    const query = new URLSearchParams()
    for (const [key, value] of Object.entries(filter)) if (value) query.set(key, value)
    const text = query.toString()
    return request<AuditPage>(`/audit-events${text ? `?${text}` : ''}`)
  },
}

export const roleLabels: Record<Role, string> = {
  SUPER_ADMIN: 'Super-administrateur',
  REVIEWER: 'Vérificateur des dossiers',
  SELECTION_MANAGER: 'Responsable de la sélection',
  AUDITOR: 'Auditeur',
}
export const roleDescriptions: Record<Role, string> = {
  SUPER_ADMIN: 'Gère les comptes et consulte le journal d’audit.',
  REVIEWER: 'Vérifiera les dossiers de candidature.',
  SELECTION_MANAGER: 'Préparera le classement et la sélection.',
  AUDITOR: 'Consulte tout en lecture seule, journal d’audit compris.',
}
// Roles that apply to every program cannot be limited to some of them.
export const globalRoles: Role[] = ['SUPER_ADMIN', 'AUDITOR']
export const statusLabels: Record<Account['status'], string> = {
  ACTIVE: 'Actif',
  PENDING_ACTIVATION: 'Activation en attente',
  DISABLED: 'Désactivé',
}
export const eventLabels: Record<string, string> = {
  ADMIN_ACCOUNT_ACTIVATED: 'Compte activé',
  ADMIN_ACCOUNT_CREATED: 'Compte créé',
  ADMIN_ACCOUNT_DISABLED: 'Compte désactivé',
  ADMIN_ACCOUNT_ENABLED: 'Compte réactivé',
  ADMIN_ACTIVATION_FAILED: 'Lien d’activation invalide utilisé',
  ADMIN_ACTIVATION_LINK_ISSUED: 'Lien d’activation émis',
  ADMIN_DATA_ACCESSED: 'Consultation de données personnelles',
  ADMIN_LOGIN_FAILED: 'Connexion échouée',
  ADMIN_LOGIN_LOCKED: 'Connexions bloquées temporairement',
  ADMIN_LOGIN_PASSWORD_ACCEPTED: 'Mot de passe accepté, second facteur attendu',
  ADMIN_LOGIN_SUCCEEDED: 'Connexion réussie',
  ADMIN_LOGOUT: 'Déconnexion',
  ADMIN_MFA_ENROLLED: 'Double authentification activée',
  ADMIN_MFA_FAILED: 'Code de double authentification refusé',
  ADMIN_MFA_RESET: 'Double authentification réinitialisée',
  ADMIN_PASSWORD_CHANGE_FAILED: 'Changement de mot de passe refusé',
  ADMIN_PASSWORD_CHANGED: 'Mot de passe modifié',
  ADMIN_PASSWORD_RESET: 'Mot de passe réinitialisé (serveur)',
  ADMIN_RECOVERY_CODE_USED: 'Code de secours utilisé',
  ADMIN_RECOVERY_CODES_REGENERATED: 'Codes de secours régénérés',
  ADMIN_ROLES_CHANGED: 'Rôles ou périmètre modifiés',
  ADMIN_SUPER_ADMIN_BOOTSTRAPPED: 'Premier super-administrateur créé (serveur)',
  APPLICATION_SUBMITTED: 'Candidature soumise',
}
export const actorLabels: Record<AuditEntry['actorType'], string> = {
  ADMIN: 'Administrateur',
  CANDIDATE: 'Candidat',
  SYSTEM: 'Système',
}

const messages: Record<string, string> = {
  ADMIN_LOGIN_FAILED: 'Identifiant ou mot de passe incorrect.',
  ADMIN_RATE_LIMITED:
    'Trop de tentatives. Patientez quelques minutes avant de réessayer : la connexion est temporairement bloquée.',
  ADMIN_SESSION_EXPIRED: 'Votre session a expiré. Connectez-vous de nouveau.',
  ADMIN_SESSION_REQUIRED: 'Connectez-vous pour accéder à l’espace d’administration.',
  ADMIN_MFA_INVALID: 'Code incorrect ou déjà utilisé. Saisissez le code affiché actuellement.',
  ADMIN_FORBIDDEN: 'Vous n’avez pas l’autorisation d’effectuer cette action.',
  ADMIN_CSRF_INVALID: 'La requête n’a pas pu être vérifiée. Rechargez la page.',
  ADMIN_ACTIVATION_INVALID:
    'Ce lien d’activation n’est plus valide. Demandez-en un nouveau à un super-administrateur.',
  ADMIN_LAST_SUPER_ADMIN: 'Il doit rester au moins un super-administrateur actif.',
  ADMIN_SELF_ACTION_FORBIDDEN: 'Vous ne pouvez pas désactiver votre propre compte.',
  ADMIN_ACCOUNT_EXISTS: 'Un compte existe déjà avec cette adresse email.',
  NETWORK_ERROR: 'Connexion au serveur impossible. Vérifiez votre réseau et réessayez.',
}
export function adminMessage(error: ApiError): string {
  return messages[error.code] ?? error.message
}
export { ApiError }
