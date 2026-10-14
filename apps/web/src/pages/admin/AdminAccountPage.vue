<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { KeyRound, RefreshCw } from 'lucide-vue-next'
import AdminLayout from '@/layouts/AdminLayout.vue'
import FormField from '@/components/ui/FormField.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiDialog from '@/components/ui/UiDialog.vue'
import PasswordInput from '@/features/admin/components/PasswordInput.vue'
import RecoveryCodes from '@/features/admin/components/RecoveryCodes.vue'
import { adminApi, adminMessage } from '@/features/admin/api'
import { useAdminStore } from '@/features/admin/store'
import { asApiError } from '@/features/application/store'

const store = useAdminStore()
const router = useRouter()
const current = ref('')
const next = ref('')
const confirmation = ref('')
const errors = ref<Record<string, string>>({})
const message = ref('')
const pending = ref(false)
const confirmCodes = ref(false)
const codes = ref<string[]>([])
const codesMessage = ref('')

async function expired(e: unknown) {
  const failure = asApiError(e)
  if (failure.status !== 401) return false
  store.clear(adminMessage(failure))
  await router.replace({ name: 'admin-login' })
  return true
}
async function changePassword() {
  if (pending.value) return
  errors.value = {}
  message.value = ''
  if (!current.value) errors.value.currentPassword = 'Saisissez votre mot de passe actuel.'
  if ([...next.value].length < 12) errors.value.newPassword = 'Choisissez au moins 12 caractères.'
  else if (next.value !== confirmation.value)
    errors.value.confirmation = 'Les deux saisies sont différentes.'
  if (Object.keys(errors.value).length) {
    await nextTick()
    document.getElementById(Object.keys(errors.value)[0]!)?.focus()
    return
  }
  pending.value = true
  try {
    await adminApi.changePassword(current.value, next.value)
    // Every session has been revoked, this one included.
    store.clear('Mot de passe modifié. Connectez-vous avec votre nouveau mot de passe.')
    await router.replace({ name: 'admin-login' })
  } catch (e) {
    if (await expired(e)) return
    const failure = asApiError(e)
    if (Object.keys(failure.fields).length) errors.value = { ...failure.fields }
    else message.value = adminMessage(failure)
    await nextTick()
    document.getElementById(Object.keys(errors.value)[0] ?? 'password-title')?.focus()
  } finally {
    pending.value = false
    current.value = ''
  }
}
async function regenerate() {
  pending.value = true
  codesMessage.value = ''
  try {
    codes.value = (await adminApi.regenerateRecoveryCodes()).recoveryCodes
    confirmCodes.value = false
  } catch (e) {
    if (await expired(e)) return
    confirmCodes.value = false
    codesMessage.value = adminMessage(asApiError(e))
  } finally {
    pending.value = false
  }
}
onMounted(() => {
  document.title = 'Mon compte — Administration IFRI'
  document.getElementById('page-title')?.focus()
})
</script>
<template>
  <AdminLayout>
    <h1 id="page-title" tabindex="-1">Mon compte</h1>
    <p class="muted mt-2">{{ store.session?.displayName }} — {{ store.session?.email }}</p>
    <section class="admin-section" aria-labelledby="password-title">
      <h2 id="password-title" tabindex="-1">Changer mon mot de passe</h2>
      <p class="muted mt-2">
        Après le changement, toutes vos sessions sont fermées : reconnectez-vous avec le nouveau mot
        de passe.
      </p>
      <UiAlert v-if="message" :title="message" error class="mt-4" />
      <form class="admin-form" novalidate @submit.prevent="changePassword">
        <input
          type="email"
          :value="store.session?.email"
          autocomplete="username"
          class="sr-only"
          tabindex="-1"
          aria-hidden="true"
          readonly
        />
        <FormField id="currentPassword" label="Mot de passe actuel" :error="errors.currentPassword"
          ><template #default="slot"
            ><PasswordInput
              id="currentPassword"
              v-model="current"
              autocomplete="current-password"
              :describedby="slot.describedby"
              :invalid="slot.invalid" /></template
        ></FormField>
        <FormField
          id="newPassword"
          label="Nouveau mot de passe"
          hint="12 caractères au moins ; les mots de passe très courants sont refusés."
          :error="errors.newPassword"
          ><template #default="slot"
            ><PasswordInput
              id="newPassword"
              v-model="next"
              autocomplete="new-password"
              :describedby="slot.describedby"
              :invalid="slot.invalid" /></template
        ></FormField>
        <FormField
          id="confirmation"
          label="Confirmez le nouveau mot de passe"
          :error="errors.confirmation"
          ><template #default="slot"
            ><PasswordInput
              id="confirmation"
              v-model="confirmation"
              autocomplete="new-password"
              :describedby="slot.describedby"
              :invalid="slot.invalid" /></template
        ></FormField>
        <UiButton type="submit" :disabled="pending" :aria-busy="pending"
          ><KeyRound :size="18" aria-hidden="true" />Changer le mot de passe</UiButton
        >
      </form>
    </section>
    <section class="admin-section" aria-labelledby="codes-title">
      <h2 id="codes-title">Codes de secours</h2>
      <p class="muted mt-2">
        Générez une nouvelle série si vous avez utilisé ou perdu vos codes. Les anciens codes ne
        fonctionneront plus.
      </p>
      <UiAlert v-if="codesMessage" :title="codesMessage" error class="mt-4" />
      <RecoveryCodes
        v-if="codes.length"
        :codes="codes"
        :email="store.session?.email ?? ''"
        class="mt-4"
      />
      <UiButton v-else variant="secondary" class="mt-4" @click="confirmCodes = true"
        ><RefreshCw :size="18" aria-hidden="true" />Générer de nouveaux codes</UiButton
      >
    </section>
    <UiDialog
      :open="confirmCodes"
      title="Générer de nouveaux codes ?"
      alert
      :pending="pending"
      @close="confirmCodes = false"
    >
      <p class="mt-3">Vos codes de secours actuels seront immédiatement invalidés.</p>
      <template #actions>
        <UiButton
          variant="secondary"
          data-initial-focus
          :disabled="pending"
          @click="confirmCodes = false"
          >Annuler</UiButton
        >
        <UiButton :disabled="pending" @click="regenerate">Générer</UiButton>
      </template>
    </UiDialog>
  </AdminLayout>
</template>
