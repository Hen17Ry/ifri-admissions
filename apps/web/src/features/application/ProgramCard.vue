<script setup lang="ts">
import { Check } from 'lucide-vue-next'
import type { Program } from './api'
defineProps<{ program: Program; disabled?: boolean }>()
const model = defineModel<string>({ required: true })
</script>
<template>
  <label class="program-card" :class="{ 'program-selected': model === program.code }">
    <input v-model="model" type="radio" name="program" :value="program.code" :disabled="disabled" />
    <span class="min-w-0"
      ><span class="program-code">{{ program.code }}</span
      ><span class="block font-semibold">{{ program.name }}</span></span
    >
    <Check v-if="model === program.code" :size="20" class="shrink-0" aria-hidden="true" />
  </label>
</template>
