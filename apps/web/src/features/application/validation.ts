import type { Profile } from './api'
export function profileErrors(profile: Profile, required = true): Record<string, string> {
  const errors: Record<string, string> = {}
  if (required && !profile.lastName.trim()) errors.lastName = 'Renseignez votre nom.'
  if (required && !profile.firstNames.trim()) errors.firstNames = 'Renseignez vos prénoms.'
  if ((required || profile.email) && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(profile.email.trim()))
    errors.email = 'Saisissez une adresse email valide.'
  const phone = profile.phone.replace(/[ ()-]/g, '').replace(/^00/, '+')
  if ((required || phone) && !/^\+[1-9]\d{6,14}$/.test(phone))
    errors.phone = 'Saisissez un numéro international commençant par +.'
  return errors
}
export function academicErrors(qualification: string, year: string): Record<string, string> {
  const errors: Record<string, string> = {}
  if (!qualification) errors.qualification = 'Choisissez votre série ou diplôme.'
  if (!/^\d{4}$/.test(year) || Number(year) < 1900 || Number(year) > new Date().getFullYear())
    errors.graduationYear = 'Saisissez une année comprise entre 1900 et l’année courante.'
  return errors
}
