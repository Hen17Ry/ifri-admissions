<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Copy, Link2, Plus, ShieldOff, UserCheck, UserCog, UserX } from 'lucide-vue-next'
import AdminLayout from '@/layouts/AdminLayout.vue'
import FormField from '@/components/ui/FormField.vue'
import UiAlert from '@/components/ui/UiAlert.vue'
import UiButton from '@/components/ui/UiButton.vue'
import UiDialog from '@/components/ui/UiDialog.vue'
import UiInput from '@/components/ui/UiInput.vue'
import UiSkeleton from '@/components/ui/UiSkeleton.vue'
import GrantsEditor from '@/features/admin/components/GrantsEditor.vue'
import {
  adminApi,
  adminMessage,
  roleLabels,
  statusLabels,
  type Account,
  type Activation,
  type GrantInput,
  type Program,
} from '@/features/admin/api'
import { useAdminStore } from '@/features/admin/store'
import { asApiError } from '@/features/application/store'

type Action = 'disable' | 'enable' | 'reset' | 'link'
const store = useAdminStore()
const router = useRouter()
const accounts = ref<Account[]>([])
const programs = ref<Program[]>([])
const loading = ref(true)
const loadError = ref('')
const status = ref('')
const manage = computed(() => store.can('accounts.manage'))
// Create form
const creating = ref(false)
const email = ref('')
const displayName = ref('')
const grants = ref<GrantInput[]>([])
const formErrors = ref<Record<string, string>>({})
const formMessage = ref('')
// Grants editing, confirmations and the one-time activation link
const editing = ref<Account | null>(null)
const editGrants = ref<GrantInput[]>([])
const confirm = ref<{ action: Action; account: Account } | null>(null)
const activation = ref<Activation | null>(null)
const copyStatus = ref('')
const pending = ref(false)
const actionMessage = ref('')
const date = (value: string | null) =>
  value
    ? new Intl.DateTimeFormat('fr-FR', {
        dateStyle: 'medium',
        timeStyle: 'short',
        timeZone: 'Africa/Porto-Novo',
      }).format(new Date(value))
    : '—'
const confirmTexts: Record<
  Action,
  { title: string; text: string; button: string; danger: boolean }
> = {
  disable: {
    title: 'Désactiver ce compte ?',
    text: 'Ses sessions sont fermées immédiatement et il ne pourra plus se connecter. Le compte et son historique sont conservés.',
    button: 'Désactiver',
    danger: true,
  },
  enable: {
    title: 'Réactiver ce compte ?',
    text: 'La personne pourra de nouveau se connecter.',
    button: 'Réactiver',
    danger: false,
  },
  reset: {
    title: 'Réinitialiser la double authentification ?',
    text: 'Son application et ses codes de secours ne fonctionneront plus ; un nouvel enrôlement lui sera demandé à la prochaine connexion. Vérifiez d’abord son identité par un autre canal.',
    button: 'Réinitialiser',
    danger: true,
  },
  link: {
    title: 'Émettre un nouveau lien d’activation ?',
    text: 'Le lien précédent cessera de fonctionner.',
    button: 'Émettre le lien',
    danger: false,
  },
}

async function expired(e: unknown) {
  const failure = asApiError(e)
  if (failure.status !== 401) return false
  store.clear(adminMessage(failure))
  await router.replace({ name: 'admin-login' })
  return true
}
async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const [list, offered] = await Promise.all([adminApi.accounts(), adminApi.programs()])
    accounts.value = list.accounts
    programs.value = offered.programs
  } catch (e) {
    if (!(await expired(e))) loadError.value = adminMessage(asApiError(e))
  } finally {
    loading.value = false
  }
}
function replace(account: Account) {
  accounts.value = accounts.value.map((a) => (a.id === account.id ? account : a))
}
function openCreate() {
  email.value = ''
  displayName.value = ''
  formErrors.value = {}
  formMessage.value = ''
  creating.value = true
}
async function create() {
  formErrors.value = {}
  formMessage.value = ''
  if (!email.value.trim()) formErrors.value.email = 'Saisissez l’adresse email professionnelle.'
  if (!displayName.value.trim()) formErrors.value.displayName = 'Saisissez le nom affiché.'
  if (!grants.value.length)
    formErrors.value.grants =
      'Attribuez au moins un rôle (et une filière si le périmètre est limité).'
  if (Object.keys(formErrors.value).length) {
    await nextTick()
    document
      .getElementById(
        formErrors.value.email
          ? 'new-email'
          : formErrors.value.displayName
            ? 'new-name'
            : 'create-role-SUPER_ADMIN',
      )
      ?.focus()
    return
  }
  pending.value = true
  try {
    const result = await adminApi.createAccount({
      email: email.value.trim(),
      displayName: displayName.value.trim(),
      grants: grants.value,
    })
    accounts.value = [...accounts.value, result.account]
    creating.value = false
    activation.value = result
    status.value = `Compte créé pour ${result.account.displayName}.`
  } catch (e) {
    if (await expired(e)) return
    const failure = asApiError(e)
    formErrors.value = { ...failure.fields }
    if (!Object.keys(failure.fields).length) formMessage.value = adminMessage(failure)
  } finally {
    pending.value = false
  }
}
function openEdit(account: Account) {
  actionMessage.value = ''
  editing.value = account
}
async function saveGrants() {
  if (!editing.value) return
  if (!editGrants.value.length) {
    actionMessage.value = 'Attribuez au moins un rôle.'
    return
  }
  pending.value = true
  try {
    const account = await adminApi.setGrants(editing.value.id, editGrants.value)
    replace(account)
    editing.value = null
    status.value = `Rôles de ${account.displayName} enregistrés ; ses sessions ont été fermées.`
    if (account.email === store.session?.email) {
      store.clear('Vos rôles ont changé : reconnectez-vous.')
      await router.replace({ name: 'admin-login' })
    }
  } catch (e) {
    if (await expired(e)) return
    actionMessage.value = adminMessage(asApiError(e))
  } finally {
    pending.value = false
  }
}
async function run() {
  if (!confirm.value) return
  const { action, account } = confirm.value
  pending.value = true
  actionMessage.value = ''
  try {
    if (action === 'link') {
      activation.value = await adminApi.reissueActivation(account.id)
      replace(activation.value.account)
    } else if (action === 'reset') {
      replace(await adminApi.resetMfa(account.id))
      status.value = `Double authentification de ${account.displayName} réinitialisée.`
    } else {
      replace(await adminApi.setStatus(account.id, action === 'enable'))
      status.value = `Compte de ${account.displayName} ${action === 'enable' ? 'réactivé' : 'désactivé'}.`
    }
    confirm.value = null
  } catch (e) {
    if (await expired(e)) return
    actionMessage.value = adminMessage(asApiError(e))
  } finally {
    pending.value = false
  }
}
async function copyLink() {
  try {
    if (!navigator.clipboard || !activation.value) throw new Error('clipboard unavailable')
    await navigator.clipboard.writeText(activation.value.activationUrl)
    copyStatus.value = 'Lien copié.'
  } catch {
    copyStatus.value = 'Copie impossible : sélectionnez le lien pour le copier.'
  }
}
onMounted(async () => {
  document.title = 'Comptes — Administration IFRI'
  document.getElementById('page-title')?.focus()
  await load()
})
</script>
<template>
  <AdminLayout>
    <div class="admin-page-heading">
      <div>
        <h1 id="page-title" tabindex="-1">Comptes administrateurs</h1>
        <p class="muted mt-2">
          Aucun compte n’est supprimé : un compte désactivé reste dans l’historique.
        </p>
      </div>
      <UiButton v-if="manage" @click="openCreate"
        ><Plus :size="18" aria-hidden="true" />Créer un compte</UiButton
      >
    </div>
    <p class="sr-only" role="status" aria-live="polite" aria-atomic="true">{{ status }}</p>
    <UiAlert v-if="status" :title="status" class="mt-6" />
    <div v-if="loading" role="status" class="mt-6">
      <UiSkeleton /><span class="sr-only">Chargement des comptes…</span>
    </div>
    <UiAlert v-else-if="loadError" :title="loadError" error class="mt-6"
      ><UiButton variant="secondary" class="mt-4" @click="load">Réessayer</UiButton></UiAlert
    >
    <div v-else class="admin-table-wrap">
      <table class="admin-table">
        <caption class="sr-only">
          Comptes administrateurs
        </caption>
        <thead>
          <tr>
            <th scope="col">Compte</th>
            <th scope="col">Rôles</th>
            <th scope="col">Statut</th>
            <th scope="col">Double authentification</th>
            <th scope="col">Dernière connexion</th>
            <th v-if="manage" scope="col"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="account in accounts" :key="account.id">
            <th scope="row" data-label="Compte">
              <span class="admin-cell-name">{{ account.displayName }}</span>
              <span class="muted text-sm admin-cell-email">{{ account.email }}</span>
            </th>
            <td data-label="Rôles">
              <ul class="admin-chips">
                <li v-for="grant in account.grants" :key="grant.role + (grant.program ?? '')">
                  {{ roleLabels[grant.role] }}{{ grant.program ? ` · ${grant.program}` : '' }}
                </li>
              </ul>
            </td>
            <td data-label="Statut">
              <span class="admin-status" :class="`admin-status-${account.status.toLowerCase()}`">{{
                statusLabels[account.status]
              }}</span>
              <span
                v-if="account.status === 'PENDING_ACTIVATION' && account.activationExpiresAt"
                class="muted text-sm admin-cell-email"
                >Lien valable jusqu’au {{ date(account.activationExpiresAt) }}</span
              >
            </td>
            <td data-label="Double authentification">
              {{ account.mfaEnabled ? 'Activée' : 'Non configurée' }}
            </td>
            <td data-label="Dernière connexion">{{ date(account.lastLoginAt) }}</td>
            <td v-if="manage" data-label="Actions">
              <div class="admin-row-actions">
                <UiButton
                  variant="secondary"
                  :aria-label="`Rôles et périmètre de ${account.displayName}`"
                  @click="openEdit(account)"
                  ><UserCog :size="18" aria-hidden="true" />Rôles</UiButton
                >
                <UiButton
                  v-if="account.status === 'PENDING_ACTIVATION'"
                  variant="secondary"
                  :aria-label="`Nouveau lien d’activation pour ${account.displayName}`"
                  @click="confirm = { action: 'link', account }"
                  ><Link2 :size="18" aria-hidden="true" />Lien</UiButton
                >
                <UiButton
                  v-if="account.mfaEnabled"
                  variant="secondary"
                  :aria-label="`Réinitialiser la double authentification de ${account.displayName}`"
                  @click="confirm = { action: 'reset', account }"
                  ><ShieldOff :size="18" aria-hidden="true" />MFA</UiButton
                >
                <UiButton
                  v-if="account.status === 'DISABLED'"
                  variant="secondary"
                  :aria-label="`Réactiver ${account.displayName}`"
                  @click="confirm = { action: 'enable', account }"
                  ><UserCheck :size="18" aria-hidden="true" />Réactiver</UiButton
                >
                <UiButton
                  v-else-if="account.email !== store.session?.email"
                  variant="danger"
                  :aria-label="`Désactiver ${account.displayName}`"
                  @click="confirm = { action: 'disable', account }"
                  ><UserX :size="18" aria-hidden="true" />Désactiver</UiButton
                >
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <UiDialog
      v-if="manage"
      :open="creating"
      title="Créer un compte administrateur"
      :pending="pending"
      @close="creating = false"
    >
      <p class="muted mt-2">Un lien d’activation à usage unique sera affiché une seule fois.</p>
      <UiAlert v-if="formMessage" :title="formMessage" error class="mt-4" />
      <form id="create-account" class="admin-form" novalidate @submit.prevent="create">
        <FormField id="new-email" label="Adresse email" :error="formErrors.email"
          ><template #default="slot"
            ><UiInput
              id="new-email"
              v-model="email"
              type="email"
              autocomplete="off"
              maxlength="254"
              :aria-describedby="slot.describedby"
              :aria-invalid="slot.invalid" /></template
        ></FormField>
        <FormField id="new-name" label="Nom affiché" :error="formErrors.displayName"
          ><template #default="slot"
            ><UiInput
              id="new-name"
              v-model="displayName"
              autocomplete="off"
              maxlength="150"
              :aria-describedby="slot.describedby"
              :aria-invalid="slot.invalid" /></template
        ></FormField>
        <GrantsEditor
          id-prefix="create"
          :programs="programs"
          :error="formErrors.grants"
          @change="grants = $event"
        />
      </form>
      <template #actions>
        <UiButton variant="secondary" :disabled="pending" @click="creating = false"
          >Annuler</UiButton
        >
        <UiButton type="submit" form="create-account" :disabled="pending" :aria-busy="pending">{{
          pending ? 'Création…' : 'Créer le compte'
        }}</UiButton>
      </template>
    </UiDialog>

    <UiDialog
      v-if="manage"
      :open="!!editing"
      :title="`Rôles et périmètre — ${editing?.displayName ?? ''}`"
      :pending="pending"
      @close="editing = null"
    >
      <p class="muted mt-2">Tout changement ferme les sessions ouvertes de ce compte.</p>
      <UiAlert v-if="actionMessage" :title="actionMessage" error class="mt-4" />
      <GrantsEditor
        v-if="editing"
        :key="editing.id"
        id-prefix="edit"
        :programs="programs"
        :initial="editing.grants"
        class="mt-4"
        @change="editGrants = $event"
      />
      <template #actions>
        <UiButton variant="secondary" :disabled="pending" @click="editing = null">Annuler</UiButton>
        <UiButton :disabled="pending" @click="saveGrants">Enregistrer</UiButton>
      </template>
    </UiDialog>

    <UiDialog
      v-if="manage"
      :open="!!confirm"
      :title="confirm ? confirmTexts[confirm.action].title : ''"
      alert
      :pending="pending"
      @close="confirm = null"
    >
      <p class="mt-3">
        <strong>{{ confirm?.account.displayName }}</strong> ({{ confirm?.account.email }})
      </p>
      <p class="mt-2">{{ confirm ? confirmTexts[confirm.action].text : '' }}</p>
      <UiAlert v-if="actionMessage" :title="actionMessage" error class="mt-4" />
      <template #actions>
        <UiButton variant="secondary" data-initial-focus :disabled="pending" @click="confirm = null"
          >Annuler</UiButton
        >
        <UiButton
          :variant="confirm && confirmTexts[confirm.action].danger ? 'danger' : 'primary'"
          :disabled="pending"
          @click="run"
          >{{ confirm ? confirmTexts[confirm.action].button : '' }}</UiButton
        >
      </template>
    </UiDialog>

    <UiDialog
      v-if="manage"
      :open="!!activation"
      title="Lien d’activation"
      alert
      @close="((activation = null), (copyStatus = ''))"
    >
      <p class="mt-3">
        Transmettez ce lien à <strong>{{ activation?.account.displayName }}</strong> par un canal
        sûr. Il ne sera plus affiché, sert une seule fois et expire le
        {{ activation ? date(activation.expiresAt) : '' }}.
      </p>
      <label for="activation-url" class="mt-4 block font-semibold">Lien</label>
      <input
        id="activation-url"
        class="flow-input admin-link-input"
        :value="activation?.activationUrl"
        readonly
        @focus="($event.target as HTMLInputElement).select()"
      />
      <p class="copy-status" role="status" aria-live="polite">{{ copyStatus }}</p>
      <template #actions>
        <UiButton variant="secondary" @click="((activation = null), (copyStatus = ''))"
          >Fermer</UiButton
        >
        <UiButton data-initial-focus @click="copyLink"
          ><Copy :size="18" aria-hidden="true" />Copier le lien</UiButton
        >
      </template>
    </UiDialog>
  </AdminLayout>
</template>
