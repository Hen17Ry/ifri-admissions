# Phase 8 — validation (14 octobre 2026)

**Statut : faite.** Plan validé par l'utilisateur avec ses ajustements :
- `SELECTION_MANAGER` et `AUDITOR` retirés, `audit.read` ajouté au Directeur en lecture seule ;
- motifs de rejet provisoires définis à un seul endroit et ajoutés aux questions IFRI ;
- retour arrière et motif obligatoire du refus du Directeur adjoint ;
- **un seul rôle métier par compte** (contrôle Go, trigger et test) ;
- preuves OCR sur une file et une boucle séparées, depuis la version exacte et les coordonnées stockées ;
- aperçu PDF intégré sous conditions d'en-têtes, avec un test sur toutes les autres routes.

PASS désigne une exécution réelle sur l'hôte avec Docker. Aucun commit effectué.

Contrat : ADMIN_REVIEW. Décisions : DECISIONS (ADR 109–121). Modèle : DATA_MODEL (phase 8). Design : `design-system/ifri-admissions/pages/admin-review.md`.

## `make check` — 26 étapes, CHECK PASSED

| Étape | Résultat | Détail |
|---|---|---|
| web build / typecheck / lint / format | PASS | Vite, vue-tsc, ESLint, Prettier |
| web Vitest | PASS | 78 tests (7 fichiers), dont 13 nouveaux pour la revue et 14 d'administration mis à jour |
| Go gofmt / vet (+ tag integration) / test / build (`api`, `db`, `ocrvalidate`) | PASS | |
| Python pytest (hôte), Ruff worker et scripts | PASS | 113 passés, 4 ignorés (dont les 4 nouveaux tests de preuves) |
| Compose config (+ profil test), `git diff --check` | PASS | |
| Stack reconstruite, `db up` (**00008** appliquée à la base de dev), seed ×2 | PASS | 5 services sains |
| Intégration Go (`references-tests`) | PASS | **280** tests et sous-tests, 0 échec, PostgreSQL et Redis réels ; génération de 5 000 dossiers en 24,5 s |
| smoke, références, brouillons, documents | PASS | Contrats des phases 1–4 inchangés |
| `ocr-tests` (régression OCR) | PASS | pytest 117/117 dans le conteneur ; fixtures identiques octet pour octet ; benchmark BAC 18/18, 0 valeur fausse acceptée |
| fixtures intactes, `ocr_smoke` | PASS | `d-photo` SUCCEEDED en 10,3 s, `c-declared-d` `SERIES_MISMATCH` en 10,3 s |
| **playwright-e2e** (stack isolée `ifri-e2e`) | PASS | 5 000 dossiers synthétiques en 23,0 s, puis 4 scénarios en 1,4 min, dont le nouveau parcours de revue ; stack détruite ensuite (0 conteneur et 0 volume `ifri-e2e` restants) |

Après le `make check`, la base de développement est en migration 8. Elle contient toujours les 4 soumissions d'avant la phase 7, chacune avec sa revue `TO_REVIEW` créée par le backfill, et 0 compte administrateur : Playwright n'y a rien ajouté.

Deux retouches ont suivi ce passage : les tests unitaires `review/model_test.go` et l'affichage sans zéros inutiles d'une moyenne exacte. `make check-static` et `make check-browser` ont ensuite été relancés : les 14 étapes statiques PASS (Vitest 78/78, tests Go `review` compris) et Playwright PASS (4 scénarios).

## Tests de la phase

### Go, intégration PostgreSQL et Redis réels (`TestPostgresReview`, schéma isolé)

5 000 soumissions synthétiques sont générées dans le schéma de test (environ 22 s), puis :

| Sous-test | Ce qui est prouvé |
|---|---|
| synthetic data passes every submission constraint | Le générateur respecte toutes les contraintes de la phase 6 (snapshot complet, moyenne exacte, numéros) ; une revue `TO_REVIEW` par soumission |
| list: server-side paging, sorting, filters and accent-insensitive search | Pages et total, tris moyenne, date et nom dans les deux sens, filtres filière, série, statut, agent, signalement, correction en attente ; « gbedji » trouve « Gbèdji » (et « Houngbédji ») ; recherche par préfixe de numéro et par email exact (casse ignorée) ; tri inconnu refusé (`INVALID_PARAMETERS`) |
| **list performance on 5000 dossiers** | p50 / p95 sur 20 requêtes chacune (tableau ci-dessous), seuil d'échec à 2 s |
| **roles × actions over HTTP** | Super-administrateur : 403 sur liste, dossier, file et référentiels ; Directeur : lecture des dossiers et du journal, 403 sur toute action ; Directeur adjoint : 403 pour proposer et prendre en charge ; agents : 403 sur la file du DA, sur réattribuer et revenir en arrière ; agent B : `REVIEW_NOT_ASSIGNEE` sur le dossier de A ; version périmée 409 ; rejet sans motif valide 422 ; retour arrière sans motif 422 ; réattribution puis seul le nouvel agent valide ; 6 événements d'historique et 5 événements d'audit |
| **one role per account, checked in Go and by the database** | `SetGrants` avec deux rôles → `VALIDATION_FAILED` ; insertion directe en base d'un second rôle (SUPER_ADMIN, REVIEW_LEAD, DIRECTOR, DEPUTY_DIRECTOR) refusée par le trigger ; rôle retiré `AUDITOR` refusé |
| **concurrent claims and reassignments: one wins, none overwrites** | 9 prises en charge simultanées (3 agents × 3) : une seule réussit, une seule version et un seul événement `CLAIMED` ; réattributions simultanées : une seule appliquée |
| **corrections: proposal, blocked validation, four eyes, exact effective average** | Proposition par un autre agent refusée, note > 20 refusée ; validation refusée avec une correction en attente, **aussi en base** ; le responsable ne décide pas ; l'auteur, même s'il obtenait le rôle d'approbateur, ne peut pas approuver (Go et CHECK en base) ; refus sans motif 422 ; approbation → moyenne effective égale au calcul `big.Rat` attendu, y compris une fraction sans développement fini (10/3) ; snapshot inchangé ; correction décidée non modifiable ; refus d'une correction de coefficient sans effet sur la moyenne ; validation ensuite possible ; file vidée ; audit `CORRECTION_*` |
| **dossier: duplicates both ways, consultations audited, documents and proofs** | Doublon visible `OUTGOING` sur le second dossier et `INCOMING` sur le premier ; chaque ouverture de dossier, de document et de preuve écrit un `ADMIN_DATA_ACCESSED` ; en-têtes du PDF (type, nosniff, inline au nom assaini, no-store, CSP `frame-ancestors 'self'`) ; preuve : demande, job sur le flux de test (contrat v1, version et empreinte exactes), routes internes (401 sans jeton, PNG invalide 422), image servie `image/png` avec `frame-ancestors 'none'` |
| **every admin route keeps frame-ancestors 'none' except the PDF preview** | Parcours du registre : toutes les routes gardent `frame-ancestors 'none'` et `X-Frame-Options: DENY`, sauf la seule route de contenu PDF |
| ranking freeze blocks every review change | Une ligne `ranking_freezes` → `REVIEW_FROZEN` sur l'API et refus en base |
| history and decisions are append-only | UPDATE, DELETE et TRUNCATE refusés sur les événements, DELETE sur les revues et les corrections ; **down de 00008 refusé** dès qu'une revue a vécu |
| dashboards: counters for readers, missing directors for the super-admin | Compteurs filière × statut, corrections en attente, non pris en charge, « mes dossiers » (sans couverture) ; super-administrateur sans compteurs, couverture complète, puis Directeur désactivé → absence signalée |
| `TestPostgresReviewMigrationRoundTrip` | 00008 up → down → up dans un schéma isolé avec des soumissions (seuls des événements `CREATED`) ; backfill des revues des soumissions existantes |

Les tests de la phase 7 (`TestPostgresAdmin`) passent avec les nouveaux rôles (le gestionnaire devient Directeur adjoint, l'auditeur Directeur) ; leurs tests de rollback descendent à la version 6.

#### Performance de la liste (intégration, 5 000 dossiers, 20 requêtes par cas)

| Requête | p50 | p95 |
|---|---|---|
| Date (défaut) | 74,4 ms | 83,2 ms |
| Moyenne décroissante | 75,8 ms | 103,9 ms |
| Nom, page 100 | 113,8 ms | 123,3 ms |
| Recherche sans accents | 73,8 ms | 79,6 ms |
| Préfixe de numéro | 71,3 ms | 80,3 ms |
| Filière + série + statut | 51,3 ms | 54,0 ms |
| Signalement | 75,9 ms | 80,3 ms |

Mesures du test d'intégration pendant le `make check` (conteneur de test, base partagée avec les autres paquets en parallèle). Dans le navigateur, sur la stack e2e (5 001 dossiers), une requête par cas, mesurée par Playwright de l'envoi à la fin de la réponse :

| Requête | Temps | Résultats |
|---|---|---|
| Date | 46 ms | 5 001 |
| Moyenne | 44 ms | 5 001 |
| Nom, page 100 | 117 ms | 5 001 |
| « gbedji » | 68 ms | 416 |
| `IFRI-2026-GL` | 26 ms | 1 090 |
| GL + `TO_REVIEW` | 30 ms | 1 090 |
| Au moins un signalement | 47 ms | 3 327 |

Index utilisés : date de soumission, email, qualification, statut, agent, moyenne effective, (filière, statut) ; `name_key` est filtré par `LIKE` sans index trigram (pas d'extension) : la recherche par nom reste sous 80 ms p95 à ce volume.

### Go, unitaires

- `admin` : permissions exactes des cinq rôles et rôles globaux, registre de routes (toute route a une permission), en-têtes stricts, limitation avant argon2 (phase 7, inchangés).
- `review` (`model_test.go`, nouveau) : nom de fichier PDF assaini (traversée, guillemets, CRLF, vide, 100 caractères, suffixe) ; moyenne affichée **tronquée, jamais arrondie** (199,99 / 10 = 19,999 → « 19.99 »), décimale à 30 chiffres et fraction conservées.

### Python (worker)

`tests/test_proofs.py` : rendu d'une seule rangée avec cellules surlignées ; refus des cellules et pages absentes ; seule la version exacte (empreinte) est envoyée ; refus 4xx acquittés, erreurs de transport relancées. pytest hôte : 113 passés, 4 ignorés ; conteneur avec Tesseract réel : 117 passés.

### Vitest

- `admin.test.ts` (14 tests, mis à jour) : création d'un compte avec **un seul rôle** (boutons radio, choisir un second rôle remplace le premier, aucun choix de filière), tableau de bord d'un agent (menu, compteurs filière × statut, total), alerte de couverture du super-administrateur.
- `review.test.ts` (13 tests, nouveaux) :
  - liste : filtres lus dans l'URL et envoyés à l'API, tri `aria-sort`, pagination, nouvelle recherche en page 1, aucune coordonnée, état vide avec réinitialisation ;
  - dossier : prise en charge puis validation, aucun bouton de décision pour l'agent qui ne détient pas le dossier, validation bloquée par une correction en attente (`disabled` + `aria-describedby`), proposition de correction (valeur obligatoire, valeur brute transmise), rejet exigeant motif et commentaire en `alertdialog`, conflit de version avec rechargement, valeurs du relevé et retenues côte à côte avec moyenne exacte et moyenne au dépôt, aperçu PDF par le flux authentifié et doublons dans les deux sens, preuve OCR demandée puis image privée ;
  - file du DA : approbation avec moyenne recalculée annoncée, refus exigeant un motif, aucune décision proposée sur sa propre proposition.

### Playwright (stack isolée `ifri-e2e`, Chromium)

`infra/scripts/e2e.sh` génère 5 000 dossiers synthétiques (23,0 s) avant les scénarios. `review.spec.ts` :

1. Le super-administrateur (MFA réinitialisée par `api admin reset-mfa`) voit l'alerte « Aucun directeur adjoint actif ».
2. Il crée deux agents et un Directeur adjoint, activés et enrôlés dans leurs propres contextes. L'alerte ne signale plus que l'absence de Directeur ; le super-administrateur reçoit 403 sur les dossiers.
3. Agent A : liste sur 5 001 dossiers (temps ci-dessus), captures aux 4 largeurs sans défilement horizontal ; recherche du dossier réel de `documents.spec.ts` par email ; prise en charge.
4. Agent B : « Pris en charge par Agent A », aucun bouton « Valider » ; un POST direct de validation avec son jeton CSRF → 403 `REVIEW_NOT_ASSIGNEE`.
5. **Aperçu PDF** : la réponse du relevé est `application/pdf`, `nosniff`, `inline; filename=…`, `no-store`, CSP `default-src 'none'; frame-ancestors 'self'` ; l'iframe est affichée ; la route du dossier garde `frame-ancestors 'none'`.
6. **Preuve OCR** de la première matière, générée par le worker réel depuis le relevé scanné soumis : image affichée (largeur naturelle > 100 px) ; captures du dossier aux 4 largeurs.
7. Agent A propose note + 1 avec motif : « Valider le dossier » désactivé, motif du blocage affiché.
8. Directeur adjoint : file « Corrections à décider », preuve affichée dans la carte, captures, approbation → « Correction approuvée. Moyenne retenue du dossier … ».
9. Agent A recharge : « corrigée » sur la note, moyenne affichée différente, « Moyenne au dépôt : … » ; validation → « Validé » ; frise : soumis, pris en charge, correction proposée, correction approuvée, validé. Agent B voit « Validé ».

`admin.spec.ts` est adapté au sélecteur de rôle unique (le second choix décoche le premier). Résultat : 4 scénarios PASS (admin, parcours candidat, extraction refusée, revue), en 1,4 min.

**Non vérifié** : le rendu du PDF **dans** l'iframe. Chromium sans interface de Playwright n'embarque pas de lecteur PDF (cadre blanc sur les captures) ; le test vérifie la requête, les en-têtes et l'iframe, pas l'affichage. À contrôler dans les navigateurs cibles (TODO, phase 10).

### Mémoire (`docker stats` pendant `make e2e`, pic par conteneur)

API e2e 367 Mio (génération de 5 000 dossiers comprise), worker OCR 416 Mio, MinIO 256 Mio, PostgreSQL 130 Mio, Redis 5 Mio : environ 1,2 Gio pour la stack e2e, contre 0,8 Gio en phase 7.

## Base de développement

`make check` reconstruit la stack de développement et applique 00008 à sa base : les 4 soumissions antérieures reçoivent leur revue `TO_REVIEW` (backfill) et aucun compte ne cumulait de rôles. La recréation du volume reste à faire par l'utilisateur (commande dans PHASE7_VALIDATION). Après la recréation, pour essayer la liste :

```sh
docker compose exec api api dev synthetic-applications --count 5000 --confirm
```

## Limites et points ouverts

- **Motifs de rejet provisoires** : à valider par l'IFRI (ROADMAP, question 6).
- **Aperçu PDF** : rendu dans l'iframe non vérifié automatiquement (voir ci-dessus) ; la CSP de production de la SPA (phase 10) doit garder `frame-src` ou `default-src 'self'`.
- **Documents synthétiques** : aucun objet MinIO ; l'aperçu et la preuve répondent 503 `DOCUMENT_UNAVAILABLE` ou échouent proprement pour eux.
- **Recherche** : par sous-chaîne sans index trigram ; `pg_trgm` à réévaluer si les volumes réels dépassent nettement 5 000 dossiers par campagne.
- **Gel** : seul le crochet existe ; la phase 9 écrira `ranking_freezes`.
- **Images de preuve** : conservées dans le bucket privé sans politique de purge (question de conservation IFRI, phase 10).
- **Périmètre par filière** : mécanisme conservé, inutilisé ; l'interface ne le propose plus.
- Corpus réel ≥ 10 relevés toujours requis avant la production (phase 5.1) : les preuves OCR ne montrent que ce que l'analyse a lu.

## Prochaine étape proposée

Phase 9 : classement par filière sur la moyenne effective des dossiers `VALIDATED` et admissibles, départage, proposition selon les quotas, validation par le Directeur, liste d'attente, gel (`ranking_freezes`), exports. À démarrer après accord.
