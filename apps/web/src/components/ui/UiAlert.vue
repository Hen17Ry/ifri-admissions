<script setup lang="ts">
withDefaults(defineProps<{ title: string; error?: boolean }>(), { error: false })
</script>
<template>
  <section
    :class="['flow-alert', { 'flow-alert-error': error }]"
    :role="error ? 'alert' : undefined"
  >
    <h2 class="text-lg font-semibold">{{ title }}</h2>
    <div class="mt-2"><slot /></div>
  </section>
</template>
