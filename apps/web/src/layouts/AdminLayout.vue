<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import {
  LayoutDashboard,
  LogOut,
  Menu,
  ScrollText,
  ShieldCheck,
  UserCog,
  Users,
  X,
} from 'lucide-vue-next'
import { useAdminStore } from '@/features/admin/store'

const store = useAdminStore()
const route = useRoute()
const router = useRouter()
const menuOpen = ref(false)
const links = computed(() =>
  [
    {
      name: 'admin-dashboard',
      label: 'Tableau de bord',
      icon: LayoutDashboard,
      permission: 'dashboard.view',
    },
    { name: 'admin-accounts', label: 'Comptes', icon: Users, permission: 'accounts.read' },
    { name: 'admin-audit', label: 'Journal d’audit', icon: ScrollText, permission: 'audit.read' },
    { name: 'admin-account', label: 'Mon compte', icon: UserCog, permission: '' },
  ].filter((link) => !link.permission || store.can(link.permission)),
)
watch(
  () => route.fullPath,
  () => {
    menuOpen.value = false
  },
)
async function logout() {
  await store.logout()
  await router.replace({ name: 'admin-login' })
}
</script>
<template>
  <div class="admin-shell">
    <header class="admin-header">
      <div class="admin-brand">
        <ShieldCheck :size="28" aria-hidden="true" />
        <div>
          <p class="font-heading font-semibold">IFRI Admissions</p>
          <p class="muted text-sm">Administration</p>
        </div>
      </div>
      <button
        type="button"
        class="secondary-button admin-menu-button"
        :aria-expanded="menuOpen"
        aria-controls="admin-nav"
        @click="menuOpen = !menuOpen"
      >
        <X v-if="menuOpen" :size="18" aria-hidden="true" /><Menu
          v-else
          :size="18"
          aria-hidden="true"
        />Menu
      </button>
      <div class="admin-user">
        <span class="admin-user-name">{{ store.session?.displayName }}</span>
        <button type="button" class="secondary-button" @click="logout">
          <LogOut :size="18" aria-hidden="true" />Déconnexion
        </button>
      </div>
    </header>
    <div class="admin-body">
      <nav
        id="admin-nav"
        class="admin-nav"
        :class="{ 'admin-nav-open': menuOpen }"
        aria-label="Administration"
      >
        <ul>
          <li v-for="link in links" :key="link.name">
            <RouterLink
              :to="{ name: link.name }"
              class="admin-nav-link"
              :aria-current="route.name === link.name ? 'page' : undefined"
              ><component :is="link.icon" :size="20" aria-hidden="true" />{{
                link.label
              }}</RouterLink
            >
          </li>
        </ul>
      </nav>
      <main id="main" class="admin-main"><slot /></main>
    </div>
  </div>
</template>
