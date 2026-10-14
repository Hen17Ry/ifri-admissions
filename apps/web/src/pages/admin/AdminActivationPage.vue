<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { CircleCheck, KeyRound } from 'lucide-vue-next'
import AdminAuthLayout from '@/layouts/AdminAuthLayout.vue'
import FormField from '@/components/ui/FormField.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiSkeleton from '@/components/ui/UiSkeleton.vue'
import PasswordInput from '@/features/admin/components/PasswordInput.vue'
import { adminApi, adminMessage } from '@/features/admin/api'
import { asApiError } from '@/features/application/store'

// The token arrives in the URL fragment (never sent to a server). It is read once, kept in
// memory and erased from the address bar and history before anything else happens.
const token = window.location.hash.slice(1)
if (window.location.hash) {
  window.history.replaceState(
    window.history.state,
    '',
    window.location.pathname + window.location.search,
  )
}
const loading = ref(true)
const invalid = ref('')
const account = ref<{ email: string; displayName: string } | null>(null)
const password = ref('')
const confirmation = ref('')
const errors = ref<Record<string, string>>({})
const message = ref('')
const pending = ref(false)
const done = ref(false)

async function submit() {
  if (pending.value) return
  errors.value = {}
  message.value = ''
  if ([...password.value].length < 12) errors.value.password = 'Choisissez au moins 12 caractères.'
  else if (password.value !== confirmation.value)
    errors.value.confirmation = 'Les deux saisies sont différentes.'
  if (Object.keys(errors.value).length) {
    await nextTick()
    document.getElementById(Object.keys(errors.value)[0]!)?.focus()
    return
  }
  pending.value = true
  try {
    await adminApi.activate(token, password.value)
    done.value = true
    password.value = confirmation.value = ''
    await nextTick()
    document.getElementById('activation-title')?.focus()
  } catch (e) {
    const error = asApiError(e)
    if (error.fields.password) errors.value = { password: error.fields.password }
    else message.value = adminMessage(error)
    if (error.code === 'ADMIN_ACTIVATION_INVALID') invalid.value = message.value
    await nextTick()
    document.getElementById(errors.value.password ? 'password' : 'activation-title')?.focus()
  } finally {
    pending.value = false
  }
}
onMounted(async () => {
  document.title = 'Activation du compte — Administration IFRI'
  try {
    if (!token) throw new Error('missing token')
    account.value = await adminApi.inspectActivation(token)
  } catch (e) {
    invalid.value = token ? adminMessage(asApiError(e)) : 'Ce lien d’activation est incomplet.'
  } finally {
    loading.value = false
    await nextTick()
    document.getElementById('activation-title')?.focus()
  }
})
</script>
<template>
  <AdminAuthLayout>
    <h1 id="activation-title" tabindex="-1">
      {{ done ? 'Compte activé' : 'Activation de votre compte' }}
    </h1>
    <div v-if="loading" role="status" class="mt-6">
      <UiSkeleton /><span class="sr-only">Vérification du lien d’activation…</span>
    </div>
    <template v-else-if="done">
      <p class="receipt-status mt-4">
        <CircleCheck :size="24" aria-hidden="true" />Mot de passe enregistré
      </p>
      <p class="mt-4">
        Connectez-vous maintenant. La double authentification vous sera demandée : préparez une
        application d’authentification sur votre téléphone.
      </p>
      <RouterLink class="primary-button mt-6" :to="{ name: 'admin-login' }"
        >Se connecter</RouterLink
      >
    </template>
    <UiAlert v-else-if="invalid" :title="invalid" error class="mt-6" />
    <template v-else-if="account">
      <p class="mt-2">
        Bienvenue {{ account.displayName }} ({{ account.email }}). Choisissez votre mot de passe.
      </p>
      <UiAlert v-if="message" :title="message" error class="mt-6" />
      <form class="admin-form" novalidate @submit.prevent="submit">
        <input
          type="email"
          :value="account.email"
          autocomplete="username"
          class="sr-only"
          tabindex="-1"
          aria-hidden="true"
          readonly
        />
        <FormField
          id="password"
          label="Nouveau mot de passe"
          hint="12 caractères au moins. Une phrase de plusieurs mots est sûre et facile à retenir. Les mots de passe très courants sont refusés."
          :error="errors.password"
          ><template #default="slot"
            ><PasswordInput
              id="password"
              v-model="password"
              autocomplete="new-password"
              :describedby="slot.describedby"
              :invalid="slot.invalid" /></template
        ></FormField>
        <FormField id="confirmation" label="Confirmez le mot de passe" :error="errors.confirmation"
          ><template #default="slot"
            ><PasswordInput
              id="confirmation"
              v-model="confirmation"
              autocomplete="new-password"
              :describedby="slot.describedby"
              :invalid="slot.invalid" /></template
        ></FormField>
        <UiButton type="submit" :disabled="pending" :aria-busy="pending"
          ><KeyRound :size="18" aria-hidden="true" />{{
            pending ? 'Activation…' : 'Activer mon compte'
          }}</UiButton
        >
      </form>
    </template>
  </AdminAuthLayout>
</template>
