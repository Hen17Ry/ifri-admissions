# Phase 5.1 — corrective en validation

Phase non terminée : changements implémentés et contrôles locaux passés ; validation Tesseract/Docker/E2E hôte nécessaire. PASS indique une exécution réelle ; NON TESTÉ ne vaut pas validation. Aucune phase 6 commencée.

## Contrôles exécutés

| Contrôle | Résultat | Preuve / limite |
|---|---|---|
| Bouton OCR / étape séparée | Supprimés | Cinq étapes ; assertions Vitest d’absence ; aucune route de lancement manuel n’existait |
| Upload automatique | Mécanisme conservé | Trigger 00005 et dispatcher inspectés, sans duplication ; nouveau parcours hôte à exercer |
| Résultat / confirmation dans Documents | PASS unitaires | Slot de la carte TRANSCRIPT, lecture seule, polling automatique et restauration PROCESSING |
| Frontend | PASS | 29 Vitest ; build, typecheck, ESLint et Prettier exécutés |
| Go | PASS local | gofmt, vet, go test ; validation des métadonnées scan bornées |
| Intégration Go | PASS hôte | Suite Docker complète : PostgreSQL, Redis, S3, callbacks et autorisation ; métadonnées scan validées |
| Python | PASS local | 44 pytest, OpenCV/NumPy réellement utilisés ; fallback moteur simulé, native PyMuPDF réel |
| Python qualité | PASS | Ruff check et format check ; uv lock/sync officiel hôte, numpy 2.4.6 et OpenCV headless 4.13.0.92 |
| BAC écrit/oral | PASS unitaires | Matières dupliquées, Total exclu, section écrite, colonnes et contrôles Points/Sur, headers/ancres multilignes |
| Géométrie | PASS unitaires | Deskew crédible, perspective signalée, coordonnées crop transformées vers page source |
| Image-only / image→PDF | PASS unitaires | PNG/JPEG sans couche texte, scan path à 300 DPI et deux passes ; moteur simulé |
| Désaccord multipass | PASS unitaire | Valeurs contradictoires → AMBIGUOUS_ROW, aucune valeur devinée |
| Compose | Configuration PASS | Profil test validé ; socket Docker refusé dans l’environnement Codex |
| Tesseract renforcé | PASS synthétique hôte, nouvelle correction à revalider | 43 pytest Docker et corpus texte/scan antérieur + sécurité/PNG/JPEG/skew réussis |
| Private real scanned BAC D transcript | FAIL initial, correction en cours | Image-only confirmé ; premier OCR privé LOW_CONFIDENCE, 32,372 s, aucune valeur devinée ; recadrage sans prérequis d’en-têtes et filtrage décoratif ajoutés |
| Playwright / responsive | NON TESTÉ pour 5.1 | Deux scénarios adaptés : scan upload → automatique → refresh → confirmation ; rejet/remplacement automatique ; quatre largeurs et zoom CSS |
| Confidentialité privée | PASS structurel | private-fixtures ignoré Git et deux contextes Docker ; aucune donnée réelle ajoutée ; oracle local ignoré ; lecture par bind mount read-only, pas de COPY |
| Git | Vérifié | Diff propre ; aucun commit/push ; fichiers nouveaux/modifiés conservés dans workspace |

## Choix à mesurer

Stratégie scan initiale : CLAHE 300 DPI/PSM 11 pour profil et ancres ; adaptive 300 DPI/PSM 6 recadré au tableau écrit quand reconnu ; troisième passage canal de contraste 350 DPI seulement si nécessaire. Deux passages comparés avant acceptation ; conflit → LOW_CONFIDENCE. Budget commun par page et analyse, maximum trois passes, 25 M pixels. Profil BENIN_BAC_GENERAL : premier groupe écrit, oral exclu. Cette source de notes doit être confirmée avec les règles officielles IFRI.

Les scores restent heuristiques. Notes et coefficients proviennent exclusivement de leurs cellules PDF ; Points et Sur servent seulement de contrôle secondaire, aucun coefficient dérivé. Preuves normalisées vers page source, avec métadonnées de profil/section/variant/DPI/PSM/deskew validées côté Go. Aucune migration modifiée ni nouvelle table.

## Validation hôte, séparément et arrêt au premier échec

Depuis la racine :

```sh
docker compose up -d --build --wait --wait-timeout 240
docker compose --profile test run --build --rm references-tests
docker compose --profile test run --build --rm ocr-tests
```

Le profil OCR exécute pytest, le corpus synthétique précédent puis le benchmark fond de sécurité/sections/PNG/JPEG/skew. Il compare aussi cinq prétraitements et PSM 6/11 à 300 DPI, ainsi que 200/300/350/400 DPI. Résultats publics synthétiques dans fixtures/scan-benchmark.json. Ces essais de calibration ne sont pas tous exécutés dans le worker produit.

Si succès :

```sh
uv run --project services/ocr --locked --no-sync python infra/scripts/smoke.py
uv run --project services/ocr --locked --no-sync python infra/scripts/ocr_smoke.py
npm --prefix apps/web run test:browser
uv run --project services/ocr --locked --no-sync python infra/scripts/ocr_private_run.py
docker compose ps --all
```

La dernière calibration ne s’exécute que si le PDF réel local et oracle.json ignorés sont présents ; sinon NON TESTÉ sans bloquer les suites synthétiques. Aucune identité, texte OCR intégral ou hash privé émis. Pas de prétention production-ready sur une fixture réelle unique.

Une validation sur un corpus anonymisé de relevés réels représentant les différents formats BAC/DT est nécessaire avant mise en production.


## Première validation hôte de 5.1

Rebuild et services healthy PASS. Suite references-tests entièrement PASS (PostgresAnalysis, documents/qpdf, sessions, référentiels et S3 privé). Pytest Docker : 43 PASS en 11,01 s. Six textes nominaux : 0,005–0,016 s ; six scans : 2,662–4,306 s, valeurs exactes. Cas dégradé/mauvais ensemble LOW_CONFIDENCE, ambiguïté toujours refusée.

Sécurité/sections synthétiques : texte 0,021 s ; PNG 5,038 s ; JPEG avec éclairage 5,768 s ; skew 5,985 s, toutes valeurs exactes. PSM 6 lit les cellules pour les cinq prétraitements à 300 DPI ; PSM 11 échoue sur les cellules mais repère les ancres pour le recadrage. Étude PSM 11 à 200/300/350/400 DPI : toutes GRADE_NOT_FOUND ; augmenter la résolution seule ne résout pas la segmentation. Les mesures de cette itération ne prouvent pas la prochaine correction ni une précision réelle.

PDF privé localisé hors workspace puis référencé par lien ignoré, sans copie de ses octets. Montage du fichier résolu et de l’oracle uniquement en lecture seule. Une page, aucun mot natif, une image ; préparation locale 300 DPI : pas de perspective signalée ni de rotation appliquée. Premier OCR privé : section BAC écrite reconnue, aucune matière extraite ; second passage non recadré et budget page épuisé avant troisième essai. Correction : bounds de section indépendantes des en-têtes ; recadrage dès ancres crédibles ; retrait des glyphes >2,5×hauteur médiane avant lignes/ancres. Test de régression décoratif ajouté, 44 tests locaux PASS. Diagnostics privés supplémentaires limités à identifiants canoniques, scores et géométrie, jamais texte brut.

Le lanceur privé reconstruit explicitement l’image de calibration avant l’exécution, pour ne pas tester une ancienne image. Build worker : dépendances verrouillées installées avant COPY src afin de les conserver en cache pendant la calibration ; runtime et versions inchangés.

### Deuxième calibration privée — encore en échec

Résultat transmis depuis Docker : LOW_CONFIDENCE, aucune matière acceptée, 28,506 s. Le recadrage fonctionne : CLAHE 300/PSM11 8,338 s, adaptive 300/PSM6 recadré 4,541 s, canal couleur 350/PSM6 recadré 14,782 s. Les en-têtes candidats ne forment pas encore un tableau cohérent. Aucun seuil abaissé ni valeur forcée. Une évaluation locale séparée Otsu/grayscale à 300 DPI PSM6 est disponible via `ocr_private_run.py --evaluate` ; elle ne modifie pas le plan de production et ne publie aucun texte OCR brut. La phase reste non terminée.

### Calibration recadrée — variantes insuffisantes

Mesures privées transmises : grayscale 3,875 s, adaptive 3,652 s, normalisation du fond 8,325 s, tous SUBJECT_NOT_FOUND sans résultat accepté. Adaptive reconnaît deux libellés attendus mais avec confiance OCR insuffisante ; aucune valeur forcée. Le prochain essai expérimental compare PSM6/11 après sélection du canal le plus clair, normalisation locale et retrait ciblé des longues lignes de grille. Cette variante `security-table` reste hors du plan de production tant que son gain et sa régression ne sont pas mesurés.

### Traitement ciblé du tableau — encore insuffisant

Calibration privée : security-table à 300 DPI PSM6 6,075 s, PSM11 6,093 s. Aucun résultat accepté. PSM6 reconnaît les trois libellés mais leur confiance reste insuffisante (0,395–0,485). Le prochain essai hors production détecte les lignes verticales longues puis OCRise séparément les bandes de colonnes ; les mots sont remappés avant reconstruction des lignes et extraction. Aucun axe X ni résultat numérique privé n'est codé en dur. Tests locaux : 47 Python PASS.

### Stratégie BAC général par cellules — correction demandée

La stratégie active pour les scans BAC général remplace les essais OCR globaux par : correction géométrique prudente (contour de page quadrilatéral crédible, puis deskew), binarisation du canal V HSV (= max RGB), ancres du premier groupe écrit et Total écrit, reconstruction morphologique d'une grille de cinq colonnes fixes, OCR PSM7 de chaque cellule agrandie ×3. Whitelist numérique `0123456789,.`. Les en-têtes OCR ne déterminent plus les colonnes. Les tables sans séparateurs horizontaux internes utilisent les bandes de texte numérique pour reconstruire les séparations ; une géométrie ou des cellules incohérentes imposent NEEDS_REVIEW. Les profils génériques conservent leur chemin existant.

La liste fermée documentaire des matières BAC sert au matching ; elle ne remplace pas le snapshot des matières d'admission. Notes et coefficients viennent toujours de leurs propres cellules. Points = note × coefficient et Sur = 20 × coefficient sont vérifiés exactement sur chaque ligne, puis les sommes coefficient/points/Sur sont comparées au Total écrit. La note récapitulative imprimée peut être arrondie ou tronquée au centième : contrôle secondaire avec tolérance `0,01 × coefficient total` ; aucun résultat de moyenne d'admission n'est créé.

Sur demande explicite de l'utilisateur, ces preuves arithmétiques remplacent le seuil Tesseract **uniquement pour ce profil**. Les confiances réelles restent présentes. Go revérifie indépendamment les produits, sommes, ligne sélectionnée et bornes de valeurs avec `big.Rat` avant acceptation d'une extraction à faible confiance. Les lignes incohérentes portent NEEDS_REVIEW dans le diagnostic ; le résultat conserve l'état terminal existant LOW_CONFIDENCE avec failureCode NEEDS_REVIEW, afin de ne pas inventer un circuit administratif de phase 6.

Diagnostic privé : texte brut des cinq cellules du tableau uniquement, valeurs parsées, contrôles par ligne, contrôles des totaux et série. Aucun en-tête d'identité, texte de page complet, hash ou PDF n'est exporté. Calibration via `ocr_private_run.py` ; l'ancien argument `--evaluate` utilise désormais la même stratégie, sans matrice de variantes. Les anciens benchmarks sont historiques et ne valident pas ce nouveau chemin. Test réel et régression Docker restent à mesurer. RapidOCR sera évalué sur les mêmes cellules seulement si la lecture numérique reste insuffisante, après vérification de compatibilité Python 3.14.

Contrôles locaux de la nouvelle stratégie : 56 tests Python PASS ; Ruff PASS ; Go `test ./...`, `vet ./...` et gofmt PASS (intégrations externes non exécutées ici). Les nouveaux tests vérifient le canal V, la grille sans en-têtes OCR, PSM7/whitelist/agrandissement, les lignes et totaux incohérents, ainsi que le refus en Go des preuves arithmétiques incomplètes malgré une faible confiance OCR. Le scan privé doit maintenant être réévalué via Docker ; aucun succès réel n'est déclaré.

### Correction de segmentation des rangées

Dernier run privé Docker : NEEDS_REVIEW, 18,054 s, deux lignes et cellules vides. Correction : les rangées ne dépendent plus des traits horizontaux ; projection de l'encre dans la colonne Matière après filtrage des motifs et bandes trop courtes selon la hauteur des caractères. La grille conserve la détection morphologique des vrais traits verticaux. Nombre plausible : 5 à 12 rangées de données, sinon ROW_SEGMENTATION_FAILED avant OCR. Ce motif est accepté par Go comme résultat LOW_CONFIDENCE terminal, sans retry technique.

Chaque crop a largeur/hauteur en pixels, proportion de pixels noirs, polarité et éventuel motif vide/timeout. La série est lue séparément, sur une petite zone à droite de son ancre en haut à gauche, whitelist ABCDE. Les images privées de debug (section, overlay des colonnes/bandes, crops avant/après agrandissement) sont écrites uniquement lors de la calibration dans un répertoire `/tmp/ifri-ocr-private-debug-*`, monté en écriture ; aucun fichier privé ne rejoint le dépôt ni le build Docker.

Vérification locale de segmentation sur une zone de tableau sélectionnée pour le diagnostic, sans Tesseract : 8 rangées de données, 40 crops non vides, dimensions minimales 196 × 42 pixels, proportion noire 0,0197 à 0,799. Ces mesures ne prouvent pas encore la localisation automatique ni la lisibilité OCR ; le nouveau run Docker reste nécessaire.

### Bordures, colonnes et budget de calibration

La sélection des colonnes est ancrée aux extrémités du premier trait horizontal long du tableau. Six axes sont recherchés à l'intérieur de cette bordure, près des ratios du profil stable : 0 / 0,535 / 0,645 / 0,755 / 0,89 / 1. Un axe absent ou décalé déclenche le fallback de ratios, jamais une translation silencieuse des cinq colonnes. Matière est nettement plus large ; Points est légèrement plus large que Note/Coeff/Sur. Un tableau sans bordure crédible renvoie COLUMN_SEGMENTATION_FAILED.

Otsu est calculé sur le canal V de chaque crop, avec marge intérieure de 8 pixels sur X et 5 sur Y pour exclure les traits. Les temps par cellule sont consignés. La boucle s'arrête dès reconnaissance du libellé Total ; aucune rangée orale n'est ajoutée. La calibration privée seule dispose de 120 secondes par page ; le budget production reste inchangé. Le prochain benchmark devra comparer ce coût à cinq appels Tesseract par bandes de colonnes PSM6 (numériques avec whitelist), sur les mêmes limites et contrôles, avant tout changement de stratégie production.

Règle d'acceptation mise à jour sur instruction utilisateur : Note et Coeff sont obligatoires ; Sur doit être lisible et égal à 20 × Coeff. Points peut être illisible (diagnostic `null` pour ce contrôle), mais une valeur lisible contradictoire bloque la ligne. Go applique la même règle, conserve Points manquant sans fabriquer de valeur extraite et vérifie les sommes contre les totaux lisibles. Les produits calculés sont exclusivement des contrôles documentaires.

Le run privé précédent reste en échec : 31,193 s, colonnes décalées, budget épuisé. La vérification locale sur la zone de tableau diagnostique retrouve 8 rangées et des largeurs relatives 53/11/11/13/11 %, sans succès OCR déclaré. Le nouveau run automatique Docker reste à effectuer.

### Règle d'acceptation corrigée — prioritaire sur la règle précédente

Sur = 20 × Coeff valide uniquement le coefficient. Une note exige Points = Note × Coeff sur sa ligne, OU les deux sommes exactes Σ Note×Coeff = Total Points et Σ Coeff = Total Coeff. Points illisible ne valide donc plus une note sans totaux. Points lisible contradictoire reste bloquant. Les statuts `coefficientStatus` et `gradeStatus` sont séparés ; validation par ligne ou par totaux explicitée. Go vérifie la preuve décimale indépendamment et exige des totaux Points lisibles pour l'acceptation du dossier. Coeff entier 1..6 ; Sur multiple de 20 cohérent ; Note 0..20. Aucun seuil de confiance ne supplée un contrôle arithmétique manquant.

La limite physique de l'en-tête est exclue avant la projection des rangées, avec marge intérieure de 5 px sur X / 4 px sur Y. Le traitement effectif de chaque cellule est loggé : HSV_V_CELL_OTSU ; pour Points trop noir (>45 %), RED_CELL_CLAHE_OTSU. Cette décision utilise la densité du crop, pas ses valeurs attendues. Le TSV d'un seul appel fournit texte et confiance : seuls les mots level=5, confidences négatives ignorées et comptées, confiance moyenne des mots restants, véritables zéros conservés.

Dernier scan privé Docker : 36,864 s, NEEDS_REVIEW, aucune extraction complète acceptée, Total et série encore illisibles. Il ne valide pas la nouvelle correction. Les contrôles locaux et le run privé suivant restent distincts.

### Borne du premier tableau écrit — non-régression

Le run privé précédent (12,611 s, ROW_SEGMENTATION_FAILED) incluait le tableau EPS dans les bandes. Les bandes sont désormais regroupées par écarts : un saut supérieur à 2,5 × l'écart médian ouvre un autre tableau ; seul le premier tableau après le titre écrit est retenu. L'écart en-tête/première matière n'est pas une coupure de tableau. Le séparateur d'en-tête est recherché exclusivement entre la bande d'en-tête et la première bande de données. L'OCR s'arrête au Total écrit inclus, sans nombre de matières fixé selon une série.

Tests : bornes transmises (en-tête + 7 matières + Total + EPS), exemple plus court à nombre variable, et image synthétique à deux tableaux largement séparés. Suite Python : 66 PASS. Les règles de preuve de note par Points/totaux, contraintes de coefficient, Otsu par cellule avec fallback rouge pour Points, et confiance du même TSV restent inchangées et couvertes. Aucun nouveau résultat OCR privé n'est déclaré avant relance Docker.

### Correction documentaire : notes dérivées exclusivement après trois totaux

Instruction utilisateur prioritaire : quand Σ Coeff, Σ Points et Σ Sur lus correspondent tous aux cellules Total, la valeur retenue de chaque note est Points ÷ Coeff, avec `gradeStatus=VALIDATED_BY_TOTALS` et `gradeSource=POINTS_TOTALS`. La lecture brute/directe et sa divergence restent conservées. Coeff n'est jamais dérivé. Une somme fausse ou incomplète garde toutes les lignes en NEEDS_REVIEW. Les décimales non terminantes ne sont ni arrondies ni inventées. Go autorise une divergence note brute/valeur retenue uniquement pour ce mode avec preuve complète indépendante en rationnels exacts et coordonnées Points/Sur ; les autres extractions gardent l'égalité lecture brute/valeur. Aucun calcul de moyenne d'admission n'est introduit.

Parseur : virgule décimale et séparateur final isolé acceptés (`18.`, `80 .`), sans assembler des chiffres séparés. Le contrôle Sur/Coeff ne dépend plus de la lisibilité de Note. Les noyaux morphologiques longs suppriment les traits sur chaque cellule avant agrandissement ; les crops de données ont une marge symétrique autour de leur propre bande, y compris Anglais, sans remonter à l'en-tête. Seuils de confiance inchangés.

Dernier run privé : 40,398 s, NEEDS_REVIEW, 8 rangées correctement isolées ; les notes et totaux restent insuffisants. La nouvelle correction attend son run OCR réel. Validation locale avant relance : 74 tests Python et Go tests PASS ; aucune précision réelle déclarée acquise.
