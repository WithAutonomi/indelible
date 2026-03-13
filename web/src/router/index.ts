import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('../views/auth/LoginView.vue'),
      meta: { guest: true },
    },
    {
      path: '/register',
      name: 'register',
      component: () => import('../views/auth/RegisterView.vue'),
      meta: { guest: true },
    },
    {
      path: '/',
      name: 'dashboard',
      component: () => import('../views/user/DashboardView.vue'),
      meta: { auth: true },
    },
    {
      path: '/uploads',
      name: 'uploads',
      component: () => import('../views/user/UploadsView.vue'),
      meta: { auth: true },
    },
    {
      path: '/collections',
      name: 'collections',
      component: () => import('../views/user/CollectionsView.vue'),
      meta: { auth: true },
    },
    {
      path: '/tokens',
      name: 'tokens',
      component: () => import('../views/user/TokensView.vue'),
      meta: { auth: true },
    },
    {
      path: '/admin/users',
      name: 'admin-users',
      component: () => import('../views/admin/UsersView.vue'),
      meta: { auth: true, admin: true },
    },
    {
      path: '/admin/settings',
      name: 'admin-settings',
      component: () => import('../views/admin/SettingsView.vue'),
      meta: { auth: true, admin: true },
    },
    {
      path: '/admin/analytics',
      name: 'admin-analytics',
      component: () => import('../views/admin/AnalyticsView.vue'),
      meta: { auth: true, admin: true },
    },
  ],
})

router.beforeEach((to, _from, next) => {
  const auth = useAuthStore()

  if (to.meta.auth && !auth.isAuthenticated) {
    next({ name: 'login' })
  } else if (to.meta.guest && auth.isAuthenticated) {
    next({ name: 'dashboard' })
  } else if (to.meta.admin && !auth.isAdmin) {
    next({ name: 'dashboard' })
  } else {
    next()
  }
})

export default router
