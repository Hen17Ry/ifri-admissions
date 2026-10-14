import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import type { Component } from 'vue'
import { adminApi, type AdminSession, type Account } from '@/features/admin/api'
import { reviewApi } from '@/features/review/api'
import { adminGuard } from '@/features/admin/guard'
import { useAdminStore } from '@/features/admin/store'
import { ApiError } from '@/lib/api'
import AdminLoginPage from './AdminLoginPage.vue'
import AdminActivationPage from './AdminActivationPage.vue'
import AdminMfaPage from './AdminMfaPage.vue'
import AdminAccountsPage from './AdminAccountsPage.vue'
import AdminAuditPage from './AdminAuditPage.vue'
import AdminAccountPage from './AdminAccountPage.vue'
import AdminDashboardPage from './AdminDashboardPage.vue'

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

const active = (permissions: string[], extra: Partial<AdminSession> = {}): AdminSession => ({
  id: 'me',
  stage: 'ACTIVE',
  csrfToken: 'csrf',
  email: 'super@ifri.example',
  displayName: 'Super',
  permissions,
  grants: [{ role: 'SUPER_ADMIN' }],
  idleExpiresAt: '2026-10-14T12:00:00Z',
  expiresAt: '2026-10-14T18:00:00Z',
  mfaRequired: true,
  ...extra,
})
const superPermissions = ['accounts.manage', 'accounts.read', 'audit.read', 'dashboard.view']
const account = (overrides: Partial<Account> = {}): Account => ({
  id: 'a1',
  email: 'reviewer@ifri.example',
  displayName: 'Vérificatrice GL',
  status: 'ACTIVE',
  mfaEnabled: true,
  lastLoginAt: null,
  createdAt: '2026-10-14T08:00:00Z',
  activationExpiresAt: null,
  grants: [{ role: 'REVIEWER' }],
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
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
})

function makeRouter() {
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
      {
        path: '/admin/activation',
        name: 'admin-activation',
        component: stub,
        meta: { admin: 'public' },
      },
      { path: '/admin/verification', name: 'admin-mfa', component: stub, meta: { admin: 'mfa' } },
      { path: '/admin/compte', name: 'admin-account', component: stub, meta: { admin: 'self' } },
      {
        path: '/admin/comptes',
        name: 'admin-accounts',
        component: stub,
        meta: { admin: 'accounts.read' },
      },
      {
        path: '/admin/journal',
        name: 'admin-audit',
        component: stub,
        meta: { admin: 'audit.read' },
      },
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
  return router
}
async function open(page: Component, path: string, session: AdminSession | null) {
  if (session) vi.mocked(adminApi.session).mockResolvedValue(session)
  else
    vi.mocked(adminApi.session).mockRejectedValue(
      new ApiError('ADMIN_SESSION_REQUIRED', 'x', {}, 401),
    )
  makeRouter()
  await router.push(path)
  wrapper = mount(page, { global: { plugins: [router] }, attachTo: document.body })
  await flushPromises()
  return wrapper
}
const button = (w: VueWrapper, label: string) =>
  w.findAll('button').find((b) => b.text().includes(label))!

describe('admin navigation guard', () => {
  it('sends each session to the right screen, the API deciding in the end', async () => {
    for (const [session, path, expected] of [
      [null, '/admin/comptes', 'admin-login'],
      [{ ...active([]), stage: 'MFA_ENROLLMENT' }, '/admin', 'admin-mfa'],
      [{ ...active([]), stage: 'MFA_VERIFICATION' }, '/admin/journal', 'admin-mfa'],
      [active(['dashboard.view']), '/admin/comptes', 'admin-dashboard'],
      [active(superPermissions), '/admin/connexion', 'admin-dashboard'],
      [active(superPermissions), '/admin/verification', 'admin-dashboard'],
      [active(superPermissions), '/admin/journal', 'admin-audit'],
      [null, '/admin/activation', 'admin-activation'],
    ] as const) {
      setActivePinia(createPinia())
      if (session) vi.mocked(adminApi.session).mockResolvedValue(session as AdminSession)
      else
        vi.mocked(adminApi.session).mockRejectedValue(
          new ApiError('ADMIN_SESSION_REQUIRED', 'x', {}, 401),
        )
      makeRouter()
      await router.push(path)
      expect(router.currentRoute.value.name, `${path}`).toBe(expected)
    }
    expect(router.currentRoute.value.query).toEqual({})
  })
})

describe('login', () => {
  it('shows one generic message whatever the cause and clears the password', async () => {
    const w = await open(AdminLoginPage, '/admin/connexion', null)
    vi.mocked(adminApi.login).mockRejectedValue(
      new ApiError('ADMIN_LOGIN_FAILED', 'serveur', {}, 401),
    )
    await w.get('#email').setValue('inconnu@ifri.example')
    await w.get('#password').setValue('un mot de passe')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.text()).toContain('Identifiant ou mot de passe incorrect.')
    expect((w.get('#password').element as HTMLInputElement).value).toBe('')
    vi.mocked(adminApi.login).mockRejectedValue(
      new ApiError('ADMIN_RATE_LIMITED', 'serveur', {}, 429),
    )
    await w.get('#password').setValue('un mot de passe')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.text()).toContain('Trop de tentatives')
  })

  it('continues to the second factor after the password', async () => {
    const w = await open(AdminLoginPage, '/admin/connexion?redirect=/admin/journal', null)
    vi.mocked(adminApi.login).mockResolvedValue({ stage: 'MFA_VERIFICATION', csrfToken: 'c' })
    vi.mocked(adminApi.session).mockResolvedValue({ ...active([]), stage: 'MFA_VERIFICATION' })
    expect(w.get('#password').attributes('type')).toBe('password')
    await w.get('.password-toggle').trigger('click')
    expect(w.get('#password').attributes('type')).toBe('text')
    expect(w.get('#password').attributes('autocomplete')).toBe('current-password')
    await w.get('#email').setValue('super@ifri.example')
    await w.get('#password').setValue('un mot de passe')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('admin-mfa')
    expect(router.currentRoute.value.query.redirect).toBe('/admin/journal')
  })
})

describe('activation', () => {
  it('reads the token from the fragment, erases it, then sets the password', async () => {
    window.history.replaceState(null, '', '/admin/activation#jeton-secret-1234')
    const replace = vi.spyOn(window.history, 'replaceState')
    vi.mocked(adminApi.inspectActivation).mockResolvedValue({
      email: 'new@ifri.example',
      displayName: 'Nouveau',
    })
    const w = await open(AdminActivationPage, '/admin/activation', null)
    expect(replace).toHaveBeenCalled()
    expect(window.location.hash).toBe('')
    expect(adminApi.inspectActivation).toHaveBeenCalledWith('jeton-secret-1234')
    expect(w.text()).toContain('Bienvenue Nouveau')
    await w.get('#password').setValue('une phrase assez longue')
    await w.get('#confirmation').setValue('une autre phrase longue')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.text()).toContain('Les deux saisies sont différentes.')
    expect(adminApi.activate).not.toHaveBeenCalled()
    vi.mocked(adminApi.activate).mockRejectedValueOnce(
      new ApiError(
        'PASSWORD_TOO_COMMON',
        'x',
        { password: 'Ce mot de passe figure parmi les plus utilisés.' },
        422,
      ),
    )
    await w.get('#confirmation').setValue('une phrase assez longue')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.text()).toContain('figure parmi les plus utilisés')
    vi.mocked(adminApi.activate).mockResolvedValue({ activated: true })
    await w.get('#password').setValue('une phrase assez longue')
    await w.get('#confirmation').setValue('une phrase assez longue')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(adminApi.activate).toHaveBeenLastCalledWith(
      'jeton-secret-1234',
      'une phrase assez longue',
    )
    expect(w.get('h1').text()).toBe('Compte activé')
  })

  it('explains an invalid or used link', async () => {
    window.history.replaceState(null, '', '/admin/activation#ancien')
    vi.mocked(adminApi.inspectActivation).mockRejectedValue(
      new ApiError('ADMIN_ACTIVATION_INVALID', 'x', {}, 410),
    )
    const w = await open(AdminActivationPage, '/admin/activation', null)
    expect(w.text()).toContain('Ce lien d’activation n’est plus valide')
    expect(w.find('form').exists()).toBe(false)
  })
})

describe('second factor', () => {
  it('enrols with a QR code, then shows the recovery codes once', async () => {
    vi.mocked(adminApi.startEnrollment).mockResolvedValue({
      secret: 'JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP',
      uri: 'otpauth://totp/x',
      qrSvg: '<svg xmlns="http://www.w3.org/2000/svg"></svg>',
    })
    const w = await open(AdminMfaPage, '/admin/verification', {
      ...active([]),
      stage: 'MFA_ENROLLMENT',
    })
    expect(w.get('img.mfa-qr').attributes('src')).toMatch(/^data:image\/svg\+xml;base64,/)
    expect(w.get('img.mfa-qr').attributes('alt')).toContain('Code QR')
    expect(w.get('.mfa-secret').text()).toBe('JBSW Y3DP EHPK 3PXP JBSW Y3DP EHPK 3PXP')
    expect(w.get('#mfa-code').attributes('autocomplete')).toBe('one-time-code')
    expect(w.get('#mfa-code').attributes('inputmode')).toBe('numeric')
    vi.mocked(adminApi.confirmEnrollment).mockRejectedValueOnce(
      new ApiError('ADMIN_MFA_INVALID', 'x', {}, 422),
    )
    await w.get('#mfa-code').setValue('000000')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.text()).toContain('Code incorrect ou déjà utilisé')
    vi.mocked(adminApi.confirmEnrollment).mockResolvedValue({
      stage: 'ACTIVE',
      csrfToken: 'c2',
      recoveryCodes: ['AAAAA-BBBBB', 'CCCCC-DDDDD'],
    })
    vi.mocked(adminApi.session).mockResolvedValue(active(superPermissions))
    await w.get('#mfa-code').setValue('123456')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(w.findAll('.recovery-list li').map((li) => li.text())).toEqual([
      'AAAAA-BBBBB',
      'CCCCC-DDDDD',
    ])
    expect(w.find('img.mfa-qr').exists()).toBe(false)
    const next = button(w, 'Accéder à l’administration')
    expect(next.attributes('disabled')).toBeDefined()
    await w.get('.admin-check input').setValue(true)
    expect(next.attributes('disabled')).toBeUndefined()
    await next.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('admin-dashboard')
  })

  it('accepts a recovery code instead of the application', async () => {
    const w = await open(AdminMfaPage, '/admin/verification', {
      ...active([]),
      stage: 'MFA_VERIFICATION',
    })
    expect(adminApi.startEnrollment).not.toHaveBeenCalled()
    await button(w, 'Utiliser un code de secours').trigger('click')
    expect(w.get('#mfa-code').attributes('autocomplete')).toBe('off')
    expect(w.text()).toContain('Format XXXXX-XXXXX')
    vi.mocked(adminApi.verify).mockResolvedValue({ stage: 'ACTIVE', csrfToken: 'c3' })
    vi.mocked(adminApi.session).mockResolvedValue(active(superPermissions))
    await w.get('#mfa-code').setValue('AAAAA-BBBBB')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(adminApi.verify).toHaveBeenCalledWith('AAAAA-BBBBB')
    expect(router.currentRoute.value.name).toBe('admin-dashboard')
  })
})

describe('accounts', () => {
  beforeEach(() => {
    vi.mocked(adminApi.accounts).mockResolvedValue({
      accounts: [
        account(),
        account({
          id: 'me',
          email: 'super@ifri.example',
          displayName: 'Super',
          grants: [{ role: 'SUPER_ADMIN' }],
        }),
      ],
    })
  })

  it('creates an account with exactly one role and shows its activation link once', async () => {
    const w = await open(AdminAccountsPage, '/admin/comptes', active(superPermissions))
    expect(w.findAll('tbody tr')).toHaveLength(2)
    expect(w.text()).toContain('Agent du secrétariat exécutif')
    await button(w, 'Créer un compte').trigger('click')
    await flushPromises()
    expect(HTMLDialogElement.prototype.showModal).toHaveBeenCalled()
    // Roles are radio buttons of one group: choosing a second one replaces the first.
    const radios = w.findAll('input[name="create-role"]')
    expect(radios).toHaveLength(5)
    expect(radios.every((radio) => radio.attributes('type') === 'radio')).toBe(true)
    expect(w.find('.grant-programs').exists()).toBe(false)
    await w.get('form#create-account').trigger('submit')
    await flushPromises()
    expect(w.text()).toContain('Choisissez le rôle du compte.')
    expect(adminApi.createAccount).not.toHaveBeenCalled()
    await w.get('#new-email').setValue('da@ifri.example')
    await w.get('#new-name').setValue('Directeur adjoint')
    await w.get('#create-role-REVIEWER').setValue(true)
    await w.get('#create-role-DEPUTY_DIRECTOR').setValue(true)
    await flushPromises()
    vi.mocked(adminApi.createAccount).mockResolvedValue({
      account: account({
        id: 'new',
        email: 'da@ifri.example',
        displayName: 'Directeur adjoint',
        status: 'PENDING_ACTIVATION',
        grants: [{ role: 'DEPUTY_DIRECTOR' }],
      }),
      activationUrl: 'http://localhost:5173/admin/activation#token',
      expiresAt: '2026-10-17T08:00:00Z',
    })
    await w.get('form#create-account').trigger('submit')
    await flushPromises()
    expect(adminApi.createAccount).toHaveBeenCalledWith({
      email: 'da@ifri.example',
      displayName: 'Directeur adjoint',
      grants: [{ role: 'DEPUTY_DIRECTOR', program: '' }],
    })
    expect((w.get('#activation-url').element as HTMLInputElement).value).toBe(
      'http://localhost:5173/admin/activation#token',
    )
    expect(w.text()).toContain('Il ne sera plus affiché')
    expect(w.findAll('tbody tr')).toHaveLength(3)
  })

  it('confirms a deactivation, never offers it on one’s own account', async () => {
    const w = await open(AdminAccountsPage, '/admin/comptes', active(superPermissions))
    const rows = w.findAll('tbody tr')
    expect(rows[1]!.text()).not.toContain('Désactiver')
    await rows[0]!
      .findAll('button')
      .find((b) => b.text().includes('Désactiver'))!
      .trigger('click')
    await flushPromises()
    const dialog = w.findAll('dialog').find((d) => d.attributes('open') !== undefined)!
    expect(dialog.attributes('role')).toBe('alertdialog')
    expect(dialog.text()).toContain('Désactiver ce compte ?')
    expect(document.activeElement?.textContent).toBe('Annuler')
    vi.mocked(adminApi.setStatus).mockResolvedValue(account({ status: 'DISABLED' }))
    await dialog
      .findAll('button')
      .find((b) => b.text() === 'Désactiver')!
      .trigger('click')
    await flushPromises()
    expect(adminApi.setStatus).toHaveBeenCalledWith('a1', false)
    expect(w.text()).toContain('Désactivé')
  })

  it('is read-only for an auditor', async () => {
    const w = await open(
      AdminAccountsPage,
      '/admin/comptes',
      active(['accounts.read', 'audit.read', 'dashboard.view']),
    )
    expect(w.text()).not.toContain('Créer un compte')
    expect(w.findAll('tbody button')).toHaveLength(0)
  })
})

describe('audit journal', () => {
  it('filters and pages through the events', async () => {
    const event = (id: string, eventType = 'ADMIN_LOGIN_FAILED') => ({
      id,
      eventType,
      actorType: 'SYSTEM' as const,
      metadata: { identifierHash: 'ab'.repeat(32) },
      occurredAt: '2026-10-14T09:30:00Z',
    })
    vi.mocked(adminApi.audit)
      .mockResolvedValueOnce({
        events: [event('1')],
        nextCursor: 'next',
        eventTypes: ['ADMIN_LOGIN_FAILED', 'ADMIN_LOGOUT'],
      })
      .mockResolvedValueOnce({
        events: [event('2')],
        eventTypes: ['ADMIN_LOGIN_FAILED', 'ADMIN_LOGOUT'],
      })
      .mockResolvedValueOnce({ events: [], eventTypes: ['ADMIN_LOGIN_FAILED', 'ADMIN_LOGOUT'] })
    const w = await open(AdminAuditPage, '/admin/journal', active(superPermissions))
    expect(w.text()).toContain('Connexion échouée')
    expect(w.text()).toContain('Système')
    expect(w.text()).toContain('14/10/2026 10:30:00')
    await button(w, 'Afficher les événements plus anciens').trigger('click')
    await flushPromises()
    expect(adminApi.audit).toHaveBeenLastCalledWith(expect.objectContaining({ cursor: 'next' }))
    expect(w.findAll('tbody tr')).toHaveLength(2)
    await w.get('#filter-event').setValue('ADMIN_LOGOUT')
    await w.get('#filter-from').setValue('2026-10-01')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(adminApi.audit).toHaveBeenLastCalledWith(
      expect.objectContaining({
        eventType: 'ADMIN_LOGOUT',
        from: '2026-09-30T23:00:00Z',
        cursor: '',
      }),
    )
    expect(w.text()).toContain('Aucun événement pour ces filtres.')
  })
})

describe('own account and navigation', () => {
  it('signs out everywhere after a password change', async () => {
    const w = await open(AdminAccountPage, '/admin/compte', active(superPermissions))
    vi.mocked(adminApi.changePassword).mockResolvedValue({ reauthenticate: true })
    await w.get('#currentPassword').setValue('ancien mot de passe')
    await w.get('#newPassword').setValue('un nouveau mot de passe')
    await w.get('#confirmation').setValue('un nouveau mot de passe')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(adminApi.changePassword).toHaveBeenCalledWith(
      'ancien mot de passe',
      'un nouveau mot de passe',
    )
    expect(router.currentRoute.value.name).toBe('admin-login')
    expect(useAdminStore().notice).toContain('Mot de passe modifié')
  })

  it('shows only the menu entries a role can use, and the review counters', async () => {
    vi.mocked(reviewApi.dashboard).mockResolvedValue({
      counters: [
        { program: 'GL', programName: 'Génie Logiciel', status: 'TO_REVIEW', count: 7 },
        { program: 'GL', programName: 'Génie Logiciel', status: 'VALIDATED', count: 2 },
        { program: 'SI', programName: 'Sécurité Informatique', status: 'IN_REVIEW', count: 1 },
      ],
      unassigned: 7,
      mine: 1,
      pendingCorrections: 0,
    })
    const w = await open(
      AdminDashboardPage,
      '/admin',
      active(
        [
          'applications.read',
          'corrections.propose',
          'dashboard.view',
          'ranking.read',
          'review.claim',
          'review.decide',
        ],
        { grants: [{ role: 'REVIEWER' }] },
      ),
    )
    const nav = w.get('nav[aria-label="Administration"]')
    expect(nav.text()).toContain('Tableau de bord')
    expect(nav.text()).toContain('Candidatures')
    expect(nav.text()).not.toContain('Corrections à décider')
    expect(nav.text()).not.toContain('Comptes')
    expect(nav.text()).not.toContain('Journal')
    expect(nav.find('[aria-current="page"]').text()).toContain('Tableau de bord')
    expect(w.text()).toContain('Agent du secrétariat exécutif')
    expect(w.text()).toContain('Mes dossiers en vérification')
    const gl = w.findAll('tbody tr').find((row) => row.text().includes('Génie Logiciel'))!
    expect(gl.findAll('td').map((cell) => cell.text())).toEqual(['7', '0', '2', '0', '9'])
    expect(w.get('tfoot').text()).toContain('10')
    expect(w.find('[data-testid="coverage-warning"]').exists()).toBe(false)
    vi.mocked(adminApi.logout).mockResolvedValue({ loggedOut: true })
    await button(w, 'Déconnexion').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('admin-login')
  })
})

describe('technical administrator dashboard', () => {
  it('warns when no deputy director or director account is active', async () => {
    vi.mocked(reviewApi.dashboard).mockResolvedValue({
      coverage: { deputyDirector: false, director: true },
    })
    const w = await open(AdminDashboardPage, '/admin', active(superPermissions))
    const warning = w.get('[data-testid="coverage-warning"]')
    expect(warning.text()).toContain('Aucun directeur adjoint actif')
    expect(warning.text()).not.toContain('Aucun directeur actif')
    expect(w.text()).toContain('jamais aux dossiers des candidats')
    expect(w.get('nav[aria-label="Administration"]').text()).not.toContain('Candidatures')
  })
})
