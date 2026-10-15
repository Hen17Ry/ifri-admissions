<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { Download, FileDown, Lock, Save, UserMinus, UserPlus } from 'lucide-vue-next'
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
import { frenchNumber } from '@/features/review/api'
import {
  criterionLabels,
  csvUrl,
  decisionLabels,
  mathSkipped,
  pdfUrl,
  selectionApi,
  variantLabels,
  type Entry,
  type ExportJob,
  type Offer,
  type RankingView,
  type Variant,
} from '@/features/selection/api'

type Dialog = 'freeze' | 'withdraw' | 'promote'
const store = useAdminStore()
const route = useRoute()
const router = useRouter()
const failure = useAdminFailure()
const offers = ref<Offer[]>([])
const selected = ref('')
const view = ref<RankingView | null>(null)
const loading = ref(true)
const loadError = ref('')
const status = ref('')
const conflict = ref(false)
const seats = ref('')
const minimum = ref('')
const settingsErrors = ref<Record<string, string>>({})
const saving = ref(false)
const dialog = ref<Dialog | null>(null)
const target = ref<Entry | null>(null)
const reason = ref('')
const dialogErrors = ref<Record<string, string>>({})
const dialogMessage = ref('')
const pending = ref(false)
const jobs = ref<ExportJob[]>([])
let stopped = false

const frozen = computed(() => view.value?.freeze ?? null)
const canConfigure = computed(() => store.can('ranking.configure') && !frozen.value)
const canFreeze = computed(() => store.can('selection.validate') && !frozen.value)
const canManage = computed(() => store.can('selection.manage') && !!frozen.value)
const canExport = computed(() => store.can('selection.export') && !!frozen.value)
// The seats line is drawn after the last ranked candidate within the seats (frozen: as frozen).
const seatLine = computed(() => frozen.value?.seats ?? view.value?.offer.seats ?? null)
const ranked = computed(() => view.value?.entries.filter((e) => e.rank !== null) ?? [])
const notRanked = computed(() => view.value?.entries.filter((e) => e.rank === null) ?? [])
const summary = computed(() => {
  const list = view.value?.entries ?? []
  return {
    admitted: list.filter((e) => e.decision === 'ADMITTED').length,
    waiting: list.filter((e) => e.decision === 'WAITLISTED').length,
    notRanked: list.filter((e) => e.decision === 'NOT_RANKED').length,
  }
})
const columns = computed(() => (canManage.value ? 10 : 9))
const date = new Intl.DateTimeFormat('fr-FR', {
  dateStyle: 'short',
  timeStyle: 'short',
  timeZone: 'Africa/Porto-Novo',
})
const when = (value: string) => date.format(new Date(value))
const label = (o: Offer) =>
  `${o.programName} (${o.programCode}) — ${o.campaignName}${o.frozen ? ' — gelé' : ''}`
const minimumText = (value: string) => (value ? `${frenchNumber(value)} / 20` : 'aucune')

function fill(v: RankingView) {
  view.value = v
  seats.value = v.offer.seats === null ? '' : String(v.offer.seats)
  minimum.value = v.offer.minimumAverage ? frenchNumber(v.offer.minimumAverage) : ''
  jobs.value = v.exports
  const index = offers.value.findIndex((o) => o.id === v.offer.id)
  if (index >= 0) offers.value[index] = v.offer
}
async function load() {
  if (!selected.value) return
  loading.value = true
  loadError.value = ''
  conflict.value = false
  try {
    fill(await selectionApi.view(selected.value))
  } catch (e) {
    loadError.value = (await failure(e)) ?? ''
  } finally {
    loading.value = false
  }
}
async function init() {
  try {
    offers.value = (await selectionApi.offers()).offers
  } catch (e) {
    loadError.value = (await failure(e)) ?? ''
    loading.value = false
    return
  }
  const wanted = typeof route.query.offre === 'string' ? route.query.offre : ''
  selected.value = offers.value.some((o) => o.id === wanted) ? wanted : (offers.value[0]?.id ?? '')
  if (!selected.value) loading.value = false
}
watch(selected, async (id, previous) => {
  if (!id) return
  if (previous !== undefined && route.query.offre !== id)
    await router.replace({ query: { offre: id } })
  status.value = ''
  await load()
})
async function failed(e: unknown, fields: (f: Record<string, string>) => void) {
  const error = asApiError(e)
  const text = await failure(e)
  if (text === null) return ''
  if (
    ['SELECTION_VERSION_CONFLICT', 'RANKING_SETTINGS_CHANGED', 'RANKING_FROZEN'].includes(
      error.code,
    )
  )
    conflict.value = true
  fields({ ...error.fields })
  return Object.keys(error.fields).length && error.code !== 'RANKING_FREEZE_BLOCKED' ? '' : text
}
async function saveSettings() {
  if (!view.value) return
  settingsErrors.value = {}
  const value = seats.value.trim()
  if (value && !/^\d+$/.test(value)) {
    settingsErrors.value.seats = 'Indiquez un nombre entier de places, entre 1 et 10 000.'
    document.getElementById('seats')?.focus()
    return
  }
  saving.value = true
  try {
    fill(
      await selectionApi.settings(view.value.offer.id, {
        seats: value ? Number(value) : null,
        minimumAverage: minimum.value.trim(),
      }),
    )
    status.value =
      'Places et moyenne minimale enregistrées : le classement provisoire est recalculé.'
  } catch (e) {
    const message = await failed(e, (f) => (settingsErrors.value = f))
    if (message) status.value = message
    await nextTick()
    document.querySelector<HTMLElement>('.selection-settings [aria-invalid="true"]')?.focus()
  } finally {
    saving.value = false
  }
}
function open(kind: Dialog, entry: Entry | null = null) {
  dialog.value = kind
  target.value = entry
  reason.value = ''
  dialogErrors.value = {}
  dialogMessage.value = ''
}
async function confirm() {
  const v = view.value
  if (!v || !dialog.value) return
  if (dialog.value === 'withdraw' && !reason.value.trim()) {
    dialogErrors.value = { reason: 'Indiquez le motif du désistement.' }
    document.getElementById('withdraw-reason')?.focus()
    return
  }
  pending.value = true
  dialogMessage.value = ''
  try {
    const id = v.offer.id
    if (dialog.value === 'freeze') {
      fill(
        await selectionApi.freeze(id, {
          seats: v.offer.seats,
          minimumAverage: v.offer.minimumAverage,
        }),
      )
      status.value = `Classement gelé et validé : liste version 1. Les revues et corrections de cette filière sont désormais bloquées.`
    } else if (dialog.value === 'withdraw') {
      fill(
        await selectionApi.withdraw(id, {
          applicationId: target.value!.applicationId,
          reason: reason.value.trim(),
          listVersion: v.freeze!.listVersion,
        }),
      )
      status.value = `Désistement de ${target.value!.applicationNumber} enregistré : liste version ${view.value!.freeze!.listVersion}.`
    } else {
      fill(
        await selectionApi.promote(id, {
          applicationId: target.value!.applicationId,
          listVersion: v.freeze!.listVersion,
        }),
      )
      status.value = `${target.value!.applicationNumber} est admis : liste version ${view.value!.freeze!.listVersion}.`
    }
    dialog.value = null
  } catch (e) {
    const message = await failed(e, (f) => (dialogErrors.value = f))
    dialogMessage.value = message
  } finally {
    pending.value = false
  }
}
const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))
async function generate(variant: Variant) {
  if (!view.value) return
  status.value = `Génération du PDF (${variantLabels[variant].toLowerCase()})…`
  try {
    let job = await selectionApi.requestPdf(view.value.offer.id, variant)
    jobs.value = [job, ...jobs.value]
    for (let attempt = 0; job.status === 'PENDING' && attempt < 60 && !stopped; attempt++) {
      await wait(1000)
      if (stopped) return
      job = await selectionApi.exportState(job.id)
    }
    jobs.value = jobs.value.map((j) => (j.id === job.id ? job : j))
    status.value =
      job.status === 'READY'
        ? `PDF prêt (${variantLabels[variant].toLowerCase()}, version ${job.listVersion}) : utilisez le lien de téléchargement.`
        : 'Le PDF n’a pas pu être généré. Réessayez ; le CSV reste disponible.'
  } catch (e) {
    status.value = (await failure(e)) ?? ''
  }
}
onMounted(async () => {
  document.title = 'Classement et sélection — Administration IFRI'
  document.getElementById('page-title')?.focus()
  await init()
})
onBeforeUnmount(() => {
  stopped = true
})
</script>
<template>
  <AdminLayout>
    <h1 id="page-title" tabindex="-1">Classement et sélection</h1>
    <p class="muted mt-2 review-intro">
      Seuls les dossiers validés sont classés, sur leur moyenne effective exacte (après corrections
      approuvées). Départage : note de mathématiques si tous les candidats à égalité en ont une,
      puis date de soumission, puis numéro.
    </p>
    <p class="sr-only" role="status" aria-live="polite" aria-atomic="true">{{ status }}</p>

    <div v-if="offers.length" class="selection-offer">
      <FormField id="offer" label="Filière et campagne">
        <template #default
          ><UiSelect id="offer" v-model="selected">
            <option v-for="o in offers" :key="o.id" :value="o.id">{{ label(o) }}</option>
          </UiSelect></template
        >
      </FormField>
    </div>

    <div v-if="loading" role="status" class="mt-6">
      <UiSkeleton /><span class="sr-only">Chargement du classement…</span>
    </div>
    <UiAlert v-else-if="loadError" :title="loadError" error class="mt-6"
      ><UiButton variant="secondary" class="mt-4" @click="load">Réessayer</UiButton></UiAlert
    >
    <section v-else-if="!offers.length" class="admin-empty">
      <h2>Aucune filière à classer</h2>
      <p class="muted">Aucune campagne ouverte ou close ne propose de filière pour le moment.</p>
    </section>
    <template v-else-if="view">
      <UiAlert v-if="status" :title="status" class="mt-6" />
      <UiAlert v-if="conflict" title="La liste a changé depuis son affichage." error class="mt-6"
        >Une autre action vient de la modifier. Rechargez-la avant de continuer.
        <UiButton variant="secondary" class="mt-4" @click="load">Recharger</UiButton>
      </UiAlert>

      <div class="selection-state mt-6" :class="{ 'selection-state-frozen': frozen }">
        <Lock v-if="frozen" :size="22" aria-hidden="true" />
        <p v-if="frozen">
          <strong>Classement gelé et validé</strong> le {{ when(frozen.frozenAt) }} par
          {{ frozen.frozenBy.name }} — liste <strong>version {{ frozen.listVersion }}</strong> du
          {{ when(frozen.versionAt) }}.
        </p>
        <p v-else>
          <strong>Classement provisoire</strong>, recalculé à chaque affichage :
          {{ view.counts.validated }} validé{{ view.counts.validated > 1 ? 's' : '' }},
          {{ view.counts.rejected }} rejeté{{ view.counts.rejected > 1 ? 's' : '' }}, non classés
          dans le classement.
        </p>
      </div>

      <section class="admin-section selection-settings" aria-labelledby="settings-title">
        <h2 id="settings-title">Places et moyenne minimale</h2>
        <form v-if="canConfigure" class="selection-form" novalidate @submit.prevent="saveSettings">
          <FormField
            id="seats"
            label="Nombre de places"
            hint="Entre 1 et 10 000. Obligatoire pour geler."
            :error="settingsErrors.seats"
          >
            <template #default="slot"
              ><UiInput
                id="seats"
                v-model="seats"
                inputmode="numeric"
                autocomplete="off"
                maxlength="5"
                :aria-describedby="slot.describedby"
                :aria-invalid="slot.invalid"
            /></template>
          </FormField>
          <FormField
            id="minimum"
            label="Moyenne minimale (facultative)"
            hint="Sur 20, 2 décimales au plus. En dessous : « Non classé », même s’il reste des places."
            :error="settingsErrors.minimumAverage"
          >
            <template #default="slot"
              ><UiInput
                id="minimum"
                v-model="minimum"
                inputmode="decimal"
                autocomplete="off"
                maxlength="5"
                :aria-describedby="slot.describedby"
                :aria-invalid="slot.invalid"
            /></template>
          </FormField>
          <UiButton type="submit" :disabled="saving" :aria-busy="saving"
            ><Save :size="18" aria-hidden="true" />Enregistrer</UiButton
          >
        </form>
        <p v-else class="mt-2">
          Places : <strong>{{ seatLine ?? 'non défini' }}</strong> · Moyenne minimale :
          <strong>{{
            minimumText(frozen ? frozen.minimumAverage : view.offer.minimumAverage)
          }}</strong>
        </p>
      </section>

      <section v-if="!frozen" class="admin-section" aria-labelledby="freeze-title">
        <h2 id="freeze-title">Gel et validation</h2>
        <ul v-if="view.blockers.length" id="freeze-blockers" class="review-list mt-2">
          <li v-for="b in view.blockers" :key="b.code" class="review-warning">
            {{ b.message }}
            <RouterLink
              v-if="b.code === 'DOSSIERS_PENDING'"
              :to="{
                name: 'admin-applications',
                query: { program: view.offer.programCode, status: 'TO_REVIEW' },
              }"
              >Voir les dossiers</RouterLink
            >
          </li>
        </ul>
        <p v-else class="review-ok mt-2">Toutes les conditions du gel sont réunies.</p>
        <UiButton
          v-if="canFreeze"
          class="mt-4"
          :disabled="view.blockers.length > 0"
          :aria-describedby="view.blockers.length ? 'freeze-blockers' : undefined"
          @click="open('freeze')"
          ><Lock :size="18" aria-hidden="true" />Geler et valider le classement</UiButton
        >
      </section>

      <section v-if="canManage && frozen" class="admin-section" aria-labelledby="manage-title">
        <h2 id="manage-title">Désistements et promotions</h2>
        <p class="mt-2">
          Places libres : <strong>{{ frozen.freeSeats }}</strong>
        </p>
        <div v-if="view.proposal" class="selection-proposal mt-4">
          <p>
            Prochain candidat de la liste d’attente :
            <strong>rang {{ view.proposal.rank }}</strong> — {{ view.proposal.lastName }}
            {{ view.proposal.firstNames }} ({{ view.proposal.applicationNumber }})
          </p>
          <UiButton @click="open('promote', view.proposal)"
            ><UserPlus :size="18" aria-hidden="true" />Promouvoir ce candidat</UiButton
          >
        </div>
        <p v-else class="muted mt-2">
          {{
            frozen.freeSeats > 0
              ? 'La liste d’attente est vide : la place reste libre.'
              : 'Aucune place libre : une promotion suit un désistement d’admis.'
          }}
        </p>
      </section>

      <section v-if="canExport" class="admin-section review-wide" aria-labelledby="exports-title">
        <h2 id="exports-title">Exports — version {{ frozen?.listVersion }}</h2>
        <p class="muted mt-2">
          La liste publique ne contient que le rang, le numéro, le nom, les prénoms et la décision.
          Chaque export est journalisé.
        </p>
        <div class="admin-row-actions mt-4">
          <a
            v-for="variant in ['PUBLIC', 'INTERNAL'] as Variant[]"
            :key="`csv-${variant}`"
            class="secondary-button"
            :href="csvUrl(view.offer.id, variant)"
            download
            ><Download :size="18" aria-hidden="true" />{{ variantLabels[variant] }} (CSV)</a
          >
          <UiButton
            v-for="variant in ['PUBLIC', 'INTERNAL'] as Variant[]"
            :key="`pdf-${variant}`"
            variant="secondary"
            @click="generate(variant)"
            ><FileDown :size="18" aria-hidden="true" />{{ variantLabels[variant] }} (PDF)</UiButton
          >
        </div>
        <ul v-if="jobs.length" class="selection-exports mt-4">
          <li v-for="job in jobs" :key="job.id">
            {{ variantLabels[job.variant] }}, version {{ job.listVersion }} —
            {{ job.requestedBy.name }}, {{ when(job.requestedAt) }} :
            <a v-if="job.status === 'READY'" :href="pdfUrl(job.id)" download>télécharger le PDF</a>
            <span v-else-if="job.status === 'PENDING'" class="muted">génération en cours…</span>
            <span v-else class="review-warning">échec de la génération</span>
          </li>
        </ul>
      </section>

      <section class="admin-section review-wide" aria-labelledby="ranking-title">
        <h2 id="ranking-title">
          {{ frozen ? 'Classement gelé' : 'Classement provisoire' }} —
          {{ view.offer.programName }}
        </h2>
        <p v-if="seatLine" class="muted mt-2">
          {{ summary.admitted }} admis, {{ summary.waiting }} en liste d’attente,
          {{ summary.notRanked }} non classé{{ summary.notRanked > 1 ? 's' : '' }}. Moyennes
          tronquées à 2 décimales pour la lecture : la fraction exacte fait foi.
        </p>
        <p v-if="!view.entries.length" class="muted mt-2">
          Aucun dossier validé pour cette filière.
        </p>
        <div v-else class="admin-table-wrap">
          <table class="admin-table selection-table">
            <caption class="sr-only">
              Classement de la filière
              {{
                view.offer.programName
              }}, par rang
            </caption>
            <thead>
              <tr>
                <th scope="col">Rang</th>
                <th scope="col">Numéro</th>
                <th scope="col">Candidat</th>
                <th scope="col">Série</th>
                <th scope="col" class="review-number">Moyenne</th>
                <th scope="col" class="review-number">Maths</th>
                <th scope="col">Soumis le</th>
                <th scope="col">Départage</th>
                <th scope="col">Décision</th>
                <th v-if="canManage" scope="col"><span class="sr-only">Actions</span></th>
              </tr>
            </thead>
            <tbody>
              <template v-for="entry in ranked" :key="entry.applicationId">
                <tr :class="{ 'selection-withdrawn': entry.decision === 'WITHDRAWN' }">
                  <th scope="row" data-label="Rang">{{ entry.rank }}</th>
                  <td data-label="Numéro">
                    <RouterLink
                      class="review-number-link"
                      :to="{ name: 'admin-application', params: { id: entry.applicationId } }"
                      >{{ entry.applicationNumber }}</RouterLink
                    >
                  </td>
                  <td data-label="Candidat">
                    <span class="admin-cell-name">{{ entry.lastName }}</span>
                    <span class="admin-cell-email">{{ entry.firstNames }}</span>
                  </td>
                  <td data-label="Série">{{ entry.qualification }}</td>
                  <td data-label="Moyenne" class="review-number">
                    {{ frenchNumber(entry.average.display) }}
                  </td>
                  <td data-label="Maths" class="review-number">
                    {{ entry.mathGrade ? frenchNumber(entry.mathGrade) : '—' }}
                  </td>
                  <td data-label="Soumis le">{{ when(entry.submittedAt) }}</td>
                  <td data-label="Départage">
                    {{ entry.decisiveCriterion ? criterionLabels[entry.decisiveCriterion] : '—' }}
                    <span v-if="entry.mathTiebreak === 'SKIPPED'" class="muted text-sm block">{{
                      mathSkipped
                    }}</span>
                  </td>
                  <td data-label="Décision">
                    <span
                      v-if="entry.decision"
                      class="admin-status"
                      :class="`selection-${entry.decision.toLowerCase()}`"
                      >{{ decisionLabels[entry.decision] }}</span
                    >
                    <span v-else class="muted">—</span>
                  </td>
                  <td v-if="canManage" data-label="Actions">
                    <UiButton
                      v-if="entry.decision === 'ADMITTED' || entry.decision === 'WAITLISTED'"
                      variant="secondary"
                      :aria-label="`Désistement : ${entry.applicationNumber}`"
                      @click="open('withdraw', entry)"
                      ><UserMinus :size="18" aria-hidden="true" />Désistement</UiButton
                    >
                  </td>
                </tr>
                <tr v-if="seatLine && entry.rank === seatLine" class="selection-quota">
                  <td :colspan="columns">Limite des {{ seatLine }} places</td>
                </tr>
              </template>
              <tr v-if="notRanked.length" class="selection-quota">
                <td :colspan="columns">
                  Non classés : moyenne effective inférieure à
                  {{ minimumText(frozen ? frozen.minimumAverage : view.offer.minimumAverage) }}
                </td>
              </tr>
              <tr v-for="entry in notRanked" :key="entry.applicationId">
                <th scope="row" data-label="Rang">—</th>
                <td data-label="Numéro">
                  <RouterLink
                    class="review-number-link"
                    :to="{ name: 'admin-application', params: { id: entry.applicationId } }"
                    >{{ entry.applicationNumber }}</RouterLink
                  >
                </td>
                <td data-label="Candidat">
                  <span class="admin-cell-name">{{ entry.lastName }}</span>
                  <span class="admin-cell-email">{{ entry.firstNames }}</span>
                </td>
                <td data-label="Série">{{ entry.qualification }}</td>
                <td data-label="Moyenne" class="review-number">
                  {{ frenchNumber(entry.average.display) }}
                </td>
                <td data-label="Maths" class="review-number">
                  {{ entry.mathGrade ? frenchNumber(entry.mathGrade) : '—' }}
                </td>
                <td data-label="Soumis le">{{ when(entry.submittedAt) }}</td>
                <td data-label="Départage">
                  {{ entry.decisiveCriterion ? criterionLabels[entry.decisiveCriterion] : '—' }}
                </td>
                <td data-label="Décision">
                  <span class="admin-status selection-not_ranked">Non classé</span>
                </td>
                <td v-if="canManage" data-label="Actions"></td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-if="frozen" class="admin-section review-wide" aria-labelledby="events-title">
        <h2 id="events-title">Historique de la liste</h2>
        <ol class="review-timeline">
          <li>
            <p>
              <strong>Version 1 — gel et validation</strong>
              <span class="muted"> — {{ when(frozen.frozenAt) }}</span>
            </p>
            <p class="muted text-sm">Par {{ frozen.frozenBy.name }}</p>
          </li>
          <li v-for="event in view.events" :key="event.version">
            <p>
              <strong
                >Version {{ event.version }} —
                {{ event.type === 'WITHDRAWN' ? 'Désistement' : 'Promotion' }} de
                {{ event.applicationNumber }}</strong
              >
              <span class="muted"> — {{ when(event.occurredAt) }}</span>
            </p>
            <p class="muted text-sm">{{ event.candidate }} · par {{ event.actor.name }}</p>
            <p v-if="event.reason" class="review-quote">{{ event.reason }}</p>
          </li>
        </ol>
      </section>
    </template>

    <UiDialog
      :open="!!dialog"
      :title="
        dialog === 'freeze'
          ? 'Geler et valider ce classement ?'
          : dialog === 'withdraw'
            ? 'Enregistrer un désistement'
            : 'Promouvoir ce candidat ?'
      "
      alert
      :pending="pending"
      @close="dialog = null"
    >
      <template v-if="dialog === 'freeze' && view">
        <p class="mt-3">
          {{ view.offer.programName }} : {{ view.offer.seats }} places, moyenne minimale
          {{ minimumText(view.offer.minimumAverage) }}. {{ summary.admitted }} admis,
          {{ summary.waiting }} en liste d’attente, {{ summary.notRanked }} non classé{{
            summary.notRanked > 1 ? 's' : ''
          }}.
        </p>
        <p class="mt-2">
          Le gel est <strong>définitif</strong> : aucun dégel n’est possible dans l’interface. Le
          classement est enregistré tel quel ; les revues et corrections de cette filière seront
          bloquées.
        </p>
      </template>
      <template v-else-if="target">
        <p class="mt-3">
          <strong>Rang {{ target.rank }}</strong> — {{ target.lastName }}
          {{ target.firstNames }} ({{ target.applicationNumber }}),
          {{ target.decision ? decisionLabels[target.decision].toLowerCase() : '' }}
        </p>
        <p v-if="dialog === 'promote'" class="mt-2">
          Il occupe la place libérée et devient admis. La liste change de version.
        </p>
      </template>
      <UiAlert v-if="dialogMessage" :title="dialogMessage" error class="mt-4" />
      <form
        v-if="dialog === 'withdraw'"
        id="withdraw-form"
        class="admin-form"
        novalidate
        @submit.prevent="confirm"
      >
        <FormField
          id="withdraw-reason"
          label="Motif du désistement"
          hint="Conservé dans l’historique de la liste."
          :error="dialogErrors.reason"
        >
          <template #default="slot">
            <textarea
              id="withdraw-reason"
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
        <UiButton variant="secondary" data-initial-focus :disabled="pending" @click="dialog = null"
          >Annuler</UiButton
        >
        <UiButton
          :type="dialog === 'withdraw' ? 'submit' : 'button'"
          :form="dialog === 'withdraw' ? 'withdraw-form' : undefined"
          :variant="dialog === 'promote' ? 'primary' : 'danger'"
          :disabled="pending"
          :aria-busy="pending"
          @click="dialog !== 'withdraw' && confirm()"
          >{{
            dialog === 'freeze'
              ? 'Geler définitivement'
              : dialog === 'withdraw'
                ? 'Enregistrer le désistement'
                : 'Promouvoir'
          }}</UiButton
        >
      </template>
    </UiDialog>
  </AdminLayout>
</template>
