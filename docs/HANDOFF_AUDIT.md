# Audit de reprise — IFRI Admissions

Date : 14 octobre 2026. Auteur : Claude Code, reprise d'un projet démarré avec un autre agent.

> Instantané d'audit avant la clôture de la phase 5.1. L'état 5.1 à jour est dans `PHASE5_1_VALIDATION.md` et `OCR_BENCHMARK.md` ; les écarts avec le cahier des charges et la liste des questions IFRI restent valables.

Périmètre : lecture de `CLAUDE.md`, `AGENTS.md`, `docs/CAHIER_DES_CHARGES.pdf`, `docs/ROADMAP.md`, de tous les `docs/PHASE*_VALIDATION.md` et des documents de référence ; inspection de l'état réel (git, migrations, modules Go, worker Python, wizard Vue) ; exécution de toutes les suites de validation. **Aucun code n'a été modifié** : ce fichier est la seule écriture dans le dépôt. Toutes les mentions PASS/FAIL ci-dessous correspondent à des exécutions réelles faites pendant cet audit, sauf mention « lecture de code ».

---

## 1. Synthèse

- **Phases 1 à 5 : solides.** Frontend, Go (unitaires et intégration PostgreSQL/Redis/MinIO/qpdf), smoke tests HTTP des phases 1 à 5 et Playwright passent sur la stack Docker reconstruite.
- **Phase 5.1, volet UX : fait.** Plus de bouton « Analyser », wizard en 5 étapes, analyse automatique à l'upload, affichée dans la carte du relevé, restauration au refresh. Vérifié par Vitest, Playwright et inspection des captures.
- **Phase 5.1, volet OCR réel : pas terminé.** Le relevé réel privé (BAC D scanné) échoue proprement (`NEEDS_REVIEW`, aucune valeur fausse), mais la lecture des cellules reste très insuffisante. La suite Docker `ocr-tests` est **rouge** : le nouveau pipeline `BAC_CELL_GRID` fait régresser 2 des 3 scans BAC synthétiques. Ruff est **rouge** sur un module du worker et sur 5 scripts.
- **Phases 6 à 10 : rien n'existe.** Moyenne, soumission, numéro, administration, classement, sélection, audit, Nginx/HTTPS, antivirus, sauvegardes. C'est environ la moitié du cahier des charges.

---

## 2. État réel du dépôt

| Élément | Constat |
|---|---|
| Git | Branche `master`, **aucun remote** (la CI GitHub n'a donc jamais tourné). 6 commits ; le dernier, `44c0576 wip: phase 5.1 OCR calibration` (14/10 01:17), contient tout le travail des phases 5 et 5.1 en un seul commit WIP. |
| Non commité | `docs/ROADMAP.md` réécrit (nouvelle numérotation 1–10 + 5.1, critères de sortie 5.1, questions IFRI) ; `CLAUDE.md` et `docs/CAHIER_DES_CHARGES.pdf` non suivis. |
| Migrations | `00001_references` → `00005_ocr_analyses`. Base locale à la version 5. Aucune migration ajoutée en 5.1 (conforme à la doc). |
| Modules Go | `references`, `drafts`, `documents`, `analysis` + plateforme (`config`, `database`, `httpserver`, `pdfcheck`, `storage`). **Absents** : identité/admin, vérification, classement, audit (prévus dans `ARCHITECTURE.md`). |
| Worker Python | `worker`, `jobs` (Redis + callbacks internes), `runner` (sous-processus isolé), `extraction` (natif PyMuPDF + matching), `scans` (chemin scan générique multi-passes), `cells` (chemin `BAC_CELL_GRID`), `layouts`, `preprocessing`. |
| Wizard Vue | `/candidature`, 5 étapes : Vos informations → Votre parcours → Filière souhaitée → Documents → Vérification. `AnalysisStep.vue` est injecté dans la carte TRANSCRIPT via le slot `transcript-analysis`. |
| Outils hôte | Go 1.27.1 (Docker/CI 1.27.2), Node 24.18.1, npm 11.16.0, uv 0.12.3 (CI 0.13.0). Tesseract et qpdf ne sont disponibles que dans les images Docker. |
| Données privées | `services/ocr/tests/private-fixtures/` contient `Relever.pdf` (lien symbolique vers un dossier personnel hors dépôt) et `oracle.json`. Ignoré par Git et par les contextes Docker. |

---

## 3. Résultats de la validation exécutée

| Contrôle | Commande | Résultat |
|---|---|---|
| Build Vue | `npm run build` | PASS |
| Typecheck | `npm run typecheck` | PASS |
| ESLint | `npm run lint` | PASS |
| Vitest | `npm run test` | PASS — 30/30, 4 fichiers |
| Prettier | `npm run format:check` | PASS |
| gofmt | `gofmt -l .` | PASS (aucune sortie) |
| go vet | `go vet ./...` et `-tags=integration` | PASS |
| go test (unitaires) | `go test -count=1 ./...` | PASS — 6 paquets |
| go build | `go build ./cmd/api` | PASS |
| pytest hôte | `uv run --locked pytest` | PASS — 74/74 (moteur simulé, pas de Tesseract sur l'hôte) |
| Ruff check worker | `ruff check .` (services/ocr) | PASS |
| **Ruff format worker** | `ruff format --check .` | **FAIL** — `src/ifri_ocr/layouts.py:73` (ligne vide manquante) |
| **Ruff check scripts** | `ruff check --config services/ocr/pyproject.toml infra/scripts` | **FAIL** — 6 × E501 (`ocr_smoke.py` ×5, `ocr_fixtures.py` ×1) |
| **Ruff format scripts** | idem `format --check` | **FAIL** — 5 fichiers : `ocr_fixtures.py`, `ocr_private_calibration.py`, `ocr_private_run.py`, `ocr_scan_benchmark.py`, `ocr_smoke.py` |
| git diff --check | | PASS |
| Compose config | `docker compose config --quiet` (+ profil test) | PASS |
| Stack Docker | `docker compose up -d --build --wait` | PASS — 78 s, 5 services healthy, `minio-init` Exited (0) |
| Migrations | `docker compose exec -T api db up` | PASS — versions 1 à 5 appliquées |
| Intégration Go | `docker compose --profile test run --build --rm references-tests` | PASS — 156 sous-tests, 0 échec, 0 skip, 89 s. PostgreSQL, Redis, MinIO et qpdf réels ; Up/Down/Up de 00005 en schéma isolé |
| Smoke phase 1 | `infra/scripts/smoke.py` | PASS |
| Smoke phase 2 | `references_smoke.py` | PASS |
| Smoke phase 3 | `drafts_smoke.py` | PASS |
| Smoke phase 4 | `documents_smoke.py` | PASS |
| Smoke phase 5 | `ocr_smoke.py` | PASS — 32 s ; 6 couples QUEUED→PROCESSING→SUCCEEDED en 3,4–4,3 s ; rejet, remplacement scan, 409/401/404 |
| pytest Docker (Tesseract 5.3.0 réel) | image `ocr-tests` | PASS — 74/74 en 10,7 s |
| Corpus synthétique (20) | `ocr_fixtures.py` | PASS — 6 textes et 6 scans exacts (scans 3,9–4,8 s) ; 8 cas négatifs refusés comme attendu |
| **Benchmark BAC synthétique** | `ocr_scan_benchmark.py` | **FAIL** — voir §5.2 |
| Playwright | `npm run test:browser` | PASS — 2/2 en 1,0 min (parcours complet 4 largeurs + zoom 200 % ; rejet → remplacement → nouvelle analyse) |
| **Relevé réel privé** | `infra/scripts/ocr_private_run.py` | **FAIL propre** — `LOW_CONFIDENCE / NEEDS_REVIEW`, 28,7 s, aucune valeur renvoyée |
| CI GitHub | | NON TESTÉ — pas de remote |

Remarques d'exécution :
- La commande officielle `docker compose --profile test run ocr-tests` **réécrit 24 PDF versionnés et `scan-benchmark.json`** dans `services/ocr/tests/fixtures` (montage en écriture, octets non déterministes). Pour ne rien modifier, la même image a été lancée avec `docker run`, mêmes variables, même réseau, `/artifacts` redirigé vers un répertoire temporaire. Le contenu est équivalent ; seule la destination diffère.
- Le run privé a produit 82 images de debug du relevé réel dans `/tmp/ifri-ocr-private-debug-*` (hors dépôt, comme prévu par le script). **À supprimer après usage.** Le log contenant les valeurs réelles a été supprimé ; ce rapport n'en reprend que des indicateurs structurels.
- Les captures Playwright (`apps/web/browser-artifacts/`, ignoré par Git) ont été inspectées à 1440 et 375 px pour l'étape Documents.

---

## 4. État par phase

Numérotation de la nouvelle `ROADMAP.md` (non commitée). L'ancienne ROADMAP de l'agent précédent (phases 0–11) ne correspondait pas aux phases réellement exécutées ; les fichiers `PHASE*_VALIDATION.md` suivent déjà la nouvelle numérotation.

| Phase | Déclaré | Constaté | Réserves |
|---|---|---|---|
| 1 — Fondations | Fait | **Confirmé** : build, stack healthy, smoke | CI jamais exécutée (pas de remote) ; la CI ne couvre ni `ocr-tests`, ni les smoke documents/OCR, ni Playwright ; Nginx réduit à `infra/nginx/README.md` ; digests d'images non relevés |
| 2 — Référentiels | Fait | **Confirmé** : intégration + `references_smoke` | Campagne `DEV_LOCAL` non officielle ; quotas NULL ; matrice issue d'une instruction utilisateur, pas d'un document IFRI |
| 3 — Brouillon | Fait | **Confirmé** : intégration + `drafts_smoke` + Playwright | Session anonyme par cookie, sans compte ni récupération depuis un autre appareil |
| 4 — Documents | Fait | **Confirmé** : intégration S3/qpdf + `documents_smoke` + Playwright | Pas d'antivirus ni de quarantaine, pas de réconciliation des objets orphelins, pas de politique de conservation |
| 5 — OCR synthétique | Fait | **Confirmé** : `ocr_smoke`, 20 fixtures, Redis/replay en intégration | Seuil 0,85 non calibré ; fixtures 100 % synthétiques |
| 5.1 — Analyse auto + OCR réel | En cours | **UX : fait. OCR réel : en échec. Suites : rouges (Ruff, benchmark)** | Voir §5 |
| 6 — Moyenne, soumission | À faire | Rien : aucune route de soumission (testé par `TestNoUUIDAccessAndNoSubmission`), aucun calcul | — |
| 7 — Admin, RBAC, audit | À faire | Rien | — |
| 8 — Back-office | À faire | Rien | — |
| 9 — Classement, sélection | À faire | Rien | — |
| 10 — Production | À faire | Rien | — |

---

## 5. Phase 5.1 en détail

### 5.1 UX automatique — faite

Preuves :
- **Code.** Aucun bouton ni endpoint de lancement. La DocumentVersion TRANSCRIPT déclenche `refresh_transcript_analysis` (trigger différé de 00005) qui crée l'analyse QUEUED du contexte exact. Le dispatcher Go publie dans Redis.
- **Interface.** `DocumentsStep.vue` rend `AnalysisStep` sous la carte du relevé. Polling au montage pour QUEUED/PROCESSING, suspendu pendant un autre upload, nettoyé au démontage.
- **Tests.** Vitest (absence du bouton, 5 étapes, lecture seule, restauration) ; Playwright réel : upload scan → analyse automatique → refresh → confirmation, puis rejet → remplacement → nouvelle analyse.
- **Captures.** 5 étapes visibles ; tableau Matière/Note/Coefficient en lecture seule ; boutons « Oui, les informations sont correctes » / « Non, les informations sont incorrectes » ; Vérification bloquée tant que les 4 documents ne sont pas fournis.

Réserves mineures :
- Le polling s'arrête après 60 requêtes (environ 8 min) sans message si l'analyse est toujours en cours.
- À 375 px, l'en-tête « Coefficient » se coupe en plein mot. Les notes s'affichent avec un point (`15.50`), alors que le cahier utilise la virgule (`17,42`). Le sélecteur natif affiche « Choose File / No file chosen » malgré la locale fr-FR.
- Une note dérivée de Points ÷ Coeff (mode `POINTS_TOTALS`) est présentée comme « extraite du PDF », sans mention de son mode de calcul.
- Le composant s'appelle toujours `AnalysisStep` alors que l'étape n'existe plus.
- Le scénario Playwright utilise `GL-DT_DWM-scan.pdf` (profil générique) : **le profil BAC n'est couvert par aucun test E2E.**

### 5.2 Pipeline `BAC_CELL_GRID` — implémenté, non validé

Ce qui existe : quand le moteur est Tesseract, `extract_scan` essaie d'abord `extract_bac_cells`. Si le profil n'est pas BAC général, la fonction renvoie `None` et le chemin scan générique historique prend le relais.

Étapes du chemin BAC :
1. Rendu 300 DPI, correction géométrique prudente, binarisation du canal V.
2. Ancres PSM 11, bornes de la section « Épreuves écrites du premier groupe ».
3. Grille à 5 colonnes : axes de bordure, sinon ratios du profil (`PROFILE_RATIOS`).
4. Rangées par projection, avec une plausibilité **codée en dur de 5 à 12 rangées de données**.
5. Par cellule : Otsu, repli canal rouge pour Points, suppression des traits ; Tesseract PSM 7 avec whitelist numérique.
6. Libellés rapprochés d'un vocabulaire fermé de 9 matières.
7. Contrôles Sur = 20 × Coeff et Points = Note × Coeff, puis Σ Coeff, Σ Points et Σ Sur comparées à la ligne Total.
8. Si les trois sommes concordent : note = Points ÷ Coeff (`POINTS_TOTALS`), lecture directe conservée.
9. Lecture de la série (whitelist ABCDE) ; preuves normalisées.

Go revérifie l'arithmétique en rationnels exacts (`validArithmetic`) avant d'accepter une extraction à faible confiance. Tests unitaires Python (`test_cells.py`, 27 cas) et Go (`TestCellArithmetic…`, 12 cas) : PASS.

**Benchmark BAC synthétique (Tesseract réel, Docker)**

| Fixture | `scan-benchmark.json` versionné | Mesure de cet audit | Chemin réellement emprunté |
|---|---|---|---|
| sections-text | SUCCEEDED, exact | SUCCEEDED, exact, 0,015 s | Natif |
| security-png | SUCCEEDED, exact | **LOW_CONFIDENCE / ROW_SEGMENTATION_FAILED**, 2,8 s | `BAC_CELL_GRID` |
| security-jpeg-photo | SUCCEEDED, exact | SUCCEEDED, exact, 5,5 s | **Générique historique** (aucune entrée `BAC_CELL_GRID`) |
| security-skew | SUCCEEDED, exact | **LOW_CONFIDENCE / ROW_SEGMENTATION_FAILED**, 3,1 s | `BAC_CELL_GRID` |

Cause, confirmée sur les fixtures versionnées : ces tableaux synthétiques ont 4 rangées de données (3 matières + Total), alors que `cells.py:172/204` en exige 5 à 12. Les colonnes retombent aussi sur `PROFILE_RATIOS`. **Aucune fixture synthétique ne valide aujourd'hui le chemin `BAC_CELL_GRID` de bout en bout.** Le seul succès BAC scanné passe par l'ancien chemin. Le JSON versionné date d'avant ce pipeline.

**Relevé réel privé (BAC D scanné, une page image)** — indicateurs structurels uniquement :

| Indicateur | Valeur |
|---|---|
| Statut | `LOW_CONFIDENCE`, failureCode `NEEDS_REVIEW`, 0 matière renvoyée, aucune valeur fausse |
| Durée | 28,7 s au total, dont 28,0 s pour le passage cellules (budget calibration 120 s/page ; **production : 30 s/page**) |
| Segmentation | 8 rangées (7 matières + Total), séparation du tableau EPS correcte ; colonnes en repli `PROFILE_RATIOS` (axes de bordure non trouvés) |
| Libellés reconnus (vocabulaire fermé) | **1 sur 7** |
| Coefficients validés (Sur = 20 × Coeff) | 3 sur 7 |
| Produits vérifiés (Points = Note × Coeff) | 2 sur 7 |
| Sommes Σ Coeff / Σ Points / Σ Sur | 0 sur 3 (non évaluées faute de coefficients tous validés) |
| Série | Non lue |
| Erreurs de crop / budget épuisé | Aucune |

Diagnostic : la segmentation n'est plus le goulot ; **c'est la lecture OCR des cellules**, surtout la colonne Matière. Or le code exige que **toutes** les rangées soient `ACCEPTED`, y compris les matières qu'IFRI n'utilise pas (philosophie, SVT…). Un seul libellé illisible bloque donc tout le relevé. Les historiques de `PHASE5_1_VALIDATION.md` montrent environ 10 itérations de correction sur ce document, toutes en échec.

**Critères de sortie 5.1 (ROADMAP)**

| Critère | État |
|---|---|
| Plus de bouton, 5 étapes, analyse dans la carte | ✅ Fait, vérifié |
| Analyse automatique, refresh → restauration + polling | ✅ Fait, vérifié |
| Profil `BENIN_BAC_GENERAL` : section écrite, grille, ligne Total | ⚠️ Implémenté ; échoue sur synthétique (règle 5–12) et réel (lecture) |
| Règle d'acceptation Points ou totaux, sinon `NEEDS_REVIEW` | ✅ Implémentée en Python et revérifiée en Go (voir lacunes ci-dessous) |
| Relevé réel : bonnes valeurs ou échec propre | ⚠️ Échec propre seulement ; aucune valeur obtenue |
| Aucune valeur fausse sur le corpus privé | ⚠️ Vrai sur 1 document : non significatif |
| Corpus ≥ 10 relevés réels anonymisés | ❌ 1 sur 10, dépend de l'IFRI |

### 5.3 Lacunes de sûreté relevées (lecture de code, non reproduites)

1. **Go ne vérifie pas la cohérence entre matières.** Chaque extraction est validée isolément. Rien n'interdit que deux matières pointent vers le même `arithmeticRowIndex`, ni que leurs copies de `arithmeticRows`/`totalCells` diffèrent (`model.go`, boucle `Validate`). Un bug Python d'appariement serait accepté.
2. **La série lue n'est jamais comparée à la qualification déclarée.** Le job Redis ne transporte que les matières attendues ; Go vérifie seulement que la série est une lettre A–E. BAC C et BAC D ont les mêmes matières attendues, donc une erreur de déclaration passe inaperçue. Le chemin générique ne lit aucune série. Le cahier (§8) demande l'« identification de la série du candidat ».
3. **Le vocabulaire fermé (`cells.py:23`) ne contient pas « Étude de fabrication ».** Si le relevé BAC E suit ce format, il ne peut jamais réussir.
4. **Un callback rejeté par Go reste silencieux et bloque longtemps.** Go répond 422 sans journaliser le motif. Le worker ne fait pas d'ACK ; le message est repris après 5 min, jusqu'à 3 tentatives. Le dispatcher finit par marquer `FAILED / ANALYSIS_TIMEOUT`. Toute dérive de contrat Python/Go se traduit par environ 15 min d'« analyse en cours » puis un motif trompeur.
5. **La calibration privée contourne Go.** `ocr_private_calibration.py` appelle `analyze()` directement : un `exactValues: true` ne prouverait pas que le callback serait accepté.
6. **Budget de temps.** 28 s mesurées avec 120 s de budget ; la production est à 30 s par page. Le chemin pourrait être tronqué en production.
7. **Note directe ignorée en mode `POINTS_TOTALS`.** La divergence entre note lue et note dérivée est seulement journalisée. Le risque résiduel (erreurs de lecture qui se compensent entre cellules Points et Total) est faible mais non nul. Décision produit à confirmer.

---

## 6. Écarts avec le cahier des charges

Légende : ✅ couvert · ⚠️ partiel · ❌ manquant · ↔ divergent (choix assumé)

| § | Exigence | État | Commentaire |
|---|---|---|---|
| 1 | Collecter les candidatures | ⚠️ | Brouillons seulement ; aucune soumission |
| 1 | PDF obligatoires | ✅ | 4 types, versionnés, privés |
| 1 | Identifier les matières, extraire note et coefficient | ⚠️ | Fiable sur synthétique (texte et scan) ; échoue sur le seul relevé réel |
| 1 | Moyenne pondérée | ❌ | Phase 6 ; fondations prêtes (NUMERIC, `big.Rat`) |
| 1 | Contrôle admin, classement | ❌ | Phases 7–9 |
| 2 | Candidat : infos, série, filière, upload, consulter, confirmer, remplacer | ✅ | Lecture seule garantie côté API |
| 2 | Candidat : soumettre définitivement | ❌ | Phase 6 |
| 2 | Administrateur : toutes les capacités | ❌ | Aucun espace admin |
| 3 | Nom, prénoms, email, téléphone, série, année, filière | ✅ | Email non vérifié (pas de preuve de possession) ; téléphone au format E.164 international |
| 4 | 5 filières | ✅ | IA, SI, GL, IM, SEIOT |
| 5 | 4 documents PDF | ✅ | Contrôle qpdf, chiffrés et JavaScript refusés ; **pas d'antivirus** |
| 6 | Matières = série + filière | ✅ | Grilles versionnées par offre, publiées immuables |
| 7 | Matrice | ↔ | Conforme aux combinaisons du cahier, interprétées ainsi : C/D = Anglais, E = Étude de fabrication. **Ajout hors cahier** : DT/PM pour IM (Projet informatique, Français, Édition), sur instruction utilisateur. Toute autre série (A, B, F, G…) est bloquée `RULE_NOT_DEFINED`. Le cahier parle d'un « exemple » : aucune grille officielle |
| 8 | Type de PDF, texte/OCR, matières, extraction, cohérence | ✅ | Natif PyMuPDF + Tesseract + regex + normalisation + règles |
| 8 | Identification de la série du candidat | ⚠️ | Lue seulement par `BAC_CELL_GRID`, jamais comparée à la déclaration |
| 8 | Calcul de la moyenne en fin de chaîne | ❌ | Phase 6 |
| 9 | Lecture seule, deux choix, nouveau relevé | ✅ | Libellés exacts du cahier ; refus → nouvelle version obligatoire |
| 10 | Moyenne Σ(note × coef) / Σ coef depuis le relevé | ❌ | Phase 6 |
| 11 | Réussie / partielle / impossible | ✅ | `SUCCEEDED` / `LOW_CONFIDENCE`·`NEEDS_REVIEW` / `FAILED`. Revue manuelle après échecs répétés non prévue : avec l'OCR réel actuel, tout candidat scanné serait bloqué |
| 12 | Conditions de soumission, numéro unique | ❌ | `verificationReady` calcule déjà 4 documents + analyse confirmée ; ni soumission ni numéro |
| 13 | Tableau de bord admin | ❌ | — |
| 14 | Fiche dossier, prévisualisation admin | ❌ | Prévisualisation authentifiée côté candidat seulement |
| 15 | Classement par filière, proposition selon quotas, validation admin | ❌ | — |
| 16 | Statuts BROUILLON…SELECTIONNE | ↔ | Axes séparés (ADR 008) : `applications.state` (DRAFT/SUBMITTED/CANCELLED) + `ocr_analyses.status` + `candidate_confirmation`. Correspondance avec les statuts du cahier à définir pour l'affichage. Noms différents de ceux de `PRODUCT_SPEC.md` |
| 17 | Vue/TS/Vite/Pinia/Router, Go REST, PostgreSQL, Python/PyMuPDF/OCR, MinIO, Redis, Docker | ✅ | Tesseract retenu ; PaddleOCR non évalué |
| 17 | Nginx | ❌ | README seulement |
| 18 | — | ? | **Section absente du PDF** (numérotation 17 → 19) |
| 19 | Authentification admin | ❌ | — |
| 19 | Contrôle des droits | ⚠️ | Côté candidat : session → candidature → ressource, sans IDOR (testé). Aucun RBAC |
| 19 | PDF non publics | ✅ | Bucket privé, proxy Go, pas d'URL présignée |
| 19 | HTTPS | ⚠️ | Exigé par la config Go en production ; aucun reverse proxy TLS |
| 19 | Validation stricte des fichiers | ⚠️ | Signature, taille, qpdf, chiffrement, actions actives ; pas d'antivirus ni de limite de pages côté API |
| 19 | Journalisation admin, historique des validations | ⚠️ | Versions de documents et tentatives OCR immuables ; aucune table d'audit |
| 19 | Sauvegardes | ❌ | — |

---

## 7. Dette technique

**Qualité et CI**
- Ruff rouge : `layouts.py` + 5 scripts OCR (voir §3). La CI échouerait sur le job worker.
- La CI n'a jamais tourné (pas de remote). Elle ne couvre ni `ocr-tests`, ni `documents_smoke`/`ocr_smoke`, ni Playwright. Versions d'outils divergentes entre hôte et CI (uv 0.12.3/0.13.0, Go 1.27.1/1.27.2).
- `ocr-tests` réécrit des fixtures versionnées à chaque exécution (octets non déterministes) et mélange entrées de test et résultats de benchmark dans `tests/fixtures/`.
- Tout le travail des phases 5 et 5.1 tient dans un commit unique `wip`.

**Documentation**
- Documents rédigés par ajouts successifs, avec contradictions :
  - `AGENTS.md` l.58 « Notes/coefs jamais dérivés de Points/Sur » contre l.62, qui autorise la dérivation ;
  - `OCR_PIPELINE.md` « aucune note ni coefficient déduit des totaux » contre sa section finale ;
  - règle d'acceptation réécrite trois fois.
- Le paragraphe « Stratégie BAC général par cellules » est copié à l'identique dans 5 documents (TODO, DOCUMENT_WORKFLOW, OCR_PIPELINE, OCR_BENCHMARK, PHASE5_1_VALIDATION).
- `PHASE5_1_VALIDATION.md` est périmé : 29 Vitest au lieu de 30, Playwright « NON TESTÉ » alors qu'il passe, aucune mention de la régression du benchmark.
- `CANDIDATE_FLOW.md` décrit encore 6 étapes dans sa section phase 5. Les états de `PRODUCT_SPEC.md` (`OCR_PROCESSING`, `AWAITING_CONFIRMATION`, `MANUAL_REVIEW`…) ne correspondent pas au schéma.
- La nouvelle ROADMAP n'est pas commitée.

**Code**
- SQL directement dans les handlers internes (`/claim`, `/document` dans `analysis/http.go`), contrairement à la règle transport → cas d'usage → domaine.
- `decimal()` recompile une regex à chaque appel ; `count(*)=4` codé en dur pour les documents requis.
- Constantes empiriques non rattachées à un profil configurable dans `cells.py` : 5–12 rangées, densité 0,45, ratios de colonnes, facteur 2,5.
- Bornes « Coeff entier 1..6 » codées en dur en Python et en Go : c'est une hypothèse sur les relevés, pas une règle IFRI.
- Deux stratégies scan coexistent (multi-passes générique et cellules), donc deux surfaces à maintenir.
- `OCR_MIN_CONFIDENCE = 0,85` jamais calibré.

**Sécurité et production**
- Manquent : antivirus/quarantaine, Nginx/TLS, comptes admin/RBAC/MFA, table d'audit, sauvegardes testées, conservation/suppression, réconciliation S3.
- Rate limiting en mémoire de processus.
- MinIO Community archivé (ADR 021) : choisir le stockage S3 de production.
- Le cookie de brouillon est un jeton porteur sans récupération.

**Données privées**
- Images de debug réelles dans `/tmp` à chaque calibration.
- La fixture privée est un lien symbolique vers un dossier personnel : rien n'empêche techniquement un `docker build` d'un contexte plus large de la suivre (les `.dockerignore` actuels l'excluent).

---

## 8. Plan priorisé pour terminer la 5.1

**P0 — Remettre les suites au vert (≈ ½ jour)**
1. `ruff format` sur `layouts.py` et les 5 scripts ; corriger les 6 E501.
2. Rendre les fixtures BAC synthétiques réalistes (7 matières + Total, bordures, fond de sécurité). Ajouter au benchmark une assertion « stratégie = `BAC_CELL_GRID` » pour les fixtures BAC, afin que le JPEG ne passe plus en silence par l'ancien chemin.
3. Rediriger `OCR_FIXTURE_OUTPUT` vers un répertoire ignoré, ou rendre la génération déterministe, pour que `ocr-tests` ne salisse plus le dépôt.

**P1 — Garde-fous avant tout succès réel (≈ 1–2 jours)**

4. **Go : contrôles inter-matières** — `arithmeticRowIndex` unique, `arithmeticRows`/`totalCells` identiques entre extractions, nombre de rangées aligné sur Python. Ajouter les tests négatifs correspondants.
5. **Série vs qualification déclarée** — transmettre le code de qualification dans le job (incrément du contrat versionné) et comparer en Go ; une incohérence doit produire un motif explicite. La politique dépend de la réponse IFRI (Q5).
6. **Callbacks rejetés** — journaliser un code de rejet sûr (sans PII). Côté worker, sur un 422, envoyer un échec terminal ou faire l'ACK plutôt que réessayer 3 × 5 min. Côté UI, afficher un message à la fin du polling.
7. **Calibration** — faire passer le résultat privé par `analysis.Validate` (petit binaire Go ou passage par l'API), pour qu'un succès signifie « accepté de bout en bout ».

**P2 — Lecture réelle des cellules (itératif, mesuré)**

8. Créer localement un oracle **au niveau des cellules** (texte attendu de chaque crop, fichier ignoré) à partir des crops de debug existants. Mesurer la précision par colonne (Matière, Note, Coeff, Points, Sur, Total, série) au lieu du seul résultat global.
9. **Libellés** : n'exiger un libellé reconnu que pour les matières cibles. Les autres rangées n'ont besoin que de leurs cellules numériques pour les sommes. Garde : unicité de la rangée cible (aucune autre rangée au-dessus d'un seuil bas). À évaluer contre le risque de mauvais appariement avant adoption. Essayer aussi une lecture du libellé en PSM 6 avec le dictionnaire `fra`.
10. **Nombres** : comparer sur les mêmes crops Tesseract (modèles `tessdata_best`, `--oem 1`), RapidOCR/PaddleOCR (vérifier d'abord la compatibilité Python 3.14), et la détection des axes de bordure (repli `PROFILE_RATIOS` systématique sur le réel).
11. **Budget** : mesurer sous 30 s/page. Si c'est insuffisant, justifier un budget propre au profil BAC ou optimiser (moins de crops inutiles, arrêt au Total déjà en place).

**P3 — Preuve et clôture**

12. Corpus réel ≥ 10 relevés anonymisés (BAC C, D, E, DT), dépendant de l'IFRI. Rapporter dans `OCR_BENCHMARK.md` : **faux acceptés (objectif 0)**, taux de succès, taux de `NEEDS_REVIEW`, temps.
13. Ajouter un scénario Playwright avec un scan BAC général synthétique passant par `BAC_CELL_GRID`.
14. Consolider la documentation : réécrire `PHASE5_1_VALIDATION.md` à partir des sorties réelles, supprimer les doublons, lever la contradiction de `AGENTS.md`, mettre à jour TODO/ROADMAP, commiter la ROADMAP.
15. **Point de décision** (à trancher par vous) : si après P2 le relevé réel échoue toujours, accepter « échec propre + revue manuelle en phase 8 » comme sortie de 5.1, ou poursuivre la calibration. Dans les deux cas, définir ce que voit un candidat bloqué en `NEEDS_REVIEW` en l'absence d'administration (voir Q6).

---

## 9. Questions qui relèvent de l'IFRI, pas du code

1. **Grille officielle** : liste complète des séries/diplômes admissibles par filière (A, B, F, G… ? autres spécialités DT ?). Confirmer l'ajout DT/PM → IM, absent du cahier, et l'interprétation « Anglais pour C/D, Étude de fabrication pour E ».
2. **Source de la note** : faut-il retenir l'écrit du 1er groupe ou l'oral du 2nd groupe quand une matière apparaît dans les deux ? Que faire pour un candidat admis au 2nd groupe ?
3. **Note dérivée** : une note calculée comme Points ÷ Coeff (deux valeurs imprimées), validée par les totaux, est-elle acceptable comme « note issue du relevé » ? Que faire si la note imprimée diffère ?
4. **Formats de relevés** : le BAC E (et les DT IMI/DWM/EAP/PM) utilise-t-il la même mise en page que le BAC général ? Fournir au moins 10 relevés réels anonymisés, avec une autorisation écrite de les utiliser pour la calibration.
5. **Série déclarée vs série lue** : si le relevé montre une autre série que celle déclarée, faut-il refuser, alerter ou corriger ?
6. **Échecs d'extraction** : combien de tentatives avant une revue manuelle ? Qui la fait ? Un agent peut-il saisir les valeurs (avec preuve et traçabilité) ? Le candidat doit-il ensuite reconfirmer ?
7. **Échelle et bornes** : notes toujours sur 20 ? Les coefficients sont-ils toujours des entiers de 1 à 6 ? Combien de décimales sur les notes ?
8. **Moyenne et départage** : précision et arrondi d'affichage, règle de départage en cas d'égalité, moyenne minimale d'admissibilité éventuelle. Confirmer que le classement porte sur la moyenne des seules matières attendues, et non sur la moyenne générale du BAC.
9. **Quotas et vœux** : places par filière et par campagne, taille de la liste d'attente ; une seule filière par candidat ou plusieurs vœux ordonnés ?
10. **Calendrier** : dates de campagne, corrections après soumission, réouverture d'un dossier.
11. **Rôles administratifs** : qui vérifie, qui valide la sélection finale, faut-il une double validation, la MFA est-elle exigée ?
12. **Pièces** : liste définitive ; modèle imposé pour la demande au Directeur et l'engagement parental ? Taille et nombre de pages maximum ; photos prises au téléphone acceptées ?
13. **Accès candidat** : compte avec email vérifié ? Reprise du dossier depuis un autre appareil ? Notifications email/SMS ?
14. **Numéro de dossier** : format `IFRI-2026-IA-000245` confirmé ? Séquence par filière ou globale ? Année = année de campagne ?
15. **Données personnelles** : durée de conservation et suppression après campagne, conformité au cadre béninois de protection des données (autorité APDP, à confirmer par l'IFRI), désignation d'un responsable.
16. **Hébergement** : serveurs UAC ou cloud, nom de domaine, certificat HTTPS, fournisseur S3, objectifs de sauvegarde et de restauration (RPO/RTO).
17. **Cahier des charges** : la section 18 manque (numérotation 17 → 19). Un contenu a-t-il été omis ?
18. **Statuts** : quels statuts du §16 le candidat et l'administration doivent-ils voir, avec quels libellés ? (L'implémentation sépare dépôt, analyse, administration et sélection.)
19. **Identité visuelle** : logo et charte officiels de l'IFRI/UAC, et autorisation de les utiliser.
