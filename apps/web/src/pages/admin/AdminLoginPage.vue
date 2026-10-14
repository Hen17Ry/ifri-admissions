<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { LogIn } from 'lucide-vue-next'
import AdminAuthLayout from '@/layouts/AdminAuthLayout.vue'
import FormField from '@/components/ui/FormField.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiInput from '@/components/ui/UiInput.vue'
import PasswordInput from '@/features/admin/components/PasswordInput.vue'
import { adminApi, adminMessage } from '@/features/admin/api'
import { useAdminStore } from '@/features/admin/store'
import { asApiError } from '@/features/application/store'

const store = useAdminStore()
const route = useRoute()
const router = useRouter()
const email = ref('')
const password = ref('')
const errors = ref<Record<string, string>>({})
const message = ref('')
const pending = ref(false)
const notice = ref(store.notice)

async function submit() {
  if (pending.value) return
  errors.value = {}
  message.value = ''
  if (!email.value.trim()) errors.value.email = 'Saisissez votre adresse email.'
  if (!password.value) errors.value.password = 'Saisissez votre mot de passe.'
  if (Object.keys(errors.value).length) {
    await nextTick()
    document.getElementById(Object.keys(errors.value)[0]!)?.focus()
    return
  }
  pending.value = true
  try {
    const response = await adminApi.login(email.value.trim(), password.value)
    password.value = ''
    store.notice = ''
    await store.applyStage(response)
    const redirect =
      typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/admin')
        ? route.query.redirect
        : '/admin'
    await router.replace(
      response.stage === 'ACTIVE' ? redirect : { name: 'admin-mfa', query: { redirect } },
    )
  } catch (e) {
    // One generic message: it never says whether the account exists.
    message.value = adminMessage(asApiError(e))
    password.value = ''
    await nextTick()
    document.getElementById('login-error')?.focus()
  } finally {
    pending.value = false
  }
}
onMounted(() => {
  document.title = 'Connexion — Administration IFRI'
  store.notice = ''
  document.getElementById('login-title')?.focus()
})
</script>
<template>
  <AdminAuthLayout>
    <h1 id="login-title" tabindex="-1">Connexion à l’administration</h1>
    <p class="muted mt-2">Accès réservé au personnel de l’IFRI disposant d’un compte.</p>
    <UiAlert v-if="notice" :title="notice" class="mt-6" />
    <div v-if="message" id="login-error" tabindex="-1" class="mt-6">
      <UiAlert :title="message" error />
    </div>
    <form class="admin-form" novalidate @submit.prevent="submit">
      <FormField id="email" label="Adresse email" :error="errors.email"
        ><template #default="slot"
          ><UiInput
            id="email"
            v-model="email"
            type="email"
            autocomplete="username"
            inputmode="email"
            maxlength="254"
            :aria-describedby="slot.describedby"
            :aria-invalid="slot.invalid"
            required /></template
      ></FormField>
      <FormField id="password" label="Mot de passe" :error="errors.password"
        ><template #default="slot"
          ><PasswordInput
            id="password"
            v-model="password"
            autocomplete="current-password"
            :describedby="slot.describedby"
            :invalid="slot.invalid" /></template
      ></FormField>
      <UiButton type="submit" :disabled="pending" :aria-busy="pending"
        ><LogIn :size="18" aria-hidden="true" />{{
          pending ? 'Connexion…' : 'Se connecter'
        }}</UiButton
      >
    </form>
    <p class="muted mt-6 text-sm">
      Mot de passe oublié ou compte bloqué : contactez un super-administrateur de l’IFRI.
    </p>
  </AdminAuthLayout>
</template>
