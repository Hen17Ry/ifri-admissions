import { expect, type Browser, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { createHmac } from 'node:crypto'

// Shared by the administration specs. They run only on the disposable stack of `make e2e`
// (the CLI acts on its API container).
export const project = process.env.E2E_PROJECT
const root = process.env.E2E_ROOT ?? '../..'
export const superAdmin = {
  email: 'super.e2e@ifri.example',
  password: 'phrase de passe du super administrateur',
}

export function cli(args: string[], input = ''): string {
  return execFileSync(
    'docker',
    ['compose', '--project-directory', root, '-p', project!, 'exec', '-T', 'api', 'api', ...args],
    { input, encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] },
  )
}

// RFC 6238 (HMAC-SHA1, 30 s, 6 digits), computed like an authenticator application.
function base32(secret: string): Buffer {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const char of secret.replace(/\s/g, ''))
    bits += alphabet.indexOf(char).toString(2).padStart(5, '0')
  const bytes = bits.match(/.{8}/g) ?? []
  return Buffer.from(bytes.map((byte) => parseInt(byte, 2)))
}
const used = new Map<string, number>()
export async function totp(secret: string): Promise<string> {
  // A code is accepted once: wait for the next 30 s step if this secret already used the current one.
  let step = Math.floor(Date.now() / 30000)
  while ((used.get(secret) ?? -1) >= step) {
    await new Promise((resolve) => setTimeout(resolve, 1000))
    step = Math.floor(Date.now() / 30000)
  }
  used.set(secret, step)
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(step))
  const digest = createHmac('sha1', base32(secret)).update(counter).digest()
  const offset = digest[digest.length - 1]! & 0x0f
  const value = (digest.readUInt32BE(offset) & 0x7fffffff) % 1_000_000
  return value.toString().padStart(6, '0')
}

// Four widths, no horizontal scroll, one full-page capture each.
export async function checkpoint(page: Page, name: string) {
  for (const width of [375, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth),
    ).toBe(false)
    await page.screenshot({ path: `browser-artifacts/admin-${name}-${width}.png`, fullPage: true })
  }
  await page.setViewportSize({ width: 1280, height: 900 })
}

export async function enrol(page: Page): Promise<string[]> {
  await expect(
    page.getByRole('heading', { name: 'Configurez la double authentification' }),
  ).toBeVisible()
  await expect(page.getByRole('img', { name: /Code QR/ })).toBeVisible()
  const secret = (await page.locator('.mfa-secret').textContent())!.replace(/\s/g, '')
  await page.getByLabel('Code à 6 chiffres').fill(await totp(secret))
  await page.getByRole('button', { name: 'Valider' }).click()
  await expect(page.getByRole('heading', { name: 'Vos codes de secours' })).toBeVisible()
  const codes = await page.locator('.recovery-list li').allTextContents()
  expect(codes).toHaveLength(10)
  await page.getByLabel('J’ai conservé ces codes en lieu sûr.').check()
  await page.getByRole('button', { name: 'Accéder à l’administration' }).click()
  await expect(page.getByRole('heading', { name: 'Tableau de bord' })).toBeVisible()
  return codes
}

export async function login(page: Page, email: string, password: string) {
  await page.goto('/admin/connexion')
  await page.getByLabel('Adresse email').fill(email)
  await page.getByLabel('Mot de passe', { exact: true }).fill(password)
  await page.getByRole('button', { name: 'Se connecter' }).click()
  await expect(page).toHaveURL(/\/admin\/verification/)
}

// Super-administrator page: creates an account with one role and returns its activation link.
export async function createAccount(page: Page, email: string, name: string, role: RegExp) {
  await page.goto('/admin/comptes')
  await page.getByRole('button', { name: 'Créer un compte' }).click()
  const create = page.getByRole('dialog', { name: 'Créer un compte administrateur' })
  await create.getByLabel('Adresse email').fill(email)
  await create.getByLabel('Nom affiché').fill(name)
  await create.getByLabel(role).check()
  await create.getByRole('button', { name: 'Créer le compte' }).click()
  const linkDialog = page.getByRole('alertdialog', { name: 'Lien d’activation' })
  const url = await linkDialog.getByLabel('Lien').inputValue()
  await linkDialog.getByRole('button', { name: 'Fermer' }).click()
  return url.replace(/^https?:\/\/[^/]+/, '')
}

// Activation, first sign-in and enrolment in a browser context of its own.
export async function activate(browser: Browser, url: string, email: string, password: string) {
  const context = await browser.newContext()
  const page = await context.newPage()
  await page.goto(url)
  await page.getByLabel('Nouveau mot de passe').fill(password)
  await page.getByLabel('Confirmez le mot de passe').fill(password)
  await page.getByRole('button', { name: 'Activer mon compte' }).click()
  await expect(page.getByRole('heading', { name: 'Compte activé' })).toBeVisible()
  await login(page, email, password)
  await enrol(page)
  return { context, page }
}
