# Phase 5.1 — validation de clôture (14 octobre 2026)

**Statut : clôturée.** Réserve : corpus réel ≥ 10 relevés requis avant la production (un seul relevé disponible, dépend de l'IFRI).

Les décisions utilisateur du 14 octobre 2026 sur les points ouverts sont appliquées (ADR 078–082). `make check` est vert sur le code définitif. Critère de la clôture : **0 valeur fausse acceptée**, atteint sur le benchmark synthétique et sur le relevé réel privé, avec chacun des deux lecteurs de chiffres. PASS désigne une exécution réelle sur l'hôte avec Docker. Aucune phase 6 commencée, aucun commit effectué.

Décisions : DECISIONS (ADR 060–082). Mesures : OCR_BENCHMARK. Pipeline : OCR_PIPELINE. Constat sur la moyenne imprimée : ADMISSION_RULES.

## Décisions appliquées

| Décision | Implémentation | Preuves |
|---|---|---|
| RapidOCR pour les seules cellules numériques, Tesseract pour le reste (ADR 078) | Port `CellReader` (`ifri_ocr.engines`), `OCR_RAPIDOCR_ENABLED` (défaut `true`), `rapidocr==3.10.0` + `onnxruntime==1.31.0` figés dans `uv.lock` du worker, modèle PP-OCRv6 small embarqué au build et vérifié par SHA-256, télémétrie onnxruntime coupée | pytest : activé par défaut, chiffres seuls, readiness refusant un modèle absent ou altéré, manifeste d'un seul modèle ; log du worker `engine: rapidocr` |
| Règle par matière attendue (ADR 079) : note lue = Points ÷ Coeff → acceptée ; note illisible et Points ÷ Coeff validé par les totaux → acceptée (`POINTS_TOTALS`) ; note lisible différente → `GRADE_POINTS_MISMATCH` | Décision worker (`ifri_ocr.bac.arithmetic`) puis revérification indépendante en Go (`analysis/model.go`) ; les autres rangées ne servent qu'aux totaux ; message candidat dédié dans `AnalysisStep` | pytest et Go : erreurs compensées (deux Points faux, somme juste) jamais acceptées, y compris derrière une note illisible ou entre deux notes illisibles ; cas `d-compensated-points` et `d-grade-points-mismatch` du benchmark ; Vitest du message |
| Reconstruction d'une seule cellule par colonne depuis le Total (ADR 080) | `RECONSTRUCTED_FROM_TOTAL`, comptée seulement si sa rangée la confirme avec des opérandes lus ; coefficient reconstruit jamais retourné ; Points reconstruits jamais seule source d'une note | pytest et Go : reconstruction valide, contredite par une autre cellule mal lue, deux cellules manquantes ; benchmark `d-faded-scale`, `d-scale-contradicted`, `d-two-scales-blank` |
| Moyenne affichée : troncature ou arrondi à 2 décimales, rien d'autre (ADR 081) | Contrôle `displayedTotalNote` du chemin « totaux », worker et Go | pytest et Go : 9,61 et 9,62 acceptés pour 202/21, 9,60 refusé ; constat consigné dans ADMISSION_RULES comme observation sur un relevé, pas comme règle officielle |
| Série = grande lettre isolée en haut à gauche (ADR 082) | Zone relative au tableau écrit et au titre de section, sans chercher le mot « Série » ; PSM 10, deux binarisations avec liste de séries + une lecture libre, concordantes ; illisible → non bloquant | pytest : candidat unique et isolé, zone relative ; benchmark `d-series-unprinted` (`NOT_FOUND`, non bloquant), `c-declared-d` (`SERIES_MISMATCH`) ; **relevé réel : série lue, exacte** |

## `make check` — 26 étapes, CHECK PASSED

| Étape | Résultat | Détail |
|---|---|---|
| web build / typecheck / lint / format | PASS | Vite, vue-tsc, ESLint, Prettier |
| web Vitest | PASS | 35 tests (4 fichiers), dont le message `GRADE_POINTS_MISMATCH` |
| Go gofmt / vet (+ tag integration) / test / build (`api`, `db`, `ocrvalidate`) | PASS | Règle par matière attendue, reconstruction, moyenne affichée, erreurs compensées |
| Python pytest (hôte) | PASS | 109 tests, 4 ignorés (bout en bout moteurs réels, exécutés dans Docker) |
| Ruff worker + scripts | PASS | check et format |
| Compose config (+ profil test), `git diff --check` | PASS | |
| Stack Docker reconstruite, migrations, seed ×2 | PASS | 5 services sains ; aucune nouvelle migration |
| Intégration Go (`references-tests`) | PASS | 180 tests et sous-tests, 0 échec |
| smoke, références, brouillons, documents | PASS | Phases 1–4 |
| `ocr-tests` (Docker, Tesseract + RapidOCR réels) | PASS | pytest 113/113 ; 20 fixtures génériques **identiques octet pour octet** ; benchmark BAC réaliste 18/18, 0 valeur fausse |
| fixtures intactes | PASS | Empreinte des fixtures suivies identique avant/après `ocr-tests` |
| `ocr_smoke` (HTTP réel) | PASS | Six couples + rejet/remplacement ; chemin BAC par cellules via API/Redis/worker/Go : `d-photo` SUCCEEDED exact (17,4 s polling inclus), relevé C déclaré D → FAILED `SERIES_MISMATCH` (10,4 s) |
| Playwright | PASS | 2 scénarios (50,0 s) : parcours complet 4 largeurs + zoom, rejet/remplacement/nouvelle analyse |

### Correctif de clôture : healthcheck du worker

Le premier `make check` après l'adoption de RapidOCR a échoué au smoke : `ocr-worker` était `unhealthy`, et son healthcheck dépassait la limite de 5 s. Cause mesurée : l'image ne contenait aucun bytecode Python (0 `.pyc` pour 1 514 modules du venv, 3 pour 1 062 modules de la bibliothèque standard). Avec le système de fichiers en lecture seule et `PYTHONDONTWRITEBYTECODE=1`, chaque processus recompilait ses imports : 2,2 s pour `import ifri_ocr.worker` au repos, plus de 15 s sous la charge de `make check`. Les sous-processus d'analyse payaient la même recompilation (numpy, OpenCV, RapidOCR).

Correctif dans `services/ocr/Dockerfile` : `UV_COMPILE_BYTECODE=1` pour le venv et `compileall` pour la bibliothèque standard, au build. Mesure : import en 0,29 s au lieu de 2,2 s ; image de 1,07 à 1,14 Go. Aucun changement de code ni de configuration d'exécution. `make check` complet vert ensuite.

## Benchmark synthétique BAC réaliste (critère : 0 valeur fausse)

18 cas (OCR_BENCHMARK §B) : scans, photos inclinées, séries C/D/E, colonnes sans filets, série absente, série contredite, note illisible validée par les totaux, Total faux, note contredite par Points, **erreurs compensées**, reconstruction valide, deux Sur manquantes, reconstruction contredite, rangée dupliquée, matière absente, PDF natif.

| Lecteur de chiffres | Résultat |
|---|---|
| RapidOCR (production, dans `make check`) | 18/18 statuts attendus, **0 valeur fausse acceptée**, 6,6–8,3 s par page |
| Tesseract (`OCR_RAPIDOCR_ENABLED=false`) | 18/18 statuts attendus, **0 valeur fausse acceptée**, 7,7–9,9 s par page |

## Relevé réel privé (BAC D scanné, budget de production 30 s)

`make ocr-private` (moteur de production) et `make ocr-private ARGS="--numeric-engine tesseract"`, validés par Go via `cmd/ocrvalidate`. Rapport anonyme, détail dans OCR_BENCHMARK §C.

| Indicateur | Audit initial | Clôture précédente | **Clôture (RapidOCR)** |
|---|---|---|---|
| Statut | — | LOW_CONFIDENCE `NEEDS_REVIEW` | LOW_CONFIDENCE `GRADE_POINTS_MISMATCH` |
| **Valeurs fausses acceptées** | — | 0 | **0** (Tesseract : 0) |
| Libellés reconnus | 1/7 | 7/7 | 7/7 |
| Série | non lue | non lue | **lue, exacte** |
| Coefficients validés | 3/7 | 3/7 | 6/7 (Tesseract : 3/7) |
| Produits Points = Note × Coeff | 2/7 | 5/7 | 5/7 |
| Rangées attendues validées exactement | — | 0/3 | **2/3** (Tesseract : 0/3) |
| Durée par page | 28,7 s (budget 120 s) | 16,0 s | 13,0 s (budget 30 s) |

Le relevé n'est pas accepté. Deux matières attendues sont prouvées par leur rangée. La troisième note est mal lue par l'OCR : Points ÷ Coeff la contredit, et la règle ADR 079 la refuse au lieu de l'accepter. Le relevé part donc en revue documentaire, sans aucune valeur fausse. La colonne Sur ne totalise pas, parce qu'une valeur y est lue fausse (et non illisible) : aucune reconstruction ne s'applique. Le relevé réel reste un objectif, pas un critère bloquant.

## Critères de sortie (ROADMAP)

| Critère | État |
|---|---|
| Plus de bouton, 5 étapes, analyse dans la carte du relevé | ✅ Vitest + Playwright |
| Upload/remplacement → analyse automatique ; refresh → restauration + polling | ✅ Playwright, ocr_smoke |
| Profil `BENIN_BAC_GENERAL` : section écrite, grille de cellules, ligne Total | ✅ 18 cas synthétiques, dont 2 de bout en bout HTTP |
| Règle par matière attendue, reconstruction, moyenne affichée, série (ADR 079–082) | ✅ pytest, Go, benchmark ; revérifié par Go |
| 0 valeur fausse acceptée : benchmark synthétique | ✅ 0 (deux moteurs) |
| 0 valeur fausse acceptée : relevé réel privé | ✅ 0 (deux moteurs), mais corpus = 1 relevé |
| Relevé réel lu entièrement | ⏳ objectif non bloquant : 2/3 rangées attendues |
| Corpus réel ≥ 10 relevés anonymisés | ❌ réserve avant production ; harnais prêt |
| `make check` vert | ✅ |

## Confidentialité

- Le relevé et son oracle restent ignorés par Git et par les contextes Docker. Ils sont montés en lecture seule sous un nom anonyme (`doc-N`).
- Les rapports ne contiennent que des issues par champ (EXACT / FALSE / MISSING), des décisions, des statuts, de la géométrie et des durées : aucun nom, aucune valeur, aucun texte OCR brut.
- Les rapports temporaires de cette exécution sont dans le scratchpad de session, hors dépôt. Aucune image de debug n'a été produite (`--keep-debug` non utilisé).

## Limites et suite

- Une seule mesure réelle : aucune précision de production n'est revendiquée. Le corpus de 10 relevés ou plus (BAC C, D, E, DT) dépend de l'IFRI.
- La lecture complète du relevé réel bute sur deux cellules lues fausses (une note, une valeur Sur). Il faut d'autres relevés pour savoir si c'est propre à ce scan ou systématique.
- Le banc séparé de l'étude RapidOCR a été retiré du dépôt ; son image Docker locale (`ifri-admissions-ocr-rapidocr-bench`) peut être supprimée à la main.
- Commit des changements 5.1 à faire par l'utilisateur.
