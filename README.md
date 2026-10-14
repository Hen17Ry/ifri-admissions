# IFRI Admissions

Plateforme d’admissions IFRI/UAC : phases 1 à 4 validées, parcours candidat en brouillon et PDF privés versionnés. La phase 5 ajoute l’analyse hybride du relevé et sa confirmation, validées sur fixtures synthétiques. La phase 5.1 corrective est en validation. Aucun calcul de moyenne, soumission définitive ou espace administrateur.

**Bootstrap technique validé** : dépendances et locks, contrôles frontend/Go/Python, stack Docker et smoke check réels, persistance du volume PostgreSQL, panne/reprise de readiness, arrêt propre du worker et appel Vue → API confirmé dans le navigateur. Contrôles visuels approfondis et CI GitHub non exécutés. Voir [le rapport exact](docs/BOOTSTRAP_VALIDATION.md).

## Prérequis

- Docker Desktop avec moteur Linux (WSL2 sur Windows), Compose ≥ 2.20 ; `docker info` doit fonctionner.
- Node.js 24 LTS, version de référence 24.18.1 (`.node-version`), npm 11.16.0 pour le frontend. Node 24 ≥ 24.15 requis par les dépendances retenues.
- Pour lancer/tester les services hors Docker : Go 1.27.2, Python 3.14 (référence Docker 3.14.8), uv 0.13.0. Python 3.15 récemment publié n’est pas retenu pour ce worker.
- Accès aux registres npm, PyPI, Go et Docker. La première construction MinIO compile les sources et peut prendre plusieurs minutes.

Les commandes ci-dessous sont utilisables dans PowerShell. Sur Linux/macOS, remplacer seulement `Copy-Item` par `cp` et éventuellement `python` par `python3`. Aucun script Bash n’est nécessaire pour démarrer ou vérifier le projet.

## Organisation

```text
apps/web/                 Vue 3, TypeScript, Vite, Router, Pinia et Tailwind
services/api/             Go : configuration, PostgreSQL, HTTP et état du stockage
services/ocr/             Package Python, bootstrap Redis et heartbeat interne
infra/minio/              Images MinIO et mc construites depuis des releases sources
infra/nginx/              Documentation ; configuration de déploiement différée
infra/scripts/            Lanceur natif et smoke check portables en Python
compose.yaml              Infrastructure locale, réseau interne et volumes nommés
docs/                     Spécifications, ADR, roadmap et validation
design-system/            Source de vérité visuelle existante
```

L’API est la source future des décisions métier. Le worker ne dispose ni d’une API publique ni d’un accès PostgreSQL. Depuis la phase 5, il consomme Redis Streams et maintient un heartbeat indépendant du traitement documentaire. Nginx n’intervient pas dans ce bootstrap.

## Configuration

Depuis la racine :

```powershell
Copy-Item .env.example .env
```

Remplacer toutes les valeurs de credentials `replace-dev-*` par des valeurs locales propres. Ces exemples sont publics, réservés au développement. `.env` est ignoré ; aucun secret ne doit porter un préfixe `VITE_`. Dans ce bootstrap, utiliser des valeurs simples sans commentaires en fin de ligne ; les scripts acceptent `KEY=value` et des valeurs entièrement citées.

`.env.example` utilise des adresses loopback pour les exécutions natives, dont `API_HOST=127.0.0.1` ; Compose impose `API_HOST=0.0.0.0` à l’intérieur du conteneur et publie seulement sur le loopback hôte. Compose remplace les hôtes par `postgres`, `redis` et `minio`, et conserve leurs ports internes standards. API_PORT et MINIO_API_PORT/MINIO_CONSOLE_PORT configurent les publications hôte. PostgreSQL et Redis restent privés dans Docker par défaut ; leurs variables HOST/PORT servent aux exécutions natives et à l’override optionnel. Aligner MINIO_ENDPOINT sur MINIO_API_PORT pour le smoke check et l’API native. L’API en conteneur reçoit automatiquement `http://minio:9000`. Vite lit uniquement `API_PORT` du `.env` racine pour son proxy technique.

`FRONTEND_ORIGIN` vaut `http://localhost:5173`. Toute autre origine est refusée par l’API ; aucune origine `*`. Le serveur Vite écoute uniquement le loopback. Ouvrir l’URL documentée ci-dessous.

Ce Compose est **local**, pas une configuration production. L’API refuse notamment HTTP pour ses origines/endpoints en production, PostgreSQL sans `verify-full`, Redis sans TLS et les mots de passe exemples. TLS, comptes de service S3 limités, authentification et déploiement sécurisé seront configurés dans les phases prévues. Seul l’initialiseur MinIO reçoit les credentials root ; l’API reçoit un compte S3 limité depuis la phase 4 ; le worker lit le PDF via le listener interne Go sans credential S3.

## Démarrage habituel

```powershell
docker compose config --quiet
docker compose up -d --build --wait --wait-timeout 240
docker compose ps --all
python infra/scripts/smoke.py
```

Le script exige les cinq services en état `healthy`, l’initialiseur MinIO terminé sans erreur, un PING Redis authentifié donnant PONG, une connexion PostgreSQL à la base/utilisateur configurés, `/health` et `/ready` à `ok`, CORS autorisant seulement l’origine configurée et le refus de l’accès anonyme au bucket. Un échec retourne un code non nul. Il ne suffit pas de voir un conteneur `running`.

Dans un autre terminal, depuis la racine :

```powershell
Set-Location apps/web
npm install
npm run dev
```

Ouvrir **http://localhost:5173**. La page vérifie réellement l’API et ses trois dépendances. Elle affiche une indisponibilité plutôt que de supposer qu’elles fonctionnent. Le worker se vérifie avec Compose ; aucun état worker fictif n’est affiché.

`apps/web/package-lock.json` a été généré par npm sur l’hôte et vérifié : utiliser désormais `npm ci` pour les installations frontend reproductibles. `services/ocr/uv.lock` a également été généré par uv sur l’hôte et vérifié ; utiliser `uv sync --locked` pour les installations Python suivantes. Aucun lockfile fabriqué manuellement.

## API et worker hors Docker

Éviter de démarrer une seconde API sur le même port ou deux workers partageant le même heartbeat local. Pour les exécutions natives, choisir des ports hôte libres dans `.env` (`POSTGRES_PORT` et `REDIS_PORT`), puis activer explicitement `compose.native.yaml`. La stack habituelle ne publie ni PostgreSQL ni Redis. Arrêter les services applicatifs Docker avant de lancer leurs équivalents natifs :

```powershell
docker compose -f compose.yaml -f compose.native.yaml up -d postgres redis minio minio-init
docker compose stop api ocr-worker
python infra/scripts/dev.py api
```

Le lanceur charge le `.env` racine, laisse les variables déjà exportées prioritaires et exécute Go depuis `services/api`. Équivalent avec variables déjà présentes : `go run -buildvcs=false ./cmd/api` depuis `services/api`. Le drapeau permet aussi de travailler avant l’initialisation Git.

Pour le worker, dans un autre terminal :

```powershell
Set-Location services/ocr
uv sync --locked
Set-Location ../..
python infra/scripts/dev.py worker
```

Le worker échoue si la configuration ou Redis sont invalides, écrit des logs JSON et s’arrête proprement avec Ctrl+C. Son healthcheck lit un heartbeat temporaire, renouvelé seulement après un PING Redis réussi ; un heartbeat périmé ne vaut pas « prêt ». Depuis la phase 5, l’extraction traite les relevés éligibles de façon asynchrone ; un document illisible ne fait pas échouer la liveness.

## Health checks et ports

| Composant | Accès local | Vérification |
|---|---|---|
| Frontend | 5173 | Page technique dans le navigateur |
| API | 18080 (8080 interne) | `GET /health` et `GET /ready` |
| PostgreSQL | Aucun port hôte par défaut ; 5432 interne | `pg_isready` en conteneur ; PING pgx via `/ready` |
| Redis | Aucun port hôte par défaut ; 6379 interne | PING authentifié en conteneur et via `/ready` |
| MinIO API | 19000 (9000 interne) | `/minio/health/ready` |
| MinIO Console | 19001 (9001 interne) | Console locale, credentials de votre `.env` |
| Worker | Aucun port | `python -m ifri_ocr.worker healthcheck` en conteneur |
| minio-init | Aucun port | Sortie 0 et bucket privé |

Toutes les publications Docker sont liées à `127.0.0.1`. PostgreSQL, Redis et le worker ne sont pas publiés dans la stack habituelle. Les services partagent un réseau Docker `internal: true`. API et MinIO rejoignent aussi le bridge `local`, nécessaire pour publier leurs ports sur le loopback hôte ; PostgreSQL, Redis, worker et initialiseur restent uniquement sur le réseau interne. L’override `compose.native.yaml` ajoute à PostgreSQL et Redis des publications loopback et le bridge local, uniquement si demandé. Les volumes `postgres-data`, `redis-data`, `minio-data` persistent après `down`.

```powershell
Invoke-RestMethod http://localhost:18080/health
Invoke-RestMethod http://localhost:18080/ready
docker compose exec ocr-worker python -m ifri_ocr.worker healthcheck
```

`/health` retourne 200 si l’API répond, indépendamment des dépendances. `/ready` effectue pgx Ping, Redis PING et contrôle HTTP MinIO avec délai borné : 200 si tous disponibles, 503 sinon. Le corps indique seulement `ok`/`unavailable`, jamais mots de passe ou détails de connexion. L’initialisation du bucket est vérifiée au démarrage par la dépendance Compose et par le smoke check ; `/ready` ne teste pas des permissions d’upload encore inexistantes.

## Tests et qualité

Frontend, depuis `apps/web` après installation :

```powershell
npm run build
npm run typecheck
npm run lint
npm run test
npm run format:check
# Correction de formatage si nécessaire :
npm run format
```

API, depuis `services/api` :

```powershell
gofmt -l .
go vet ./...
go test ./...
go build -buildvcs=false ./cmd/api
```

`gofmt -l .` doit rester vide ; `gofmt -w .` applique les corrections. Les tests vérifient liveness/readiness, CORS, configuration et référentiels. Goose 3.29.0 gère désormais la migration de phase 2 ; voir `services/api/migrations/README.md`.

Worker, depuis `services/ocr` :

```powershell
uv sync --locked
uv run --no-sync pytest
uv run --no-sync ruff check .
uv run --no-sync ruff format --check .
```

Le projet Python possède un `pyproject.toml`, une `.venv` ignorée et des versions exactes, avec extraction PyMuPDF et moteur Tesseract en Docker depuis la phase 5. Les tests couvrent configuration, PING initial, expiration du heartbeat et contexte des logs.

La CI minimale dans `.github/workflows/checks.yml` reprend les contrôles des trois stacks et un smoke check Compose. Elle ne publie rien. Elle n’a pas été exécutée sur GitHub dans cette session ; les installations y nécessitent les registres réseau.

## Commandes utiles et nettoyage

```powershell
docker compose logs --tail 100 api ocr-worker
docker compose logs -f api
docker compose restart api
docker compose up -d --build api ocr-worker
docker compose down
```

`down` conserve les données. Pour **effacer toutes les données locales** et repartir de zéro, seulement si souhaité :

```powershell
docker compose down --volumes --remove-orphans
```

Changer `POSTGRES_PASSWORD` dans `.env` ne change pas le mot de passe d’une base déjà initialisée : modifier le rôle SQL ou recréer volontairement le volume local. Ne pas utiliser la commande destructive pour une base à conserver.

## UI et sécurité du bootstrap

Master : `design-system/ifri-admissions/MASTER.md`. Page technique sobre avec tokens sémantiques, labels textuels de disponibilité, focus visible, lien d’évitement et reduced-motion. Fontsource livre Lexend et Source Sans 3 localement via le bundle, sous licence OFL-1.1 ; aucune requête Google Fonts. Les assets sont inclus dans le build validé ; leur rendu détaillé et les contrôles visuels aux quatre largeurs restent à vérifier.

Les aliases Vite/TypeScript `@/*` sont préparés. shadcn-vue/Reka seront ajoutés à la demande d’un composant, avec leurs règles Vue/Tailwind compatibles et le master existant ; aucun composant ou package React n’est introduit. Les dossiers de fonctionnalités/modules ne seront créés qu’à leur première utilisation.

MinIO Community et mc sont archivés et distribués en sources : les Dockerfiles fixent leurs releases et les compilent. Cette solution est limitée au développement demandé ; la maintenance, les licences et le choix S3 de production devront être revus avant déploiement. Les buckets sont privés, l’initialiseur applique `anonymous set none`, et le smoke check teste le refus anonyme.

## Phase 2 — référentiels et API de lecture

**Phase 2 validée** : migrations, seed idempotent, contraintes/matrice PostgreSQL et HTTP réel. Preuves dans [PHASE2_VALIDATION.md](docs/PHASE2_VALIDATION.md).

Après résolution des nouvelles dépendances Go depuis la racine (Go installé) :

```powershell
go -C services/api mod tidy
docker compose up -d --build --wait --wait-timeout 240
docker compose exec -T api db up
docker compose exec -T api db seed
docker compose exec -T api db seed
docker compose --profile test run --build --rm references-tests
uv run --project services/ocr --locked --no-sync python infra/scripts/smoke.py
uv run --project services/ocr --locked --no-sync python infra/scripts/references_smoke.py
```

Le second seed confirme la relance opérationnelle ; les tests PostgreSQL comparent aussi toutes les lignes/UUID avant et après deux seeds. Le runner de test crée et supprime uniquement un schéma temporaire unique ; aucun volume existant effacé et aucun port PostgreSQL supplémentaire publié. Les tests d’intégration sont exécutés avec `-tags=integration` ; les tests unitaires restent `go test ./...`.

`db up` est idempotent et séparé du démarrage HTTP. `db seed` est autorisé uniquement avec APP_ENV development/test. Il charge les référentiels canoniques puis DEV_LOCAL (campagne de démonstration non officielle), sa matrice et des quotas NULL. Aucun seed automatique en production.

Contrat et matrice : [docs/ADMISSION_RULES.md](docs/ADMISSION_RULES.md). Exemples PowerShell, URLs également utilisables dans un navigateur :

```powershell
Invoke-RestMethod 'http://localhost:18080/api/v1/programs?campaign=DEV_LOCAL'
Invoke-RestMethod 'http://localhost:18080/api/v1/expected-subjects?campaign=DEV_LOCAL&program=GL&qualification=DT_DWM'
Invoke-RestMethod 'http://localhost:18080/api/v1/expected-subjects?campaign=DEV_LOCAL&program=GL&qualification=BAC_E'
```

Toutes les routes sont GET ; aucune authentification, écriture administrative, candidature ou extraction documentaire. Les notes et coefficients restent absents des règles. Aucun écran métier ajouté.

## Phase 3 — brouillon candidat

**Validations techniques réussies** : migration, intégration PostgreSQL, smoke HTTP avec session réelle et interface fonctionnelle confirmée par l’utilisateur. Contrôles visuels détaillés aux quatre largeurs, clavier/zoom et CI non confirmés ; voir le rapport.

Route : **http://localhost:5173/candidature** après démarrage Vite. Quatre étapes seulement, brouillon PostgreSQL restauré par cookie HttpOnly. Aucun document ou bouton de soumission définitive. Contrat, session, autosave et limites : [CANDIDATE_FLOW.md](docs/CANDIDATE_FLOW.md) ; preuves : [PHASE3_VALIDATION.md](docs/PHASE3_VALIDATION.md).

Durée de session : DRAFT_SESSION_TTL_SECONDS (défaut local 604800 secondes, 7 jours ; valeur explicite obligatoire en production). Le proxy Vite conserve les cookies et l’origine configurée. Ouvrir localhost plutôt qu’une autre origine ; aucune donnée personnelle ou session dans localStorage.

Depuis la racine, pour la migration 00003 et la validation réelle :

```powershell
docker compose up -d --build --wait --wait-timeout 240 api
docker compose exec -T api db up
docker compose --profile test run --build --rm references-tests
uv run --project services/ocr --locked --no-sync python infra/scripts/smoke.py
uv run --project services/ocr --locked --no-sync python infra/scripts/references_smoke.py
uv run --project services/ocr --locked --no-sync python infra/scripts/drafts_smoke.py
npm --prefix apps/web run dev
```

Le profil test exécute désormais les tests PostgreSQL des référentiels et des brouillons dans des schémas isolés. Le dernier smoke utilise un profil synthétique, contrôle le cookie sans afficher son secret, la normalisation et BAC_E/GL, DT_DWM/GL, DT_EAP/GL refusé et l’invalidation GL ; il conserve un brouillon local de test sans soumission ni suppression.

Le contrôle navigateur réel reste distinct des tests Vitest : vérifier restauration après recharge, retour/modification, erreurs, clavier, zoom 200 % et largeurs 375/768/1024/1440. Ne jamais interpréter un build ou un test jsdom comme validation visuelle.

## Documents PDF — phase 4

Voir docs/DOCUMENT_WORKFLOW.md et docs/PHASE4_VALIDATION.md. L’API native nécessite qpdf dans PATH ; l’image Docker l’installe. Copier les nouvelles variables S3_ACCESS_KEY, S3_SECRET_KEY, S3_REGION et DOCUMENT_MAX_BYTES depuis .env.example dans un environnement existant en remplaçant les credentials d’exemple. MinIO init crée le compte S3 limité ; ne pas utiliser le compte racine pour l’API. S3_ENDPOINT natif pointe sur le port MinIO local, Compose le remplace par l’endpoint interne.

Depuis la racine, migration et tests d’intégration :

```powershell
docker compose up -d --build --wait --wait-timeout 240 api
docker compose exec -T api db up
docker compose --profile test run --build --rm references-tests
uv run --project services/ocr --locked --no-sync python infra/scripts/documents_smoke.py
```

Navigateur réel (API démarrée et campagne DEV_LOCAL seedée), depuis apps/web : arrêter d’abord le serveur Vite existant sur 5173 avec Ctrl+C. Playwright démarre et arrête son propre serveur avec réoptimisation des dépendances (--force).

```powershell
npx playwright install chromium
npm run test:browser
```

Les captures de toutes les étapes aux quatre largeurs sont dans browser-artifacts, ignoré par Git. Les fixtures et brouillons sont synthétiques ; aucune soumission. Ne pas détruire les volumes pour exécuter les tests.

## Phase 5 en cours — dépendances OCR

Python 3.14 conservé. Le worker utilise PyMuPDF/RapidFuzz, Tesseract système avec fra+eng et, depuis la phase 5.1, RapidOCR/onnxruntime pour les cellules numériques BAC, installés dans l’image Docker. Avant de reconstruire cette image, générer le lock officiel et synchroniser le projet :

```powershell
uv lock --project services/ocr
uv sync --project services/ocr --locked
```

La nouvelle configuration requiert OCR_INTERNAL_SECRET aléatoire (32 caractères minimum), OCR_MIN_CONFIDENCE, OCR_ANALYSIS_TIMEOUT_SECONDS, OCR_PAGE_TIMEOUT_SECONDS et OCR_MAX_PAGES ; valeurs locales dans .env.example. Le secret ne doit jamais porter VITE_. Listener callback Go 8081 uniquement interne à Docker, aucune publication. Les tests réels et le benchmark sont encore en attente ; consulter PHASE5_VALIDATION. Aucun calcul ni soumission ajoutés.

Validation OCR synthétique, après nouvelle stack reconstruite et migration appliquée :

```powershell
docker compose --profile test run --build --rm references-tests
docker compose --profile test run --build --rm ocr-tests
```

Le second service réutilise le build du service ocr-worker via additional_contexts, exécute pytest avec Tesseract réel, régénère les fixtures génériques dans son tmpfs et échoue si un octet diffère des copies suivies (il n'écrit jamais dans le dépôt), puis exécute le benchmark BAC synthétique réaliste. `make fixtures` régénère délibérément les fixtures suivies.


## Phase 5.1 — validation locale (`make check`)

L’analyse est automatique dans la carte du relevé : cinq étapes, confirmation/rejet dans Documents, aucun bouton de lancement OCR. Les relevés BAC général scannés sont lus cellule par cellule (paquet `ifri_ocr.bac`) — chiffres par RapidOCR, libellés et série par Tesseract — et acceptés uniquement par l'arithmétique (voir docs/OCR_PIPELINE.md). `OCR_RAPIDOCR_ENABLED=false` repasse les chiffres sur Tesseract ; le modèle RapidOCR est téléchargé au build de l'image worker et vérifié par SHA-256, jamais à l'exécution.

```sh
make check          # tout : statique, stack Docker + intégration + smoke + ocr-tests, Playwright
make check-static   # sans Docker : web, Go, Python, Ruff, Compose config
make check-stack    # stack reconstruite, migrations, seed, intégration Go, smoke HTTP, ocr-tests
make check-browser  # Playwright (Vite démarré par la suite, port 5173 libre)
make ocr-private    # corpus privé ignoré : <nom>.pdf + <nom>.expected.json ; ARGS=--keep-debug
                    # ou ARGS="--numeric-engine tesseract" pour comparer les lecteurs de chiffres
make ocr-engines-benchmark  # benchmark BAC synthétique avec RapidOCR (production) puis Tesseract
```

`make check` tient lieu de CI tant que le dépôt n'a pas de remote ; il continue après un échec et imprime un tableau PASS/FAIL avec durées et le chemin des journaux.

Corpus privé : chaque `services/ocr/tests/private-fixtures/<nom>.pdf` accompagné de `<nom>.expected.json` (`{"qualification": "BAC_D", "program": "GL", "series": "D", "subjects": {"MATH": {"grade": "…", "coefficient": "…"}, …}}`), tous deux ignorés par Git et les contextes Docker. Montage en lecture seule sous un nom anonyme, budget de page de production, validation Go incluse ; le rapport ne contient que des issues par champ (EXACT / FALSE / MISSING) et le nombre de valeurs fausses acceptées. Sans corpus : NON TESTÉ, sans bloquer la suite.
