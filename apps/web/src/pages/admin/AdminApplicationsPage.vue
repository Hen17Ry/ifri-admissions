<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter, type LocationQuery } from 'vue-router'
import { ChevronLeft, ChevronRight, RotateCcw, Search } from 'lucide-vue-next'
import AdminLayout from '@/layouts/AdminLayout.vue'
import FormField from '@/components/ui/FormField.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiInput from '@/components/ui/UiInput.vue'
import UiSelect from '@/components/ui/UiSelect.vue'
import UiSkeleton from '@/components/ui/UiSkeleton.vue'
import { useAdminFailure } from '@/features/admin/failure'
import { useAdminStore } from '@/features/admin/store'
import {
  flagLabels,
  frenchNumber,
  reviewApi,
  statusLabels,
  type ListFilter,
  type ListPage,
  type Reference,
} from '@/features/review/api'

// The URL holds the filters: dashboard links, back navigation and reloads keep the same view.
const store = useAdminStore()
const route = useRoute()
const router = useRouter()
const failure = useAdminFailure()
const pageSize = 25
const sorts = {
  'submittedAt:desc': 'Soumission, plus récentes d’abord',
  'submittedAt:asc': 'Soumission, plus anciennes d’abord',
  'average:desc': 'Moyenne, décroissante',
  'average:asc': 'Moyenne, croissante',
  'name:asc': 'Nom, de A à Z',
  'name:desc': 'Nom, de Z à A',
} as const
type SortKey = keyof typeof sorts
const text = (value: LocationQuery[string]) => (typeof value === 'string' ? value : '')
function fromQuery(query: LocationQuery): ListFilter {
  const [sort, direction] = (
    Object.hasOwn(sorts, text(query.sort)) ? text(query.sort) : 'submittedAt:desc'
  ).split(':') as [ListFilter['sort'], ListFilter['direction']]
  return {
    q: text(query.q),
    program: text(query.program),
    qualification: text(query.qualification),
    status: text(query.status),
    flag: text(query.flag),
    assignee: text(query.assignee),
    pendingCorrection: query.pendingCorrection === 'true',
    sort,
    direction,
    page: Math.max(1, Number.parseInt(text(query.page), 10) || 1),
    pageSize,
  }
}
const form = reactive({ ...fromQuery(route.query), sortKey: 'submittedAt:desc' as SortKey })
const reference = ref<Reference>({
  rejectionReasons: [],
  agents: [],
  programs: [],
  qualifications: [],
})
const result = ref<ListPage | null>(null)
const loading = ref(false)
const error = ref('')
const status = ref('')
const current = computed(() => fromQuery(route.query))
const pages = computed(() =>
  result.value ? Math.max(1, Math.ceil(result.value.total / result.value.pageSize)) : 1,
)
const filtered = computed(() =>
  ['q', 'program', 'qualification', 'status', 'flag', 'assignee', 'pendingCorrection'].some(
    (key) => key in route.query,
  ),
)
const ariaSort = (column: ListFilter['sort']) =>
  current.value.sort === column
    ? current.value.direction === 'asc'
      ? 'ascending'
      : 'descending'
    : undefined
const date = new Intl.DateTimeFormat('fr-FR', {
  dateStyle: 'short',
  timeStyle: 'short',
  timeZone: 'Africa/Porto-Novo',
})

function sync() {
  Object.assign(form, current.value, {
    sortKey: `${current.value.sort}:${current.value.direction}` as SortKey,
  })
}
function query(filter: Omit<ListFilter, 'pageSize'>, sortKey: SortKey): Record<string, string> {
  const entries: Record<string, string> = {}
  for (const key of ['q', 'program', 'qualification', 'status', 'flag', 'assignee'] as const) {
    const value = filter[key].trim()
    if (value) entries[key] = value
  }
  if (filter.pendingCorrection) entries.pendingCorrection = 'true'
  if (sortKey !== 'submittedAt:desc') entries.sort = sortKey
  if (filter.page > 1) entries.page = String(filter.page)
  return entries
}
async function apply() {
  await router.push({ query: query({ ...form, page: 1 }, form.sortKey) })
}
async function reset() {
  await router.push({ query: {} })
}
async function goTo(page: number) {
  await router.push({ query: query({ ...current.value, page }, form.sortKey) })
  document.getElementById('results-title')?.focus()
}
async function load() {
  loading.value = true
  error.value = ''
  try {
    result.value = await reviewApi.list(current.value)
    const total = result.value.total
    status.value =
      total === 0
        ? 'Aucun dossier ne correspond à ces critères.'
        : `${total} dossier${total > 1 ? 's' : ''}, page ${result.value.page} sur ${pages.value}.`
  } catch (e) {
    error.value = (await failure(e)) ?? ''
  } finally {
    loading.value = false
  }
}
watch(
  () => route.query,
  async () => {
    sync()
    await load()
  },
)
onMounted(async () => {
  document.title = 'Candidatures — Administration IFRI'
  document.getElementById('page-title')?.focus()
  sync()
  const [, loaded] = await Promise.allSettled([load(), reviewApi.reference()])
  if (loaded.status === 'fulfilled') reference.value = loaded.value
})
</script>
<template>
  <AdminLayout>
    <h1 id="page-title" tabindex="-1">Candidatures soumises</h1>
    <p class="muted mt-2">
      Coordonnées des candidats visibles uniquement dans le dossier ; chaque consultation est
      journalisée.
    </p>

    <form
      class="review-filters"
      role="search"
      aria-label="Rechercher et filtrer les dossiers"
      @submit.prevent="apply"
    >
      <div class="review-search">
        <FormField
          id="filter-q"
          label="Rechercher"
          hint="Nom ou prénoms (accents ignorés), numéro IFRI-… ou adresse email exacte."
        >
          <template #default="slot"
            ><UiInput
              id="filter-q"
              v-model="form.q"
              type="search"
              maxlength="120"
              autocomplete="off"
              :aria-describedby="slot.describedby"
          /></template>
        </FormField>
      </div>
      <FormField id="filter-program" label="Filière">
        <template #default
          ><UiSelect id="filter-program" v-model="form.program">
            <option value="">Toutes</option>
            <option v-for="program in reference.programs" :key="program.code" :value="program.code">
              {{ program.label }}
            </option>
          </UiSelect></template
        >
      </FormField>
      <FormField id="filter-qualification" label="Série ou diplôme">
        <template #default
          ><UiSelect id="filter-qualification" v-model="form.qualification">
            <option value="">Toutes</option>
            <option
              v-for="qualification in reference.qualifications"
              :key="qualification.code"
              :value="qualification.code"
            >
              {{ qualification.label }}
            </option>
          </UiSelect></template
        >
      </FormField>
      <FormField id="filter-status" label="Statut">
        <template #default
          ><UiSelect id="filter-status" v-model="form.status">
            <option value="">Tous</option>
            <option v-for="(label, code) in statusLabels" :key="code" :value="code">
              {{ label }}
            </option>
          </UiSelect></template
        >
      </FormField>
      <FormField id="filter-assignee" label="Agent en charge">
        <template #default
          ><UiSelect id="filter-assignee" v-model="form.assignee">
            <option value="">Tous</option>
            <option value="none">Non pris en charge</option>
            <option v-if="store.can('review.claim')" value="me">Moi</option>
            <option v-for="agent in reference.agents" :key="agent.id" :value="agent.id">
              {{ agent.name }}
            </option>
          </UiSelect></template
        >
      </FormField>
      <FormField id="filter-flag" label="Signalement">
        <template #default
          ><UiSelect id="filter-flag" v-model="form.flag">
            <option value="">Indifférent</option>
            <option value="ANY">Au moins un signalement</option>
            <option v-for="(label, code) in flagLabels" :key="code" :value="code">
              {{ label }}
            </option>
          </UiSelect></template
        >
      </FormField>
      <FormField id="filter-sort" label="Trier par">
        <template #default
          ><UiSelect id="filter-sort" v-model="form.sortKey">
            <option v-for="(label, key) in sorts" :key="key" :value="key">{{ label }}</option>
          </UiSelect></template
        >
      </FormField>
      <div class="review-filter-footer">
        <label class="admin-check">
          <input v-model="form.pendingCorrection" type="checkbox" />Correction en attente
        </label>
        <div class="review-filter-actions">
          <UiButton type="submit"><Search :size="18" aria-hidden="true" />Rechercher</UiButton>
          <UiButton v-if="filtered" variant="secondary" @click="reset"
            ><RotateCcw :size="18" aria-hidden="true" />Réinitialiser</UiButton
          >
        </div>
      </div>
    </form>

    <p class="sr-only" role="status" aria-live="polite" aria-atomic="true">{{ status }}</p>
    <h2 id="results-title" class="review-results-title" tabindex="-1">
      {{ result ? `${result.total} dossier${result.total > 1 ? 's' : ''}` : 'Dossiers' }}
    </h2>
    <p id="average-note" class="muted text-sm mt-1">
      Moyennes tronquées à 2 décimales pour la lecture : la fraction exacte, visible dans le
      dossier, fait foi.
    </p>
    <UiAlert v-if="error" :title="error" error class="mt-6"
      ><UiButton variant="secondary" class="mt-4" @click="load">Réessayer</UiButton></UiAlert
    >
    <div v-else-if="loading && !result" role="status" class="mt-6">
      <UiSkeleton /><span class="sr-only">Chargement des dossiers…</span>
    </div>
    <section v-else-if="result && !result.items.length" class="admin-empty">
      <h2>Aucun dossier ne correspond à ces critères</h2>
      <p class="muted">
        Vérifiez l’orthographe, retirez un filtre ou recherchez par numéro de dossier.
      </p>
      <UiButton v-if="filtered" variant="secondary" @click="reset"
        ><RotateCcw :size="18" aria-hidden="true" />Réinitialiser les filtres</UiButton
      >
    </section>
    <div v-else-if="result" class="admin-table-wrap" :aria-busy="loading">
      <table class="admin-table review-table">
        <caption class="sr-only">
          Dossiers soumis, page
          {{
            result.page
          }}
          sur
          {{
            pages
          }}
        </caption>
        <thead>
          <tr>
            <th scope="col">Numéro</th>
            <th scope="col" :aria-sort="ariaSort('name')">Candidat</th>
            <th scope="col">Série</th>
            <th scope="col">Filière</th>
            <th scope="col" :aria-sort="ariaSort('average')" class="review-number">Moyenne</th>
            <th scope="col" :aria-sort="ariaSort('submittedAt')">Soumis le</th>
            <th scope="col">Statut</th>
            <th scope="col">Agent</th>
            <th scope="col">Signalements</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in result.items" :key="item.id">
            <th scope="row" data-label="Numéro">
              <RouterLink
                class="review-number-link"
                :to="{ name: 'admin-application', params: { id: item.id } }"
                >{{ item.applicationNumber }}</RouterLink
              >
            </th>
            <td data-label="Candidat">
              <span class="admin-cell-name">{{ item.lastName }}</span>
              <span class="admin-cell-email">{{ item.firstNames }}</span>
            </td>
            <td data-label="Série">{{ item.qualificationLabel }}</td>
            <td data-label="Filière">{{ item.program }}</td>
            <td data-label="Moyenne" class="review-number">
              <span v-if="item.average" aria-describedby="average-note">{{
                frenchNumber(item.average.display)
              }}</span>
              <span v-else class="muted">—</span>
            </td>
            <td data-label="Soumis le">{{ date.format(new Date(item.submittedAt)) }}</td>
            <td data-label="Statut">
              <span class="admin-status" :class="`review-status-${item.status.toLowerCase()}`">{{
                statusLabels[item.status]
              }}</span>
            </td>
            <td data-label="Agent">{{ item.assignee?.name ?? '—' }}</td>
            <td data-label="Signalements">
              <ul v-if="item.flags.length || item.pendingCorrections" class="admin-chips">
                <li v-for="flag in item.flags" :key="flag" class="review-flag">
                  {{ flagLabels[flag] ?? flag }}
                </li>
                <li v-if="item.pendingCorrections" class="review-flag">Correction en attente</li>
              </ul>
              <span v-else class="muted">—</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <nav v-if="result && pages > 1" class="review-pagination" aria-label="Pagination">
      <UiButton
        variant="secondary"
        :disabled="result.page <= 1 || loading"
        @click="goTo(result.page - 1)"
        ><ChevronLeft :size="18" aria-hidden="true" />Précédente</UiButton
      >
      <p>Page {{ result.page }} sur {{ pages }}</p>
      <UiButton
        variant="secondary"
        :disabled="result.page >= pages || loading"
        @click="goTo(result.page + 1)"
        >Suivante<ChevronRight :size="18" aria-hidden="true"
      /></UiButton>
    </nav>
  </AdminLayout>
</template>
