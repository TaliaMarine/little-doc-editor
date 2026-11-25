import { describe, expect, it } from 'vitest'
import { resolveApiBaseUrl, resolveCollabSetting } from '@/config'
import { resolveCollabUrl } from '@/editor/collabSession'

describe('runtime config precedence', () => {
  it('prefers runtime, then env, then default (api)', () => {
    expect(resolveApiBaseUrl({ apiBaseUrl: 'https://api.x/api/v1' }, '/env')).toBe('https://api.x/api/v1')
    expect(resolveApiBaseUrl({ apiBaseUrl: '' }, '/env')).toBe('/env')
    expect(resolveApiBaseUrl({}, undefined)).toBe('/api/v1')
  })
  it('prefers runtime, then env (collab)', () => {
    expect(resolveCollabSetting({ collabUrl: 'wss://c' }, 'wss://e')).toBe('wss://c')
    expect(resolveCollabSetting({ collabUrl: '' }, undefined)).toBeUndefined()
  })
  it('expands a path-only collab setting to the page host', () => {
    expect(resolveCollabUrl('/collab', { protocol: 'https:', host: 'h' }, true)).toBe('wss://h/collab')
  })
})
