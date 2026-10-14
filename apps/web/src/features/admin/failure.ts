import { useRouter } from 'vue-router'
import { asApiError } from '@/features/application/store'
import { adminMessage } from './api'
import { useAdminStore } from './store'

// An expired session sends back to the login page; any other failure becomes a message.
export function useAdminFailure() {
  const store = useAdminStore()
  const router = useRouter()
  return async function failure(e: unknown): Promise<string | null> {
    const error = asApiError(e)
    if (error.status === 401) {
      store.clear(adminMessage(error))
      await router.replace({ name: 'admin-login' })
      return null
    }
    return adminMessage(error)
  }
}
