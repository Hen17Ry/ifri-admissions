export class ApiError extends Error {
  constructor(
    public code: string,
    message: string,
    public fields: Record<string, string> = {},
    public status = 0,
  ) {
    super(message)
  }
}
export async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  let response: Response
  try {
    response = await fetch(`/api/v1${path}`, {
      ...options,
      credentials: 'include',
      headers: {
        ...(options.body instanceof FormData ? {} : { 'Content-Type': 'application/json' }),
        ...options.headers,
      },
      signal: options.signal ?? AbortSignal.timeout(8000),
    })
  } catch {
    throw new ApiError(
      'NETWORK_ERROR',
      'Connexion indisponible. Vos modifications ne sont pas encore enregistrées.',
    )
  }
  const body = await response.json().catch(() => null)
  if (!response.ok) {
    const error = body?.error ?? body
    throw new ApiError(
      error?.code ?? 'HTTP_ERROR',
      error?.message ?? 'Une erreur est survenue. Réessayez.',
      error?.fields ?? {},
      response.status,
    )
  }
  if (body === null)
    throw new ApiError('INVALID_RESPONSE', 'Le serveur a renvoyé une réponse inattendue.')
  return body as T
}
