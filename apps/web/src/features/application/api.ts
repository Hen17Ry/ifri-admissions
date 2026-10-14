import { api } from '@/lib/api'
export interface Profile {
  lastName: string
  firstNames: string
  email: string
  phone: string
}
export interface Campaign {
  id: string
  code: string
  name: string
  academicYear: string
  startsAt: string
  endsAt: string
  status: string
}
export interface Qualification {
  code: string
  name: string
  family: string
}
export interface Program {
  code: string
  name: string
  availableSeats?: number | null
}
export interface Draft {
  id: string
  profile: Profile
  campaign: Campaign
  qualification: Qualification | null
  graduationYear: number | null
  program: Program | null
  state: string
  applicationNumber?: string | null
  submittedAt?: string | null
  version: number
  updatedAt: string
  sessionExpiresAt: string
}
export const draftApi = {
  current: () => api<Draft>('/applications/drafts/current'),
  create: (campaign: string) =>
    api<Draft>('/applications/drafts', { method: 'POST', body: JSON.stringify({ campaign }) }),
  patch: (kind: string, value: unknown) =>
    api<Draft>(`/applications/drafts/current/${kind}`, {
      method: 'PATCH',
      body: JSON.stringify(value),
    }),
  campaign: (code?: string) =>
    api<Campaign>(`/campaigns/current${code ? `?campaign=${encodeURIComponent(code)}` : ''}`),
  qualifications: () => api<Qualification[]>('/origin-qualifications'),
  programs: (campaign: string, qualification: string) =>
    api<Program[]>(
      `/programs?campaign=${encodeURIComponent(campaign)}&qualification=${encodeURIComponent(qualification)}`,
    ),
}
