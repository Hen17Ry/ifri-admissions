<script setup lang="ts">
const model = defineModel<string>({ required: true })
</script>
<template>
  <select v-model="model" class="flow-input">
    <slot />
  </select>
</template>
