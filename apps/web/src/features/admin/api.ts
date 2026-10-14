import { api, ApiError } from '@/lib/api'

export type Stage = 'MFA_ENROLLMENT' | 'MFA_VERIFICATION' | 'ACTIVE'
export type Role = 'SUPER_ADMIN' | 'REVIEWER' | 'REVIEW_LEAD' | 'DEPUTY_DIRECTOR' | 'DIRECTOR'
export interface Grant {
  role: Role
  program?: string
  programName?: string
}
export interface AdminSession {
  id: string
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
export function adminRequest<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
  const mutation = method !== 'GET'
  return api<T>(`/admin${path}`, {
    method,
    body: mutation ? JSON.stringify(body ?? {}) : undefined,
    headers: mutation && csrfToken ? { 'X-CSRF-Token': csrfToken } : {},
  })
}
export const adminApi = {
  session: () => adminRequest<AdminSession>('/session'),
  login: (email: string, password: string) =>
    adminRequest<StageResponse>('/auth/login', 'POST', { email, password }),
  logout: () => adminRequest<{ loggedOut: boolean }>('/auth/logout', 'POST'),
  startEnrollment: () => adminRequest<Enrollment>('/auth/mfa/enrollment', 'POST'),
  confirmEnrollment: (code: string) =>
    adminRequest<StageResponse>('/auth/mfa/enrollment/confirm', 'POST', { code }),
  verify: (code: string) => adminRequest<StageResponse>('/auth/mfa/verify', 'POST', { code }),
  changePassword: (currentPassword: string, newPassword: string) =>
    adminRequest<{ reauthenticate: boolean }>('/auth/password', 'POST', {
      currentPassword,
      newPassword,
    }),
  regenerateRecoveryCodes: () =>
    adminRequest<{ recoveryCodes: string[] }>('/auth/recovery-codes', 'POST'),
  inspectActivation: (token: string) =>
    adminRequest<{ email: string; displayName: string }>('/activation/inspect', 'POST', { token }),
  activate: (token: string, password: string) =>
    adminRequest<{ activated: boolean }>('/activation', 'POST', { token, password }),
  accounts: () => adminRequest<{ accounts: Account[] }>('/accounts'),
  createAccount: (input: { email: string; displayName: string; grants: GrantInput[] }) =>
    adminRequest<Activation>('/accounts', 'POST', input),
  reissueActivation: (id: string) =>
    adminRequest<Activation>(`/accounts/${encodeURIComponent(id)}/activation-link`, 'POST'),
  setStatus: (id: string, enabled: boolean) =>
    adminRequest<Account>(
      `/accounts/${encodeURIComponent(id)}/${enabled ? 'enable' : 'disable'}`,
      'POST',
    ),
  setGrants: (id: string, grants: GrantInput[]) =>
    adminRequest<Account>(`/accounts/${encodeURIComponent(id)}/grants`, 'PUT', { grants }),
  resetMfa: (id: string) =>
    adminRequest<Account>(`/accounts/${encodeURIComponent(id)}/mfa/reset`, 'POST'),
  programs: () => adminRequest<{ programs: Program[] }>('/programs'),
  audit: (filter: AuditFilter) => {
    const query = new URLSearchParams()
    for (const [key, value] of Object.entries(filter)) if (value) query.set(key, value)
    const text = query.toString()
    return adminRequest<AuditPage>(`/audit-events${text ? `?${text}` : ''}`)
  },
}

// One role per account (ADR 110): the order follows the organisation chart of docs/ADMIN_REVIEW.md.
export const roleLabels: Record<Role, string> = {
  REVIEWER: 'Agent du secrétariat exécutif',
  REVIEW_LEAD: 'Responsable du secrétariat exécutif',
  DEPUTY_DIRECTOR: 'Directeur adjoint',
  DIRECTOR: 'Directeur',
  SUPER_ADMIN: 'Administrateur technique',
}
export const roleDescriptions: Record<Role, string> = {
  REVIEWER:
    'Prend des dossiers en charge, les valide ou les rejette et propose des corrections de notes ou de coefficients.',
  REVIEW_LEAD:
    'Mêmes droits qu’un agent ; peut aussi réattribuer un dossier ou le remettre en vérification.',
  DEPUTY_DIRECTOR:
    'Consulte les dossiers ; seul à approuver ou refuser une correction. Ne propose jamais de correction.',
  DIRECTOR:
    'Consulte tous les dossiers et le journal d’audit, sans rien modifier ; validera la sélection finale.',
  SUPER_ADMIN:
    'Gère les comptes et consulte le journal d’audit. N’a pas accès aux dossiers des candidats.',
}
export function roleLabel(role: string): string {
  return roleLabels[role as Role] ?? role
}
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
  ADMIN_ROLES_CHANGED: 'Rôle modifié',
  ADMIN_SUPER_ADMIN_BOOTSTRAPPED: 'Premier super-administrateur créé (serveur)',
  APPLICATION_SUBMITTED: 'Candidature soumise',
  CORRECTION_APPROVED: 'Correction approuvée',
  CORRECTION_PROPOSED: 'Correction proposée',
  CORRECTION_REFUSED: 'Correction refusée',
  REVIEW_CLAIMED: 'Dossier pris en charge',
  REVIEW_REASSIGNED: 'Dossier réattribué',
  REVIEW_REJECTED: 'Dossier rejeté',
  REVIEW_REVERTED: 'Dossier remis en vérification',
  REVIEW_VALIDATED: 'Dossier validé',
  SYNTHETIC_DATA_GENERATED: 'Données synthétiques générées (serveur)',
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
