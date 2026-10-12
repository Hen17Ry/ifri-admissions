import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DocumentsStep from './DocumentsStep.vue'
import { documentsApi, type DocumentListing } from './api'
import { ApiError } from '@/lib/api'
vi.mock('./api', () => ({
  documentsApi: {
    list: vi.fn(),
    upload: vi.fn(),
    content: (type: string) => `/api/v1/applications/drafts/current/documents/${type}/content`,
  },
}))
const listing: DocumentListing = {
  complete: false,
  maxBytes: 1024,
  applicationVersion: 2,
  documents: [
    {
      type: 'REQUEST_LETTER',
      label: 'Demande',
      required: true,
      uploaded: false,
      version: 0,
      filename: '',
      sizeBytes: 0,
    },
    {
      type: 'TRANSCRIPT',
      label: 'Relevé',
      required: true,
      uploaded: false,
      version: 0,
      filename: '',
      sizeBytes: 0,
    },
    {
      type: 'IDENTITY_DOCUMENT',
      label: 'Identité',
      required: true,
      uploaded: false,
      version: 0,
      filename: '',
      sizeBytes: 0,
    },
    {
      type: 'PARENTAL_COMMITMENT',
      label: 'Engagement parental',
      required: true,
      uploaded: false,
      version: 0,
      filename: '',
      sizeBytes: 0,
    },
  ],
}
async function select(w: ReturnType<typeof mount>, file: File) {
  const input = w.get('input[type=file]')
  Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
  await input.trigger('change')
  await flushPromises()
}
beforeEach(() => vi.resetAllMocks())
describe('Document uploads', () => {
  it('displays four explicit destinations and server size limit', () => {
    const w = mount(DocumentsStep, { props: { listing } })
    expect(w.findAll('li')).toHaveLength(4)
    expect(w.text()).toContain('1 Ko')
    expect(w.findAll('input[type=file]')).toHaveLength(4)
  })
  it('uploads a PDF and emits acknowledged server state', async () => {
    vi.mocked(documentsApi.upload).mockResolvedValue({ ...listing, applicationVersion: 3 })
    const w = mount(DocumentsStep, { props: { listing } })
    const file = new File(['%PDF-1.4'], 'test.pdf', { type: 'application/pdf' })
    await select(w, file)
    expect(documentsApi.upload).toHaveBeenCalledWith('REQUEST_LETTER', file, 2)
    expect(w.emitted('saved')).toHaveLength(1)
  })
  it('rejects wrong format and excessive size', async () => {
    const w = mount(DocumentsStep, { props: { listing } })
    await select(w, new File(['x'], 'photo.png', { type: 'image/png' }))
    expect(w.text()).toContain('format PDF')
    await select(w, new File(['x'.repeat(2048)], 'large.pdf', { type: 'application/pdf' }))
    expect(w.text()).toContain('taille maximale')
    expect(documentsApi.upload).not.toHaveBeenCalled()
  })
  it('reports actual pending state without fake percentages', async () => {
    let resolve!: (value: DocumentListing) => void
    vi.mocked(documentsApi.upload).mockImplementation(
      () =>
        new Promise((r) => {
          resolve = r
        }),
    )
    const w = mount(DocumentsStep, { props: { listing } })
    await select(w, new File(['%PDF-1.4'], 'x.pdf', { type: 'application/pdf' }))
    expect(w.text()).toContain('Envoi en cours')
    expect(w.find('input').attributes('disabled')).toBeDefined()
    resolve(listing)
    await flushPromises()
  })
  it('shows current filename and secure preview link while replacing', async () => {
    const current = {
      ...listing,
      documents: [
        {
          ...listing.documents[0]!,
          uploaded: true,
          version: 1,
          filename: 'old.pdf',
          sizeBytes: 32,
        },
      ],
    }
    vi.mocked(documentsApi.upload).mockRejectedValue(
      new ApiError('NETWORK_ERROR', 'Envoi interrompu.'),
    )
    const w = mount(DocumentsStep, { props: { listing: current } })
    expect(w.get('a').attributes('href')).toContain('/content')
    expect(w.get('a').attributes('rel')).toContain('noopener')
    await select(w, new File(['%PDF-1.4'], 'replacement.pdf', { type: 'application/pdf' }))
    expect(w.text()).toContain('old.pdf')
    expect(w.text()).toContain('Envoi interrompu.')
  })
  it('surfaces session expiration and retains existing file', async () => {
    vi.mocked(documentsApi.upload).mockRejectedValue(
      new ApiError('DRAFT_SESSION_EXPIRED', 'Votre session a expiré.', {}, 401),
    )
    const w = mount(DocumentsStep, { props: { listing } })
    await select(w, new File(['%PDF-1.4'], 'x.pdf', { type: 'application/pdf' }))
    expect(w.text()).toContain('session a expiré')
    expect(w.emitted('failure')).toHaveLength(1)
  })
  it('restores uploaded state and completion from API metadata', () => {
    const w = mount(DocumentsStep, {
      props: {
        listing: {
          ...listing,
          complete: true,
          documents: listing.documents.map((d) => ({
            ...d,
            uploaded: true,
            version: 2,
            filename: 'file.pdf',
            sizeBytes: 25,
          })),
        },
        readonly: true,
      },
    })
    expect(w.text()).toContain('documents obligatoires sont présents')
    expect(w.findAll('a')).toHaveLength(4)
    expect(w.find('input').exists()).toBe(false)
  })
})
