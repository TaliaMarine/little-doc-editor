/**
 * router/index.ts
 *
 * Manual routes for ./src/pages/*.vue
 */

// Composables
import { createRouter, createWebHistory } from 'vue-router'
import AuthCallback from '@/pages/auth-callback.vue'
import Index from '@/pages/index.vue'
import Login from '@/pages/login.vue'
import PageView from '@/pages/spaces/page-view.vue'
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
      path: '/auth/callback',
      component: AuthCallback,
      meta: { public: true },
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
  if (to.path === '/login' && authStore.isAuthenticated) {
    return { path: '/' }
  }
})

export default router
