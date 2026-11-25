/**
 * router/index.ts
 *
 * Manual routes for ./src/pages/*.vue
 */

// Composables
import { createRouter, createWebHistory } from 'vue-router'
import AdminInvites from '@/pages/admin/invites.vue'
import AdminSettings from '@/pages/admin/settings.vue'
import AdminUsers from '@/pages/admin/users.vue'
import AuthCallback from '@/pages/auth-callback.vue'
import Index from '@/pages/index.vue'
import InviteRedeem from '@/pages/invite.vue'
import Login from '@/pages/login.vue'
import ShareLinkPage from '@/pages/share-link.vue'
import ShareTarget from '@/pages/share-target.vue'
import SpaceMembers from '@/pages/spaces/members.vue'
import PageView from '@/pages/spaces/page-view.vue'
import SpaceSettings from '@/pages/spaces/settings.vue'
import SpaceHome from '@/pages/spaces/space-home.vue'
import { useAuthStore } from '@/stores/auth'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      component: Index,
    },
    {
      path: '/login',
      component: Login,
      meta: { public: true },
    },
    {
      // Invite redemption (Decision 41): public like /login, the token is the credential.
      path: '/invite/:token',
      component: InviteRedeem,
      props: true,
      meta: { public: true, guest: true },
    },
    { path: '/admin/invites', component: AdminInvites, meta: { admin: true } },
    { path: '/admin/users', component: AdminUsers, meta: { admin: true } },
    { path: '/admin/settings', component: AdminSettings, meta: { admin: true } },
    {
      path: '/spaces/:spaceId/members',
      component: SpaceMembers,
      props: true,
    },
    {
      path: '/spaces/:spaceId/settings',
      component: SpaceSettings,
      props: true,
    },
    {
      path: '/auth/callback',
      component: AuthCallback,
      meta: { public: true },
    },
    {
      // Android share-target landing (SPEC.md §5.1). Not public: no session
      // sends the user through /login, and the guard keeps ?key= in redirect.
      path: '/share-target',
      component: ShareTarget,
    },
    {
      // Share-link guest entry (Decisions 6, 21): public, no member chrome.
      // `:pageId` is a descendant of the shared page.
      path: '/share/:token/:pageId?',
      component: ShareLinkPage,
      props: true,
      meta: { public: true, guest: true },
    },
    {
      path: '/spaces/:spaceId',
      component: SpaceHome,
      props: true,
    },
    {
      path: '/spaces/:spaceId/pages/:pageId',
      component: PageView,
      props: true,
    },
  ],
})

// Every route requires an active session except /login (Decision 11: no
// open self-service registration, so there's no /signup to also exempt).
router.beforeEach(to => {
  const authStore = useAuthStore()
  if (!to.meta.public && !authStore.isAuthenticated) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  // Org-admin screens: hidden from everyone else (the api enforces it too).
  if (to.meta.admin && !authStore.isOrgAdmin) {
    return { path: '/' }
  }
  if (to.path === '/login' && authStore.isAuthenticated) {
    return { path: '/' }
  }
})

export default router
