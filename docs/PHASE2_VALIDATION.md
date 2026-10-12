# Validation phase 2 — 2026-10-12

**PHASE 2 VALIDÉE.** Tous les critères techniques demandés sont PASS. Les preuves Docker/PostgreSQL/HTTP proviennent des sorties réelles fournies par l’utilisateur sur l’hôte ; Docker reste inaccessible à Codex. Aucun candidat, candidature, upload, OCR métier, authentification ou écran candidat créé.

| Contrôle | État | Preuve |
|---|---|---|
| Git initial | PASS | master, working tree clean avant modification |
| Frontend build/typecheck/lint/Vitest/Prettier | PASS | Exécutés dans Codex avec dépendances installées ; 2 tests |
| Python pytest | PASS | Environnement uv verrouillé ; 10 tests |
| Python Ruff lint/format | PASS | Exécution réelle ; 5 fichiers |
| Script smoke phase 2 Ruff | PASS | Configuration Ruff du projet |
| Go test ./..., vet, gofmt | PASS | Hôte après go mod tidy, puis revalidation Codex ; gofmt sans sortie |
| Compose config, profil test | PASS | config --quiet normal et --profile test |
| Build API/db | PASS | Image hôte construite, binaires compilés avec Go 1.27.2 |
| Migrations 00001 et 00002 | PASS | db up hôte et Up depuis schéma vide en tests PostgreSQL |
| Seed puis second seed | PASS | Deux commandes seed PASS ; test compare toutes les lignes et UUID avant/après |
| Contraintes PostgreSQL et matrice | PASS | TestPostgresReferences complet, 0,48 s ; package 0,501 s |
| Rollback puis réapplication | PASS | DownTo(0), Up et seed dans schéma temporaire |
| Stack Docker phase 2 | PASS | Cinq services healthy ; initialiseur MinIO sortie 0 |
| Bootstrap smoke | PASS | Redis PONG authentifié, base/utilisateur PostgreSQL, /health 200, /ready 200, CORS, bucket anonyme refusé |
| Nouveaux endpoints HTTP réels | PASS | references_smoke.py : six endpoints, matrice, filtres et erreurs |
| GL/DT_DWM et GL/BAC_E réels | PASS | Réponses JSON imprimées par le smoke et fournies par l’utilisateur |
| git diff --check | PASS | Exécuté dans Codex |
| CI GitHub | NON TESTÉ | Workflow préparé, aucun push ni déclenchement |

## Données créées et invariants

8 tables métier : admission_campaigns, programs, origin_qualifications, subjects, campaign_programs, rule_set_versions, program_qualification_eligibilities, expected_subject_rules. Goose possède en plus sa table technique goose_db_version.

5 filières, 7 qualifications, 11 matières, 5 offres et grilles publiées, 23 combinaisons éligibles, 69 matières attendues pour DEV_LOCAL. Campagne explicitement locale/non officielle ; quotas NULL. Aucun coefficient ni note dans le schéma des règles ou dans les réponses.

UUID v4, codes uniques, FK restrictives, dates cohérentes, statuts CHECK, quotas >=0 ou NULL, positions >0 et contiguës à publication, unicité qualification/version et matière/position par combinaison. Grilles publiées immuables ; les combinaisons absentes restent non autorisées avec motif RULE_NOT_DEFINED. Refus explicite distinct : INELIGIBLE.

## Tests ajoutés et réellement exécutés

- Service : règles éligibles/refusées/absentes, grilles incomplètes, doublons/ordre, codes inconnus, campagne absente/ambiguë/fermée et bornes temporelles.
- HTTP : six routes, paramètres absents/vides/répétés/inconnus/mal encodés, inconnus 404 et non-éligibilité 200.
- Invariant : ExpectedSubjectRule contient uniquement Code/Name/Position ; aucune valeur scolaire en JSON.
- PostgreSQL réel : schéma isolé vide, Up, seed deux fois sans modification, seed production refusé, 35 couples dont 23 éligibles, huit cas explicites dont BAC D, BAC E pour les cinq filières, refus connu, isolation campagne, quotas null/zéro, contraintes FK/uniques/checks, publication/immutabilité, branches INSERT/UPDATE/DELETE du trigger et absence de colonnes coefficient.
- HTTP avec repository PostgreSQL réel, puis rollback et réapplication.
- Smoke via HTTP local réellement démarré : tous les endpoints, filtres, cas métier, erreurs et réponses réelles ci-dessous.

## Extraits des réponses HTTP réelles

Campagne DEV_LOCAL : id `1bdd2d2e-330e-404c-a876-0e407326090c`, statut OPEN, academicYear DEVELOPMENT ; grille version 1. Les deux réponses ont eligible=true et reason=ELIGIBLE.

GL + DT_DWM :

```json
[
  {"code":"APPLIED_MATH","name":"Mathématiques appliquées","position":1},
  {"code":"FRENCH","name":"Français","position":2},
  {"code":"WEB_SITES_APPS","name":"Sites et Applications Web","position":3}
]
```

GL + BAC_E :

```json
[
  {"code":"MATH","name":"Mathématiques","position":1},
  {"code":"FRENCH","name":"Français","position":2},
  {"code":"MANUFACTURING_STUDY","name":"Étude de Fabrication","position":3}
]
```

## Commandes exécutées

Frontend : npm run build, typecheck, lint, test, format:check. Python : uv run --locked --no-sync pytest, ruff check ., ruff format --check . Go : go mod tidy, go test ./..., go vet ./..., gofmt -l ; dépendances officiellement résolues dans go.mod/go.sum.

Sur l’hôte depuis la racine :

```powershell
docker compose up -d --build --wait --wait-timeout 240 api
docker compose exec -T api db up
docker compose exec -T api db seed
docker compose exec -T api db seed
docker compose --profile test run --build --rm references-tests
uv run --project services/ocr --locked --no-sync python infra/scripts/smoke.py
uv run --project services/ocr --locked --no-sync python infra/scripts/references_smoke.py
```

Le premier build de phase 2 a également construit toute la stack, et compose ps --all a confirmé les publications loopback API 18080, MinIO 19000/19001, PostgreSQL/Redis privés et worker sans port. Dernier smoke : cinq services healthy. Aucun volume supprimé ; tests uniquement dans un schéma temporaire propre à l’exécution, supprimé à la fin.

## Correction et historique

Première exécution : migration 00001 PASS, seed et intégration FAIL (42703 : record NEW sans eligibility_id), smoke phase 2 FAIL (DEV_LOCAL absent). Le seed transactionnel a été annulé.

Correction : migration 00002 remplace la fonction de trigger avec des branches PL/pgSQL séparées, sans modifier 00001 déjà appliquée. Test de régression ajouté et rollback adapté aux deux migrations. Dernière exécution hôte : migrations, deux seeds, toute l’intégration et HTTP PASS. Ces échecs historiques sont résolus.

## Hypothèses et suites

Matrice issue de l’instruction utilisateur de phase 2, pas d’un communiqué officiel archivé dans le dépôt. Quotas non définis, aucun quota officiel inventé. Une grille publiée par offre, remplacement intra-offre à cadrer avant CRUD ; nouvelle campagne supportée sans recompilation. Aucun blocage de phase 2 restant. CI GitHub et contrôles UI approfondis restent non exécutés, hors preuve de cette phase sans écran métier.

Git : master, modifications non commitées ; aucun commit/push automatique. Liste exacte des fichiers de cette phase ci-dessous.

### Fichiers modifiés

- `.github/workflows/checks.yml`
- `AGENTS.md`
- `README.md`
- `apps/web/vite.config.ts`
- `compose.yaml`
- `docs/ARCHITECTURE.md`
- `docs/DATA_MODEL.md`
- `docs/DECISIONS.md`
- `docs/PRODUCT_SPEC.md`
- `docs/TODO.md`
- `services/api/Dockerfile`
- `services/api/cmd/api/main.go`
- `services/api/go.mod`
- `services/api/go.sum`
- `services/api/internal/platform/httpserver/router.go`
- `services/api/migrations/README.md`

### Fichiers créés

- `docs/ADMISSION_RULES.md`
- `docs/PHASE2_VALIDATION.md`
- `infra/scripts/references_smoke.py`
- `services/api/cmd/db/main.go`
- `services/api/internal/modules/references/http.go`
- `services/api/internal/modules/references/model.go`
- `services/api/internal/modules/references/postgres_integration_test.go`
- `services/api/internal/modules/references/repository.go`
- `services/api/internal/modules/references/service.go`
- `services/api/internal/modules/references/service_test.go`
- `services/api/migrations/00001_references.sql`
- `services/api/migrations/00002_fix_rule_child_guard.sql`
- `services/api/migrations/embed.go`
- `services/api/seeds/development.sql`
- `services/api/seeds/references.sql`
- `services/api/seeds/seed.go`
