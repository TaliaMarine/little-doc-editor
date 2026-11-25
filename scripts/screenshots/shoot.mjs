#!/usr/bin/env node
// Produces docs/screenshots/*.png with headless Chromium against a running,
// seeded stack (see DEVELOPER.md: `just screenshots`). Logs in as the seeded
// demo users through the REST API and injects the session, then visits each
// feature. Fixed 1440x900 viewport so re-runs give stable diffs.
//
// Env: SHOT_BASE_URL (the SPA, default http://localhost:3000), DEMO_API_URL
// (api, used for login and lookups), plus the DEMO_* credentials from
// ../seed-demo/config.mjs.
import { spawnSync } from 'node:child_process'
import { mkdirSync, readdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from 'playwright'
import { ADMIN, API_URL, MAIN_SPACE_SLUG, TITLES, USERS } from '../seed-demo/config.mjs'

const BASE = (process.env.SHOT_BASE_URL ?? 'http://localhost:3000').replace(/\/$/, '')
const OUT = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'docs', 'screenshots')
const VIEWPORT = { width: 1440, height: 900 }
const SESSION_KEY = 'little-doc-editor.session'
mkdirSync(OUT, { recursive: true })

async function api (method, path, token, body) {
  const res = await fetch(API_URL + path, {
    method,
    headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(body ? { 'Content-Type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) throw new Error(`${method} ${path} -> ${res.status}`)
  return res.status === 204 ? null : res.json()
}
const login = c => api('POST', '/auth/login', null, { email: c.email, password: c.password })

const browser = await chromium.launch()
const contexts = []

async function newContext (session) {
  const ctx = await browser.newContext({ viewport: VIEWPORT, deviceScaleFactor: 1, locale: 'en-US', colorScheme: 'light' })
  if (session) {
    await ctx.addInitScript(([key, value]) => localStorage.setItem(key, value), [SESSION_KEY, JSON.stringify(session)])
  }
  contexts.push(ctx)
  return ctx
}
async function openAs (session) {
  const ctx = await newContext(session)
  return ctx.newPage()
}

async function shot (page, name) {
  await page.waitForTimeout(400)
  await page.screenshot({ path: join(OUT, `${name}.png`), animations: 'disabled' })
  console.log(`  ${name}.png`)
}

// --- lookups ------------------------------------------------------------------

const adminSession = await login(ADMIN)
const sessions = { admin: adminSession }
for (const [key, u] of Object.entries(USERS)) sessions[key] = await login(u)

const spaces = await api('GET', '/spaces', adminSession.token)
const main = spaces.find(s => s.slug === MAIN_SPACE_SLUG)
if (!main) throw new Error('demo data missing: run `just demo-seed` first')
const pages = await api('GET', `/spaces/${main.id}/pages`, adminSession.token)
const pageByTitle = title => {
  const found = pages.find(x => x.title === title)
  if (!found) throw new Error(`demo page missing: ${title}`)
  return found
}
const pageUrl = title => `${BASE}/spaces/${main.id}/pages/${pageByTitle(title).id}`

async function openPage (page, title) {
  await page.goto(pageUrl(title))
  await page.waitForSelector('[data-testid="editor"] .ProseMirror')
  // Wait for the live (collab) session, not the read-only fallback.
  await page.waitForFunction(() => /connected|synced|live/i.test(document.querySelector('[data-testid="collab-status"]')?.textContent ?? ''), null, { timeout: 15000 }).catch(() => {})
  await page.waitForTimeout(800)
}

// --- 1. page tree + editor (admin) ---------------------------------------------

const admin = await openAs(adminSession)
await admin.goto(`${BASE}/spaces/${main.id}`)
await admin.waitForSelector('[data-testid^="tree-row-"]')
// Expand every collapsed branch so the nesting is visible.
for (let i = 0; i < 4; i++) {
  const toggles = admin.locator('[data-testid="tree-toggle"][aria-expanded="false"]')
  if (await toggles.count() === 0) break
  await toggles.first().click()
}
await openPage(admin, TITLES.incident)
await shot(admin, '01-page-tree-editor')

// --- 2. live collaboration: two users on one page ------------------------------

const mira = await openAs(sessions.mira)
await openPage(mira, TITLES.incident)
await openPage(admin, TITLES.incident)
// Mira selects a phrase; presence + her cursor then show up in the admin's window.
await mira.locator('[data-testid="editor"] .ProseMirror li').nth(3).click({ clickCount: 3 })
await mira.waitForTimeout(1200)
await shot(admin, '02-live-collaboration')

// --- 3. attachments + PDF viewer -----------------------------------------------

const scrollToStart = (page, selector) => page.locator(selector).first().evaluate(el => el.scrollIntoView({ block: 'start' }))

await openPage(admin, TITLES.expense)
await admin.locator('[aria-label="View PDF"]').first().click()
await admin.waitForSelector('canvas', { timeout: 15000 })
await admin.waitForTimeout(1200)
await shot(admin, '03-attachments-pdf-viewer')
await admin.keyboard.press('Escape')

// Inline image (stored by attachment id, shown from an authenticated blob URL).
await openPage(admin, TITLES.architecture)
await admin.waitForSelector('[data-testid="editor"] img.attachment-image__img')
await shot(admin, '04-inline-image-attachments')

// --- 4. mermaid ----------------------------------------------------------------

await admin.waitForSelector('.code-block--mermaid .code-block__svg svg')
await scrollToStart(admin, '.code-block--mermaid')
await shot(admin, '05-mermaid-diagram')

// --- 5. members screen + invites admin ------------------------------------------

await admin.goto(`${BASE}/spaces/${main.id}/members`)
await admin.waitForSelector('[data-testid="user-members"]')
await shot(admin, '06-space-members')

await admin.goto(`${BASE}/admin/invites`)
await admin.waitForTimeout(1000)
await shot(admin, '07-admin-invites')

// --- 6. share link dialog + guest view ------------------------------------------

await openPage(admin, TITLES.architecture)
await admin.locator('[data-testid="open-share"]').click()
await admin.waitForSelector('[data-testid="share-dialog"]')
await admin.waitForTimeout(800)
await shot(admin, '08-share-link-dialog')
await admin.keyboard.press('Escape')

const links = await api('GET', `/pages/${pageByTitle(TITLES.architecture).id}/share-links`, adminSession.token)
const link = links.find(l => !l.revoked_at && l.permission === 'view' && !l.requires_password)
if (!link) throw new Error('demo share link missing: run `just demo-seed` first')
const guest = await openAs(null)
await guest.goto(`${BASE}/share/${link.token}`)
await guest.waitForSelector('[data-testid="share-name"] input')
await guest.fill('[data-testid="share-name"] input', 'Visiting Heron')
await guest.click('[data-testid="share-enter"]')
await guest.waitForSelector('.ProseMirror')
await guest.waitForTimeout(1500)
await shot(guest, '09-guest-share-view')

// --- 7. history drawer ----------------------------------------------------------

await openPage(admin, TITLES.incident)
await admin.locator('[data-testid="open-history"]').click()
await admin.waitForSelector('[data-testid="history-list"]')
await admin.waitForTimeout(600)
await shot(admin, '10-version-history')
await admin.locator('[data-testid^="revision-"]:not([data-testid="revision-preview"]):not([data-testid="revision-content"]):not([data-testid="revision-restore"]):not([data-testid="revision-restore-confirm"])').nth(1).click()
await admin.waitForSelector('[data-testid="revision-preview"]')
await admin.waitForTimeout(1200)
await shot(admin, '12-version-preview')
await admin.keyboard.press('Escape')

// --- 8. export menu -------------------------------------------------------------

await openPage(admin, TITLES.architecture)
await admin.locator('[data-testid="export-menu"]').click()
await admin.waitForSelector('[data-testid="export-page-md"]')
await shot(admin, '11-export-menu')

// --- 9. search dialog -----------------------------------------------------------

await openPage(admin, TITLES.incident)
await admin.keyboard.press('Control+k')
await admin.waitForSelector('[data-testid="search-dialog"]')
// Search for the first word of a seeded page title so there is always a hit.
await admin.fill('[data-testid="search-input"] input', TITLES.incident.split(/\s+/)[0])
await admin.waitForSelector('[data-testid="search-results"] [data-testid^="search-hit-"]', { timeout: 10000 })
await shot(admin, '13-search')
await admin.keyboard.press('Escape')

// --- 10. dark theme (also exercises mermaid following the theme) --------------

await admin.evaluate(() => localStorage.setItem('lde.theme', 'dark'))
await openPage(admin, TITLES.architecture)
await admin.waitForSelector('.code-block--mermaid .code-block__svg svg')
await scrollToStart(admin, '.code-block--mermaid')
await shot(admin, '14-dark-theme')

await browser.close()

// Shrink the PNGs (lossy palette quantisation) when pngquant is installed;
// skipped silently otherwise. UI screenshots lose nothing visible.
const pngs = readdirSync(OUT).filter(f => f.endsWith('.png')).map(f => join(OUT, f))
const quant = spawnSync('pngquant', ['--force', '--skip-if-larger', '--quality=70-95', '--ext', '.png', ...pngs], { stdio: 'ignore' })
if (quant.error) console.log('  (pngquant not found: PNGs left uncompressed)')

console.log(`Wrote screenshots to ${OUT}`)
