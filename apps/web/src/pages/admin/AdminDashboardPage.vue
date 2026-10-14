<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { FileCheck2, FolderOpen, Inbox, ScrollText, UserCheck, Users } from 'lucide-vue-next'
import AdminLayout from '@/layouts/AdminLayout.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiSkeleton from '@/components/ui/UiSkeleton.vue'
import { roleLabel } from '@/features/admin/api'
import { useAdminFailure } from '@/features/admin/failure'
import { useAdminStore } from '@/features/admin/store'
import { reviewApi, statusLabels, type Dashboard, type ReviewStatus } from '@/features/review/api'

const store = useAdminStore()
const route = useRoute()
const failure = useAdminFailure()
const dashboard = ref<Dashboard | null>(null)
const loading = ref(true)
const error = ref('')
const statuses = Object.keys(statusLabels) as ReviewStatus[]
const roles = computed(() =>
  [...new Set(store.session?.grants.map((grant) => roleLabel(grant.role)))].join(', '),
)
// One row per program, one column per review status (counts the API returned).
const rows = computed(() => {
  const programs = new Map<string, { name: string; counts: Record<string, number> }>()
  for (const counter of dashboard.value?.counters ?? []) {
    const row = programs.get(counter.program) ?? { name: counter.programName, counts: {} }
    row.counts[counter.status] = counter.count
    programs.set(counter.program, row)
  }
  return [...programs].map(([code, row]) => ({
    code,
    ...row,
    total: Object.values(row.counts).reduce((sum, count) => sum + count, 0),
  }))
})
const total = (status: ReviewStatus) =>
  rows.value.reduce((sum, row) => sum + (row.counts[status] ?? 0), 0)
const missing = computed(() => {
  const coverage = dashboard.value?.coverage
  if (!coverage) return []
  return [
    ...(coverage.deputyDirector
      ? []
      : ['Aucun directeur adjoint actif : aucune correction ne peut être approuvée ni refusée.']),
    ...(coverage.director
      ? []
      : ['Aucun directeur actif : la sélection finale ne pourra pas être validée.']),
  ]
})
async function load() {
  loading.value = true
  error.value = ''
  try {
    dashboard.value = await reviewApi.dashboard()
  } catch (e) {
    error.value = (await failure(e)) ?? ''
  } finally {
    loading.value = false
  }
}
onMounted(async () => {
  document.title = 'Tableau de bord — Administration IFRI'
  document.getElementById('page-title')?.focus()
  await load()
})
</script>
<template>
  <AdminLayout>
    <h1 id="page-title" tabindex="-1">Tableau de bord</h1>
    <p class="muted mt-2">Connecté en tant que {{ store.session?.displayName }} — {{ roles }}</p>
    <UiAlert v-if="route.query.refus" title="Accès non autorisé." class="mt-6"
      >Votre rôle ne donne pas accès à la page demandée.</UiAlert
    >
    <div v-if="loading" role="status" class="mt-6">
      <UiSkeleton /><span class="sr-only">Chargement du tableau de bord…</span>
    </div>
    <UiAlert v-else-if="error" :title="error" error class="mt-6"
      ><UiButton variant="secondary" class="mt-4" @click="load">Réessayer</UiButton></UiAlert
    >
    <template v-else-if="dashboard">
      <UiAlert
        v-if="missing.length"
        title="Organisation incomplète"
        error
        class="mt-6"
        data-testid="coverage-warning"
      >
        <ul class="review-list">
          <li v-for="text in missing" :key="text">{{ text }}</li>
        </ul>
        <RouterLink class="secondary-button mt-4" :to="{ name: 'admin-accounts' }"
          ><Users :size="18" aria-hidden="true" />Gérer les comptes</RouterLink
        >
      </UiAlert>

      <template v-if="dashboard.counters">
        <ul class="review-kpis" aria-label="À traiter">
          <li>
            <RouterLink
              class="review-kpi"
              :to="{
                name: 'admin-applications',
                query: { status: 'TO_REVIEW', assignee: 'none' },
              }"
            >
              <Inbox :size="22" aria-hidden="true" />
              <span class="review-kpi-value">{{ dashboard.unassigned }}</span>
              <span>À vérifier, non pris en charge</span>
            </RouterLink>
          </li>
          <li v-if="dashboard.mine !== undefined">
            <RouterLink
              class="review-kpi"
              :to="{ name: 'admin-applications', query: { status: 'IN_REVIEW', assignee: 'me' } }"
            >
              <UserCheck :size="22" aria-hidden="true" />
              <span class="review-kpi-value">{{ dashboard.mine }}</span>
              <span>Mes dossiers en vérification</span>
            </RouterLink>
          </li>
          <li>
            <RouterLink
              class="review-kpi"
              :to="
                store.can('corrections.decide')
                  ? { name: 'admin-corrections' }
                  : { name: 'admin-applications', query: { pendingCorrection: 'true' } }
              "
            >
              <FileCheck2 :size="22" aria-hidden="true" />
              <span class="review-kpi-value">{{ dashboard.pendingCorrections }}</span>
              <span>Corrections en attente du directeur adjoint</span>
            </RouterLink>
          </li>
        </ul>

        <section class="admin-section review-wide" aria-labelledby="counters-title">
          <h2 id="counters-title">Dossiers soumis par filière et par statut</h2>
          <p v-if="!rows.length" class="muted mt-2">Aucune candidature soumise pour le moment.</p>
          <div v-else class="admin-table-wrap">
            <table class="admin-table review-counters">
              <caption class="sr-only">
                Nombre de dossiers par filière et par statut de vérification
              </caption>
              <thead>
                <tr>
                  <th scope="col">Filière</th>
                  <th v-for="status in statuses" :key="status" scope="col">
                    {{ statusLabels[status] }}
                  </th>
                  <th scope="col">Total</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="row in rows" :key="row.code">
                  <th scope="row">{{ row.name }} ({{ row.code }})</th>
                  <td v-for="status in statuses" :key="status" :data-label="statusLabels[status]">
                    <RouterLink
                      class="review-count-link"
                      :to="{
                        name: 'admin-applications',
                        query: { program: row.code, status },
                      }"
                      :aria-label="`${row.counts[status] ?? 0} dossier(s) ${statusLabels[status]} en ${row.name}`"
                      >{{ row.counts[status] ?? 0 }}</RouterLink
                    >
                  </td>
                  <td data-label="Total">{{ row.total }}</td>
                </tr>
              </tbody>
              <tfoot>
                <tr>
                  <th scope="row">Toutes filières</th>
                  <td v-for="status in statuses" :key="status" :data-label="statusLabels[status]">
                    {{ total(status) }}
                  </td>
                  <td data-label="Total">{{ rows.reduce((sum, row) => sum + row.total, 0) }}</td>
                </tr>
              </tfoot>
            </table>
          </div>
          <RouterLink class="secondary-button mt-4" :to="{ name: 'admin-applications' }"
            ><FolderOpen :size="18" aria-hidden="true" />Tous les dossiers</RouterLink
          >
        </section>
      </template>

      <section v-else class="admin-empty" aria-labelledby="empty-title">
        <Inbox :size="32" aria-hidden="true" />
        <h2 id="empty-title">Administration technique</h2>
        <p class="muted">
          Votre rôle donne accès aux comptes et au journal d’audit, jamais aux dossiers des
          candidats.
        </p>
        <div class="receipt-actions">
          <RouterLink
            v-if="store.can('accounts.read')"
            class="secondary-button"
            :to="{ name: 'admin-accounts' }"
            ><Users :size="18" aria-hidden="true" />Gérer les comptes</RouterLink
          >
          <RouterLink
            v-if="store.can('audit.read')"
            class="secondary-button"
            :to="{ name: 'admin-audit' }"
            ><ScrollText :size="18" aria-hidden="true" />Consulter le journal d’audit</RouterLink
          >
        </div>
      </section>
    </template>
  </AdminLayout>
</template>
