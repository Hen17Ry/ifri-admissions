<script setup lang="ts">
import { ShieldCheck } from 'lucide-vue-next'
</script>
<template>
  <div class="candidate-shell admin-auth-shell">
    <header class="candidate-header">
      <div class="candidate-header-inner">
        <ShieldCheck :size="32" aria-hidden="true" />
        <div>
          <p class="font-heading text-lg font-semibold">IFRI</p>
          <p class="muted text-sm">Université d’Abomey-Calavi</p>
        </div>
        <span class="candidate-header-label">Administration</span>
      </div>
    </header>
    <main id="main" class="admin-auth-main">
      <div class="admin-auth-panel"><slot /></div>
    </main>
    <footer class="candidate-footer">Institut de Formation et de Recherche en Informatique</footer>
  </div>
</template>
