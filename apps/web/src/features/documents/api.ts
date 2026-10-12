import { api } from '@/lib/api'
export interface ApplicationDocument {
  type: string
  label: string
  required: boolean
  uploaded: boolean
  version: number
  filename: string
  sizeBytes: number
}
export interface DocumentListing {
  documents: ApplicationDocument[]
  complete: boolean
  maxBytes: number
  applicationVersion: number
}
export const documentsApi = {
  list: () => api<DocumentListing>('/applications/drafts/current/documents'),
  upload: (type: string, file: File, version: number) => {
    const body = new FormData()
    body.append('file', file)
    return api<DocumentListing>(
      `/applications/drafts/current/documents/${encodeURIComponent(type)}`,
      {
        method: 'POST',
        headers: { 'X-Application-Version': String(version) },
        body,
        signal: AbortSignal.timeout(65000),
      },
    )
  },
  content: (type: string) =>
    `/api/v1/applications/drafts/current/documents/${encodeURIComponent(type)}/content`,
}
