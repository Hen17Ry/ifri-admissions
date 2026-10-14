import { api, type ApiError } from '@/lib/api'
// The receipt never carries the average: it is computed and kept by the backend only.
export interface SubmissionReceipt {
  applicationNumber: string
  submittedAt: string
  candidate: { lastName: string; firstNames: string }
  campaign: { code: string; name: string; academicYear: string }
  program: { code: string; name: string }
  qualification: { code: string; name: string }
  graduationYear: number
  sessionExpiresAt: string
}
const path = '/applications/drafts/current/submission'
export const submissionApi = {
  // Idempotent on the server: a retry after a timeout returns the same number.
  submit: (version: number) =>
    api<SubmissionReceipt>(path, {
      method: 'POST',
      body: JSON.stringify({ version }),
      signal: AbortSignal.timeout(20000),
    }),
  receipt: () => api<SubmissionReceipt>(path),
}
// French texts chosen by the client for each API code, with the action expected next.
const messages: Record<string, string> = {
  CAMPAIGN_CLOSED: 'La campagne est close : votre candidature ne peut plus être soumise.',
  APPLICATION_INCOMPLETE:
    'Votre dossier est incomplet. Complétez les éléments signalés, puis soumettez de nouveau.',
  APPLICATION_NOT_ELIGIBLE:
    'Cette filière n’est plus accessible avec votre qualification. Choisissez une autre filière.',
  TRANSCRIPT_ANALYSIS_REQUIRED:
    'Confirmez d’abord les informations lues sur votre relevé de notes, puis soumettez de nouveau.',
  TRANSCRIPT_ANALYSIS_OUTDATED:
    'Votre relevé ou votre parcours a changé depuis votre confirmation. Vérifiez et confirmez la nouvelle analyse de votre relevé.',
  ADMISSION_RULES_CHANGED:
    'Les règles d’admission de votre filière ont changé. Votre relevé est analysé de nouveau : confirmez le nouveau résultat avant de soumettre.',
  APPLICATION_LOCKED: 'Votre candidature a déjà été soumise.',
  VERSION_CONFLICT:
    'Votre dossier a été modifié dans un autre onglet. Rechargez-le et vérifiez-le avant de soumettre.',
  NETWORK_ERROR:
    'La connexion a été interrompue. Réessayez : si votre candidature a déjà été enregistrée, le même numéro vous sera affiché.',
  SUBMISSION_FAILED:
    'La soumission n’a pas pu être enregistrée. Réessayez : tant qu’aucun numéro n’est affiché, votre candidature reste un brouillon.',
}
export function submissionMessage(error: ApiError): string {
  return messages[error.code] ?? error.message
}
// Step of the wizard where each incomplete field is corrected.
export function incompleteStep(fields: Record<string, string>): number {
  const steps = Object.keys(fields).map((key) =>
    ['lastName', 'firstNames', 'email', 'phone'].includes(key)
      ? 0
      : ['qualification', 'graduationYear'].includes(key)
        ? 1
        : key === 'program'
          ? 2
          : 3,
  )
  return steps.length ? Math.min(...steps) : 3
}
export function formatSubmissionDate(value: string): string {
  return new Intl.DateTimeFormat('fr-FR', {
    dateStyle: 'long',
    timeStyle: 'short',
    timeZone: 'Africa/Porto-Novo',
  }).format(new Date(value))
}
