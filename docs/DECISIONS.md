# Décisions — ADR simplifiées

Date de préparation : 2026-10-12. « Retenue » = direction documentaire ; « Proposée » = choix à confirmer par mise en œuvre ou arbitrage, jamais infrastructure déjà testée.

| ADR | Statut | Contexte et décision | Conséquences |
|---|---|---|---|
| 001 | Retenue | Architecture simple : Go monolithe modulaire, Vue SPA, Python worker | Trois responsabilités nettes ; pas de microservices métier |
| 002 | Retenue | Matières variables : règles versionnées par diplôme/série + filière, rattachées aux campagnes | Grille officielle à compléter ; aucune règle générique de repli |
| 003 | Retenue | Coefficients et notes issus exclusivement du PDF | Aucun coefficient configuré ; provenance par champ, données absentes bloquantes |
| 004 | Retenue | Confirmation candidat en lecture seule | API sans mutation directe des valeurs ; refus = nouveau PDF ; revue manuelle à cadrer |
| 005 | Retenue | Go possède logique métier et écritures PostgreSQL | Worker spécialisé, callback validé, calcul serveur |
| 006 | Proposée | Redis Streams + outbox PostgreSQL + ACK après persistence Go | Au moins une fois, idempotence, récupération/rapprochement ; pas de promesse exactly-once |
| 007 | Retenue | Documents immuables privés MinIO/S3, autorisation Go et accès temporaire | Version/hash, quarantaine, pas d’URL publique permanente ; rapprochement SQL/S3 |
| 008 | Retenue | États dépôt/OCR/admin/sélection séparés | Guards et historique ; VERIFIED distinct de OCR_VALIDATED et SELECTED |
| 009 | Retenue | Calcul décimal, classement sur valeur non arrondie, snapshots par campagne/filière | Comparaison exacte des ratios, reproduction ; départage officiel requis |
| 010 | Proposée | Sessions opaques cookies, Argon2id, MFA admin et CSRF | Révocation centrale ; calibrage et choix auth candidat avant phase identité |
| 011 | Retenue | Vue 3/TS/Vite/Router/Pinia/Tailwind, shadcn-vue/Reka au besoin, Lucide Vue | Aucune dépendance React ; versions stables verrouillées au bootstrap |
| 012 | Retenue | Master UI UX Pro Max adapté, institutionnel clair, Lexend/Source Sans 3, marine | Suggestions commerciales rejetées ; adaptations tracées dans UI_UX et MASTER |
| 013 | Proposée | Un dossier, une filière par candidat/campagne ; notes sur 20 | À confirmer avant migrations/validation notes ; aucun automatisme interéchelles |
| 014 | Différée | PaddleOCR vs Tesseract et usage OpenCV | Décision par corpus ; PyMuPDF envisagé, licences et exactitude à examiner |
| 015 | Différée | Correction manuelle de données, délais hors campagne, seuils/essais, conservation, départage | Pas de valeurs inventées ; blocages localisés aux fonctions concernées |

## Traçabilité des adaptations UI
Première recherche UI UX Pro Max : « institutional university admission administrative document workflow » ; résultat Minimalism & Swiss Style, palette marine, recommandations de gateway commerciale et polices sérif. Nouvelle recherche ciblée « university admissions dashboard » : pattern portfolio également inadapté. Ces deux patterns n’ont pas de correspondance vérifiée avec le parcours produit : ils ne sont pas conservés comme règles.
Recherche complémentaire typographie : résultat Corporate Trust (Lexend / Source Sans 3), retenu pour formulaires et tables. Recherches UX erreurs et stack Vue réalisées. Le master généré via `--persist` a été édité pour ne conserver que la synthèse applicable et les décisions explicitement motivées. Ni React ni GSAP introduits.

## Procédure ADR future
Ajouter une entrée datée avec problème, options si pertinentes, décision, statut, impact et validation. Toute modification des invariants requiert un arbitrage explicite de l’utilisateur ; toute règle officielle doit référencer sa source/version. Une hypothèse ne devient pas règle en étant répétée dans le code.

## Phase 1 — décisions du 2026-10-12

| ADR | Statut | Décision | Validation / conséquence |
|---|---|---|---|
| 016 | Retenue | Organisation apps/web + services/api + services/ocr, sans orchestrateur JS | Remplace uniquement les anciens noms proposés dans l’arborescence |
| 017 | Retenue | net/http + pgxpool + go-redis, configuration explicite, slog JSON | Deux endpoints techniques ; tests et vet Go exécutés ; zéro framework/ORM/DI |
| 018 | Retenue | Liveness indépendante ; readiness PostgreSQL/Redis/MinIO avec délai borné | Tests httptest de statuts et CORS ; démarrage réel interdit par les sockets de l’environnement |
| 019 | Retenue | Python 3.14, setuptools/pyproject, Redis uniquement ; heartbeat interne | Pas d’API HTTP, pas de moteur OCR, pas de queue ; tests bootstrap exécutés avec pytest système de secours |
| 020 | Retenue | Compose local, réseau interne, ports loopback et volumes nommés | Configuration Compose validée ; daemon Docker inaccessible ici |
| 021 | Retenue pour local | MinIO et mc compilés depuis releases source fixées ; bucket privé via initialiseur ponctuel | Dépôts Community archivés ; ne pas confondre avec choix production maintenu ; builds et policy effective à vérifier sous Docker |
| 022 | Retenue | Goose 3.29.0 choisi pour migrations SQL futures | Pas installé inutilement dans cette phase, pas de table métier ni migration vide |
| 023 | Retenue | Vite proxy technique, CORS origine unique ; aucun VITE_* secret | API rejette wildcard et origines différentes ; TLS exigé par config production |
| 024 | Retenue | Page technique conforme au master, fonts Fontsource auto-hébergées, aucun React | Build, rendu, tests Vue et accessibilité visuelle non vérifiés faute d’installation npm |
| 025 | Partiellement exécutée | Dépendances directes exactes ; checksums Go ; locks transitifs npm/uv à générer après rétablissement réseau | Ne pas présenter package.json/pyproject comme locks complets ; aucun lock inventé |
| 026 | Préparée | CI minimale web/Go/Python/Compose, déclenchement PR ou manuel uniquement | Aucun push/publication ; workflow non exécuté dans cette session |

### Versions retenues et compatibilité
Les versions stables ont été recherchées avant les installations via les sources officielles et les métadonnées de registres disponibles. « Stable » ne signifie pas « toutes testées ensemble » ; seuls les contrôles du rapport de validation ont été exécutés.

| Technologie / dépendance | Version choisie | Raison / verrouillage |
|---|---|---|
| Node / npm | 24.18.1 LTS / 11.16.0 | Runtime présent ; .node-version 24.18.1, engines Node 24 >=24.15, packageManager npm exact, CI explicite |
| Vue | 3.5.43 | Vue 3 stable, pas de Vue 4 prerelease ; package.json exact |
| Vite / plugin-vue | 8.3.4 / 6.0.8 | Plugin déclare Vite 8 compatible, Node >=22.12 ; exacts |
| Vue Router / Pinia | 4.6.4 / 3.0.4 | APIs classiques suffisantes ; versions stables compatibles Vue 3 ; évolutions majeures 5/4 identifiées mais non nécessaires au bootstrap |
| TypeScript / vue-tsc | 6.0.3 / 3.3.7 | TypeScript 7 stable existe, mais typescript-eslint retenu accepte <6.1 ; ligne 6 conservée pour compatibilité tooling Vue |
| Tailwind / @tailwindcss/vite | 4.3.3 / 4.3.3 | Plugin Vite officiel, pas de configuration Tailwind v3 recopiée |
| Lucide Vue | 1.0.0 (`lucide-vue-next`) | Version publiée du paquet prévu ; correction du verrouillage erroné 1.55.0, voir note ci-dessous |
| Fontsource Lexend / Source Sans 3 | 5.3.0 / 5.3.1 | Fonts locales, licence OFL-1.1 ; 600/700 titres, 400/600 texte, sous-ensemble latin |
| Vitest / Vue Test Utils / jsdom | 5.0.3 / 2.5.1 / 30.1.2 | Vitest accepte Vite 8 ; jsdom impose Node >=24.15 sur la ligne 24 |
| ESLint / @eslint/js / plugin-vue | 10.12.0 / 10.0.1 / 10.11.0 | Flat config ; plugin Vue déclare ESLint 10 compatible |
| typescript-eslint / eslint-config-prettier / Prettier | 8.71.1 / 10.1.8 / 3.9.10 | TypeScript <6.1 ; format séparé du lint ; versions exactes |
| @types/node | 24.19.2 | Types ligne Node 24 |
| Go | 1.27.2 Docker/CI ; go.mod >=1.27.0 | Tests locaux sur 1.27.1 existant ; go.mod et go.sum |
| pgx / go-redis | 5.11.0 / 9.23.0 | Clients sans ORM ; stdlib net/http/slog ; versions exactes |
| Python | 3.14.8 Docker/CI ; >=3.14,<3.15 projet | Python 3.15.0 vient de paraître ; 3.14 explicitement supporté par redis-py et déjà disponible ici (3.14.4) |
| Redis Python / setuptools | 7.4.0 / 84.0.0 | Une dépendance runtime ; build backend exact |
| pytest / Ruff / uv | 9.1.1 / 0.17.0 / 0.13.0 recommandé | Dev group exact ; uv n’est pas une dépendance runtime ; local uv 0.12.3 et pytest système 9.0.2 utilisés pour secours, pas assimilés à validation des versions cibles |
| PostgreSQL | 18.6-bookworm | Branche stable 18 mature ; volume /var/lib/postgresql adapté aux images >=18 ; aucun schéma métier |
| Redis serveur | 8.10.2-alpine | Release stable avec correctifs de sécurité ; PING/authentication seulement pour l’instant |
| MinIO | RELEASE.2025-10-15T17-29-55Z | Dernière release Community officielle identifiée, compilée depuis sources ; local seulement |
| mc | RELEASE.2025-08-13T08-35-41Z | Release officielle source fixée ; initialiseur privé idempotent |
| Alpine runtime | 3.24.2 | Runtime API/MinIO/mc fixé à une patch stable |
| Actions CI | checkout 7.0.1, setup-node 7.1.0, setup-go/setup-python 7.0.0 | Releases officielles exactes ; aucun workflow exécuté/push dans cette session |

Les modules Go transitoires x/sync 0.24.0 et x/text 0.43.0 sont explicitement résolus dans go.mod/go.sum ; installation via cache local recopié dans /tmp pour respecter le filesystem en lecture seule. Les autres versions transitives sont dans go.mod/go.sum. Aucun paramètre GOPROXY=off/GOSUMDB=off propre à cette validation hors réseau n’est enregistré dans la configuration du projet.
Images PostgreSQL/Redis/Python/Go fixées sur tags de version, pas latest ; les digests ne sont pas inventés et restent à relever lors de la première construction Docker autorisée. Les tags OS suffixés alpine/bookworm peuvent recevoir leurs mises à jour de base ; ce n’est pas un verrouillage intégral par digest.

### Sources de versions et statut MinIO
- [Releases Go](https://go.dev/dl/?mode=json), [pgx 5.11.0](https://github.com/jackc/pgx/releases/tag/v5.11.0), [go-redis](https://github.com/redis/go-redis/releases).
- [Registre Vue](https://registry.npmjs.org/vue/latest), [Vite](https://github.com/vitejs/vite/releases), [Vitest](https://github.com/vitest-dev/vitest/releases), [Vue Test Utils](https://github.com/vuejs/test-utils/releases/latest), [Vue Router 4.6.4](https://github.com/vuejs/router/releases/tag/v4.6.4), [Pinia 3.0.4](https://github.com/vuejs/pinia/releases/tag/v3.0.4), [Lucide](https://github.com/lucide-icons/lucide/releases).
- [Lexend](https://api.fontsource.org/v1/fonts/lexend), [Source Sans 3 npm](https://www.npmjs.com/package/%40fontsource/source-sans-3?activeTab=code), [licence Fontsource](https://fontsource.org/fonts/source-sans-3/about).
- [Python 3.14.8](https://discuss.python.org/t/python-3-10-22-3-11-17-3-12-15-3-13-16-and-3-14-8-are-now-available/109296), [Python 3.15.0](https://www.python.org/downloads/release/python-3150/), [redis-py](https://pypi.org/project/redis/), [pytest](https://pypi.org/project/pytest/), [Ruff](https://pypi.org/project/ruff/), [uv](https://github.com/astral-sh/uv/releases/latest).
- [PostgreSQL releases](https://www.postgresql.org/docs/release/), [Redis releases](https://github.com/redis/redis/releases), [Alpine](https://alpinelinux.org/releases/), [Goose](https://github.com/pressly/goose/releases).
- [MinIO officiel](https://github.com/minio/minio) : dépôt archivé et distribution source-only ; [release serveur](https://github.com/minio/minio/releases/tag/RELEASE.2025-10-15T17-29-55Z), [release mc](https://github.com/minio/mc/releases/tag/RELEASE.2025-08-13T08-35-41Z).
Les versions npm de tooling non détaillées dans ces liens ont été vérifiées dans les métadonnées de registre mises en cache ; leur compatibilité à l’installation effective reste à contrôler lorsque npm pourra résoudre les paquets.

### Correction du verrouillage Lucide après exécution hôte
L’installation hôte a échoué avec `ETARGET` sur `lucide-vue-next@1.55.0`. Le numéro de release global Lucide avait été assimilé à tort à une version publiée de ce paquet. Correction minimale : conserver le paquet Vue et les imports existants, verrouiller `lucide-vue-next` à **1.0.0**, version publiée vérifiée sur [npm](https://www.npmjs.com/package/lucide-vue-next). Le mainteneur signale sa dépréciation et son renommage officiel en `@lucide/vue` dans [le guide Lucide v1](https://lucide.dev/guide/version-1). Cette correction ne prétend pas valider l’installation ou le build : la nouvelle exécution hôte reste nécessaire. Aucun paquet React ajouté, aucune version d’un autre paquet modifiée.

### ADR 027 — publication des ports de développement Docker
Statut : retenue, application sur l’hôte restant à vérifier. La stack a démarré avec Docker 29.6.1 et cinq services healthy, mais `docker compose ps` n’affiche aucune publication loopback, seulement les ports conteneurs. Les services étaient exclusivement attachés au réseau `internal: true` ; ce comportement est rapporté dans [Moby](https://github.com/moby/moby/discussions/53256).
Correction limitée au réseau local : ajouter un bridge non interne `local` aux quatre services qui publient des ports (api, postgres, redis, minio), en conservant leurs bindings explicites `127.0.0.1`. Le réseau interne reste présent ; worker et initialiseur ne rejoignent que ce réseau. Aucun volume supprimé, aucune image ou solution de stockage remplacée. [Docker documente les bindings localhost](https://docs.docker.com/engine/network/port-publishing/).

### ADR 028 — Redis privé par défaut, accès natif optionnel
Statut : retenue. Les ports hôte 6379 puis 6380 sont tous deux occupés sur le poste de validation. Redis n’a pas besoin d’être publié pour le bootstrap Docker : API et worker l’utilisent via `redis:6379`, et redis-cli peut être exécuté dans le conteneur.
Supprimer la publication Redis et sa connexion au bridge local dans compose.yaml ; conserver uniquement le réseau interne. Ajouter compose.native.yaml comme override explicite pour les développeurs lançant API/worker hors Docker, avec un port loopback libre choisi dans REDIS_PORT. Les credentials, image, volume et clients internes restent inchangés ; aucun service tiers arrêté. ADR 027 reste applicable à API/PostgreSQL/MinIO, et à Redis uniquement avec l’override natif. Ce choix remplace la tentative de contournement par changement automatique vers 6380.

### ADR 029 — PostgreSQL privé par défaut
Statut : retenue. L’exécution hôte a confirmé un conflit sur 127.0.0.1:5432 après résolution du conflit Redis. Étendre la décision ADR 028 à PostgreSQL : aucun port hôte ni bridge local dans compose.yaml ; publication loopback uniquement via compose.native.yaml pour exécutions natives. API toujours sur postgres:5432 ; connexion de validation via psql dans le conteneur ; même base, mêmes credentials et volume, aucun schéma métier ajouté. Seuls API et MinIO nécessitent des publications dans le bootstrap habituel. Aucun processus/service tiers arrêté.

### ADR 030 — ports hôte dédiés à IFRI
Statut : retenue, disponibilité finale à vérifier sur l’hôte. La liste des conteneurs exécutée par l’utilisateur montre que les ports hôte 8080, 9000 et 9001 sont déjà publiés par d’autres projets. Fixer API_PORT=18080, MINIO_API_PORT=19000 et MINIO_CONSOLE_PORT=19001 dans .env.example et la configuration locale, avec MINIO_ENDPOINT=http://127.0.0.1:19000. Les ports internes restent API 8080, MinIO 9000/9001, PostgreSQL 5432, Redis 6379. Vite utilise API_PORT du .env racine, fallback aligné à 18080 ; smoke check utilise la même configuration. Aucun service tiers arrêté, aucun volume supprimé. Ces trois nouveaux ports ne figurent pas dans les publications Docker fournies ; leur disponibilité vis-à-vis de processus non Docker n’est pas encore prouvée.

## Phase 2 — décisions du 2026-10-12

| ADR | Statut | Décision | Conséquences |
|---|---|---|---|
| 031 | Retenue | UUID v4 PostgreSQL via gen_random_uuid(), conformément au modèle initial | Aucun générateur/extension supplémentaire ; codes métier uniques séparés des UUID. [Documentation PostgreSQL](https://www.postgresql.org/docs/current/functions-uuid.html) |
| 032 | Retenue | OriginQualification canonique BAC_C/DT_IMI…, Subject, CampaignProgram pour quotas | Pas de texte libre d’origine ni de note/coefficient dans les référentiels |
| 033 | Retenue | Grille versionnée au niveau CampaignProgram, éligibilité unique par version/qualification | Précise ADR 002 : aucune répétition campaign_id/program_id en aval, une publiée par offre, publiée immuable ; remplacement intra-offre différé |
| 034 | Retenue | Goose 3.29.0 intégré par Provider et pgx stdlib, commande db explicite, verrou PostgreSQL | Schéma embarqué Up/Down ; aucune migration automatique HTTP. Version déjà décidée, [release officielle](https://github.com/pressly/goose/releases/tag/v3.29.0) vérifiée |
| 035 | Retenue | Schéma, seed canonique et fixture locale séparés ; transaction et verrou seed | Campagne DEV_LOCAL non officielle ; quotas NULL ; seed refusé en production, aucune écriture d’une grille publiée |
| 036 | Retenue | GET /api/v1, campagne ouverte unique implicite ou code explicite, [starts_at,ends_at[ | Ambiguïté 409 ; codes inconnus 404 ; règle absente 200 eligible=false RULE_NOT_DEFINED, refus explicite INELIGIBLE ; matrice utilisateur persistée SQL |
| 037 | Retenue | Tests PostgreSQL via profil Compose test et schéma temporaire propre à chaque exécution | Pas de port PostgreSQL publié, aucune base locale effacée ; tests opt-in -tags=integration ; HTTP réel via script séparé |

Source de la matrice : instruction utilisateur phase 2 du 12 octobre 2026 ; voir ADMISSION_RULES. Aucun quota ni communiqué officiel inventé. Les validations réelles et celles bloquées sont suivies dans PHASE2_VALIDATION.

## Phase 3 — décisions du 2026-10-12

| ADR | Statut | Décision | Conséquences |
|---|---|---|---|
| 038 | Retenue | Brouillon anonyme avec cookie opaque 256 bits, hash SHA-256, expiration absolue configurable | HttpOnly/SameSite=Lax, Secure production, Path limité ; aucun secret localStorage, révocation DB et aucun accès UUID public |
| 039 | Retenue | Origin obligatoire exactement configuré + JSON, CORS credentials explicites | Pas de token CSRF supplémentaire pour ce périmètre ; refus des mutations sans Origin, SameSite complémentaire ; sources OWASP dans CANDIDATE_FLOW |
| 040 | Retenue | Candidate distinct d’Application, DRAFT uniquement, profil incomplet autorisé vide | Pas d’identité déduite de l’email, pas de numéro final, aucun statut OCR/document mélangé ; FK composite campagne/offre |
| 041 | Retenue | Autosave 800 ms sérialisé et validation d’étape attendue, version optimiste | Backend source de vérité ; conflit 409, perte de réseau/expiration affichées, avertissement pour modifications non acquittées |
| 042 | Retenue | Qualification modifiée : choix filière revérifié et supprimé si incompatible dans transaction | Cas DT_DWM + GL → DT_EAP explicitement testé ; règles exclusivement PostgreSQL |
| 043 | Retenue | UI UX Pro Max actif, master inchangé, override candidate-application ; primitives natives Vue | Pas de dépendance React ni combobox/validation framework ajouté pour ce formulaire limité ; règles validation client centralisées et serveur obligatoire |
| 044 | Retenue | Téléphone normalisé international, validation syntaxique sans lib de numéros | Formats étrangers permis ; pas de preuve de validité/possession du numéro. Année jusqu’à année UTC courante |
| 045 | Retenue pour local | Création limitée par IP RemoteAddr, 5/min, mémoire bornée | Protection minimale sans confiance proxy ; rate limiting distribué à traiter lors du déploiement |

## Phase 4 — décisions du 2026-10-12

| ADR | Statut | Décision | Conséquences |
|---|---|---|---|
| 046 | Retenue | Quatre types applicatifs stables, libellés envoyés par API | Pas de table de référentiel sans besoin ; aucune duplication frontend des libellés |
| 047 | Retenue | ApplicationDocument + DocumentVersion, pointeur courant et FK/triggers différés | Historique conservé, version future OCR précise, pas de modification des migrations 00001–00003 |
| 048 | Retenue | Upload API multipart, temporaire borné, SHA-256 et qpdf système | 10 MiB local configurable, chiffrés/actions actives refusés ; contrôle structurel sans promesse antivirus |
| 049 | Retenue | Port S3 générique, SDK minio-go/v7 7.3.0 dans adaptateur uniquement | Compte API limité aux objets applications/*, endpoint/région configurables ; root réservé à initialisation |
| 050 | Retenue | Objet S3 puis transaction DB ; compensation sur échec certain | Commit indéterminé conserve objet ; réconciliation future documentée |
| 051 | Retenue | Preview proxy API authentifié + nouvel onglet PDF natif | Pas d’URL publique/présignée persistée, Range et en-têtes sûrs, download distinct |
| 052 | Retenue | Origin et X-Application-Version aussi sur multipart | Upload sérialisé côté UI, conflit 409 entre onglets, complétude relue au serveur |
| 053 | Retenue | Playwright 1.64.0 dev-only pour parcours réel et captures | Tests navigateur séparés de Vitest ; exécution hôte requise, aucune donnée personnelle réelle |

Sources : [SDK S3 MinIO](https://github.com/minio/minio-go/releases), [qpdf CLI](https://qpdf.readthedocs.io/en/stable/cli.html). Versions npm/Go verrouillées par les outils officiels. La version système qpdf réellement installée en conteneur sera relevée lors de la validation hôte ; paquet Alpine dans l’image existante, sans moteur OCR.

## Phase 5 — décisions en cours de validation

| ADR | Décision | Conséquence |
|---|---|---|
| 054 | Snapshot contexte et document_version_id exact ; invalidation/enqueue transactionnels par triggers | Anciennes analyses OBSOLETE, versions historiques, aucune réutilisation silencieuse |
| 055 | Redis Streams avec analyse durable servant d’outbox et groupe consommateur | Au moins une fois ; claim, callbacks par tentative, digest et ACK durable ; reprise bornée |
| 056 | PyMuPDF 1.28.2 + RapidFuzz 3.14.6, Python 3.14 conservé ; Tesseract fra+eng | Versions compatibles publiées ; pas de runtime ML lourd ; installation/mesures réelles encore attendues |
| 057 | PDF worker via API interne utilisant le port S3 existant | Aucun credential S3 ou accès PostgreSQL côté Python ; listener 8081 privé, secret obligatoire |
| 058 | Décimales contractuelles sous forme de chaînes, Decimal/big.Rat/NUMERIC | Aucune note ou coefficient en float ; confiances/bbox séparées |
| 059 | Score heuristique minimum et gardes spatiales, seuil local centralisé | Pas de probabilité scientifique ni précision sur corpus réel revendiquée |

[PyMuPDF](https://pypi.org/project/pymupdf/) publie des wheels compatibles et utilise une licence AGPL ou commerciale ; conditions de distribution/déploiement à traiter avant production selon le modèle de licence du projet. [RapidFuzz](https://pypi.org/project/RapidFuzz/) 3.14.6 supporte Python 3.14, licence MIT. Ces décisions locales ne constituent pas une validation de production.

Précisions ADR 055 : ledger ocr_analysis_attempts pour idempotence et audit des reprises ; contractVersion=1 explicite. Le profil ocr-tests est un conteneur de validation opt-in, pas un service produit supplémentaire ; il réutilise l’image worker et écrit uniquement des fixtures synthétiques. Refus d’une version de relevé valable globalement pour cette version, sans contournement par changement de contexte.

Précision construction Phase 5 : après timeout TLS du registre GHCR sur l’hôte, uv 0.12.3 (version déjà utilisée pour le lock local) est installé depuis PyPI dans l’image Python, puis les dépendances restent installées avec `uv sync --locked`. Le build de validation utilise `additional_contexts: service:ocr-worker` et non une image locale potentiellement ancienne. Sources : https://pypi.org/project/uv/0.12.3/ et https://docs.docker.com/reference/compose-file/build/#additional_contexts.

## Phase 5.1 — décisions (clôture du 14 octobre 2026, compléments 078–082 du même jour)

| ADR | Décision | Conséquence |
|---|---|---|
| 060 | Analyse automatique affichée sous la carte relevé, cinq étapes | Déclenchement backend existant conservé, aucune route de lancement manuel ; refresh = lecture seulement |
| 061 | TranscriptLayoutProfile BAC général : premier groupe écrit, oral exclu | Hypothèse de source pour ce format à confirmer avec règles officielles IFRI ; aucun coefficient métier |
| 062 | OpenCV headless 4.13.0.92 + NumPy 2.4.6, Python 3.14 conservé | Lock officiel hôte ; deskew crédible et refus perspective forte, géométrie inverse testée |
| 063 | ~~300 DPI, trois passes globales~~ | Remplacée par 065 pour le profil BAC ; conservée pour le profil générique |
| 064 | Corpus réel privé via bind mount read-only et oracle ignoré | Remplacée dans sa forme par 075 (harnais `<nom>.expected.json`) ; mêmes garanties de confidentialité |
| 065 | Profil BAC lu par cellules dans un paquet `ifri_ocr.bac` (geometry, reading, arithmetic, assignment, pipeline) | `cells.py` supprimé ; chemin générique inchangé ; Tesseract reste le moteur de production |
| 066 | Tableau = en-tête + N ≥ 1 matières + ligne Total (borne 30 rangées = contrat Go) | Fin de la contrainte « 5 à 12 rangées » ; bandes de rangées comptées par caractères traversant chaque ligne de pixels (indépendant de la longueur du libellé) ; l'encre au-dessus du bas du titre de section n'est jamais une rangée |
| 067 | Rangées de tête non-données retirées après lecture (ni libellé reconnaissable, ni Total, ni couple Coeff/Sur) | Un en-tête sur deux lignes ou masqué par un filigrane ne devient pas une matière ; retirer une vraie rangée ne peut que casser les sommes, donc jamais créer une valeur acceptée |
| 068 | Chiffres lus sur toutes les rangées jusqu'au Total ; libellés seulement pour retrouver les matières attendues | Un libellé illisible ne bloque rien si les totaux bouclent et si les matières attendues sont trouvées ; toutes les rangées doivent toutefois donner Points ÷ Coeff exact dans [0, 20] (sinon erreur compensée suspectée) |
| 069 | Attribution matières attendues × rangées par matrice de scores sur vocabulaire documentaire fermé, affectation hongroise | Plancher 0,85, écart ≥ 0,08 avec la 2ᵉ rangée et avec le 2ᵉ mot du vocabulaire, sinon `AMBIGUOUS_ROW` ; libellé introuvable → `NEEDS_REVIEW` ; vocabulaire étendu (Étude de fabrication, sciences physiques, technologie, économie, EPS) ; confusions 0→o, 1→i, 5→s appliquées à tous les mots ; ordre des matières jamais utilisé |
| 070 | Crops numériques : fond aplati (fermeture 17 px puis division), agrandi ×2 avant Otsu ; repli canal rouge pour toute cellule trop dense ; isolement de la ligne de texte, glyphes retenus transmis en niveaux de gris | Mesuré sur le relevé réel : coefficients validés 1/7 → 3/7 (Tesseract) ; fragments de règles, taches et blocs de fond écartés sans redessiner de glyphe ; décision fondée sur la densité, jamais sur une valeur attendue |
| 071 | Hauteur de glyphe normalisée à 36 px avant Tesseract (au lieu de ×3) ; chiffres `eng` + liste blanche, libellés `fra`, ancres `fra` | Mesuré : ×3 (≈ 90 px) confondait 5/9/« . » sur des chiffres nets ; relecture PSM 8 puis 13 uniquement si PSM 7 ne donne rien d'analysable (ordre fixe, sans oracle) |
| 072 | Tesseract par stdin, cellules en parallèle (`OCR_CELL_WORKERS`, défaut min(4, CPU), 1–16), lecture en deux temps (libellés → Total → chiffres) | Relevé réel 23,8 s → 15,3 s par page ; ancrage demi-résolution rejeté (mesuré : bornes de section fausses sur le réel) |
| 073 | ~~Série lue près de « Série » (haut gauche)~~ (localisation remplacée par 082) ; job v1 enrichi de `qualificationCode` (champ additif) | Lue et différente → `SERIES_MISMATCH` (FAILED, aucune valeur) côté worker, revérifiée par Go ; illisible → non bloquante, signalée dans le diagnostic |
| 074 | Unicité des rangées contrôlée deux fois : worker (affectation + contrôle `analyze`) et Go (index de rangée unique, tables de preuve identiques, cellules de note distinctes) | Deux matières ne peuvent jamais être lues sur la même rangée |
| 075 | Résultat refusé par Go → analyse courante `FAILED/RESULT_REJECTED`, motif statique journalisé ; 422 refus, 409 périmé, 400 malformé, 503 transitoire | Le worker acquitte 400/409/422 ; plus de rejeu 3 × 5 min ni de faux `ANALYSIS_TIMEOUT` ; aucune migration (statut et code existants) |
| 076 | Outillage : fixtures génériques déterministes comparées octet par octet, `ocr-tests` n'écrit qu'en tmpfs, générateur BAC réaliste non stocké, `make check` comme CI locale | `make fixtures` régénère délibérément ; harnais privé `<nom>.expected.json` + validation Go (`cmd/ocrvalidate`) + rapport anonyme ; images de debug seulement avec `--keep-debug`, sinon supprimées |
| 077 | ~~RapidOCR évalué dans une image de banc séparée~~ | Remplacée par 078 (adopté) ; image de banc et `rapidocr_reader.py` supprimés |
| 078 | **RapidOCR 3.10.0 + onnxruntime 1.31.0 adoptés pour les seules cellules numériques BAC** (décision utilisateur du 14 octobre 2026), derrière le port `CellReader` (`ifri_ocr.engines`), `OCR_RAPIDOCR_ENABLED` (défaut `true`) | Tesseract garde ancres, libellés, série et chemin générique ; dépendances figées dans `uv.lock` du worker ; seul le modèle de reconnaissance PP-OCRv6 small est embarqué, téléchargé au build et vérifié contre `services/ocr/rapidocr-models.sha256`, jamais à l'exécution ; `ORT_DISABLE_TELEMETRY=1` (les roues Linux d'onnxruntime 1.31 embarquent une télémétrie 1DS vers un collecteur Microsoft) ; readiness = manifeste + SHA-256 + inférence d'essai ; modèle chargé pendant la passe d'ancrage (≈ 3 s par sous-processus) ; provenance `tesseract+rapidocr` ; image worker +≈ 250 Mo |
| 079 | **Règle d'acceptation BAC par matière attendue** (décision utilisateur) : note lue = Points ÷ Coeff → acceptée ; note illisible et Points ÷ Coeff validé par les totaux → acceptée ; note lisible mais différente → `GRADE_POINTS_MISMATCH` (statut LOW_CONFIDENCE) ; les autres rangées ne servent qu'aux totaux | Remplace la règle des trois totaux pour toutes les notes (ADR 068). « Validé par les totaux » est lu strictement : sommes exactes, coefficients validés, quotients plausibles, aucune note lisible contradictoire, moyenne affichée cohérente, **et toutes les autres rangées confirmées par leur note lue**, seule condition où le Total isole les Points de la rangée dérivée (deux notes illisibles pourraient compenser leurs erreurs ; test dédié). Code ajouté côté Go et UI ; pas de migration (`failure_code` texte libre) |
| 080 | Reconstruction d'une seule cellule illisible par colonne additive (Coeff, Points, Sur) : Total − somme des autres, `RECONSTRUCTED_FROM_TOTAL` (décision utilisateur) | Comptée seulement si un contrôle de sa rangée la confirme avec des opérandes lus ; deux cellules illisibles dans une colonne → aucune ; coefficient reconstruit jamais retourné (Coeff reste sa cellule) ; Points reconstruits jamais seule source d'une note ; Go recalcule chaque reconstruction (champ additif `reconstructed` des `arithmeticRows`) |
| 081 | Moyenne affichée de la ligne Total : troncature ou arrondi demi-supérieur à 2 décimales de Total Points ÷ Total Coeff, rien d'autre (constat sur un relevé : 202/21 = 9,619… imprimé 9,61) | Remplace la tolérance ± 0,01 × Σ Coeff ; contrôle du chemin « totaux » uniquement ; constat consigné dans ADMISSION_RULES, pas une règle officielle |
| 082 | Série = grande lettre isolée dans une zone fixe en haut à gauche sous l'en-tête, localisée relativement au tableau écrit, sans chercher le mot « Série » (décision utilisateur) | Zone et filtres de forme mesurés en géométrie anonyme sur le relevé réel ; lecture PSM 10 : deux binarisations liste A–G + une lecture libre concordantes ; absente, multiple ou discordante → non bloquante |

### Règle d'acceptation BAC en vigueur (ADR 079–081, remplace les formulations antérieures)

- Coefficient : sa propre cellule, entier 1 à 6, validé seulement si Sur = 20 × Coeff. Jamais retourné s'il a été reconstruit.
- Note d'une matière attendue :
  - lue et Points = Note × Coeff sur sa rangée → acceptée (`gradeSource=DIRECT_CELL`), sans dépendre des totaux ;
  - lue et Points lus ≠ Note × Coeff → `GRADE_POINTS_MISMATCH`, aucune valeur ;
  - illisible → Points ÷ Coeff (`POINTS_TOTALS`), décimale finie dans [0, 20], jamais arrondie, seulement si les Points de la rangée sont lus et si le tableau est vérifié : Σ Coeff, Σ Points, Σ Sur égaux aux cellules Total, coefficients validés et quotients plausibles sur toutes les rangées, aucune note lisible contradictoire, moyenne affichée (si lisible) égale à la troncature ou à l'arrondi à 2 décimales, toutes les autres rangées confirmées par leur note lue.
- Les rangées des autres matières ne servent qu'aux totaux.
- Une seule cellule illisible par colonne Coeff / Points / Sur peut être reconstruite depuis le Total ; elle ne compte que confirmée par sa rangée avec des cellules lues.
- Aucune confiance OCR ne remplace un contrôle arithmétique ; Go revérifie tout cela en rationnels exacts, plus la rangée, la série et l'unicité.
- Série contradictoire : `SERIES_MISMATCH` ; matière introuvable ou ambiguë : `NEEDS_REVIEW` ou `AMBIGUOUS_ROW`.

Sources primaires consultées : [Tesseract qualité/PSM](https://tesseract-ocr.github.io/tessdoc/ImproveQuality.html), [OpenCV CLAHE](https://docs.opencv.org/4.13.0/d5/daf/tutorial_py_histogram_equalization.html), [OpenCV headless](https://pypi.org/project/opencv-python-headless/4.13.0.92/), [NumPy](https://pypi.org/project/numpy/2.4.6/), [RapidOCR](https://pypi.org/project/rapidocr/3.10.0/), [onnxruntime](https://pypi.org/project/onnxruntime/1.31.0/). Seuils et variantes restent des heuristiques mesurées sur un corpus synthétique et un seul relevé réel : aucune précision de production revendiquée.

## Phase 6 — décisions (14 octobre 2026, plan validé par l'utilisateur)

Contrat complet : SUBMISSION_WORKFLOW.

| ADR | Statut | Décision | Conséquences |
|---|---|---|---|
| 083 | Retenue | Soumission en une transaction PostgreSQL (`SELECT … FOR UPDATE` application + session, revalidation complète en base, calcul, numéro, snapshot, drapeaux, audit, `SUBMITTED`) ; rejeu d'une application déjà soumise **avant** le contrôle de version | Double clic, retry réseau et requêtes concurrentes reçoivent le même récépissé (même numéro, une seule ligne de snapshot) ; codes distincts `CAMPAIGN_CLOSED`, `APPLICATION_INCOMPLETE`, `APPLICATION_NOT_ELIGIBLE` (422, code existant), `TRANSCRIPT_ANALYSIS_REQUIRED`, `TRANSCRIPT_ANALYSIS_OUTDATED`, `ADMISSION_RULES_CHANGED`, `APPLICATION_LOCKED`, `VERSION_CONFLICT` ; seule l'invalidation `ADMISSION_RULES_CHANGED` (analyse `OBSOLETE` + nouvelle analyse en file) est commitée |
| 084 | Retenue | Numéro `IFRI-{ANNÉE}-{PROGRAMME}-{SERIAL6}` : ANNÉE = `admission_campaigns.numbering_year` (`smallint NOT NULL`, rempli à partir de la date d'ouverture en heure de Porto-Novo), compteur `application_number_counters` par (année, filière) incrémenté par `INSERT … ON CONFLICT DO UPDATE … RETURNING` dans la transaction | Une campagne à cheval sur deux années garde une seule année ; un rollback annule aussi l'incrément : **sans trou en pratique, sans garantie contractuelle** ; unicité par `UNIQUE`, compteur seulement incrémentable de 1 ; `SEQUENCE` écartée (non transactionnelle, une par couple) |
| 085 | Retenue | Moyenne = Σ(note × coeff) / Σ coeff en `big.Rat` depuis les `NUMERIC` persistés ; `weighted_sum` et `coefficient_sum` exactes, `average numeric(32,30)` tronquée (jamais arrondie) + `average_is_exact`, `calculation_version` | La fraction est la valeur de référence ; une valeur périodique ne peut pas tenir en décimal sans troncature ; CHECK PostgreSQL de troncature et contrôle différé des sommes contre les matières copiées ; aucun calcul dans Vue, moyenne jamais exposée au candidat |
| 086 | Retenue | Le classement de la phase 9 compare d'abord la colonne décimale puis, en cas d'égalité, les **fractions exactement par produit en croix** (`a.weighted_sum × b.coefficient_sum` contre `b.weighted_sum × a.coefficient_sum`) | Deux moyennes égales à 30 décimales (1/3 et 0,333…3) restent départagées ; la règle IFRI d'arrondi ou de départage s'appliquera à la fraction (question ouverte, ROADMAP) |
| 087 | Retenue | Snapshot immuable par triggers : `application_submissions`, `application_submission_subjects` (note, coefficient, méthode `DIRECT_READ` / `ROW_ARITHMETIC` / `TABLE_TOTALS`, cellules reconstruites), `application_submission_documents`, drapeaux et audit refusent UPDATE, DELETE et TRUNCATE ; copie vérifiée contre l'extraction à l'insertion ; contrainte différée « SUBMITTED ⇒ snapshot complet et cohérent » ; CHECK « SUBMITTED ⇒ numéro + date » | L'admin de la phase 8 voit comment chaque valeur a été établie ; `ocr_extracted_subjects` est fermée en insertion après résultat ou décision ; l'analyse soumise ne devient jamais `OBSOLETE` ; `refresh_transcript_analysis` s'arrête hors `DRAFT` (correctif : le passage à `SUBMITTED` rendait l'analyse confirmée obsolète) |
| 088 | Retenue | **Statuts administratifs hors de `applications`** : `state` reste `SUBMITTED` (ou `CANCELLED`) et la ligne reste immuable ; la revue et la sélection de la phase 8 iront dans une table séparée, historisée | Le trigger d'immuabilité n'aura jamais besoin d'être assoupli ; une réouverture par l'admin, si l'IFRI la demande, sera un événement explicite et audité, pas une modification de la ligne |
| 089 | Retenue | `audit_events` générique dès la phase 6 : `actor_type` (CANDIDATE, ADMIN, SYSTEM), `actor_id` nullable, `target_type`/`target_id`, `metadata` JSON technique, append-only | La phase 7 réutilise la table sans migration ; `APPLICATION_SUBMITTED` ne contient aucune PII, note, moyenne ni numéro |
| 090 | Retenue | Après soumission, toutes les routes candidat renvoient 409 `APPLICATION_LOCKED` (contrôle avant la version) ; la session reste valable en **lecture seule** pour `GET …/current` et `GET …/current/submission`, prolongée jusqu'à la fin de la campagne avec un minimum de 7 jours | Lectures de documents et d'analyse refusées aussi : un cookie longue durée n'ouvre plus l'accès aux PDF d'identité ; `/candidature` redirige vers la confirmation sans jamais créer de brouillon ; consultation par numéro + code email différée (TODO) |
| 091 | Retenue | Drapeaux non bloquants calculés sous verrou consultatif de campagne contre les seules candidatures **soumises** : `DUPLICATE_EMAIL`, `DUPLICATE_PHONE` (valeurs normalisées), `DUPLICATE_TRANSCRIPT` (SHA-256), `SERIES_UNREADABLE` | Deux doublons simultanés se voient toujours ; la première candidature d'une paire ne porte aucun drapeau (lien inverse à afficher en phase 8) ; politique de doublons à trancher par l'IFRI |
| 092 | Retenue (préparation seulement) | Repli manuel futur : `validation_path` sous CHECK, `ocr_analysis_id`, sommes et moyenne nullables sous CHECK conditionnel (NOT NULL pour `CANDIDATE_CONFIRMED_OCR`) | Une soumission « vérification manuelle requise » après plusieurs échecs d'analyse ne demandera qu'un élargissement de CHECK, une table de transcription administrative séparée (jamais d'écriture dans le snapshot) et un compteur d'échecs par version de relevé, sans migration destructive ; politique et seuil d'échecs à définir avec l'IFRI |
| 093 | Retenue | Dialogue de confirmation = `<dialog>` natif ouvert par `showModal()` (jamais `show()`), rôle `alertdialog`, focus initial « Annuler », Escape bloqué seulement pendant l'envoi, focus restitué explicitement | Fond inerte et focus piégé natifs, sans Reka UI ni dépendance ; jsdom n'implémentant pas `showModal`, Vitest le simule et Playwright vérifie le comportement réel |
| 094 | Retenue | Le down de 00006 refuse de s'exécuter (SQLSTATE 55000) s'il existe une soumission, un compteur, un événement d'audit ou une application non `DRAFT` | Aucune perte silencieuse ; cycle up/down/up prouvé en schéma isolé sans soumission, refus prouvé avec soumissions |
