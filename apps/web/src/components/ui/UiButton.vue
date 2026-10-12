<script setup lang="ts">
withDefaults(defineProps<{ variant?: 'primary' | 'secondary'; type?: 'button' | 'submit' }>(), {
  variant: 'primary',
  type: 'button',
})
</script>
<template>
  <button :type="type" :class="variant === 'primary' ? 'primary-button' : 'secondary-button'">
    <slot />
  </button>
</template>
