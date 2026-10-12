<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { CircleCheck, RefreshCw } from 'lucide-vue-next'
import { getReadiness, type Readiness } from '@/lib/health'

const pending = ref(false)
const readiness = ref<Readiness | null>(null)
const message = ref('Vérification des services…')
const services = { postgres: 'PostgreSQL', redis: 'Redis', minio: 'MinIO' } as const

async function refresh() {
  if (pending.value) return
  pending.value = true
  readiness.value = null
  message.value = 'Vérification des services…'
  try {
    readiness.value = await getReadiness()
    message.value =
      readiness.value.status === 'ok'
        ? 'API et dépendances disponibles.'
        : 'API joignable ; certaines dépendances sont indisponibles.'
  } catch {
    message.value = 'API inaccessible. Vérifiez le démarrage des services Docker.'
  } finally {
    pending.value = false
  }
}
onMounted(refresh)
</script>

<template>
  <main id="main" class="mx-auto max-w-3xl px-4 py-12 md:px-6 md:py-16">
    <header class="mb-8">
      <p class="institution">IFRI <span>Université d’Abomey-Calavi</span></p>
      <h1 class="mt-6 text-2xl font-semibold md:text-3xl">Plateforme de gestion des admissions</h1>
    </header>
    <section class="panel p-6 md:p-8" aria-labelledby="environment-title">
      <div class="flex items-start gap-3">
        <CircleCheck class="frontend-icon" :size="24" aria-hidden="true" />
        <div>
          <h2 id="environment-title" class="text-lg font-semibold">Frontend opérationnel</h2>
          <p class="muted mt-2">Environnement de développement — bootstrap technique.</p>
        </div>
      </div>
      <p class="mt-6" role="status" aria-atomic="true">{{ message }}</p>
      <dl class="mt-4 grid gap-3" aria-label="Disponibilité des dépendances de l’API">
        <div v-for="(label, key) in services" :key="key" class="service-row">
          <dt>{{ label }}</dt>
          <dd :class="readiness?.checks[key] === 'ok' ? 'success' : 'muted'">
            {{
              pending
                ? 'Vérification…'
                : readiness
                  ? readiness.checks[key] === 'ok'
                    ? 'Disponible'
                    : 'Indisponible'
                  : 'Non vérifié'
            }}
          </dd>
        </div>
      </dl>
      <button class="primary-button mt-6" type="button" :disabled="pending" @click="refresh">
        <RefreshCw :size="18" aria-hidden="true" />
        {{ pending ? 'Vérification en cours…' : 'Vérifier les services' }}
      </button>
      <p class="muted mt-4 text-sm">
        Le worker se vérifie avec Docker Compose ; aucun traitement OCR n’est activé.
      </p>
    </section>
  </main>
</template>
