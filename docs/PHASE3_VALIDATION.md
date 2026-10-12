# Validation phase 3 — 2026-10-12

**Implémentation et validations techniques de phase 3 terminées. Interface fonctionnelle confirmée sur l’hôte ; contrôles visuels détaillés aux quatre largeurs, clavier et zoom non confirmés.** Aucun document/OCR/soumission définitive ajouté. Les preuves des phases antérieures ne sont pas utilisées comme preuve d’exécution des nouveaux endpoints.

| Contrôle | Résultat | Preuve / limite |
|---|---|---|
| Git initial | PASS | Branche master, working tree clean avant modifications |
| Lecture des sources et inspection | PASS | AGENTS, README, specs, architecture, données, règles, ADR, TODO, phase 2, master ; migrations/seeds/tests existants inspectés |
| UI UX Pro Max | PASS | Skill lu ; génération/persistance page et recherches UX/produit/Vue exécutées ; master inchangé |
| Frontend build | PASS | Vite 8.3.4, page candidature lazy construite |
| Frontend typecheck, ESLint, Prettier | PASS | Exécutés après correction des globals navigateur ESLint et formatage |
| Frontend Vitest | PASS | 12 tests : 10 parcours candidat et 2 techniques existants |
| Go test ./... | PASS | Exécution sans cache de tests ; nouveau module drafts et bootstrap/référentiels passent |
| Go vet ./..., gofmt | PASS | Exécutés ; gofmt sans sortie |
| Compilation des tests PostgreSQL | PASS | -tags=integration -run '^$' ; ne vaut pas exécution de ces tests |
| Python pytest / Ruff lint et format | PASS | Environnement uv verrouillé ; 10 tests, 5 fichiers contrôlés |
| Script drafts_smoke Ruff lint/format | PASS | Exécuté avec configuration du projet Python |
| Compose config et profil test | PASS | config --quiet normal/profil test |
| Migration 00003 sur PostgreSQL réel | PASS | db up hôte : PASS |
| Contraintes/rollback/transactions/session DB | PASS | TestPostgresDraftLifecycle 0,44 s ; package drafts 0,491 s ; intégration référentiels également PASS |
| Rebuild et stack Docker phase 3 healthy | PASS | Build hôte, compose ps : cinq healthy, minio-init Exited (0) |
| Parcours HTTP réel avec cookie | PASS | drafts_smoke : création/profil/qualification/filière/restauration, cookie et hash vérifiés sans secret affiché |
| Interface fonctionnelle dans le navigateur hôte | PASS | Confirmation utilisateur : « Côté interface, aucun problème. ça marche. » ; pas de constat visuel direct Codex |
| Visuel 375/768/1024/1440, clavier et zoom | NON TESTÉ | Aucun screenshot ou rendu réel obtenu |
| CI GitHub | NON TESTÉ | Préparée, aucun push ni déclenchement |
| git diff --check | PASS | Exécuté |

## Périmètre livré

Migration 00003_candidate_drafts.sql, tables candidates/applications/application_draft_sessions, FK composite offre/campagne et version optimiste. Migrations 00001/00002 inchangées. Cookie opaque 256 bits hashé, expiration/revocation, transactions, normalisation, qualification/filière contrôlées PostgreSQL et retrait du choix incompatible.

POST /api/v1/applications/drafts ; GET /current ; PATCH /current/profile, /current/academic-background, /current/program. Aucun GET par UUID ni endpoint de suppression/soumission. JSON strict borné, Origin obligatoire, CORS credentials explicites, cookie HttpOnly/Lax/Secure production, DTO contrôlés, logs sans identité/secret, limitation de création par IP.

Vue /candidature : CandidateLayout, stepper ol, FormField/Input/Select/Button/Alert/Skeleton natifs, radios en cartes, store Pinia de données serveur, client HTTP central. Quatre étapes, sauvegarde après pause et avant navigation, retour/édition, restauration, campagne fermée, erreurs réseau/session et résumé sans soumission. Palette, typographie et master conservés. Aucun package ajouté ; aucun React.

## Tests ajoutés

Go unit/HTTP : génération/hachage/flags cookie, accès sans session ou jeton invalide, expirée/révoquée, Origin absent/interdit, champs inconnus et absence de soumission, normalisation Unicode/email/téléphone international, erreurs internes masquées, version requise, inéligibilité, aucune route UUID, limitation création et preflight CORS credentials. Configuration durée invalide.

PostgreSQL : création transactionnelle (collision de hash ne laisse pas de Candidate orphelin), secret hashé, ownership, profil normalisé/valeurs invalides, version obsolète, qualification/année, BAC_E+GL, DT_DWM+GL, DT_EAP+GL refusé, changement DT_DWM→DT_EAP retirant GL, campagne fermée, expiration/révocation, état OCR rejeté par CHECK, migration depuis schéma vide et rollback/réapplication de 00003. Exécution uniquement dans un schéma isolé temporaire.

Vue : rendu quatre étapes, validation/focus, sauvegarde avant changement d’étape, qualifications API, filières autorisées, aucune filière, résumé et sélection, restauration sans duplication/édition, erreur API conservant saisie, campagne fermée, expiration sans retry et autosave debounce/version serveur. Tests de comportements, pas de CSS. Les API sont simulées dans ces tests unitaires uniquement ; le code produit utilise fetch réel.

## Limites effectivement constatées

Docker : permission denied sur /var/run/docker.sock. Chromium installé essayé avec profil temporaire /tmp : échec `setsockopt: Operation not permitted`, code 133. Aucun outil navigateur connecté ni rendu visuel exploitable ; absence de Playwright/puppeteer dans les dépendances projet. Les quatre largeurs et l’accessibilité visuelle ne sont pas présentées comme testées par jsdom/build.

## Derniers contrôles sur l’hôte, depuis la racine

```powershell
docker compose up -d --build --wait --wait-timeout 240 api
docker compose exec -T api db up
docker compose --profile test run --build --rm references-tests
uv run --project services/ocr --locked --no-sync python infra/scripts/smoke.py
uv run --project services/ocr --locked --no-sync python infra/scripts/references_smoke.py
uv run --project services/ocr --locked --no-sync python infra/scripts/drafts_smoke.py
docker compose ps --all
npm --prefix apps/web run dev
```

Attendu : nouvelle migration PASS, intégration référentiels+brouillons PASS, cinq services healthy et les trois smoke checks PASS. Le smoke brouillon utilise exclusivement des données synthétiques, ne révèle pas le cookie et conserve un brouillon local sans soumission. Aucun volume supprimé ni autre projet arrêté.

Ouvrir http://localhost:5173/candidature : créer un brouillon, renseigner identité, choisir DT/DWM et année passée, GL, voir résumé, recharger, modifier qualification vers DT/EAP et vérifier retrait de GL. Vérifier BAC E + GL, étape précédente et persistance. Dans les outils responsive du navigateur, contrôler 375/768/1024/1440 et zoom 200 % : aucun overflow global, textes/labels complets, focus visible, clavier et erreurs près des champs. Transmettre les sorties et observations avant de conclure la phase.

Confirmation utilisateur : interface fonctionnelle, aucun problème signalé. Cette observation ne constitue pas une preuve d’exécution des tests PostgreSQL/smoke ni une confirmation détaillée des quatre largeurs, du clavier et du zoom. Ces contrôles restent explicitement en attente.

## Preuves finales fournies par l’hôte

La dernière exécution utilisateur confirme : build API/db, db up PASS, tous les tests Go avec tag integration PASS, bootstrap smoke PASS et six endpoints référentiels PASS. PostgreSQL/Redis/MinIO/API/worker healthy ; initialiseur sortie 0. /health et /ready HTTP 200.

Parcours de brouillon réel : cookie HttpOnly/SameSite=Lax/Path limité/expiration, hash SHA-256 en base ; BAC_E+GL et DT_DWM+GL enregistrés ; DT_EAP+GL refusé HTTP 422 ; passage DT_DWM→DT_EAP supprime GL ; récupération du même brouillon. Sans session 401, sans Origin 403, version obsolète 409. Le secret n’a pas été affiché. Données synthétiques conservées en brouillon, aucune soumission.

Tests PostgreSQL réels : création atomique et hash, ownership, normalisation/validation, qualifications/éligibilité/invalidation, campagne fermée, expiration/révocation, contraintes et rollback/réapplication PASS. Tests de référentiels et techniques existants également PASS. Ces résultats remplacent les mentions historiques d’attente ci-dessus ; les contrôles visuels détaillés et CI restent NON TESTÉS.

Aucun blocage technique restant pour ce périmètre. La confirmation utilisateur générale de l’interface ne vaut pas contrôle visuel direct Codex ou certification d’accessibilité. Aucun chantier documents/OCR commencé.
