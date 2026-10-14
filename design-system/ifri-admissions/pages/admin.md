# Espace d'administration — adaptations du master

MASTER.md reste la source principale : mêmes tokens (marine, surfaces claires, `destructive` pour l'action dangereuse), même typographie (Lexend / Source Sans 3), mêmes espacements, rayons et contrats de composants. Cette page ne fixe que la composition propre à `/admin` (phase 7).

## Recherches UI UX Pro Max exécutées

- **Recommandation de design system** : `--design-system` « university admissions administration back-office secure institutional », dials variance 3, motion 2, density 6.
  - Retenus : Minimalism & Swiss Style (déjà le master) et la palette marine identique au master.
  - Écartés, comme lors de la génération du master : le pattern « Portfolio Grid » (vitrine), les polices EB Garamond / Crimson Text (moins lisibles pour formulaires et tableaux) et le « scroll reveal » GSAP (animation décorative).
- **Recherches `ux`**, recommandations retenues :
  - admin dashboard sidebar navigation → Keyboard Navigation, Active State (élément courant signalé par la couleur et pas seulement par elle) ;
  - empty state dashboard → Empty States (message utile et action) ;
  - login form authentication error → Accessible Authentication (gestionnaires de mots de passe et collage autorisés), Error Messages en `role=alert`, Submit Feedback ;
  - two factor code input → Input Types, Input Labels ;
  - password strength feedback → Password Visibility (bascule afficher / masquer) ;
  - destructive action confirmation → Confirmation Dialogs ;
  - status badge → Color Only (texte + couleur), Color Contrast ;
  - filter audit log table → Table Handling (cartes sur mobile), Chip Collection Reflow ;
  - form validation error summary → Focusable Error Summary.
  - Écartés : Bulk Actions (aucune action de masse sur les comptes, volontairement) et Haptic Feedback.
- **Recherches `vue`** : route guard authentication → navigation guards (`beforeEach`), pas de contrôle d'accès dispersé dans les composants ; Pinia pour l'état de session partagé ; `defineEmits` typé ; tests du comportement rendu.
- **Recherche `icons`** : lock, shield, key, user, rendus avec Lucide Vue (`ShieldCheck`, `KeyRound`, `Users`, `ScrollText`, `UserCog`, `ShieldOff`).
- **Sans correspondance** dans la base : clipboard, print, session timeout warning, security headers. Ce sont des choix produit, documentés ici.

## Composition

- **Écrans d'authentification** (connexion, activation, second facteur) : en-tête institutionnel minimal (« Administration »), un seul panneau centré de 480 px au plus, une action principale. Pas d'illustration ni de hero.
- **Espace connecté** :
  - en-tête avec nom affiché et « Déconnexion » ;
  - navigation latérale de 240 px à partir de 1024 px (master : « Navigation admin latérale ~240 px ») ;
  - en dessous de 1024 px, bouton « Menu » (`aria-expanded`, `aria-controls`) qui déplie la même liste ;
  - entrées filtrées par permission, pour l'affichage seulement (l'API décide) ;
  - page courante signalée par `aria-current="page"`, un fond `muted` et un liseré marine.
- **Tableau de bord** : état vide utile (« Aucun dossier à traiter pour le moment »), liens vers les écrans autorisés, aucune promesse de date.
- **Tableaux** (comptes, journal) : `table` sémantique avec légende, `th scope`, en-têtes de lignes. En dessous de 768 px, chaque ligne devient une carte libellée (`data-label`) : pas de défilement horizontal de la page, rien de masqué.
- **Statuts** : badge texte + pastille + couleur (actif `success`, en attente `warning`, désactivé `on-muted`) ; rôles en puces qui passent à la ligne.

## Formulaires et authentification

- Labels permanents, aides et erreurs liées (`FormField`), focus sur le premier champ en erreur ; message d'échec focalisable en `role=alert`.
- **Mots de passe** :
  - bascule afficher / masquer (`aria-pressed`, `aria-controls`) ;
  - `autocomplete` `current-password` / `new-password` ;
  - champ `username` caché pour les gestionnaires de mots de passe ;
  - collage autorisé ;
  - aide « 12 caractères au moins, une phrase de plusieurs mots », sans jauge de complexité trompeuse.
- **Code TOTP** : `inputmode="numeric"`, `autocomplete="one-time-code"`. Le code de secours passe en saisie texte libre sur demande explicite.
- **Enrôlement** : trois étapes numérotées. Le QR code (image SVG en `data:`, avec un texte alternatif) est placé à côté de la clé en groupes de 4 caractères, avec « Copier la clé ». Les codes de secours sont montrés une seule fois, avec « Copier » et « Télécharger (.txt) », et une case « J'ai conservé ces codes en lieu sûr » doit être cochée avant de continuer.
- **Message de connexion** : un seul message générique, pour ne pas révéler l'existence d'un compte. La limitation de débit a son propre message.

## Dialogues

- `<dialog>` natif ouvert par `showModal()` (ADR 093), composant `UiDialog`.
- Rôle `alertdialog` pour une action destructive ou irréversible (désactiver, réinitialiser la MFA, régénérer des codes, lien d'activation montré une fois).
- Focus initial sur « Annuler » quand l'action est dangereuse ; focus rendu à l'élément d'origine ; Escape bloqué pendant l'envoi.
- Action dangereuse en variante `danger` (token `destructive`, texte blanc, contraste 4,8:1). L'action n'est jamais proposée sur son propre compte.
- **Lien d'activation** : champ en lecture seule sélectionné au focus, « Copier le lien », date d'expiration et rappel « il ne sera plus affiché ».

## Journal d'audit

- Lecture seule, filtres simples (type d'événement en libellé français, acteur, période) et « Afficher les événements plus anciens » par curseur. Pas de pagination numérotée : le volume est inconnu.
- Dates en heure du Bénin. Les métadonnées techniques sont dans un `<details>` replié, et les empreintes HMAC sont présentées comme telles.

## Mouvement, accessibilité, responsive

- Seules les transitions couleur et opacité de 150 ms du master sont utilisées ; `prefers-reduced-motion` global ; aucune animation d'entrée.
- Cibles de 44 px ou plus. Les quatre largeurs (375, 768, 1024, 1440) sont vérifiées par Playwright, avec captures et absence de défilement horizontal.
