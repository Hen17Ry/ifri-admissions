<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { CircleCheck, Copy, Printer } from 'lucide-vue-next'
import CandidateLayout from '@/layouts/CandidateLayout.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiSkeleton from '@/components/ui/UiSkeleton.vue'
import { asApiError } from '@/features/application/store'
import {
  formatSubmissionDate,
  submissionApi,
  type SubmissionReceipt,
} from '@/features/submission/api'
import { useSubmissionStore } from '@/features/submission/store'
import type { ApiError } from '@/lib/api'

const router = useRouter()
const submission = useSubmissionStore()
const receipt = ref<SubmissionReceipt | null>(submission.receipt)
const loading = ref(false)
const error = ref<ApiError | null>(null)
const copyStatus = ref('')
const sessionEnded = computed(() => error.value?.status === 401)
const submittedAt = computed(() =>
  receipt.value ? formatSubmissionDate(receipt.value.submittedAt) : '',
)
const availableUntil = computed(() =>
  receipt.value
    ? new Intl.DateTimeFormat('fr-FR', { dateStyle: 'long', timeZone: 'Africa/Porto-Novo' }).format(
        new Date(receipt.value.sessionExpiresAt),
      )
    : '',
)

async function load() {
  loading.value = true
  error.value = null
  try {
    receipt.value = await submissionApi.receipt()
    submission.receipt = receipt.value
  } catch (e) {
    const failure = asApiError(e)
    if (failure.code === 'SUBMISSION_NOT_FOUND') {
      // Not submitted yet: the draft continues where it was.
      await router.replace({ name: 'candidate-application' })
      return
    }
    error.value = failure
  } finally {
    loading.value = false
  }
}
async function copyNumber() {
  if (!receipt.value) return
  try {
    if (!navigator.clipboard) throw new Error('clipboard unavailable')
    await navigator.clipboard.writeText(receipt.value.applicationNumber)
    copyStatus.value = 'Numéro copié dans le presse-papiers.'
  } catch {
    copyStatus.value = 'La copie automatique a échoué : sélectionnez le numéro pour le copier.'
  }
}
function print() {
  window.print()
}
onMounted(async () => {
  document.title = 'Candidature soumise — Admissions IFRI'
  if (!receipt.value) await load()
  await nextTick()
  document.getElementById('receipt-title')?.focus()
})
</script>

<template>
  <CandidateLayout>
    <div v-if="loading" role="status" aria-label="Chargement du récépissé">
      <UiSkeleton /><span class="sr-only">Chargement de votre récépissé…</span>
    </div>
    <UiAlert
      v-else-if="sessionEnded"
      title="Votre récépissé n’est plus accessible sur ce navigateur."
    >
      Votre candidature soumise reste enregistrée par l’IFRI. Conservez le numéro de candidature qui
      vous a été attribué : il vous sera demandé pour toute question.
    </UiAlert>
    <UiAlert v-else-if="error" :title="error.message" error
      ><UiButton variant="secondary" class="mt-4" @click="load">Réessayer</UiButton></UiAlert
    >
    <article v-else-if="receipt" class="receipt" aria-labelledby="receipt-title">
      <p class="receipt-status"><CircleCheck :size="24" aria-hidden="true" />Candidature soumise</p>
      <h1 id="receipt-title" tabindex="-1">Votre candidature a bien été enregistrée</h1>
      <p class="muted">{{ receipt.campaign.name }}</p>
      <section class="receipt-number-block" aria-labelledby="number-label">
        <h2 id="number-label">Votre numéro de candidature</h2>
        <p id="application-number" class="receipt-number">{{ receipt.applicationNumber }}</p>
        <div class="receipt-actions no-print">
          <UiButton @click="copyNumber"
            ><Copy :size="18" aria-hidden="true" />Copier le numéro</UiButton
          >
          <UiButton variant="secondary" @click="print"
            ><Printer :size="18" aria-hidden="true" />Imprimer le récépissé</UiButton
          >
        </div>
        <p class="copy-status no-print" role="status" aria-live="polite" aria-atomic="true">
          {{ copyStatus }}
        </p>
      </section>
      <dl class="receipt-details">
        <div>
          <dt>Nom et prénoms</dt>
          <dd>{{ receipt.candidate.lastName }} {{ receipt.candidate.firstNames }}</dd>
        </div>
        <div>
          <dt>Filière demandée</dt>
          <dd>{{ receipt.program.name }}</dd>
        </div>
        <div>
          <dt>Diplôme d’origine</dt>
          <dd>{{ receipt.qualification.name }} ({{ receipt.graduationYear }})</dd>
        </div>
        <div>
          <dt>Soumise le</dt>
          <dd>{{ submittedAt }} (heure du Bénin)</dd>
        </div>
      </dl>
      <section class="receipt-advice" aria-labelledby="advice-title">
        <h2 id="advice-title">Conservez ce numéro</h2>
        <p class="mt-2">
          Notez-le, copiez-le ou imprimez cette page : ce récépissé vous sera utile pour toute
          question sur votre candidature. Il reste consultable sur ce navigateur jusqu’au
          {{ availableUntil }}.
        </p>
        <p class="mt-2">
          Votre candidature ne peut plus être modifiée. L’IFRI va maintenant vérifier votre dossier
          ; cette soumission ne vaut pas admission.
        </p>
      </section>
    </article>
  </CandidateLayout>
</template>
