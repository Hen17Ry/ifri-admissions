import { test, expect } from '@playwright/test'
import { checkpoint, cli, enrol, project, superAdmin } from './support'

test('administration: CLI super-admin, TOTP, account creation, activation, deactivation', async ({
  page,
  browser,
}) => {
  test.skip(!project, 'Run through `make e2e`: the scenario drives the CLI of the isolated stack.')
  const { email: superEmail, password: superPassword } = superAdmin
  expect(
    cli(
      ['admin', 'create-super-admin', '--email', superEmail, '--name', 'Super Admin E2E'],
      `${superPassword}\n`,
    ),
  ).toContain('Super-administrator created')
  expect(() =>
    cli(
      ['admin', 'create-super-admin', '--email', 'other@ifri.example', '--name', 'Other'],
      `${superPassword}\n`,
    ),
  ).toThrow()

  // Strict headers on the administration pages themselves.
  const document = await page.goto('/admin')
  expect(document?.headers()['x-frame-options']).toBe('DENY')
  expect(document?.headers()['content-security-policy']).toContain("frame-ancestors 'none'")
  await expect(page).toHaveURL(/\/admin\/connexion/)
  await page.setViewportSize({ width: 1280, height: 900 })
  await checkpoint(page, 'login')
  await page.getByLabel('Adresse email').fill(superEmail)
  await page.getByLabel('Mot de passe', { exact: true }).fill('mauvais mot de passe saisi')
  await page.getByRole('button', { name: 'Se connecter' }).click()
  await expect(page.getByText('Identifiant ou mot de passe incorrect.')).toBeVisible()
  await page.getByLabel('Mot de passe', { exact: true }).fill(superPassword)
  await page.getByRole('button', { name: 'Se connecter' }).click()
  await expect(page).toHaveURL(/\/admin\/verification/)
  await checkpoint(page, 'enrolment')
  await enrol(page)
  await checkpoint(page, 'dashboard')

  // Create an agent of the executive secretariat (one role per account); the link is shown once.
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Comptes' })
    .click()
  await expect(page.getByRole('heading', { name: 'Comptes administrateurs' })).toBeVisible()
  await page.getByRole('button', { name: 'Créer un compte' }).click()
  const create = page.getByRole('dialog', { name: 'Créer un compte administrateur' })
  await create.getByLabel('Adresse email').fill('reviewer.e2e@ifri.example')
  await create.getByLabel('Nom affiché').fill('Vérificatrice GL')
  await create.getByLabel(/Directeur adjoint/).check()
  await create.getByLabel(/Agent du secrétariat exécutif/).check()
  await expect(create.getByLabel(/Directeur adjoint/)).not.toBeChecked()
  await checkpoint(page, 'create-account')
  await create.getByRole('button', { name: 'Créer le compte' }).click()
  const linkDialog = page.getByRole('alertdialog', { name: 'Lien d’activation' })
  await expect(linkDialog).toBeVisible()
  const activationUrl = await linkDialog.getByLabel('Lien').inputValue()
  expect(activationUrl).toMatch(/\/admin\/activation#[A-Za-z0-9_-]{43}$/)
  await linkDialog.getByRole('button', { name: 'Fermer' }).click()
  await expect(page.getByRole('row', { name: /Vérificatrice GL/ })).toContainText(
    'Activation en attente',
  )
  await checkpoint(page, 'accounts')

  // Activation in another browser: the token leaves the address bar at once.
  const reviewerContext = await browser.newContext()
  const reviewer = await reviewerContext.newPage()
  await reviewer.goto(activationUrl.replace(/^https?:\/\/[^/]+/, ''))
  await expect(reviewer.getByText(/Bienvenue Vérificatrice GL/)).toBeVisible()
  expect(reviewer.url()).not.toContain('#')
  await reviewer.getByLabel('Nouveau mot de passe').fill('Password1234')
  await reviewer.getByLabel('Confirmez le mot de passe').fill('Password1234')
  await reviewer.getByRole('button', { name: 'Activer mon compte' }).click()
  await expect(reviewer.getByText(/figure parmi les plus utilisés/)).toBeVisible()
  const reviewerPassword = 'une phrase de passe pour la revue'
  await reviewer.getByLabel('Nouveau mot de passe').fill(reviewerPassword)
  await reviewer.getByLabel('Confirmez le mot de passe').fill(reviewerPassword)
  await reviewer.getByRole('button', { name: 'Activer mon compte' }).click()
  await expect(reviewer.getByRole('heading', { name: 'Compte activé' })).toBeVisible()
  await reviewer.getByRole('link', { name: 'Se connecter' }).click()
  await reviewer.getByLabel('Adresse email').fill('reviewer.e2e@ifri.example')
  await reviewer.getByLabel('Mot de passe', { exact: true }).fill(reviewerPassword)
  await reviewer.getByRole('button', { name: 'Se connecter' }).click()
  await enrol(reviewer)
  await expect(reviewer.getByRole('navigation', { name: 'Administration' })).not.toContainText(
    'Comptes',
  )
  const forbidden = await reviewerContext.request.get('/api/v1/admin/accounts')
  expect(forbidden.status()).toBe(403)

  // Deactivation closes the reviewer's session at once; the same generic message afterwards.
  await page
    .getByRole('row', { name: /Vérificatrice GL/ })
    .getByRole('button', { name: /Désactiver/ })
    .click()
  const confirm = page.getByRole('alertdialog', { name: 'Désactiver ce compte ?' })
  await expect(confirm.getByRole('button', { name: 'Annuler' })).toBeFocused()
  await confirm.getByRole('button', { name: 'Désactiver', exact: true }).click()
  await expect(page.getByRole('row', { name: /Vérificatrice GL/ })).toContainText('Désactivé')
  expect((await reviewerContext.request.get('/api/v1/admin/session')).status()).toBe(401)
  await reviewer.goto('/admin/compte')
  await expect(reviewer).toHaveURL(/\/admin\/connexion/)
  await reviewer.getByLabel('Adresse email').fill('reviewer.e2e@ifri.example')
  await reviewer.getByLabel('Mot de passe', { exact: true }).fill(reviewerPassword)
  await reviewer.getByRole('button', { name: 'Se connecter' }).click()
  await expect(reviewer.getByText('Identifiant ou mot de passe incorrect.')).toBeVisible()
  await reviewerContext.close()

  // The journal shows the deactivation, read-only.
  await page
    .getByRole('navigation', { name: 'Administration' })
    .getByRole('link', { name: 'Journal d’audit' })
    .click()
  await page.getByLabel('Événement').selectOption({ label: 'Compte désactivé' })
  await page.getByRole('button', { name: 'Filtrer' }).click()
  await expect(page.getByRole('row', { name: /Compte désactivé/ })).toContainText(
    'reviewer.e2e@ifri.example',
  )
  await checkpoint(page, 'audit')

  // Candidate and admin sessions never cross; logout ends the admin session.
  expect((await page.context().request.get('/api/v1/applications/drafts/current')).status()).toBe(
    401,
  )
  await page.getByRole('button', { name: 'Déconnexion' }).click()
  await expect(page.getByText('Vous êtes déconnecté.')).toBeVisible()
  expect((await page.context().request.get('/api/v1/admin/session')).status()).toBe(401)
})
