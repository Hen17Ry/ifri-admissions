import { test, expect } from '@playwright/test'
import path from 'node:path'
import { readFile } from 'node:fs/promises'
const transcript = path.resolve('../../services/ocr/tests/fixtures/GL-DT_DWM-scan.pdf')
const pdf = path.resolve('../../services/api/internal/modules/documents/testdata/valid.pdf')

test('real journey to submission, keyboard, four widths, 200 percent zoom and lock', async ({
  page,
}) => {
  async function checkpoint(name: string) {
    for (const width of [375, 768, 1024, 1440]) {
      await page.setViewportSize({ width, height: 950 })
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth),
      ).toBe(false)
      await page.screenshot({ path: `browser-artifacts/${name}-${width}.png`, fullPage: true })
    }
    await page.setViewportSize({ width: 375, height: 950 })
  }
  // Diagnose bootstrap failures before waiting for a business control.
  // Only script paths/status and runtime errors are recorded, never cookies or API bodies.
  const startupErrors: string[] = []
  page.on('pageerror', (error) => startupErrors.push(error.message))
  page.on('response', (response) => {
    const resource = response.request().resourceType()
    if (['script', 'stylesheet'].includes(resource) && response.status() >= 400) {
      startupErrors.push(
        `${resource}: ${new URL(response.url()).pathname} HTTP ${response.status()}`,
      )
    }
  })
  await page.goto('/candidature')
  try {
    await expect(page.locator('#app')).not.toBeEmpty({ timeout: 15000 })
    await expect(page.getByRole('button', { name: 'Commencer mon brouillon' })).toBeVisible({
      timeout: 15000,
    })
  } catch (error) {
    throw new Error(
      `Candidate page bootstrap failed: ${startupErrors.join('; ') || 'No runtime error reported; check the served page and API connection.'}`,
      { cause: error },
    )
  }
  await page.getByRole('button', { name: 'Commencer mon brouillon' }).click()
  await page.getByRole('button', { name: 'Continuer', exact: true }).click()
  await expect(page.getByLabel('Nom', { exact: true })).toBeFocused()
  await checkpoint('profile-errors')
  await page.getByLabel('Nom', { exact: true }).fill('Validation')
  await page.getByLabel('Prénoms', { exact: true }).fill('Parcours synthétique')
  await page.getByLabel('Email', { exact: true }).fill('browser-phase4@example.invalid')
  await page.getByLabel('Téléphone', { exact: true }).fill('+33123456789')
  await checkpoint('profile')
  await page.getByRole('button', { name: 'Continuer', exact: true }).click()
  await page.getByLabel('Série ou diplôme d’origine').selectOption('DT_DWM')
  await page.getByLabel('Année d’obtention').fill(String(new Date().getFullYear() - 1))
  await checkpoint('academic')
  await page.getByRole('button', { name: 'Continuer', exact: true }).click()
  const programSaved = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === '/api/v1/applications/drafts/current/program' &&
      response.request().method() === 'PATCH',
  )
  await page.getByRole('radio', { name: /Génie Logiciel/ }).check()
  expect((await programSaved).status()).toBe(200)
  const documentsButton = page.getByRole('button', { name: 'Ajouter les documents', exact: true })
  await expect(documentsButton).toBeEnabled()
  await checkpoint('program')
  const programSubmitted = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === '/api/v1/applications/drafts/current/program' &&
      response.request().method() === 'PATCH',
  )
  await documentsButton.click()
  expect((await programSubmitted).status()).toBe(200)
  await expect(page.getByRole('heading', { name: 'Documents', exact: true })).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Voir le récapitulatif', exact: true }),
  ).toBeDisabled()
  await page.keyboard.press('Tab')
  expect(await page.evaluate(() => document.activeElement?.tagName)).not.toBe('BODY')
  await checkpoint('documents-empty')
  await page.locator('#file-TRANSCRIPT').setInputFiles({
    name: 'fausse-image.pdf',
    mimeType: 'image/png',
    buffer: Buffer.from('not pdf'),
  })
  await expect(page.getByText('Choisissez un fichier au format PDF.')).toBeVisible()
  await checkpoint('documents-error')
  for (const type of ['TRANSCRIPT', 'REQUEST_LETTER', 'IDENTITY_DOCUMENT', 'PARENTAL_COMMITMENT']) {
    await page.locator(`#file-${type}`).setInputFiles(type === 'TRANSCRIPT' ? transcript : pdf)
    await expect(
      page.locator('.document-list li').filter({ has: page.locator(`#file-${type}`) }),
    ).toContainText('Ajouté — version 1')
    if (type === 'TRANSCRIPT') {
      await expect(page.getByRole('button', { name: 'Analyser mon relevé' })).toHaveCount(0)
      await expect(
        page.getByText('Analyse de votre relevé en cours…', { exact: true }),
      ).toBeVisible()
      await page.reload()
      await expect(page.getByRole('heading', { name: 'Documents', exact: true })).toBeVisible()
      await expect(
        page.getByRole('heading', { name: 'Informations détectées', exact: true }),
      ).toBeVisible({ timeout: 90000 })
      await checkpoint('documents-analysis')
      await page.getByRole('button', { name: 'Oui, les informations sont correctes' }).click()
      await expect(
        page.getByText('Informations du relevé confirmées', { exact: true }),
      ).toBeVisible()
    }
  }
  await page.locator('#file-TRANSCRIPT').setInputFiles({
    name: 'releve-' + 'notes-'.repeat(25) + '.pdf',
    mimeType: 'application/pdf',
    buffer: await readFile(transcript),
  })
  await expect(
    page.locator('.document-list li').filter({ has: page.locator('#file-TRANSCRIPT') }),
  ).toContainText('version 2')
  await checkpoint('documents-complete')
  const contentPath = '/api/v1/applications/drafts/current/documents/TRANSCRIPT/content'
  const pdfResponsePromise = page.context().waitForEvent('response', {
    predicate: (response) => new URL(response.url()).pathname === contentPath,
    timeout: 15000,
  })
  const popupPromise = page.waitForEvent('popup')
  await page
    .locator('.document-list li')
    .filter({ has: page.locator('#file-TRANSCRIPT') })
    .getByRole('link')
    .click()
  const popup = await popupPromise
  // The native PDF viewer may expose neither an HTML load event nor a page URL.
  // Assert the response triggered by the actual click before making a separate integrity request.
  const pdfResponse = await pdfResponsePromise
  expect(pdfResponse.request().isNavigationRequest()).toBe(true)
  expect([200, 206]).toContain(pdfResponse.status())
  expect(pdfResponse.headers()['content-type']).toContain('application/pdf')
  expect(pdfResponse.headers()['content-disposition']).toContain('inline')
  const preview = await page.context().request.get(contentPath)
  expect(preview.status()).toBe(200)
  expect(await preview.body()).toEqual(await readFile(transcript))
  await popup.close()
  await expect(
    page.getByRole('heading', { name: 'Informations détectées', exact: true }),
  ).toBeVisible({ timeout: 90000 })
  await expect(page.locator('.analysis-table tbody tr')).toHaveCount(3)
  await expect(page.locator('.analysis-table')).toContainText('15.50')
  await expect(page.locator('.analysis-table input')).toHaveCount(0)
  await page.getByRole('button', { name: 'Oui, les informations sont correctes' }).click()
  await expect(page.getByText('Informations du relevé confirmées', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Voir le récapitulatif', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Vérifiez et soumettez' })).toBeVisible()
  await page.reload()
  await expect(page.getByRole('heading', { name: 'Soumission définitive' })).toBeVisible()
  await expect(page.getByRole('link', { name: /Voir.*nouvel onglet/ })).toHaveCount(4)
  await expect(page.getByText(/moyenne/i)).toHaveCount(0)
  await checkpoint('review')
  await page.evaluate(() => {
    document.documentElement.style.zoom = '2'
  })
  await checkpoint('review-zoom200')
  await page.evaluate(() => {
    document.documentElement.style.zoom = '1'
  })

  // Native modal dialog: initial focus on Annuler, focus kept inside, Escape restores focus.
  const submit = page.getByRole('button', { name: 'Soumettre ma candidature' })
  await submit.focus()
  await page.keyboard.press('Enter')
  const dialog = page.getByRole('alertdialog', { name: 'Soumettre votre candidature ?' })
  await expect(dialog).toBeVisible()
  await expect(dialog).toContainText(
    'Après soumission, vous ne pourrez plus modifier votre candidature',
  )
  await expect(dialog.getByRole('button', { name: 'Annuler' })).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(dialog.getByRole('button', { name: 'Confirmer la soumission' })).toBeFocused()
  await checkpoint('submit-dialog')
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
  await expect(submit).toBeFocused()

  await submit.click()
  const submitted = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === '/api/v1/applications/drafts/current/submission' &&
      response.request().method() === 'POST',
  )
  await dialog.getByRole('button', { name: 'Confirmer la soumission' }).click()
  expect((await submitted).status()).toBe(200)
  await expect(page).toHaveURL(/\/candidature\/confirmation$/)
  const number = page.locator('#application-number')
  await expect(number).toHaveText(/^IFRI-\d{4}-GL-\d{6}$/)
  const value = (await number.textContent())!
  await expect(page.getByText('Génie Logiciel')).toBeVisible()
  await expect(page.getByRole('button', { name: /Modifier/ })).toHaveCount(0)
  await expect(page.getByText(/moyenne/i)).toHaveCount(0)
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.getByRole('button', { name: 'Copier le numéro' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Numéro copié' })).toBeVisible()
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(value)
  await checkpoint('confirmation')
  // One line for the number down to 375 px: never split at a hyphen when copied by hand.
  expect(
    await number.evaluate(
      (element) =>
        element.getBoundingClientRect().height < 2 * parseFloat(getComputedStyle(element).fontSize),
    ),
  ).toBe(true)
  // Printable receipt, at the width of an A4 page (210 mm at 96 dpi).
  await page.setViewportSize({ width: 794, height: 1123 })
  await page.emulateMedia({ media: 'print' })
  await expect(page.getByRole('button', { name: 'Copier le numéro' })).toBeHidden()
  await expect(number).toBeVisible()
  await page.screenshot({ path: 'browser-artifacts/confirmation-print.png', fullPage: true })
  await page.emulateMedia({ media: 'screen' })
  await page.setViewportSize({ width: 375, height: 950 })

  // Refresh, then the draft URL: both lead back to the same receipt, never to a new draft.
  await page.reload()
  await expect(number).toHaveText(value)
  await page.goto('/candidature')
  await expect(page).toHaveURL(/\/candidature\/confirmation$/)
  await expect(number).toHaveText(value)

  // Backend lock: candidate mutations refused, submission replayed with the same number.
  const origin = 'http://localhost:5173'
  const refused = await page
    .context()
    .request.patch('/api/v1/applications/drafts/current/profile', {
      headers: { Origin: origin },
      data: {
        version: 99,
        profile: {
          lastName: 'Modifié',
          firstNames: 'Après soumission',
          email: 'late@example.invalid',
          phone: '+33123456789',
        },
      },
    })
  expect(refused.status()).toBe(409)
  expect((await refused.json()).error.code).toBe('APPLICATION_LOCKED')
  const documentsRead = await page
    .context()
    .request.get('/api/v1/applications/drafts/current/documents')
  expect(documentsRead.status()).toBe(409)
  const replay = await page
    .context()
    .request.post('/api/v1/applications/drafts/current/submission', {
      headers: { Origin: origin },
      data: { version: 1 },
    })
  expect(replay.status()).toBe(200)
  expect((await replay.json()).applicationNumber).toBe(value)
})

test('rejected extraction requires transcript replacement and a fresh analysis', async ({
  page,
}) => {
  const origin = 'http://localhost:5173'
  const base = '/api/v1/applications/drafts'
  async function jsonRequest(route: string, method: 'post' | 'patch', data: unknown) {
    const response = await page
      .context()
      .request[method](route, { headers: { Origin: origin }, data })
    expect(response.ok()).toBe(true)
    return response.json()
  }
  let draft = await jsonRequest(base, 'post', { campaign: 'DEV_LOCAL' })
  draft = await jsonRequest(`${base}/current/profile`, 'patch', {
    version: draft.version,
    profile: {
      lastName: 'Validation',
      firstNames: 'Rejet synthétique',
      email: 'reject@example.invalid',
      phone: '+33123456789',
    },
  })
  draft = await jsonRequest(`${base}/current/academic-background`, 'patch', {
    version: draft.version,
    qualification: 'DT_DWM',
    graduationYear: new Date().getFullYear() - 1,
  })
  draft = await jsonRequest(`${base}/current/program`, 'patch', {
    version: draft.version,
    program: 'GL',
  })
  let version: number = draft.version
  for (const type of ['REQUEST_LETTER', 'TRANSCRIPT', 'IDENTITY_DOCUMENT', 'PARENTAL_COMMITMENT']) {
    const response = await page.context().request.post(`${base}/current/documents/${type}`, {
      headers: { Origin: origin, 'X-Application-Version': String(version) },
      multipart: {
        file: {
          name: 'synthetic.pdf',
          mimeType: 'application/pdf',
          buffer: await readFile(type === 'TRANSCRIPT' ? transcript : pdf),
        },
      },
    })
    expect(response.status()).toBe(201)
    version = (await response.json()).applicationVersion
  }
  await page.goto('/candidature')
  await expect(
    page.getByRole('heading', { name: 'Informations détectées', exact: true }),
  ).toBeVisible({ timeout: 90000 })
  const before = await (
    await page.context().request.get(`${base}/current/transcript-analysis`)
  ).json()
  await page.getByRole('button', { name: 'Non, les informations sont incorrectes' }).click()
  await page.getByRole('button', { name: 'Rejeter et remplacer mon relevé' }).click()
  await expect(page.getByRole('heading', { name: 'Documents', exact: true })).toBeVisible()
  const rejected = await (
    await page.context().request.get(`${base}/current/transcript-analysis`)
  ).json()
  expect(rejected.candidateConfirmation).toBe('REJECTED')
  expect(rejected.verificationReady).toBe(false)
  const repeat = await page.context().request.post(`${base}/current/transcript-analysis/confirm`, {
    headers: { Origin: origin },
    data: { analysisId: before.id, version },
  })
  expect(repeat.status()).toBe(409)
  await page.locator('#file-TRANSCRIPT').setInputFiles(transcript)
  await expect(
    page.locator('.document-list li').filter({ has: page.locator('#file-TRANSCRIPT') }),
  ).toContainText('version 2')
  await expect(page.getByRole('button', { name: 'Analyser mon relevé' })).toHaveCount(0)
  await expect(
    page.getByRole('heading', { name: 'Informations détectées', exact: true }),
  ).toBeVisible({ timeout: 90000 })
  const after = await (
    await page.context().request.get(`${base}/current/transcript-analysis`)
  ).json()
  expect(after.id).not.toBe(before.id)
  expect(after.documentVersionId).not.toBe(before.documentVersionId)
  expect(after.candidateConfirmation).toBe('PENDING')
  expect(after.verificationReady).toBe(false)
  await page.getByRole('button', { name: 'Oui, les informations sont correctes' }).click()
  await expect(page.getByText('Informations du relevé confirmées', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Voir le récapitulatif', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Vérifiez et soumettez' })).toBeVisible()
})
