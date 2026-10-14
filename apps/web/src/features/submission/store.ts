import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { SubmissionReceipt } from './api'
// Receipt handed from the submission to the confirmation page; a refresh reloads it from the API.
export const useSubmissionStore = defineStore('submission', () => {
  const receipt = ref<SubmissionReceipt | null>(null)
  return { receipt }
})
