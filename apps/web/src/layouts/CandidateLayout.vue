<script setup lang="ts">
import { GraduationCap } from 'lucide-vue-next'
</script>
<template>
  <div class="candidate-shell">
    <header class="candidate-header">
      <div class="candidate-header-inner">
        <GraduationCap :size="32" aria-hidden="true" />
        <div>
          <p class="font-heading text-lg font-semibold">IFRI</p>
          <p class="muted text-sm">Université d’Abomey-Calavi</p>
        </div>
        <span class="candidate-header-label">Admissions</span>
      </div>
    </header>
    <main id="main" class="candidate-main"><slot /></main>
    <footer class="candidate-footer">Institut de Formation et de Recherche en Informatique</footer>
  </div>
</template>
