<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Filter } from 'lucide-vue-next'
import AdminLayout from '@/layouts/AdminLayout.vue'
import FormField from '@/components/ui/FormField.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiInput from '@/components/ui/UiInput.vue'
import UiSelect from '@/components/ui/UiSelect.vue'
import UiSkeleton from '@/components/ui/UiSkeleton.vue'
import {
  actorLabels,
  adminApi,
  adminMessage,
  eventLabels,
  type AuditEntry,
} from '@/features/admin/api'
import { useAdminStore } from '@/features/admin/store'
import { asApiError } from '@/features/application/store'

const store = useAdminStore()
const router = useRouter()
const filters = reactive({ eventType: '', actorType: '', from: '', to: '' })
const events = ref<AuditEntry[]>([])
const eventTypes = ref<string[]>([])
const cursor = ref('')
const loading = ref(false)
const error = ref('')
const status = ref('')
const format = new Intl.DateTimeFormat('fr-FR', {
  dateStyle: 'short',
  timeStyle: 'medium',
  timeZone: 'Africa/Porto-Novo',
})
// Day boundaries in Benin time (UTC+1), sent as RFC 3339 instants.
const instant = (day: string, end = false) =>
  day ? new Date(`${day}T00:00:00+01:00`).getTime() + (end ? 86400000 : 0) : 0
function actor(entry: AuditEntry) {
  return entry.actorLabel || actorLabels[entry.actorType]
}
function target(entry: AuditEntry) {
  if (entry.targetLabel) return entry.targetLabel
  return entry.targetType ? `${entry.targetType} ${entry.targetId?.slice(0, 8)}…` : '—'
}
async function load(more = false) {
  loading.value = true
  error.value = ''
  try {
    const from = instant(filters.from)
    const to = instant(filters.to, true)
    const page = await adminApi.audit({
      eventType: filters.eventType,
      actorType: filters.actorType,
      from: from ? new Date(from).toISOString().replace(/\.\d{3}Z$/, 'Z') : '',
      to: to ? new Date(to).toISOString().replace(/\.\d{3}Z$/, 'Z') : '',
      cursor: more ? cursor.value : '',
    })
    events.value = more ? [...events.value, ...page.events] : page.events
    eventTypes.value = page.eventTypes
    cursor.value = page.nextCursor ?? ''
    status.value = `${events.value.length} événement${events.value.length > 1 ? 's' : ''} affiché${events.value.length > 1 ? 's' : ''}${cursor.value ? ', d’autres sont disponibles' : ''}.`
  } catch (e) {
    const failure = asApiError(e)
    if (failure.status === 401) {
      store.clear(adminMessage(failure))
      await router.replace({ name: 'admin-login' })
      return
    }
    error.value = adminMessage(failure)
  } finally {
    loading.value = false
  }
}
onMounted(async () => {
  document.title = 'Journal d’audit — Administration IFRI'
  document.getElementById('page-title')?.focus()
  await load()
})
</script>
<template>
  <AdminLayout>
    <h1 id="page-title" tabindex="-1">Journal d’audit</h1>
    <p class="muted mt-2">
      Lecture seule. Les identifiants tentés et les adresses IP n’y figurent que sous forme
      d’empreinte (HMAC), jamais en clair.
    </p>
    <form class="admin-filters" aria-label="Filtres du journal" @submit.prevent="load()">
      <FormField id="filter-event" label="Événement"
        ><template #default
          ><UiSelect id="filter-event" v-model="filters.eventType"
            ><option value="">Tous</option>
            <option v-for="type in eventTypes" :key="type" :value="type">
              {{ eventLabels[type] ?? type }}
            </option></UiSelect
          ></template
        ></FormField
      >
      <FormField id="filter-actor" label="Acteur"
        ><template #default
          ><UiSelect id="filter-actor" v-model="filters.actorType"
            ><option value="">Tous</option>
            <option value="ADMIN">Administrateur</option>
            <option value="SYSTEM">Système</option>
            <option value="CANDIDATE">Candidat</option></UiSelect
          ></template
        ></FormField
      >
      <FormField id="filter-from" label="Du"
        ><template #default
          ><UiInput id="filter-from" v-model="filters.from" type="date" /></template
      ></FormField>
      <FormField id="filter-to" label="Au"
        ><template #default><UiInput id="filter-to" v-model="filters.to" type="date" /></template
      ></FormField>
      <UiButton type="submit" variant="secondary" :disabled="loading"
        ><Filter :size="18" aria-hidden="true" />Filtrer</UiButton
      >
    </form>
    <p class="sr-only" role="status" aria-live="polite" aria-atomic="true">{{ status }}</p>
    <UiAlert v-if="error" :title="error" error class="mt-6"
      ><UiButton variant="secondary" class="mt-4" @click="load()">Réessayer</UiButton></UiAlert
    >
    <div v-else-if="loading && !events.length" role="status" class="mt-6">
      <UiSkeleton /><span class="sr-only">Chargement du journal…</span>
    </div>
    <UiAlert v-else-if="!events.length" title="Aucun événement pour ces filtres." class="mt-6"
      >Élargissez la période ou retirez un filtre.</UiAlert
    >
    <div v-else class="admin-table-wrap">
      <table class="admin-table admin-audit-table">
        <caption class="sr-only">
          Événements du journal d’audit, du plus récent au plus ancien
        </caption>
        <thead>
          <tr>
            <th scope="col">Date (heure du Bénin)</th>
            <th scope="col">Événement</th>
            <th scope="col">Acteur</th>
            <th scope="col">Cible</th>
            <th scope="col">Détails techniques</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="entry in events" :key="entry.id">
            <td data-label="Date">{{ format.format(new Date(entry.occurredAt)) }}</td>
            <th scope="row" data-label="Événement">
              {{ eventLabels[entry.eventType] ?? entry.eventType }}
              <span class="muted text-sm admin-cell-email">{{ entry.eventType }}</span>
            </th>
            <td data-label="Acteur">{{ actor(entry) }}</td>
            <td data-label="Cible">{{ target(entry) }}</td>
            <td data-label="Détails">
              <details v-if="Object.keys(entry.metadata).length">
                <summary>Afficher</summary>
                <pre class="admin-metadata">{{ JSON.stringify(entry.metadata, null, 2) }}</pre>
              </details>
              <span v-else class="muted">—</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <UiButton
      v-if="cursor && !error"
      variant="secondary"
      class="mt-6"
      :disabled="loading"
      :aria-busy="loading"
      @click="load(true)"
      >{{ loading ? 'Chargement…' : 'Afficher les événements plus anciens' }}</UiButton
    >
  </AdminLayout>
</template>
