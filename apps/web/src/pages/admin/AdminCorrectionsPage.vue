<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { Check, FileSearch, FolderOpen, Inbox, X } from 'lucide-vue-next'
import AdminLayout from '@/layouts/AdminLayout.vue'
import FormField from '@/components/ui/FormField.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiDialog from '@/components/ui/UiDialog.vue'
import UiSkeleton from '@/components/ui/UiSkeleton.vue'
import { useAdminFailure } from '@/features/admin/failure'
import { useAdminStore } from '@/features/admin/store'
import { asApiError } from '@/features/application/store'
import ProofViewer from '@/features/review/components/ProofViewer.vue'
import { fieldLabels, frenchNumber, reviewApi, type CorrectionView } from '@/features/review/api'

// Queue of the deputy director: the only role that approves or refuses a correction, never
// one of their own (four eyes, checked again by the API and the database).
const store = useAdminStore()
const failure = useAdminFailure()
const corrections = ref<CorrectionView[]>([])
const loading = ref(true)
const loadError = ref('')
const status = ref('')
const proof = ref('')
const deciding = ref<{ correction: CorrectionView; approve: boolean } | null>(null)
const reason = ref('')
const reasonError = ref('')
const dialogMessage = ref('')
const pending = ref(false)
const date = new Intl.DateTimeFormat('fr-FR', {
  dateStyle: 'medium',
  timeStyle: 'short',
  timeZone: 'Africa/Porto-Novo',
})
const relative = new Intl.RelativeTimeFormat('fr', { numeric: 'auto' })
function age(value: string) {
  const minutes = Math.round((Date.now() - new Date(value).getTime()) / 60000)
  if (minutes < 1) return 'à l’instant'
  if (minutes < 60) return relative.format(-minutes, 'minute')
  if (minutes < 48 * 60) return relative.format(-Math.round(minutes / 60), 'hour')
  return relative.format(-Math.round(minutes / 1440), 'day')
}
async function load() {
  loading.value = true
  loadError.value = ''
  try {
    corrections.value = (await reviewApi.corrections()).corrections
  } catch (e) {
    loadError.value = (await failure(e)) ?? ''
  } finally {
    loading.value = false
  }
}
function open(correction: CorrectionView, approve: boolean) {
  reason.value = ''
  reasonError.value = ''
  dialogMessage.value = ''
  deciding.value = { correction, approve }
}
async function decide() {
  if (!deciding.value) return
  const { correction, approve } = deciding.value
  if (!approve && !reason.value.trim()) {
    reasonError.value = 'Indiquez le motif du refus.'
    document.getElementById('refusal-reason')?.focus()
    return
  }
  pending.value = true
  dialogMessage.value = ''
  try {
    const detail = await reviewApi.decide(correction.id, approve, {
      version: correction.reviewVersion ?? 0,
      reason: reason.value.trim() || undefined,
    })
    corrections.value = corrections.value.filter((item) => item.id !== correction.id)
    deciding.value = null
    status.value = approve
      ? `Correction approuvée. Moyenne retenue du dossier ${detail.applicationNumber} : ${frenchNumber(detail.effectiveAverage?.display ?? '—')}.`
      : `Correction refusée pour le dossier ${detail.applicationNumber}.`
  } catch (e) {
    const error = asApiError(e)
    const text = await failure(e)
    if (text === null) return
    if (error.fields.reason) {
      reasonError.value = error.fields.reason
    } else if (
      error.code === 'REVIEW_VERSION_CONFLICT' ||
      error.code === 'CORRECTION_ALREADY_DECIDED'
    ) {
      deciding.value = null
      status.value = `${text} La liste a été rechargée.`
      await load()
    } else {
      dialogMessage.value = text
    }
  } finally {
    pending.value = false
  }
}
onMounted(async () => {
  document.title = 'Corrections à décider — Administration IFRI'
  document.getElementById('page-title')?.focus()
  await load()
})
</script>
<template>
  <AdminLayout>
    <h1 id="page-title" tabindex="-1">Corrections à décider</h1>
    <p class="muted mt-2 review-intro">
      Chaque correction a été proposée par l’agent en charge du dossier. Le relevé soumis n’est
      jamais modifié : une correction approuvée change la valeur retenue et la moyenne est
      recalculée exactement par le serveur.
    </p>
    <p class="sr-only" role="status" aria-live="polite" aria-atomic="true">{{ status }}</p>
    <UiAlert v-if="status" :title="status" class="mt-6" />
    <div v-if="loading" role="status" class="mt-6">
      <UiSkeleton /><span class="sr-only">Chargement des corrections…</span>
    </div>
    <UiAlert v-else-if="loadError" :title="loadError" error class="mt-6"
      ><UiButton variant="secondary" class="mt-4" @click="load">Réessayer</UiButton></UiAlert
    >
    <section v-else-if="!corrections.length" class="admin-empty">
      <Inbox :size="32" aria-hidden="true" />
      <h2>Aucune correction en attente</h2>
      <p class="muted">Les nouvelles propositions des agents apparaîtront ici.</p>
    </section>
    <ul v-else class="review-queue">
      <li v-for="correction in corrections" :key="correction.id">
        <article class="review-card" :aria-labelledby="`correction-${correction.id}`">
          <h2 :id="`correction-${correction.id}`">
            {{ fieldLabels[correction.field] }} — {{ correction.subjectName }} ·
            {{ correction.applicationNumber }}
          </h2>
          <p class="muted">{{ correction.candidate }}</p>
          <dl class="review-values">
            <div>
              <dt>Valeur du relevé</dt>
              <dd>{{ frenchNumber(correction.originalValue) }}</dd>
            </div>
            <div>
              <dt>Valeur retenue actuelle</dt>
              <dd>{{ frenchNumber(correction.currentValue) }}</dd>
            </div>
            <div>
              <dt>Valeur proposée</dt>
              <dd class="review-proposed">{{ frenchNumber(correction.proposedValue) }}</dd>
            </div>
          </dl>
          <p class="mt-2">
            Proposée par <strong>{{ correction.proposedBy.name }}</strong>
            <span class="muted">
              — {{ age(correction.proposedAt) }} ({{
                date.format(new Date(correction.proposedAt))
              }})</span
            >
          </p>
          <p class="review-quote">{{ correction.reason }}</p>
          <div class="admin-row-actions mt-4">
            <UiButton
              variant="secondary"
              :aria-expanded="proof === correction.id"
              :aria-controls="`proof-${correction.id}`"
              @click="proof = proof === correction.id ? '' : correction.id"
              ><FileSearch :size="18" aria-hidden="true" />Preuve</UiButton
            >
            <RouterLink
              class="secondary-button"
              :to="{ name: 'admin-application', params: { id: correction.applicationId } }"
              ><FolderOpen :size="18" aria-hidden="true" />Dossier</RouterLink
            >
            <UiButton
              v-if="correction.proposedBy.id !== store.session?.id"
              @click="open(correction, true)"
              ><Check :size="18" aria-hidden="true" />Approuver</UiButton
            >
            <UiButton
              v-if="correction.proposedBy.id !== store.session?.id"
              variant="danger"
              @click="open(correction, false)"
              ><X :size="18" aria-hidden="true" />Refuser</UiButton
            >
          </div>
          <div :id="`proof-${correction.id}`" aria-live="polite">
            <ProofViewer
              v-if="proof === correction.id"
              :application-id="correction.applicationId"
              :subject-id="correction.subjectId"
              :subject-name="correction.subjectName"
              class="mt-4"
            />
          </div>
        </article>
      </li>
    </ul>

    <UiDialog
      :open="!!deciding"
      :title="deciding?.approve ? 'Approuver cette correction ?' : 'Refuser cette correction'"
      :alert="!deciding?.approve"
      :pending="pending"
      @close="deciding = null"
    >
      <p v-if="deciding" class="mt-3">
        {{ fieldLabels[deciding.correction.field] }} de {{ deciding.correction.subjectName }} ({{
          deciding.correction.applicationNumber
        }}) : {{ frenchNumber(deciding.correction.currentValue) }} →
        <strong>{{ frenchNumber(deciding.correction.proposedValue) }}</strong>
      </p>
      <p v-if="deciding?.approve" class="mt-2">
        La valeur proposée deviendra la valeur retenue et la moyenne du dossier sera recalculée.
      </p>
      <UiAlert v-if="dialogMessage" :title="dialogMessage" error class="mt-4" />
      <form
        v-if="deciding && !deciding.approve"
        id="refusal-form"
        class="admin-form"
        novalidate
        @submit.prevent="decide"
      >
        <FormField
          id="refusal-reason"
          label="Motif du refus"
          hint="Communiqué à l’agent dans l’historique du dossier."
          :error="reasonError"
        >
          <template #default="slot">
            <textarea
              id="refusal-reason"
              v-model="reason"
              class="flow-input review-textarea"
              maxlength="2000"
              :aria-describedby="slot.describedby"
              :aria-invalid="slot.invalid"
            />
          </template>
        </FormField>
      </form>
      <template #actions>
        <UiButton
          variant="secondary"
          :disabled="pending"
          :data-initial-focus="deciding?.approve ? '' : undefined"
          @click="deciding = null"
          >Annuler</UiButton
        >
        <UiButton
          :type="deciding?.approve ? 'button' : 'submit'"
          :form="deciding?.approve ? undefined : 'refusal-form'"
          :variant="deciding?.approve ? 'primary' : 'danger'"
          :disabled="pending"
          :aria-busy="pending"
          @click="deciding?.approve && decide()"
          >{{ deciding?.approve ? 'Approuver' : 'Refuser la correction' }}</UiButton
        >
      </template>
    </UiDialog>
  </AdminLayout>
</template>
