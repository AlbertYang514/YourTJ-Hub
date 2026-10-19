// @vitest-environment happy-dom
import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import BadgesManagementPage from '../src/admin/pages/management/BadgesManagementPage.vue'
import BadgeStylePreview from '../src/admin/components/BadgeStylePreview.vue'
import { getBadges, saveBadge, uploadBadgeAsset } from '../src/admin/runtime/api'
import { isSystemBadgeArtwork } from '../src/site/utils/badge-style'

vi.mock('../src/admin/runtime/api', () => ({ getBadges: vi.fn(), saveBadge: vi.fn(), uploadBadgeAsset: vi.fn(), deleteBadge: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('../src/admin/runtime/i18n-text', () => ({ adminText: (key: string) => key }))
vi.mock('../src/admin/runtime/toast', () => ({ adminToast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }))
vi.mock('../src/admin/components/global-layout', () => ({ BasicPage: { template: '<div><slot name="actions" /><slot /></div>' } }))
vi.mock('../src/admin/components/ui/dialog', () => ({
  Dialog: { props: ['open'], template: '<div v-if="open"><slot /></div>' },
  DialogContent: { template: '<div><slot /></div>' }, DialogHeader: { template: '<div><slot /></div>' },
  DialogTitle: { template: '<h2><slot /></h2>' }, DialogDescription: { template: '<p><slot /></p>' }, DialogFooter: { template: '<div><slot /></div>' },
}))
vi.mock('../src/admin/components/AdminSelect.vue', () => ({ default: {
  props: ['modelValue', 'options'], emits: ['update:modelValue'],
  template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :value="option.value">{{ option.label }}</option></select>',
} }))
const badge = { code: 'test', name: 'Original badge', description: 'A badge', type: 'custom', grantMode: 'manual', iconUrl: '/static/badges/contributor.svg', color: 'blue', level: 'bronze', isEnabled: true, isWearable: true, sortOrder: 1 }
afterEach(() => vi.resetAllMocks())
async function editor() {
  vi.mocked(getBadges).mockResolvedValue([badge])
  const wrapper = mount(BadgesManagementPage, { props: { payload: {} as never }, global: { stubs: { UserAvatar: true } } })
  await flushPromises()
  await wrapper.findAll('button').find(b => b.attributes('title') === 'Original badge · test')!.trigger('click')
  return wrapper
}
async function upload(wrapper: Awaited<ReturnType<typeof editor>>) {
  const input = wrapper.get('input[type="file"]')
  Object.defineProperty(input.element, 'files', { configurable: true, value: [new File(['GIF89a'], 'badge.gif', { type: 'image/gif' })] })
  await input.trigger('change')
}
it('keeps existing local paths and links editable, previews and saves the new upload', async () => {
  const wrapper = await editor()
  expect(wrapper.findAllComponents(BadgeStylePreview)).toHaveLength(1)
  await wrapper.get('#badge-artwork').setValue('https://cdn.example.com/legacy.svg')
  expect(wrapper.getComponent(BadgeStylePreview).props('badge').iconUrl).toBe('https://cdn.example.com/legacy.svg')
  vi.mocked(uploadBadgeAsset).mockResolvedValue({ url: '/static/badges/upload.gif' })
  await upload(wrapper); await flushPromises()
  expect((wrapper.get('#badge-artwork').element as HTMLInputElement).value).toBe('/static/badges/upload.gif')
  expect(wrapper.getComponent(BadgeStylePreview).props('badge').iconUrl).toBe('/static/badges/upload.gif')
  expect(badge.iconUrl).toBe('/static/badges/contributor.svg')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(saveBadge).toHaveBeenCalledWith(expect.objectContaining({ iconUrl: '/static/badges/upload.gif' }))
  wrapper.unmount()
})
it('blocks saving during an upload and retains the draft after failure for retry', async () => {
  const wrapper = await editor()
  let reject!: (error: Error) => void
  vi.mocked(uploadBadgeAsset).mockReturnValue(new Promise((_, fail) => { reject = fail }))
  await upload(wrapper)
  expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()
  await wrapper.get('form').trigger('submit')
  expect(saveBadge).not.toHaveBeenCalled()
  reject(new Error('Upload unavailable')); await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toBe('Upload unavailable')
  expect((wrapper.get('#badge-artwork').element as HTMLInputElement).value).toBe(badge.iconUrl)
  vi.mocked(uploadBadgeAsset).mockResolvedValue({ url: '/static/badges/retry.png' })
  await upload(wrapper); await flushPromises()
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  wrapper.unmount()
})
it('previews both backgrounds and reports failed artwork without breaking the form', async () => {
  const wrapper = mount(BadgeStylePreview, { props: { badge }, global: { stubs: { UserAvatar: true } } })
  expect(wrapper.findAll('section')).toHaveLength(2)
  await wrapper.get('img').trigger('error')
  expect(wrapper.get('[role="status"]').text()).toBe('badgeAdmin.previewUnavailable')
  await wrapper.setProps({ badge: { ...badge, iconUrl: '/static/badges/new.gif' } })
  expect(wrapper.find('[role="status"]').exists()).toBe(false)
  wrapper.unmount()
})
it('uses system framing for normalized uploads and preserves legacy artwork', () => {
  expect(isSystemBadgeArtwork(badge)).toBe(true)
  expect(isSystemBadgeArtwork({ ...badge, iconUrl: '/static/badges/normalized/12345678-1234-1234-1234-123456789abc.gif' })).toBe(true)
  expect(isSystemBadgeArtwork({ ...badge, iconUrl: '/static/badges/12345678-1234-1234-1234-123456789abc.gif' })).toBe(false)
})
