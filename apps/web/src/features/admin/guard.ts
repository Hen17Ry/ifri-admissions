import type { RouteLocationNormalized, RouteLocationRaw } from 'vue-router'
import { useAdminStore } from './store'

// meta.admin: 'public' (login, activation), 'mfa' (second factor), 'self' (any active
// account) or a permission. The API decides; the guard only routes to the right screen.
export async function adminGuard(to: RouteLocationNormalized): Promise<true | RouteLocationRaw> {
  const access = to.meta.admin as string | undefined
  if (!access) return true
  const store = useAdminStore()
  let session
  try {
    session = await store.load()
  } catch {
    if (access === 'public') return true
    store.clear('Le service d’administration est momentanément indisponible. Réessayez.')
    return { name: 'admin-login' }
  }
  if (access === 'public') {
    return to.name === 'admin-login' && session?.stage === 'ACTIVE'
      ? { name: 'admin-dashboard' }
      : true
  }
  if (!session) return { name: 'admin-login', query: { redirect: to.fullPath } }
  if (session.stage !== 'ACTIVE') return access === 'mfa' ? true : { name: 'admin-mfa' }
  if (access === 'mfa') return { name: 'admin-dashboard' }
  if (access !== 'self' && !store.can(access)) {
    return { name: 'admin-dashboard', query: { refus: '1' } }
  }
  return true
}
