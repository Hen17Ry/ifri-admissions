<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { Send } from 'lucide-vue-next'
import UiButton from '@/components/ui/UiButton.vue'
const props = defineProps<{ open: boolean; pending: boolean; program?: string }>()
const emit = defineEmits<{ confirm: []; close: [] }>()
const dialog = ref<HTMLDialogElement | null>(null)
let opener: HTMLElement | null = null
// showModal(): inert background and focus kept inside the dialog by the browser.
watch(
  () => props.open,
  async (open) => {
    const element = dialog.value
    if (!element) return
    if (open && !element.open) {
      opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
      element.showModal()
      await nextTick()
      element.querySelector<HTMLElement>('[data-initial-focus]')?.focus()
    } else if (!open && element.open) {
      element.close()
    }
  },
)
function cancel(event: Event) {
  // Escape closes the dialog, except while the submission request is running.
  if (props.pending) event.preventDefault()
}
function closed() {
  emit('close')
  opener?.focus()
  opener = null
}
</script>
<template>
  <dialog
    ref="dialog"
    class="confirm-dialog"
    role="alertdialog"
    aria-modal="true"
    aria-labelledby="submit-dialog-title"
    aria-describedby="submit-dialog-description"
    :aria-busy="pending"
    @cancel="cancel"
    @close="closed"
  >
    <h2 id="submit-dialog-title" class="confirm-dialog-title">Soumettre votre candidature ?</h2>
    <p id="submit-dialog-description" class="mt-3">
      Après soumission, vous ne pourrez plus modifier votre candidature.
    </p>
    <p v-if="program" class="muted mt-3">
      Filière demandée : <strong>{{ program }}</strong>
    </p>
    <div class="confirm-dialog-actions">
      <UiButton variant="secondary" data-initial-focus :disabled="pending" @click="emit('close')"
        >Annuler</UiButton
      >
      <UiButton :disabled="pending" @click="emit('confirm')"
        ><Send :size="18" aria-hidden="true" />{{
          pending ? 'Soumission en cours…' : 'Confirmer la soumission'
        }}</UiButton
      >
    </div>
  </dialog>
</template>
