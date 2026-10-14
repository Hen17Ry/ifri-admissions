<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { analysisApi, type TranscriptAnalysis } from './api'
import { ApiError } from '@/lib/api'
import { asApiError } from '@/features/application/store'
const props = defineProps<{ version: number; readonly?: boolean; disabled?: boolean }>()
const emit = defineEmits<{ changed: [TranscriptAnalysis]; replace: []; failure: [unknown] }>()
const analysis = ref<TranscriptAnalysis | null>(null)
const error = ref('')
const replacementRequired = ref(false)
const pending = ref(false)
const rejecting = ref(false)
const announcement = computed(() => {
  if (replacementRequired.value) return 'Un nouveau relevé est nécessaire.'
  if (error.value) return error.value
  if (!analysis.value) return 'Chargement de l’analyse du relevé.'
  if (['QUEUED', 'PROCESSING'].includes(analysis.value.status))
    return 'Analyse de votre relevé en cours.'
  if (analysis.value.candidateConfirmation === 'REJECTED')
    return 'Un nouveau relevé est nécessaire.'
  if (analysis.value.failureCode === 'NEEDS_REVIEW')
    return 'Certaines informations du relevé nécessitent une vérification.'
  if (analysis.value.status !== 'SUCCEEDED')
    return 'Nous n’avons pas pu lire correctement votre relevé.'
  return analysis.value.candidateConfirmation === 'CONFIRMED'
    ? 'Informations du relevé confirmées.'
    : 'Analyse terminée. Les informations détectées sont à vérifier.'
})
let timer: ReturnType<typeof setTimeout> | undefined
let stopped = false
let polls = 0
let requestNumber = 0
function pollWhenVisible() {
  if (stopped) return
  if (document.visibilityState === 'hidden') {
    timer = setTimeout(pollWhenVisible, 10000)
    return
  }
  void load()
}
function checkVersion(value: TranscriptAnalysis) {
  if (value.applicationVersion !== undefined && value.applicationVersion !== props.version)
    throw new ApiError(
      'DRAFT_CONFLICT',
      'Le brouillon a changé dans un autre onglet. Rechargez-le avant de continuer.',
      {},
      409,
    )
}
async function load() {
  if (stopped || props.disabled) return
  clearTimeout(timer)
  const requestedVersion = props.version
  const request = ++requestNumber
  try {
    const result = await analysisApi.current()
    if (stopped || props.disabled || request !== requestNumber) return
    if (requestedVersion !== props.version) return
    checkVersion(result)
    analysis.value = result
    error.value = ''
    replacementRequired.value = false
    emit('changed', result)
    if (['QUEUED', 'PROCESSING'].includes(result.status) && polls++ < 60)
      timer = setTimeout(pollWhenVisible, Math.min(3000 + polls * 200, 10000))
  } catch (e) {
    if (stopped || props.disabled || request !== requestNumber) return
    const failure = asApiError(e)
    replacementRequired.value = failure.code === 'TRANSCRIPT_REPLACEMENT_REQUIRED'
    error.value = replacementRequired.value ? '' : failure.message
    emit('failure', e)
  }
}
async function decide(action: 'confirm' | 'reject') {
  if (!analysis.value || pending.value || props.disabled) return
  pending.value = true
  try {
    analysis.value = await analysisApi.decide(action, analysis.value.id, props.version)
    checkVersion(analysis.value)
    emit('changed', analysis.value)
    rejecting.value = false
    if (action === 'reject') emit('replace')
  } catch (e) {
    error.value = asApiError(e).message
    emit('failure', e)
  } finally {
    pending.value = false
  }
}
watch(
  () => props.version,
  () => {
    requestNumber++
    clearTimeout(timer)
    polls = 0
    void load()
  },
)
watch(
  () => props.disabled,
  (disabled) => {
    requestNumber++
    clearTimeout(timer)
    if (!disabled) void load()
  },
)
onMounted(() => void load())
onBeforeUnmount(() => {
  stopped = true
  requestNumber++
  clearTimeout(timer)
})
</script>
<template>
  <section aria-label="Analyse du relevé">
    <p class="sr-only" role="status" aria-live="polite" aria-atomic="true">{{ announcement }}</p>
    <UiAlert v-if="replacementRequired" title="Un nouveau relevé est nécessaire.">
      Choisissez une nouvelle version de votre relevé pour permettre une nouvelle analyse.
      <UiButton class="mt-4" variant="secondary" :disabled="disabled" @click="emit('replace')"
        >Remplacer le relevé</UiButton
      >
    </UiAlert>
    <UiAlert v-else-if="error" :title="error" error>
      <UiButton variant="secondary" :disabled="disabled" @click="load"
        >Actualiser le statut</UiButton
      >
    </UiAlert>
    <p v-else-if="!analysis">Chargement de l’analyse…</p>
    <div v-else-if="['QUEUED', 'PROCESSING'].includes(analysis.status)">
      <h3>Analyse de votre relevé en cours…</h3>
      <p>
        Nous recherchons automatiquement les matières nécessaires à l’étude de votre candidature.
      </p>
      <UiButton v-if="polls >= 60" variant="secondary" @click="load">Actualiser le statut</UiButton>
    </div>
    <template
      v-else-if="analysis.status === 'SUCCEEDED' && analysis.candidateConfirmation !== 'REJECTED'"
    >
      <h3>Informations détectées</h3>
      <div
        class="analysis-table-wrap"
        role="region"
        aria-label="Matières, notes et coefficients détectés"
        tabindex="0"
      >
        <table class="analysis-table">
          <caption class="sr-only">
            Matières, notes et coefficients extraits du PDF, en lecture seule
          </caption>
          <thead>
            <tr>
              <th scope="col">Matière</th>
              <th scope="col">Note</th>
              <th scope="col">Coefficient</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="subject in analysis.subjects" :key="subject.subjectId">
              <th scope="row">{{ subject.name ?? subject.detectedSubjectText }}</th>
              <td>{{ subject.gradeValue }}</td>
              <td>{{ subject.coefficientValue }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <p v-if="analysis.candidateConfirmation === 'CONFIRMED'">Informations du relevé confirmées</p>
      <template v-else-if="!readonly">
        <p class="mt-6">Vérifiez que ces informations correspondent bien à votre relevé.</p>
        <div class="flow-actions">
          <UiButton :disabled="pending || disabled" @click="decide('confirm')"
            >Oui, les informations sont correctes</UiButton
          ><UiButton variant="secondary" :disabled="pending || disabled" @click="rejecting = true"
            >Non, les informations sont incorrectes</UiButton
          >
        </div>
        <UiAlert
          v-if="rejecting"
          title="Vous devrez fournir un nouveau relevé afin que nous puissions effectuer une nouvelle analyse."
        >
          <UiButton :disabled="pending || disabled" @click="decide('reject')"
            >Rejeter et remplacer mon relevé</UiButton
          >
          <UiButton variant="secondary" :disabled="pending || disabled" @click="rejecting = false"
            >Annuler</UiButton
          >
        </UiAlert>
      </template>
    </template>
    <UiAlert
      v-else
      :title="
        analysis?.candidateConfirmation === 'REJECTED'
          ? 'Un nouveau relevé est nécessaire.'
          : 'Nous n’avons pas pu lire correctement votre relevé.'
      "
    >
      <p v-if="analysis?.failureCode === 'NEEDS_REVIEW'">
        Certaines lignes du relevé ne passent pas les contrôles de cohérence. Leurs valeurs ne
        peuvent pas être confirmées.
      </p>
      <p>Essayez de fournir une version plus nette et complète du document.</p>
      <UiButton class="mt-4" variant="secondary" @click="emit('replace')"
        >Remplacer le relevé</UiButton
      >
    </UiAlert>
  </section>
</template>
