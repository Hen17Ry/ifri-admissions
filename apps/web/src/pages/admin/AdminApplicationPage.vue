<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import {
  ArrowLeft,
  CheckCircle2,
  ExternalLink,
  Eye,
  EyeOff,
  FileSearch,
  Hand,
  PencilLine,
  RotateCcw,
  UserRoundCog,
  XCircle,
} from 'lucide-vue-next'
import AdminLayout from '@/layouts/AdminLayout.vue'
import FormField from '@/components/ui/FormField.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiDialog from '@/components/ui/UiDialog.vue'
import UiInput from '@/components/ui/UiInput.vue'
import UiSelect from '@/components/ui/UiSelect.vue'
import UiSkeleton from '@/components/ui/UiSkeleton.vue'
import { useAdminFailure } from '@/features/admin/failure'
import { useAdminStore } from '@/features/admin/store'
import { asApiError } from '@/features/application/store'
import ProofViewer from '@/features/review/components/ProofViewer.vue'
import {
  documentLabels,
  documentUrl,
  eventLabels,
  fieldLabels,
  exactNumber,
  flagLabels,
  frenchNumber,
  methodLabels,
  reviewApi,
  statusLabels,
  type CorrectionField,
  type Detail,
  type Reference,
  type ReviewAction,
  type SubjectView,
} from '@/features/review/api'

type Dialog = 'validate' | 'reject' | 'reassign' | 'revert' | 'propose'
const store = useAdminStore()
const route = useRoute()
const failure = useAdminFailure()
const id = computed(() => String(route.params.id))
const detail = ref<Detail | null>(null)
const reference = ref<Reference>({
  rejectionReasons: [],
  agents: [],
  programs: [],
  qualifications: [],
})
const loading = ref(true)
const loadError = ref('')
const status = ref('')
const conflict = ref(false)
const preview = ref('')
const proof = ref<SubjectView | null>(null)
// Dialogs share one form; the API checks everything again.
const dialog = ref<Dialog | null>(null)
const pending = ref(false)
const dialogMessage = ref('')
const errors = ref<Record<string, string>>({})
const form = reactive({
  reasonCode: '',
  comment: '',
  assignee: '',
  subject: null as SubjectView | null,
  field: 'GRADE' as CorrectionField,
  value: '',
})
const backTo = computed(() => {
  const back = window.history.state?.back
  return typeof back === 'string' && back.startsWith('/admin/candidatures')
    ? back
    : '/admin/candidatures'
})
const date = new Intl.DateTimeFormat('fr-FR', {
  dateStyle: 'medium',
  timeStyle: 'short',
  timeZone: 'Africa/Porto-Novo',
})
const when = (value: string | null | undefined) => (value ? date.format(new Date(value)) : '—')
const mine = computed(
  () => !!detail.value?.assignee && detail.value.assignee.id === store.session?.id,
)
const open = computed(() => detail.value?.status === 'IN_REVIEW' && !detail.value.frozen)
const pendingCorrections = computed(
  () => detail.value?.corrections.filter((correction) => correction.status === 'PENDING') ?? [],
)
const can = computed(() => {
  const d = detail.value
  if (!d) return {} as Record<Dialog | 'claim', boolean>
  return {
    claim: store.can('review.claim') && d.status === 'TO_REVIEW' && !d.frozen,
    validate: store.can('review.decide') && open.value && mine.value,
    reject: store.can('review.decide') && open.value && mine.value,
    propose: store.can('corrections.propose') && open.value && mine.value,
    reassign: store.can('review.reassign') && open.value,
    revert:
      store.can('review.revert') &&
      (d.status === 'VALIDATED' || d.status === 'REJECTED') &&
      !d.frozen,
  }
})
const anyAction = computed(() => Object.values(can.value).some(Boolean))
const otherAgents = computed(() =>
  reference.value.agents.filter((agent) => agent.id !== detail.value?.assignee?.id),
)
const seriesState = computed(() => {
  const d = detail.value
  if (!d?.declaredSeries) return ''
  if (!d.readSeries) return 'unread'
  return d.readSeries === d.declaredSeries ? 'match' : 'mismatch'
})
const averageChanged = computed(() => {
  const d = detail.value
  return (
    !!d?.snapshotAverage &&
    !!d.effectiveAverage &&
    (d.snapshotAverage.weightedSum !== d.effectiveAverage.weightedSum ||
      d.snapshotAverage.coefficientSum !== d.effectiveAverage.coefficientSum)
  )
})
const reasonLabel = (code?: string) =>
  reference.value.rejectionReasons.find((reason) => reason.code === code)?.label ?? code ?? ''
const corrected = (original: string, effective: string) => Number(original) !== Number(effective)
const incoming: Record<string, string> = {
  DUPLICATE_EMAIL: 'utilise la même adresse email que ce dossier',
  DUPLICATE_PHONE: 'utilise le même numéro de téléphone que ce dossier',
  DUPLICATE_TRANSCRIPT: 'contient un relevé identique (même fichier) à celui de ce dossier',
}
const dialogTitles: Record<Dialog, string> = {
  validate: 'Valider ce dossier ?',
  reject: 'Rejeter ce dossier',
  reassign: 'Réattribuer ce dossier',
  revert: 'Remettre ce dossier en vérification',
  propose: 'Proposer une correction',
}

async function load() {
  loading.value = true
  loadError.value = ''
  conflict.value = false
  try {
    const [loaded, choices] = await Promise.all([reviewApi.detail(id.value), reviewApi.reference()])
    detail.value = loaded
    reference.value = choices
    document.title = `Dossier ${loaded.applicationNumber} — Administration IFRI`
  } catch (e) {
    loadError.value = (await failure(e)) ?? ''
  } finally {
    loading.value = false
  }
}
function openDialog(
  kind: Dialog,
  subject: SubjectView | null = null,
  field: CorrectionField = 'GRADE',
) {
  Object.assign(form, { reasonCode: '', comment: '', assignee: '', subject, field, value: '' })
  errors.value = {}
  dialogMessage.value = ''
  dialog.value = kind
}
// A refused action keeps the dialog open with the field errors returned by the API.
async function submit(action: () => Promise<Detail>, done: string) {
  pending.value = true
  dialogMessage.value = ''
  errors.value = {}
  try {
    detail.value = await action()
    dialog.value = null
    status.value = done
  } catch (e) {
    const error = asApiError(e)
    const text = await failure(e)
    if (text === null) return
    if (error.code === 'REVIEW_VERSION_CONFLICT') conflict.value = true
    errors.value = { ...error.fields }
    if (!Object.keys(error.fields).length) dialogMessage.value = text
  } finally {
    pending.value = false
  }
}
async function claim() {
  if (!detail.value) return
  pending.value = true
  status.value = ''
  try {
    detail.value = await reviewApi.transition(id.value, 'claim', { version: detail.value.version })
    status.value = 'Dossier pris en charge : vous pouvez le vérifier.'
  } catch (e) {
    const error = asApiError(e)
    const text = await failure(e)
    if (text === null) return
    if (error.code === 'REVIEW_VERSION_CONFLICT' || error.code === 'REVIEW_TRANSITION_INVALID')
      conflict.value = true
    status.value = text
  } finally {
    pending.value = false
  }
}
async function confirmDialog() {
  const d = detail.value
  if (!d || !dialog.value) return
  const local: Record<string, string> = {}
  if (dialog.value === 'reject' && !form.reasonCode)
    local.reasonCode = 'Choisissez un motif de rejet.'
  if (dialog.value === 'reassign' && !form.assignee) local.assignee = 'Choisissez un autre agent.'
  if (dialog.value === 'propose' && !form.value.trim())
    local.value = 'Saisissez la valeur proposée.'
  if (dialog.value !== 'validate' && !form.comment.trim())
    local[dialog.value === 'propose' ? 'reason' : 'comment'] =
      'Ce champ est obligatoire : expliquez votre décision.'
  if (Object.keys(local).length) {
    errors.value = local
    await nextTick()
    document.querySelector<HTMLElement>('.admin-dialog[open] [aria-invalid="true"]')?.focus()
    return
  }
  const version = d.version
  const texts: Record<Exclude<Dialog, 'propose'>, string> = {
    validate: 'Dossier validé.',
    reject: 'Dossier rejeté.',
    reassign: 'Dossier réattribué.',
    revert: 'Dossier remis en vérification.',
  }
  if (dialog.value === 'propose') {
    const subject = form.subject!
    await submit(
      () =>
        reviewApi.propose(id.value, {
          version,
          subjectId: subject.subjectId,
          field: form.field,
          value: form.value.trim(),
          reason: form.comment.trim(),
        }),
      `Correction proposée pour ${subject.name} : elle attend la décision du directeur adjoint.`,
    )
    return
  }
  const action: ReviewAction = dialog.value
  await submit(
    () =>
      reviewApi.transition(id.value, action, {
        version,
        reasonCode: action === 'reject' ? form.reasonCode : undefined,
        comment: action === 'validate' ? undefined : form.comment.trim(),
        assignee: action === 'reassign' ? form.assignee : undefined,
      }),
    texts[action],
  )
}
function showProof(subject: SubjectView) {
  proof.value = proof.value?.subjectId === subject.subjectId ? null : subject
}
onMounted(async () => {
  document.title = 'Dossier — Administration IFRI'
  document.getElementById('page-title')?.focus()
  await load()
})
</script>
<template>
  <AdminLayout>
    <RouterLink class="link-button" :to="backTo"
      ><ArrowLeft :size="18" aria-hidden="true" />Retour à la liste</RouterLink
    >
    <h1 id="page-title" tabindex="-1" class="mt-2">
      {{ detail ? `Dossier ${detail.applicationNumber}` : 'Dossier' }}
    </h1>
    <p class="sr-only" role="status" aria-live="polite" aria-atomic="true">{{ status }}</p>

    <div v-if="loading && !detail" role="status" class="mt-6">
      <UiSkeleton /><span class="sr-only">Chargement du dossier…</span>
    </div>
    <UiAlert v-else-if="loadError" :title="loadError" error class="mt-6"
      ><UiButton variant="secondary" class="mt-4" @click="load">Réessayer</UiButton></UiAlert
    >
    <template v-else-if="detail">
      <div class="review-summary">
        <p class="review-candidate">{{ detail.lastName }} {{ detail.firstNames }}</p>
        <span class="admin-status" :class="`review-status-${detail.status.toLowerCase()}`">{{
          statusLabels[detail.status]
        }}</span>
        <p class="muted">
          Soumis le {{ when(detail.submittedAt) }} · {{ detail.programName }} ({{ detail.program }})
          · Agent en charge : {{ detail.assignee?.name ?? 'aucun' }}
        </p>
      </div>

      <UiAlert v-if="status" :title="status" class="mt-6" />
      <UiAlert v-if="conflict" title="Le dossier a changé depuis son affichage." error class="mt-6"
        >Une autre personne vient d’agir sur ce dossier. Rechargez-le pour voir son état actuel.
        <UiButton variant="secondary" class="mt-4" @click="load">Recharger le dossier</UiButton>
      </UiAlert>
      <UiAlert v-if="detail.frozen" title="Classement gelé" class="mt-6"
        >Le classement de cette filière est gelé : le dossier ne peut plus être modifié.</UiAlert
      >
      <UiAlert
        v-if="detail.status === 'REJECTED'"
        :title="`Motif du rejet : ${detail.rejectionLabel ?? reasonLabel(detail.rejectionCode)}`"
        class="mt-6"
        ><p class="review-quote">{{ detail.rejectionComment }}</p></UiAlert
      >

      <section class="admin-section review-wide" aria-labelledby="review-title">
        <h2 id="review-title">Vérification</h2>
        <p v-if="detail.status === 'IN_REVIEW' && !mine" class="muted mt-2">
          Pris en charge par {{ detail.assignee?.name }} : seul cet agent peut valider, rejeter ou
          proposer une correction.
        </p>
        <p v-else-if="detail.status === 'TO_REVIEW'" class="muted mt-2">
          Ce dossier attend qu’un agent le prenne en charge.
        </p>
        <p
          v-if="pendingCorrections.length && detail.status === 'IN_REVIEW'"
          id="validation-blocked"
          class="review-blocked mt-2"
        >
          {{ pendingCorrections.length }} correction{{ pendingCorrections.length > 1 ? 's' : '' }}
          en attente du directeur adjoint : la validation est impossible tant qu’elle n’est pas
          décidée.
        </p>
        <div v-if="anyAction" class="admin-row-actions mt-4">
          <UiButton v-if="can.claim" :disabled="pending" @click="claim"
            ><Hand :size="18" aria-hidden="true" />Prendre en charge</UiButton
          >
          <UiButton
            v-if="can.validate"
            :disabled="pending || pendingCorrections.length > 0"
            :aria-describedby="pendingCorrections.length ? 'validation-blocked' : undefined"
            @click="openDialog('validate')"
            ><CheckCircle2 :size="18" aria-hidden="true" />Valider le dossier</UiButton
          >
          <UiButton v-if="can.reject" variant="danger" @click="openDialog('reject')"
            ><XCircle :size="18" aria-hidden="true" />Rejeter</UiButton
          >
          <UiButton v-if="can.reassign" variant="secondary" @click="openDialog('reassign')"
            ><UserRoundCog :size="18" aria-hidden="true" />Réattribuer</UiButton
          >
          <UiButton v-if="can.revert" variant="secondary" @click="openDialog('revert')"
            ><RotateCcw :size="18" aria-hidden="true" />Remettre en vérification</UiButton
          >
        </div>
        <p
          v-else-if="detail.status !== 'TO_REVIEW' && detail.status !== 'IN_REVIEW'"
          class="muted mt-2"
        >
          Aucune action disponible pour votre rôle sur ce dossier.
        </p>
      </section>

      <section class="admin-section" aria-labelledby="candidate-title">
        <h2 id="candidate-title">Candidat</h2>
        <dl class="review-facts">
          <div>
            <dt>Nom</dt>
            <dd>{{ detail.lastName }}</dd>
          </div>
          <div>
            <dt>Prénoms</dt>
            <dd>{{ detail.firstNames }}</dd>
          </div>
          <div>
            <dt>Adresse email</dt>
            <dd>{{ detail.email }}</dd>
          </div>
          <div>
            <dt>Téléphone</dt>
            <dd>{{ detail.phone }}</dd>
          </div>
          <div>
            <dt>Diplôme ou série d’origine</dt>
            <dd>{{ detail.qualificationLabel }} ({{ detail.qualification }})</dd>
          </div>
          <div>
            <dt>Année d’obtention</dt>
            <dd>{{ detail.graduationYear }}</dd>
          </div>
          <div>
            <dt>Filière demandée</dt>
            <dd>{{ detail.programName }} ({{ detail.program }})</dd>
          </div>
          <div>
            <dt>Campagne</dt>
            <dd>{{ detail.campaign }}</dd>
          </div>
          <div v-if="detail.declaredSeries" class="review-facts-wide">
            <dt>Série déclarée et série lue sur le relevé</dt>
            <dd>
              Déclarée : <strong>{{ detail.declaredSeries }}</strong> · Lue :
              <strong>{{ detail.readSeries || 'non lue' }}</strong>
              <span v-if="seriesState === 'match'" class="review-ok"> — concordantes</span>
              <span v-else-if="seriesState === 'unread'" class="review-warning">
                — série non lue sur le relevé : vérifiez-la sur le document</span
              >
              <span v-else class="review-warning"> — différentes : vérifiez le relevé</span>
            </dd>
          </div>
        </dl>
      </section>

      <section class="admin-section" aria-labelledby="average-title">
        <h2 id="average-title">Moyenne</h2>
        <template v-if="detail.effectiveAverage">
          <p class="review-average">
            <span class="review-average-value">{{
              frenchNumber(detail.effectiveAverage.display)
            }}</span>
            <span class="muted">/ 20, tronquée à 2 décimales pour la lecture</span>
          </p>
          <p class="mt-2">
            Valeur exacte, qui fait foi :
            {{ frenchNumber(detail.effectiveAverage.weightedSum) }} ÷
            {{ frenchNumber(detail.effectiveAverage.coefficientSum) }} =
            <span class="review-exact">{{ exactNumber(detail.effectiveAverage) }}</span>
            <span class="muted">{{
              detail.effectiveAverage.exact ? '' : ' (développement infini, tronqué)'
            }}</span>
          </p>
          <p v-if="averageChanged && detail.snapshotAverage" class="mt-2 review-warning">
            Moyenne recalculée après corrections approuvées. Moyenne au dépôt :
            {{ frenchNumber(detail.snapshotAverage.display) }} ({{
              frenchNumber(detail.snapshotAverage.weightedSum)
            }}
            ÷ {{ frenchNumber(detail.snapshotAverage.coefficientSum) }}).
          </p>
          <p class="muted text-sm mt-2">
            Le classement utilisera cette moyenne retenue, calculée par le serveur.
          </p>
        </template>
        <p v-else class="muted mt-2">Moyenne indisponible.</p>
      </section>

      <section class="admin-section review-wide" aria-labelledby="subjects-title">
        <h2 id="subjects-title">Notes du relevé</h2>
        <p class="muted mt-2">
          Valeurs lues sur le relevé et confirmées par le candidat, à côté des valeurs retenues
          après corrections approuvées.
        </p>
        <div class="admin-table-wrap">
          <table class="admin-table review-subjects">
            <caption class="sr-only">
              Matières de classement, notes et coefficients
            </caption>
            <thead>
              <tr>
                <th scope="col">Matière</th>
                <th scope="col" class="review-number">Note du relevé</th>
                <th scope="col" class="review-number">Note retenue</th>
                <th scope="col" class="review-number">Coeff. du relevé</th>
                <th scope="col" class="review-number">Coeff. retenu</th>
                <th scope="col">Validation</th>
                <th scope="col"><span class="sr-only">Actions</span></th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="subject in detail.subjects" :key="subject.subjectId">
                <th scope="row" data-label="Matière">{{ subject.name }}</th>
                <td data-label="Note du relevé" class="review-number">
                  {{ frenchNumber(subject.grade) }}
                </td>
                <td data-label="Note retenue" class="review-number">
                  <strong>{{ frenchNumber(subject.effectiveGrade) }}</strong>
                  <span v-if="corrected(subject.grade, subject.effectiveGrade)" class="review-tag"
                    >corrigée</span
                  >
                </td>
                <td data-label="Coeff. du relevé" class="review-number">
                  {{ frenchNumber(subject.coefficient) }}
                </td>
                <td data-label="Coeff. retenu" class="review-number">
                  <strong>{{ frenchNumber(subject.effectiveCoefficient) }}</strong>
                  <span
                    v-if="corrected(subject.coefficient, subject.effectiveCoefficient)"
                    class="review-tag"
                    >corrigé</span
                  >
                </td>
                <td data-label="Validation">
                  {{ methodLabels[subject.validationMethod] ?? subject.validationMethod }}
                  <span v-if="subject.reconstructedCells?.length" class="muted text-sm block"
                    >Cellule reconstruite depuis le total :
                    {{ subject.reconstructedCells.join(', ') }}</span
                  >
                </td>
                <td data-label="Actions">
                  <div class="admin-row-actions">
                    <UiButton
                      variant="secondary"
                      :aria-expanded="proof?.subjectId === subject.subjectId"
                      aria-controls="proof-panel"
                      :aria-label="`Preuve de lecture : ${subject.name}`"
                      @click="showProof(subject)"
                      ><FileSearch :size="18" aria-hidden="true" />Preuve</UiButton
                    >
                    <UiButton
                      v-if="can.propose"
                      variant="secondary"
                      :aria-label="`Corriger : ${subject.name}`"
                      @click="openDialog('propose', subject)"
                      ><PencilLine :size="18" aria-hidden="true" />Corriger</UiButton
                    >
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div id="proof-panel" class="review-proof-panel" aria-live="polite">
          <template v-if="proof">
            <h3 class="review-subtitle">Preuve de lecture — {{ proof.name }}</h3>
            <ProofViewer
              :key="proof.subjectId"
              :application-id="detail.id"
              :subject-id="proof.subjectId"
              :subject-name="proof.name"
            />
          </template>
        </div>
      </section>

      <section class="admin-section review-wide" aria-labelledby="corrections-title">
        <h2 id="corrections-title">Corrections</h2>
        <p v-if="!detail.corrections.length" class="muted mt-2">
          Aucune correction proposée. Une correction est proposée par l’agent en charge et décidée
          par le directeur adjoint ; le relevé soumis n’est jamais modifié.
        </p>
        <ul v-else class="review-corrections">
          <li v-for="correction in detail.corrections" :key="correction.id">
            <p>
              <strong
                >{{ fieldLabels[correction.field] }} — {{ correction.subjectName }} :
                {{ frenchNumber(correction.currentValue) }} →
                {{ frenchNumber(correction.proposedValue) }}</strong
              >
              <span
                class="admin-status"
                :class="`review-correction-${correction.status.toLowerCase()}`"
                >{{
                  { PENDING: 'En attente', APPROVED: 'Approuvée', REFUSED: 'Refusée' }[
                    correction.status
                  ]
                }}</span
              >
            </p>
            <p class="muted text-sm">
              Valeur du relevé : {{ frenchNumber(correction.originalValue) }} · proposée par
              {{ correction.proposedBy.name }} le {{ when(correction.proposedAt) }}
            </p>
            <p class="review-quote">{{ correction.reason }}</p>
            <p v-if="correction.decidedBy" class="muted text-sm">
              {{ correction.status === 'APPROVED' ? 'Approuvée' : 'Refusée' }} par
              {{ correction.decidedBy.name }} le {{ when(correction.decidedAt) }}
              <template v-if="correction.decisionReason">
                : « {{ correction.decisionReason }} »</template
              >
            </p>
          </li>
        </ul>
      </section>

      <section class="admin-section review-wide" aria-labelledby="documents-title">
        <h2 id="documents-title">Pièces du dossier</h2>
        <p class="muted mt-2">
          Versions exactes soumises. Sur un petit écran, ouvrez le document dans un nouvel onglet.
        </p>
        <ul class="review-documents">
          <li v-for="doc in detail.documents" :key="doc.type">
            <div>
              <p class="font-semibold">{{ documentLabels[doc.type] ?? doc.type }}</p>
              <p class="muted text-sm">
                {{ doc.filename }} · version {{ doc.version }} ·
                {{ Math.max(1, Math.round(doc.sizeBytes / 1024)) }} Ko
              </p>
            </div>
            <div class="admin-row-actions">
              <UiButton
                variant="secondary"
                :aria-expanded="preview === doc.type"
                aria-controls="document-preview"
                @click="preview = preview === doc.type ? '' : doc.type"
                ><component
                  :is="preview === doc.type ? EyeOff : Eye"
                  :size="18"
                  aria-hidden="true"
                />{{ preview === doc.type ? 'Masquer l’aperçu' : 'Aperçu'
                }}<span class="sr-only"> : {{ documentLabels[doc.type] }}</span></UiButton
              >
              <a
                class="secondary-button"
                :href="documentUrl(detail.id, doc.type)"
                target="_blank"
                rel="noopener noreferrer"
                ><ExternalLink :size="18" aria-hidden="true" />Nouvel onglet<span class="sr-only">
                  : {{ documentLabels[doc.type] }}</span
                ></a
              >
            </div>
          </li>
        </ul>
        <div id="document-preview">
          <iframe
            v-if="preview"
            :key="preview"
            class="review-pdf"
            :src="documentUrl(detail.id, preview)"
            :title="`Aperçu : ${documentLabels[preview] ?? preview}`"
            referrerpolicy="no-referrer"
          />
        </div>
      </section>

      <section class="admin-section review-wide" aria-labelledby="flags-title">
        <h2 id="flags-title">Doublons et signalements</h2>
        <p v-if="!detail.flags.length" class="muted mt-2">Aucun signalement.</p>
        <ul v-else class="review-list mt-2">
          <li v-for="flag in detail.flags" :key="flag.code + flag.direction + flag.applicationId">
            <template v-if="flag.direction === 'OUTGOING'">
              <strong>{{ flagLabels[flag.code] ?? flag.code }}</strong>
              <template v-if="flag.applicationNumber">
                — même valeur que le dossier
                <RouterLink
                  :to="{ name: 'admin-application', params: { id: flag.applicationId } }"
                  >{{ flag.applicationNumber }}</RouterLink
                ></template
              >
            </template>
            <template v-else>
              Le dossier
              <RouterLink :to="{ name: 'admin-application', params: { id: flag.applicationId } }">{{
                flag.applicationNumber
              }}</RouterLink>
              {{ incoming[flag.code] ?? `porte le signalement ${flag.code}` }}.
            </template>
          </li>
        </ul>
      </section>

      <section class="admin-section review-wide" aria-labelledby="history-title">
        <h2 id="history-title">Historique</h2>
        <ol class="review-timeline">
          <li v-for="event in detail.history" :key="event.version">
            <p>
              <strong>{{ eventLabels[event.type] ?? event.type }}</strong>
              <span class="muted"> — {{ when(event.occurredAt) }}</span>
            </p>
            <p class="muted text-sm">
              {{ event.actor ? `Par ${event.actor.name}` : 'Par le système' }}
              <template v-if="event.fromStatus && event.fromStatus !== event.toStatus">
                · {{ statusLabels[event.fromStatus as keyof typeof statusLabels] }} →
                {{ statusLabels[event.toStatus] }}</template
              >
              <template v-if="event.type === 'REASSIGNED'">
                · {{ event.fromAssignee?.name ?? 'aucun agent' }} →
                {{ event.toAssignee?.name }}</template
              >
              <template v-if="event.reasonCode">
                · Motif : {{ reasonLabel(event.reasonCode) }}</template
              >
            </p>
            <p v-if="event.comment" class="review-quote">{{ event.comment }}</p>
          </li>
        </ol>
      </section>
    </template>

    <UiDialog
      :open="!!dialog"
      :title="dialog ? dialogTitles[dialog] : ''"
      :alert="dialog === 'reject'"
      :pending="pending"
      @close="dialog = null"
    >
      <UiAlert v-if="dialogMessage" :title="dialogMessage" error class="mt-4" />
      <form id="review-dialog-form" class="admin-form" novalidate @submit.prevent="confirmDialog">
        <p v-if="dialog === 'validate'">
          Le dossier sera déclaré conforme. Il pourra participer au classement de sa filière ; un
          responsable du secrétariat peut le remettre en vérification avant le gel du classement.
        </p>
        <FormField
          v-if="dialog === 'reject'"
          id="reject-reason"
          label="Motif du rejet"
          :error="errors.reasonCode"
        >
          <template #default="slot"
            ><UiSelect
              id="reject-reason"
              v-model="form.reasonCode"
              :aria-describedby="slot.describedby"
              :aria-invalid="slot.invalid"
            >
              <option value="">Choisissez un motif</option>
              <option
                v-for="reason in reference.rejectionReasons"
                :key="reason.code"
                :value="reason.code"
              >
                {{ reason.label }}
              </option>
            </UiSelect></template
          >
        </FormField>
        <FormField
          v-if="dialog === 'reassign'"
          id="reassign-agent"
          label="Nouvel agent en charge"
          :error="errors.assignee"
        >
          <template #default="slot"
            ><UiSelect
              id="reassign-agent"
              v-model="form.assignee"
              :aria-describedby="slot.describedby"
              :aria-invalid="slot.invalid"
            >
              <option value="">Choisissez un agent</option>
              <option v-for="agent in otherAgents" :key="agent.id" :value="agent.id">
                {{ agent.name }}
              </option>
            </UiSelect></template
          >
        </FormField>
        <template v-if="dialog === 'propose' && form.subject">
          <p>
            <strong>{{ form.subject.name }}</strong> — note du relevé
            {{ frenchNumber(form.subject.grade) }} (retenue
            {{ frenchNumber(form.subject.effectiveGrade) }}), coefficient du relevé
            {{ frenchNumber(form.subject.coefficient) }} (retenu
            {{ frenchNumber(form.subject.effectiveCoefficient) }}).
          </p>
          <fieldset>
            <legend class="font-semibold">Valeur à corriger</legend>
            <label class="admin-check"
              ><input
                v-model="form.field"
                type="radio"
                name="correction-field"
                value="GRADE"
              />Note</label
            >
            <label class="admin-check"
              ><input
                v-model="form.field"
                type="radio"
                name="correction-field"
                value="COEFFICIENT"
              />Coefficient</label
            >
          </fieldset>
          <FormField
            id="correction-value"
            :label="form.field === 'GRADE' ? 'Note proposée (sur 20)' : 'Coefficient proposé'"
            :hint="
              form.field === 'GRADE'
                ? 'Entre 0 et 20, 3 décimales au plus, par exemple 12,5.'
                : 'Nombre positif, 2 décimales au plus, lu sur le relevé.'
            "
            :error="errors.value"
          >
            <template #default="slot"
              ><UiInput
                id="correction-value"
                v-model="form.value"
                inputmode="decimal"
                autocomplete="off"
                maxlength="6"
                :aria-describedby="slot.describedby"
                :aria-invalid="slot.invalid"
            /></template>
          </FormField>
        </template>
        <FormField
          v-if="dialog && dialog !== 'validate'"
          id="dialog-comment"
          :label="
            dialog === 'propose'
              ? 'Motif de la correction'
              : dialog === 'reject'
                ? 'Commentaire'
                : 'Motif'
          "
          :hint="
            dialog === 'propose'
              ? 'Ce que montre le relevé et pourquoi la lecture doit être corrigée. Visible du directeur adjoint.'
              : 'Conservé dans l’historique du dossier.'
          "
          :error="errors.comment ?? errors.reason"
        >
          <template #default="slot">
            <textarea
              id="dialog-comment"
              v-model="form.comment"
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
          :data-initial-focus="dialog === 'reject' || dialog === 'validate' ? '' : undefined"
          @click="dialog = null"
          >Annuler</UiButton
        >
        <UiButton
          type="submit"
          form="review-dialog-form"
          :variant="dialog === 'reject' ? 'danger' : 'primary'"
          :disabled="pending"
          :aria-busy="pending"
          >{{
            {
              validate: 'Valider',
              reject: 'Rejeter le dossier',
              reassign: 'Réattribuer',
              revert: 'Remettre en vérification',
              propose: 'Proposer la correction',
            }[dialog ?? 'validate']
          }}</UiButton
        >
      </template>
    </UiDialog>
  </AdminLayout>
</template>
