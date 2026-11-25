// Shared by seed.mjs and ../screenshots/shoot.mjs. Everything here is
// fictional: no real people, companies or addresses (the .example TLD is
// reserved and never resolves).

export const API_URL = (process.env.DEMO_API_URL ?? 'http://localhost:8080/api/v1').replace(/\/$/, '')

// The bootstrap admin the api creates on startup (BOOTSTRAP_ADMIN_EMAIL / _PASSWORD).
export const ADMIN = {
  email: process.env.DEMO_ADMIN_EMAIL ?? 'admin@demo.example',
  password: process.env.DEMO_ADMIN_PASSWORD ?? 'demo-admin-password',
}

const userPassword = process.env.DEMO_USER_PASSWORD ?? 'demo-user-password'

// Accounts created through the invite flow. `role` is the space role they get in
// the spaces listed in SPACES[].members.
export const USERS = {
  mira: { email: 'mira.okonkwo@demo.example', displayName: 'Mira Okonkwo', password: userPassword },
  juniper: { email: 'juniper.calloway@demo.example', displayName: 'Juniper Calloway', password: userPassword },
  bram: { email: 'bram.tolliver@demo.example', displayName: 'Bram Tolliver', password: userPassword },
  odette: { email: 'odette.marsh@demo.example', displayName: 'Odette Marsh', password: userPassword },
}

// An invite that is deliberately left unredeemed so the invites screen has a pending row.
export const PENDING_INVITE_EMAIL = 'new.hire@demo.example'

export const MAIN_SPACE_SLUG = 'meridian-handbook'

// slug -> display + members (user key -> role). The admin is always the owner.
export const SPACES = [
  {
    slug: MAIN_SPACE_SLUG,
    name: 'Meridian Labs Handbook',
    icon: 'mdi-book-open-variant',
    members: { odette: 'admin', mira: 'editor', juniper: 'editor', bram: 'viewer' },
  },
  {
    slug: 'aurora-garden-club',
    name: 'Aurora Garden Club',
    icon: 'mdi-flower-outline',
    members: { mira: 'owner', bram: 'editor' },
  },
  {
    slug: 'lighthouse-trail-guide',
    name: 'Lighthouse Trail Guide',
    icon: 'mdi-lighthouse',
    members: { juniper: 'owner', odette: 'viewer' },
  },
]

// Page titles the screenshot script navigates to.
export const TITLES = {
  welcome: 'Welcome to Meridian Labs',
  architecture: 'Architecture overview',
  incident: 'Incident response',
  expense: 'Expense policy',
}
