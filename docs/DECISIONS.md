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

## Phase 5.1 — corrections (validation réelle en cours)

| ADR | Décision | Conséquence |
|---|---|---|
| 060 | Analyse automatique affichée sous la carte relevé, cinq étapes | Déclenchement backend existant conservé, aucune route de lancement manuel ; refresh = lecture seulement |
| 061 | TranscriptLayoutProfile BAC général : premier groupe écrit, oral exclu | Hypothèse de source pour ce format à confirmer avec règles officielles IFRI ; aucun coefficient métier |
| 062 | OpenCV headless 4.13.0.92 + NumPy 2.4.6, Python 3.14 conservé | Lock officiel hôte ; CLAHE/adaptive/canal, deskew crédible et refus perspective, géométrie inverse testée |
| 063 | 300 DPI initial, second prétraitement, 350 DPI optionnel ; maximum trois passes | Choix provisoire à mesurer, PSM 11 ancres / 6 section ; contradictions refusées, pas de guess |
| 064 | Corpus réel privé via bind mount read-only et oracle ignoré | Exclusion Git et contextes Docker ; sortie limitée aux matières demandées, valeurs et diagnostic anonyme |

Sources primaires consultées : [Tesseract qualité/PSM](https://tesseract-ocr.github.io/tessdoc/ImproveQuality.html), [OpenCV CLAHE](https://docs.opencv.org/4.13.0/d5/daf/tutorial_py_histogram_equalization.html), [OpenCV headless](https://pypi.org/project/opencv-python-headless/4.13.0.92/), [NumPy](https://pypi.org/project/numpy/2.4.6/). Les variantes et seuils sont des heuristiques à calibrer, aucune précision de production revendiquée.

### Stratégie BAC général par cellules — correction demandée

La stratégie active pour les scans BAC général remplace les essais OCR globaux par : correction géométrique prudente (contour de page quadrilatéral crédible, puis deskew), binarisation du canal V HSV (= max RGB), ancres du premier groupe écrit et Total écrit, reconstruction morphologique d'une grille de cinq colonnes fixes, OCR PSM7 de chaque cellule agrandie ×3. Whitelist numérique `0123456789,.`. Les en-têtes OCR ne déterminent plus les colonnes. Les tables sans séparateurs horizontaux internes utilisent les bandes de texte numérique pour reconstruire les séparations ; une géométrie ou des cellules incohérentes imposent NEEDS_REVIEW. Les profils génériques conservent leur chemin existant.

La liste fermée documentaire des matières BAC sert au matching ; elle ne remplace pas le snapshot des matières d'admission. Notes et coefficients viennent toujours de leurs propres cellules. Points = note × coefficient et Sur = 20 × coefficient sont vérifiés exactement sur chaque ligne, puis les sommes coefficient/points/Sur sont comparées au Total écrit. La note récapitulative imprimée peut être arrondie ou tronquée au centième : contrôle secondaire avec tolérance `0,01 × coefficient total` ; aucun résultat de moyenne d'admission n'est créé.

Sur demande explicite de l'utilisateur, ces preuves arithmétiques remplacent le seuil Tesseract **uniquement pour ce profil**. Les confiances réelles restent présentes. Go revérifie indépendamment les produits, sommes, ligne sélectionnée et bornes de valeurs avec `big.Rat` avant acceptation d'une extraction à faible confiance. Les lignes incohérentes portent NEEDS_REVIEW dans le diagnostic ; le résultat conserve l'état terminal existant LOW_CONFIDENCE avec failureCode NEEDS_REVIEW, afin de ne pas inventer un circuit administratif de phase 6.

Diagnostic privé : texte brut des cinq cellules du tableau uniquement, valeurs parsées, contrôles par ligne, contrôles des totaux et série. Aucun en-tête d'identité, texte de page complet, hash ou PDF n'est exporté. Calibration via `ocr_private_run.py` ; l'ancien argument `--evaluate` utilise désormais la même stratégie, sans matrice de variantes. Les anciens benchmarks sont historiques et ne valident pas ce nouveau chemin. Test réel et régression Docker restent à mesurer. RapidOCR sera évalué sur les mêmes cellules seulement si la lecture numérique reste insuffisante, après vérification de compatibilité Python 3.14.

### Bordures, colonnes et budget de calibration

La sélection des colonnes est ancrée aux extrémités du premier trait horizontal long du tableau. Six axes sont recherchés à l'intérieur de cette bordure, près des ratios du profil stable : 0 / 0,535 / 0,645 / 0,755 / 0,89 / 1. Un axe absent ou décalé déclenche le fallback de ratios, jamais une translation silencieuse des cinq colonnes. Matière est nettement plus large ; Points est légèrement plus large que Note/Coeff/Sur. Un tableau sans bordure crédible renvoie COLUMN_SEGMENTATION_FAILED.

Otsu est calculé sur le canal V de chaque crop, avec marge intérieure de 8 pixels sur X et 5 sur Y pour exclure les traits. Les temps par cellule sont consignés. La boucle s'arrête dès reconnaissance du libellé Total ; aucune rangée orale n'est ajoutée. La calibration privée seule dispose de 120 secondes par page ; le budget production reste inchangé. Le prochain benchmark devra comparer ce coût à cinq appels Tesseract par bandes de colonnes PSM6 (numériques avec whitelist), sur les mêmes limites et contrôles, avant tout changement de stratégie production.

Règle d'acceptation mise à jour sur instruction utilisateur : Note et Coeff sont obligatoires ; Sur doit être lisible et égal à 20 × Coeff. Points peut être illisible (diagnostic `null` pour ce contrôle), mais une valeur lisible contradictoire bloque la ligne. Go applique la même règle, conserve Points manquant sans fabriquer de valeur extraite et vérifie les sommes contre les totaux lisibles. Les produits calculés sont exclusivement des contrôles documentaires.

Le run privé précédent reste en échec : 31,193 s, colonnes décalées, budget épuisé. La vérification locale sur la zone de tableau diagnostique retrouve 8 rangées et des largeurs relatives 53/11/11/13/11 %, sans succès OCR déclaré. Le nouveau run automatique Docker reste à effectuer.

### Règle d'acceptation corrigée — prioritaire sur la règle précédente

Sur = 20 × Coeff valide uniquement le coefficient. Une note exige Points = Note × Coeff sur sa ligne, OU les deux sommes exactes Σ Note×Coeff = Total Points et Σ Coeff = Total Coeff. Points illisible ne valide donc plus une note sans totaux. Points lisible contradictoire reste bloquant. Les statuts `coefficientStatus` et `gradeStatus` sont séparés ; validation par ligne ou par totaux explicitée. Go vérifie la preuve décimale indépendamment et exige des totaux Points lisibles pour l'acceptation du dossier. Coeff entier 1..6 ; Sur multiple de 20 cohérent ; Note 0..20. Aucun seuil de confiance ne supplée un contrôle arithmétique manquant.

La limite physique de l'en-tête est exclue avant la projection des rangées, avec marge intérieure de 5 px sur X / 4 px sur Y. Le traitement effectif de chaque cellule est loggé : HSV_V_CELL_OTSU ; pour Points trop noir (>45 %), RED_CELL_CLAHE_OTSU. Cette décision utilise la densité du crop, pas ses valeurs attendues. Le TSV d'un seul appel fournit texte et confiance : seuls les mots level=5, confidences négatives ignorées et comptées, confiance moyenne des mots restants, véritables zéros conservés.

Dernier scan privé Docker : 36,864 s, NEEDS_REVIEW, aucune extraction complète acceptée, Total et série encore illisibles. Il ne valide pas la nouvelle correction. Les contrôles locaux et le run privé suivant restent distincts.

### Correction documentaire : notes dérivées exclusivement après trois totaux

Instruction utilisateur prioritaire : quand Σ Coeff, Σ Points et Σ Sur lus correspondent tous aux cellules Total, la valeur retenue de chaque note est Points ÷ Coeff, avec `gradeStatus=VALIDATED_BY_TOTALS` et `gradeSource=POINTS_TOTALS`. La lecture brute/directe et sa divergence restent conservées. Coeff n'est jamais dérivé. Une somme fausse ou incomplète garde toutes les lignes en NEEDS_REVIEW. Les décimales non terminantes ne sont ni arrondies ni inventées. Go autorise une divergence note brute/valeur retenue uniquement pour ce mode avec preuve complète indépendante en rationnels exacts et coordonnées Points/Sur ; les autres extractions gardent l'égalité lecture brute/valeur. Aucun calcul de moyenne d'admission n'est introduit.

Parseur : virgule décimale et séparateur final isolé acceptés (`18.`, `80 .`), sans assembler des chiffres séparés. Le contrôle Sur/Coeff ne dépend plus de la lisibilité de Note. Les noyaux morphologiques longs suppriment les traits sur chaque cellule avant agrandissement ; les crops de données ont une marge symétrique autour de leur propre bande, y compris Anglais, sans remonter à l'en-tête. Seuils de confiance inchangés.

Dernier run privé : 40,398 s, NEEDS_REVIEW, 8 rangées correctement isolées ; les notes et totaux restent insuffisants. La nouvelle correction attend son run OCR réel. Validation locale avant relance : 74 tests Python et Go tests PASS ; aucune précision réelle déclarée acquise.
