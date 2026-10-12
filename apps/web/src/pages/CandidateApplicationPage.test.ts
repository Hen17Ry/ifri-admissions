import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CandidateApplicationPage from './CandidateApplicationPage.vue'
import { draftApi, type Draft } from '@/features/application/api'
import { ApiError } from '@/lib/api'
vi.mock('@/features/application/api', () => ({
  draftApi: {
    current: vi.fn(),
    create: vi.fn(),
    patch: vi.fn(),
    campaign: vi.fn(),
    qualifications: vi.fn(),
    programs: vi.fn(),
  },
}))
const campaign = {
  id: 'campaign',
  code: 'DEV_LOCAL',
  name: 'Campagne de démonstration',
  academicYear: 'DEVELOPMENT',
  startsAt: '2000-01-01',
  endsAt: '2100-01-01',
  status: 'OPEN',
}
const qualification = { code: 'DT_DWM', name: 'DT/DWM', family: 'DT' }
const program = { code: 'GL', name: 'Génie Logiciel' }
let stored: Draft
let wrapper: VueWrapper | undefined
beforeEach(() => {
  vi.resetAllMocks()
  stored = {
    id: 'draft',
    profile: { lastName: '', firstNames: '', email: '', phone: '' },
    campaign,
    qualification: null,
    graduationYear: null,
    program: null,
    state: 'DRAFT',
    version: 1,
    updatedAt: '2026-10-12',
    sessionExpiresAt: '2026-10-19',
  }
  vi.mocked(draftApi.current).mockRejectedValue(
    new ApiError('DRAFT_SESSION_REQUIRED', 'Pas de session', {}, 401),
  )
  vi.mocked(draftApi.campaign).mockResolvedValue(campaign)
  vi.mocked(draftApi.qualifications).mockResolvedValue([qualification])
  vi.mocked(draftApi.programs).mockResolvedValue([program])
  vi.mocked(draftApi.create).mockImplementation(async () => structuredClone(stored))
  vi.mocked(draftApi.patch).mockImplementation(async (kind, value) => {
    const input = value as {
      profile?: Draft['profile']
      qualification?: string
      graduationYear?: number
      program?: string
    }
    if (input.profile) stored.profile = input.profile
    if (kind === 'academic-background') {
      stored.qualification = qualification
      stored.graduationYear = input.graduationYear ?? null
    }
    if (input.program) stored.program = program
    stored.version++
    return structuredClone(stored)
  })
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  vi.useRealTimers()
})
async function page(start = true) {
  wrapper = mount(CandidateApplicationPage, {
    global: { plugins: [createPinia()] },
    attachTo: document.body,
  })
  await flushPromises()
  if (start) {
    await wrapper
      .findAll('button')
      .find((b) => b.text().includes('Commencer'))!
      .trigger('click')
    await flushPromises()
  }
  return wrapper
}
async function identity(w: VueWrapper) {
  for (const [id, value] of Object.entries({
    lastName: 'Dossou',
    firstNames: 'Élise',
    email: 'elise@example.com',
    phone: '+33123456789',
  }))
    await w.get(`#${id}`).setValue(value)
  await w.get('form').trigger('submit')
  await flushPromises()
}
async function academic(w: VueWrapper) {
  await w.get('#qualification').setValue('DT_DWM')
  await w.get('#graduationYear').setValue(String(new Date().getFullYear() - 1))
  await w.get('form').trigger('submit')
  await flushPromises()
}
describe('Candidate journey', () => {
  it('shows four steps and loads qualifications', async () => {
    const w = await page()
    expect(w.get('nav[aria-label="Étapes du brouillon"]').findAll('li')).toHaveLength(4)
    expect(w.text()).toContain('Vos informations')
    expect(w.text()).not.toContain('Soumettre')
    expect(draftApi.qualifications).toHaveBeenCalledOnce()
  })
  it('validates profile and focuses first error', async () => {
    const w = await page()
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.text()).toContain('Renseignez votre nom.')
    expect(document.activeElement?.id).toBe('lastName')
    expect(draftApi.patch).not.toHaveBeenCalled()
  })
  it('saves profile before advancing', async () => {
    const w = await page()
    await identity(w)
    expect(w.get('#step-title').text()).toBe('Votre parcours')
    expect(w.get('select').text()).toContain('DT/DWM')
    expect(draftApi.patch).toHaveBeenCalledWith('profile', expect.objectContaining({ version: 1 }))
  })
  it('shows eligible programs and saves selection', async () => {
    const w = await page()
    await identity(w)
    await academic(w)
    expect(draftApi.programs).toHaveBeenCalledWith('DEV_LOCAL', 'DT_DWM')
    expect(w.findAll('input[type="radio"]')).toHaveLength(1)
    await w.get('input[type="radio"]').setValue()
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.text()).toContain('Votre candidature reste un brouillon.')
    expect(w.text()).toContain('Génie Logiciel')
  })
  it('explains no eligible programs', async () => {
    vi.mocked(draftApi.programs).mockResolvedValue([])
    const w = await page()
    await identity(w)
    await academic(w)
    expect(w.text()).toContain('Aucune filière proposée pour cette qualification.')
    expect(w.find('input[type="radio"]').exists()).toBe(false)
  })
  it('restores without duplication and permits editing', async () => {
    stored.profile = {
      lastName: 'Dossou',
      firstNames: 'Élise',
      email: 'elise@example.com',
      phone: '+33123456789',
    }
    stored.qualification = qualification
    stored.graduationYear = 2025
    stored.program = program
    vi.mocked(draftApi.current).mockResolvedValue(structuredClone(stored))
    const w = await page(false)
    expect(w.text()).toContain('elise@example.com')
    expect(draftApi.create).not.toHaveBeenCalled()
    await w
      .findAll('button')
      .find((b) => b.text() === 'Modifier les informations')!
      .trigger('click')
    await flushPromises()
    expect((w.get('#firstNames').element as HTMLInputElement).value).toBe('Élise')
  })
  it('retains values on API failure', async () => {
    const w = await page()
    vi.mocked(draftApi.patch).mockRejectedValue(
      new ApiError('NETWORK_ERROR', 'Connexion indisponible.'),
    )
    await identity(w)
    expect(w.text()).toContain('Connexion indisponible.')
    expect(w.get('#step-title').text()).toBe('Vos informations')
  })
  it('shows campaign closed without form', async () => {
    vi.mocked(draftApi.campaign).mockRejectedValue(
      new ApiError('NO_CURRENT_CAMPAIGN', 'Fermée', {}, 404),
    )
    const w = await page(false)
    expect(w.text()).toContain('Les candidatures ne sont actuellement pas ouvertes.')
    expect(w.find('form').exists()).toBe(false)
    expect(draftApi.create).not.toHaveBeenCalled()
  })
  it('explains expiration without retries', async () => {
    vi.mocked(draftApi.current).mockRejectedValue(
      new ApiError('DRAFT_SESSION_EXPIRED', 'Expirée', {}, 401),
    )
    const w = await page(false)
    expect(w.text()).toContain('Votre session a expiré')
    expect(draftApi.current).toHaveBeenCalledOnce()
    expect(draftApi.create).not.toHaveBeenCalled()
  })
  it('debounces saves and uses latest version', async () => {
    const w = await page()
    vi.useFakeTimers()
    await w.get('#lastName').setValue('Dos')
    await w.get('#lastName').setValue('Dossou')
    expect(draftApi.patch).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(800)
    await flushPromises()
    expect(draftApi.patch).toHaveBeenCalledOnce()
    expect(w.text()).toContain('Enregistré')
    await w.get('#firstNames').setValue('Élise')
    await vi.advanceTimersByTimeAsync(800)
    await flushPromises()
    expect(draftApi.patch).toHaveBeenLastCalledWith(
      'profile',
      expect.objectContaining({ version: 2 }),
    )
  })
})
