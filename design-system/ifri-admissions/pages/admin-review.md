# Back-office des dossiers — adaptations du master (phase 8)

MASTER.md et `pages/admin.md` restent la référence : mêmes tokens (marine, surfaces claires, `success` / `warning` / `destructive`), typographie Lexend / Source Sans 3, espacements, rayons, cibles de 44 px, dialogues natifs. Cette page ne fixe que la composition propre aux écrans de la phase 8 : tableau de bord, `/admin/candidatures`, `/admin/candidatures/:id` et `/admin/corrections`.

## Recherches UI UX Pro Max exécutées

- **Style `--domain style`** « data-dense dashboard admin table » → *Data-Dense Dashboard* : rangée d'indicateurs, tableaux triables, en-têtes collants. **Adapté** :
  - trois indicateurs cliquables sur le fond clair du master, pas de grille de graphiques ;
  - en-têtes collants écartés : les tableaux passent en cartes sous 768 px et la page défile normalement ;
  - aucune couleur ni ombre supplémentaire.
- **Recherches `ux`**, recommandations retenues :
  - table handling → *Table Handling* : tableau sémantique au-dessus de 768 px, cartes libellées (`data-label`) en dessous, jamais de défilement horizontal de la page ;
  - no results → *Empty States* : message, suggestion (orthographe, filtre, numéro) et bouton « Réinitialiser les filtres » ;
  - navigation active state → *Active State* : « Candidatures » reste courant sur la fiche d'un dossier ;
  - image scaling → *Image Scaling* : preuve OCR en `max-width: 100%`, hauteur automatique ;
  - required indicators, confirmation dialogs → motifs obligatoires annoncés par l'aide et l'erreur liée ; rejet et refus en `alertdialog`, variante `danger`, focus initial « Annuler » ;
  - loading indicators → squelette au premier chargement, `aria-busy` sur le tableau pendant les rechargements, mise en page conservée ;
  - keyboard navigation → tout est atteignable au clavier, boutons dépliants avec `aria-expanded` / `aria-controls`, focus déplacé sur le titre des résultats après un changement de page ;
  - number and date formatting → virgule décimale française, dates en heure du Bénin (`Intl`, `Africa/Porto-Novo`), chiffres tabulaires.
- **Recherche `vue`** → `watch` sur une source précise (la requête de la route) et `computed` pour les états dérivés (droits d'action, lignes du tableau de bord, couverture des rôles).
- **Écarté** : *Bulk Actions* (aucune action de masse sur des dossiers : chaque décision est individuelle, motivée et auditée), graphiques décoratifs, animations d'entrée.

## Composition

- **Tableau de bord** :
  - trois indicateurs-liens (« À vérifier, non pris en charge », « Mes dossiers en vérification », « Corrections en attente du directeur adjoint ») qui ouvrent la liste filtrée ou la file ;
  - tableau filière × statut, chaque nombre étant un lien vers la liste filtrée, avec une ligne de total ;
  - pour le super-administrateur : pas de dossier, mais une alerte « Organisation incomplète » si aucun Directeur adjoint ou Directeur actif.
- **Liste** :
  - formulaire `role="search"` : recherche, filière, série, statut, agent, signalement, tri, case « Correction en attente » ;
  - filtres dans l'URL ;
  - tableau à colonnes fixes (numéro en en-tête de ligne et lien, candidat, série, filière, moyenne, date, statut, agent, signalements) ;
  - `aria-sort` sur la colonne triée ;
  - mention permanente : moyennes tronquées à 2 décimales, la fraction exacte fait foi ;
  - pagination « Précédente / Page n sur N / Suivante » ;
  - nombre de résultats annoncé par une région `status`.
- **Dossier** : sections empilées dans l'ordre du travail de vérification :
  1. vérification (actions permises seulement, motif du blocage de validation lié au bouton par `aria-describedby`) ;
  2. candidat (liste de définitions, série déclarée et lue avec un texte, pas seulement une couleur) ;
  3. moyenne (valeur tronquée en grand, fraction et décimale exactes dessous, moyenne au dépôt si elle diffère) ;
  4. notes (relevé et retenue côte à côte, étiquette « corrigée », méthode en clair, bouton « Preuve » qui ouvre l'image sous le tableau) ;
  5. corrections ;
  6. pièces (aperçu intégré et « Nouvel onglet ») ;
  7. doublons dans les deux sens, avec lien vers l'autre dossier ;
  8. historique en frise chronologique.
- **File des corrections** : une carte par correction (valeurs du relevé, retenue et proposée alignées, auteur, ancienneté relative et date, motif cité), avec « Preuve », « Dossier », « Approuver » et « Refuser ». Aucun bouton de décision sur une proposition de la personne connectée.

## Dérogations motivées

- **Aperçu PDF sans `sandbox`** : décision utilisateur (ADR 117), protégée par les en-têtes de la route elle-même. Iframe de 70 vh (60 vh sur mobile, où le lien « Nouvel onglet » reste la voie conseillée).
- **Statuts de revue** : réutilisent le badge texte + pastille de la phase 7 (`warning` à vérifier, marine en vérification, `success` validé, `destructive` rejeté). Le texte porte toujours l'information.
- **Valeurs corrigées** : étiquette `warning` « corrigée » à côté de la valeur retenue en gras ; la valeur du relevé reste visible dans sa colonne, jamais barrée.

## Mouvement, accessibilité, responsive

- Seules les transitions couleur de 150 ms du master ; aucune animation de chargement autre que le squelette existant ; `prefers-reduced-motion` global inchangé.
- Contrastes AA des tokens existants. Focus visible marine de 3 px sur les liens-indicateurs et les nombres. Libellés visibles inclus dans les noms accessibles (« Preuve de lecture : … », « Corriger : … », « Aperçu : … »).
- Vérifié par Playwright à 375, 768, 1024 et 1440 px (liste, dossier avec aperçu et preuve, file des corrections), sans défilement horizontal.
