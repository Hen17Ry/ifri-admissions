# Back-office des candidatures soumises (phase 8)

Contrat de la phase 8 : rôles IFRI, liste et dossier, revue, corrections, preuves OCR, aperçu des PDF et audit. Décisions : ADR 109 à 121 (DECISIONS). Preuves : PHASE8_VALIDATION. Sécurité des comptes et des sessions : ADMIN_SECURITY (phase 7, inchangée).

Hors périmètre : classement, sélection, exports (phase 9), notifications. Le parcours candidat est inchangé.

## Organigramme des rôles

Un compte porte **un seul rôle** (ADR 110). Une personne qui exerce deux fonctions reçoit deux comptes. Aucun rôle n'est limité à une filière (le mécanisme de périmètre de la phase 7 reste disponible, sans usage).

```text
                         Directeur (DIRECTOR)
                lit tous les dossiers et le journal d'audit,
              validera la sélection finale (phase 9), ne modifie rien
                                  │
                     Directeur adjoint (DEPUTY_DIRECTOR)
              lit les dossiers ; seul à approuver ou refuser
                une correction ; ne propose jamais de correction
                                  │
          Responsable du secrétariat exécutif (REVIEW_LEAD)
           droits d'un agent + réattribuer un dossier,
             remettre un dossier validé ou rejeté en vérification
                                  │
             Agents du secrétariat exécutif (REVIEWER)
        prennent les dossiers en charge, les valident ou les rejettent,
               proposent des corrections de note ou de coefficient

   Administrateur technique (SUPER_ADMIN) — hors chaîne métier :
   comptes et journal d'audit, aucun accès aux dossiers des candidats.
```

| Rôle | Libellé | Permissions |
|---|---|---|
| `REVIEWER` | Agent du secrétariat exécutif | `dashboard.view`, `applications.read`, `review.claim`, `review.decide`, `corrections.propose`, `ranking.read` |
| `REVIEW_LEAD` | Responsable du secrétariat exécutif | celles de l'agent + `review.reassign`, `review.revert` |
| `DEPUTY_DIRECTOR` | Directeur adjoint | `dashboard.view`, `applications.read`, `corrections.decide`, `ranking.read` |
| `DIRECTOR` | Directeur | `dashboard.view`, `applications.read`, `audit.read` (lecture seule), `ranking.read`, `selection.validate` (phase 9) |
| `SUPER_ADMIN` | Administrateur technique | `dashboard.view`, `accounts.read`, `accounts.manage`, `audit.read` |

- **Exclusivité** : REVIEWER, REVIEW_LEAD, DEPUTY_DIRECTOR et DIRECTOR sont mutuellement exclusifs ; SUPER_ADMIN est exclusif de tout rôle métier. Contrôle en Go (`resolveGrants`, 422 sur le champ `grants`) et par le trigger `guard_single_role` (verrou de la ligne du compte, puis refus d'un second rôle distinct non révoqué). La migration 00008 refuse de s'appliquer si un compte détient déjà plusieurs rôles.
- **Rôles retirés** : `SELECTION_MANAGER` et `AUDITOR` sont révoqués par 00008 (historique conservé) et ne peuvent plus être attribués. Le classement de la phase 9 sera calculé automatiquement puis validé par le Directeur : aucun rôle de préparation n'est nécessaire.
- **Le Directeur lit l'audit** : le super-administrateur n'est pas le seul lecteur d'un journal qui contient ses propres actions.
- **Ni intérim ni remplacement** : le tableau de bord du super-administrateur signale l'absence d'un Directeur adjoint actif (aucune correction ne peut alors être décidée) ou d'un Directeur actif (`GET /api/v1/admin/accounts/coverage`, et dans `GET /dashboard`).

## Modèle

| Table | Rôle |
|---|---|
| `application_reviews` | Une ligne par candidature soumise, créée par trigger à l'insertion du snapshot (et backfill par 00008) : statut, agent en charge, motif de rejet, `version`, moyenne effective (sommes exactes, décimale tronquée à 30, `effective_average_is_exact`). |
| `application_review_events` | Historique append-only : un événement par version (`CREATED`, `CLAIMED`, `VALIDATED`, `REJECTED`, `REASSIGNED`, `REVERTED`, `CORRECTION_PROPOSED`, `CORRECTION_APPROVED`, `CORRECTION_REFUSED`), acteur, statuts et agents avant/après, motif, commentaire. |
| `review_rejection_reasons` | Liste fermée et **provisoire** des motifs de rejet, avec leurs libellés : seul endroit à modifier (migration de données, sans code). |
| `application_corrections` | Corrections proposées et décidées ; jamais supprimées. |
| `ranking_freezes` | Crochet de gel du classement pour la phase 9 (aucun gel créé en phase 8). |
| `ocr_proof_images` | Une image de preuve par matière extraite : statut, tentatives, clé privée de l'objet. |

`applications.state` reste `SUBMITTED` et le snapshot de soumission n'est jamais modifié (ADR 087, 088).

### Garde-fous en base (migration 00008)

- Chaque mise à jour d'une revue augmente `version` d'exactement 1 et doit avoir son événement de même version (trigger différé `review_has_event`).
- L'événement d'une validation, d'un rejet ou d'une proposition de correction doit avoir pour acteur l'agent en charge (`guard_review_event`).
- Commentaire obligatoire pour tout événement autre que `CREATED`, `CLAIMED`, `VALIDATED` et `CORRECTION_APPROVED` (CHECK).
- Correction : `decided_by IS DISTINCT FROM proposed_by` (quatre yeux), motif obligatoire pour un refus, une seule correction `PENDING` par (dossier, matière, champ), transitions PENDING → APPROVED/REFUSED uniquement (`guard_correction`).
- Moyenne effective recalculée en SQL depuis le snapshot et la dernière correction approuvée de chaque champ, comparée aux colonnes stockées (trigger différé `check_effective_average`, sur les revues et sur les corrections).
- Filière gelée (`ranking_frozen(program)`) : toute écriture de revue ou de correction est refusée.
- Tables append-only : ni DELETE ni TRUNCATE ; le down de 00008 refuse de s'exécuter dès qu'une revue a vécu (événement autre que `CREATED`, correction, preuve, gel ou attribution d'un nouveau rôle).

## Revue d'un dossier

```text
TO_REVIEW ──prise en charge──▶ IN_REVIEW ──validation──▶ VALIDATED
                                   │    └───rejet─────▶ REJECTED
                                   │                        │
                       réattribution (responsable)   retour en vérification (responsable)
                                   ▲────────────────────────┘
```

| Action | Permission | Condition | Saisie |
|---|---|---|---|
| Prendre en charge | `review.claim` | `TO_REVIEW` | — |
| Valider | `review.decide` | `IN_REVIEW`, agent en charge, **aucune correction en attente** (`REVIEW_CORRECTION_PENDING`) | — |
| Rejeter | `review.decide` | `IN_REVIEW`, agent en charge | motif fermé + commentaire |
| Réattribuer | `review.reassign` | `IN_REVIEW` | autre agent (REVIEWER ou REVIEW_LEAD actif) + motif |
| Remettre en vérification | `review.revert` | `VALIDATED` ou `REJECTED` → `IN_REVIEW` uniquement, même agent | motif |

- **Concurrence** : chaque action envoie la `version` affichée ; la ligne de revue est verrouillée (`SELECT … FOR UPDATE`) puis la version comparée : 409 `REVIEW_VERSION_CONFLICT` sinon. Deux prises en charge simultanées : une seule réussit (testé).
- **Gel** : après le gel de la filière (phase 9), 409 `REVIEW_FROZEN` ; réattribution et retour arrière restent possibles jusque-là.
- **Codes** : `REVIEW_NOT_ASSIGNEE` (403), `REVIEW_TRANSITION_INVALID` (409), `VALIDATION_FAILED` (422, champs `reasonCode`, `comment`, `assignee`).

### Motifs de rejet provisoires

`DOCUMENT_ILLEGIBLE` (Document illisible), `DOCUMENT_INVALID` (Document invalide ou erroné), `IDENTITY_MISMATCH` (Identité non concordante), `TRANSCRIPT_INCONSISTENT` (Relevé de notes incohérent), `QUALIFICATION_NOT_ELIGIBLE` (Qualification non éligible), `DUPLICATE_APPLICATION` (Candidature en double), `FRAUD_SUSPECTED` (Suspicion de fraude), `OTHER` (Autre motif). Liste à valider par l'IFRI (ROADMAP, question 6).

## Corrections administratives

- **Proposition** : par l'agent qui détient le dossier (`corrections.propose`, donc REVIEWER ou REVIEW_LEAD), dossier `IN_REVIEW`, avec un motif. Champ `GRADE` (0 à 20, 3 décimales au plus) ou `COEFFICIENT` (positif, 2 décimales au plus) d'une matière du snapshot ; virgule acceptée ; refus si la valeur est déjà la valeur retenue.
- **Décision** : par le Directeur adjoint seul (`corrections.decide`), jamais par l'auteur (quatre yeux : rôle, Go `CORRECTION_FOUR_EYES` et CHECK en base). Le refus exige un motif. La file `/admin/corrections` montre la preuve OCR, la valeur du relevé, la valeur retenue actuelle, la valeur proposée, l'auteur, le motif et l'ancienneté.
- **Effet** : le snapshot n'est jamais modifié. La valeur retenue d'un champ est la dernière correction approuvée, sinon la valeur du snapshot. La moyenne effective est recalculée en fraction exacte (`big.Rat`) et stockée dans la revue, sous contrôle du trigger différé. Le dossier affiche côte à côte valeurs du relevé et valeurs retenues, la moyenne au dépôt et la moyenne retenue.
- **Phase 9** : le classement utilisera la **moyenne effective** (même comparaison que l'ADR 086 : décimale à 30 chiffres puis produit en croix des fractions).

## Liste des dossiers

`GET /api/v1/admin/applications` — pagination et tri côté serveur (25 par page, 100 au plus).

| Paramètre | Valeurs |
|---|---|
| `q` | contient `@` → email exact ; commence par `IFRI-` → préfixe de numéro ; sinon mots du nom et des prénoms, insensibles aux accents et à la casse (tous les mots doivent apparaître) |
| `program`, `qualification` | code de filière, code de série ou diplôme |
| `status` | `TO_REVIEW`, `IN_REVIEW`, `VALIDATED`, `REJECTED` |
| `assignee` | `none`, `me` ou identifiant d'agent |
| `flag` | `ANY`, `DUPLICATE_EMAIL`, `DUPLICATE_PHONE`, `DUPLICATE_TRANSCRIPT`, `SERIES_UNREADABLE` |
| `pendingCorrection` | `true` |
| `sort`, `direction` | `submittedAt` (défaut, décroissant), `average` (moyenne effective), `name` ; `asc` ou `desc` |

- Colonnes : numéro, candidat, série, filière, moyenne (2 décimales **tronquées**, avec la mention que la fraction exacte fait foi), statut, agent, signalements. Email et téléphone seulement dans le dossier.
- Recherche : fonction immuable `ifri_fold` (accents, ligatures œ/æ, casse, espaces), colonne générée `application_submissions.name_key`, aucune extension PostgreSQL.
- Filtres conservés dans l'URL : liens du tableau de bord, retour arrière et rechargement gardent la vue.
- Mesures sur 5 000 dossiers : PHASE8_VALIDATION.

## Dossier

`GET /api/v1/admin/applications/{id}` (audité `ADMIN_DATA_ACCESSED`, `REVIEW_DETAIL`) : identité et coordonnées, diplôme, année, filière, campagne ; série déclarée et série lue sur le relevé ; moyenne au dépôt et moyenne retenue (affichage tronqué, fraction et décimale exactes) ; matières avec note et coefficient du relevé, valeurs retenues, méthode de validation (`DIRECT_READ`, `ROW_ARITHMETIC`, `TABLE_TOTALS`) et cellules reconstruites ; corrections ; pièces (version exacte soumise) ; doublons **dans les deux sens** (`OUTGOING` : ce dossier porte le signalement ; `INCOMING` : un dossier soumis après lui le porte) ; historique complet.

### Aperçu des PDF

`GET …/applications/{id}/documents/{type}/content` sert la version exacte soumise (audité `DOCUMENT:<type>`), en flux authentifié par le cookie de session, avec :

- `Content-Type: application/pdf`, `X-Content-Type-Options: nosniff` ;
- `Content-Disposition: inline; filename="…"` (nom assaini : lettres, chiffres, `.-_` et espace, 100 caractères, suffixe `.pdf`) ;
- `Cache-Control: no-store` ;
- `Content-Security-Policy: default-src 'none'; frame-ancestors 'self'` et `X-Frame-Options: SAMEORIGIN`.

C'est la **seule** route admin qui peut être encadrée, et seulement par la page d'administration elle-même : l'iframe n'a pas de `sandbox` (le lecteur PDF du navigateur n'y fonctionne pas). Toutes les autres routes gardent `frame-ancestors 'none'` et `X-Frame-Options: DENY` (test qui parcourt le registre). Un lien « Nouvel onglet » ouvre la même route (petits écrans).

### Preuves OCR

- `POST …/subjects/{subject}/proof` demande l'image (202), `GET …/proof` donne l'état (`NONE`, `PENDING`, `READY`, `FAILED`), `GET …/proof/image` sert le PNG (audité `OCR_PROOF:<matière>`). Aucune URL publique ni pré-signée.
- Génération à la demande, avec cache : une ligne `ocr_proof_images` par matière extraite. Un échec peut être redemandé jusqu'à 3 tentatives ; une boucle de l'API, toutes les 5 s, redistribue une demande `PENDING` jamais envoyée ou envoyée depuis plus de 2 min, et la passe `FAILED` (`RENDER_FAILED`) après 10 min.
- **File séparée** `ifri:ocr:proofs:v1` (groupe `ifri-ocr-proofs-v1`), traitée par une **boucle dédiée du worker** avec sa propre connexion Redis : une preuve n'attend jamais derrière les analyses des candidats.
- Contrat v1 : `proofId`, `analysisId`, `documentVersionId`, `documentSha256`, `pageNumber`, `evidence` (coordonnées stockées de l'analyse soumise). Le worker télécharge le PDF par `GET /internal/proofs/{id}/document` (port interne 8081, jeton OCR), vérifie le SHA-256, rend la page à 150 DPI, découpe la bande de la rangée, surligne note, coefficient, points et « sur » (ambre) et entoure les autres cellules (marine), puis envoie le PNG (`/image`) ou un code d'échec (`/failure` : `DOCUMENT_UNAVAILABLE`, `PAGE_MISSING`, `RENDER_FAILED`).
- L'API n'accepte qu'un PNG valide de 2 Mio et 4 000 px au plus pour une preuve `PENDING`, le stocke sous `applications/<application>/proofs/<proof>.png` dans le bucket privé, puis la passe `READY`.

## Tableau de bord

`GET /api/v1/admin/dashboard` : compteurs par filière × statut, corrections en attente, dossiers « À vérifier » non pris en charge, dossiers de l'agent en vérification (agents), et alerte de couverture pour le super-administrateur. Chaque compteur mène à la liste filtrée.

## Audit

- `RecordAccess` (`ADMIN_DATA_ACCESSED`) à chaque ouverture de dossier, de document et de preuve, dans la transaction de lecture.
- Actions : `REVIEW_CLAIMED`, `REVIEW_VALIDATED`, `REVIEW_REJECTED`, `REVIEW_REASSIGNED`, `REVIEW_REVERTED`, `CORRECTION_PROPOSED`, `CORRECTION_APPROVED`, `CORRECTION_REFUSED` (métadonnées techniques : version, codes, identifiants ; aucune note ni donnée personnelle), `SYNTHETIC_DATA_GENERATED`.

## Données synthétiques

```sh
docker compose exec api api dev synthetic-applications --count 5000 --confirm   # campagne DEV_LOCAL par défaut
```

Refusé en production ; `--confirm` obligatoire. Noms accentués synthétiques, emails `synthetic-N@example.invalid`, téléphones `+22990…`, drapeaux périodiques, moyennes exactes, aucune donnée personnelle. Les soumissions sont immuables : dans une base de développement elles restent jusqu'à la recréation de son volume. Les documents synthétiques n'ont pas d'objet dans MinIO (aperçu et preuve indisponibles pour eux). `infra/scripts/e2e.sh` en génère 5 000 (`E2E_SYNTHETIC_COUNT`) sur la stack jetable.

## Interface

`/admin/candidatures` (liste), `/admin/candidatures/:id` (dossier), `/admin/corrections` (file du Directeur adjoint) ; entrées de navigation filtrées par permission, pour l'affichage seulement (l'API décide). Composition et adaptations UI UX Pro Max : `design-system/ifri-admissions/pages/admin-review.md`.
