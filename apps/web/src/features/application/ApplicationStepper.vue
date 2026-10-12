<script setup lang="ts">
import { Check } from 'lucide-vue-next'
defineProps<{ current: number; reachable: number; busy: boolean }>()
defineEmits<{ change: [number] }>()
const labels = [
  'Vos informations',
  'Votre parcours',
  'Filière souhaitée',
  'Documents',
  'Vérification',
]
</script>
<template>
  <nav aria-label="Étapes du brouillon" class="flow-stepper">
    <ol>
      <li v-for="(label, i) in labels" :key="label">
        <button
          type="button"
          :disabled="i > reachable || busy"
          :aria-current="i === current ? 'step' : undefined"
          @click="$emit('change', i)"
        >
          <span
            class="step-number"
            :class="{ 'step-active': i === current, 'step-complete': i < reachable }"
            ><Check v-if="i < reachable" :size="18" aria-hidden="true" /><span
              v-else
              aria-hidden="true"
              >{{ i + 1 }}</span
            ></span
          >
          <span class="step-label">{{ label }}</span>
          <span class="sr-only"
            >Étape {{ i + 1 }} sur 5{{
              i === current ? ', actuelle' : i < reachable ? ', terminée' : ', à compléter'
            }}</span
          >
        </button>
      </li>
    </ol>
  </nav>
</template>
