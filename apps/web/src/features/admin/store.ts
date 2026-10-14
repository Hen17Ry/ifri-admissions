import { defineStore } from 'pinia'
import { ref } from 'vue'
import { asApiError } from '@/features/application/store'
import { adminApi, setCsrfToken, type AdminSession, type StageResponse } from './api'

export const useAdminStore = defineStore('admin', () => {
  const session = ref<AdminSession | null>(null)
  const loaded = ref(false)
  // One message carried to the login page (expired session, password changed…).
  const notice = ref('')

  async function load(force = false): Promise<AdminSession | null> {
    if (loaded.value && !force) return session.value
    try {
      session.value = await adminApi.session()
      setCsrfToken(session.value.csrfToken)
    } catch (e) {
      if (asApiError(e).status !== 401) throw e
      session.value = null
      setCsrfToken('')
    } finally {
      loaded.value = true
    }
    return session.value
  }
  async function applyStage(response: StageResponse) {
    setCsrfToken(response.csrfToken)
    await load(true)
  }
  function clear(message = '') {
    session.value = null
    loaded.value = true
    setCsrfToken('')
    notice.value = message
  }
  async function logout() {
    try {
      await adminApi.logout()
    } catch {
      /* the server session ends anyway at expiry; the browser forgets it now */
    }
    clear('Vous êtes déconnecté.')
  }
  // For display only: every decision is taken again by the API.
  function can(permission: string) {
    return session.value?.stage === 'ACTIVE' && session.value.permissions.includes(permission)
  }
  return { session, loaded, notice, load, applyStage, clear, logout, can }
})
