import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CandidateApplicationPage from './CandidateApplicationPage.vue'
import { draftApi, type Draft } from '@/features/application/api'
import { ApiError } from '@/lib/api'
import { analysisApi } from '@/features/analysis/api'
vi.mock('@/features/analysis/api', () => ({ analysisApi: { current: vi.fn(), decide: vi.fn() } }))
import { documentsApi } from '@/features/documents/api'
vi.mock('@/features/documents/api', () => ({
  documentsApi: {
    list: vi.fn(),
    upload: vi.fn(),
    content: (type: string) => '/api/v1/applications/drafts/current/documents/' + type + '/content',
  },
}))
import { submissionApi } from '@/features/submission/api'
vi.mock('@/features/submission/api', async (original) => ({
  ...(await original<typeof import('@/features/submission/api')>()),
  submissionApi: { submit: vi.fn(), receipt: vi.fn() },
}))
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
let router: Router
beforeEach(() => {
  vi.resetAllMocks()
  // jsdom has no modal dialog: record showModal()/close() and refuse the non-modal show().
  HTMLDialogElement.prototype.showModal = vi.fn(function (this: HTMLDialogElement) {
    this.setAttribute('open', '')
  })
  HTMLDialogElement.prototype.close = vi.fn(function (this: HTMLDialogElement) {
    this.removeAttribute('open')
    this.dispatchEvent(new Event('close'))
  })
  HTMLDialogElement.prototype.show = vi.fn(() => {
    throw new Error('show() is not modal')
  })
  vi.mocked(analysisApi.current).mockResolvedValue({
    id: 'analysis',
    documentVersionId: 'v1',
    status: 'SUCCEEDED',
    candidateConfirmation: 'CONFIRMED',
    subjects: [],
    verificationReady: true,
  })
  vi.mocked(documentsApi.list).mockImplementation(async () => ({
    documents: [],
    complete: false,
    maxBytes: 10485760,
    applicationVersion: stored.version,
  }))
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
  const stub = { render: () => null }
  router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/candidature', name: 'candidate-application', component: stub },
      { path: '/candidature/confirmation', name: 'candidate-confirmation', component: stub },
    ],
  })
  await router.push('/candidature')
  wrapper = mount(CandidateApplicationPage, {
    global: { plugins: [createPinia(), router] },
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
  it('shows five steps and loads qualifications', async () => {
    const w = await page()
    expect(w.get('nav[aria-label="Étapes du brouillon"]').findAll('li')).toHaveLength(5)
    expect(w.text()).toContain('Vos informations')
    expect(w.text()).not.toContain('Soumettre')
    expect(w.text()).not.toContain('Analyser mon relevé')
    expect(w.get('nav').text()).not.toContain('Analyse du relevé')
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
    expect(w.text()).toContain('Quatre documents sont nécessaires.')
    expect(stored.program?.code).toBe('GL')
  })

  it('advances to documents after program autosave has completed', async () => {
    const w = await page()
    await identity(w)
    await academic(w)
    vi.useFakeTimers()
    await w.get('input[type="radio"]').setValue()
    await vi.advanceTimersByTimeAsync(800)
    await flushPromises()
    expect(w.get('#step-title').text()).toBe('Filière souhaitée')
    expect(w.text()).toContain('Enregistré')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.get('#step-title').text()).toBe('Documents')
    expect(w.text()).toContain('Quatre documents sont nécessaires.')
  })

  it('requires server completion before opening verification', async () => {
    stored.profile = {
      lastName: 'Validation',
      firstNames: 'Test',
      email: 'test@example.invalid',
      phone: '+33123456789',
    }
    stored.qualification = qualification
    stored.graduationYear = 2025
    stored.program = program
    vi.mocked(draftApi.current).mockResolvedValue(structuredClone(stored))
    vi.mocked(documentsApi.list).mockImplementation(async () => ({
      documents: [
        {
          type: 'TRANSCRIPT',
          label: 'Relevé',
          required: true,
          uploaded: true,
          version: 1,
          filename: 'synthetic.pdf',
          sizeBytes: 100,
        },
      ],
      complete: false,
      maxBytes: 10485760,
      applicationVersion: stored.version,
    }))
    const w = await page(false)
    const next = () => w.findAll('button').find((b) => b.text() === 'Voir le récapitulatif')!
    expect(next().attributes('disabled')).toBeDefined()
    const component = w.findComponent({ name: 'DocumentsStep' })
    stored.version++
    vi.mocked(documentsApi.list).mockImplementation(async () => ({
      documents: [
        {
          type: 'TRANSCRIPT',
          label: 'Relevé',
          required: true,
          uploaded: true,
          version: 1,
          filename: 'synthetic.pdf',
          sizeBytes: 100,
        },
      ],
      complete: true,
      maxBytes: 10485760,
      applicationVersion: stored.version,
    }))
    component.vm.$emit('saved', {
      documents: [
        {
          type: 'TRANSCRIPT',
          label: 'Relevé',
          required: true,
          uploaded: true,
          version: 1,
          filename: 'synthetic.pdf',
          sizeBytes: 100,
        },
      ],
      complete: true,
      maxBytes: 10485760,
      applicationVersion: stored.version,
    })
    await flushPromises()
    await next().trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Informations du relevé confirmées')
    expect(w.text()).toContain('Soumission définitive')
    expect(w.text()).not.toContain('moyenne')
  })
  it('automatically displays pending analysis inside transcript card after restoration', async () => {
    stored.profile = {
      lastName: 'Validation',
      firstNames: 'Test',
      email: 'test@example.invalid',
      phone: '+33123456789',
    }
    stored.qualification = qualification
    stored.graduationYear = 2025
    stored.program = program
    vi.mocked(draftApi.current).mockResolvedValue(structuredClone(stored))
    vi.mocked(documentsApi.list).mockResolvedValue({
      documents: [
        {
          type: 'TRANSCRIPT',
          label: 'Relevé',
          required: true,
          uploaded: true,
          version: 1,
          filename: 'synthetic.pdf',
          sizeBytes: 100,
        },
      ],
      complete: false,
      maxBytes: 10485760,
      applicationVersion: stored.version,
    })
    vi.mocked(analysisApi.current).mockResolvedValue({
      id: 'analysis',
      documentVersionId: 'v1',
      status: 'PROCESSING',
      candidateConfirmation: 'PENDING',
      subjects: [],
      verificationReady: false,
    })
    const w = await page(false)
    const card = w.get('.document-row')
    expect(card.text()).toContain('Analyse de votre relevé en cours')
    expect(w.text()).not.toContain('Analyser mon relevé')
    expect(w.get('#step-title').text()).toBe('Documents')
    expect(draftApi.create).not.toHaveBeenCalled()
    expect(
      w
        .findAll('button')
        .find((b) => b.text() === 'Voir le récapitulatif')!
        .attributes('disabled'),
    ).toBeDefined()
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
    vi.mocked(documentsApi.list).mockImplementation(async () => ({
      documents: [],
      complete: true,
      maxBytes: 10485760,
      applicationVersion: stored.version,
    }))
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

const complete = ['REQUEST_LETTER', 'TRANSCRIPT', 'IDENTITY_DOCUMENT', 'PARENTAL_COMMITMENT'].map(
  (type) => ({
    type,
    label: type,
    required: true,
    uploaded: true,
    version: 1,
    filename: 'synthetic.pdf',
    sizeBytes: 100,
  }),
)
const receipt = {
  applicationNumber: 'IFRI-2026-GL-000042',
  submittedAt: '2026-10-14T09:30:00Z',
  candidate: { lastName: 'Dossou', firstNames: 'Élise' },
  campaign: { code: 'DEV_LOCAL', name: 'Campagne de démonstration', academicYear: '2026-2027' },
  program: { code: 'GL', name: 'Génie Logiciel' },
  qualification: { code: 'DT_DWM', name: 'DT/DWM' },
  graduationYear: 2025,
  sessionExpiresAt: '2027-06-30T00:00:00Z',
}
// A restored, complete draft opens directly on the Verification step.
async function verification() {
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
  vi.mocked(documentsApi.list).mockImplementation(async () => ({
    documents: complete,
    complete: true,
    maxBytes: 10485760,
    applicationVersion: stored.version,
  }))
  const w = await page(false)
  expect(w.get('#step-title').text()).toBe('Vérifiez et soumettez')
  return w
}
const submitButton = (w: VueWrapper) => w.get('#submit-application')
const dialogButton = (w: VueWrapper, label: string) =>
  w
    .get('dialog')
    .findAll('button')
    .find((b) => b.text() === label)!

describe('Submission', () => {
  it('shows a complete review without the average and a single submit action', async () => {
    const w = await verification()
    for (const value of ['Dossou', 'elise@example.com', 'DT/DWM', '2025', 'Génie Logiciel'])
      expect(w.text()).toContain(value)
    expect(w.text()).not.toMatch(/moyenne/i)
    expect(submitButton(w).text()).toBe('Soumettre ma candidature')
    expect(submitButton(w).attributes('disabled')).toBeUndefined()
    expect(w.get('dialog').attributes('open')).toBeUndefined()
  })

  it('confirms in a modal dialog: initial focus, Escape and focus restoration', async () => {
    const w = await verification()
    ;(submitButton(w).element as HTMLButtonElement).focus()
    await submitButton(w).trigger('click')
    await flushPromises()
    const dialog = w.get('dialog')
    expect(HTMLDialogElement.prototype.showModal).toHaveBeenCalledOnce()
    expect(HTMLDialogElement.prototype.show).not.toHaveBeenCalled()
    expect(dialog.attributes('role')).toBe('alertdialog')
    expect(dialog.text()).toContain(
      'Après soumission, vous ne pourrez plus modifier votre candidature',
    )
    expect(document.getElementById(dialog.attributes('aria-describedby')!)?.textContent).toContain(
      'Après soumission',
    )
    expect(document.activeElement?.textContent).toBe('Annuler')
    // Escape: the browser fires "cancel", then closes the dialog.
    const escape = new Event('cancel', { cancelable: true })
    dialog.element.dispatchEvent(escape)
    expect(escape.defaultPrevented).toBe(false)
    ;(dialog.element as HTMLDialogElement).close()
    await flushPromises()
    expect(dialog.attributes('open')).toBeUndefined()
    expect(document.activeElement?.id).toBe('submit-application')
    expect(submissionApi.submit).not.toHaveBeenCalled()
    // Annuler closes the same way.
    await submitButton(w).trigger('click')
    await flushPromises()
    await dialogButton(w, 'Annuler').trigger('click')
    await flushPromises()
    expect(w.get('dialog').attributes('open')).toBeUndefined()
    expect(document.activeElement?.id).toBe('submit-application')
  })

  it('shows the loading state, blocks Escape while sending, then opens the confirmation', async () => {
    const w = await verification()
    let resolve!: (value: typeof receipt) => void
    vi.mocked(submissionApi.submit).mockReturnValue(new Promise((done) => (resolve = done)))
    await submitButton(w).trigger('click')
    await flushPromises()
    await dialogButton(w, 'Confirmer la soumission').trigger('click')
    await flushPromises()
    expect(submissionApi.submit).toHaveBeenCalledWith(stored.version)
    expect(w.get('dialog').attributes('aria-busy')).toBe('true')
    expect(dialogButton(w, 'Soumission en cours…').attributes('disabled')).toBeDefined()
    expect(dialogButton(w, 'Annuler').attributes('disabled')).toBeDefined()
    const escape = new Event('cancel', { cancelable: true })
    w.get('dialog').element.dispatchEvent(escape)
    expect(escape.defaultPrevented).toBe(true)
    resolve(receipt)
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('candidate-confirmation')
    expect(submissionApi.submit).toHaveBeenCalledOnce()
  })

  it('returns to Documents when admission rules changed, where the new analysis must be confirmed', async () => {
    const w = await verification()
    vi.mocked(submissionApi.submit).mockRejectedValue(
      new ApiError('ADMISSION_RULES_CHANGED', 'Règles modifiées', {}, 409),
    )
    vi.mocked(analysisApi.current).mockResolvedValue({
      id: 'reanalysis',
      documentVersionId: 'v1',
      status: 'QUEUED',
      candidateConfirmation: 'PENDING',
      subjects: [],
      verificationReady: false,
    })
    await submitButton(w).trigger('click')
    await flushPromises()
    await dialogButton(w, 'Confirmer la soumission').trigger('click')
    await flushPromises()
    expect(w.get('#step-title').text()).toBe('Documents')
    expect(document.activeElement?.id).toBe('step-title')
    expect(w.text()).toContain('Les règles d’admission de votre filière ont changé')
    const transcript = w
      .findAll('.document-row')
      .find((row) => row.find('#file-TRANSCRIPT').exists())!
    expect(transcript.text()).toContain('Analyse de votre relevé en cours')
    const next = w.findAll('button').find((b) => b.text() === 'Voir le récapitulatif')!
    expect(next.attributes('disabled')).toBeDefined()
    expect(router.currentRoute.value.name).toBe('candidate-application')
  })

  it('explains each refusal in French with the next action', async () => {
    for (const [code, text, title] of [
      ['VERSION_CONFLICT', 'modifié dans un autre onglet', 'Vérifiez et soumettez'],
      ['CAMPAIGN_CLOSED', 'La campagne est close', 'Vérifiez et soumettez'],
      ['APPLICATION_NOT_ELIGIBLE', 'Choisissez une autre filière', 'Filière souhaitée'],
      ['TRANSCRIPT_ANALYSIS_OUTDATED', 'confirmez la nouvelle analyse', 'Documents'],
      ['NETWORK_ERROR', 'le même numéro vous sera affiché', 'Vérifiez et soumettez'],
    ] as const) {
      vi.mocked(submissionApi.submit).mockRejectedValue(new ApiError(code, 'serveur', {}, 409))
      const w = await verification()
      await submitButton(w).trigger('click')
      await flushPromises()
      await dialogButton(w, 'Confirmer la soumission').trigger('click')
      await flushPromises()
      expect(w.text()).toContain(text)
      expect(w.get('#step-title').text()).toBe(title)
      expect(router.currentRoute.value.name).toBe('candidate-application')
      if (code === 'VERSION_CONFLICT' || code === 'CAMPAIGN_CLOSED')
        expect(w.text()).toContain('Recharger le brouillon')
      wrapper?.unmount()
      wrapper = undefined
    }
  })

  it('sends an incomplete application back to the first incomplete step', async () => {
    const w = await verification()
    vi.mocked(submissionApi.submit).mockRejectedValue(
      new ApiError(
        'APPLICATION_INCOMPLETE',
        'Incomplet',
        { phone: 'Renseignez votre numéro de téléphone.', 'document.TRANSCRIPT': 'Ajoutez-le.' },
        422,
      ),
    )
    await submitButton(w).trigger('click')
    await flushPromises()
    await dialogButton(w, 'Confirmer la soumission').trigger('click')
    await flushPromises()
    expect(w.get('#step-title').text()).toBe('Vos informations')
    expect(w.text()).toContain('Votre dossier est incomplet')
    expect(w.text()).toContain('Renseignez votre numéro de téléphone.')
  })

  it('redirects a submitted application to its confirmation without creating a draft', async () => {
    stored.state = 'SUBMITTED'
    stored.applicationNumber = receipt.applicationNumber
    vi.mocked(draftApi.current).mockResolvedValue(structuredClone(stored))
    await page(false)
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('candidate-confirmation')
    expect(draftApi.create).not.toHaveBeenCalled()
    expect(documentsApi.list).not.toHaveBeenCalled()
    expect(analysisApi.current).not.toHaveBeenCalled()
  })

  it('treats APPLICATION_LOCKED from another tab as a submitted application', async () => {
    const w = await verification()
    vi.mocked(submissionApi.submit).mockRejectedValue(
      new ApiError('APPLICATION_LOCKED', 'Soumise', {}, 409),
    )
    stored.state = 'SUBMITTED'
    vi.mocked(draftApi.current).mockResolvedValue(structuredClone(stored))
    await submitButton(w).trigger('click')
    await flushPromises()
    await dialogButton(w, 'Confirmer la soumission').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('candidate-confirmation')
  })
})
