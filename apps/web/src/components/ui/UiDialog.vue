<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
const props = withDefaults(
  defineProps<{ open: boolean; title: string; pending?: boolean; alert?: boolean }>(),
  { pending: false, alert: false },
)
const emit = defineEmits<{ close: [] }>()
const dialog = ref<HTMLDialogElement | null>(null)
const titleId = `dialog-${Math.random().toString(36).slice(2, 10)}`
let opener: HTMLElement | null = null
// Native modal: inert background and trapped focus; focus returns to the opener.
watch(
  () => props.open,
  async (open) => {
    const element = dialog.value
    if (!element) return
    if (open && !element.open) {
      opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
      element.showModal()
      await nextTick()
      const first =
        element.querySelector<HTMLElement>('[data-initial-focus]') ??
        element.querySelector<HTMLElement>('input, select, textarea, button')
      first?.focus()
    } else if (!open && element.open) {
      element.close()
    }
  },
)
function cancel(event: Event) {
  if (props.pending) event.preventDefault()
}
function closed() {
  emit('close')
  if (opener?.isConnected) opener.focus()
  opener = null
}
</script>
<template>
  <dialog
    ref="dialog"
    class="confirm-dialog admin-dialog"
    :role="alert ? 'alertdialog' : 'dialog'"
    aria-modal="true"
    :aria-labelledby="titleId"
    :aria-busy="pending"
    @cancel="cancel"
    @close="closed"
  >
    <h2 :id="titleId" class="confirm-dialog-title">{{ title }}</h2>
    <slot />
    <div v-if="$slots.actions" class="confirm-dialog-actions">
      <slot name="actions" />
    </div>
  </dialog>
</template>
