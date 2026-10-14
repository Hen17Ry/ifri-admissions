<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { RefreshCw } from 'lucide-vue-next'
import UiButton from '@/components/ui/UiButton.vue'
import { useAdminFailure } from '@/features/admin/failure'
import { proofFailures, proofImageUrl, reviewApi, type ProofState } from '@/features/review/api'

// OCR proof of one subject: generated on demand by the worker from the exact submitted
// document version and the stored coordinates, then served by the authenticated API only.
const props = defineProps<{ applicationId: string; subjectId: string; subjectName: string }>()
const failure = useAdminFailure()
const state = ref<'loading' | ProofState | 'error'>('loading')
const message = ref('')
const source = ref('')
let stopped = false
const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

async function start() {
  state.value = 'loading'
  message.value = ''
  try {
    let proof = await reviewApi.proof(props.applicationId, props.subjectId)
    if (proof.status === 'NONE' || proof.status === 'FAILED') {
      proof = await reviewApi.requestProof(props.applicationId, props.subjectId)
    }
    // About one minute of patience; the worker treats proofs apart from candidate analyses.
    for (let attempt = 0; proof.status === 'PENDING' && attempt < 40 && !stopped; attempt++) {
      state.value = 'PENDING'
      await wait(1500)
      if (stopped) return
      proof = await reviewApi.proof(props.applicationId, props.subjectId)
    }
    if (proof.status === 'READY') {
      source.value = proofImageUrl(props.applicationId, props.subjectId)
      state.value = 'READY'
    } else if (proof.status === 'FAILED') {
      state.value = 'FAILED'
      message.value = proof.failureCode ? (proofFailures[proof.failureCode] ?? '') : ''
    } else {
      state.value = 'error'
      message.value = 'La preuve n’est pas encore prête. Réessayez dans un instant.'
    }
  } catch (e) {
    const text = await failure(e)
    if (text === null) return
    state.value = 'error'
    message.value = text
  }
}
onMounted(start)
onBeforeUnmount(() => {
  stopped = true
})
</script>
<template>
  <div class="review-proof" :aria-busy="state === 'loading' || state === 'PENDING'">
    <p
      v-if="state === 'loading' || state === 'PENDING'"
      class="muted"
      role="status"
      aria-live="polite"
    >
      Génération de la preuve pour {{ subjectName }}…
    </p>
    <figure v-else-if="state === 'READY'">
      <img
        :src="source"
        :alt="`Extrait du relevé de notes : ligne ${subjectName}, note, coefficient et points surlignés`"
        class="review-proof-image"
        @error="((state = 'error'), (message = 'L’image de la preuve n’a pas pu être affichée.'))"
      />
      <figcaption class="muted text-sm">
        Ligne du relevé lue par l’analyse ({{ subjectName }}) : cellules utilisées surlignées. Image
        générée depuis la version exacte du document soumis.
      </figcaption>
    </figure>
    <div v-else role="alert">
      <p>
        Preuve indisponible{{ message ? ` : ${message}` : '.' }}
        <template v-if="state === 'FAILED'">
          Consultez le relevé complet dans « Pièces du dossier ».</template
        >
      </p>
      <UiButton variant="secondary" class="mt-2" @click="start"
        ><RefreshCw :size="18" aria-hidden="true" />Réessayer</UiButton
      >
    </div>
  </div>
</template>
