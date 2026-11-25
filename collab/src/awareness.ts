// Presence (awareness) hardening. Awareness is arbitrary client-supplied
// JSON that Hocuspocus relays to every peer, so without this a client could
// show up as someone else (cursor label spoofing) or push junk/huge state at
// everyone — including read-only viewers and share-link guests.
//
// Policy, applied to every inbound awareness update before it is applied or
// broadcast:
//   - only the keys `user` and `cursor` survive (y-tiptap's cursor plugin
//     reads these; anything else is dropped);
//   - `user.name` is pinned to the authenticated displayName; `user.color` is
//     kept only if it is a plain CSS hex color, else dropped;
//   - read-only connections get only the pinned `user` — no cursor/selection
//     state, since they can't edit and it is the easiest channel for abuse;
//   - `cursor` must be {anchor, head} with finite-number-like values.
const HEX_COLOR_RE = /^#[0-9a-f]{3,8}$/i

export function pinAwarenessState (
  state: Record<string, any>,
  displayName: string,
  readOnly: boolean,
): Record<string, any> {
  const user: Record<string, string> = { name: displayName }
  const color = state?.user?.color
  if (typeof color === 'string' && HEX_COLOR_RE.test(color)) {
    user.color = color
  }
  const out: Record<string, any> = { user }
  if (!readOnly && state?.cursor && typeof state.cursor === 'object') {
    const { anchor, head } = state.cursor as Record<string, unknown>
    // y-tiptap encodes relative positions as JSON objects; accept objects only.
    if (anchor && head && typeof anchor === 'object' && typeof head === 'object'
      && JSON.stringify(state.cursor).length < 2048) {
      out.cursor = { anchor, head }
    }
  }
  return out
}

// Awareness clientId ownership. Without it any connection could send an update
// for another participant's clientId (visible in broadcasts) and overwrite or
// remove their presence, or flood fake clientIds. A connection may only write
// or remove clientIds it claimed first, and claims at most MAX_CLIENT_IDS.
const MAX_CLIENT_IDS = 2

export class AwarenessOwnership<C extends object> {
  private owners = new Map<number, C>()
  private counts = new WeakMap<C, number>()

  // isLive: whether an earlier owner is still connected (else its ids are free).
  filter (conn: C, states: Map<number, unknown>, removed: Set<number>, isLive: (c: C) => boolean): void {
    for (const id of [...states.keys()]) {
      if (!this.claim(conn, id, isLive)) states.delete(id)
    }
    for (const id of [...removed]) {
      if (this.owners.get(id) !== conn) removed.delete(id)
    }
  }

  private claim (conn: C, id: number, isLive: (c: C) => boolean): boolean {
    const owner = this.owners.get(id)
    if (owner === conn) return true
    if (owner && isLive(owner)) return false
    if ((this.counts.get(conn) ?? 0) >= MAX_CLIENT_IDS) return false
    this.owners.set(id, conn)
    this.counts.set(conn, (this.counts.get(conn) ?? 0) + 1)
    return true
  }
}
