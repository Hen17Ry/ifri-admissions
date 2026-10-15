import { test, expect, type APIRequestContext, type Page } from '@playwright/test'
import { readFile } from 'node:fs/promises'
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

// Ranking, freeze, exports and selection on the disposable stack of `make e2e`. It runs last
// (files in order, one worker): it closes the campaign. The IA program holds only the six
// synthetic dossiers generated here (e2e.sh leaves IA out of its 5,000).
const origin = 'http://localhost:5173'

async function mutate(request: APIRequestContext, path: string, data: unknown) {
  const session = await (await request.get('/api/v1/admin/session')).json()
  return request.post(`/api/v1/admin${path}`, {
    headers: { 'X-CSRF-Token': session.csrfToken, Origin: origin },
    data,
  })
}

// Claims and validates a dossier through the API, as an agent of the secretariat.
async function validate(request: APIRequestContext, id: string) {
  const detail = await (await request.get(`/api/v1/admin/applications/${id}`)).json()
  expect(
    (
      await mutate(request, `/applications/${id}/review/claim`, { version: detail.version })
    ).status(),
  ).toBe(200)
  expect(
    (
      await mutate(request, `/applications/${id}/review/validate`, { version: detail.version + 1 })
    ).status(),
  ).toBe(200)
}

async function download(page: Page, name: RegExp): Promise<{ text: string; filename: string }> {
  const waiting = page.waitForEvent('download')
  await page.getByRole('link', { name }).click()
  const file = await waiting
  return {
    text: await readFile((await file.path())!, 'latin1'),
    filename: file.suggestedFilename(),
  }
}

test('selection: seats, refused then accepted freeze, public export, withdrawal and promotion', async ({
  browser,
}) => {
  test.skip(!project, 'Run through `make e2e`: the scenario drives the CLI of the isolated stack.')
  test.setTimeout(300000)
  expect(
    cli(['dev', 'synthetic-applications', '--count', '6', '--program', 'IA', '--confirm']),
  ).toContain('6 synthetic submitted applications')
  expect(cli(['dev', 'close-campaign', '--confirm'])).toContain('Campaign DEV_LOCAL closed')

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
  const password = 'une phrase de passe pour la sélection'
  const people = [
    { email: 'directeur.e2e@ifri.example', name: 'Directeur E2E', role: /^Directeur\s?Consulte/ },
    { email: 'da.selection.e2e@ifri.example', name: 'DA Sélection', role: /Directeur adjoint/ },
    {
      email: 'agent.selection.e2e@ifri.example',
      name: 'Agent Sélection',
      role: /Agent du secrétariat exécutif/,
    },
  ]
  const links: string[] = []
  for (const person of people)
    links.push(await createAccount(superPage, person.email, person.name, person.role))
  await superContext.close()
  const [director, deputy, agent] = await Promise.all(
    people.map((person, i) => activate(browser, links[i]!, person.email, password)),
  )

  // Five of the six IA dossiers are validated by the agent; one stays to review.
  const list = await (
    await agent!.context.request.get(
      '/api/v1/admin/applications?program=IA&status=TO_REVIEW&sort=submittedAt&direction=asc',
    )
  ).json()
  expect(list.total).toBe(6)
  for (const item of list.items.slice(0, 5)) await validate(agent!.context.request, item.id)

  // The Director sets the seats; the freeze is refused while a dossier is not decided.
  const page = director!.page
  await page.goto('/admin/classement')
  const offerSelect = page.getByLabel('Filière et campagne')
  const value = await offerSelect.locator('option', { hasText: '(IA)' }).getAttribute('value')
  await offerSelect.selectOption(value!)
  await expect(
    page.getByRole('heading', { name: /Classement provisoire — Intelligence Artificielle/ }),
  ).toBeVisible()
  await page.getByLabel('Nombre de places').fill('3')
  await page.getByRole('button', { name: 'Enregistrer' }).click()
  await expect(page.getByText('Places et moyenne minimale enregistrées').first()).toBeVisible()
  await expect(page.locator('#freeze-blockers')).toContainText(
    '1 dossier n’est pas encore validé ou rejeté.',
  )
  await expect(page.getByRole('button', { name: 'Geler et valider le classement' })).toBeDisabled()
  await expect(page.locator('.selection-quota').first()).toHaveText('Limite des 3 places')
  const refused = await mutate(director!.context.request, `/selection/offers/${value}/freeze`, {
    seats: 3,
    minimumAverage: '',
  })
  expect(refused.status()).toBe(409)
  expect(await refused.text()).toContain('RANKING_FREEZE_BLOCKED')
  await checkpoint(page, 'selection-provisional')

  // The last dossier is decided: the freeze passes after an explicit confirmation.
  await validate(agent!.context.request, list.items[5].id)
  await page.reload()
  await expect(page.getByText('Toutes les conditions du gel sont réunies.')).toBeVisible()
  await page.getByRole('button', { name: 'Geler et valider le classement' }).click()
  const dialog = page.getByRole('alertdialog', { name: 'Geler et valider ce classement ?' })
  await expect(dialog).toContainText('définitif')
  await dialog.getByRole('button', { name: 'Geler définitivement' }).click()
  await expect(page.getByText(/Classement gelé et validé.+liste version 1/).first()).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Exports — version 1' })).toBeVisible()

  // Public export: rank, number, names and decision only; versioned header.
  const publicCsv = await download(page, /Liste publique \(CSV\)/)
  expect(publicCsv.filename).toBe('liste-ia-dev_local-v1-publique.csv')
  const csv = Buffer.from(publicCsv.text, 'latin1').toString('utf8')
  expect(csv).toContain('Version 1 du ')
  expect(csv).toContain('Rang;Numéro;Nom;Prénoms;Décision')
  expect(csv.split('\r\n').filter((line) => /;Admis$/.test(line))).toHaveLength(3)
  expect(csv).not.toMatch(/Moyenne|\d{1,2},\d{2}|@|\+229/)
  await page.getByRole('button', { name: 'Liste publique (PDF)' }).click()
  await expect(page.getByRole('link', { name: 'télécharger le PDF' })).toBeVisible({
    timeout: 60000,
  })
  const pdf = await download(page, /télécharger le PDF/)
  expect(pdf.filename).toBe('liste-ia-dev_local-v1-publique.pdf')
  expect(pdf.text.startsWith('%PDF-')).toBe(true)
  await checkpoint(page, 'selection-frozen')

  // The deputy director records a withdrawal and promotes the first waiting candidate.
  const da = deputy!.page
  await da.goto(`/admin/classement?offre=${value}`)
  await expect(da.getByText(/liste version 1/)).toBeVisible()
  const first = da.locator('.selection-table tbody tr').first()
  const firstNumber = (await first.locator('td[data-label="Numéro"]').textContent())!.trim()
  await first.getByRole('button', { name: `Désistement : ${firstNumber}` }).click()
  const withdraw = da.getByRole('alertdialog', { name: 'Enregistrer un désistement' })
  await withdraw.getByLabel('Motif du désistement').fill('Inscrit dans un autre établissement.')
  await withdraw.getByRole('button', { name: 'Enregistrer le désistement' }).click()
  await expect(
    da.getByText(`Désistement de ${firstNumber} enregistré : liste version 2.`).first(),
  ).toBeVisible()
  await expect(da.getByText(/Prochain candidat de la liste d’attente : rang 4/)).toBeVisible()
  await da.getByRole('button', { name: 'Promouvoir ce candidat' }).click()
  await da
    .getByRole('alertdialog', { name: 'Promouvoir ce candidat ?' })
    .getByRole('button', { name: 'Promouvoir', exact: true })
    .click()
  await expect(da.getByText(/est admis : liste version 3\./).first()).toBeVisible()
  await expect(da.getByRole('heading', { name: 'Exports — version 3' })).toBeVisible()
  await expect(da.locator('.review-timeline li')).toHaveCount(3)
  const after = Buffer.from((await download(da, /Liste publique \(CSV\)/)).text, 'latin1').toString(
    'utf8',
  )
  expect(after).toContain('Version 3 du ')
  expect(after).not.toContain(firstNumber)
  expect(after.split('\r\n').filter((line) => /;Admis$/.test(line))).toHaveLength(3)
  await checkpoint(da, 'selection-deputy')

  for (const person of [director, deputy, agent]) await person!.context.close()
})
