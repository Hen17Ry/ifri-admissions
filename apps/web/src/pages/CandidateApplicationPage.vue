<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ArrowLeft, ArrowRight, Check, LockKeyhole } from 'lucide-vue-next'
import CandidateLayout from '@/layouts/CandidateLayout.vue'
import FormField from '@/components/ui/FormField.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiInput from '@/components/ui/UiInput.vue'
import UiSelect from '@/components/ui/UiSelect.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import UiSkeleton from '@/components/ui/UiSkeleton.vue'
import ApplicationStepper from '@/features/application/ApplicationStepper.vue'
import ProgramCard from '@/features/application/ProgramCard.vue'
import { draftApi, type Draft, type Profile, type Program } from '@/features/application/api'
import { academicErrors, profileErrors } from '@/features/application/validation'
import { asApiError, useApplicationStore } from '@/features/application/store'

const store = useApplicationStore()
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
const active = ref(0)
const sessionLocked = ref(false)
const dirty = ref(false)
const titles = [
  'Vos informations',
  'Votre parcours',
  'Filière souhaitée',
  'Vérifiez votre brouillon',
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
  return d.program ? 3 : 2
})
let ready = false
let timer: ReturnType<typeof setTimeout> | undefined
let queue: Promise<void> = Promise.resolve()
let revision = 0
let programsRequest = 0

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
  step.value = reachable.value
}
async function load() {
  clearTimeout(timer)
  await queue.catch(() => {})
  ready = false
  sessionLocked.value = false
  errorMessage.value = ''
  errors.value = {}
  await store.restore()
  if (store.draft && !store.error) await hydrate(store.draft)
}
async function start() {
  await store.start()
  if (store.draft && !store.error) await hydrate(store.draft)
}
function fail(e: unknown) {
  const error = asApiError(e)
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
  await nextTick()
  document.getElementById('step-title')?.focus()
})
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
  if (dirty.value) {
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
      <h2 class="text-xl font-semibold">Quatre étapes, à votre rythme</h2>
      <p class="muted mt-3">
        Vos informations, votre parcours, votre filière, puis un récapitulatif. Les étapes
        complétées sont enregistrées automatiquement.
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
            <p class="eyebrow">Étape {{ step + 1 }} sur 4</p>
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
                >{{ active ? 'Enregistrement…' : step === 2 ? 'Voir le récapitulatif' : 'Continuer'
                }}<ArrowRight :size="18" aria-hidden="true"
              /></UiButton>
            </div>
          </form>
          <div v-else class="mt-6">
            <p class="muted mb-6">
              Votre brouillon est enregistré. Vous pouvez encore modifier vos informations.
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
            <UiAlert title="Votre candidature reste un brouillon." class="mt-6"
              >Aucune soumission définitive n’est effectuée à cette étape. Vous pouvez fermer cette
              page et retrouver les informations enregistrées sur ce navigateur tant que votre
              session est valide.</UiAlert
            >
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
            Sur un appareil partagé, veillez à protéger votre profil navigateur. Aucun numéro
            officiel n’est attribué à ce stade.
          </p>
        </aside>
      </div>
    </template>
  </CandidateLayout>
</template>
