<script setup lang="ts">
import { ref, watch } from 'vue'
import {
  roleDescriptions,
  roleLabels,
  type Grant,
  type GrantInput,
  type Role,
} from '@/features/admin/api'

// One role per account (ADR 110): a radio group, never a combination. A person who needs two
// roles has two accounts. Every role applies to all programs: no scope is offered.
const props = withDefaults(
  defineProps<{ initial?: Grant[]; error?: string; idPrefix?: string }>(),
  {
    initial: () => [],
    error: '',
    idPrefix: 'role',
  },
)
const emit = defineEmits<{ change: [GrantInput[]] }>()
const roles = Object.keys(roleLabels) as Role[]
const selected = ref<Role | ''>(
  roles.find((role) => props.initial.some((grant) => grant.role === role)) ?? '',
)
watch(selected, (role) => emit('change', role ? [{ role, program: '' }] : []), {
  immediate: true,
})
</script>
<template>
  <fieldset
    class="grants-editor"
    :aria-describedby="error ? `${idPrefix}-error` : `${idPrefix}-hint`"
  >
    <legend>Rôle</legend>
    <p :id="`${idPrefix}-hint`" class="muted text-sm">
      Un seul rôle par compte. Une personne qui a besoin de deux rôles reçoit deux comptes.
    </p>
    <div v-for="role in roles" :key="role" class="grant-role">
      <label class="admin-check">
        <input
          :id="`${idPrefix}-role-${role}`"
          v-model="selected"
          type="radio"
          :value="role"
          :name="`${idPrefix}-role`"
        />
        <span
          ><strong>{{ roleLabels[role] }}</strong
          ><span class="muted text-sm grant-description">{{ roleDescriptions[role] }}</span></span
        >
      </label>
    </div>
    <p :id="`${idPrefix}-error`" class="field-error">{{ error }}</p>
  </fieldset>
</template>
