import { test, expect, type Page } from '@playwright/test'
import {
  activate,
  checkpoint,
  cli,
  createAccount,
  enrol,
  login,
  project,
  superAdmin,
} from './support'

// Back-office of submitted applications on the disposable stack of `make e2e`: about 5,000
// synthetic dossiers (infra/scripts/e2e.sh) plus the real journey of documents.spec.ts, whose
// dossier carries real PDFs and OCR coordinates (Playwright runs the files in order, 1 worker).
const candidateEmail = 'browser-phase4@example.invalid'

// Server time of one list request, as the browser measured it (request start → response end).
async function timedList(page: Page, query: string) {
  const waiting = page.waitForResponse(
    (r) => new URL(r.url()).pathname === '/api/v1/admin/applications',
  )
  await page.goto(`/admin/candidatures${query}`)
  const response = await waiting
  await response.finished()
  expect(response.status()).toBe(200)
  const body = (await response.json()) as { total: number }
  return { ms: Math.round(response.request().timing().responseEnd), total: body.total }
}

test('review: two agents and the deputy director, PDF preview and OCR proof', async ({
  browser,
}) => {
  test.skip(!project, 'Run through `make e2e`: the scenario drives the CLI of the isolated stack.')
  test.setTimeout(420000)

  // The super-administrator of admin.spec.ts exists: its second factor is reset by the CLI.
  try {
    cli(
      ['admin', 'create-super-admin', '--email', superAdmin.email, '--name', 'Super Admin E2E'],
      `${superAdmin.password}\n`,
    )
  } catch {
    cli(['admin', 'reset-mfa', '--email', superAdmin.email])
  }
  const superContext = await browser.newContext()
  const superPage = await superContext.newPage()
  await login(superPage, superAdmin.email, superAdmin.password)
  await enrol(superPage)
  await expect(superPage.getByTestId('coverage-warning')).toContainText(
    'Aucun directeur adjoint actif',
  )

  // One role per account: two agents of the secretariat and the deputy director.
  const password = 'une phrase de passe pour la revue'
  const people = [
    { email: 'agent.a.e2e@ifri.example', name: 'Agent A', role: /Agent du secrétariat exécutif/ },
    { email: 'agent.b.e2e@ifri.example', name: 'Agent B', role: /Agent du secrétariat exécutif/ },
    { email: 'da.e2e@ifri.example', name: 'Directeur adjoint E2E', role: /Directeur adjoint/ },
  ]
  const links: string[] = []
  for (const person of people)
    links.push(await createAccount(superPage, person.email, person.name, person.role))
  const [a, b, da] = await Promise.all(
    people.map((person, i) => activate(browser, links[i]!, person.email, password)),
  )
  await superPage.goto('/admin')
  await expect(superPage.getByTestId('coverage-warning')).not.toContainText(
    'Aucun directeur adjoint actif',
  )
  await expect(superPage.getByTestId('coverage-warning')).toContainText('Aucun directeur actif')
  expect((await superContext.request.get('/api/v1/admin/applications')).status()).toBe(403)
  await superContext.close()

  // List on about 5,000 dossiers: server pagination, sorts, accent-insensitive search, filters.
  const timings: Record<string, { ms: number; total: number }> = {}
  for (const [name, query] of [
    ['date', ''],
    ['average', '?sort=average:desc'],
    ['name-page-100', '?sort=name:asc&page=100'],
    ['accent-search', '?q=gbedji'],
    ['number-prefix', `?q=IFRI-${new Date().getFullYear()}-GL`],
    ['program-status', '?program=GL&status=TO_REVIEW'],
    ['any-flag', '?flag=ANY'],
  ] as const) {
    timings[name] = await timedList(a!.page, query)
  }
  console.log(`Back-office list timings (ms, browser → API → browser): ${JSON.stringify(timings)}`)
  test.info().annotations.push({ type: 'list-timings', description: JSON.stringify(timings) })
  expect(timings.date!.total).toBeGreaterThanOrEqual(5000)
  expect(timings['accent-search']!.total).toBeGreaterThan(0)
  for (const timing of Object.values(timings)) expect(timing.ms).toBeLessThan(3000)
  await expect(a!.page.getByRole('heading', { name: /dossiers?$/ })).toBeVisible()
  await expect(a!.page.locator('tbody tr').first()).toBeVisible()
  await checkpoint(a!.page, 'review-list')

  // Agent A finds the real dossier by email and claims it.
  await a!.page.goto(`/admin/candidatures?q=${encodeURIComponent(candidateEmail)}`)
  const numberLink = a!.page.getByRole('link', { name: /^IFRI-\d{4}-GL-\d{6}$/ })
  await expect(numberLink).toHaveCount(1)
  await numberLink.click()
  await expect(a!.page.getByRole('heading', { name: /^Dossier IFRI-/ })).toBeVisible()
  const dossierUrl = a!.page.url()
  const id = new URL(dossierUrl).pathname.split('/').pop()!
  await expect(a!.page.getByText(candidateEmail)).toBeVisible()
  await a!.page.getByRole('button', { name: 'Prendre en charge' }).click()
  const status = (page: Page) => page.locator('.review-summary .admin-status')
  await expect(status(a!.page)).toHaveText('En vérification')

  // Agent B sees who holds it and cannot decide, neither in the page nor through the API.
  await b!.page.goto(dossierUrl)
  await expect(b!.page.getByText('Pris en charge par Agent A')).toBeVisible()
  await expect(b!.page.getByRole('button', { name: /Valider le dossier/ })).toHaveCount(0)
  const bSession = await (await b!.context.request.get('/api/v1/admin/session')).json()
  const current = await (await b!.context.request.get(`/api/v1/admin/applications/${id}`)).json()
  const denied = await b!.context.request.post(`/api/v1/admin/applications/${id}/review/validate`, {
    headers: { 'X-CSRF-Token': bSession.csrfToken, Origin: 'http://localhost:5173' },
    data: { version: current.version },
  })
  expect(denied.status()).toBe(403)
  expect(await denied.text()).toContain('REVIEW_NOT_ASSIGNEE')

  // PDF preview: authenticated stream of the exact submitted version, framed by this page only.
  const pdfResponse = a!.page.waitForResponse((r) =>
    new URL(r.url()).pathname.endsWith(`/applications/${id}/documents/TRANSCRIPT/content`),
  )
  await a!.page.getByRole('button', { name: 'Aperçu : Relevé de notes' }).click()
  const pdf = await pdfResponse
  expect(pdf.status()).toBe(200)
  const headers = pdf.headers()
  expect(headers['content-type']).toBe('application/pdf')
  expect(headers['x-content-type-options']).toBe('nosniff')
  expect(headers['content-disposition']).toMatch(/^inline; filename=/)
  expect(headers['cache-control']).toBe('no-store')
  expect(headers['content-security-policy']).toBe("default-src 'none'; frame-ancestors 'self'")
  await expect(a!.page.locator('iframe[title="Aperçu : Relevé de notes"]')).toBeVisible()
  const detailHeaders = (await a!.context.request.get(`/api/v1/admin/applications/${id}`)).headers()
  expect(detailHeaders['content-security-policy']).toContain("frame-ancestors 'none'")

  // OCR proof of the first subject, generated on demand by the worker's separate loop.
  const firstRow = a!.page.locator('.review-subjects tbody tr').first()
  const subjectName = (await firstRow.locator('th').textContent())!.trim()
  await firstRow.getByRole('button', { name: `Preuve de lecture : ${subjectName}` }).click()
  const proof = a!.page.locator('#proof-panel img')
  await expect(proof).toBeVisible({ timeout: 90000 })
  expect(await proof.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeGreaterThan(100)
  await checkpoint(a!.page, 'review-dossier')

  // Agent A proposes a correction of the first grade; validation is then blocked.
  const averageBefore = (await a!.page.locator('.review-average-value').textContent())!.trim()
  const grade = Number(
    (await firstRow.locator('[data-label="Note retenue"] strong').textContent())!.replace(',', '.'),
  )
  const proposed = Math.trunc(grade) >= 19 ? '18' : String(Math.trunc(grade) + 1)
  await firstRow.getByRole('button', { name: `Corriger : ${subjectName}` }).click()
  const proposal = a!.page.getByRole('dialog', { name: 'Proposer une correction' })
  await proposal.getByLabel('Note proposée (sur 20)').fill(proposed)
  await proposal.getByLabel('Motif de la correction').fill('Lecture vérifiée sur le relevé.')
  await proposal.getByRole('button', { name: 'Proposer la correction' }).click()
  await expect(
    a!.page.getByRole('heading', { name: /elle attend la décision du directeur adjoint/ }),
  ).toBeVisible()
  await expect(a!.page.getByRole('button', { name: 'Valider le dossier' })).toBeDisabled()
  await expect(a!.page.locator('#validation-blocked')).toContainText('validation est impossible')

  // The deputy director decides from the queue, with the proof beside the values.
  await da!.page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Corrections à décider' })
    .click()
  const card = da!.page.locator('article').filter({ hasText: subjectName })
  await expect(card).toContainText('Proposée par Agent A')
  await expect(card).toContainText('Lecture vérifiée sur le relevé.')
  await card.getByRole('button', { name: 'Preuve' }).click()
  await expect(card.locator('img')).toBeVisible({ timeout: 90000 })
  await checkpoint(da!.page, 'review-corrections')
  await card.getByRole('button', { name: 'Approuver' }).click()
  await da!.page
    .getByRole('dialog', { name: 'Approuver cette correction ?' })
    .getByRole('button', { name: 'Approuver', exact: true })
    .click()
  await expect(
    da!.page.getByRole('heading', { name: /^Correction approuvée\. Moyenne retenue du dossier/ }),
  ).toBeVisible()

  // Agent A sees the corrected value beside the original one and the new average, then validates.
  await a!.page.reload()
  await expect(firstRow.locator('[data-label="Note retenue"]')).toContainText('corrigée')
  await expect(a!.page.locator('.review-average-value')).not.toHaveText(averageBefore)
  await expect(a!.page.getByText(`Moyenne au dépôt : ${averageBefore}`)).toBeVisible()
  await a!.page.getByRole('button', { name: 'Valider le dossier' }).click()
  await a!.page
    .getByRole('dialog', { name: 'Valider ce dossier ?' })
    .getByRole('button', { name: 'Valider', exact: true })
    .click()
  await expect(status(a!.page)).toHaveText('Validé')
  await expect(a!.page.locator('.review-timeline li')).toContainText([
    'Dossier soumis',
    'Pris en charge',
    'Correction proposée',
    'Correction approuvée',
    'Validé',
  ])
  await b!.page.reload()
  await expect(status(b!.page)).toHaveText('Validé')

  for (const person of [a, b, da]) await person!.context.close()
})
