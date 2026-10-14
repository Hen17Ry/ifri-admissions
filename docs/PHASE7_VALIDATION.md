# Phase 7 — validation (14 octobre 2026)

**Statut : faite.** Plan validé par l'utilisateur avec ses ajustements :
- stack Playwright isolée et sans rebuild, avec `check-fast`, `e2e` et la mémoire mesurée ;
- gel de `numbering_year` ;
- sous-clés HKDF séparées et versionnées ;
- limitation de débit avant tout argon2 ;
- QR code servi seulement pendant l'enrôlement ;
- `reset-mfa` et `reactivate` en CLI ;
- jeton d'activation effacé de l'historique ;
- TOTP à ±1 pas, régénération qui invalide les anciens codes ;
- IP en HMAC.

PASS désigne une exécution réelle sur l'hôte avec Docker. Aucun commit effectué.

Contrat : ADMIN_SECURITY. Décisions : DECISIONS (ADR 095–108). Modèle : DATA_MODEL (phase 7). Design : `design-system/ifri-admissions/pages/admin.md`.

## `make check` — 26 étapes, CHECK PASSED

| Étape | Résultat | Détail |
|---|---|---|
| web build / typecheck / lint / format | PASS | Vite, vue-tsc, ESLint, Prettier |
| web Vitest | PASS | 64 tests (6 fichiers), dont 13 pour l'administration |
| Go gofmt / vet (+ tag integration) / test / build (`api`, `db`, `ocrvalidate`) | PASS | 187 tests et sous-tests unitaires |
| Python pytest (hôte), Ruff worker et scripts | PASS | 109 passés, 4 ignorés ; Ruff couvre le nouveau `common_passwords.py` |
| Compose config (+ profil test), `git diff --check` | PASS | |
| Stack reconstruite, `db up` (00007), seed ×2 | PASS | 5 services sains |
| Intégration Go (`references-tests`) | PASS | **266** tests et sous-tests (unitaires compris), 0 échec : PostgreSQL et Redis réels |
| smoke, références, brouillons, documents | PASS | Contrats des phases 1–4 inchangés |
| `ocr-tests` (régression OCR) | PASS | pytest 113/113 ; fixtures identiques octet pour octet ; benchmark BAC 18/18, 0 valeur fausse acceptée |
| fixtures intactes, `ocr_smoke` | PASS | `d-photo` SUCCEEDED en 10,3 s, `c-declared-d` `SERIES_MISMATCH` en 9,2 s |
| **playwright-e2e** (stack isolée `ifri-e2e`) | PASS | 3 scénarios en 56,8 s, dont le nouveau parcours administrateur ; stack détruite ensuite (0 conteneur et 0 volume `ifri-e2e` restants) |

La base de développement est vérifiée après le `make check` : **toujours 4 soumissions** (celles d'avant la phase 7), aucune ajoutée par Playwright, 0 compte administrateur, migration 7.

## Préalable : isolation de Playwright et `numbering_year`

- **Stack jetable** : `infra/scripts/e2e.sh` lance le projet Compose `ifri-e2e`.
  - Il a ses propres PostgreSQL, Redis, MinIO et worker OCR, sur les ports 18180, 19100 et 19101.
  - Il démarre avec `--no-build` à partir des images déjà construites (noms explicites `ifri-admissions-*` dans `compose.yaml`) et s'arrête avec un message clair si une image manque.
  - Il applique la migration et le seed, lance Playwright (Vite pointé par `API_PORT`), puis exécute `down -v` sur ce seul projet, même après un échec ou une interruption.
- **Cibles** : `make check-fast` (statique + stack, sans e2e), `make e2e` (e2e seule, `ARGS="admin.spec.ts"` possible) ; `make check` garde l'e2e.
- **Seed** : `DEV_LOCAL.numbering_year` vaut l'année courante dans un schéma neuf (testé). La migration 00007 gèle l'année dès le premier numéro (testé), et le seed relancé ne la change plus.

### Mémoire mesurée (`docker stats` toutes les 2 s pendant un `make e2e` complet, les deux stacks démarrées)

| Conteneur | Pic, stack de dev | Pic, stack e2e |
|---|---|---|
| api | 74 Mio | 140 Mio (argon2id compris) |
| minio | 88 à 192 Mio | 190 Mio |
| ocr-worker | 120 à 138 Mio | 398 à 440 Mio (analyse OCR en cours) |
| postgres | 67 Mio | 69 à 81 Mio |
| redis | 5 Mio | 5 Mio |
| **Total des pics** | **355 à 474 Mio** | **802 à 853 Mio** |

Côté hôte, mesuré par `ps` pendant le scénario admin : Vite, le lanceur Playwright et Chromium sans interface représentent environ **1,3 Gio**. Total observé avec les deux stacks : **environ 2,5 Gio**. Sur une machine plus juste, arrêter la stack de développement (`docker compose stop`) avant `make e2e` libère environ 0,4 à 0,5 Gio. Le worker OCR e2e reste le poste le plus lourd pendant une analyse.

## Tests de la phase

### Go, unitaires

- **`secrets`** :
  - sous-clés distinctes par usage et par version ;
  - secret chiffré lié au compte (illisible avec un autre compte, une autre version ou une autre clé maître) ;
  - CSRF lié au jeton de session ;
  - décodage de la clé (taille minimale).
- **`passwords`** :
  - format PHC, sel aléatoire, normalisation NFC, empreintes malformées refusées ;
  - paramètres documentés figés ;
  - détection du réhachage ;
  - politique : 11 caractères refusés, 12 acceptés, Unicode compté en caractères, 129 refusés, `123456789012`, `QWERTYUIOPASDFGH`, `Password1234` refusés, email et partie locale refusés ;
  - liste de plus de 40 000 entrées chargée.
- **`totp`** :
  - **six vecteurs de la RFC 6238** (8 chiffres, puis 6) ;
  - ±1 pas accepté, ±2 refusé ;
  - **rejeu refusé**, pas antérieur refusé, pas suivant accepté ;
  - codes espacés acceptés ;
  - URI `otpauth`, SVG sans script.
- **`admin`** :
  - **une requête bloquée par la limitation ne déclenche aucun argon2** : comptage sur 5 emails inventés, pour chacune des politiques IP et identifiant, ainsi que pour l'activation, le changement de mot de passe et la MFA ;
  - aucune clé de limitation ne contient l'email ni l'IP en clair ;
  - toutes les routes déclarent une permission connue, une route sans permission est refusée (panic), seules la connexion et l'activation sont publiques ;
  - ressource inconnue → 404 avec les en-têtes stricts ;
  - Origin et JSON contrôlés avant tout ;
  - matrice rôles × permissions et périmètre par filière ;
  - forme et normalisation des codes de secours.
- **`config`** : valeurs par défaut de l'administration, refus (clé absente, courte ou non base64, version 0, durées hors bornes, durée absolue plus courte que l'inactivité), et en production refus de la MFA désactivée et de la clé d'exemple.

### Go, intégration PostgreSQL et Redis réels (`TestPostgresAdmin`, schéma isolé, horloge simulée)

| Scénario | Vérifié |
|---|---|
| CLI | Mot de passe courant refusé ; premier super-administrateur créé ; second refusé ; audit SYSTEM |
| Parcours complet | Cookie `Path=/api/v1/admin`, HttpOnly, SameSite=Strict ; session de pré-authentification refusée sur les routes métier ; mutation sans CSRF → 403 ; mauvais code → 422 sans perdre la session ; enrôlement → `ACTIVE`, **ancien jeton invalide (rotation)**, 10 codes de secours ; secret TOTP non lisible en base, `key_version = 1` |
| TOTP et codes de secours | Code du pas précédent accepté ; **rejeu refusé** ; code de secours accepté une fois, puis refusé (même en minuscules) ; **régénération : anciens codes refusés**, nouveaux acceptés |
| Non-énumération | Compte inconnu et mauvais mot de passe : réponses **identiques octet pour octet**, **un argon2 chacun** ; compte désactivé avec le bon mot de passe : même réponse |
| Activation | Jeton stocké en SHA-256 seulement ; inspection ; mot de passe courant ou égal à l'email refusé ; activation ; réutilisation → 410 ; expiration après 72 h → 410 ; nouveau lien valide |
| **Matrice permissions × routes** | Toutes les routes enregistrées : 401 sans session ; pour SUPER_ADMIN, REVIEWER (GL), SELECTION_MANAGER et AUDITOR, 403 `ADMIN_FORBIDDEN` sans la permission, accès avec ; routes du second facteur refusées à une session active |
| Périmètre par filière | Vérificateur GL : `CanFor(GL)` vrai, `CanFor(IM)` faux, `Scope` = [GL] ; rôle global limité à une filière, filière ou rôle inconnus, aucun rôle → 422 |
| Cloisonnement | Jeton candidat valide → 401 sur l'API admin (même posé dans le cookie admin) ; jeton admin → 401 `DRAFT_SESSION_REQUIRED` sur l'API candidat |
| Révocations | Désactivation, changement de rôle et réinitialisation MFA ferment la session de la cible ; changement de mot de passe : mauvais mot de passe actuel → 422 (session gardée), mot de passe courant refusé, puis toutes les sessions révoquées (y compris une autre session ouverte) et ancien mot de passe refusé ; déconnexion |
| Dernier super-administrateur | Auto-désactivation → 409 ; rétrogradation du dernier super-administrateur → 409 ; avec un second super-administrateur, sa rétrogradation est permise et révoque ses sessions |
| Expirations | Pré-authentification après 10 min ; inactivité après 30 min ; durée absolue après 8 h malgré une activité toutes les 20 min ; sessions révoquées `EXPIRED` |
| **Limitation de débit** | Identifiant : délais progressifs puis verrouillage de 15 min (`Retry-After`) ; compte verrouillé : 429 **sans argon2**, même avec le bon mot de passe ; libéré après 15 min. IP : 45 emails inventés → plus de 15 requêtes bloquées, **aucune ne calcule d'argon2** |
| Mot de passe oublié (CLI) | `reset-password` : sessions révoquées, ancien mot de passe refusé, nouveau lien d'activation, **MFA conservée** |
| Audit | 22 types d'événements présents (connexions réussies et échouées, verrouillage, MFA, codes, déconnexion, mot de passe, comptes, rôles, `ADMIN_DATA_ACCESSED` via `RecordAccess`…) ; `ADMIN_MFA_RESET` attribué à ADMIN et à SYSTEM (CLI) ; connexion échouée avec `identifierHash` et `ipHash` de 64 caractères ; **aucune métadonnée ne contient un email, une IP de test ou un mot de passe** ; pagination par curseur ; filtre de l'API ; type inconnu → 400 ; suppression de compte et modification d'attribution refusées par la base ; down de 00007 refusé tant qu'un compte existe |
| Limiteur Redis | Délais 0, 0, 1 s, 2 s, 4 s, puis verrouillage ; clés indépendantes ; remise à zéro ; plafond de tentatives |
| Migration | Schéma neuf : up → down (tables admin supprimées) → up → création d'un super-administrateur |

Les tests de soumission ont été adaptés à l'année courante. Un nouveau test vérifie que `numbering_year` reste modifiable avant le premier numéro, puis est gelé, et que le seed relancé la garde.

### Vitest (13 nouveaux tests)

- **Garde de routeur** : sans session → connexion ; étape MFA → vérification ; permission absente → tableau de bord avec refus ; connexion ou vérification déjà actives → tableau de bord ; activation publique.
- **Connexion** : message générique, mot de passe effacé, message de limitation ; passage au second facteur avec redirection conservée ; bascule afficher/masquer, `autocomplete`.
- **Activation** : jeton lu dans le fragment puis **effacé de l'URL** (`replaceState`) ; saisies différentes ; erreur de politique liée au champ ; succès ; lien invalide.
- **Second facteur** : QR en `data:` avec texte alternatif, clé groupée, `one-time-code` ; mauvais code ; codes de secours affichés une fois, QR retiré, poursuite bloquée tant que la case n'est pas cochée ; code de secours au lieu du TOTP.
- **Comptes** : création d'un vérificateur limité à IM (`grants` envoyés), lien affiché une fois ; désactivation confirmée par `alertdialog` avec focus sur Annuler ; jamais proposée sur son propre compte ; lecture seule pour un auditeur.
- **Journal** : libellés en français, dates en heure du Bénin, curseur, filtre type + date convertie en UTC.
- **Mon compte et navigation** : changement de mot de passe → déconnexion avec message ; menu filtré par permission ; tableau de bord vide ; déconnexion.

### Playwright (stack isolée, Chromium)

1. `api admin create-super-admin` par `docker compose exec` ; un second appel est refusé.
2. En-têtes de la page `/admin` : `X-Frame-Options: DENY`, CSP `frame-ancestors 'none'`.
3. Connexion : mauvais mot de passe → message générique ; bon mot de passe → enrôlement, **code TOTP calculé dans le test (RFC 6238)** → 10 codes de secours → tableau de bord.
4. Création d'un vérificateur limité à Génie Logiciel ; lien d'activation au format attendu (jeton de 43 caractères dans le fragment).
5. Dans un autre contexte de navigateur : activation, **fragment absent de l'URL**, mot de passe courant refusé, activation réussie, connexion avec enrôlement ; menu sans « Comptes » ; API comptes → 403.
6. Désactivation confirmée (focus initial sur Annuler) : la session du vérificateur → 401, rechargement → connexion, bon mot de passe → message générique.
7. Journal d'audit filtré sur « Compte désactivé ».
8. Session admin → 401 sur l'API candidat ; déconnexion → 401 sur la session admin.

Les captures aux 4 largeurs (connexion, enrôlement, tableau de bord, création, comptes, journal) sont sans défilement horizontal. Les deux parcours candidat passent aussi sur la stack isolée.

## Recréer le volume de la base de développement (à faire par l'utilisateur)

La base de dev garde ses 4 soumissions synthétiques (immuables) et l'année 2000, désormais gelée. Pour repartir de zéro, cette commande ne touche que le projet de dev. Elle supprime ses volumes `ifri-admissions_postgres-data`, `ifri-admissions_redis-data` et `ifri-admissions_minio-data` : brouillons, documents et soumissions de dev ; les images restent. Depuis la racine du dépôt :

```sh
docker compose -p ifri-admissions down -v
docker compose -p ifri-admissions up -d --build --wait
docker compose -p ifri-admissions exec -T api db up
docker compose -p ifri-admissions exec -T api db seed
```

Le seed fixera alors `numbering_year` à l'année courante. Créer ensuite le premier super-administrateur avec `docker compose exec api api admin create-super-admin --email … --name …`.

## Limites et points ouverts

- **IP cliente** : c'est l'adresse du socket. En développement, toutes les requêtes viennent du proxy Vite. Les proxys de confiance et les en-têtes de production (Nginx, HSTS) relèvent de la phase 10.
- **Égalité des temps de réponse** : prouvée par le **nombre** de calculs argon2 (un par tentative), pas par une mesure de durée, trop instable en test.
- **Navigateurs** : seul Chromium est testé, pas Firefox ni Safari. Aucun lecteur d'écran réel n'a été testé.
- **Rotation de `ADMIN_SECRETS_KEY`** : préparée (`key_version`), pas automatisée.
- **Emails** : aucun envoi ; liens d'activation et de réinitialisation transmis à la main. Il n'existe pas de réinitialisation de mot de passe en libre-service.
- **Questions pour l'IFRI** (ROADMAP, questions 11 et 12) : conservation du journal et des IP pseudonymisées ; réinitialisation par email ; second facteur matériel.
- **Hors périmètre** (phases 8 et 9) : consultation des dossiers, revue, classement.
