import { describe, expect, it, vi } from 'vitest'
import { createAttachmentBlobCache } from '@/utils/attachmentBlobCache'

function setup (maxIdle = 2) {
  let n = 0
  const fetchBlob = vi.fn(async (_id: string) => new Blob(['x'], { type: 'image/png' }))
  const createObjectURL = vi.fn(() => `blob:${++n}`)
  const revokeObjectURL = vi.fn()
  return { cache: createAttachmentBlobCache({ fetchBlob, createObjectURL, revokeObjectURL, maxIdle }), fetchBlob, createObjectURL, revokeObjectURL }
}

describe('attachmentBlobCache', () => {
  it('fetches once for concurrent and repeated acquires', async () => {
    const { cache, fetchBlob } = setup()
    const [a, b] = await Promise.all([cache.acquire('x'), cache.acquire('x')])
    expect(a).toBe(b)
    cache.release('x')
    cache.release('x')
    await cache.acquire('x')
    expect(fetchBlob).toHaveBeenCalledTimes(1)
  })

  it('keeps idle entries but revokes the oldest beyond the cap', async () => {
    const { cache, revokeObjectURL } = setup(1)
    const first = await cache.acquire('a')
    await cache.acquire('b')
    cache.release('a')
    expect(revokeObjectURL).not.toHaveBeenCalled()
    cache.release('b')
    expect(revokeObjectURL).toHaveBeenCalledWith(first)
    expect(cache.size()).toBe(1)
  })

  it('never revokes a held entry', async () => {
    const { cache, revokeObjectURL } = setup(0)
    await cache.acquire('a')
    await cache.acquire('b')
    cache.release('b')
    expect(revokeObjectURL).toHaveBeenCalledTimes(1)
    expect(cache.size()).toBe(1)
  })

  it('clear revokes everything', async () => {
    const { cache, revokeObjectURL } = setup()
    await cache.acquire('a')
    await cache.acquire('b')
    cache.clear()
    expect(revokeObjectURL).toHaveBeenCalledTimes(2)
    expect(cache.size()).toBe(0)
  })

  it('does not cache failures', async () => {
    const { cache, fetchBlob } = setup()
    fetchBlob.mockRejectedValueOnce(new Error('boom'))
    await expect(cache.acquire('a')).rejects.toThrow('boom')
    await expect(cache.acquire('a')).resolves.toMatch(/^blob:/)
  })
})
