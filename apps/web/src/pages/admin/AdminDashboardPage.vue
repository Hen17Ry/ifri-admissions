<script setup lang="ts">
import { onMounted } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { Inbox, ScrollText, Users } from 'lucide-vue-next'
import AdminLayout from '@/layouts/AdminLayout.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import { roleLabels } from '@/features/admin/api'
import { useAdminStore } from '@/features/admin/store'

const store = useAdminStore()
const route = useRoute()
onMounted(() => {
  document.title = 'Tableau de bord — Administration IFRI'
  document.getElementById('page-title')?.focus()
})
</script>
<template>
  <AdminLayout>
    <h1 id="page-title" tabindex="-1">Tableau de bord</h1>
    <p class="muted mt-2">
      Connecté en tant que {{ store.session?.displayName }} —
      {{
        store.session?.grants
          .map((g) => roleLabels[g.role] + (g.program ? ` (${g.program})` : ''))
          .join(', ')
      }}
    </p>
    <UiAlert v-if="route.query.refus" title="Accès non autorisé." class="mt-6"
      >Votre rôle ne donne pas accès à la page demandée.</UiAlert
    >
    <section class="admin-empty" aria-labelledby="empty-title">
      <Inbox :size="32" aria-hidden="true" />
      <h2 id="empty-title">Aucun dossier à traiter pour le moment</h2>
      <p class="muted">
        La consultation et la vérification des candidatures soumises seront disponibles ici dans une
        prochaine version.
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
  </AdminLayout>
</template>
