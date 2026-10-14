<script setup lang="ts">
import { ref } from 'vue'
import { Copy, Download } from 'lucide-vue-next'
import UiButton from '@/components/ui/UiButton.vue'
const props = defineProps<{ codes: string[]; email: string }>()
const status = ref('')
function text() {
  return `Codes de secours IFRI Admissions — ${props.email}\nChaque code ne sert qu’une fois.\n\n${props.codes.join('\n')}\n`
}
async function copy() {
  try {
    if (!navigator.clipboard) throw new Error('clipboard unavailable')
    await navigator.clipboard.writeText(text())
    status.value = 'Codes copiés dans le presse-papiers.'
  } catch {
    status.value = 'La copie automatique a échoué : sélectionnez les codes pour les copier.'
  }
}
function download() {
  const url = URL.createObjectURL(new Blob([text()], { type: 'text/plain;charset=utf-8' }))
  const link = document.createElement('a')
  link.href = url
  link.download = 'ifri-admissions-codes-de-secours.txt'
  link.click()
  URL.revokeObjectURL(url)
  status.value = 'Fichier des codes téléchargé.'
}
</script>
<template>
  <section class="recovery-codes" aria-labelledby="recovery-title">
    <h2 id="recovery-title">Vos codes de secours</h2>
    <p class="mt-2">
      Ils remplacent l’application d’authentification si vous la perdez. Chaque code ne sert qu’une
      fois. Conservez-les hors de cet ordinateur : ils ne seront plus affichés.
    </p>
    <ol class="recovery-list">
      <li v-for="code in codes" :key="code">{{ code }}</li>
    </ol>
    <div class="receipt-actions">
      <UiButton variant="secondary" @click="copy"
        ><Copy :size="18" aria-hidden="true" />Copier les codes</UiButton
      >
      <UiButton variant="secondary" @click="download"
        ><Download :size="18" aria-hidden="true" />Télécharger (.txt)</UiButton
      >
    </div>
    <p class="copy-status" role="status" aria-live="polite" aria-atomic="true">{{ status }}</p>
  </section>
</template>
