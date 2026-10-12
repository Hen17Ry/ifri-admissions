import { defineStore } from 'pinia'
import { ref } from 'vue'
import { ApiError } from '@/lib/api'
import { draftApi, type Campaign, type Draft, type Qualification } from './api'
export const useApplicationStore = defineStore('application', () => {
  const draft = ref<Draft | null>(null)
  const campaign = ref<Campaign | null>(null)
  const qualifications = ref<Qualification[]>([])
  const loading = ref(false)
  const error = ref<ApiError | null>(null)
  const sessionEnded = ref(false)
  async function restore() {
    loading.value = true
    error.value = null
    sessionEnded.value = false
    draft.value = null
    campaign.value = null
    try {
      try {
        draft.value = await draftApi.current()
      } catch (e) {
        if (
          !(e instanceof ApiError) ||
          !['DRAFT_SESSION_REQUIRED', 'DRAFT_SESSION_EXPIRED', 'DRAFT_SESSION_REVOKED'].includes(
            e.code,
          )
        )
          throw e
        sessionEnded.value = e.code !== 'DRAFT_SESSION_REQUIRED'
      }
      campaign.value = await draftApi.campaign(draft.value?.campaign.code)
      qualifications.value = await draftApi.qualifications()
    } catch (e) {
      error.value = asApiError(e)
    } finally {
      loading.value = false
    }
  }
  async function start() {
    if (!campaign.value || loading.value) return
    loading.value = true
    error.value = null
    try {
      draft.value = await draftApi.create(campaign.value.code)
      sessionEnded.value = false
    } catch (e) {
      error.value = asApiError(e)
    } finally {
      loading.value = false
    }
  }
  return { draft, campaign, qualifications, loading, error, sessionEnded, restore, start }
})
export function asApiError(error: unknown): ApiError {
  return error instanceof ApiError
    ? error
    : new ApiError('UNKNOWN_ERROR', 'Une erreur est survenue. Réessayez.')
}
