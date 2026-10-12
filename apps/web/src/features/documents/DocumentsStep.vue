<script setup lang="ts">
import { ref } from 'vue'
import { Check, FileText, Upload } from 'lucide-vue-next'
import UiAlert from '@/components/ui/UiAlert.vue'
import type { ApplicationDocument, DocumentListing } from './api'
import { documentsApi } from './api'
import { asApiError } from '@/features/application/store'
const props = defineProps<{ listing: DocumentListing; readonly?: boolean; disabled?: boolean }>()
const emit = defineEmits<{ saved: [DocumentListing]; busy: [boolean]; failure: [unknown] }>()
const pending = ref('')
const errors = ref<Record<string, string>>({})
async function choose(event: Event, document: ApplicationDocument) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || pending.value || props.disabled) return
  errors.value[document.type] = ''
  if (!file.name.toLowerCase().endsWith('.pdf') || file.type !== 'application/pdf') {
    errors.value[document.type] = 'Choisissez un fichier au format PDF.'
    return
  }
  if (!file.size) {
    errors.value[document.type] = 'Ce fichier est vide.'
    return
  }
  if (file.size > props.listing.maxBytes) {
    errors.value[document.type] = 'Ce fichier dépasse la taille maximale autorisée.'
    return
  }
  pending.value = document.type
  emit('busy', true)
  try {
    emit('saved', await documentsApi.upload(document.type, file, props.listing.applicationVersion))
  } catch (e) {
    errors.value[document.type] = asApiError(e).message
    emit('failure', e)
  } finally {
    pending.value = ''
    emit('busy', false)
  }
}
function size(bytes: number) {
  return new Intl.NumberFormat('fr-FR', { maximumFractionDigits: 1 }).format(bytes / 1024) + ' Ko'
}
</script>
<template>
  <div class="documents-step">
    <p v-if="!readonly" class="muted mb-6">
      Ajoutez un PDF pour chaque document. Taille maximale : {{ size(listing.maxBytes) }} par
      fichier. Vous pourrez remplacer un fichier sans perdre sa version précédente.
    </p>
    <p role="status" aria-atomic="true" class="mb-4">
      {{
        listing.complete
          ? 'Les documents obligatoires sont présents.'
          : `${listing.documents.filter((d) => d.uploaded).length} document(s) ajouté(s). Votre dossier documentaire est incomplet.`
      }}
    </p>
    <ul class="document-list">
      <li
        v-for="document in listing.documents"
        :key="document.type"
        class="document-row"
        :aria-busy="pending === document.type"
      >
        <div class="document-title">
          <Check
            v-if="document.uploaded"
            :size="20"
            class="success shrink-0"
            aria-hidden="true"
          /><FileText v-else :size="20" class="shrink-0" aria-hidden="true" />
          <div class="min-w-0">
            <h3 class="font-semibold">{{ document.label }}</h3>
            <p class="muted text-sm">
              PDF · {{ document.required ? 'Obligatoire' : 'Facultatif' }} ·
              {{ document.uploaded ? `Ajouté — version ${document.version}` : 'À ajouter' }}
            </p>
          </div>
        </div>
        <p v-if="document.uploaded" class="document-filename mt-3">
          {{ document.filename }} · {{ size(document.sizeBytes) }}
        </p>
        <div class="document-actions mt-4">
          <a
            v-if="document.uploaded"
            class="secondary-button"
            :href="documentsApi.content(document.type)"
            target="_blank"
            rel="noopener noreferrer"
            :aria-label="`Voir ${document.label} (nouvel onglet)`"
            >Voir le document<span class="sr-only"> (nouvel onglet)</span></a
          >
          <div v-if="!readonly" class="file-picker">
            <label :for="`file-${document.type}`" class="sr-only"
              >{{ document.uploaded ? 'Remplacer' : 'Choisir un fichier pour' }}
              {{ document.label }}</label
            ><input
              :id="`file-${document.type}`"
              type="file"
              accept="application/pdf,.pdf"
              :disabled="!!pending || disabled"
              :aria-describedby="`error-${document.type}`"
              @change="choose($event, document)"
            />
          </div>
        </div>
        <p v-if="pending === document.type" role="status" class="mt-3">
          <Upload :size="16" class="inline" aria-hidden="true" /> Envoi en cours…
        </p>
        <p :id="`error-${document.type}`" role="alert" class="field-error">
          {{ errors[document.type] }}
        </p>
        <p v-if="document.uploaded && !readonly" class="muted text-sm mt-3">
          Pour remplacer ce document, choisissez un nouveau PDF. La version affichée reste
          disponible jusqu’à confirmation de l’envoi.
        </p>
      </li>
    </ul>
    <UiAlert
      v-if="!listing.complete && !readonly"
      title="Quatre documents sont nécessaires."
      class="mt-6"
      >Vous pourrez accéder au récapitulatif lorsque le serveur aura confirmé les quatre
      fichiers.</UiAlert
    >
  </div>
</template>
