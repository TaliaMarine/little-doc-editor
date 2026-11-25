import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getAuthToken } from '@/api/client'
import { ShareError, useGuestStore } from '@/stores/guest'

const resolveShareLink = vi.fn()
const redeemShareLink = vi.fn()
vi.mock('@/api/generated', () => ({
  resolveShareLink: (...args: unknown[]) => resolveShareLink(...args),
  redeemShareLink: (...args: unknown[]) => redeemShareLink(...args),
}))
vi.mock('@/api/generated/client.gen', () => ({
  client: { setConfig: vi.fn(), interceptors: { request: { use: vi.fn() }, response: { use: vi.fn() } } },
}))

// header.{exp in 2100}.sig
const jwt = (exp = 4_102_444_800) => `h.${btoa(JSON.stringify({ exp })).replaceAll('=', '')}.s`

describe('guest store', () => {
  beforeEach(() => {
    sessionStorage.clear()
    setActivePinia(createPinia())
    resolveShareLink.mockReset()
    redeemShareLink.mockReset()
  })

  it('refuses to redeem without a display name (Decision 6)', async () => {
    const store = useGuestStore()
    await expect(store.redeem('tok', ' '.repeat(3))).rejects.toMatchObject({ kind: 'invalid' })
    expect(redeemShareLink).not.toHaveBeenCalled()
  })

  it('redeems with the trimmed name and makes the guest token the active api token', async () => {
    redeemShareLink.mockResolvedValue({ data: { token: jwt(), permission: 'edit', pageId: 'p1' }, response: { status: 200 } })
    const store = useGuestStore()
    const session = await store.redeem('tok', '  Gus  ', 'pw')
    expect(redeemShareLink).toHaveBeenCalledWith({ path: { token: 'tok' }, body: { displayName: 'Gus', password: 'pw' } })
    expect(session.displayName).toBe('Gus')
    expect(getAuthToken()).toBe(session.token)
    store.deactivate()
    expect(getAuthToken()).toBeNull()
  })

  it('maps api failures', async () => {
    redeemShareLink.mockResolvedValue({ data: undefined, response: { status: 401 } })
    const store = useGuestStore()
    await expect(store.redeem('tok', 'Gus')).rejects.toBeInstanceOf(ShareError)
    await expect(store.redeem('tok', 'Gus')).rejects.toMatchObject({ kind: 'wrongPassword' })
    resolveShareLink.mockResolvedValue({ data: undefined, response: { status: 410 } })
    await expect(store.resolve('tok')).rejects.toMatchObject({ kind: 'gone' })
  })

  it('restores a stored session per link, drops expired ones', async () => {
    redeemShareLink.mockResolvedValue({ data: { token: jwt(), permission: 'view', pageId: 'p1' }, response: { status: 200 } })
    await useGuestStore().redeem('tok', 'Gus')

    setActivePinia(createPinia())
    const fresh = useGuestStore()
    expect(fresh.restore('other')).toBe(false)
    expect(fresh.restore('tok')).toBe(true)
    expect(fresh.session?.displayName).toBe('Gus')

    fresh.clear()
    expect(fresh.restore('tok')).toBe(false)

    sessionStorage.setItem('little-doc-editor.guest.old', JSON.stringify({ linkToken: 'old', token: jwt(1), permission: 'view', pageId: 'p', displayName: 'x' }))
    expect(fresh.restore('old')).toBe(false)
    expect(sessionStorage.getItem('little-doc-editor.guest.old')).toBeNull()
  })
})
