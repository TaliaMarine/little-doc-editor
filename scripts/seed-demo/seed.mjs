#!/usr/bin/env node
// Repeatable demo-data seed. Talks to the REST API only (no SQL), so it
// exercises the same paths the UI does: invites -> redemption, spaces,
// members, nested pages, attachments, content saves (=> history), share links.
//
// Re-running is safe: every step looks for what it would create first (users by
// email, spaces by slug, pages by title under the same parent, attachments by
// filename, share links per page) and skips it. Existing page *content* is not
// overwritten, so a re-run never clobbers edits made since. To start over, use a
// fresh database (see DEVELOPER.md).
//
// Env: DEMO_API_URL (default http://localhost:8080/api/v1), DEMO_ADMIN_EMAIL,
// DEMO_ADMIN_PASSWORD (the api's BOOTSTRAP_ADMIN_*), DEMO_USER_PASSWORD.
import { ADMIN, API_URL, MAIN_SPACE_SLUG, PENDING_INVITE_EMAIL, SPACES, TITLES, USERS } from './config.mjs'
import { diagramPng, textPdf } from './assets.mjs'
import { code, doc, h, hr, image, link, mermaid, ol, p, quote, t, ul } from './content.mjs'

async function call (method, path, { token, body, form, ok = [200, 201, 204], quiet = [] } = {}) {
  const headers = {}
  if (token) headers.Authorization = `Bearer ${token}`
  let payload
  if (form) payload = form
  else if (body !== undefined) { headers['Content-Type'] = 'application/json'; payload = JSON.stringify(body) }
  const res = await fetch(API_URL + path, { method, headers, body: payload })
  const text = await res.text()
  const json = text ? JSON.parse(text) : null
  if (quiet.includes(res.status)) return { status: res.status, json }
  if (!ok.includes(res.status)) throw new Error(`${method} ${path} -> ${res.status} ${text.slice(0, 300)}`)
  return { status: res.status, json }
}

const login = async ({ email, password }) => (await call('POST', '/auth/login', { body: { email, password } })).json.token

// --- accounts ---------------------------------------------------------------

async function ensureUsers (adminToken) {
  const existing = (await call('GET', '/users', { token: adminToken })).json
  const byEmail = new Map(existing.map(u => [u.email, u]))
  const ids = {}
  for (const [key, u] of Object.entries(USERS)) {
    let user = byEmail.get(u.email)
    if (!user) {
      const invite = (await call('POST', '/invites', { token: adminToken, body: { email: u.email } })).json
      await call('POST', `/invites/${invite.token}/redeem`, { body: { email: u.email, displayName: u.displayName, password: u.password } })
      user = (await call('GET', '/users', { token: adminToken })).json.find(x => x.email === u.email)
      console.log(`  user ${u.displayName} (invite redeemed)`)
    }
    ids[key] = user.id
  }
  // One invite stays pending so the invites admin screen has a live row.
  const invites = (await call('GET', '/invites', { token: adminToken })).json
  if (!invites.some(i => i.email === PENDING_INVITE_EMAIL && i.status === 'pending') && !byEmail.has(PENDING_INVITE_EMAIL)) {
    await call('POST', '/invites', { token: adminToken, body: { email: PENDING_INVITE_EMAIL } })
    console.log(`  pending invite for ${PENDING_INVITE_EMAIL}`)
  }
  return ids
}

// --- spaces + members -------------------------------------------------------

async function ensureSpaces (adminToken, userIds) {
  const spaces = {}
  const list = (await call('GET', '/spaces', { token: adminToken })).json
  for (const def of SPACES) {
    let space = list.find(s => s.slug === def.slug)
    if (!space) {
      space = (await call('POST', '/spaces', { token: adminToken, body: { name: def.name, slug: def.slug, icon: def.icon } })).json
      console.log(`  space ${def.name}`)
    }
    spaces[def.slug] = space
    for (const [userKey, role] of Object.entries(def.members)) {
      const res = await call('POST', `/spaces/${space.id}/members`, { token: adminToken, body: { userId: userIds[userKey], role }, quiet: [409] })
      if (res.status === 409) await call('PATCH', `/spaces/${space.id}/members/${userIds[userKey]}`, { token: adminToken, body: { role } })
    }
  }
  return spaces
}

// --- page content -----------------------------------------------------------

const architecture = att => doc(
  h(1, TITLES.architecture),
  p('Meridian Labs runs three services that scale independently. This page is the map; the runbooks below it are the territory.'),
  ...(att.diagram ? [p(image(att.diagram, 'Request path: browser, gateway, workers'))] : []),
  h(2, 'Request flow'),
  mermaid(`flowchart LR
  Browser[Browser] --> Gateway{{Gateway}}
  Gateway --> Telemetry[Telemetry API]
  Gateway --> Scheduler[Scheduler]
  Telemetry --> Store[(Timeseries store)]
  Scheduler --> Queue[[Job queue]]
  Queue --> Workers[Observation workers]
  Workers --> Store`),
  h(2, 'Service ownership'),
  ul(
    [t('Gateway', 'bold'), ' - terminates TLS, rate limits, routes by path. Owner: Platform.'],
    [t('Telemetry API', 'bold'), ' - reads and writes instrument readings. Owner: Data.'],
    [t('Scheduler', 'bold'), ' - plans nightly observation runs. Owner: Observations.'],
  ),
  h(2, 'Conventions'),
  quote('If a diagram and the code disagree, the code is right and the diagram is a bug. Fix the diagram in the same pull request.'),
  p('Release steps live in ', link('Deploying to staging', '#'), '; when something is on fire, start at ', link('Incident response', '#'), '.'),
)

const incidentV1 = doc(
  h(1, TITLES.incident),
  p('Draft: page the on-call, then figure it out.'),
)
const incidentV2 = doc(
  h(1, TITLES.incident),
  p('When an alert fires and customers might notice, follow these steps in order.'),
  ol('Acknowledge the page within 5 minutes.', 'Open an incident channel named #inc-YYYYMMDD-short-name.', 'Post a one-line summary and who is incident lead.'),
)
const incidentV3 = doc(
  h(1, TITLES.incident),
  p('When an alert fires and customers might notice, follow these steps in order. Severity levels are defined below.'),
  ol(
    'Acknowledge the page within 5 minutes.',
    'Open an incident channel named #inc-YYYYMMDD-short-name.',
    'Post a one-line summary and name an incident lead.',
    'Mitigate first (roll back, fail over, disable the flag), investigate second.',
    'Update the status page every 30 minutes until resolved.',
  ),
  h(2, 'Severity levels'),
  ul(
    [t('SEV1', 'bold'), ': the telescope fleet is unreachable or data is being lost.'],
    [t('SEV2', 'bold'), ': degraded service, a workaround exists.'],
    [t('SEV3', 'bold'), ': cosmetic or internal-only impact.'],
  ),
  h(2, 'Useful commands'),
  code('kubectl -n meridian rollout undo deploy/telemetry-api\nkubectl -n meridian logs deploy/scheduler --since=15m | grep ERROR', 'bash'),
  quote('Blameless reviews are due within five working days of resolution.'),
)

// key -> builder. `versions` are saved in order by the named users first so the
// page has a history; the final `content` is saved last by the admin.
const PAGES = [
  {
    title: TITLES.welcome,
    content: () => doc(
      h(1, TITLES.welcome),
      p('Meridian Labs builds software for a small network of imaginary observatories. This handbook is where we write down how we work, so nobody has to ask twice.'),
      h(2, 'Start here'),
      ul('Read Your first week, then pick a buddy from the team directory.', 'Skim the Engineering section before your first on-call shadow.', 'Company policies are short. Read them.'),
      hr(),
      p(t('Everything in this space is fictional demo data.', 'italic')),
    ),
    children: [
      {
        title: 'Your first week',
        content: () => doc(
          h(1, 'Your first week'),
          ol('Day 1: laptop, accounts, and lunch with your buddy.', 'Day 2: ship a one-line change to the docs.', 'Day 3: shadow the on-call engineer.', 'Day 5: retro with your manager.'),
          p('Questions go in #ask-anything; there are no bad ones.'),
        ),
      },
      {
        title: 'Team directory',
        content: () => doc(h(1, 'Team directory'), ul('Platform: Odette Marsh (lead)', 'Data: Mira Okonkwo', 'Observations: Juniper Calloway', 'Operations: Bram Tolliver')),
      },
    ],
  },
  {
    title: 'Engineering',
    content: () => doc(h(1, 'Engineering'), p('Architecture, runbooks and design documents for the Meridian platform.')),
    children: [
      {
        title: TITLES.architecture,
        attachments: [{ key: 'diagram', filename: 'request-flow.png', type: 'image/png', data: () => diagramPng() }],
        content: architecture,
      },
      {
        title: 'Runbooks',
        content: () => doc(h(1, 'Runbooks'), p('Step-by-step procedures. Keep them short enough to follow at 3 a.m.')),
        children: [
          { title: TITLES.incident, versions: [['odette', incidentV1], ['mira', incidentV2]], content: () => incidentV3 },
          {
            title: 'Deploying to staging',
            content: () => doc(
              h(1, 'Deploying to staging'),
              p('Staging deploys on every merge to main. To deploy a branch by hand:'),
              code('just deploy-staging BRANCH=my-feature', 'bash'),
              p('The deploy is healthy when the smoke test page shows three green checks.'),
            ),
          },
        ],
      },
      {
        title: 'Design docs',
        content: () => doc(h(1, 'Design docs'), p('One page per proposal, using the RFC template.')),
        children: [
          {
            title: 'RFC-012: Nightly observation planner',
            content: () => doc(
              h(1, 'RFC-012: Nightly observation planner'),
              h(2, 'Motivation'),
              p('Planning runs by hand takes an hour each evening and is easy to get wrong when the weather changes.'),
              h(2, 'Proposal'),
              p('Let the scheduler propose a plan from the weather forecast and the target priority list, and let a human approve it.'),
              h(2, 'Open questions'),
              ul('How do we weight targets that are only visible for a few nights?', 'Who approves when the duty astronomer is out?'),
            ),
          },
        ],
      },
    ],
  },
  {
    title: 'Company policies',
    content: () => doc(h(1, 'Company policies'), p('Short, plain-language policies. If one is unclear, that is a bug in the policy.')),
    children: [
      {
        title: TITLES.expense,
        attachments: [{
          key: 'form',
          filename: 'expense-form.pdf',
          type: 'application/pdf',
          data: () => textPdf([
            { title: 'Expense claim form', lines: ['Meridian Labs (fictional demo document)', '', 'Employee: ______________________', 'Date: __________________________', 'Amount: ________________________', 'Category: travel / equipment / other', '', 'Attach receipts for anything over 25.'] },
            { title: 'Approval', lines: ['Manager signature: ______________', 'Finance reference: ______________', '', 'Claims are paid on the 15th of each month.'] },
          ]),
        }],
        content: () => doc(
          h(1, TITLES.expense),
          p('Spend company money like you would spend your own, then write it down. Claims are reimbursed on the 15th.'),
          ul('Receipts are required over 25.', 'Equipment over 500 needs manager approval first.', 'Travel is booked through the travel desk.'),
          p('Download and attach the expense form (PDF) to the claim.'),
        ),
      },
      {
        title: 'Remote work',
        content: () => doc(h(1, 'Remote work'), p('Work from wherever you do your best work, within these hours of overlap: 10:00 to 15:00 in the team time zone.')),
      },
    ],
  },
]

const GARDEN = [
  {
    title: 'Spring planting calendar',
    content: () => doc(h(1, 'Spring planting calendar'), ul('March: peas and broad beans under cloches.', 'April: carrots, beetroot, early potatoes.', 'May: courgettes and runner beans after the last frost.')),
    children: [{ title: 'Seed swap notes', content: () => doc(h(1, 'Seed swap notes'), p('Bring labelled envelopes. Heritage tomatoes go first.')) }],
  },
  { title: 'Compost rota', content: () => doc(h(1, 'Compost rota'), p('Turn the heap every second Saturday; the rota is on the shed door.')) },
]
const TRAIL = [
  {
    title: 'North loop',
    content: () => doc(h(1, 'North loop'), p('6.5 km, mostly flat, best at low tide. Allow two hours including the viewpoint.'), ul('Start: harbour car park', 'Water: tap at the lighthouse', 'Dogs: on leads April to July')),
  },
]
const TREES = { [MAIN_SPACE_SLUG]: PAGES, 'aurora-garden-club': GARDEN, 'lighthouse-trail-guide': TRAIL }

// --- pages ------------------------------------------------------------------

async function ensurePages (ctx, space, defs, parentId = null) {
  const siblings = () => ctx.pageLists.get(space.id)
  for (const def of defs) {
    let page = siblings().find(x => x.title === def.title && (x.parent_id ?? null) === parentId)
    if (!page) {
      page = (await call('POST', `/spaces/${space.id}/pages`, { token: ctx.admin, body: { title: def.title, parentId, content: doc(p('')) } })).json
      siblings().push(page)
      const att = {}
      for (const a of def.attachments ?? []) {
        const form = new FormData()
        form.set('file', new Blob([a.data()], { type: a.type }), a.filename)
        att[a.key] = (await call('POST', `/pages/${page.id}/attachments`, { token: ctx.admin, form })).json.id
      }
      // Earlier versions are saved by other members so the history drawer has entries.
      for (const [userKey, content] of def.versions ?? []) {
        await call('PUT', `/pages/${page.id}/content`, { token: ctx.tokens[userKey], body: { content } })
      }
      await call('PUT', `/pages/${page.id}/content`, { token: ctx.admin, body: { content: def.content(att) } })
      console.log(`  page ${def.title}`)
    }
    if (def.children) await ensurePages(ctx, space, def.children, page.id)
  }
}

// A view link on the architecture page, reused if one is already active.
async function ensureShareLink (ctx, space) {
  const page = ctx.pageLists.get(space.id).find(x => x.title === TITLES.architecture)
  const links = (await call('GET', `/pages/${page.id}/share-links`, { token: ctx.admin })).json
  let link = links.find(l => !l.revoked_at && l.permission === 'view' && !l.requires_password)
  if (!link) {
    link = (await call('POST', `/pages/${page.id}/share-links`, { token: ctx.admin, body: { permission: 'view' } })).json
    console.log('  share link on the architecture page')
  }
  return link
}

async function main () {
  console.log(`Seeding ${API_URL}`)
  const admin = await login(ADMIN)
  const userIds = await ensureUsers(admin)
  const tokens = {}
  for (const [key, u] of Object.entries(USERS)) tokens[key] = await login(u)
  const spaces = await ensureSpaces(admin, userIds)
  const ctx = { admin, tokens, pageLists: new Map() }
  for (const def of SPACES) {
    const space = spaces[def.slug]
    ctx.pageLists.set(space.id, (await call('GET', `/spaces/${space.id}/pages`, { token: admin })).json)
    await ensurePages(ctx, space, TREES[def.slug])
  }
  const shareLink = await ensureShareLink(ctx, spaces[MAIN_SPACE_SLUG])
  console.log(`Done. Guest link path: /share/${shareLink.token}`)
}

main().catch(e => { console.error(e.message); process.exit(1) })
