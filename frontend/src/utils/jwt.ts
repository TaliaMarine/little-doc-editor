// Reads the exp claim (no verification — the server does that) so a stored
// session that has already expired is discarded instead of "restored".
export function isTokenExpired (token: string, now = Date.now()): boolean {
  try {
    const payload = JSON.parse(atob(token.split('.', 2)[1]!.replaceAll('-', '+').replaceAll('_', '/')))
    return typeof payload.exp !== 'number' || payload.exp * 1000 <= now
  } catch {
    return true
  }
}
