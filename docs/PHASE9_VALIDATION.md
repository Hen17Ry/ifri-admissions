# Phase 9 — validation (15 octobre 2026)

**Statut : faite.** Plan validé par l'utilisateur avec ses ajustements :
- critère mathématique appliqué **par groupe d'égalité**, signalé quand il est sauté, avec l'exemple A/B/C et une question IFRI à deux options ;
- export public limité aux admis et à la liste d'attente (rang du gel, décision actuelle) ;
- exports seulement après le gel, avec version et date en en-tête ;
- bornes : 1 à 10 000 places, seuil de 0 à 20 « au moins égal », pas de rang pour un Non classé ;
- promotions avec place libre, rang du gel conservé ;
- permissions ;
- PDF par le worker, CSV par Go, formules neutralisées par préfixe et testées avec un nom synthétique qui commence par un tiret.

PASS désigne une exécution réelle sur l'hôte avec Docker. Aucun commit effectué.

Contrat : RANKING_SELECTION. Décisions : DECISIONS (ADR 122–131). Modèle : DATA_MODEL (phase 9). Design : `design-system/ifri-admissions/pages/admin-selection.md`.

## `make check` — 26 étapes, CHECK PASSED

| Étape | Résultat | Détail |
|---|---|---|
| web build / typecheck / lint / format | PASS | Vite, vue-tsc, ESLint, Prettier |
| web Vitest | PASS | 84 tests (8 fichiers), dont 6 nouveaux pour le classement |
| Go gofmt / vet (+ tag integration) / test / build | PASS | Unitaires du classement, des exports et des permissions compris |
| Python pytest (hôte), Ruff worker et scripts | PASS | 122 passés, 4 ignorés |
| Compose config (+ profil test), `git diff --check` | PASS | |
| Stack reconstruite, `db up` (**00009** appliquée à la base de dev), seed ×2 | PASS | Nouvelle politique S3 de l'initialiseur appliquée |
| Intégration Go (`references-tests`) | PASS | **301** tests et sous-tests, 0 échec, PostgreSQL et Redis réels |
| smoke, références, brouillons, documents | PASS | Contrats des phases 1–4 inchangés |
| `ocr-tests` (régression OCR) | PASS | pytest 126/126 dans le conteneur (DejaVu réel) ; fixtures identiques octet pour octet ; benchmark BAC 18/18, 0 valeur fausse acceptée |
| fixtures intactes, `ocr_smoke` | PASS | `d-photo` SUCCEEDED en 17,8 s, `c-declared-d` `SERIES_MISMATCH` en 10,4 s |
| **playwright-e2e** (stack isolée `ifri-e2e`) | PASS | 5 000 dossiers synthétiques en 23,1 s, puis 5 scénarios en 1,5 min, dont le nouveau parcours de sélection (14,2 s) ; stack détruite ensuite (0 conteneur et 0 volume `ifri-e2e`) |

Ce passage vert est le troisième :
- **premier** : le port 5173 était occupé par le Vite d'un autre projet, arrêté avec l'accord de l'utilisateur ;
- **deuxième** : la fixture de quota à 0 de la phase 2 ; puis le préfixe S3 manquant (voir Playwright) ;
- **troisième** : tout passe.

Mémoire de la stack e2e (pic par conteneur pendant le passage) : API 274 Mio, worker 288 Mio, MinIO 256 Mio, PostgreSQL 162 Mio, Redis 6 Mio.

## Tests de la phase

### Go, unitaires (`selection/rank_test.go`)

| Test | Ce qui est prouvé |
|---|---|
| `TestAverageIsComparedExactlyByCrossProduct` | 40/3 passe devant sa propre troncature à 30 décimales (la colonne décimale les aurait confondues) ; 40/3 et 80/6 sont à égalité exacte et départagées par les mathématiques |
| `TestMathematicsThenDateThenNumber` | Ordre moyenne → mathématiques → date → numéro ; critère décisif de chaque rang ; mathématiques jamais regardées pour une moyenne unique |
| `TestMathematicsSkippedForTheWholeGroupWithoutMathSubject` | L'exemple A/B/C donne A, B, C (date) et marque tout le groupe « sauté » ; hors groupe, un candidat sans mathématiques est classé normalement |
| `TestSeatsAndMinimumAverage` | Égalité pile sur la ligne des places départagée ; moyenne exactement égale au seuil classée, 9,998 non ; places restantes jamais données sous le seuil ; 1 place, autant de places que de candidats, places non définies |
| `TestWithdrawalsAndPromotionsFollowTheList` | Places libres, premier en attente, désistements successifs, désistement d'un candidat en attente, version passée reconstruite |
| `TestPublicListHoldsNoAverageNorWithdrawnNorNotRanked` | Colonnes publiques exactes, admis et attente seulement, aucune moyenne, aucun « Mathématiques », ni désisté ni non classé ; version et dates en heure du Bénin |
| `TestInternalListShowsCriteriaAndSkippedMathematics` | Moyenne tronquée, mention « Maths non comparées… », critère, « Premier de la liste », non classé sans rang |
| `TestCSVPrefixesFormulaCellsWithoutChangingThem` | BOM, CRLF ; `-Tiret` → `'-Tiret`, `=SOMME(A1)` → `'=SOMME(A1)`, tabulation, `+`, `@` ; `A-B` et les noms accentués inchangés |

Le module `admin` ajoute 11 cas de permissions de la phase 9 à sa matrice.

### Go, intégration PostgreSQL et Redis réels (`TestPostgresSelection`, schéma isolé)

Jeu de données SEIOT généré avec des valeurs imposées :
- une égalité exacte 40/3 = 80/6 départagée par les mathématiques ;
- un groupe d'égalité à 14 avec un candidat DT/EAP sans mathématiques ;
- deux dossiers de même moyenne et même date, départagés par le numéro ;
- un candidat sous la moyenne minimale ;
- un dossier rejeté ;
- un nom synthétique `-Tiret`.

Trois dossiers IA servent au test de concurrence.

| Sous-test | Ce qui est prouvé |
|---|---|
| provisional ranking | Seuls les validés sont classés (le rejeté n'apparaît pas) ; conditions de blocage ; chaque consultation auditée |
| roles × selection routes | Lecture : Directeur, Directeur adjoint, responsable ; 403 pour l'agent et le super-administrateur ; réglages et gel : Directeur seul ; désistements et promotions : Directeur adjoint seul ; exports : Directeur et Directeur adjoint |
| seats and minimum average | 0, 10 001, 20,01, 10,555 et « dix » refusés (422) ; 1, 10 000 et 20 acceptés ; « 11,00 » stocké 11.00 ; 3 changements audités ; ligne des places et non classé |
| every precondition of the freeze | Campagne ouverte (API et trigger) ; places affichées différentes des places enregistrées ; dossier remis en vérification ; correction en attente ; places non définies ; gel accepté ; second gel refusé |
| frozen snapshot | Ordre exact des 9 candidats, critère décisif, état des mathématiques et décisions conformes au calcul ; Non classé sans rang ; gel audité |
| after the freeze | Réglages refusés (API et base) ; retour arrière `REVIEW_FROZEN` ; UPDATE, DELETE et TRUNCATE refusés sur le snapshot et le gel ; ligne ajoutée après le gel refusée même fidèle ; rollback de 00009 refusé |
| withdrawals and promotions | Promotion sans place libre refusée ; désistement sans motif refusé ; version périmée refusée ; promotion qui saute un candidat refusée ; deux désistements puis deux promotions ; désistement d'un candidat en attente ; désistement d'un promu ; non classé refusé ; proposition qui saute le désisté ; états finaux ; la base refuse seule une promotion hors ordre et une version sautée ; audit |
| exports | CSV public : en-tête versionné, colonnes, `'-Tiret`, aucune moyenne, aucun désisté ni non classé, ni email ni téléphone ; CSV interne : moyennes, mathématiques sautées, critères ; job Redis = identifiant seul ; contenu interne sans moyenne pour la version publique ; PDF invalide refusé ; PDF accepté, servi en pièce jointe, refusé au responsable ; 4 exports et 1 téléchargement audités |
| a freeze and a revert at the same time | Jamais les deux : soit le gel passe et le retour arrière reçoit `REVIEW_FROZEN`, soit le retour arrière passe et le gel reçoit `RANKING_FREEZE_BLOCKED` |
| `TestPostgresSelectionMigrationRoundTrip` | 00009 down puis up ; down refusé dès qu'une moyenne minimale existe |

Ce test de concurrence a révélé un défaut de la phase 8 : le verrou de revue lisait l'état gelé avec l'instantané d'avant l'attente du verrou. La base refusait bien l'écriture, mais par une erreur brute. C'est corrigé par l'ADR 127. Les tests de la phase 8 ont été adaptés :
- un gel inséré triggers désactivés pour tester le seul verrou ;
- des rollbacks qui descendent à la version 7.

Le test des références (phase 2) a vu sa fixture « 0 place » passer à « non défini », 0 étant désormais hors bornes.

### Python (worker)

`tests/test_exports.py` :
- la liste publique relue contient chaque cellule, l'en-tête versionné, les lettres béninoises (Ɖɔkɔ) et aucune moyenne ;
- une liste de 150 lignes répète l'en-tête du tableau et la pagination à chaque page ;
- les documents malformés sont refusés ;
- traitement : PDF envoyé, 404 acquitté, échec de rendu signalé, 409 acquitté, erreurs de transport relancées.

pytest hôte : 122 passés, 4 ignorés (rendu testé avec les polices DejaVu de l'hôte, ignoré si elles manquent) ; conteneur : 126 passés.

### Vitest

`selection.test.ts` (6 tests, nouveaux) :
- ligne des places, mathématiques sautées, non classés ;
- réglages invalides refusés, puis enregistrés ;
- blocages qui désactivent le gel (`aria-describedby`) ;
- gel après confirmation définitive en `alertdialog` ;
- lecture seule pour le responsable ;
- désistement motivé, promotion du proposé, conflit de version ;
- liens CSV versionnés et génération du PDF.

Les tests d'administration et de revue sont adaptés au retrait de `ranking.read` pour l'agent et à la nouvelle entrée « Classement ». Total : 84 tests Vitest, 8 fichiers.

### Playwright (stack isolée `ifri-e2e`, Chromium)

`selection.spec.ts` s'exécute en dernier. Il génère 6 dossiers IA et ferme la campagne par la CLI, puis :

1. Le super-administrateur crée le Directeur, le Directeur adjoint et un agent, qui sont activés et enrôlés.
2. L'agent valide 5 des 6 dossiers par l'API.
3. Le Directeur saisit 3 places. Le classement provisoire affiche « Limite des 3 places » et « 1 dossier n'est pas encore validé ou rejeté ». Le bouton de gel est désactivé, et un POST direct est refusé (409 `RANKING_FREEZE_BLOCKED`). Captures aux 4 largeurs.
4. Le dernier dossier est validé : « Toutes les conditions du gel sont réunies ». Gel confirmé (« définitif ») : « liste version 1 ».
5. Export public :
   - CSV `liste-ia-dev_local-v1-publique.csv` : « Version 1 du », colonnes publiques, 3 admis, ni moyenne ni nombre décimal, ni email ni téléphone ;
   - PDF généré par le worker réel, téléchargé (`%PDF-`).
6. Le Directeur adjoint enregistre le désistement du rang 1 avec un motif (« liste version 2 ») et promeut le rang 4 proposé (« liste version 3 »). L'historique compte 3 versions ; le nouveau CSV public porte « Version 3 du », sans le désisté et avec 3 admis.

Le premier passage complet a révélé un défaut que les tests d'intégration ne pouvaient pas voir (ils utilisent un bucket en mémoire). Le compte S3 de l'API n'avait droit qu'au préfixe `applications/*` : le dépôt du PDF échouait (503) et l'export restait en attente. La politique de l'initialiseur MinIO autorise désormais aussi `selection/*` (ADR 130), et l'échec de stockage est journalisé côté API.

Résultat : 5 scénarios PASS (administration, parcours candidat, extraction refusée, revue, sélection).

## Base de développement

`make check` applique 00009 à la base de développement. Ni gel ni événement ; la campagne `DEV_LOCAL` reste ouverte : seule la stack e2e jetable est close. Pour essayer le classement en développement, voir RANKING_SELECTION (commandes irréversibles dans cette base).

## Limites et points ouverts

- **Questions IFRI** (ROADMAP 3 et 4) :
  - critère mathématique absent : règle par groupe appliquée, ou alternative à proposer ;
  - conduite à tenir en cas de fraude après le gel.
- **Aucun dégel** : un gel par erreur ne se corrige que par une décision hors application, à définir avec l'IFRI.
- **PDF** : rendu vérifié par relecture du texte (pytest) et par téléchargement réel (Playwright). L'aspect visuel est relu sur des rendus de test, pas dans chaque navigateur.
- **Dates de campagne** : pas d'écran d'administration. En production, la période se termine par `ends_at`. En développement, seulement par `api dev close-campaign`.
- **Conservation** des exports PDF stockés : aucune purge, à définir en phase 10 avec la politique de conservation.
- **Production** : la politique du compte S3 de l'API doit inclure `selection/*` en plus de `applications/*`.
- **Consultation des résultats par le candidat** et notifications : hors périmètre.

## Prochaine étape proposée

Phase 10, mise en production :
- CI GitHub ;
- Nginx et HTTPS, en-têtes de production ;
- sauvegardes et restauration testée ;
- antivirus et quarantaine des uploads ;
- rétention et purge (documents, exports, journal) ;
- observabilité ;
- audit d'accessibilité ;
- test de charge ;
- plan de déploiement.

À démarrer après accord.
