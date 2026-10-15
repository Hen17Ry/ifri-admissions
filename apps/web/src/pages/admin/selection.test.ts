import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { adminApi, type AdminSession } from '@/features/admin/api'
import { adminGuard } from '@/features/admin/guard'
import { selectionApi, type Entry, type Offer, type RankingView } from '@/features/selection/api'
import { ApiError } from '@/lib/api'
import AdminSelectionPage from './AdminSelectionPage.vue'

vi.mock('@/features/admin/api', async (original) => {
  const actual = await original<typeof import('@/features/admin/api')>()
  const fns = Object.fromEntries(Object.keys(actual.adminApi).map((key) => [key, vi.fn()]))
  return { ...actual, adminApi: fns }
})
vi.mock('@/features/selection/api', async (original) => {
  const actual = await original<typeof import('@/features/selection/api')>()
  const fns = Object.fromEntries(Object.keys(actual.selectionApi).map((key) => [key, vi.fn()]))
  return { ...actual, selectionApi: fns }
})

const permissions = {
  director: [
    'applications.read',
    'audit.read',
    'dashboard.view',
    'ranking.configure',
    'ranking.read',
    'selection.export',
    'selection.validate',
  ],
  deputy: [
    'applications.read',
    'dashboard.view',
    'ranking.read',
    'selection.export',
    'selection.manage',
  ],
  lead: ['applications.read', 'dashboard.view', 'ranking.read', 'review.claim'],
}
const session = (who: keyof typeof permissions): AdminSession => ({
  id: who,
  stage: 'ACTIVE',
  csrfToken: 'csrf',
  email: `${who}@ifri.example`,
  displayName: who,
  permissions: permissions[who],
  grants: [{ role: 'DIRECTOR' }],
  idleExpiresAt: '2026-10-14T12:00:00Z',
  expiresAt: '2026-10-14T18:00:00Z',
  mfaRequired: true,
})
const offer = (overrides: Partial<Offer> = {}): Offer => ({
  id: 'offer-gl',
  campaignCode: 'DEV_LOCAL',
  campaignName: 'Démonstration',
  programCode: 'GL',
  programName: 'Génie Logiciel',
  seats: 2,
  minimumAverage: '10.00',
  campaignOpen: false,
  endsAt: '2026-10-01T00:00:00Z',
  frozen: false,
  ...overrides,
})
const entry = (n: number, overrides: Partial<Entry> = {}): Entry => ({
  applicationId: `app-${n}`,
  applicationNumber: `IFRI-2026-GL-00000${n}`,
  lastName: `Nom${n}`,
  firstNames: 'Élodie',
  qualification: 'BAC D',
  position: n,
  rank: n,
  average: {
    display: '13.50',
    decimal: '13.5',
    exact: true,
    weightedSum: '40.5',
    coefficientSum: '3',
  },
  mathGrade: '14',
  mathTiebreak: 'NOT_NEEDED',
  submittedAt: '2026-09-01T08:00:00Z',
  decisiveCriterion: n === 1 ? undefined : 'AVERAGE',
  decision: n <= 2 ? 'ADMITTED' : 'WAITLISTED',
  ...overrides,
})
const provisional = (overrides: Partial<RankingView> = {}): RankingView => ({
  offer: offer(),
  counts: { validated: 4, rejected: 1, toReview: 0, inReview: 0, pendingCorrections: 0 },
  blockers: [],
  freeze: null,
  entries: [
    entry(1),
    entry(2, { mathTiebreak: 'SKIPPED', decisiveCriterion: 'SUBMITTED_AT' }),
    entry(3, { mathTiebreak: 'SKIPPED', decisiveCriterion: 'SUBMITTED_AT' }),
    entry(4, {
      rank: null,
      decision: 'NOT_RANKED',
      average: { ...entry(4).average, display: '08.00' },
    }),
  ],
  events: [],
  proposal: null,
  exports: [],
  ...overrides,
})
const frozenView = (overrides: Partial<RankingView> = {}): RankingView =>
  provisional({
    offer: offer({ frozen: true }),
    freeze: {
      frozenAt: '2026-10-14T08:00:00Z',
      frozenBy: { id: 'director', name: 'Directeur' },
      seats: 2,
      minimumAverage: '10.00',
      listVersion: 2,
      versionAt: '2026-10-14T09:00:00Z',
      freeSeats: 1,
    },
    entries: [
      entry(1, { decision: 'WITHDRAWN', initialDecision: 'ADMITTED' }),
      entry(2, { initialDecision: 'ADMITTED' }),
      entry(3, { initialDecision: 'WAITLISTED' }),
    ],
    proposal: entry(3),
    events: [
      {
        version: 2,
        type: 'WITHDRAWN',
        applicationId: 'app-1',
        applicationNumber: 'IFRI-2026-GL-000001',
        candidate: 'Nom1 Élodie',
        reason: 'Inscrit ailleurs.',
        actor: { id: 'deputy', name: 'Directeur adjoint' },
        occurredAt: '2026-10-14T09:00:00Z',
      },
    ],
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
  vi.mocked(selectionApi.offers).mockResolvedValue({ offers: [offer()] })
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
})
async function open(who: keyof typeof permissions, view: RankingView) {
  vi.mocked(adminApi.session).mockResolvedValue(session(who))
  vi.mocked(selectionApi.view).mockResolvedValue(view)
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
      { path: '/admin/candidatures', name: 'admin-applications', component: stub },
      { path: '/admin/corrections', name: 'admin-corrections', component: stub },
      { path: '/admin/comptes', name: 'admin-accounts', component: stub },
      { path: '/admin/journal', name: 'admin-audit', component: stub },
      { path: '/admin/candidatures/:id', name: 'admin-application', component: stub },
      {
        path: '/admin/classement',
        name: 'admin-selection',
        component: stub,
        meta: { admin: 'ranking.read' },
      },
    ],
  })
  router.beforeEach(adminGuard)
  await router.push('/admin/classement')
  wrapper = mount(AdminSelectionPage, { global: { plugins: [router] }, attachTo: document.body })
  await flushPromises()
  return wrapper
}
const button = (w: VueWrapper, label: string) =>
  w.findAll('button').find((b) => b.text().includes(label))
const openDialog = (w: VueWrapper) =>
  w.findAll('dialog').find((d) => d.attributes('open') !== undefined)!

describe('provisional ranking', () => {
  it('shows the seats line, skipped mathematics and the candidates below the minimum', async () => {
    const w = await open('director', provisional())
    expect(selectionApi.view).toHaveBeenCalledWith('offer-gl')
    expect(router.currentRoute.value.query).toEqual({ offre: 'offer-gl' })
    const rows = w.findAll('tbody tr').map((row) => row.text())
    expect(rows[2]).toBe('Limite des 2 places')
    expect(rows[1]).toContain(
      'Maths non comparées : candidat sans matière mathématique dans le groupe',
    )
    expect(rows[1]).toContain('Date de soumission')
    expect(rows[4]).toContain('Non classés : moyenne effective inférieure à 10,00 / 20')
    expect(rows[5]).toContain('Non classé')
    expect(rows[5]).toContain('08,00')
    expect(w.text()).toContain('2 admis, 1 en liste d’attente, 1 non classé')
  })

  it('saves seats and minimum average, then lists blockers that disable the freeze', async () => {
    const w = await open(
      'director',
      provisional({ offer: offer({ seats: null, minimumAverage: '' }) }),
    )
    await w.get('#seats').setValue('abc')
    await w.get('.selection-settings form').trigger('submit')
    expect(selectionApi.settings).not.toHaveBeenCalled()
    expect(w.text()).toContain('Indiquez un nombre entier de places')
    vi.mocked(selectionApi.settings).mockResolvedValue(
      provisional({
        blockers: [
          {
            code: 'DOSSIERS_PENDING',
            message: '2 dossiers ne sont pas encore validés ou rejetés.',
            count: 2,
          },
          { code: 'CAMPAIGN_OPEN', message: 'La période de candidature n’est pas terminée.' },
        ],
      }),
    )
    await w.get('#seats').setValue('3')
    await w.get('#minimum').setValue('11,5')
    await w.get('.selection-settings form').trigger('submit')
    await flushPromises()
    expect(selectionApi.settings).toHaveBeenCalledWith('offer-gl', {
      seats: 3,
      minimumAverage: '11,5',
    })
    const freeze = button(w, 'Geler et valider le classement')!
    expect(freeze.attributes('disabled')).toBeDefined()
    expect(freeze.attributes('aria-describedby')).toBe('freeze-blockers')
    expect(w.get('#freeze-blockers').text()).toContain('2 dossiers ne sont pas encore validés')
    expect(w.get('#freeze-blockers').text()).toContain('Voir les dossiers')
  })

  it('freezes after an explicit, irreversible confirmation', async () => {
    const w = await open('director', provisional())
    await button(w, 'Geler et valider le classement')!.trigger('click')
    await flushPromises()
    const dialog = openDialog(w)
    expect(dialog.attributes('role')).toBe('alertdialog')
    expect(dialog.text()).toContain('2 places')
    expect(dialog.text()).toContain('définitif')
    expect(document.activeElement?.textContent).toBe('Annuler')
    vi.mocked(selectionApi.freeze).mockResolvedValue(
      frozenView({ freeze: { ...frozenView().freeze!, listVersion: 1 } }),
    )
    await button(w, 'Geler définitivement')!.trigger('click')
    await flushPromises()
    expect(selectionApi.freeze).toHaveBeenCalledWith('offer-gl', {
      seats: 2,
      minimumAverage: '10.00',
    })
    expect(w.text()).toContain('Classement gelé et validé : liste version 1')
    expect(button(w, 'Geler et valider')).toBeUndefined()
  })

  it('stays read-only for the head of the secretariat', async () => {
    const w = await open('lead', provisional())
    expect(w.find('.selection-settings form').exists()).toBe(false)
    expect(w.text()).toContain('Places : 2 · Moyenne minimale : 10,00 / 20')
    expect(button(w, 'Geler')).toBeUndefined()
    expect(w.text()).not.toContain('Exports')
  })
})

describe('after the freeze', () => {
  it('lets the deputy director withdraw with a reason and promote the proposed candidate', async () => {
    const w = await open('deputy', frozenView())
    expect(w.text()).toContain('liste version 2')
    expect(w.text()).toContain('Places libres : 1')
    expect(w.text()).toContain('Version 2 — Désistement de IFRI-2026-GL-000001')
    expect(w.find('.selection-settings form').exists()).toBe(false)
    vi.mocked(selectionApi.promote).mockResolvedValue(frozenView({ proposal: null }))
    await button(w, 'Promouvoir ce candidat')!.trigger('click')
    await flushPromises()
    expect(openDialog(w).text()).toContain('Rang 3')
    await openDialog(w)
      .findAll('button')
      .find((b) => b.text() === 'Promouvoir')!
      .trigger('click')
    await flushPromises()
    expect(selectionApi.promote).toHaveBeenCalledWith('offer-gl', {
      applicationId: 'app-3',
      listVersion: 2,
    })

    await w.get('button[aria-label="Désistement : IFRI-2026-GL-000002"]').trigger('click')
    await flushPromises()
    await w.get('form#withdraw-form').trigger('submit')
    await flushPromises()
    expect(selectionApi.withdraw).not.toHaveBeenCalled()
    expect(openDialog(w).text()).toContain('Indiquez le motif du désistement.')
    await w.get('#withdraw-reason').setValue('Ne donne pas suite.')
    vi.mocked(selectionApi.withdraw).mockRejectedValue(
      new ApiError('SELECTION_VERSION_CONFLICT', 'La liste vient d’être modifiée.', {}, 409),
    )
    await w.get('form#withdraw-form').trigger('submit')
    await flushPromises()
    expect(selectionApi.withdraw).toHaveBeenCalledWith('offer-gl', {
      applicationId: 'app-2',
      reason: 'Ne donne pas suite.',
      listVersion: 2,
    })
    expect(w.text()).toContain('La liste a changé depuis son affichage.')
  })

  it('offers versioned CSV downloads and PDF generation to exporters', async () => {
    const w = await open('director', frozenView())
    expect(w.text()).toContain('Exports — version 2')
    expect(w.find('.selection-proposal').exists()).toBe(false)
    expect(w.find('button[aria-label^="Désistement"]').exists()).toBe(false)
    const links = w.findAll('a[download]').map((a) => a.attributes('href'))
    expect(links).toEqual([
      '/api/v1/admin/selection/offers/offer-gl/export.csv?variant=public',
      '/api/v1/admin/selection/offers/offer-gl/export.csv?variant=internal',
    ])
    vi.mocked(selectionApi.requestPdf).mockResolvedValue({
      id: 'export-1',
      listVersion: 2,
      variant: 'PUBLIC',
      status: 'READY',
      requestedBy: { id: 'director', name: 'Directeur' },
      requestedAt: '2026-10-14T10:00:00Z',
    })
    await button(w, 'Liste publique (PDF)')!.trigger('click')
    await flushPromises()
    expect(selectionApi.requestPdf).toHaveBeenCalledWith('offer-gl', 'PUBLIC')
    const pdf = w.findAll('a').find((a) => a.text() === 'télécharger le PDF')!
    expect(pdf.attributes('href')).toBe('/api/v1/admin/selection/exports/export-1/content')
    expect(w.text()).toContain('PDF prêt (liste publique, version 2)')
  })
})
