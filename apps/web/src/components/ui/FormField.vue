<script setup lang="ts">
defineProps<{ id: string; label: string; hint?: string; error?: string }>()
</script>
<template>
  <div class="form-field">
    <label :for="id">{{ label }}</label>
    <slot :describedby="`${id}-hint ${id}-error`" :invalid="!!error" />
    <p :id="`${id}-hint`" class="field-hint">{{ hint }}</p>
    <p :id="`${id}-error`" class="field-error">{{ error }}</p>
  </div>
</template>
