<script setup lang="ts">
import { reactive, watch } from 'vue'
import {
  globalRoles,
  roleDescriptions,
  roleLabels,
  type Grant,
  type GrantInput,
  type Program,
  type Role,
} from '@/features/admin/api'

const props = withDefaults(
  defineProps<{ programs: Program[]; initial?: Grant[]; error?: string; idPrefix?: string }>(),
  { initial: () => [], error: '', idPrefix: 'grant' },
)
const emit = defineEmits<{ change: [GrantInput[]] }>()
const roles = Object.keys(roleLabels) as Role[]
// One entry per role: selected, and for program-scoped roles "all" or a list of programs.
const state = reactive(
  Object.fromEntries(
    roles.map((role) => {
      const held = (props.initial ?? []).filter((g) => g.role === role)
      const scoped = held.filter((g) => g.program).map((g) => g.program!)
      return [
        role,
        { selected: held.length > 0, scope: scoped.length ? 'some' : 'all', programs: scoped },
      ]
    }),
  ) as Record<Role, { selected: boolean; scope: 'all' | 'some'; programs: string[] }>,
)
function grants(): GrantInput[] {
  return roles.flatMap((role) => {
    const s = state[role]
    if (!s.selected) return []
    if (globalRoles.includes(role) || s.scope === 'all') return [{ role, program: '' }]
    return s.programs.map((program) => ({ role, program }))
  })
}
watch(state, () => emit('change', grants()), { deep: true, immediate: true })
</script>
<template>
  <fieldset class="grants-editor" :aria-describedby="error ? `${idPrefix}-error` : undefined">
    <legend>Rôles</legend>
    <div v-for="role in roles" :key="role" class="grant-role">
      <label class="admin-check">
        <input
          :id="`${idPrefix}-role-${role}`"
          v-model="state[role].selected"
          type="checkbox"
          :name="`${idPrefix}-role-${role}`"
        />
        <span
          ><strong>{{ roleLabels[role] }}</strong
          ><span class="muted text-sm grant-description">{{ roleDescriptions[role] }}</span></span
        >
      </label>
      <fieldset v-if="state[role].selected && !globalRoles.includes(role)" class="grant-scope">
        <legend class="sr-only">Périmètre du rôle {{ roleLabels[role] }}</legend>
        <label class="admin-check"
          ><input
            v-model="state[role].scope"
            type="radio"
            value="all"
            :name="`${idPrefix}-scope-${role}`"
          />Toutes les filières</label
        >
        <label class="admin-check"
          ><input
            v-model="state[role].scope"
            type="radio"
            value="some"
            :name="`${idPrefix}-scope-${role}`"
          />Certaines filières</label
        >
        <div v-if="state[role].scope === 'some'" class="grant-programs">
          <label v-for="program in programs" :key="program.code" class="admin-check">
            <input v-model="state[role].programs" type="checkbox" :value="program.code" />{{
              program.name
            }}
            ({{ program.code }})
          </label>
        </div>
      </fieldset>
    </div>
    <p :id="`${idPrefix}-error`" class="field-error">{{ error }}</p>
  </fieldset>
</template>
