import { adminRequest } from '@/features/admin/api'
import type { Average, Person } from '@/features/review/api'

export type Decision = 'ADMITTED' | 'WAITLISTED' | 'WITHDRAWN' | 'NOT_RANKED'
export type Criterion = 'AVERAGE' | 'MATH' | 'SUBMITTED_AT' | 'APPLICATION_NUMBER'
export type MathTiebreak = 'NOT_NEEDED' | 'COMPARED' | 'SKIPPED'
export type Variant = 'PUBLIC' | 'INTERNAL'
export interface Offer {
  id: string
  campaignCode: string
  campaignName: string
  programCode: string
  programName: string
  seats: number | null
  minimumAverage: string
  campaignOpen: boolean
  endsAt: string
  frozen: boolean
}
export interface Entry {
  applicationId: string
  applicationNumber: string
  lastName: string
  firstNames: string
  qualification: string
  position: number
  rank: number | null
  average: Average
  mathSubject?: string
  mathGrade?: string
  mathTiebreak: MathTiebreak
  submittedAt: string
  decisiveCriterion?: Criterion
  initialDecision?: Decision
  decision?: Decision
}
export interface Blocker {
  code: string
  message: string
  count?: number
}
export interface Freeze {
  frozenAt: string
  frozenBy: Person
  seats: number
  minimumAverage: string
  listVersion: number
  versionAt: string
  freeSeats: number
}
export interface SelectionEvent {
  version: number
  type: 'WITHDRAWN' | 'PROMOTED'
  applicationId: string
  applicationNumber: string
  candidate: string
  reason?: string
  actor: Person
  occurredAt: string
}
export interface ExportJob {
  id: string
  listVersion: number
  variant: Variant
  status: 'PENDING' | 'READY' | 'FAILED'
  failureCode?: string
  requestedBy: Person
  requestedAt: string
}
export interface RankingView {
  offer: Offer
  counts: {
    validated: number
    rejected: number
    toReview: number
    inReview: number
    pendingCorrections: number
  }
  blockers: Blocker[]
  freeze: Freeze | null
  entries: Entry[]
  events: SelectionEvent[]
  proposal: Entry | null
  exports: ExportJob[]
}
export interface Settings {
  seats: number | null
  minimumAverage: string
}

const offer = (id: string) => `/selection/offers/${encodeURIComponent(id)}`
export const selectionApi = {
  offers: () => adminRequest<{ offers: Offer[] }>('/selection/offers'),
  view: (id: string) => adminRequest<RankingView>(offer(id)),
  settings: (id: string, input: Settings) =>
    adminRequest<RankingView>(`${offer(id)}/settings`, 'PUT', input),
  freeze: (id: string, input: Settings) =>
    adminRequest<RankingView>(`${offer(id)}/freeze`, 'POST', input),
  withdraw: (id: string, input: { applicationId: string; reason: string; listVersion: number }) =>
    adminRequest<RankingView>(`${offer(id)}/withdrawals`, 'POST', input),
  promote: (id: string, input: { applicationId: string; listVersion: number }) =>
    adminRequest<RankingView>(`${offer(id)}/promotions`, 'POST', input),
  requestPdf: (id: string, variant: Variant) =>
    adminRequest<ExportJob>(`${offer(id)}/exports`, 'POST', { variant }),
  exportState: (id: string) =>
    adminRequest<ExportJob>(`/selection/exports/${encodeURIComponent(id)}`),
}
// Authenticated downloads (session cookie); each one is audited by the API.
export const csvUrl = (id: string, variant: Variant) =>
  `/api/v1/admin${offer(id)}/export.csv?variant=${variant === 'PUBLIC' ? 'public' : 'internal'}`
export const pdfUrl = (exportId: string) =>
  `/api/v1/admin/selection/exports/${encodeURIComponent(exportId)}/content`

export const decisionLabels: Record<Decision, string> = {
  ADMITTED: 'Admis',
  WAITLISTED: 'Liste d’attente',
  WITHDRAWN: 'Désisté',
  NOT_RANKED: 'Non classé',
}
export const criterionLabels: Record<Criterion, string> = {
  AVERAGE: 'Moyenne',
  MATH: 'Note de mathématiques',
  SUBMITTED_AT: 'Date de soumission',
  APPLICATION_NUMBER: 'Numéro de candidature',
}
export const mathSkipped = 'Maths non comparées : candidat sans matière mathématique dans le groupe'
export const variantLabels: Record<Variant, string> = {
  PUBLIC: 'Liste publique',
  INTERNAL: 'Liste interne',
}
