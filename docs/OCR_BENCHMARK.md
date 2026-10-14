# Benchmark OCR — synthétique et réel, mesurés séparément

Mesures du 14 octobre 2026, clôture de la phase 5.1. Environnement : image worker Docker, Python 3.14.8, PyMuPDF 1.28.2, RapidFuzz 3.14.6, Tesseract 5.3.0 (`fra`, `eng`), OpenCV headless 4.13.0.92, NumPy 2.4.6, **RapidOCR 3.10.0 + onnxruntime 1.31.0** (modèle de reconnaissance PP-OCRv6 small, lecteur de chiffres de production) ; hôte 8 CPU, `OCR_CELL_WORKERS` par défaut (4) ; budget de page de production 30 s. Les durées sont celles du moteur (hors file Redis et callback), sauf mention « HTTP ».

Un corpus synthétique, même réaliste, ne prouve aucune précision sur des relevés réels. Le corpus réel disponible compte **un seul** relevé : aucune précision de production n'est revendiquée. Corpus réel ≥ 10 relevés requis avant la production.

## A. Synthétique générique — 20 fixtures suivies (chemins natif et scan générique)

Commande : `docker compose --profile test run ocr-tests` (via `make check`). Les PDF sont régénérés dans le tmpfs du conteneur et **identiques octet pour octet** aux copies suivies. Ce chemin n'utilise pas RapidOCR.

| Catégorie | Résultat |
|---|---|
| 6 PDF texte (BAC C, BAC E, DT DWM, DT IMI, DT PM, DT EAP) | SUCCEEDED, valeurs exactes |
| 6 scans correspondants | SUCCEEDED, valeurs exactes |
| Sans accents, mise en page alternative, scan incliné | SUCCEEDED, exact |
| Matière absente, coefficient absent | FAILED `SUBJECT_NOT_FOUND`, `COEFFICIENT_NOT_FOUND` |
| Ligne ambiguë, scan dégradé, mauvaises matières | LOW_CONFIDENCE (`AMBIGUOUS_ROW`, `LOW_CONFIDENCE`) |

## B. Synthétique BAC général réaliste — chemin par cellules (18 cas)

Commande : `docker compose --profile test run ocr-tests` (via `make check`, lecteur de chiffres RapidOCR) et `make ocr-engines-benchmark` (même banc avec `OCR_RAPIDOCR_ENABLED=false`). Générateur déterministe `tests/support/synthetic_bac.py` : relevé BAC général sur fond de sécurité, section écrite, ligne Total, moyenne imprimée tronquée ; PDF créés dans un répertoire temporaire, jamais dans le dépôt. Critère : statut attendu atteint, valeurs exactes quand `SUCCEEDED`, **aucune valeur fausse acceptée**.

| Cas | Ce qu'il éprouve | Attendu et obtenu (deux moteurs) |
|---|---|---|
| `d-scan`, `d-photo`, `e-scan`, `c-photo-shuffled` | Scan, photo inclinée, série E, rangées dans un autre ordre | SUCCEEDED, exact |
| `d-no-vertical-rules` | Colonnes sans filets verticaux | SUCCEEDED, exact |
| `d-smudged-philosophy` | Rangée non attendue tachée | SUCCEEDED, exact |
| `d-series-unprinted` | Lettre de série absente : non bloquant | SUCCEEDED, exact, série `NOT_FOUND` |
| `c-declared-d` | Relevé C déclaré D | FAILED `SERIES_MISMATCH` |
| `d-math-note-blank` | Note attendue illisible, Points ÷ Coeff validé par les totaux | SUCCEEDED, exact (origine `POINTS_TOTALS`) |
| `d-wrong-total` | Même cas, Total Points faux | LOW_CONFIDENCE `NEEDS_REVIEW` |
| `d-grade-points-mismatch` | Note lisible ≠ Points ÷ Coeff | LOW_CONFIDENCE `GRADE_POINTS_MISMATCH` |
| `d-compensated-points` | Deux Points faux (+2 / −2), somme toujours juste | LOW_CONFIDENCE `GRADE_POINTS_MISMATCH` |
| `d-faded-scale` | Une cellule Sur vide : reconstruite depuis le Total puis confirmée par sa rangée | SUCCEEDED, exact |
| `d-two-scales-blank` | Deux cellules Sur vides dans la colonne : aucune reconstruction | LOW_CONFIDENCE `NEEDS_REVIEW` |
| `d-scale-contradicted` | Reconstruction faussée par une autre cellule Sur erronée | LOW_CONFIDENCE `NEEDS_REVIEW` |
| `d-duplicate-french` | Matière attendue sur deux rangées | LOW_CONFIDENCE `AMBIGUOUS_ROW` |
| `d-missing-math` | Matière attendue absente | LOW_CONFIDENCE `NEEDS_REVIEW` |
| `d-native-text` | PDF natif (couche texte) | SUCCEEDED, exact, sans OCR |

| Lecteur de chiffres | Résultat | Durée par page (scans/photos) | dont ancrage / cellules |
|---|---|---|---|
| RapidOCR (production) | **18/18 conformes, 0 valeur fausse acceptée** | 6,6–8,3 s | 3,1–4,2 s / 2,7–3,7 s |
| Tesseract (`OCR_RAPIDOCR_ENABLED=false`) | **18/18 conformes, 0 valeur fausse acceptée** | 7,7–9,9 s | 2,9–4,2 s / 4,0–4,8 s |

Sur ce corpus imprimé net, les deux moteurs lisent toutes les cellules : la différence de précision n'apparaît que sur le réel (§C). De bout en bout HTTP (`ocr_smoke`, API → Redis → worker → Go), `d-photo` atteint SUCCEEDED exact en 17,4 s et `c-declared-d` FAILED `SERIES_MISMATCH` en 10,4 s, polling compris.

## C. Réel privé — 1 relevé BAC D scanné (image seule)

Commande : `make ocr-private` (moteur de production) et `make ocr-private ARGS="--numeric-engine tesseract"` (comparaison), harnais `ocr_private_eval.py`, validation Go `cmd/ocrvalidate`, budget de production 30 s. Rapport anonyme : aucune valeur ni texte reproduit ici ; les issues par rangée (EXACT / FALSE) comparent la décision à l'oracle local sans l'afficher.

| Indicateur | RapidOCR (production) | Tesseract (comparaison) |
|---|---|---|
| Statut | LOW_CONFIDENCE `GRADE_POINTS_MISMATCH` | LOW_CONFIDENCE `GRADE_POINTS_MISMATCH` |
| **Valeurs fausses acceptées** (worker et Go) | **0** | **0** |
| Décisions de rangée fausses | 0 | 0 |
| Durée moteur (ancrage / cellules) | 13,0 s (7,2 / 4,8) | 13,7 s (5,7 / 6,9) |
| Libellés reconnus | 7/7 | 7/7 |
| Série (lettre isolée, ADR 082) | lue, EXACT | lue, EXACT |
| Cellules lisibles Note / Coeff / Points / Sur | 7 / 7 / 7 / 7 | 7 / 7 / 7 / 7 |
| Coefficients validés (Sur = 20 × Coeff) | 6/7 | 3/7 |
| Produits Points = Note × Coeff | 5/7 (2 contredits) | 5/7 (2 contredits) |
| Cellules reconstruites depuis le Total | 0 (aucune illisible) | 0 |
| Totaux Σ Coeff / Σ Points / Σ Sur | ✓ / ✓ / ✗ | ✓ / ✗ / ✗ |
| Moyenne affichée (troncature ou arrondi) | ✗ | non lue |
| Rangées attendues validées exactement | **2/3** (`VALIDATED_BY_ROW`) | 0/3 (`NEEDS_REVIEW`) |
| Rangée attendue restante | note mal lue par l'OCR, contredite par Points ÷ Coeff → `GRADE_POINTS_MISMATCH` | idem |
| Résultat accepté par Go comme terminal valide | oui | oui |

Lecture : la règle par matière attendue (ADR 079) fait exactement ce pour quoi elle a été choisie. Deux notes sont prouvées par leur rangée ; la troisième, mal lue, est refusée au lieu d'être acceptée, et le relevé part en revue documentaire. Σ Sur ne concorde pas : au moins une valeur de la colonne Sur est lue fausse, et non illisible. Les totaux ne sont donc pas vérifiés, et aucune reconstruction ne s'applique. C'est voulu : une cellule mal lue ne déclenche jamais de reconstruction. Le relevé réel reste un objectif ; le critère bloquant, 0 valeur fausse acceptée, est atteint avec les deux moteurs.

## D. RapidOCR — adopté pour les cellules numériques (ADR 078)

- Décision utilisateur du 14 octobre 2026 : RapidOCR lit les seules cellules numériques du profil BAC ; Tesseract garde ancres, libellés, lettre de série et chemin générique. Port `CellReader` (`ifri_ocr.engines`), `OCR_RAPIDOCR_ENABLED=true` par défaut, `false` pour revenir aux chiffres Tesseract dans la même image.
- Runtime : `rapidocr==3.10.0` (Apache-2.0) et `onnxruntime==1.31.0` (MIT) figés dans `services/ocr/uv.lock` avec leurs dépendances (pyclipper, shapely, omegaconf, pillow, requests…, toutes en roues cp314) ; `opencv-python-headless` reste l'unique OpenCV.
- Modèle : seul `PP-OCRv6_rec_small.onnx` (21 Mo) est embarqué dans `/opt/rapidocr-models`, téléchargé au build et vérifié contre `services/ocr/rapidocr-models.sha256` (même empreinte que le manifeste publié par RapidOCR 3.10.0) ; le build échoue sinon. Aucun téléchargement à l'exécution : le worker est sur un réseau interne sans sortie et la readiness refuse un modèle absent ou altéré.
- Télémétrie : les roues Linux d'onnxruntime 1.31 embarquent une télémétrie 1DS (HTTPS vers un collecteur Microsoft) ; `ORT_DISABLE_TELEMETRY=1` est posé dans l'image et avant l'import.
- Coûts mesurés : image worker 823 Mo → 1,07 Go, puis 1,14 Go avec le bytecode Python précompilé (système de fichiers en lecture seule, voir PHASE5_1_VALIDATION) ; chargement du modèle ≈ 3 s par sous-processus d'analyse, masqué en le lançant pendant la passe d'ancrage ; second moteur à maintenir.
- La règle d'acceptation arithmétique et la validation Go ne dépendent pas du moteur : un chiffre mal lu par l'un ou l'autre ne peut être accepté que s'il passe les mêmes contrôles.

## Historique

Avant la phase 5.1 corrective, deux fixtures BAC synthétiques sur trois échouaient (`ROW_SEGMENTATION_FAILED`, contrainte 5–12 rangées). Phase 5 (200 DPI) : fixtures natives exactes en 0,005–0,014 s, scans nominaux exacts en 0,75–0,85 s, parcours HTTP des six couples en 3,7–7,1 s ; ces mesures précèdent le profil BAC par cellules.

Une validation sur un corpus anonymisé de relevés réels représentant les différents formats BAC/DT est nécessaire avant mise en production.
