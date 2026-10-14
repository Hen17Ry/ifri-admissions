import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import SubmissionConfirmationPage from './SubmissionConfirmationPage.vue'
import { submissionApi } from '@/features/submission/api'
import { useSubmissionStore } from '@/features/submission/store'
import { ApiError } from '@/lib/api'
vi.mock('@/features/submission/api', async (original) => ({
  ...(await original<typeof import('@/features/submission/api')>()),
  submissionApi: { submit: vi.fn(), receipt: vi.fn() },
}))
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
let wrapper: VueWrapper | undefined
let router: Router
beforeEach(() => {
  vi.resetAllMocks()
  setActivePinia(createPinia())
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
})
async function page() {
  const stub = { render: () => null }
  router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/candidature', name: 'candidate-application', component: stub },
      { path: '/candidature/confirmation', name: 'candidate-confirmation', component: stub },
    ],
  })
  await router.push('/candidature/confirmation')
  wrapper = mount(SubmissionConfirmationPage, {
    global: { plugins: [router] },
    attachTo: document.body,
  })
  await flushPromises()
  return wrapper
}

describe('Submission confirmation', () => {
  it('reloads the receipt after a refresh and shows the number, program, date and advice', async () => {
    vi.mocked(submissionApi.receipt).mockResolvedValue(receipt)
    const w = await page()
    expect(submissionApi.receipt).toHaveBeenCalledOnce()
    expect(w.get('#application-number').text()).toBe('IFRI-2026-GL-000042')
    expect(w.text()).toContain('Génie Logiciel')
    expect(w.text()).toContain('14 octobre 2026 à 10:30 (heure du Bénin)')
    expect(w.text()).toContain('Conservez ce numéro')
    expect(w.text()).toContain('30 juin 2027')
    expect(document.activeElement?.id).toBe('receipt-title')
    expect(document.title).toBe('Candidature soumise — Admissions IFRI')
  })

  it('uses the receipt handed over by the submission without a second request', async () => {
    useSubmissionStore().receipt = receipt
    const w = await page()
    expect(submissionApi.receipt).not.toHaveBeenCalled()
    expect(w.get('#application-number').text()).toBe(receipt.applicationNumber)
  })

  it('offers no modification and never shows the average', async () => {
    vi.mocked(submissionApi.receipt).mockResolvedValue(receipt)
    const w = await page()
    expect(w.findAll('button').map((b) => b.text())).toEqual([
      'Copier le numéro',
      'Imprimer le récépissé',
    ])
    expect(w.text()).not.toMatch(/modifier le|moyenne/i)
    expect(w.findAll('input, select, textarea, form')).toHaveLength(0)
  })

  it('copies the number and announces the result', async () => {
    vi.mocked(submissionApi.receipt).mockResolvedValue(receipt)
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    const w = await page()
    await w
      .findAll('button')
      .find((b) => b.text() === 'Copier le numéro')!
      .trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('IFRI-2026-GL-000042')
    expect(w.get('.copy-status').attributes('role')).toBe('status')
    expect(w.get('.copy-status').text()).toBe('Numéro copié dans le presse-papiers.')
    writeText.mockRejectedValue(new Error('denied'))
    await w
      .findAll('button')
      .find((b) => b.text() === 'Copier le numéro')!
      .trigger('click')
    await flushPromises()
    expect(w.get('.copy-status').text()).toContain('sélectionnez le numéro')
  })

  it('prints the page as the receipt', async () => {
    vi.mocked(submissionApi.receipt).mockResolvedValue(receipt)
    const print = vi.spyOn(window, 'print').mockImplementation(() => {})
    const w = await page()
    await w
      .findAll('button')
      .find((b) => b.text() === 'Imprimer le récépissé')!
      .trigger('click')
    expect(print).toHaveBeenCalledOnce()
    expect(w.get('.receipt-actions').classes()).toContain('no-print')
  })

  it('sends a draft that is not submitted back to the application', async () => {
    vi.mocked(submissionApi.receipt).mockRejectedValue(
      new ApiError('SUBMISSION_NOT_FOUND', 'Aucune', {}, 404),
    )
    await page()
    expect(router.currentRoute.value.name).toBe('candidate-application')
  })

  it('explains an ended session without offering a new draft', async () => {
    vi.mocked(submissionApi.receipt).mockRejectedValue(
      new ApiError('DRAFT_SESSION_EXPIRED', 'Expirée', {}, 401),
    )
    const w = await page()
    expect(w.text()).toContain('Votre récépissé n’est plus accessible sur ce navigateur.')
    expect(w.text()).toContain('reste enregistrée')
    expect(w.findAll('button')).toHaveLength(0)
  })

  it('lets the candidate retry after a network error', async () => {
    vi.mocked(submissionApi.receipt)
      .mockRejectedValueOnce(new ApiError('NETWORK_ERROR', 'Connexion indisponible.'))
      .mockResolvedValueOnce(receipt)
    const w = await page()
    expect(w.text()).toContain('Connexion indisponible.')
    await w
      .findAll('button')
      .find((b) => b.text() === 'Réessayer')!
      .trigger('click')
    await flushPromises()
    expect(w.get('#application-number').text()).toBe(receipt.applicationNumber)
  })
})
