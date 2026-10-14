<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowLeft, ArrowRight, Check, LockKeyhole, Send } from 'lucide-vue-next'
import AnalysisStep from '@/features/analysis/AnalysisStep.vue'
import { analysisApi, type TranscriptAnalysis } from '@/features/analysis/api'
import CandidateLayout from '@/layouts/CandidateLayout.vue'
import FormField from '@/components/ui/FormField.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiInput from '@/components/ui/UiInput.vue'
import UiSelect from '@/components/ui/UiSelect.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import UiSkeleton from '@/components/ui/UiSkeleton.vue'
import ApplicationStepper from '@/features/application/ApplicationStepper.vue'
import ProgramCard from '@/features/application/ProgramCard.vue'
import DocumentsStep from '@/features/documents/DocumentsStep.vue'
import { documentsApi, type DocumentListing } from '@/features/documents/api'
import { draftApi, type Draft, type Profile, type Program } from '@/features/application/api'
import { academicErrors, profileErrors } from '@/features/application/validation'
import { asApiError, useApplicationStore } from '@/features/application/store'
import SubmitDialog from '@/features/submission/SubmitDialog.vue'
import { incompleteStep, submissionApi, submissionMessage } from '@/features/submission/api'
import { useSubmissionStore } from '@/features/submission/store'
import { ApiError } from '@/lib/api'

const router = useRouter()
const store = useApplicationStore()
const submission = useSubmissionStore()
const confirmOpen = ref(false)
const submitting = ref(false)
const profile = reactive<Profile>({ lastName: '', firstNames: '', email: '', phone: '' })
const qualification = ref('')
const year = ref('')
const program = ref('')
const step = ref(0)
const errors = ref<Record<string, string>>({})
const errorMessage = ref('')
const saveStatus = ref('')
const programs = ref<Program[]>([])
const programsLoading = ref(false)
const programsError = ref('')
const analysis = ref<TranscriptAnalysis | null>(null)
const replacementRequested = ref(false)
const needsReplacement = computed(
  () =>
    replacementRequested.value ||
    analysis.value?.candidateConfirmation === 'REJECTED' ||
    ['FAILED', 'LOW_CONFIDENCE', 'OBSOLETE'].includes(analysis.value?.status ?? ''),
)
const documents = ref<DocumentListing | null>(null)
const documentsLoading = ref(false)
const documentsError = ref('')
const active = ref(0)
const sessionLocked = ref(false)
const dirty = ref(false)
const titles = [
  'Vos informations',
  'Votre parcours',
  'Filière souhaitée',
  'Documents',
  'Vérifiez et soumettez',
]
const fields: {
  key: keyof Profile
  label: string
  type: string
  autocomplete: string
  hint?: string
  max: number
}[] = [
  { key: 'lastName', label: 'Nom', type: 'text', autocomplete: 'family-name', max: 100 },
  { key: 'firstNames', label: 'Prénoms', type: 'text', autocomplete: 'given-name', max: 150 },
  { key: 'email', label: 'Email', type: 'email', autocomplete: 'email', max: 254 },
  {
    key: 'phone',
    label: 'Téléphone',
    type: 'tel',
    autocomplete: 'tel',
    max: 30,
    hint: 'Format international, par exemple +229 01 23 45 67 89.',
  },
]
const closed = computed(() =>
  ['NO_CURRENT_CAMPAIGN', 'CAMPAIGN_NOT_OPEN'].includes(store.error?.code ?? ''),
)
const reachable = computed(() => {
  const d = store.draft
  if (!d || Object.keys(profileErrors(d.profile)).length) return 0
  if (!d.qualification || !d.graduationYear) return 1
  return d.program
    ? documents.value?.complete
      ? needsReplacement.value
        ? 3
        : analysis.value?.verificationReady
          ? 4
          : 3
      : 3
    : 2
})
let ready = false
let timer: ReturnType<typeof setTimeout> | undefined
let queue: Promise<void> = Promise.resolve()
let revision = 0
let programsRequest = 0

function reviewAnalysis(value: TranscriptAnalysis) {
  analysis.value = value
  if (!value.verificationReady) void goTo(3)
}
async function hydrate(d: Draft) {
  ready = false
  Object.assign(profile, d.profile)
  qualification.value = d.qualification?.code ?? ''
  year.value = d.graduationYear?.toString() ?? ''
  program.value = d.program?.code ?? ''
  await nextTick()
  ready = true
  dirty.value = false
  saveStatus.value = 'Enregistré'
  step.value = Math.min(reachable.value, 4)
}
async function load() {
  replacementRequested.value = false
  clearTimeout(timer)
  await queue.catch(() => {})
  ready = false
  sessionLocked.value = false
  errorMessage.value = ''
  errors.value = {}
  await store.restore()
  if (await leaveIfSubmitted()) return
  if (store.draft && !store.error) {
    await loadDocuments()
    try {
      analysis.value = await analysisApi.current()
    } catch (e) {
      analysis.value = null
      if (asApiError(e).code === 'TRANSCRIPT_REPLACEMENT_REQUIRED')
        replacementRequested.value = true
    }
    await hydrate(store.draft)
  }
}
// A submitted application only has its confirmation page: never a new or editable draft.
async function leaveIfSubmitted() {
  if (store.draft?.state !== 'SUBMITTED') return false
  await router.replace({ name: 'candidate-confirmation' })
  return true
}
async function start() {
  await store.start()
  if (await leaveIfSubmitted()) return
  if (store.draft && !store.error) {
    await loadDocuments()
    try {
      analysis.value = await analysisApi.current()
    } catch (e) {
      analysis.value = null
      if (asApiError(e).code === 'TRANSCRIPT_REPLACEMENT_REQUIRED')
        replacementRequested.value = true
    }
    await hydrate(store.draft)
  }
}
function fail(e: unknown) {
  const error = asApiError(e)
  if (error.code === 'APPLICATION_LOCKED') {
    // Submitted from another tab: reload, which leads to the confirmation page.
    void load()
    return
  }
  if (error.code === 'TRANSCRIPT_REPLACEMENT_REQUIRED') replacementRequested.value = true
  errorMessage.value = error.message
  errors.value = error.fields
  saveStatus.value = 'Modifications non enregistrées'
  if (error.status === 401 || error.code === 'DRAFT_CONFLICT' || error.code === 'CAMPAIGN_NOT_OPEN')
    sessionLocked.value = true
}
function persist(kind: string): Promise<void> {
  clearTimeout(timer)
  if (!store.draft || sessionLocked.value) return Promise.reject(new Error('session unavailable'))
  const snapshot =
    kind === 'profile'
      ? { profile: { ...profile } }
      : kind === 'academic-background'
        ? { qualification: qualification.value, graduationYear: Number(year.value) }
        : { program: program.value }
  const localRevision = revision
  active.value++
  saveStatus.value = 'Enregistrement…'
  const work = queue
    .catch(() => {})
    .then(async () => {
      if (sessionLocked.value || !store.draft) throw new Error('session unavailable')
      const previous = store.draft.program?.code
      const saved = await draftApi.patch(kind, { ...snapshot, version: store.draft.version })
      store.draft = saved
      if (kind !== 'profile') {
        analysis.value = null
        replacementRequested.value = false
        try {
          analysis.value = await analysisApi.current()
        } catch (e) {
          if (asApiError(e).code === 'TRANSCRIPT_REPLACEMENT_REQUIRED')
            replacementRequested.value = true
        }
      }
      if (kind === 'academic-background' && previous && !saved.program) {
        ready = false
        program.value = ''
        await nextTick()
        ready = true
        errorMessage.value = 'Votre nouvelle qualification nécessite de choisir une autre filière.'
      }
      if (localRevision === revision) {
        dirty.value = false
        saveStatus.value = 'Enregistré'
      }
    })
    .catch((e) => {
      fail(e)
      throw e
    })
    .finally(() => {
      active.value--
    })
  queue = work
  void work.catch(() => {})
  return work
}
function changed(kind: string) {
  if (!ready || !store.draft || sessionLocked.value) return
  revision++
  dirty.value = true
  saveStatus.value = 'Modifications non enregistrées'
  clearTimeout(timer)
  const valid =
    kind === 'profile'
      ? !Object.keys(profileErrors(profile, false)).length
      : kind === 'academic-background'
        ? !Object.keys(academicErrors(qualification.value, year.value)).length
        : !!program.value
  if (valid)
    timer = setTimeout(() => {
      void persist(kind).catch(() => {})
    }, 800)
}
watch(profile, () => changed('profile'), { deep: true })
watch([qualification, year], () => changed('academic-background'))
watch(program, () => changed('program'))
async function loadPrograms() {
  const request = ++programsRequest
  programsLoading.value = true
  programsError.value = ''
  programs.value = []
  try {
    if (!store.campaign || !store.draft?.qualification) return
    const result = await draftApi.programs(store.campaign.code, store.draft.qualification.code)
    if (request === programsRequest) programs.value = result
  } catch (e) {
    if (request === programsRequest) programsError.value = asApiError(e).message
  } finally {
    if (request === programsRequest) programsLoading.value = false
  }
}
watch(step, async () => {
  document.title = `${titles[step.value]} — Admissions IFRI`
  if (step.value === 2) await loadPrograms()
  if (step.value >= 3) await loadDocuments()
  await nextTick()
  document.getElementById('step-title')?.focus()
})
async function loadDocuments() {
  if (!store.draft) return
  documentsLoading.value = true
  documentsError.value = ''
  try {
    const list = await documentsApi.list()
    if (list.applicationVersion !== store.draft.version) {
      throw new ApiError(
        'DRAFT_CONFLICT',
        'Le brouillon a changé dans un autre onglet. Rechargez-le avant de continuer.',
        {},
        409,
      )
    }
    documents.value = list
  } catch (e) {
    documentsError.value = asApiError(e).message
    if (
      asApiError(e).status === 401 ||
      ['DRAFT_CONFLICT', 'APPLICATION_LOCKED'].includes(asApiError(e).code)
    )
      fail(e)
  } finally {
    documentsLoading.value = false
  }
}
function documentSaved(list: DocumentListing) {
  const previous = documents.value?.documents.find(
    (document) => document.type === 'TRANSCRIPT',
  )?.version
  const current = list.documents.find((document) => document.type === 'TRANSCRIPT')?.version
  replacementRequested.value = needsReplacement.value && previous === current
  if (previous !== current) analysis.value = null
  errorMessage.value = ''
  documents.value = list
  if (store.draft) store.draft.version = list.applicationVersion
}
function uploadBusy(busy: boolean) {
  active.value += busy ? 1 : -1
  saveStatus.value = busy
    ? 'Envoi en cours…'
    : errorMessage.value
      ? 'Modifications non enregistrées'
      : 'Enregistré'
}
async function finishDocuments() {
  await loadDocuments()
  if (!documents.value?.complete || documentsError.value) return
  try {
    analysis.value = await analysisApi.current()
    if (analysis.value.verificationReady && !needsReplacement.value) step.value = 4
  } catch (e) {
    fail(e)
  }
}
async function advance() {
  errors.value =
    step.value === 0
      ? profileErrors(profile)
      : step.value === 1
        ? academicErrors(qualification.value, year.value)
        : !program.value
          ? { program: 'Choisissez une filière.' }
          : {}
  if (Object.keys(errors.value).length) {
    await nextTick()
    document.getElementById(Object.keys(errors.value)[0]!)?.focus()
    return
  }
  errorMessage.value = ''
  try {
    await persist(['profile', 'academic-background', 'program'][step.value]!)
    step.value++
  } catch {
    /* the save error remains visible and the step remains unchanged */
  }
}
async function goTo(target: number) {
  if (active.value || target > reachable.value) return
  clearTimeout(timer)
  if (dirty.value && step.value < 3) {
    const valid =
      step.value === 0
        ? !Object.keys(profileErrors(profile, false)).length
        : step.value === 1
          ? !Object.keys(academicErrors(qualification.value, year.value)).length
          : !!program.value
    if (valid) {
      try {
        await persist(['profile', 'academic-background', 'program'][step.value]!)
      } catch {
        return
      }
    }
  }
  errors.value = {}
  step.value = target
}
async function replaceTranscript() {
  replacementRequested.value = true
  await goTo(3)
  await nextTick()
  document.getElementById('file-TRANSCRIPT')?.focus()
}
async function submitApplication() {
  if (!store.draft || submitting.value) return
  submitting.value = true
  try {
    submission.receipt = await submissionApi.submit(store.draft.version)
    await router.replace({ name: 'candidate-confirmation' })
  } catch (e) {
    confirmOpen.value = false
    await submissionFailed(asApiError(e))
  } finally {
    submitting.value = false
  }
}
async function submissionFailed(error: ApiError) {
  if (error.code === 'APPLICATION_LOCKED') {
    await load()
    return
  }
  const message = submissionMessage(error)
  if (
    [
      'ADMISSION_RULES_CHANGED',
      'TRANSCRIPT_ANALYSIS_REQUIRED',
      'TRANSCRIPT_ANALYSIS_OUTDATED',
    ].includes(error.code)
  ) {
    // Back to Documents, where the new analysis is displayed and must be confirmed again.
    analysis.value = null
    try {
      analysis.value = await analysisApi.current()
    } catch {
      /* the transcript card shows the analysis state itself */
    }
    await goTo(3)
  } else if (error.code === 'APPLICATION_NOT_ELIGIBLE') {
    await goTo(2)
  } else if (error.code === 'APPLICATION_INCOMPLETE') {
    await goTo(incompleteStep(error.fields))
    // Form fields get their inline error; missing documents are visible in the Documents step.
    errors.value = Object.fromEntries(
      Object.entries(error.fields).filter(([key]) => !key.startsWith('document.')),
    )
  } else if (
    error.status === 401 ||
    error.code === 'VERSION_CONFLICT' ||
    error.code === 'CAMPAIGN_CLOSED'
  ) {
    sessionLocked.value = true
  }
  errorMessage.value = message
  await nextTick()
  document.getElementById('step-title')?.focus()
}
function beforeUnload(event: BeforeUnloadEvent) {
  if (dirty.value || active.value) event.preventDefault()
}
onMounted(() => {
  window.addEventListener('beforeunload', beforeUnload)
  void load()
})
onBeforeUnmount(() => {
  clearTimeout(timer)
  programsRequest++
  window.removeEventListener('beforeunload', beforeUnload)
})
</script>

<template>
  <CandidateLayout>
    <div class="flow-intro">
      <p class="eyebrow">Votre projet commence ici</p>
      <h1>Préparez votre candidature</h1>
      <p class="muted intro-copy">
        Renseignez vos informations et choisissez une filière adaptée à votre parcours. Vous pourrez
        revenir sur votre brouillon.
      </p>
      <p v-if="store.campaign" class="campaign-label">{{ store.campaign.name }}</p>
    </div>
    <div v-if="store.loading" role="status" aria-label="Chargement du parcours">
      <UiSkeleton /><span class="sr-only">Chargement de votre brouillon…</span>
    </div>
    <UiAlert v-else-if="closed" title="Les candidatures ne sont actuellement pas ouvertes."
      >Les informations de la prochaine campagne seront disponibles ici.</UiAlert
    >
    <UiAlert v-else-if="store.error" :title="store.error.message" error
      ><UiButton variant="secondary" class="mt-4" @click="load">Réessayer</UiButton></UiAlert
    >
    <section v-else-if="!store.draft" class="flow-start">
      <UiAlert
        v-if="store.sessionEnded"
        title="Votre session a expiré ou a été révoquée."
        class="mb-6"
        >Votre ancien brouillon n’est plus accessible sur ce navigateur. Aucun nouveau brouillon n’a
        été créé automatiquement.</UiAlert
      >
      <h2 class="text-xl font-semibold">Cinq étapes, à votre rythme</h2>
      <p class="muted mt-3">
        Vos informations, votre parcours, votre filière, vos documents, puis une vérification avant
        la soumission. Les étapes complétées sont enregistrées automatiquement.
      </p>
      <UiButton class="mt-6" @click="start"
        >Commencer mon brouillon <ArrowRight :size="18" aria-hidden="true"
      /></UiButton>
    </section>
    <template v-else>
      <ApplicationStepper
        :current="step"
        :reachable="reachable"
        :busy="active > 0 || sessionLocked"
        @change="goTo"
      />
      <div class="flow-grid">
        <section class="flow-content" :aria-busy="active > 0">
          <div class="step-heading">
            <p class="eyebrow">Étape {{ step + 1 }} sur 5</p>
            <p role="status" aria-atomic="true" class="save-status">
              <Check v-if="saveStatus === 'Enregistré'" :size="16" aria-hidden="true" />{{
                saveStatus
              }}
            </p>
          </div>
          <h2 id="step-title" tabindex="-1" class="flow-title">{{ titles[step] }}</h2>
          <UiAlert v-if="errorMessage" :title="errorMessage" error class="mt-6"
            ><UiButton v-if="sessionLocked" variant="secondary" class="mt-4" @click="load"
              >Recharger le brouillon</UiButton
            ></UiAlert
          >
          <div v-if="Object.keys(errors).length > 1" class="error-summary" role="alert">
            <h3 class="font-semibold">Vérifiez les champs suivants</h3>
            <ul>
              <li v-for="(message, key) in errors" :key="key">
                <a :href="`#${key}`">{{ message }}</a>
              </li>
            </ul>
          </div>
          <form v-if="step < 3" novalidate class="mt-6" @submit.prevent="advance">
            <fieldset :disabled="sessionLocked">
              <legend class="sr-only">{{ titles[step] }}</legend>
              <template v-if="step === 0"
                ><p class="muted mb-6">
                  Tous les champs sont nécessaires pour compléter cette étape.
                </p>
                <div class="field-grid">
                  <FormField
                    v-for="field in fields"
                    :id="field.key"
                    :key="field.key"
                    :label="field.label"
                    :hint="field.hint"
                    :error="errors[field.key]"
                    ><template #default="slot"
                      ><UiInput
                        :id="field.key"
                        v-model="profile[field.key]"
                        class="flow-input"
                        :type="field.type"
                        :autocomplete="field.autocomplete"
                        :maxlength="field.max"
                        :aria-describedby="slot.describedby"
                        :aria-invalid="slot.invalid"
                        required /></template
                  ></FormField></div
              ></template>
              <div v-else-if="step === 1" class="field-grid">
                <FormField
                  id="qualification"
                  label="Série ou diplôme d’origine"
                  :error="errors.qualification"
                  ><template #default="slot"
                    ><UiSelect
                      id="qualification"
                      v-model="qualification"
                      class="flow-input"
                      :aria-describedby="slot.describedby"
                      :aria-invalid="slot.invalid"
                      required
                    >
                      <option value="" disabled>Choisir votre qualification</option>
                      <option v-for="q in store.qualifications" :key="q.code" :value="q.code">
                        {{ q.name }}
                      </option>
                    </UiSelect></template
                  ></FormField
                ><FormField
                  id="graduationYear"
                  label="Année d’obtention"
                  :error="errors.graduationYear"
                  hint="Indiquez l’année figurant sur votre diplôme."
                  ><template #default="slot"
                    ><UiInput
                      id="graduationYear"
                      v-model="year"
                      class="flow-input"
                      inputmode="numeric"
                      maxlength="4"
                      :aria-describedby="slot.describedby"
                      :aria-invalid="slot.invalid"
                      required /></template
                ></FormField>
              </div>
              <template v-else>
                <p class="muted mb-6">
                  Les filières ci-dessous sont accessibles avec votre qualification
                  {{ store.draft.qualification?.name }}.
                </p>
                <div v-if="programsLoading" role="status">
                  <UiSkeleton /><span class="sr-only">Recherche des filières éligibles…</span>
                </div>
                <UiAlert v-else-if="programsError" :title="programsError" error
                  ><UiButton variant="secondary" class="mt-4" @click="loadPrograms"
                    >Réessayer</UiButton
                  ></UiAlert
                >
                <UiAlert
                  v-else-if="!programs.length"
                  title="Aucune filière proposée pour cette qualification."
                  >Les règles de cette campagne ne permettent pas actuellement de proposer une
                  filière. Vérifiez votre qualification ou renseignez-vous auprès de
                  l’institut.</UiAlert
                >
                <fieldset
                  v-else
                  id="program"
                  tabindex="-1"
                  :aria-describedby="errors.program ? 'program-error' : undefined"
                >
                  <legend class="sr-only">Choisissez une filière</legend>
                  <div class="program-list">
                    <ProgramCard
                      v-for="p in programs"
                      :key="p.code"
                      v-model="program"
                      :program="p"
                      :disabled="sessionLocked"
                    />
                  </div>
                  <p id="program-error" class="field-error">{{ errors.program }}</p>
                </fieldset>
              </template>
            </fieldset>
            <div class="flow-actions">
              <UiButton
                v-if="step > 0"
                variant="secondary"
                :disabled="active > 0"
                @click="goTo(step - 1)"
                ><ArrowLeft :size="18" aria-hidden="true" />Retour</UiButton
              ><UiButton
                type="submit"
                :disabled="
                  active > 0 ||
                  sessionLocked ||
                  (step === 2 && (programsLoading || !!programsError || !programs.length))
                "
                >{{ active ? 'Enregistrement…' : step === 2 ? 'Ajouter les documents' : 'Continuer'
                }}<ArrowRight :size="18" aria-hidden="true"
              /></UiButton>
            </div>
          </form>
          <div v-else-if="step === 3" class="mt-6">
            <UiAlert v-if="needsReplacement" title="Un nouveau relevé est nécessaire."
              >Remplacez votre relevé pour permettre une nouvelle analyse. Les autres documents sont
              conservés.</UiAlert
            >
            <UiSkeleton v-if="documentsLoading" />
            <UiAlert v-else-if="documentsError" :title="documentsError" error
              ><UiButton variant="secondary" @click="loadDocuments">Réessayer</UiButton></UiAlert
            >
            <DocumentsStep
              v-else-if="documents"
              :listing="documents"
              :highlight="needsReplacement ? 'TRANSCRIPT' : undefined"
              :disabled="sessionLocked"
              @saved="documentSaved"
              @busy="uploadBusy"
              @failure="fail"
            >
              <template #transcript-analysis="{ document }">
                <AnalysisStep
                  :key="`${document.version}-${store.draft.qualification?.code}-${store.draft.program?.code}`"
                  :version="store.draft.version"
                  :disabled="active > 0 || sessionLocked"
                  @changed="analysis = $event"
                  @replace="replaceTranscript"
                  @failure="fail"
                />
              </template>
            </DocumentsStep>
            <div class="flow-actions">
              <UiButton variant="secondary" :disabled="active > 0" @click="goTo(2)"
                >Retour</UiButton
              >
              <UiButton
                :disabled="
                  active > 0 ||
                  documentsLoading ||
                  !documents?.complete ||
                  !analysis?.verificationReady ||
                  needsReplacement ||
                  !!documentsError ||
                  sessionLocked
                "
                @click="finishDocuments"
                >Voir le récapitulatif</UiButton
              >
            </div>
          </div>
          <div v-else class="mt-6">
            <p class="muted mb-6">
              Votre brouillon est enregistré. Vérifiez chaque information : vous pouvez encore la
              modifier avant de soumettre.
            </p>
            <section class="review-section">
              <div class="review-title">
                <h3>Vos informations</h3>
                <UiButton variant="secondary" @click="goTo(0)">Modifier les informations</UiButton>
              </div>
              <dl>
                <div>
                  <dt>Nom</dt>
                  <dd>{{ store.draft.profile.lastName }}</dd>
                </div>
                <div>
                  <dt>Prénoms</dt>
                  <dd>{{ store.draft.profile.firstNames }}</dd>
                </div>
                <div>
                  <dt>Email</dt>
                  <dd>{{ store.draft.profile.email }}</dd>
                </div>
                <div>
                  <dt>Téléphone</dt>
                  <dd>{{ store.draft.profile.phone }}</dd>
                </div>
              </dl>
            </section>
            <section class="review-section">
              <div class="review-title">
                <h3>Votre parcours</h3>
                <UiButton variant="secondary" @click="goTo(1)">Modifier le parcours</UiButton>
              </div>
              <dl>
                <div>
                  <dt>Qualification</dt>
                  <dd>{{ store.draft.qualification?.name }}</dd>
                </div>
                <div>
                  <dt>Année</dt>
                  <dd>{{ store.draft.graduationYear }}</dd>
                </div>
              </dl>
            </section>
            <section class="review-section">
              <div class="review-title">
                <h3>Filière souhaitée</h3>
                <UiButton variant="secondary" @click="goTo(2)">Modifier la filière</UiButton>
              </div>
              <p>{{ store.draft.program?.name }}</p>
            </section>
            <section class="review-section">
              <div class="review-title">
                <h3>Documents</h3>
                <UiButton variant="secondary" @click="goTo(3)">Modifier les documents</UiButton>
              </div>
              <DocumentsStep v-if="documents" :listing="documents" readonly />
            </section>
            <AnalysisStep
              :version="store.draft.version"
              readonly
              @changed="reviewAnalysis"
              @failure="fail"
            />
            <section class="submit-panel" aria-labelledby="submit-title">
              <h3 id="submit-title">Soumission définitive</h3>
              <p class="mt-2">
                Après soumission, vous ne pourrez plus modifier votre candidature. Un numéro de
                candidature vous sera attribué et affiché immédiatement.
              </p>
              <UiButton
                id="submit-application"
                class="mt-4"
                :disabled="
                  active > 0 || sessionLocked || submitting || !analysis?.verificationReady
                "
                @click="confirmOpen = true"
                ><Send :size="18" aria-hidden="true" />Soumettre ma candidature</UiButton
              >
            </section>
            <SubmitDialog
              :open="confirmOpen"
              :pending="submitting"
              :program="store.draft.program?.name"
              @confirm="submitApplication"
              @close="confirmOpen = false"
            />
          </div>
        </section>
        <aside class="flow-help" aria-labelledby="help-title">
          <LockKeyhole :size="24" aria-hidden="true" />
          <h2 id="help-title" class="mt-4 text-lg font-semibold">Un brouillon personnel</h2>
          <p class="muted mt-3">
            Votre progression est enregistrée sur le serveur. Utilisez le même navigateur pour
            revenir sur votre dossier.
          </p>
          <p class="muted mt-4">
            Sur un appareil partagé, veillez à protéger votre profil navigateur. Le numéro de
            candidature est attribué à la soumission, après la vérification.
          </p>
        </aside>
      </div>
    </template>
  </CandidateLayout>
</template>
