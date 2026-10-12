import { test, expect } from '@playwright/test'
import path from 'node:path'
import { readFile } from 'node:fs/promises'
const pdf = path.resolve('../../services/api/internal/modules/documents/testdata/valid.pdf')

test('real document journey, keyboard, four widths and 200 percent zoom', async ({ page }) => {
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
  await page.getByRole('radio', { name: /Génie Logiciel/ }).check()
  await checkpoint('program')
  await page.locator('form button[type=submit]').click()
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
  for (const type of ['REQUEST_LETTER', 'TRANSCRIPT', 'IDENTITY_DOCUMENT', 'PARENTAL_COMMITMENT']) {
    await page.locator(`#file-${type}`).setInputFiles(pdf)
    await expect(
      page.locator('.document-list li').filter({ has: page.locator(`#file-${type}`) }),
    ).toContainText('Ajouté — version 1')
  }
  await page.locator('#file-TRANSCRIPT').setInputFiles({
    name: 'releve-' + 'notes-'.repeat(25) + '.pdf',
    mimeType: 'application/pdf',
    buffer: await readFile(pdf),
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
  expect(await preview.body()).toEqual(await readFile(pdf))
  await popup.close()
  await page.getByRole('button', { name: 'Voir le récapitulatif', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Vérifiez votre brouillon' })).toBeVisible()
  await page.reload()
  await expect(page.getByText('Votre candidature reste un brouillon.')).toBeVisible()
  await expect(page.getByRole('link', { name: /Voir.*nouvel onglet/ })).toHaveCount(4)
  await checkpoint('review')
  await page.evaluate(() => {
    document.documentElement.style.zoom = '2'
  })
  await checkpoint('review-zoom200')
})
