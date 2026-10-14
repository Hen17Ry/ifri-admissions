<script setup lang="ts">
import { ref } from 'vue'
import { Eye, EyeOff } from 'lucide-vue-next'
defineProps<{ id: string; autocomplete: string; describedby?: string; invalid?: boolean }>()
const model = defineModel<string>({ required: true })
const visible = ref(false)
</script>
<template>
  <div class="password-input">
    <input
      :id="id"
      v-model="model"
      class="flow-input"
      :type="visible ? 'text' : 'password'"
      :autocomplete="autocomplete"
      :aria-describedby="describedby"
      :aria-invalid="invalid"
      maxlength="512"
      spellcheck="false"
      required
    />
    <button
      type="button"
      class="password-toggle"
      :aria-pressed="visible"
      :aria-controls="id"
      @click="visible = !visible"
    >
      <EyeOff v-if="visible" :size="18" aria-hidden="true" /><Eye
        v-else
        :size="18"
        aria-hidden="true"
      /><span class="sr-only">{{ visible ? 'Masquer' : 'Afficher' }} le mot de passe</span>
    </button>
  </div>
</template>
