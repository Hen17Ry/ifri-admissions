import { api } from '@/lib/api'
export interface ExtractedSubject {
  subjectId: string
  code?: string
  name?: string
  detectedSubjectText: string
  gradeValue: string
  coefficientValue: string
  position: number
}
export interface TranscriptAnalysis {
  id: string
  applicationVersion?: number
  documentVersionId: string
  status: 'QUEUED' | 'PROCESSING' | 'SUCCEEDED' | 'FAILED' | 'LOW_CONFIDENCE' | 'OBSOLETE'
  candidateConfirmation: 'PENDING' | 'CONFIRMED' | 'REJECTED'
  subjects: ExtractedSubject[]
  failureCode?: string
  verificationReady: boolean
}
const base = '/applications/drafts/current/transcript-analysis'
export const analysisApi = {
  current: () => api<TranscriptAnalysis>(base),
  decide: (action: 'confirm' | 'reject', analysisId: string, version: number) =>
    api<TranscriptAnalysis>(`${base}/${action}`, {
      method: 'POST',
      body: JSON.stringify({ analysisId, version }),
    }),
}
