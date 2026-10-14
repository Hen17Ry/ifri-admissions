import { adminRequest } from '@/features/admin/api'

export type ReviewStatus = 'TO_REVIEW' | 'IN_REVIEW' | 'VALIDATED' | 'REJECTED'
export type ReviewAction = 'claim' | 'validate' | 'reject' | 'reassign' | 'revert'
export type CorrectionField = 'GRADE' | 'COEFFICIENT'
export type ProofState = 'NONE' | 'PENDING' | 'READY' | 'FAILED'
export interface Person {
  id: string
  name: string
}
// The exact fraction (weightedSum / coefficientSum) is the reference; display is truncated.
export interface Average {
  display: string
  decimal: string
  exact: boolean
  weightedSum: string
  coefficientSum: string
}
export interface ListItem {
  id: string
  applicationNumber: string
  submittedAt: string
  lastName: string
  firstNames: string
  qualification: string
  qualificationLabel: string
  program: string
  programName: string
  average: Average | null
  status: ReviewStatus
  assignee: Person | null
  flags: string[]
  pendingCorrections: number
}
export interface ListPage {
  items: ListItem[]
  total: number
  page: number
  pageSize: number
}
export interface ListFilter {
  q: string
  program: string
  qualification: string
  status: string
  flag: string
  assignee: string
  pendingCorrection: boolean
  sort: 'submittedAt' | 'average' | 'name'
  direction: 'asc' | 'desc'
  page: number
  pageSize: number
}
export interface DocumentView {
  type: string
  filename: string
  sizeBytes: number
  version: number
}
export interface CorrectionView {
  id: string
  applicationId: string
  applicationNumber?: string
  candidate?: string
  subjectId: string
  subjectName: string
  field: CorrectionField
  originalValue: string
  currentValue: string
  proposedValue: string
  reason: string
  proposedBy: Person
  proposedAt: string
  status: 'PENDING' | 'APPROVED' | 'REFUSED'
  decidedBy: Person | null
  decidedAt: string | null
  decisionReason?: string
  reviewVersion?: number
}
export interface SubjectView {
  subjectId: string
  code: string
  name: string
  position: number
  gradeRaw: string
  grade: string
  coefficientRaw: string
  coefficient: string
  effectiveGrade: string
  effectiveCoefficient: string
  validationMethod: string
  reconstructedCells: string[] | null
  proofStatus: '' | ProofState
}
export interface EventView {
  version: number
  type: string
  actor: Person | null
  fromStatus?: string
  toStatus: ReviewStatus
  fromAssignee?: Person
  toAssignee?: Person
  reasonCode?: string
  comment?: string
  correctionId?: string
  occurredAt: string
}
export interface FlagView {
  code: string
  direction: 'OUTGOING' | 'INCOMING'
  applicationId?: string
  applicationNumber?: string
}
export interface Detail {
  id: string
  applicationNumber: string
  submittedAt: string
  campaign: string
  lastName: string
  firstNames: string
  email: string
  phone: string
  qualification: string
  qualificationLabel: string
  graduationYear: number
  program: string
  programName: string
  declaredSeries: string
  readSeries: string
  snapshotAverage: Average | null
  effectiveAverage: Average | null
  status: ReviewStatus
  assignee: Person | null
  version: number
  rejectionCode?: string
  rejectionLabel?: string
  rejectionComment?: string
  frozen: boolean
  documents: DocumentView[]
  subjects: SubjectView[]
  corrections: CorrectionView[]
  history: EventView[]
  flags: FlagView[]
}
export interface Counter {
  program: string
  programName: string
  status: ReviewStatus
  count: number
}
export interface Dashboard {
  counters?: Counter[]
  pendingCorrections?: number
  unassigned?: number
  mine?: number
  coverage?: { deputyDirector: boolean; director: boolean }
}
export interface Choice {
  code: string
  label: string
}
export interface Reference {
  rejectionReasons: Choice[]
  agents: Person[]
  programs: Choice[]
  qualifications: Choice[]
}
export interface ActionInput {
  version: number
  reasonCode?: string
  comment?: string
  assignee?: string
}

const application = (id: string) => `/applications/${encodeURIComponent(id)}`
const subject = (id: string, subjectId: string) =>
  `${application(id)}/subjects/${encodeURIComponent(subjectId)}/proof`

export const reviewApi = {
  dashboard: () => adminRequest<Dashboard>('/dashboard'),
  reference: () => adminRequest<Reference>('/review/reference'),
  list: (filter: ListFilter) => {
    const query = new URLSearchParams()
    for (const [key, value] of Object.entries(filter)) {
      if (value !== '' && value !== false) query.set(key, String(value))
    }
    return adminRequest<ListPage>(`/applications?${query.toString()}`)
  },
  detail: (id: string) => adminRequest<Detail>(application(id)),
  transition: (id: string, action: ReviewAction, input: ActionInput) =>
    adminRequest<Detail>(`${application(id)}/review/${action}`, 'POST', input),
  propose: (
    id: string,
    input: {
      version: number
      subjectId: string
      field: CorrectionField
      value: string
      reason: string
    },
  ) => adminRequest<Detail>(`${application(id)}/corrections`, 'POST', input),
  corrections: () => adminRequest<{ corrections: CorrectionView[] }>('/corrections'),
  decide: (id: string, approve: boolean, input: { version: number; reason?: string }) =>
    adminRequest<Detail>(
      `/corrections/${encodeURIComponent(id)}/${approve ? 'approve' : 'refuse'}`,
      'POST',
      input,
    ),
  proof: (id: string, subjectId: string) =>
    adminRequest<{ status: ProofState; failureCode?: string }>(subject(id, subjectId)),
  requestProof: (id: string, subjectId: string) =>
    adminRequest<{ status: ProofState; failureCode?: string }>(subject(id, subjectId), 'POST'),
}
// Authenticated, same-origin streams: the session cookie travels, nothing is ever public.
export const documentUrl = (id: string, type: string) =>
  `/api/v1/admin${application(id)}/documents/${encodeURIComponent(type)}/content`
export const proofImageUrl = (id: string, subjectId: string) =>
  `/api/v1/admin${subject(id, subjectId)}/image`

export const statusLabels: Record<ReviewStatus, string> = {
  TO_REVIEW: 'À vérifier',
  IN_REVIEW: 'En vérification',
  VALIDATED: 'Validé',
  REJECTED: 'Rejeté',
}
export const flagLabels: Record<string, string> = {
  DUPLICATE_EMAIL: 'Email déjà utilisé',
  DUPLICATE_PHONE: 'Téléphone déjà utilisé',
  DUPLICATE_TRANSCRIPT: 'Relevé identique',
  SERIES_UNREADABLE: 'Série non lue',
}
export const documentLabels: Record<string, string> = {
  REQUEST_LETTER: 'Demande manuscrite',
  TRANSCRIPT: 'Relevé de notes',
  IDENTITY_DOCUMENT: 'Pièce d’identité',
  PARENTAL_COMMITMENT: 'Engagement parental',
}
export const methodLabels: Record<string, string> = {
  DIRECT_READ: 'Lecture directe de la note',
  ROW_ARITHMETIC: 'Note = Points ÷ Coeff sur la ligne',
  TABLE_TOTALS: 'Note reconstituée et validée par les totaux du tableau',
}
export const fieldLabels: Record<CorrectionField, string> = {
  GRADE: 'Note',
  COEFFICIENT: 'Coefficient',
}
export const eventLabels: Record<string, string> = {
  CREATED: 'Dossier soumis, en attente de vérification',
  CLAIMED: 'Pris en charge',
  VALIDATED: 'Validé',
  REJECTED: 'Rejeté',
  REASSIGNED: 'Réattribué',
  REVERTED: 'Remis en vérification',
  CORRECTION_PROPOSED: 'Correction proposée',
  CORRECTION_APPROVED: 'Correction approuvée',
  CORRECTION_REFUSED: 'Correction refusée',
}
export const proofFailures: Record<string, string> = {
  DOCUMENT_UNAVAILABLE: 'le document n’a pas pu être lu',
  PAGE_MISSING: 'la page indiquée par l’analyse est absente',
  RENDER_FAILED: 'le rendu de l’image a échoué',
  IMAGE_REJECTED: 'l’image produite a été refusée',
}
// French number display of an exact decimal string, without rounding ("12.5" → "12,5").
export const frenchNumber = (value: string) => value.replace('.', ',')
// An exact average needs no trailing zeros ("12.500…0" → "12,5"); a truncated one keeps all places.
export const exactNumber = (average: { decimal: string; exact: boolean }) =>
  frenchNumber(
    average.exact && average.decimal.includes('.')
      ? average.decimal.replace(/0+$/, '').replace(/\.$/, '')
      : average.decimal,
  )
