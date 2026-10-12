export type Readiness = {
  status: 'ok' | 'unavailable'
  checks: {
    postgres: 'ok' | 'unavailable'
    redis: 'ok' | 'unavailable'
    minio: 'ok' | 'unavailable'
  }
}

export async function getReadiness(): Promise<Readiness> {
  const response = await fetch('/api/ready', { signal: AbortSignal.timeout(5000) })
  if (response.status !== 200 && response.status !== 503) {
    throw new Error('API inaccessible')
  }
  const value: unknown = await response.json()
  if (!value || typeof value !== 'object' || !('checks' in value) || !('status' in value)) {
    throw new Error('Réponse technique invalide')
  }
  const { status, checks } = value
  if (status !== 'ok' && status !== 'unavailable') throw new Error('Statut invalide')
  if (!checks || typeof checks !== 'object') throw new Error('Contrôles invalides')
  for (const key of ['postgres', 'redis', 'minio']) {
    const state = (checks as Record<string, unknown>)[key]
    if (state !== 'ok' && state !== 'unavailable') throw new Error('Contrôle invalide')
  }
  return value as Readiness
}
