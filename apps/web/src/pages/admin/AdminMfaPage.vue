<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowRight, Copy, LogOut, ShieldCheck } from 'lucide-vue-next'
import AdminAuthLayout from '@/layouts/AdminAuthLayout.vue'
import FormField from '@/components/ui/FormField.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiSkeleton from '@/components/ui/UiSkeleton.vue'
import RecoveryCodes from '@/features/admin/components/RecoveryCodes.vue'
import { adminApi, adminMessage, type Enrollment } from '@/features/admin/api'
import { useAdminStore } from '@/features/admin/store'
import { asApiError } from '@/features/application/store'

const store = useAdminStore()
const route = useRoute()
const router = useRouter()
const enrolling = computed(() => store.session?.stage === 'MFA_ENROLLMENT')
const enrollment = ref<Enrollment | null>(null)
const codes = ref<string[]>([])
const kept = ref(false)
const code = ref('')
const useRecovery = ref(false)
const error = ref('')
const message = ref('')
const pending = ref(false)
const copyStatus = ref('')
const qr = computed(() =>
  enrollment.value ? `data:image/svg+xml;base64,${btoa(enrollment.value.qrSvg)}` : '',
)
const groupedSecret = computed(() => enrollment.value?.secret.match(/.{1,4}/g)?.join(' ') ?? '')
const redirect = computed(() =>
  typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/admin')
    ? route.query.redirect
    : '/admin',
)

async function expired(e: unknown) {
  const failure = asApiError(e)
  if (failure.status !== 401) return false
  store.clear(adminMessage(failure))
  await router.replace({ name: 'admin-login' })
  return true
}
async function start() {
  pending.value = true
  message.value = ''
  try {
    enrollment.value = await adminApi.startEnrollment()
  } catch (e) {
    if (!(await expired(e))) message.value = adminMessage(asApiError(e))
  } finally {
    pending.value = false
  }
}
async function copySecret() {
  try {
    if (!navigator.clipboard || !enrollment.value) throw new Error('clipboard unavailable')
    await navigator.clipboard.writeText(enrollment.value.secret)
    copyStatus.value = 'Clé copiée.'
  } catch {
    copyStatus.value = 'Copie impossible : saisissez la clé à la main.'
  }
}
async function submit() {
  if (pending.value) return
  error.value = ''
  message.value = ''
  if (!code.value.trim()) {
    error.value = useRecovery.value
      ? 'Saisissez un code de secours.'
      : 'Saisissez le code à 6 chiffres.'
    document.getElementById('mfa-code')?.focus()
    return
  }
  pending.value = true
  try {
    if (enrolling.value) {
      const response = await adminApi.confirmEnrollment(code.value)
      enrollment.value = null
      codes.value = response.recoveryCodes ?? []
      await store.applyStage(response)
      await nextTick()
      document.getElementById('recovery-title')?.focus()
    } else {
      await store.applyStage(await adminApi.verify(code.value))
      await router.replace(redirect.value)
    }
  } catch (e) {
    if (await expired(e)) return
    error.value = adminMessage(asApiError(e))
    code.value = ''
    await nextTick()
    document.getElementById('mfa-code')?.focus()
  } finally {
    pending.value = false
  }
}
async function logout() {
  await store.logout()
  await router.replace({ name: 'admin-login' })
}
onMounted(async () => {
  document.title = 'Double authentification — Administration IFRI'
  if (enrolling.value) await start()
  await nextTick()
  document.getElementById('mfa-title')?.focus()
})
</script>
<template>
  <AdminAuthLayout>
    <template v-if="codes.length">
      <h1 id="mfa-title" tabindex="-1">Double authentification activée</h1>
      <RecoveryCodes :codes="codes" :email="store.session?.email ?? ''" class="mt-6" />
      <label class="admin-check mt-6">
        <input v-model="kept" type="checkbox" />
        J’ai conservé ces codes en lieu sûr.
      </label>
      <UiButton class="mt-6" :disabled="!kept" @click="router.replace(redirect)"
        >Accéder à l’administration<ArrowRight :size="18" aria-hidden="true"
      /></UiButton>
    </template>
    <template v-else>
      <h1 id="mfa-title" tabindex="-1">
        {{ enrolling ? 'Configurez la double authentification' : 'Vérification en deux étapes' }}
      </h1>
      <UiAlert v-if="message" :title="message" error class="mt-6"
        ><UiButton v-if="enrolling" variant="secondary" class="mt-4" @click="start"
          >Réessayer</UiButton
        ></UiAlert
      >
      <template v-if="enrolling">
        <ol class="admin-steps mt-4">
          <li>
            Installez une application d’authentification (par exemple FreeOTP, Aegis, Google
            Authenticator ou Microsoft Authenticator).
          </li>
          <li>Scannez le code QR, ou saisissez la clé à la main.</li>
          <li>Saisissez le code à 6 chiffres affiché par l’application.</li>
        </ol>
        <div v-if="pending && !enrollment" role="status" class="mt-6">
          <UiSkeleton /><span class="sr-only">Préparation de la clé…</span>
        </div>
        <div v-else-if="enrollment" class="mfa-enrollment">
          <img
            :src="qr"
            alt="Code QR de configuration de la double authentification"
            class="mfa-qr"
            width="200"
            height="200"
          />
          <div>
            <p class="text-sm muted">Clé à saisir à la main</p>
            <p class="mfa-secret">{{ groupedSecret }}</p>
            <UiButton variant="secondary" class="mt-2" @click="copySecret"
              ><Copy :size="18" aria-hidden="true" />Copier la clé</UiButton
            >
            <p class="copy-status" role="status" aria-live="polite">{{ copyStatus }}</p>
          </div>
        </div>
      </template>
      <p v-else class="muted mt-2">
        Saisissez le code affiché par votre application d’authentification.
      </p>
      <form class="admin-form" novalidate @submit.prevent="submit">
        <FormField
          id="mfa-code"
          :label="useRecovery ? 'Code de secours' : 'Code à 6 chiffres'"
          :hint="useRecovery ? 'Format XXXXX-XXXXX. Chaque code ne sert qu’une fois.' : ''"
          :error="error"
          ><template #default="slot"
            ><input
              id="mfa-code"
              v-model="code"
              class="flow-input mfa-code-input"
              :inputmode="useRecovery ? 'text' : 'numeric'"
              :autocomplete="useRecovery ? 'off' : 'one-time-code'"
              :maxlength="useRecovery ? 13 : 7"
              :aria-describedby="slot.describedby"
              :aria-invalid="slot.invalid"
              spellcheck="false"
              required /></template
        ></FormField>
        <UiButton
          type="submit"
          :disabled="pending || (enrolling && !enrollment)"
          :aria-busy="pending"
          ><ShieldCheck :size="18" aria-hidden="true" />{{
            pending ? 'Vérification…' : 'Valider'
          }}</UiButton
        >
      </form>
      <div class="admin-auth-links">
        <button
          v-if="!enrolling"
          type="button"
          class="link-button"
          @click="((useRecovery = !useRecovery), (code = ''), (error = ''))"
        >
          {{
            useRecovery
              ? 'Utiliser l’application d’authentification'
              : 'Utiliser un code de secours'
          }}
        </button>
        <button type="button" class="link-button" @click="logout">
          <LogOut :size="16" aria-hidden="true" />Se déconnecter
        </button>
      </div>
      <p v-if="!enrolling" class="muted mt-6 text-sm">
        Téléphone et codes de secours perdus : un super-administrateur peut réinitialiser votre
        double authentification.
      </p>
    </template>
  </AdminAuthLayout>
</template>
