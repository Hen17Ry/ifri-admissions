# Validation du bootstrap — 2026-10-12

## État courant — preuves hôte du 12 octobre 2026

**Validation technique du bootstrap terminée.** Tous les critères de sortie demandés sont PASS. Les contrôles visuels approfondis et la CI GitHub restent NON TESTÉS et ne sont pas présentés comme exécutés.

Ce tableau prévaut sur les tentatives historiques ci-dessous.

| Contrôle | Résultat | Preuve |
|---|---|---|
| Dépendances et lock npm | PASS | Installation hôte ; package-lock.json généré par npm |
| Build, typecheck, ESLint, Vitest, Prettier | PASS | Exécutés avec les dépendances installées ; 2 tests Vitest |
| Environnement Python et uv.lock | PASS | uv sync hôte ; lock généré par uv |
| pytest, Ruff check et format | PASS | Environnement projet ; 10 tests |
| Go test, vet, gofmt | PASS | Exécution réelle ; gofmt sans sortie |
| Compose config et construction | PASS | Exécution hôte et images construites |
| PostgreSQL, Redis, MinIO, API, worker | PASS | Dernier up --force-recreate --wait : cinq healthy |
| Initialiseur MinIO | PASS | Terminé ; smoke check vérifie exit 0 |
| Redis authentifié | PASS | Smoke check : PONG |
| Connexion PostgreSQL | PASS | Base et utilisateur configurés vérifiés |
| /health et /ready | PASS | HTTP 200, status=ok ; toutes dépendances ok |
| CORS | PASS | Origine configurée autorisée, autre origine refusée |
| Bucket privé | PASS | Initialisation privée et accès anonyme HTTP 403 |
| Persistance du volume après recréation PostgreSQL | PASS | Marqueur bootstrap-validation retrouvé puis supprimé ; conteneur healthy |
| /ready en panne et reprise ; /health indépendant | PASS | PostgreSQL arrêté : /health 200, /ready 503 avec postgres unavailable ; après reprise smoke check réussi |
| Arrêt propre et logs réels du worker | PASS | Exited (0), log JSON Worker stopped ; redémarrage healthy |
| Démarrage Vite | PASS | Vite 8.3.4 ready, écoute 127.0.0.1:5173 sur l’hôte |
| Vue → API réelle | PASS | Observation navigateur confirmée par l’utilisateur : API et dépendances disponibles ; PostgreSQL, Redis et MinIO Disponible |
| Rendu responsive/clavier et CI GitHub | NON TESTÉ | Aucun navigateur ni déclenchement CI dans cette session |
| git status | FAIL | Environnement Codex : « not a git repository » |

Docker reste inaccessible à Codex : permission denied sur /var/run/docker.sock. Les résultats Docker PASS ci-dessus proviennent des sorties hôte fournies par l’utilisateur, pas d’une exécution simulée dans Codex. Aucun passage à la phase métier.


## Historique des validations partielles (états antérieurs, résolus)

À l’issue de la première tentative, la phase 1 était créée, mais **pas entièrement validée**. Aucun résultat ci-dessous ne vaut validation OCR/métier ou déploiement production. Les versions directes sont exactes ; les locks complets npm et uv restent à générer, faute d’accès aux registres depuis le terminal.

## Environnement effectivement disponible

Node 24.18.1, npm 11.16.0, Python 3.14.4, uv 0.12.3, pytest système 9.0.2 ; Go 1.27.1 disponible directement via `/snap/go/current/bin/go` (wrapper `/snap/bin/go` échoue dans cet environnement confiné). Docker CLI 29.6.1 et Compose 5.3.1 installés, daemon non accessible. Ce ne sont pas toutes les versions Docker/CI cibles, documentées dans DECISIONS.

## Contrôles exécutés

| Contrôle | Résultat réel |
|---|---|
| Installation npm | Tentée ; erreur DNS `EAI_AGAIN registry.npmjs.org` ; dépendances frontend non installées |
| `npm run build` | Tenté ; `vite` introuvable, donc build non effectué |
| `npm run typecheck` | Tenté ; `vue-tsc` introuvable, donc typecheck non effectué |
| `npm run lint` | Tenté ; seul ESLint système 6.4.0 trouvé, incompatible avec flat config ; lint projet non effectué |
| `npm run test` | Tenté ; `vitest` introuvable, aucun test Vue exécuté |
| `npm run format:check` | Première tentative bloquée par Prettier absent ; ensuite Prettier 3.9.10 exact récupéré du cache npm, formatage et `--check apps/web` exécutés avec succès |
| Installation Go / `go mod tidy` | Réussie depuis modules en cache recopiés dans /tmp ; go.mod/go.sum générés |
| `go test ./...` depuis services/api | Réussi ; configuration, santé HTTP et CORS via httptest, sans sockets réseau |
| `go vet ./...` depuis services/api | Réussi |
| `gofmt -l` | Vide après formatage |
| Build API | Réussi avec `go build -buildvcs=false -o /tmp/ifri-api ./cmd/api` |
| Démarrage API natif | Tenté ; écoute HTTP refusée. Contrôle de socket Python séparé : `PermissionError [Errno 1] Operation not permitted` même sur port attribué dynamiquement |
| `uv sync` | Tenté ; DNS PyPI indisponible ; environnement complet déclaré non installé et uv.lock non généré |
| Installation worker de secours | Réussie : .venv Python 3.14.4, redis 7.4.0 et setuptools 84.0.0 du cache pip, package ifri-ocr 0.1.0 installé en editable avec uv |
| Tests Python de secours | Réussis : 10 tests avec pytest système 9.0.2 et sources/client Redis du projet. **Ne valide pas encore pytest 9.1.1 déclaré dans le dev group** |
| Ruff check / format check | Tentés ; exécutable Ruff indisponible et téléchargement bloqué ; aucun résultat Ruff réussi revendiqué |
| Compile/syntaxe Python | Réussie avec compileall ; ne remplace pas Ruff |
| `docker compose --env-file .env.example config --quiet` | Réussi |
| `docker compose --env-file .env.example up -d --build` | Tenté ; accès `/var/run/docker.sock` refusé avant build/démarrage |
| Healthchecks Docker, politique MinIO effective, volumes | Non exécutés : aucun conteneur ne peut être démarré ici |
| CI préparée | Workflow écrit, jamais déclenché/publié dans cette session |
| Rendu UI / quatre largeurs / clavier | Non vérifiés : dépendances/build frontend indisponibles |
| Git | `git status --short` tenté ; répertoire fourni non reconnu comme dépôt Git exploitable ; aucune initialisation, aucun commit/push |

Les erreurs de lancement de commandes Go depuis la racine (absence de go.mod racine) ont été corrigées en exécutant les contrôles dans services/api. Le premier build natif était bloqué par la détection du marqueur Git inexploitable : `-buildvcs=false` est maintenant documenté et utilisé par le lanceur. Le cache utilisateur n’a pas été modifié ; seuls workspace et /tmp ont reçu les installations temporaires.

## Finalisation requise dans un environnement réseau/Docker autorisé

1. Suivre README, installer toutes les dépendances exactes avec npm et uv ; conserver package-lock.json et uv.lock générés, puis passer les installations CI à npm ci / uv sync --locked.
2. Exécuter build, typecheck, lint et Vitest ; corriger toute incompatibilité réellement détectée. Exécuter Ruff et pytest dans l’environnement projet déclaré.
3. Construire et démarrer Compose, exécuter `python infra/scripts/smoke.py`, vérifier les healthchecks et la politique privée MinIO. Tester la conservation des volumes après down/up sans suppression des données.
4. Vérifier UI aux largeurs 375/768/1024/1440, fontes auto-hébergées, clavier et focus ; relever les digests des images effectivement tirées/construites.

Cette finalisation reste dans la phase 1. Aucune étape métier suivante n’est autorisée ni commencée par ce rapport.

## Nouvelle preuve hôte — installation frontend

- **PASS** : versions hôte Node 24.18.1 et npm 11.16.0 rapportées par l’utilisateur.
- **FAIL** : `npm install` retourne `ETARGET` pour `lucide-vue-next@1.55.0`, version inexistante pour ce paquet. Le manifest est corrigé vers la version publiée 1.0.0 ; nouvelle installation non encore exécutée sur le poste hôte.
- **NON TESTÉ** : build, typecheck, ESLint projet, Vitest et format check dans l’environnement frontend complet. Les commandes lancées après l’installation échouée trouvent des outils absents ou ESLint système 6.4.0 ; elles ne valident aucun contrôle frontend.
- **NON TESTÉ** : génération complète de package-lock.json après correction. Aucun lockfile fabriqué manuellement.

## Validation frontend après installation hôte réussie

Installation hôte rapportée : 246 paquets ajoutés, audit de 247 paquets sans vulnérabilité rapportée. `apps/web/package-lock.json` généré par npm, présent dans le répertoire partagé ; toutes les versions directes concordent avec le manifest.

- **PASS** : installation frontend et lockfile npm généré.
- **PASS** : `npm run build` exécuté dans Codex avec Vite 8.3.4, bundle et fontes locales produits.
- **PASS** : `npm run typecheck` exécuté, aucune erreur.
- **PASS** : `npm run test` exécuté avec Vitest 5.0.3 : 1 fichier, 2 tests réussis.
- Correction : `loadEnv` importé depuis `vite` ; `defineConfig` conservé depuis `vitest/config`. Le premier build/typecheck/test a révélé cet import incorrect ; les trois commandes ont été relancées avec succès après correction.
- **NON TESTÉ** : frontend → API réelle et rendu navigateur, toujours dépendants d’un environnement autorisant les sockets. Les deux tests Vue simulent les réponses API, sans prouver la connectivité réelle.
- **NON TESTÉ** : environnement Python complet verrouillé et services Docker, en attente d’exécution hôte.

- **PASS** : `npm run lint` relancé après correction, aucune erreur ESLint.
- **PASS** : `npm run format:check` relancé après correction, tous les fichiers conformes à Prettier.
- La CI frontend utilise désormais `npm ci`, puisque le lock officiel existe.

## Validation Python après installation hôte réussie

`uv sync` exécuté par l’utilisateur avec uv 0.12.3. `services/ocr/uv.lock` généré par uv, présent et versions redis/pytest/Ruff conformes au pyproject. La `.venv` utilise Python 3.14.4 ; aucune version de dépendance modifiée.

- **PASS** : installation de l’environnement Python déclaré et génération de uv.lock.
- **PASS** : `uv run --locked --no-sync pytest` : pytest 9.1.1, 10 tests réussis ; commande relancée après corrections.
- **PASS** : `uv run --locked --no-sync ruff check .` : Ruff 0.17.0, aucune erreur après correction UP037 (annotation Config sans guillemets).
- **PASS** : `uv run --locked --no-sync ruff format --check .` : 5 fichiers conformes après formatage de worker.py pour Python 3.14. Aucune règle désactivée ni aucun test supprimé.
- Le cache uv utilisateur est en lecture seule dans Codex : commandes exécutées avec `UV_CACHE_DIR=/tmp/ifri-uv-cache`, sans modifier la configuration du projet.
- **PASS** : configuration Compose revalidée avec .env.example.
- **FAIL** : nouvel essai `docker info` dans Codex, accès au socket Docker refusé.
- **NON TESTÉ** : démarrage Docker, healthchecks réels, bucket privé effectif, persistance PostgreSQL, heartbeat/arrêt réel du worker, CORS réseau et frontend → API.

Les contrôles frontend et Python précédemment bloqués sont maintenant validés ; le bootstrap complet reste ouvert jusqu’aux preuves Docker et de connectivité réelle. Un .env local a été préparé sans écraser un fichier existant, avec des mots de passe aléatoires si sa création était nécessaire ; aucun secret dans les documents ou logs.

## Construction et démarrage Docker sur l’hôte

Preuves transmises par l’utilisateur : Docker Engine 29.6.1, configuration Compose sans erreur, construction des images terminée, `up --wait` réussi et `ps --all` fourni.

| Contrôle sur la configuration initiale | Résultat |
|---|---|
| Docker accessible sur l’hôte, build des quatre images applicatives/stockage | PASS |
| PostgreSQL 18.6, Redis 8.10.2, MinIO, API et worker : healthchecks Docker | PASS — cinq services healthy |
| minio-init | PASS — Exited (0) |
| Publication des ports sur l’hôte | NON TESTÉ par requête ; aucune publication affichée dans ps, correction réseau préparée |
| /health et /ready depuis l’hôte, Redis PONG explicite, identité PostgreSQL et persistance | NON TESTÉ |
| Bucket privé : refus HTTP anonyme et inspection de policy | NON TESTÉ |
| Frontend → API réelle, panne de dépendance, arrêt propre du worker | NON TESTÉ |

Correction préparée : bridge `local` complémentaire pour les quatre services publiés, maintien du réseau interne pour le worker/initialiseur et des bindings loopback. `docker compose config` revalidé dans Codex ; recréation des conteneurs et smoke check à exécuter sur l’hôte avant toute revendication de connectivité.

Digests effectivement montrés dans le build hôte : Alpine 3.24.2 `sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6` ; builder Go 1.27.2-alpine `sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d` ; Python 3.14.8-slim-bookworm `sha256:48b13b003dda20b16f9442b8475aa05fe21bf6579a8c881db92ffb4d8fd20f83`. Ils sont relevés comme preuve de construction, pas présentés comme déjà verrouillés dans les Dockerfiles.

## Conflit du port Redis lors de la publication hôte

- **FAIL** : tentative hôte après correction du réseau : `Bind ... :6379 failed: port is already allocated`. La stack n’est pas prête : PostgreSQL/Redis/MinIO sont Created, API Created ; ne pas réutiliser les états healthy de la configuration précédente comme preuve de l’état courant.
- **FAIL** : smoke check fourni par l’utilisateur : `postgres: Docker health is not healthy` (conséquence du démarrage interrompu).
- Correction locale : `.env` fixe désormais `REDIS_PORT=6380` pour ce projet seulement. Redis conserve son port interne 6379 ; API et worker utilisent toujours `redis:6379`. Aucun service tiers arrêté, aucun volume supprimé, aucune dépendance modifiée. `.env.example` conserve la valeur standard configurable 6379.
- **PASS** : configuration Compose effective vérifiée dans Codex : publication `127.0.0.1:6380 → 6379` et connexions internes inchangées.
- **NON TESTÉ** : nouvelle disponibilité du port 6380 et redémarrage réel de la stack sur l’hôte. Exécuter up --wait puis smoke check avant de conclure.

## Deuxième conflit Redis et correction définitive de la configuration par défaut

- **FAIL** : nouvelle tentative hôte : port 6380 également occupé ; la stack complète reste non validée dans sa configuration publiée.
- Correction : Redis sans port hôte ni bridge local par défaut. Les clients Docker restent sur redis:6379 ; même volume et mêmes credentials. compose.native.yaml prépare un accès hôte optionnel, sans l’activer pour la validation Docker.
- **PASS** : configuration Compose normale et override natif validés avec config --quiet. Assertions de configuration : Redis non publié/interne par défaut ; API/worker sur port interne 6379 ; autres publications loopback conservées ; override Redis limité au loopback.
- **PASS** : Ruff check et format check du smoke script renforcé, avec la configuration Python du projet.
- Smoke script renforcé pour vérifier réellement PONG authentifié, base/utilisateur PostgreSQL, santé/readiness HTTP et origines CORS. Ces nouveaux contrôles réseau restent **NON TESTÉS** jusqu’à l’exécution hôte ; ils ne sont pas assimilés aux tests unitaires précédemment réussis.
- **NON TESTÉ** : nouveau démarrage Docker après suppression de la publication Redis, accès HTTP hôte et frontend → API.

## Conflit PostgreSQL observé sur l’hôte

- **PASS** : Redis healthy dans la nouvelle exécution, après retrait de sa publication hôte.
- **FAIL** : démarrage global interrompu par le port hôte 127.0.0.1:5432 déjà occupé ; l’API n’est pas encore prête.
- Correction : PostgreSQL désormais interne par défaut, comme Redis ; publication optionnelle dans compose.native.yaml. API conserve postgres:5432 et les volumes/credentials restent inchangés.
- **PASS** : configs normale et native vérifiées : aucun port hôte PostgreSQL/Redis par défaut, override loopback et ports internes 5432/6379 inchangés.
- **NON TESTÉ** : application de cette correction sur l’hôte et smoke check complet. Les publications encore nécessaires sont API et MinIO ; vérifier les ports occupés avant de poursuivre le démarrage.

## Publications hôte dédiées à IFRI

La sortie docker ps fournie par l’utilisateur confirme les conflits 8080 et 9000/9001 avec d’autres projets. Configuration IFRI mise à jour vers 18080 pour API et 19000/19001 pour MinIO, sans modifier les ports internes ni les credentials. PostgreSQL et Redis restent privés par défaut.
- **PASS** : cohérence de la configuration Compose, environnement d’exemple/local, proxy Vite et URLs documentées vérifiée dans Codex.
- **NON TESTÉ** : disponibilité effective de ces ports sur l’hôte, nouvelle stack complète et smoke check réseau. Aucun autre conteneur n’a été arrêté.

- **PASS** : après alignement du fallback Vite à 18080, build, typecheck, ESLint, Vitest (2 tests) et format check relancés avec succès.
