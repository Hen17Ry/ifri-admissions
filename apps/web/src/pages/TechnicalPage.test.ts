import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import TechnicalPage from './TechnicalPage.vue'

afterEach(() => vi.unstubAllGlobals())

describe('Page technique', () => {
  it('affiche le frontend et des dépendances réellement disponibles', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        status: 200,
        json: async () => ({ status: 'ok', checks: { postgres: 'ok', redis: 'ok', minio: 'ok' } }),
      }),
    )
    const wrapper = mount(TechnicalPage)
    await flushPromises()
    expect(wrapper.find('h1').text()).toBe('Plateforme de gestion des admissions')
    expect(wrapper.text()).toContain('Frontend opérationnel')
    expect(wrapper.find('[role="status"]').text()).toBe('API et dépendances disponibles.')
    expect(wrapper.findAll('dd').map((item) => item.text())).toEqual([
      'Disponible',
      'Disponible',
      'Disponible',
    ])
    wrapper.unmount()
  })

  it('ne présente pas les services comme prêts si l’API est inaccessible', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))
    const wrapper = mount(TechnicalPage)
    await flushPromises()
    expect(wrapper.find('[role="status"]').text()).toContain('API inaccessible')
    expect(wrapper.findAll('dd').every((item) => item.text() === 'Non vérifié')).toBe(true)
    expect(wrapper.find('button').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
})
