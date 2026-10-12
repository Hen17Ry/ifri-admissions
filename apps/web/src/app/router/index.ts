import { createRouter, createWebHistory } from 'vue-router'
import TechnicalPage from '@/pages/TechnicalPage.vue'

export const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [{ path: '/', name: 'technical', component: TechnicalPage }],
})
