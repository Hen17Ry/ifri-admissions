import { createRouter, createWebHistory } from 'vue-router'
import TechnicalPage from '@/pages/TechnicalPage.vue'
import { adminGuard } from '@/features/admin/guard'

export const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    { path: '/', name: 'technical', component: TechnicalPage },
    {
      path: '/candidature',
      name: 'candidate-application',
      component: () => import('@/pages/CandidateApplicationPage.vue'),
    },
    {
      path: '/candidature/confirmation',
      name: 'candidate-confirmation',
      component: () => import('@/pages/SubmissionConfirmationPage.vue'),
    },
    {
      path: '/admin',
      name: 'admin-dashboard',
      component: () => import('@/pages/admin/AdminDashboardPage.vue'),
      meta: { admin: 'dashboard.view' },
    },
    {
      path: '/admin/candidatures',
      name: 'admin-applications',
      component: () => import('@/pages/admin/AdminApplicationsPage.vue'),
      meta: { admin: 'applications.read' },
    },
    {
      path: '/admin/candidatures/:id',
      name: 'admin-application',
      component: () => import('@/pages/admin/AdminApplicationPage.vue'),
      meta: { admin: 'applications.read' },
    },
    {
      path: '/admin/corrections',
      name: 'admin-corrections',
      component: () => import('@/pages/admin/AdminCorrectionsPage.vue'),
      meta: { admin: 'corrections.decide' },
    },
    {
      path: '/admin/connexion',
      name: 'admin-login',
      component: () => import('@/pages/admin/AdminLoginPage.vue'),
      meta: { admin: 'public' },
    },
    {
      path: '/admin/activation',
      name: 'admin-activation',
      component: () => import('@/pages/admin/AdminActivationPage.vue'),
      meta: { admin: 'public' },
    },
    {
      path: '/admin/verification',
      name: 'admin-mfa',
      component: () => import('@/pages/admin/AdminMfaPage.vue'),
      meta: { admin: 'mfa' },
    },
    {
      path: '/admin/compte',
      name: 'admin-account',
      component: () => import('@/pages/admin/AdminAccountPage.vue'),
      meta: { admin: 'self' },
    },
    {
      path: '/admin/comptes',
      name: 'admin-accounts',
      component: () => import('@/pages/admin/AdminAccountsPage.vue'),
      meta: { admin: 'accounts.read' },
    },
    {
      path: '/admin/journal',
      name: 'admin-audit',
      component: () => import('@/pages/admin/AdminAuditPage.vue'),
      meta: { admin: 'audit.read' },
    },
  ],
})
router.beforeEach(adminGuard)
