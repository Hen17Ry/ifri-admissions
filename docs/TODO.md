# Checklist ordonnée

## 0 — Préparation
- [x] Inspecter le répertoire : aucun code applicatif existant.
- [x] Vérifier et lire UI UX Pro Max ; exécuter génération, recherches UX/typographie/Vue et persistance.
- [x] Écrire AGENTS, spécifications, architecture, modèle, pipeline, roadmap et ADR.
- [x] Adapter le master aux admissions et documenter l’inspiration 21st.dev sans code React.
- [x] Proposer l’arborescence cible dans ARCHITECTURE.
- [x] Contrôler liens locaux et invariants documentaires.

## 1 — Bootstrap : critères de sortie techniques validés
- [x] Valider le périmètre du bootstrap minimal, autorisé par l’utilisateur.
- [x] Rechercher les versions stables et inscrire les versions directes exactes ; documenter les choix.
- [x] Créer squelettes Vue/Go/Python, Compose, README PowerShell, scripts et CI minimale.
- [x] Installer les dépendances Go depuis cache, générer go.sum, exécuter tests/vet/build et contrôler gofmt.
- [x] Installer Redis Python et le package worker depuis cache ; 10 tests exécutés avec pytest système de secours.
- [x] Valider la configuration Docker Compose, les manifests et l’absence de dépendance React déclarée.
- [x] Formater et contrôler les fichiers frontend avec Prettier 3.9.10 récupéré du cache.
- [x] Installer les dépendances frontend sur l’hôte et générer package-lock.json ; versions directes vérifiées.
- [x] Installer intégralement l’environnement Python déclaré sur l’hôte et générer uv.lock.
- [x] Exécuter build/typecheck/ESLint/Vitest/Prettier du frontend avec ses dépendances installées.
- [x] Exécuter pytest 9.1.1 (10 tests) et Ruff 0.17.0 (lint et format) dans l’environnement projet complet.
- [x] Construire et démarrer la stack sur l’hôte ; cinq services healthy, initialiseur terminé et smoke check réel réussi.
- [x] Vérifier persistance du volume après recréation PostgreSQL, readiness en panne puis reprise et arrêt propre du worker avec logs structurés.
- [x] Démarrer réellement Vite sur l’hôte.
- [x] Confirmer l’appel réel Vue → API dans le navigateur (observation utilisateur).

### Contrôles complémentaires non exécutés
- [ ] Contrôler le rendu UI aux quatre largeurs, clavier et fontes locales, puis exécuter la CI préparée.
- [ ] Relever les digests des images effectivement construites/tirées et finaliser la reproductibilité.

Rapport de preuves et limitations : `docs/BOOTSTRAP_VALIDATION.md`. Tous les critères techniques de sortie du bootstrap sont validés. Les contrôles complémentaires restent explicitement non exécutés ; aucun parcours candidat commencé.

## Phase 2 — Référentiels validés
- [x] Créer migration Goose Up/Down, contraintes, UUID et grilles versionnées.
- [x] Séparer seed canonique et fixture locale sans quotas officiels.
- [x] Ajouter repositories pgx, services et six endpoints GET /api/v1.
- [x] Ajouter tests unitaires/HTTP, tests PostgreSQL isolés et smoke HTTP réel.
- [x] Installer Goose 3.29.0 sur l’hôte et résoudre go.mod/go.sum avec go mod tidy.
- [x] Exécuter go test ./... et go vet ./... avec toutes les dépendances.
- [x] Appliquer les deux migrations réelles, seed deux fois, tests PostgreSQL, stack healthy et smoke HTTP phase 2.

## Phase 3 — Parcours brouillon : validations techniques terminées
- [x] Ajouter Candidate/Application/DraftSession, migration 00003 et contraintes sans documents.
- [x] Ajouter endpoints cookie-authentifiés, Origin/JSON, normalisation et invalidation de filière.
- [x] Utiliser UI UX Pro Max, recherches adaptées, override candidat sans modifier MASTER.
- [x] Ajouter route candidature, quatre étapes, état serveur Pinia et autosave sérialisé.
- [x] Ajouter tests Go/HTTP et PostgreSQL isolés, tests Vue et smoke de parcours réel.
- [x] Finaliser contrôles techniques locaux et rapport PHASE3_VALIDATION (validations hôte encore NON TESTÉES).
- [x] Appliquer migration et exécuter intégration PostgreSQL + parcours HTTP réel sur l’hôte.
- [x] Confirmer le fonctionnement de l’interface dans le navigateur hôte (observation utilisateur).
- [ ] Confirmer les contrôles détaillés responsive 375/768/1024/1440, clavier/zoom/erreurs.

## 2 — Arbitrages requis avant fonctions dépendantes
- [ ] Obtenir grille officielle complète : admissibilité et matières par chaque série/filière, au-delà de la matrice phase 2 fournie (C/D/E exactes, SI et IA désormais précisées).
- [ ] Confirmer un dossier/une filière par candidat/campagne et échelle des notes.
- [ ] Choisir authentification candidat, vérifier droits Reviewer et périmètres admin.
- [ ] Définir quotas, règles d’égalité et validation/publication des résultats.
- [ ] Cadrer réouverture, correction hors délai et transcription manuelle OCR.
- [ ] Fixer limites PDF, conservation/effacement, RPO/RTO, seuils et essais sur preuves mesurées.
- [ ] Fournir éléments de marque officiels et corpus autorisé/anonymisé pour benchmark.

## 3 — Construction incrémentale
- [x] Campagnes/référentiels versionnés et migrations avec tests de matrice (phase 2).
- [ ] Identité/sessions/RBAC/anti-IDOR avant exposition des dossiers.
- [ ] Brouillon candidat et sauvegarde, UI conforme au master.
- [ ] PDF privés, quarantaine, versionnement, prévisualisation protégée.
- [ ] Orchestration OCR avec worker simulé et tests de panne/concurrence.
- [ ] Benchmark OCR puis choix moteur et preuves par champ.
- [ ] Confirmation/refus, lecture seule et moyenne backend exacte.
- [ ] Soumission, revue, demande de correction/rejet et audit.
- [ ] Classements par campagne/filière, quotas, départage et décision humaine.
- [ ] E2E, accessibilité, sécurité, restauration et préparation production.

Chaque ligne est un chantier à autoriser et découper ; cette liste ne constitue pas une autorisation de commencer la phase suivante.

Phase 2 terminée : migration corrective 00002, seed idempotent, intégration PostgreSQL complète et HTTP réel validés sur l’hôte. Voir PHASE2_VALIDATION. Aucun chantier suivant commencé.

## Phase 4 — documents (autorisée, validation en cours)

- [x] Ajouter migration 00004 et versionnement avec contraintes, sans modifier les anciennes migrations.
- [x] Implémenter upload PDF privé, checksum, proxy sécurisé et compensation.
- [x] Ajouter étape Documents, récapitulatif et tests Vue/Go.
- [x] Exécuter build/typecheck/lint/format/Vitest, tests Go et contrôles Python locaux.
- [x] Exécuter réellement migration/rollback/réapplication et intégration PostgreSQL/S3/qpdf sur l’hôte.
- [x] Exécuter scénario HTTP des quatre PDF et remplacement TRANSCRIPT.
- [x] Confirmer stack reconstruite healthy et accès S3 privé.
- [x] Exécuter navigateur réel, inspecter captures 375/768/1024/1440 et zoom CSS 200 %, vérifier les assertions clavier/focus du scénario.
- [x] Finaliser PHASE4_VALIDATION avec preuves hôte.
- [ ] Avant production : scanner/quarantaine, conservation et réconciliation des objets orphelins.

Aucun OCR ni phase suivante commencé ; accord utilisateur requis pour la suite.

## Phase 5 — réalisée et validée localement / sur l’hôte

- [x] Ajouter migration OCR/contextes, callback interne, API de lecture/confirmation et contrôles numériques exacts.
- [x] Ajouter pipeline spatial hybride, moteur pluggable, Redis consumer/heartbeat et limites.
- [x] Ajouter étape Analyse en lecture seule et tests frontend/Go unitaires.
- [x] Générer uv.lock et installer réellement les nouveaux paquets Python sur l’hôte.
- [x] Exécuter pytest et extraction native sur fixtures unitaires (26 tests Python).
- [x] Exécuter/corriger Tesseract réel et le benchmark des 20 fixtures.
- [x] Compléter scénarios de panne/replay Redis, résultats stale, refus/remplacement et tests HTTP.
- [x] Exécuter migration/rollback/base vide PostgreSQL et listener interne réel.
- [x] Mesurer benchmark synthétique et consigner OCR_BENCHMARK.
- [x] Exécuter Playwright confirmation et refus/remplacement, contrôles visuels six étapes.
- [x] Finaliser PHASE5_VALIDATION avec preuves hôte.

Aucune moyenne, soumission, numéro officiel, administration ou classement autorisé dans cette phase.

Avant production : corpus BAC/DT réel anonymisé, calibration des seuils, audit accessibilité approfondi et CI dédiée restent à traiter.


## Phase 5.1 — corrective en validation

- [x] Supprimer bouton OCR et étape séparée ; intégrer analyse et confirmation dans Documents.
- [x] Conserver déclenchement transactionnel existant, gating backend et historique/version exacts.
- [x] Reprendre polling à la restauration et nouvelle version ; suspendre pendant mutation locale.
- [x] Ajouter profils, sélection écrit/oral, colonnes, contrôle secondaire et désaccord multipass.
- [x] Ajouter variantes scan/deskew/géométrie et limites, dépendances verrouillées sur l’hôte.
- [x] Ignorer corpus privé dans Git et contextes Docker ; préparer commande locale read-only.
- [ ] Exécuter nouveau benchmark Tesseract réel synthétique scan/photo et comparer variantes.
- [ ] Exécuter scan réel privé si disponible ; ne déclarer aucune précision de production.
- [ ] Rebuild Docker et régression Redis/S3/HTTP complète.
- [ ] Exécuter Playwright analyse automatique/refresh/rejet/remplacement aux quatre largeurs.
- [ ] Finaliser PHASE5_1_VALIDATION à partir des sorties réelles.

Aucune phase 6 autorisée.

### Stratégie BAC général par cellules — correction demandée

La stratégie active pour les scans BAC général remplace les essais OCR globaux par : correction géométrique prudente (contour de page quadrilatéral crédible, puis deskew), binarisation du canal V HSV (= max RGB), ancres du premier groupe écrit et Total écrit, reconstruction morphologique d'une grille de cinq colonnes fixes, OCR PSM7 de chaque cellule agrandie ×3. Whitelist numérique `0123456789,.`. Les en-têtes OCR ne déterminent plus les colonnes. Les tables sans séparateurs horizontaux internes utilisent les bandes de texte numérique pour reconstruire les séparations ; une géométrie ou des cellules incohérentes imposent NEEDS_REVIEW. Les profils génériques conservent leur chemin existant.

La liste fermée documentaire des matières BAC sert au matching ; elle ne remplace pas le snapshot des matières d'admission. Notes et coefficients viennent toujours de leurs propres cellules. Points = note × coefficient et Sur = 20 × coefficient sont vérifiés exactement sur chaque ligne, puis les sommes coefficient/points/Sur sont comparées au Total écrit. La note récapitulative imprimée peut être arrondie ou tronquée au centième : contrôle secondaire avec tolérance `0,01 × coefficient total` ; aucun résultat de moyenne d'admission n'est créé.

Sur demande explicite de l'utilisateur, ces preuves arithmétiques remplacent le seuil Tesseract **uniquement pour ce profil**. Les confiances réelles restent présentes. Go revérifie indépendamment les produits, sommes, ligne sélectionnée et bornes de valeurs avec `big.Rat` avant acceptation d'une extraction à faible confiance. Les lignes incohérentes portent NEEDS_REVIEW dans le diagnostic ; le résultat conserve l'état terminal existant LOW_CONFIDENCE avec failureCode NEEDS_REVIEW, afin de ne pas inventer un circuit administratif de phase 6.

Diagnostic privé : texte brut des cinq cellules du tableau uniquement, valeurs parsées, contrôles par ligne, contrôles des totaux et série. Aucun en-tête d'identité, texte de page complet, hash ou PDF n'est exporté. Calibration via `ocr_private_run.py` ; l'ancien argument `--evaluate` utilise désormais la même stratégie, sans matrice de variantes. Les anciens benchmarks sont historiques et ne valident pas ce nouveau chemin. Test réel et régression Docker restent à mesurer. RapidOCR sera évalué sur les mêmes cellules seulement si la lecture numérique reste insuffisante, après vérification de compatibilité Python 3.14.
