import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import type { Component } from 'vue'
import { adminApi, type AdminSession } from '@/features/admin/api'
import { adminGuard } from '@/features/admin/guard'
import {
  exactNumber,
  reviewApi,
  type CorrectionView,
  type Detail,
  type ListItem,
  type Reference,
} from '@/features/review/api'
import { ApiError } from '@/lib/api'
import AdminApplicationsPage from './AdminApplicationsPage.vue'
import AdminApplicationPage from './AdminApplicationPage.vue'
import AdminCorrectionsPage from './AdminCorrectionsPage.vue'

vi.mock('@/features/admin/api', async (original) => {
  const actual = await original<typeof import('@/features/admin/api')>()
  const fns = Object.fromEntries(Object.keys(actual.adminApi).map((key) => [key, vi.fn()]))
  return { ...actual, adminApi: fns }
})
vi.mock('@/features/review/api', async (original) => {
  const actual = await original<typeof import('@/features/review/api')>()
  const fns = Object.fromEntries(Object.keys(actual.reviewApi).map((key) => [key, vi.fn()]))
  return { ...actual, reviewApi: fns }
})

const agentPermissions = [
  'applications.read',
  'corrections.propose',
  'dashboard.view',
  'ranking.read',
  'review.claim',
  'review.decide',
]
const deputyPermissions = [
  'applications.read',
  'corrections.decide',
  'dashboard.view',
  'ranking.read',
]
const session = (id: string, permissions: string[]): AdminSession => ({
  id,
  stage: 'ACTIVE',
  csrfToken: 'csrf',
  email: `${id}@ifri.example`,
  displayName: id,
  permissions,
  grants: [{ role: 'REVIEWER' }],
  idleExpiresAt: '2026-10-14T12:00:00Z',
  expiresAt: '2026-10-14T18:00:00Z',
  mfaRequired: true,
})
const reference: Reference = {
  rejectionReasons: [
    { code: 'DOCUMENT_ILLEGIBLE', label: 'Pièce illisible' },
    { code: 'OTHER', label: 'Autre motif' },
  ],
  agents: [
    { id: 'agent-a', name: 'Agent A' },
    { id: 'agent-b', name: 'Agent B' },
  ],
  programs: [
    { code: 'GL', label: 'Génie Logiciel' },
    { code: 'SI', label: 'Sécurité Informatique' },
  ],
  qualifications: [{ code: 'BAC_D', label: 'BAC D' }],
}
const item = (overrides: Partial<ListItem> = {}): ListItem => ({
  id: 'app-1',
  applicationNumber: 'IFRI-2026-00001',
  submittedAt: '2026-10-14T08:00:00Z',
  lastName: 'GBÈDJI',
  firstNames: 'Élodie',
  qualification: 'BAC_D',
  qualificationLabel: 'BAC D',
  program: 'GL',
  programName: 'Génie Logiciel',
  average: {
    display: '12.53',
    decimal: '12.533333333333333333333333333333',
    exact: false,
    weightedSum: '188',
    coefficientSum: '15',
  },
  status: 'TO_REVIEW',
  assignee: null,
  flags: ['DUPLICATE_EMAIL'],
  pendingCorrections: 0,
  ...overrides,
})
const average = (weightedSum: string, coefficientSum: string, display: string) => ({
  display,
  decimal: display,
  exact: true,
  weightedSum,
  coefficientSum,
})
const correction = (overrides: Partial<CorrectionView> = {}): CorrectionView => ({
  id: 'corr-1',
  applicationId: 'app-1',
  applicationNumber: 'IFRI-2026-00001',
  candidate: 'GBÈDJI Élodie',
  subjectId: 'math',
  subjectName: 'Mathématiques',
  field: 'GRADE',
  originalValue: '12',
  currentValue: '12',
  proposedValue: '14',
  reason: 'La note lue est 14 sur le relevé.',
  proposedBy: { id: 'agent-a', name: 'Agent A' },
  proposedAt: new Date(Date.now() - 2 * 3600 * 1000).toISOString(),
  status: 'PENDING',
  decidedBy: null,
  decidedAt: null,
  reviewVersion: 3,
  ...overrides,
})
const detail = (overrides: Partial<Detail> = {}): Detail => ({
  id: 'app-1',
  applicationNumber: 'IFRI-2026-00001',
  submittedAt: '2026-10-14T08:00:00Z',
  campaign: 'DEV_LOCAL',
  lastName: 'GBÈDJI',
  firstNames: 'Élodie',
  email: 'elodie@example.invalid',
  phone: '+22990000001',
  qualification: 'BAC_D',
  qualificationLabel: 'BAC D',
  graduationYear: 2026,
  program: 'GL',
  programName: 'Génie Logiciel',
  declaredSeries: 'D',
  readSeries: 'D',
  snapshotAverage: average('180', '15', '12'),
  effectiveAverage: average('180', '15', '12'),
  status: 'TO_REVIEW',
  assignee: null,
  version: 1,
  frozen: false,
  documents: [
    { type: 'TRANSCRIPT', filename: 'releve.pdf', sizeBytes: 204800, version: 2 },
    { type: 'IDENTITY_DOCUMENT', filename: 'cni.pdf', sizeBytes: 102400, version: 1 },
  ],
  subjects: [
    {
      subjectId: 'math',
      code: 'MATH',
      name: 'Mathématiques',
      position: 1,
      gradeRaw: '12,00',
      grade: '12',
      coefficientRaw: '5',
      coefficient: '5',
      effectiveGrade: '12',
      effectiveCoefficient: '5',
      validationMethod: 'ROW_ARITHMETIC',
      reconstructedCells: [],
      proofStatus: '',
    },
    {
      subjectId: 'pc',
      code: 'PC',
      name: 'Physique-Chimie',
      position: 2,
      gradeRaw: '12',
      grade: '12',
      coefficientRaw: '10',
      coefficient: '10',
      effectiveGrade: '12',
      effectiveCoefficient: '10',
      validationMethod: 'TABLE_TOTALS',
      reconstructedCells: ['POINTS'],
      proofStatus: 'READY',
    },
  ],
  corrections: [],
  history: [
    {
      version: 1,
      type: 'CREATED',
      actor: null,
      toStatus: 'TO_REVIEW',
      occurredAt: '2026-10-14T08:00:00Z',
    },
  ],
  flags: [
    {
      code: 'DUPLICATE_EMAIL',
      direction: 'OUTGOING',
      applicationId: 'app-0',
      applicationNumber: 'IFRI-2026-00000',
    },
    {
      code: 'DUPLICATE_PHONE',
      direction: 'INCOMING',
      applicationId: 'app-2',
      applicationNumber: 'IFRI-2026-00002',
    },
  ],
  ...overrides,
})
const inReview = (assignee: string, overrides: Partial<Detail> = {}) =>
  detail({
    status: 'IN_REVIEW',
    assignee: { id: assignee, name: assignee === 'agent-a' ? 'Agent A' : 'Agent B' },
    version: 2,
    ...overrides,
  })

let router: Router
let wrapper: VueWrapper | undefined
beforeEach(() => {
  vi.resetAllMocks()
  setActivePinia(createPinia())
  HTMLDialogElement.prototype.showModal = vi.fn(function (this: HTMLDialogElement) {
    this.setAttribute('open', '')
  })
  HTMLDialogElement.prototype.close = vi.fn(function (this: HTMLDialogElement) {
    this.removeAttribute('open')
    this.dispatchEvent(new Event('close'))
  })
  vi.mocked(reviewApi.reference).mockResolvedValue(reference)
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
})
async function open(page: Component, path: string, current: AdminSession) {
  vi.mocked(adminApi.session).mockResolvedValue(current)
  const stub = { render: () => null }
  router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/admin',
        name: 'admin-dashboard',
        component: stub,
        meta: { admin: 'dashboard.view' },
      },
      { path: '/admin/connexion', name: 'admin-login', component: stub, meta: { admin: 'public' } },
      { path: '/admin/compte', name: 'admin-account', component: stub, meta: { admin: 'self' } },
      {
        path: '/admin/candidatures',
        name: 'admin-applications',
        component: stub,
        meta: { admin: 'applications.read' },
      },
      {
        path: '/admin/candidatures/:id',
        name: 'admin-application',
        component: stub,
        meta: { admin: 'applications.read' },
      },
      {
        path: '/admin/corrections',
        name: 'admin-corrections',
        component: stub,
        meta: { admin: 'corrections.decide' },
      },
    ],
  })
  router.beforeEach(adminGuard)
  await router.push(path)
  wrapper = mount(page, { global: { plugins: [router] }, attachTo: document.body })
  await flushPromises()
  return wrapper
}
const button = (w: VueWrapper, label: string) =>
  w.findAll('button').find((b) => b.text().includes(label))
const openDialog = (w: VueWrapper) =>
  w.findAll('dialog').find((d) => d.attributes('open') !== undefined)!

describe('applications list', () => {
  it('reads the filters from the URL, sorts and pages on the server, hides contact details', async () => {
    vi.mocked(reviewApi.list).mockResolvedValue({
      items: [item()],
      total: 60,
      page: 1,
      pageSize: 25,
    })
    const w = await open(
      AdminApplicationsPage,
      '/admin/candidatures?q=gbedji&status=TO_REVIEW&sort=average:desc',
      session('agent-a', agentPermissions),
    )
    expect(reviewApi.list).toHaveBeenCalledWith({
      q: 'gbedji',
      program: '',
      qualification: '',
      status: 'TO_REVIEW',
      flag: '',
      assignee: '',
      pendingCorrection: false,
      sort: 'average',
      direction: 'desc',
      page: 1,
      pageSize: 25,
    })
    expect((w.get('#filter-q').element as HTMLInputElement).value).toBe('gbedji')
    const row = w.get('tbody tr')
    expect(row.text()).toContain('IFRI-2026-00001')
    expect(row.text()).toContain('12,53')
    expect(row.text()).toContain('Email déjà utilisé')
    expect(w.text()).not.toContain('@example')
    expect(w.text()).toContain('la fraction exacte, visible dans le dossier, fait foi')
    expect(w.get('th[aria-sort]').text()).toBe('Moyenne')
    expect(w.get('th[aria-sort]').attributes('aria-sort')).toBe('descending')
    expect(w.text()).toContain('Page 1 sur 3')

    await button(w, 'Suivante')!.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({
      q: 'gbedji',
      status: 'TO_REVIEW',
      sort: 'average:desc',
      page: '2',
    })
    expect(vi.mocked(reviewApi.list).mock.lastCall![0].page).toBe(2)

    // A new search starts again at page 1; the URL keeps the view for back navigation.
    await w.get('#filter-program').setValue('SI')
    await w.get('#filter-assignee').setValue('me')
    await w.get('form[role="search"]').trigger('submit')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({
      q: 'gbedji',
      program: 'SI',
      status: 'TO_REVIEW',
      assignee: 'me',
      sort: 'average:desc',
    })
    expect(vi.mocked(reviewApi.list).mock.lastCall![0]).toMatchObject({
      program: 'SI',
      assignee: 'me',
      page: 1,
    })
  })

  it('shows a useful empty state that resets the filters', async () => {
    vi.mocked(reviewApi.list).mockResolvedValue({ items: [], total: 0, page: 1, pageSize: 25 })
    const w = await open(
      AdminApplicationsPage,
      '/admin/candidatures?q=inconnu',
      session('agent-a', agentPermissions),
    )
    expect(w.text()).toContain('Aucun dossier ne correspond à ces critères')
    await w
      .findAll('button')
      .filter((b) => b.text().includes('Réinitialiser'))[1]!
      .trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({})
  })
})

describe('dossier review', () => {
  it('lets an agent claim a dossier, then validate it', async () => {
    vi.mocked(reviewApi.detail).mockResolvedValue(detail())
    const w = await open(
      AdminApplicationPage,
      '/admin/candidatures/app-1',
      session('agent-a', agentPermissions),
    )
    expect(w.text()).toContain('elodie@example.invalid')
    expect(w.text()).toContain('Déclarée : D · Lue : D')
    expect(button(w, 'Valider le dossier')).toBeUndefined()
    vi.mocked(reviewApi.transition).mockResolvedValue(inReview('agent-a'))
    await button(w, 'Prendre en charge')!.trigger('click')
    await flushPromises()
    expect(reviewApi.transition).toHaveBeenCalledWith('app-1', 'claim', { version: 1 })
    expect(w.text()).toContain('Dossier pris en charge')

    await button(w, 'Valider le dossier')!.trigger('click')
    await flushPromises()
    expect(openDialog(w).text()).toContain('Valider ce dossier ?')
    vi.mocked(reviewApi.transition).mockResolvedValue(
      inReview('agent-a', { status: 'VALIDATED', version: 3 }),
    )
    await w.get('form#review-dialog-form').trigger('submit')
    await flushPromises()
    expect(reviewApi.transition).toHaveBeenLastCalledWith('app-1', 'validate', {
      version: 2,
      reasonCode: undefined,
      comment: undefined,
      assignee: undefined,
    })
    expect(w.text()).toContain('Dossier validé.')
  })

  it('offers no decision to an agent who does not hold the dossier', async () => {
    vi.mocked(reviewApi.detail).mockResolvedValue(inReview('agent-a'))
    const w = await open(
      AdminApplicationPage,
      '/admin/candidatures/app-1',
      session('agent-b', agentPermissions),
    )
    expect(w.text()).toContain('Pris en charge par Agent A')
    for (const label of ['Valider le dossier', 'Rejeter', 'Corriger', 'Prendre en charge'])
      expect(button(w, label)).toBeUndefined()
  })

  it('blocks validation while a correction waits for the deputy director', async () => {
    vi.mocked(reviewApi.detail).mockResolvedValue(
      inReview('agent-a', { corrections: [correction()] }),
    )
    const w = await open(
      AdminApplicationPage,
      '/admin/candidatures/app-1',
      session('agent-a', agentPermissions),
    )
    const validate = button(w, 'Valider le dossier')!
    expect(validate.attributes('disabled')).toBeDefined()
    expect(validate.attributes('aria-describedby')).toBe('validation-blocked')
    expect(w.get('#validation-blocked').text()).toContain('la validation est impossible')
    expect(w.text()).toContain('Note — Mathématiques : 12 → 14')
  })

  it('proposes a correction with a reason; the API receives the raw value', async () => {
    vi.mocked(reviewApi.detail).mockResolvedValue(inReview('agent-a'))
    const w = await open(
      AdminApplicationPage,
      '/admin/candidatures/app-1',
      session('agent-a', agentPermissions),
    )
    await w.get('button[aria-label="Corriger : Mathématiques"]').trigger('click')
    await flushPromises()
    await w.get('form#review-dialog-form').trigger('submit')
    await flushPromises()
    expect(reviewApi.propose).not.toHaveBeenCalled()
    expect(openDialog(w).text()).toContain('Saisissez la valeur proposée.')
    await w.get('#correction-value').setValue('14,5')
    await w.get('#dialog-comment').setValue('Le relevé indique 14,50.')
    vi.mocked(reviewApi.propose).mockResolvedValue(
      inReview('agent-a', { version: 3, corrections: [correction({ proposedValue: '14.5' })] }),
    )
    await w.get('form#review-dialog-form').trigger('submit')
    await flushPromises()
    expect(reviewApi.propose).toHaveBeenCalledWith('app-1', {
      version: 2,
      subjectId: 'math',
      field: 'GRADE',
      value: '14,5',
      reason: 'Le relevé indique 14,50.',
    })
    expect(w.text()).toContain('elle attend la décision du directeur adjoint')
  })

  it('requires a reason code and a comment to reject', async () => {
    vi.mocked(reviewApi.detail).mockResolvedValue(inReview('agent-a'))
    const w = await open(
      AdminApplicationPage,
      '/admin/candidatures/app-1',
      session('agent-a', agentPermissions),
    )
    await button(w, 'Rejeter')!.trigger('click')
    await flushPromises()
    expect(openDialog(w).attributes('role')).toBe('alertdialog')
    expect(document.activeElement?.textContent).toBe('Annuler')
    await w.get('form#review-dialog-form').trigger('submit')
    await flushPromises()
    expect(reviewApi.transition).not.toHaveBeenCalled()
    expect(openDialog(w).text()).toContain('Choisissez un motif de rejet.')
    await w.get('#reject-reason').setValue('DOCUMENT_ILLEGIBLE')
    await w.get('#dialog-comment').setValue('Relevé illisible malgré deux versions.')
    vi.mocked(reviewApi.transition).mockResolvedValue(
      inReview('agent-a', {
        status: 'REJECTED',
        version: 3,
        rejectionCode: 'DOCUMENT_ILLEGIBLE',
        rejectionLabel: 'Pièce illisible',
        rejectionComment: 'Relevé illisible malgré deux versions.',
      }),
    )
    await w.get('form#review-dialog-form').trigger('submit')
    await flushPromises()
    expect(reviewApi.transition).toHaveBeenCalledWith('app-1', 'reject', {
      version: 2,
      reasonCode: 'DOCUMENT_ILLEGIBLE',
      comment: 'Relevé illisible malgré deux versions.',
      assignee: undefined,
    })
    expect(w.text()).toContain('Motif du rejet : Pièce illisible')
  })

  it('reports a concurrent change and offers to reload', async () => {
    vi.mocked(reviewApi.detail).mockResolvedValue(detail())
    const w = await open(
      AdminApplicationPage,
      '/admin/candidatures/app-1',
      session('agent-b', agentPermissions),
    )
    vi.mocked(reviewApi.transition).mockRejectedValue(
      new ApiError('REVIEW_VERSION_CONFLICT', 'Le dossier vient d’être modifié.', {}, 409),
    )
    await button(w, 'Prendre en charge')!.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Le dossier a changé depuis son affichage.')
    vi.mocked(reviewApi.detail).mockResolvedValue(inReview('agent-a'))
    await button(w, 'Recharger le dossier')!.trigger('click')
    await flushPromises()
    expect(w.text()).toContain('Pris en charge par Agent A')
  })

  it('shows original and corrected values side by side with the exact average', async () => {
    const corrected = detail({
      effectiveAverage: {
        display: '12.66',
        decimal: '12.666666666666666666666666666666',
        exact: false,
        weightedSum: '190',
        coefficientSum: '15',
      },
      corrections: [
        correction({
          status: 'APPROVED',
          decidedBy: { id: 'da', name: 'Directeur adjoint' },
          decidedAt: '2026-10-14T10:00:00Z',
        }),
      ],
    })
    corrected.subjects[0]!.effectiveGrade = '14'
    vi.mocked(reviewApi.detail).mockResolvedValue(corrected)
    const w = await open(
      AdminApplicationPage,
      '/admin/candidatures/app-1',
      session('da', deputyPermissions),
    )
    const math = w.findAll('tbody tr').find((row) => row.text().includes('Mathématiques'))!
    expect(math.get('[data-label="Note du relevé"]').text()).toBe('12')
    expect(math.get('[data-label="Note retenue"]').text()).toContain('14')
    expect(math.get('[data-label="Note retenue"]').text()).toContain('corrigée')
    expect(w.text()).toContain('12,66')
    expect(w.text()).toContain('190 ÷ 15 = 12,666666666666666666666666666666')
    expect(w.text()).toContain('Moyenne au dépôt : 12')
    expect(exactNumber(average('180', '15', '12.500000000000000000000000000000'))).toBe('12,5')
    expect(exactNumber(average('180', '15', '12.000000000000000000000000000000'))).toBe('12')
    expect(w.text()).toContain('Approuvée par Directeur adjoint')
    // The deputy director reads; only the queue decides, and never proposes.
    expect(button(w, 'Corriger')).toBeUndefined()
  })

  it('previews a PDF through the authenticated stream and shows duplicates both ways', async () => {
    vi.mocked(reviewApi.detail).mockResolvedValue(detail())
    const w = await open(
      AdminApplicationPage,
      '/admin/candidatures/app-1',
      session('agent-a', agentPermissions),
    )
    expect(w.find('iframe').exists()).toBe(false)
    const preview = w
      .findAll('button')
      .find((b) => b.text().includes('Aperçu') && b.text().includes('Relevé de notes'))!
    await preview.trigger('click')
    const frame = w.get('iframe')
    expect(frame.attributes('src')).toBe(
      '/api/v1/admin/applications/app-1/documents/TRANSCRIPT/content',
    )
    expect(frame.attributes('title')).toBe('Aperçu : Relevé de notes')
    expect(preview.attributes('aria-expanded')).toBe('true')
    const tab = w.findAll('a').find((a) => a.text().includes('Nouvel onglet'))!
    expect(tab.attributes('target')).toBe('_blank')
    expect(tab.attributes('rel')).toBe('noopener noreferrer')
    expect(w.text()).toContain('Email déjà utilisé — même valeur que le dossier IFRI-2026-00000')
    expect(w.text()).toContain(
      'Le dossier IFRI-2026-00002 utilise le même numéro de téléphone que ce dossier',
    )
  })

  it('requests an OCR proof on demand and shows the private image', async () => {
    vi.mocked(reviewApi.detail).mockResolvedValue(detail())
    vi.mocked(reviewApi.proof).mockResolvedValue({ status: 'NONE' })
    vi.mocked(reviewApi.requestProof).mockResolvedValue({ status: 'READY' })
    const w = await open(
      AdminApplicationPage,
      '/admin/candidatures/app-1',
      session('agent-a', agentPermissions),
    )
    await w.get('button[aria-label="Preuve de lecture : Mathématiques"]').trigger('click')
    await flushPromises()
    expect(reviewApi.requestProof).toHaveBeenCalledWith('app-1', 'math')
    const image = w.get('#proof-panel img')
    expect(image.attributes('src')).toBe(
      '/api/v1/admin/applications/app-1/subjects/math/proof/image',
    )
    expect(image.attributes('alt')).toContain('Mathématiques')
  })
})

describe('corrections queue', () => {
  it('approves a correction and reports the recomputed average', async () => {
    vi.mocked(reviewApi.corrections).mockResolvedValue({ corrections: [correction()] })
    const w = await open(
      AdminCorrectionsPage,
      '/admin/corrections',
      session('da', deputyPermissions),
    )
    const card = w.get('article')
    expect(card.text()).toContain('Note — Mathématiques · IFRI-2026-00001')
    expect(card.text()).toContain('Proposée par Agent A')
    expect(card.text()).toContain('il y a 2 heures')
    expect(card.text()).toContain('La note lue est 14 sur le relevé.')
    vi.mocked(reviewApi.decide).mockResolvedValue(
      inReview('agent-a', { effectiveAverage: average('190', '15', '12.66') }),
    )
    await button(w, 'Approuver')!.trigger('click')
    await flushPromises()
    await openDialog(w)
      .findAll('button')
      .find((b) => b.text() === 'Approuver')!
      .trigger('click')
    await flushPromises()
    expect(reviewApi.decide).toHaveBeenCalledWith('corr-1', true, { version: 3, reason: undefined })
    expect(w.text()).toContain('Moyenne retenue du dossier IFRI-2026-00001 : 12,66')
    expect(w.text()).toContain('Aucune correction en attente')
  })

  it('requires a reason to refuse, and never offers a decision on one’s own proposal', async () => {
    vi.mocked(reviewApi.corrections).mockResolvedValue({
      corrections: [
        correction(),
        correction({ id: 'corr-2', proposedBy: { id: 'da', name: 'DA' } }),
      ],
    })
    const w = await open(
      AdminCorrectionsPage,
      '/admin/corrections',
      session('da', deputyPermissions),
    )
    const [first, own] = w.findAll('article')
    expect(own!.text()).not.toContain('Approuver')
    expect(own!.text()).not.toContain('Refuser')
    await first!
      .findAll('button')
      .find((b) => b.text().includes('Refuser'))!
      .trigger('click')
    await flushPromises()
    await w.get('form#refusal-form').trigger('submit')
    await flushPromises()
    expect(reviewApi.decide).not.toHaveBeenCalled()
    expect(openDialog(w).text()).toContain('Indiquez le motif du refus.')
    await w.get('#refusal-reason').setValue('La note du relevé est bien 12.')
    vi.mocked(reviewApi.decide).mockResolvedValue(inReview('agent-a'))
    await w.get('form#refusal-form').trigger('submit')
    await flushPromises()
    expect(reviewApi.decide).toHaveBeenCalledWith('corr-1', false, {
      version: 3,
      reason: 'La note du relevé est bien 12.',
    })
    expect(w.text()).toContain('Correction refusée pour le dossier IFRI-2026-00001.')
  })
})
