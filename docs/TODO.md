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
- [x] Définir quotas, règles d’égalité et validation des résultats (phase 9 : places et seuil par le Directeur, départage IFRI, gel). Publication aux candidats hors périmètre.
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
- [x] Classements par campagne/filière, quotas, départage et décision humaine (phase 9).
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


## Phase 5.1 — clôturée (14 octobre 2026), réserve corpus réel avant production

- [x] Supprimer bouton OCR et étape séparée ; intégrer analyse et confirmation dans Documents.
- [x] Conserver déclenchement transactionnel existant, gating backend et historique/version exacts.
- [x] Reprendre polling à la restauration et nouvelle version ; arrêt borné avec message et actualisation manuelle.
- [x] Niveau 1 : Ruff vert ; `ocr-tests` n'écrit plus dans le dépôt (génération déterministe + comparaison octet par octet) ; contrainte 5–12 rangées remplacée par en-tête + N ≥ 1 + Total ; `make check`.
- [x] Niveau 2 : série lue/comparée (`SERIES_MISMATCH`, illisible non bloquante) ; unicité des rangées côté worker et Go ; résultat refusé par Go → `FAILED/RESULT_REJECTED` journalisé, acquitté, affiché immédiatement.
- [x] Niveau 3 : chiffres sur toutes les rangées, libellés seulement pour l'attribution (hongroise, écart minimal) ; recadrage encre + 3 binarisations ; fond aplati ; glyphes normalisés ; lecture en deux temps parallélisée.
- [x] Niveau 4 : générateur BAC synthétique réaliste validé de bout en bout (12 cas, 18 avec les cas des ADR 079–082) ; harnais privé `<nom>.expected.json` + validation Go + rapport anonyme ; debug supprimé sauf `--keep-debug`.
- [x] RapidOCR adopté pour les seules cellules numériques (ADR 078) : port `CellReader`, `OCR_RAPIDOCR_ENABLED` (défaut `true`), versions figées dans le runtime du worker, modèle épinglé par SHA-256, télémétrie onnxruntime coupée, readiness mise à jour ; image de banc supprimée.
- [x] Règle d'acceptation par matière attendue (ADR 079) : note lue = Points ÷ Coeff, ou note illisible + totaux vérifiés et autres rangées confirmées ; `GRADE_POINTS_MISMATCH` ; tests d'erreurs compensées (worker et Go).
- [x] Reconstruction d'une cellule par colonne depuis le Total (ADR 080), tests : reconstruction valide, contredite, deux cellules manquantes.
- [x] Moyenne affichée : troncature ou arrondi à 2 décimales (ADR 081), constat consigné dans ADMISSION_RULES.
- [x] Série : grande lettre isolée en haut à gauche, zone relative au tableau (ADR 082) ; lue « D » exacte sur le relevé réel.
- [x] Harnais privé et benchmark synthétique relancés : 0 valeur fausse acceptée (18/18 synthétiques ; réel : 2 matières sur 3 validées exactes par leur rangée, la 3ᵉ en revue).
- [x] Healthcheck du worker trop lent sous charge : bytecode Python précompilé au build (`UV_COMPILE_BYTECODE=1` + `compileall`), import 2,2 s → 0,29 s.
- [ ] **Avant production** : corpus réel ≥ 10 relevés anonymisés (BAC C, D, E, DT) — dépend de l'IFRI ; seul 1 relevé disponible.
- [x] Commit des changements 5.1 (fait par l'utilisateur, f38d3fe).

## Phase 6 — soumission définitive (14 octobre 2026)

- [x] Migration 00006 : `numbering_year` (rempli depuis l'ouverture), numéro et date de soumission, compteur par (année, filière), snapshot, matières, documents, drapeaux, `audit_events` générique, immuabilité par triggers, contraintes « SUBMITTED ⇒ numéro, date, snapshot » ; `refresh_transcript_analysis` corrigé hors DRAFT ; down refusé avec soumissions.
- [x] Module Go `submissions` : transaction unique, rejeu idempotent, codes distincts, moyenne exacte, numéro, drapeaux, audit sans PII, session en lecture seule prolongée.
- [x] `APPLICATION_LOCKED` dans brouillons, documents et analyse, avant la version ; CORS du POST de soumission.
- [x] UI : récapitulatif sans moyenne, dialogue natif `showModal()`, messages par code, retour à Documents sur `ADMISSION_RULES_CHANGED`, page de confirmation (copie, impression, aucune modification), redirection après refresh.
- [x] Tests : unitaires (calcul, troncature, numéro, méthodes), HTTP, intégration PostgreSQL (préconditions, concurrence réelle, verrouillage, immuabilité, rollback, migrations), Vitest, Playwright jusqu'à `SUBMITTED`.
- [x] `make check` vert ; SUBMISSION_WORKFLOW et PHASE6_VALIDATION écrits.
- [x] Commit des changements de la phase 6 (par l'utilisateur).

### Suites notées pendant la phase 6

- [x] **Phase 8** : la fiche admin doit afficher le lien de doublon **dans les deux sens** ; la première candidature d'une paire ne porte aucun drapeau (requête sur `related_application_id`).
- [x] **Phase 8** : statuts de revue dans une table séparée et historisée ; une réouverture éventuelle sera un événement audité, jamais une modification de la ligne `applications` (ADR 088). Statuts de sélection : phase 9.
- [x] **Phase 9** : classement sur la moyenne **effective** de la revue (ADR 114), fractions comparées par produit en croix ; départage IFRI confirmé (ADR 122).
- [ ] **Phase 10 ou plus tard** : consultation du récépissé par numéro + code envoyé par email, pour un candidat qui a perdu son cookie ou changé de navigateur.
- [ ] **Repli manuel** (ADR 092) : politique et seuil d'échecs d'analyse à définir avec l'IFRI avant implémentation.
- [ ] **Avant production** : `numbering_year` de chaque campagne réelle à confirmer ; politique de doublons à trancher par l'IFRI.

## Phase 7 — administration (14 octobre 2026)

- [x] Préalable : stack Playwright isolée et jetable (`infra/scripts/e2e.sh`, `make e2e`, `make check-fast`), mémoire mesurée ; seed `numbering_year` = année courante, année gelée dès le premier numéro (00007).
- [x] Migration 00007 : comptes, secrets TOTP chiffrés, codes de secours, jetons d'activation, sessions, attributions de rôle historisées ; down refusé s'il existe des comptes.
- [x] Paquets `secrets` (HKDF, trois sous-clés), `passwords` (argon2id, liste figée), `totp` (RFC 6238, QR SVG), `ratelimit` (Redis Lua) ; module `audit` partagé.
- [x] Module `admin` : connexion, MFA, activation, mot de passe, comptes, rôles et périmètres, journal ; registre de routes à permission obligatoire ; en-têtes stricts ; CLI `create-super-admin`, `reset-mfa`, `reactivate`, `reset-password`.
- [x] Interface `/admin` : connexion, activation, enrôlement et saisie TOTP, codes de secours, mon compte, comptes, journal, tableau de bord vide ; UI UX Pro Max (`pages/admin.md`).
- [x] Tests Go unitaires et intégration (PostgreSQL + Redis réels), Vitest, Playwright sur stack isolée ; `make check` vert.
- [x] Commit des changements de la phase 7 (par l'utilisateur, df6c8fb).
- [ ] **Base de dev** : recréer son volume pour repartir sans les 4 soumissions synthétiques et avec `numbering_year` = année courante (commande dans PHASE7_VALIDATION), à faire par l'utilisateur.

### Suites notées pendant la phase 7

- [x] **Phase 8** : appeler `audit.RecordAccess` à chaque ouverture de dossier ; filtrer les listes avec `Principal.Scope` ; afficher les drapeaux de doublon dans les deux sens.
- [ ] **Phase 10** : en-têtes et CSP de production dans Nginx (politique dans ADMIN_SECURITY), HSTS, proxys de confiance pour l'IP cliente de la limitation de débit, secrets (`ADMIN_SECRETS_KEY`) dans le gestionnaire de secrets.
- [ ] **Rotation de `ADMIN_SECRETS_KEY`** : trousseau multi-versions et rechiffrement à la connexion (préparé par `key_version`, non implémenté).
- [ ] **IFRI** : durée de conservation de l'audit et des IP pseudonymisées ; réinitialisation de mot de passe par email ; second facteur matériel (WebAuthn) éventuel.

## Phase 8 — back-office des dossiers soumis (14 octobre 2026)

- [x] Migration 00008 : rôles IFRI, un rôle par compte (trigger), anciens rôles révoqués ; motifs de rejet ; revues, événements, corrections, gel, preuves ; `ifri_fold` et `name_key` ; index ; garde-fous (version + événement, acteur, quatre yeux, moyenne effective revérifiée, gel, append-only) ; backfill ; down refusé dès qu'une revue a vécu.
- [x] Permissions et rôles Go ; `resolveGrants` refuse un second rôle ; couverture DA / Directeur ; `id` dans la session admin.
- [x] Module `review` : liste, dossier, tableau de bord, référentiels, flux PDF, actions de revue, corrections, preuves (file séparée, routes internes, redistribution), générateur synthétique (`api dev synthetic-applications`).
- [x] Worker : boucle de preuves séparée (`ifri_ocr.proofs`), rendu depuis la version exacte et les coordonnées stockées.
- [x] Interface : tableau de bord, liste, dossier, file des corrections, sélecteur de rôle unique ; UI UX Pro Max (`pages/admin-review.md`).
- [x] Tests : Go unitaires et intégration (PostgreSQL + Redis réels), pytest, Vitest, Playwright (deux agents et un DA, aperçu PDF, preuve OCR, temps de la liste sur 5 001 dossiers) ; `make check` vert.
- [x] Commit des changements de la phase 8 (par l'utilisateur, 48a57c8).
- [ ] **Base de dev** : la migration 00008 s'applique à la base de dev au prochain `make check` ; recréer son volume reste à faire par l'utilisateur pour repartir propre (PHASE7_VALIDATION).

### Suites notées pendant la phase 8

- [x] **Phase 9** : écrire dans `ranking_freezes` au gel d'une filière (les triggers de revue et de correction s'y fient déjà) ; ne classer que les dossiers `VALIDATED` ; validation par `selection.validate` (Directeur).
- [ ] **IFRI** : valider la liste provisoire des motifs de rejet (ROADMAP, question 6).
- [ ] **Phase 10** : la CSP de production de la SPA doit garder `frame-src 'self'` (ou `default-src 'self'`) pour l'aperçu PDF ; vérifier l'aperçu intégré dans les navigateurs cibles (Chromium headless ne l'affiche pas) ; nettoyage des images de preuve orphelines si une politique de conservation l'exige.
- [ ] **Après la phase 8** : `pg_trgm` à réévaluer si la recherche par sous-chaîne dépasse les temps mesurés sur les volumes réels.

## Phase 9 — classement, gel et sélection (14 octobre 2026)

- [x] Migration 00009 :
  - `ranking_math_subjects` ;
  - places bornées et moyenne minimale, figées après le gel ;
  - en-tête de gel complété, snapshot `ranking_entries`, `selection_events`, `selection_exports` ;
  - préconditions et cohérence revérifiées en base, ajout seul ;
  - down refusé dès qu'un gel, un export ou un seuil existe.
- [x] Module Go `selection` :
  - classement pur et exact ;
  - classement provisoire, réglages, gel transactionnel, désistements et promotions ;
  - exports CSV et demandes PDF, routes internes, redistribution.
- [x] Lecture du gel après le verrou de revue (ADR 127, correctif de la phase 8).
- [x] Worker :
  - boucle d'exports dédiée, rendu PyMuPDF et DejaVu ;
  - boucle Redis commune aux preuves et aux exports ;
  - `fonts-dejavu-core` dans l'image.
- [x] Interface `/admin/classement` (UI UX Pro Max, `pages/admin-selection.md`), entrée « Classement ».
- [x] Outillage : filtres et valeurs imposées du générateur, `api dev close-campaign` ; IA exclue des 5 000 dossiers de `make e2e`.
- [x] Tests : unitaires (critères, bornes, états, documents, CSV), intégration PostgreSQL et Redis, pytest des exports, Vitest, Playwright ; `make check` vert.
- [ ] Commit des changements de la phase 9 (par l'utilisateur).

### Suites notées pendant la phase 9

- [ ] **IFRI** : critère mathématique absent (règle par groupe ou alternative) et conduite à tenir en cas de fraude après le gel (ROADMAP, questions 3 et 4).
- [ ] **Consultation des résultats par le candidat** et notifications : hors périmètre, à cadrer (numéro + code par email, phase 10 ou suivante).
- [ ] **Phase 10** : conservation des exports PDF et du journal ; administration des dates de campagne (aujourd'hui seulement par seed ou `api dev close-campaign` en développement).
