# Phase 6 — validation (14 octobre 2026)

**Statut : faite.** Plan validé par l'utilisateur avec ses ajustements (année de numérotation par campagne, compteur par année et filière, moyenne exacte + 30 décimales, drapeaux contre les seules soumissions, down refusé, `showModal()`, statuts administratifs hors `applications`, audit générique, retour à Documents sur `ADMISSION_RULES_CHANGED`, session en lecture seule prolongée). PASS désigne une exécution réelle sur l'hôte avec Docker. Aucun commit effectué.

Contrat : SUBMISSION_WORKFLOW. Décisions : DECISIONS (ADR 083–094). Modèle : DATA_MODEL (phase 6).

## `make check` — 26 étapes, CHECK PASSED

| Étape | Résultat | Détail |
|---|---|---|
| web build / typecheck / lint / format | PASS | Vite, vue-tsc, ESLint, Prettier |
| web Vitest | PASS | 51 tests (5 fichiers), dont 16 nouveaux pour la soumission et la confirmation |
| Go gofmt / vet (+ tag integration) / test / build | PASS | Nouveau module `submissions` : calcul, troncature, numéro, méthodes, HTTP |
| Python pytest (hôte) / Ruff worker + scripts | PASS | 109 passés, 4 ignorés (moteurs réels dans Docker) ; aucun changement Python |
| Compose config, `git diff --check` | PASS | |
| Stack reconstruite, `db up` (00006 appliquée à la base locale), seed ×2 | PASS | 5 services sains |
| Intégration Go (`references-tests`) | PASS | **225** tests et sous-tests (unitaires compris), 0 échec ; `submissions` en compte 46, dont 16 sur PostgreSQL |
| smoke, références, brouillons, documents | PASS | Contrats des phases 1–4 inchangés |
| `ocr-tests` (régression OCR) | PASS | pytest 113/113 ; fixtures **identiques octet pour octet** ; benchmark BAC 18/18, 0 valeur fausse acceptée |
| fixtures intactes, `ocr_smoke` | PASS | Chemin BAC par cellules de bout en bout : `d-photo` SUCCEEDED en 17,4 s, `c-declared-d` `SERIES_MISMATCH` en 9,4 s |
| Playwright | PASS | 2 scénarios (47 s) |

Après le `make check`, une condition morte (`DRAFT_NOT_EDITABLE`, code supprimé côté API) a été retirée de la page Vue. `make check-static` (14 étapes) et `make check-browser` ont été relancés ensuite : tous PASS.

## Tests de la phase

### Go, unitaires et HTTP (`internal/modules/submissions`)

- **Calcul exact**
  - Cas décimaux : 15,50 × 2,5 + 12,25 × 3 + 9 × 1 = 84,5, divisé par 6,5, donne exactement 13 ; 0,1 et 0,2 sans dérive flottante.
  - Valeurs périodiques : 31/3, 2/3 et 202/21 sont **tronquées** à 30 décimales, pas arrondies. Pour 202/21, la 31ᵉ décimale vaut 6 : un arrondi finirait par …048, la valeur stockée finit par …047.
  - **Deux moyennes identiques à 2 décimales mais différentes en interne** : 31/3 et 10,33. Même à 30 décimales, 1/3 et 0,333…3 (trente 3) ont la même colonne décimale : le drapeau d'exactitude et le produit en croix les départagent.
  - Refus : aucune matière, coefficient nul, note hors [0, 20], valeur absente, texte `1e3` ou `1/3`.
- **Numéro** : format, bornes du serial (0 et 1 000 000 refusés), année minimale 2000, code programme invalide refusé.
- **Méthode de validation** : chemin générique `DIRECT_READ`, `ROW_ARITHMETIC`, `TABLE_TOTALS`, cellules reconstruites ; coefficient reconstruit ou index de rangée hors du tableau refusés.
- **HTTP**
  - Le même récépissé est renvoyé deux fois et ne contient jamais `average` ; le cookie est réémis HttpOnly et Secure, prolongé.
  - Réponses vérifiées : Origin absente ou étrangère → 403 ; non-JSON → 415 ; sans session → 401 avec cookie effacé ; version 0, champ inconnu `average` ou données en trop → 400 ; codes métier relayés tels quels ; erreur interne masquée (`SUBMISSION_FAILED`) ; récépissé d'un brouillon → 404.
- **CORS** : le preflight du POST de soumission est accepté (204) ; PATCH et DELETE sont refusés.
- **Documents** : `APPLICATION_LOCKED` sur un dossier soumis, avant la version, pour l'envoi, la liste et le contenu.

### Go, intégration PostgreSQL réelle (schéma isolé, `TestPostgresSubmission`, `…MigrationRoundTrip`)

| Scénario | Vérifié |
|---|---|
| Remplissage de `numbering_year` | Campagne ouverte le 31/12 à 23:30 UTC (1ᵉʳ janvier à 00:30 à Porto-Novo) → 2027 ; année 1999 refusée |
| Chaque précondition | Session inconnue → `DRAFT_SESSION_REQUIRED` ; brouillon vide → `APPLICATION_INCOMPLETE` (422) avec les champs du profil, du parcours, de la filière et des documents ; un document manquant → un seul champ `document.IDENTITY_DOCUMENT` ; analyse en file ou non confirmée → `TRANSCRIPT_ANALYSIS_REQUIRED` ; version périmée → `VERSION_CONFLICT` ; campagne `CLOSED`, puis campagne `OPEN` hors période → `CAMPAIGN_CLOSED` ; offre désactivée → `APPLICATION_NOT_ELIGIBLE` ; relevé remplacé après confirmation → `TRANSCRIPT_ANALYSIS_OUTDATED` ; aucune soumission persistée |
| `ADMISSION_RULES_CHANGED` | Publication simulée d'une grille v2 (triggers de garde désactivés le temps du test) → code renvoyé, ancienne analyse `OBSOLETE` (commitée), nouvelle analyse `QUEUED` sur la même version du relevé et liée à v2, dossier inchangé ; après confirmation, la soumission aboutit avec un snapshot lié à v2 |
| Soumission réussie | Numéro attendu ; `SUBMITTED`, version + 1 ; `weighted_sum` = 76,75, `coefficient_sum` = 6, `average` = 12,791666…666 avec `average_is_exact = false` ; 3 matières `DIRECT_READ`, 4 documents ; **l'analyse reste `SUCCEEDED`/`CONFIRMED`** (correctif de `refresh_transcript_analysis`) ; audit CANDIDATE → APPLICATION dont les métadonnées ne contiennent ni email, ni nom, ni téléphone, ni numéro, ni somme, ni moyenne ; session prolongée jusqu'à la fin de campagne ; rejeu et `GET` identiques |
| Campagne courte | Session prolongée à 7 jours minimum |
| Verrouillage (repository + HTTP réel) | Mise à jour du brouillon, enregistrement de document, lecture et décision d'analyse, liste et contenu des documents → `APPLICATION_LOCKED`. Par HTTP : PATCH profil, parcours et filière, POST multipart, GET documents, contenu et analyse, POST confirm → 409. `GET /current`, `GET /submission`, `POST /drafts` et `POST /submission` → 200 avec le même numéro, aucune nouvelle application créée |
| Immuabilité en base | 15 écritures directes refusées avec le message du bon trigger (application soumise, candidat, snapshot, matières, documents, audit, analyse soumise, insertion d'extraction, version de document, compteur décrémenté ou supprimé) ; TRUNCATE refusé sur 4 tables ; insertion refusée dans l'extraction d'une analyse confirmée en brouillon ; `SUBMITTED` sans numéro (CHECK), `SUBMITTED` sans snapshot (contrainte différée), brouillon numéroté (CHECK) |
| **Concurrence réelle, même dossier** | 8 soumissions lancées au même instant sur 8 connexions → 8 réponses identiques, 7 rejeux, **1 snapshot, 1 événement d'audit** |
| Concurrence, dossiers distincts | 6 soumissions simultanées → 6 numéros distincts et serials contigus |
| **Rollback** | Panne injectée (trigger sur `audit_events`) → dossier `DRAFT`, ni numéro, ni snapshot, ni drapeau, compteur et session inchangés ; nouvel essai → serial suivant, sans trou |
| Pleine précision | 31/3 stockée `10.333333333333333333333333333333` (inexacte), 10,33 stockée `10.330000000000000000000000000000` (exacte) ; `trunc(…, 2)` égales, produit en croix strictement supérieur pour 31/3 |
| Méthodes et série | Preuves par cellules : `ROW_ARITHMETIC`, `TABLE_TOTALS:scale`, `ROW_ARITHMETIC` ; BAC sans série lue → `SERIES_UNREADABLE` ; série « D » lue → aucun drapeau |
| Doublons | A puis B (email en majuscules, même relevé) → B porte `DUPLICATE_EMAIL` et `DUPLICATE_TRANSCRIPT` vers A, A aucun drapeau, alors que C, encore brouillon, partage le téléphone de A ; C soumis ensuite → `DUPLICATE_PHONE` vers A ; rien n'est bloqué |
| Migrations | Down refusé (« rollback refused ») avec soumissions, schéma intact ; schéma neuf avec une analyse en cours : up → down (tables, colonnes et fonction de la phase 5 restaurées) → up (`numbering_year` rempli de nouveau, analyse intacte) |

Les tests d'intégration existants passent tous avec 00006. Les insertions de campagnes de test fournissent désormais `numbering_year`.

### Vitest

- **Page candidat**
  - Récapitulatif complet sans moyenne, action unique.
  - Dialogue : `showModal()` appelé, `show()` jamais ; `alertdialog` décrit ; focus initial sur Annuler ; Escape et Annuler ferment et rendent le focus au bouton.
  - Chargement : `aria-busy`, boutons désactivés, Escape bloqué pendant l'envoi ; succès → route de confirmation.
  - **`ADMISSION_RULES_CHANGED` → étape Documents, focus sur le titre, message, nouvelle analyse « en cours » dans la carte du relevé, récapitulatif bloqué.**
  - Messages par code : `VERSION_CONFLICT`, `CAMPAIGN_CLOSED`, `APPLICATION_NOT_ELIGIBLE` (→ Filière), `TRANSCRIPT_ANALYSIS_OUTDATED` (→ Documents), `NETWORK_ERROR` ; `APPLICATION_INCOMPLETE` → première étape incomplète avec erreur de champ.
  - Dossier `SUBMITTED` restauré → confirmation, sans création de brouillon ni lecture des documents et de l'analyse ; `APPLICATION_LOCKED` venu d'un autre onglet → confirmation.
- **Confirmation** : récépissé relu après refresh (numéro, filière, date « 14 octobre 2026 à 10:30 (heure du Bénin) », conseil, date limite) ou repris du store sans requête ; seuls « Copier » et « Imprimer », aucun champ ; copie annoncée, et son échec aussi ; `window.print` ; dossier non soumis → `/candidature` ; session expirée → message sans relance ; nouvel essai après erreur réseau.

### Playwright (Chromium, stack Docker réelle)

Le parcours complet va jusqu'à `SUBMITTED`, avec le relevé synthétique scanné et l'analyse réelle par le worker :

- récapitulatif sans « moyenne », aux 4 largeurs et au zoom 200 % ;
- dialogue ouvert au clavier, focus initial sur Annuler, Tab reste dans le dialogue, Escape ferme et rend le focus ;
- soumission en 200, URL `/candidature/confirmation`, numéro `IFRI-2000-GL-NNNNNN` ;
- aucun bouton « Modifier » ;
- copie vérifiée dans le presse-papiers ;
- numéro sur une seule ligne à 375 px ;
- capture d'impression à largeur A4, avec boutons masqués.

Après un **refresh**, le même numéro s'affiche ; `/candidature` redirige vers la confirmation. Par l'API du contexte navigateur : PATCH profil → **409 `APPLICATION_LOCKED`**, GET documents → 409, POST de soumission → 200 avec le même numéro. Le scénario de rejet et remplacement reste vert.

Captures inspectées (ignorées par Git, `apps/web/browser-artifacts/`) : `submit-dialog-375`, `confirmation-375`, `confirmation-1440`, `confirmation-print`.

## Critères de la demande

| Exigence | État |
|---|---|
| Préconditions revérifiées dans la transaction, codes distincts | ✅ intégration |
| Calcul exact en Go depuis les valeurs persistées, aucun calcul dans Vue | ✅ unitaires + intégration ; le client ne reçoit jamais la moyenne |
| Somme pondérée, somme des coefficients et moyenne en pleine précision | ✅ fraction exacte + 30 décimales tronquées + exactitude (ADR 085) |
| Snapshot immuable (références + copie par matière + méthode) ; extractions d'une analyse confirmée immuables | ✅ triggers, prouvés par écritures directes |
| Transaction tout ou rien ; `SUBMITTED` ⇒ numéro, date, snapshot | ✅ rollback injecté, CHECK et contrainte différée |
| Numéro unique, concurrency-safe, mécanisme documenté | ✅ ADR 084, 8 + 6 soumissions concurrentes |
| Idempotence (double clic, retry, déjà soumis) | ✅ un numéro, un snapshot |
| Verrouillage backend, session en lecture seule, redirection sans brouillon | ✅ intégration HTTP, Vitest, Playwright |
| Drapeaux non bloquants | ✅ email, téléphone, relevé, série |
| Repli manuel préparé sans migration destructive | ✅ ADR 092 (CHECK conditionnels) |
| Audit `APPLICATION_SUBMITTED` sans PII | ✅ métadonnées vérifiées |
| UI : récapitulatif, dialogue, chargement, erreurs, page de succès imprimable | ✅ Vitest + Playwright + captures |
| Migrations up/down/up | ✅ schéma isolé ; down refusé avec soumissions |
| Régression OCR | ✅ `ocr-tests`, `ocr_smoke`, fixtures intactes |

## Sécurité et confidentialité

- Autorisation : hash du cookie → application → ressource ; aucun identifiant fourni par le client.
- Origin et JSON obligatoires sur le POST ; corps limité à 1 Kio, champs inconnus refusés.
- Journaux et audit : identifiants techniques seulement ; erreurs SQL masquées.
- Après soumission, le cookie ne donne plus accès aux PDF ni aux données extraites.
- Aucun relevé réel utilisé dans cette phase.

## Limites et points ouverts

- **Base locale** : chaque exécution de Playwright ajoute une candidature soumise synthétique à `DEV_LOCAL` (4 après cette validation, 4 numéros distincts, avec les drapeaux de doublon attendus). Comme voulu (ADR 094), `db down` refuse désormais de défaire 00006 sur cette base ; le cycle up/down/up est prouvé en schéma isolé.
- `DEV_LOCAL` numérote en **2000**, son année d'ouverture ; les campagnes réelles devront fixer `numbering_year` (ROADMAP, question 10).
- Seul le cookie donne accès au récépissé ; après sa perte, seul le numéro conservé fait foi. La consultation par numéro + code email est en TODO (phase 10 ou plus tard).
- Le comportement natif de `<dialog>` (inertie, piégeage) est vérifié dans Chromium seulement ; Vitest le simule, faute d'implémentation dans jsdom. Firefox et Safari n'ont pas été testés.
- Lecteur d'écran réel non testé : rôles, noms, régions `status` et focus sont vérifiés par Vitest et Playwright.
- Politique de doublons, seuil du repli manuel et règle d'arrondi ou de départage : questions pour l'IFRI (ROADMAP).
- Corpus réel ≥ 10 relevés toujours requis avant la production (réserve de la phase 5.1).
