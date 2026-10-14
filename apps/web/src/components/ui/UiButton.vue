<script setup lang="ts">
withDefaults(
  defineProps<{ variant?: 'primary' | 'secondary' | 'danger'; type?: 'button' | 'submit' }>(),
  {
    variant: 'primary',
    type: 'button',
  },
)
</script>
<template>
  <button
    :type="type"
    :class="
      variant === 'primary'
        ? 'primary-button'
        : variant === 'danger'
          ? 'danger-button'
          : 'secondary-button'
    "
  >
    <slot />
  </button>
</template>
