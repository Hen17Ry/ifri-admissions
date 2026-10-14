# Benchmark OCR — catégories et versions du pipeline

## Mesures historiques phase 5 (200 DPI)

Exécution hôte après correction du matching : Python 3.14.8, PyMuPDF 1.28.2, RapidFuzz 3.14.6, Tesseract 5.3.0. Le profil Docker valide les 20 fixtures. Les six PDF texte nominaux réussissent avec valeurs exactes en 0,005 à 0,014 s. Les six scans nominaux réussissent avec valeurs exactes en 0,749 à 0,847 s.

Sans accents : réussite 0,005 s ; layout alternatif : réussite 0,008 s ; scan incliné : réussite 0,766 s. Matière absente : SUBJECT_NOT_FOUND 0,004 s ; coefficient absent : COEFFICIENT_NOT_FOUND 0,005 s ; ligne ambiguë : LOW_CONFIDENCE / AMBIGUOUS_ROW 0,004 s ; scan dégradé : SUBJECT_NOT_FOUND 0,841 s ; mauvais ensemble : SUBJECT_NOT_FOUND 0,671 s. Les notes et coefficients des cas positifs sont comparés exactement à ceux écrits dans les fixtures.

Parcours HTTP réel des six couples : QUEUED → PROCESSING → SUCCEEDED, 3,733 à 7,093 s observés, polling inclus. Confirmation/restauration et rejet/remplacement par scan passent. Les durées moteur ci-dessus excluent queue Redis, accès privé S3 et callback. Elles concernent seulement des fixtures synthétiques et ne prouvent aucune précision sur des formats IFRI réels.

L’échec initial BAC_C provenait d’un concurrent fuzzy sous le seuil minimal ; il est corrigé avec tests de régression, sans baisse du seuil ni acceptation des véritables ambiguïtés.

Une validation sur un corpus anonymisé de relevés réels représentant les différents formats BAC/DT est nécessaire avant mise en production.


## Phase 5.1 — mesures distinctes à produire

| Catégorie | Corpus | État actuel |
|---|---|---|
| A. PDF texte synthétique | Six couples de référentiel + BAC écrit/oral synthétique | Tests natifs unitaires exécutés ; benchmark nouveau moteur hôte à refaire |
| B. PDF scan synthétique | Six scans antérieurs + fond vert/jaune et sections/doublons | Prétraitements/géométrie testés ; Tesseract nouveau pipeline non mesuré ici |
| C. PDF image/photo synthétique | PNG/JPEG convertis, éclairage simulé et skew | Détection image-only et pipeline testés avec adapter simulé ; OCR réel à mesurer |
| D. Scan réel privé local | Relever.pdf + oracle ignoré | NON TESTÉ : fixture absente de cet environnement ; commande optionnelle préparée |

OpenCV/NumPy installés officiellement sur l’hôte et utilisés par les tests locaux. Résolution initiale proposée 300 DPI, repli 350 DPI ; plafond configurable 400 DPI et trois passes. Comparaison de grayscale/CLAHE/adaptive/Otsu/canal et PSM 6/11 à 300 DPI, puis étude séparée 200/300/350/400 DPI prévue dans ocr_scan_benchmark.py, séparée de la stratégie de production bornée. Les valeurs 200 DPI historiques ci-dessus ne sont pas extrapolées à 300/350 DPI. Aucun coût/qualité réel privé inventé.

La calibration privée imprime seulement profil/section/prétraitement/résolution, temps, valeurs des matières attendues et scores heuristiques ; aucun texte complet/nom/empreinte privée. Un seul scan réussi ne constitue pas un corpus de précision ni une validation production.


## Première mesure hôte 5.1 (avant correction du recadrage privé)

| Catégorie | Résultat mesuré |
|---|---|
| A. Texte synthétique | Six nominales exactes, 0,005–0,016 s ; écrit/oral BAC synthétique exact en 0,021 s |
| B. Scan synthétique | Six nominales exactes, 2,662–4,306 s ; fond sécurité PNG exact en 5,038 s |
| C. Image/photo synthétique | JPEG éclairage exact en 5,768 s ; skew exact en 5,985 s |
| D. Réel privé local | LOW_CONFIDENCE, aucune valeur retournée, 32,372 s ; section écrite reconnue, pas de recadrage faute d’en-têtes exacts |

À 300 DPI sur le BAC sécurité synthétique, PSM 6 obtient les valeurs exactes avec grayscale (1,711 s), CLAHE (1,783 s), adaptive (1,248 s), Otsu (1,140 s) et canal (1,210 s). PSM 11 obtient GRADE_NOT_FOUND pour ces cinq variantes, mais son repérage d’ancres sert au recadrage. Étude CLAHE/PSM 11 : 200 DPI 1,812 s, 300 2,164 s, 350 2,482 s, 400 2,654 s ; aucune ne résout la lecture des cellules. Ceci ne prouve ni une supériorité générale d’un mode, ni un avantage qualité des hautes résolutions.

Le coût du nouveau scan est supérieur au chemin simple 200 DPI historique ; son gain mesuré porte sur profil/sections, fond coloré, JPEG et skew synthétiques. Après échec réel, le recadrage n’attend plus les en-têtes et les grands glyphes sont filtrés avant reconstruction. Mesures privées et régression de cette correction restent à produire. Seul le résultat anonyme de calibration sera consigné ; aucun nom, chemin personnel, texte intégral, hash ou copie privée dans ce document.

### Stratégie BAC général par cellules — correction demandée

La stratégie active pour les scans BAC général remplace les essais OCR globaux par : correction géométrique prudente (contour de page quadrilatéral crédible, puis deskew), binarisation du canal V HSV (= max RGB), ancres du premier groupe écrit et Total écrit, reconstruction morphologique d'une grille de cinq colonnes fixes, OCR PSM7 de chaque cellule agrandie ×3. Whitelist numérique `0123456789,.`. Les en-têtes OCR ne déterminent plus les colonnes. Les tables sans séparateurs horizontaux internes utilisent les bandes de texte numérique pour reconstruire les séparations ; une géométrie ou des cellules incohérentes imposent NEEDS_REVIEW. Les profils génériques conservent leur chemin existant.

La liste fermée documentaire des matières BAC sert au matching ; elle ne remplace pas le snapshot des matières d'admission. Notes et coefficients viennent toujours de leurs propres cellules. Points = note × coefficient et Sur = 20 × coefficient sont vérifiés exactement sur chaque ligne, puis les sommes coefficient/points/Sur sont comparées au Total écrit. La note récapitulative imprimée peut être arrondie ou tronquée au centième : contrôle secondaire avec tolérance `0,01 × coefficient total` ; aucun résultat de moyenne d'admission n'est créé.

Sur demande explicite de l'utilisateur, ces preuves arithmétiques remplacent le seuil Tesseract **uniquement pour ce profil**. Les confiances réelles restent présentes. Go revérifie indépendamment les produits, sommes, ligne sélectionnée et bornes de valeurs avec `big.Rat` avant acceptation d'une extraction à faible confiance. Les lignes incohérentes portent NEEDS_REVIEW dans le diagnostic ; le résultat conserve l'état terminal existant LOW_CONFIDENCE avec failureCode NEEDS_REVIEW, afin de ne pas inventer un circuit administratif de phase 6.

Diagnostic privé : texte brut des cinq cellules du tableau uniquement, valeurs parsées, contrôles par ligne, contrôles des totaux et série. Aucun en-tête d'identité, texte de page complet, hash ou PDF n'est exporté. Calibration via `ocr_private_run.py` ; l'ancien argument `--evaluate` utilise désormais la même stratégie, sans matrice de variantes. Les anciens benchmarks sont historiques et ne valident pas ce nouveau chemin. Test réel et régression Docker restent à mesurer. RapidOCR sera évalué sur les mêmes cellules seulement si la lecture numérique reste insuffisante, après vérification de compatibilité Python 3.14.

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
