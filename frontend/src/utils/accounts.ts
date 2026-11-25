import type { SpaceRole } from '@/api/generated'
import { roleAtLeast } from '@/utils/permissions'

export const SPACE_ROLES: SpaceRole[] = ['owner', 'admin', 'editor', 'viewer']

// The invite link is shown exactly once (SPEC Decision 41): the token is
// stored only as a hash, so it can't be reconstructed later.
export function inviteLink (origin: string, token: string): string {
  return `${origin}/invite/${encodeURIComponent(token)}`
}

// Roles the caller may grant/change to in the members UI. Only space owners
// (and org admins, reported as owner) may grant, change or revoke owner
// (Decision 46) — hiding it for admins avoids offering a guaranteed 403.
export function assignableRoles (callerRole: SpaceRole | null | undefined): SpaceRole[] {
  if (!roleAtLeast(callerRole, 'admin')) {
    return []
  }
  return callerRole === 'owner' ? SPACE_ROLES : SPACE_ROLES.filter(r => r !== 'owner')
}

// An admin cannot touch an existing owner's row.
export function canChangeMemberRole (callerRole: SpaceRole | null | undefined, memberRole: SpaceRole): boolean {
  if (!roleAtLeast(callerRole, 'admin')) {
    return false
  }
  return memberRole !== 'owner' || callerRole === 'owner'
}

export function expiryFromDays (days: number | null | undefined, now = new Date()): string | undefined {
  if (days == null || !Number.isFinite(days) || days <= 0) {
    return undefined
  }
  return new Date(now.getTime() + days * 86_400_000).toISOString()
}

const MIB = 1024 * 1024
export const MAX_ATTACHMENT_MIB = 100

export function bytesToMib (bytes: number): number {
  return Math.round((bytes / MIB) * 100) / 100
}

export function mibToBytes (mib: number): number {
  return Math.round(mib * MIB)
}

// One MIME type / pattern per line (commas also accepted); blank lines dropped,
// duplicates removed. Empty result = invalid (server needs minItems 1).
export function parseAllowlist (text: string): string[] {
  const seen = new Set<string>()
  for (const part of text.split(/[\n,]/)) {
    const v = part.trim()
    if (v) {
      seen.add(v)
    }
  }
  return [...seen]
}

export function formatAllowlist (list: string[]): string {
  return list.join('\n')
}

export function slugify (name: string): string {
  return name
    .toLowerCase()
    .normalize('NFKD')
    .replaceAll(/[\u0300-\u036F]/g, '')
    .replaceAll(/[^a-z0-9]+/g, '-')
    .replaceAll(/^-+|-+$/g, '')
}

// Blank / non-positive input means "no limit set" (null); anything else must be
// a whole number >= 1. Returns undefined when invalid.
export function parseOptionalPositiveInt (value: string | number | null | undefined): number | null | undefined {
  if (value === null || value === undefined || String(value).trim() === '') {
    return null
  }
  const n = Number(value)
  return Number.isInteger(n) && n >= 1 ? n : undefined
}

// Maps an invite endpoint status to an i18n key. 410 = used/revoked/expired,
// 409 = email taken, 429 = rate limited (Decisions 35, 41).
export function redeemErrorKey (status: number, phase: 'resolve' | 'redeem'): string {
  switch (status) {
    case 404: { return 'redeem.invalid' }
    case 410: { return 'redeem.gone' }
    case 409: { return 'redeem.emailTaken' }
    case 429: { return 'redeem.rateLimited' }
    default: { return phase === 'resolve' ? 'redeem.invalid' : 'redeem.failed' }
  }
}

export interface Candidate { value: string, title: string, subtitle?: string }
