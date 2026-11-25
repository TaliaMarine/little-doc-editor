import type { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createVuetify } from 'vuetify'
import AttachmentPanel from '@/components/AttachmentPanel.vue'
import i18nPlugin from '@/plugins/i18n'
import { useAttachmentsStore } from '@/stores/attachments'

const listPageAttachments = vi.fn()
vi.mock('@/api/generated', () => ({
  listPageAttachments: (...args: unknown[]) => listPageAttachments(...args),
  downloadAttachment: vi.fn(),
}))
const uploadAttachment = vi.fn()
vi.mock('@/api/attachments', () => ({
  uploadAttachment: (...args: unknown[]) => uploadAttachment(...args),
  fetchAttachmentBlob: vi.fn(),
  UploadError: class UploadError extends Error {},
}))
vi.mock('@/composables/useAttachmentUrl', () => ({
  useAttachmentUrl: () => ({ url: { value: null }, error: { value: false }, loading: { value: false } }),
}))

function mountPanel (page: { can_edit: boolean, locked: boolean }) {
  return mount(AttachmentPanel, {
    props: { page: { id: 'p1', ...page } },
    global: { plugins: [createVuetify(), i18nPlugin as ReturnType<typeof createI18n>] },
  })
}

const item = { id: 'a1', page_id: 'p1', filename: 'doc.pdf', content_type: 'application/pdf', size_bytes: 2048 }

describe('AttachmentPanel', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    listPageAttachments.mockReset().mockResolvedValue({ data: [item] })
    uploadAttachment.mockReset()
  })

  it('hides upload controls when the caller cannot edit, but still lists files', async () => {
    const wrapper = mountPanel({ can_edit: false, locked: false })
    await flushPromises()
    expect(wrapper.find('[data-testid="dropzone"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('doc.pdf')
  })

  it('hides upload controls on a locked page', async () => {
    const wrapper = mountPanel({ can_edit: true, locked: true })
    await flushPromises()
    expect(wrapper.find('[data-testid="dropzone"]').exists()).toBe(false)
  })

  it('shows the server message as-is for a rejected upload', async () => {
    uploadAttachment.mockRejectedValue(new Error('attachment exceeds the configured maximum size'))
    const wrapper = mountPanel({ can_edit: true, locked: false })
    await flushPromises()

    const file = new File(['x'], 'big.bin')
    const input = wrapper.find('[data-testid="file-input"]')
    Object.defineProperty(input.element, 'files', { value: [file] })
    await input.trigger('change')
    await flushPromises()

    expect(wrapper.text()).toContain('attachment exceeds the configured maximum size')
    expect(useAttachmentsStore().uploads[0]?.status).toBe('error')
  })

  it('adds a successfully uploaded file to the list', async () => {
    uploadAttachment.mockResolvedValue({ ...item, id: 'a2', filename: 'new.pdf' })
    const wrapper = mountPanel({ can_edit: true, locked: false })
    await flushPromises()

    const input = wrapper.find('[data-testid="file-input"]')
    Object.defineProperty(input.element, 'files', { value: [new File(['x'], 'new.pdf')] })
    await input.trigger('change')
    await flushPromises()

    expect(wrapper.text()).toContain('new.pdf')
    expect(useAttachmentsStore().uploads).toHaveLength(0)
  })
})
