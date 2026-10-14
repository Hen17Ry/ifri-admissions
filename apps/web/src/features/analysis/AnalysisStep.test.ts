import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import AnalysisStep from './AnalysisStep.vue'
import { ApiError } from '@/lib/api'
import { analysisApi, type TranscriptAnalysis } from './api'
vi.mock('./api', () => ({ analysisApi: { current: vi.fn(), decide: vi.fn() } }))
const result: TranscriptAnalysis = {
  id: 'analysis',
  documentVersionId: 'v1',
  status: 'SUCCEEDED',
  candidateConfirmation: 'PENDING',
  verificationReady: false,
  subjects: [
    {
      subjectId: 's1',
      detectedSubjectText: 'Mathématiques',
      gradeValue: '15.5',
      coefficientValue: '2.5',
      position: 1,
    },
  ],
}
beforeEach(() => {
  vi.resetAllMocks()
  vi.mocked(analysisApi.current).mockResolvedValue(structuredClone(result))
})
it('shows immutable data and explicit confirmation', async () => {
  const w = mount(AnalysisStep, { props: { version: 7 } })
  await flushPromises()
  expect(w.text()).toContain('15.5')
  expect(w.find('input').exists()).toBe(false)
  expect(w.find('[contenteditable]').exists()).toBe(false)
  vi.mocked(analysisApi.decide).mockResolvedValue({
    ...result,
    candidateConfirmation: 'CONFIRMED',
    verificationReady: true,
  })
  await w
    .findAll('button')
    .find((b) => b.text().startsWith('Oui,'))!
    .trigger('click')
  await flushPromises()
  expect(analysisApi.decide).toHaveBeenCalledWith('confirm', 'analysis', 7)
  expect(w.text()).toContain('Informations du relevé confirmées')
  w.unmount()
})
it('requires rejection confirmation and requests replacement', async () => {
  const w = mount(AnalysisStep, { props: { version: 7 } })
  await flushPromises()
  await w
    .findAll('button')
    .find((b) => b.text().startsWith('Non,'))!
    .trigger('click')
  expect(analysisApi.decide).not.toHaveBeenCalled()
  vi.mocked(analysisApi.decide).mockResolvedValue({ ...result, candidateConfirmation: 'REJECTED' })
  await w
    .findAll('button')
    .find((b) => b.text() === 'Rejeter et remplacer mon relevé')!
    .trigger('click')
  await flushPromises()
  expect(w.emitted('replace')).toHaveLength(1)
  w.unmount()
})
it('low confidence forbids confirmation', async () => {
  vi.mocked(analysisApi.current).mockResolvedValue({
    ...result,
    status: 'LOW_CONFIDENCE',
    subjects: [],
  })
  const w = mount(AnalysisStep, { props: { version: 7 } })
  await flushPromises()
  expect(w.text()).toContain('Remplacer le relevé')
  expect(w.text()).not.toContain('Oui,')
  w.unmount()
})
it('polls only queued processing and stops after success', async () => {
  vi.useFakeTimers()
  vi.mocked(analysisApi.current).mockResolvedValueOnce({
    ...result,
    status: 'QUEUED',
    subjects: [],
  })
  const w = mount(AnalysisStep, { props: { version: 7 } })
  await flushPromises()
  expect(w.text()).toContain('Analyse de votre relevé en cours')
  await vi.advanceTimersByTimeAsync(4000)
  await flushPromises()
  expect(analysisApi.current).toHaveBeenCalledTimes(2)
  await vi.advanceTimersByTimeAsync(30000)
  expect(analysisApi.current).toHaveBeenCalledTimes(2)
  w.unmount()
  vi.useRealTimers()
})

it('pauses background polling and rejects a stale application version', async () => {
  vi.useFakeTimers()
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
  vi.mocked(analysisApi.current).mockResolvedValueOnce({
    ...result,
    status: 'QUEUED',
    subjects: [],
  })
  const w = mount(AnalysisStep, { props: { version: 7 } })
  await flushPromises()
  await vi.advanceTimersByTimeAsync(30000)
  expect(analysisApi.current).toHaveBeenCalledTimes(1)
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
  vi.mocked(analysisApi.current).mockResolvedValue({ ...result, applicationVersion: 8 })
  await vi.advanceTimersByTimeAsync(10000)
  await flushPromises()
  expect(w.text()).toContain('Le brouillon a changé dans un autre onglet.')
  expect(w.find('table').exists()).toBe(false)
  w.unmount()
  vi.useRealTimers()
})

it('refreshes current analysis after another document changes the application version', async () => {
  const w = mount(AnalysisStep, { props: { version: 7 } })
  await flushPromises()
  await w.setProps({ disabled: true })
  vi.mocked(analysisApi.current).mockResolvedValue({ ...result, applicationVersion: 8 })
  await w.setProps({ version: 8 })
  expect(analysisApi.current).toHaveBeenCalledTimes(1)
  await w.setProps({ disabled: false })
  await flushPromises()
  expect(analysisApi.current).toHaveBeenCalledTimes(2)
  expect(w.text()).toContain('15.5')
  expect(w.emitted('failure')).toBeUndefined()
  w.unmount()
})

it('requires replacement after a rejected version changes context, without a retry action', async () => {
  vi.mocked(analysisApi.current).mockRejectedValue(
    new ApiError('TRANSCRIPT_REPLACEMENT_REQUIRED', 'Un nouveau relevé est nécessaire.', {}, 409),
  )
  const w = mount(AnalysisStep, { props: { version: 7 } })
  await flushPromises()
  expect(w.text()).toContain('Un nouveau relevé est nécessaire.')
  expect(w.findAll('button').map((b) => b.text())).toEqual(['Remplacer le relevé'])
  await w.get('button').trigger('click')
  expect(w.emitted('replace')).toHaveLength(1)
  expect(analysisApi.decide).not.toHaveBeenCalled()
  w.unmount()
})

it('explains arithmetic review and prevents confirmation', async () => {
  vi.mocked(analysisApi.current).mockResolvedValue({
    ...result,
    status: 'LOW_CONFIDENCE',
    failureCode: 'NEEDS_REVIEW',
    subjects: [],
  })
  const w = mount(AnalysisStep, { props: { version: 7 } })
  await flushPromises()
  expect(w.text()).toContain('ne passent pas les contrôles de cohérence')
  expect(w.text()).not.toContain('Oui,')
  expect(w.find('[role="status"]').text()).toContain('nécessitent une vérification')
  w.unmount()
})

it.each([
  ['SERIES_MISMATCH', 'FAILED', 'ne correspond pas à celle déclarée'],
  ['RESULT_REJECTED', 'FAILED', 'n’ont pas passé nos contrôles'],
  ['AMBIGUOUS_ROW', 'LOW_CONFIDENCE', 'de façon ambiguë'],
  ['GRADE_POINTS_MISMATCH', 'LOW_CONFIDENCE', 'ne correspond pas aux points'],
] as const)('shows %s at once with a replacement action', async (code, status, text) => {
  vi.mocked(analysisApi.current).mockResolvedValue({
    ...result,
    status,
    failureCode: code,
    subjects: [],
  })
  const w = mount(AnalysisStep, { props: { version: 7 } })
  await flushPromises()
  expect(w.text()).toContain(text)
  expect(w.text()).not.toContain('Oui,')
  const replace = w.findAll('button').find((b) => b.text() === 'Remplacer le relevé')
  expect(replace).toBeDefined()
  await replace!.trigger('click')
  expect(w.emitted('replace')).toHaveLength(1)
  expect(analysisApi.current).toHaveBeenCalledTimes(1) // Terminal: no polling.
  w.unmount()
})

it('stops polling after the bound and offers a manual refresh', async () => {
  vi.useFakeTimers()
  vi.mocked(analysisApi.current).mockResolvedValue({
    ...result,
    status: 'PROCESSING',
    subjects: [],
  })
  const w = mount(AnalysisStep, { props: { version: 7 } })
  for (let i = 0; i < 70; i++) {
    await flushPromises()
    await vi.advanceTimersByTimeAsync(10000)
  }
  await flushPromises()
  expect(analysisApi.current).toHaveBeenCalledTimes(61) // Initial read + 60 polls.
  expect(w.text()).toContain('prend plus de temps que prévu')
  await w.get('button').trigger('click')
  await flushPromises()
  expect(analysisApi.current).toHaveBeenCalledTimes(62)
  w.unmount()
  vi.useRealTimers()
})
