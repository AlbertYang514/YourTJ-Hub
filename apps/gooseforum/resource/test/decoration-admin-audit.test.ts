// @vitest-environment happy-dom
import { afterEach, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UserDecorationsManagementPage from '../src/admin/pages/management/UserDecorationsManagementPage.vue'
import { changeUserDecoration, getUserDecorations, getUserList } from '../src/admin/runtime/api'
import type { DecorationState } from '@gooseforum/client'
vi.mock('../src/admin/runtime/api', () => ({ getUserDecorations: vi.fn(), changeUserDecoration: vi.fn(), getUserList: vi.fn() }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))
vi.mock('../src/admin/runtime/i18n-text', () => ({ adminText: (key: string) => key }))
vi.mock('../src/admin/components/global-layout', () => ({ BasicPage: { template: '<div><slot /></div>' } }))
vi.mock('../src/admin/components/AdminSelect.vue', () => ({ default: {
  props: ['modelValue', 'options', 'disabled'], emits: ['update:modelValue'],
  template: '<select :value="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
} }))
vi.mock('../src/admin/components/AdminConfirmDialog.vue', () => ({ default: {
  props: ['open', 'loading', 'description'], emits: ['confirm', 'update:open'],
  template: '<div v-if="open" role="dialog">{{ description }}<button data-testid="confirm-revoke" :disabled="loading" @click="$emit(\'confirm\')">Confirm</button><button data-testid="cancel-revoke" @click="$emit(\'update:open\', false)">Cancel</button></div>',
} }))
vi.mock('../src/admin/components/DecorationStylePreview.vue', () => ({ default: {
  props: ['style'], template: '<div data-testid="style-preview">{{ style.name }}</div>',
} }))
const state: DecorationState = { catalog: [{ code: 'frame-demo', name: 'Frame demo', kind: 'avatarFrame', lightColors: ['#166534'], darkColors: ['#86efac'], isEnabled: true, sortOrder: 1 }], ownedCodes: [], equipped: { avatarFrame: '', nicknameStyle: '' }, audits: [{ id: 1, requestKey: 'old', userId: 7, code: 'frame-demo', action: 'grant', source: 'manual', actorId: 1, reason: 'Original grant', createdAt: '2026-10-13T00:00:00Z' }] }
afterEach(() => vi.resetAllMocks())
it('does not change the previously loaded account after entering a different user ID', async () => {
  vi.mocked(getUserDecorations).mockResolvedValue(structuredClone(state))
  const wrapper = mount(UserDecorationsManagementPage)
  await wrapper.get('#decoration-user-id').setValue(7); await wrapper.get('form').trigger('submit'); await flushPromises()
  await wrapper.get('select').setValue('frame-demo'); await wrapper.get('textarea').setValue('Helpful member')
  await wrapper.get('#decoration-user-id').setValue(8)
  const grant = wrapper.findAll('button').find(button => button.text() === 'decoration.grant')!
  expect(grant.attributes('disabled')).toBeDefined()
  await grant.trigger('click'); await flushPromises()
  expect(changeUserDecoration).not.toHaveBeenCalled()
  wrapper.unmount()
})
it('refreshes audit history after grant; read failure keeps existing log and does not repeat the completed write', async () => {
  vi.mocked(getUserDecorations).mockResolvedValueOnce(structuredClone(state)).mockRejectedValueOnce(new Error('Audit read failed'))
  vi.mocked(changeUserDecoration).mockResolvedValue({ ...state, audits: undefined, ownedCodes: ['frame-demo'] })
  const wrapper = mount(UserDecorationsManagementPage)
  await wrapper.get('#decoration-user-id').setValue(7); await wrapper.get('form').trigger('submit'); await flushPromises()
  await wrapper.get('select').setValue('frame-demo'); await wrapper.get('textarea').setValue('Helpful member')
  const grant = wrapper.findAll('button').find(button => button.text() === 'decoration.grant')!
  await grant.trigger('click'); await flushPromises()
  expect(getUserDecorations).toHaveBeenCalledTimes(2)
  expect(changeUserDecoration).toHaveBeenCalledTimes(1)
  expect(wrapper.text()).toContain('Original grant')
  expect(wrapper.get('[role="alert"]').text()).toContain('Audit read failed')
  expect(wrapper.get('[role="alert"]').text()).toContain('decoration.grantsAdmin.refreshFailed')
  expect(wrapper.get('[role="status"]').text()).toContain('decoration.grantsAdmin.grantSuccess')
  expect(grant.attributes('disabled')).toBeDefined()
  expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('')
  wrapper.unmount()
})

it('keeps the request key when retrying an ambiguous write failure, including after a read refresh', async () => {
  vi.mocked(getUserDecorations).mockResolvedValue(structuredClone(state))
  vi.mocked(changeUserDecoration).mockRejectedValueOnce(new Error('Network interrupted')).mockResolvedValueOnce({ ...state, ownedCodes: ['frame-demo'] })
  const wrapper = mount(UserDecorationsManagementPage)
  await wrapper.get('#decoration-user-id').setValue(7); await wrapper.get('form').trigger('submit'); await flushPromises()
  await wrapper.get('select').setValue('frame-demo'); await wrapper.get('textarea').setValue(' Helpful member ')
  await wrapper.get('[data-testid="grant"]').trigger('click'); await flushPromises()
  const firstBody = vi.mocked(changeUserDecoration).mock.calls[0][1]
  expect(firstBody.reason).toBe('Helpful member')
  expect(firstBody.requestKey).toBeTruthy()
  expect(wrapper.get('[role="alert"]').text()).toContain('Network interrupted')
  expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe(' Helpful member ')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  await wrapper.get('[data-testid="grant"]').trigger('click'); await flushPromises()
  expect(changeUserDecoration).toHaveBeenCalledTimes(2)
  expect(vi.mocked(changeUserDecoration).mock.calls[1][1]).toEqual(firstBody)
  wrapper.unmount()
})

it('confirms revocation before writing and shows the updated inventory and equipment', async () => {
  const ownedState = { ...structuredClone(state), ownedCodes: ['frame-demo'], equipped: { avatarFrame: 'frame-demo', nicknameStyle: '' } }
  vi.mocked(getUserDecorations).mockResolvedValueOnce(ownedState).mockResolvedValueOnce(structuredClone(state))
  vi.mocked(changeUserDecoration).mockResolvedValue(structuredClone(state))
  const wrapper = mount(UserDecorationsManagementPage)
  await wrapper.get('#decoration-user-id').setValue(7); await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(wrapper.text()).toContain('decoration.grantsAdmin.equipped')
  await wrapper.get('select').setValue('frame-demo'); await wrapper.get('textarea').setValue('Accidental grant')
  expect(wrapper.get('[data-testid="style-preview"]').text()).toBe('Frame demo')
  expect(wrapper.get('[data-testid="grant"]').attributes('disabled')).toBeDefined()
  await wrapper.get('[data-testid="revoke"]').trigger('click'); await flushPromises()
  expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
  expect(changeUserDecoration).not.toHaveBeenCalled()
  await wrapper.get('[data-testid="cancel-revoke"]').trigger('click')
  expect(changeUserDecoration).not.toHaveBeenCalled()
  await wrapper.get('[data-testid="revoke"]').trigger('click')
  await wrapper.get('[data-testid="confirm-revoke"]').trigger('click'); await flushPromises()
  expect(changeUserDecoration).toHaveBeenCalledWith('revoke', expect.objectContaining({ userId: 7, code: 'frame-demo', reason: 'Accidental grant' }))
  expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  expect(wrapper.text()).toContain('decoration.grantsAdmin.inventoryEmpty')
  expect(wrapper.text()).not.toContain('decoration.grantsAdmin.equipped')
  expect(wrapper.get('[role="status"]').text()).toContain('decoration.grantsAdmin.revokeSuccess')
  wrapper.unmount()
})

it('shows readable audit actions and dates and blocks granting a disabled decoration', async () => {
  vi.mocked(getUserDecorations).mockResolvedValue({ ...structuredClone(state), catalog: [{ ...state.catalog[0], isEnabled: false }] })
  const wrapper = mount(UserDecorationsManagementPage)
  await wrapper.get('#decoration-user-id').setValue(7); await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(wrapper.get('time').text()).not.toBe('2026-10-13T00:00:00Z')
  expect(wrapper.get('time').attributes('datetime')).toBe('2026-10-13T00:00:00Z')
  expect(wrapper.get('tbody').text()).toContain('decoration.grant')
  expect(wrapper.get('tbody').text()).toContain('decoration.grantsAdmin.manualSource')
  await wrapper.get('select').setValue('frame-demo'); await wrapper.get('textarea').setValue('Helpful member')
  expect(wrapper.text()).toContain('decoration.grantsAdmin.disabledHint')
  expect(wrapper.get('[data-testid="grant"]').attributes('disabled')).toBeDefined()
  wrapper.unmount()
})

it('shows empty and failed account states with a read-only retry', async () => {
  vi.mocked(getUserDecorations).mockRejectedValueOnce(new Error('Account not found')).mockResolvedValueOnce({ ...state, catalog: [], audits: [] })
  const wrapper = mount(UserDecorationsManagementPage)
  expect(wrapper.text()).toContain('decoration.grantsAdmin.noAccount')
  await wrapper.get('#decoration-user-id').setValue(7); await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(wrapper.get('[role="alert"]').text()).toContain('Account not found')
  await wrapper.findAll('button').find(button => button.text() === 'decoration.retry')!.trigger('click'); await flushPromises()
  expect(wrapper.text()).toContain('decoration.grantsAdmin.catalogEmpty')
  expect(wrapper.text()).toContain('decoration.grantsAdmin.auditEmpty')
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  expect(changeUserDecoration).not.toHaveBeenCalled()
  wrapper.unmount()
})

it('searches users by username and grants with the resolved numeric id', async () => {
  vi.useFakeTimers()
  try {
    vi.mocked(getUserList).mockResolvedValue({
      list: [{ userId: 42, username: 'bob', nickname: '鲍勃', email: 'bob@example.com', status: 1, validate: 1, actorType: 0, prestige: 0, createTime: '2026-01-01T00:00:00Z' }],
      total: 1,
      page: 1,
    })
    vi.mocked(getUserDecorations).mockResolvedValue(structuredClone(state))
    vi.mocked(changeUserDecoration).mockResolvedValue({ ...state, ownedCodes: ['frame-demo'] })
    const wrapper = mount(UserDecorationsManagementPage)
    await wrapper.get('#decoration-user-search').setValue('bob')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(getUserList).toHaveBeenCalledWith({ page: 1, pageSize: 8, username: 'bob' })
    const candidate = wrapper.findAll('button').find(button => button.text().includes('@bob'))!
    expect(candidate.text()).toContain('#42')
    await candidate.trigger('click')
    expect((wrapper.get('#decoration-user-id').element as HTMLInputElement).value).toBe('42')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(getUserDecorations).toHaveBeenCalledWith(42)
    await wrapper.get('select').setValue('frame-demo'); await wrapper.get('textarea').setValue('Searched by username')
    await wrapper.findAll('button').find(button => button.text() === 'decoration.grant')!.trigger('click')
    await flushPromises()
    expect(vi.mocked(changeUserDecoration).mock.calls[0][1].userId).toBe(42)
    // 服务端只按字段过滤（username/email LIKE、userId 精确），不支持昵称搜索：
    // 搜索框按输入形态只填一个字段。
    await wrapper.get('#decoration-user-search').setValue('bob@example.com')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(getUserList).toHaveBeenLastCalledWith({ page: 1, pageSize: 8, email: 'bob@example.com' })
    await wrapper.get('#decoration-user-search').setValue('42')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(getUserList).toHaveBeenCalledWith({ page: 1, pageSize: 8, userId: 42 })
    expect(getUserList).toHaveBeenLastCalledWith({ page: 1, pageSize: 8, username: '42' })
    wrapper.unmount()
  } finally {
    vi.useRealTimers()
  }
})

it('finds a numeric username as well as an exact numeric user id, without duplicates', async () => {
  vi.useFakeTimers()
  try {
    const user = (userId: number, username: string) => ({ userId, username, nickname: username, email: `${username}@example.com`, status: 1, validate: 1, actorType: 0, prestige: 0, createTime: '2026-01-01T00:00:00Z' })
    vi.mocked(getUserList).mockImplementation(async params => params.userId !== undefined
      ? { list: [user(2333, 'alice')], total: 1, page: 1 }
      : { list: [user(9, '2333'), user(2333, 'alice')], total: 2, page: 1 })
    const wrapper = mount(UserDecorationsManagementPage)
    await wrapper.get('#decoration-user-search').setValue('2333')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(getUserList).toHaveBeenCalledWith({ page: 1, pageSize: 8, userId: 2333 })
    expect(getUserList).toHaveBeenCalledWith({ page: 1, pageSize: 8, username: '2333' })
    const candidates = wrapper.findAll('button').filter(button => button.text().includes('#'))
    expect(candidates.map(button => button.text().includes('#9'))).toContain(true)
    expect(candidates.filter(button => button.text().includes('#2333'))).toHaveLength(1)
    wrapper.unmount()
  } finally {
    vi.useRealTimers()
  }
})

it('reports an empty username search and leaves the numeric id untouched', async () => {
  vi.useFakeTimers()
  try {
    vi.mocked(getUserList).mockResolvedValue({ list: [], total: 0, page: 1 })
    const wrapper = mount(UserDecorationsManagementPage)
    await wrapper.get('#decoration-user-search').setValue('nobody')
    await vi.advanceTimersByTimeAsync(300)
    await flushPromises()
    expect(wrapper.text()).toContain('decoration.grantsAdmin.userSearchEmpty')
    expect((wrapper.get('#decoration-user-id').element as HTMLInputElement).value).toBe('')
    expect(getUserDecorations).not.toHaveBeenCalled()
    wrapper.unmount()
  } finally {
    vi.useRealTimers()
  }
})
