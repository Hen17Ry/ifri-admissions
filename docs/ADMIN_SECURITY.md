# Sécurité de l'espace d'administration — phase 7

Comptes, authentification, double authentification, sessions, permissions et audit de l'espace `/admin`. Décisions : DECISIONS (ADR 095–108). Preuves : PHASE7_VALIDATION. Hors périmètre : consultation des dossiers, revue, classement (phases 8 et 9).

## Comptes

- **Aucune inscription publique.** Il n'existe aucune route de création de compte anonyme.
- **Premier super-administrateur** : `api admin create-super-admin --email … --name …`, sur le serveur uniquement.
  - Le mot de passe est saisi sans écho dans un terminal, ou lu sur la première ligne de l'entrée standard. Il n'est jamais passé en argument ni dans une variable d'environnement.
  - La commande est refusée s'il existe déjà un super-administrateur actif.
  - Audit `ADMIN_SUPER_ADMIN_BOOTSTRAPPED`, acteur SYSTEM.
  - Aucun seed ne crée de compte ni de mot de passe par défaut.
- **Autres comptes** : créés par un super-administrateur (`accounts.manage`), au moins un rôle chacun.
  - Le compte naît `PENDING_ACTIVATION`, sans mot de passe.
  - Le lien d'activation est affiché **une seule fois** au créateur. Il est à usage unique et expire après 72 h (`ADMIN_ACTIVATION_TTL_SECONDS`).
  - Le jeton fait 256 bits ; seul son SHA-256 est stocké.
  - Le jeton voyage dans le **fragment** (`/admin/activation#jeton`) : il n'est jamais envoyé au serveur web, ni journalisé, ni transmis dans un `Referer`.
  - La page lit le fragment, puis l'efface aussitôt de la barre d'adresse et de l'historique (`history.replaceState`). Le jeton n'est envoyé qu'en corps de requête POST.
  - Un nouveau lien révoque le précédent.
- **Désactivation** : jamais de suppression (trigger), un compte désactivé reste dans l'historique et l'audit. La désactivation révoque toutes les sessions et les liens d'activation en attente.
  - On ne peut pas désactiver son propre compte.
  - Il reste toujours au moins un super-administrateur actif : la règle est vérifiée sous verrou consultatif, donc deux désactivations simultanées ne peuvent pas la contourner.
- **Réactivation** : un compte réactivé repasse `ACTIVE`, ou `PENDING_ACTIVATION` s'il n'a jamais défini de mot de passe.

## Mots de passe

- **Hachage** : argon2id, `golang.org/x/crypto/argon2`, au format PHC `$argon2id$v=19$m=65536,t=3,p=2$sel$clé`.
  - Paramètres : 64 Mio de mémoire, 3 passes, 2 voies, sel de 16 octets, clé de 32 octets. C'est la seconde recommandation de la RFC 9106, ramenée à 2 voies.
  - Les paramètres sont inscrits dans chaque empreinte. Une empreinte faite avec d'anciens paramètres est recalculée à la connexion réussie suivante.
  - Au plus **2 calculs argon2 simultanés** par processus, soit 128 Mio au maximum ; les autres attendent une place, dans la limite du délai de la requête.
- **Politique** : 12 à 128 caractères (Unicode normalisé NFC), pas de règle de composition (NIST SP 800-63B).
  - Refus de l'adresse email du compte et de sa partie locale.
  - Refus des mots de passe courants : 46 825 entrées de 12 caractères ou plus, comparées en minuscules.
  - La liste est la réunion de la liste NCSC des 100 000 mots de passe les plus utilisés (issue de Have I Been Pwned) et du top 1 million de xato. Les deux viennent de SecLists, licence MIT, figées au commit `1a7bb912…` et vérifiées par SHA-256.
  - Elle est régénérée de façon reproductible par `infra/scripts/common_passwords.py` et embarquée compressée dans le binaire.
  - Les entrées de moins de 12 caractères sont déjà refusées par la longueur minimale.
- **Changement de mot de passe** : il demande le mot de passe actuel et **révoque toutes les sessions**, y compris la session courante.

## Double authentification (TOTP)

- **Algorithme** : RFC 6238, HMAC-SHA1, pas de 30 s, 6 chiffres, compatible avec toutes les applications d'authentification. Le code est vérifié contre les vecteurs de test de la RFC.
  - Tolérance de **±1 pas** pour la dérive d'horloge.
  - **Anti-rejeu** : le dernier pas accepté est mémorisé, et un code d'un pas égal ou antérieur est refusé, y compris par une autre requête simultanée (mise à jour conditionnelle).
- **Obligatoire par défaut** (`ADMIN_MFA_REQUIRED=true`) ; la valeur `false` est refusée en production. L'enrôlement se fait à la première connexion.
  - Le secret fait 160 bits. Le QR code est généré **en SVG côté Go** (`rsc.io/qr` v0.2.0, BSD).
  - Le QR code n'est servi que pendant l'étape d'enrôlement, en `no-store`, et n'est jamais journalisé. La clé est aussi affichée en texte pour une saisie manuelle.
  - Le secret reste « en attente » jusqu'au premier code valide.
- **Codes de secours** : 10 codes de 50 bits (Crockford base32, `XXXXX-XXXXX`), affichés une seule fois. Seule leur empreinte SHA-256 est stockée.
  - Chaque code sert une fois.
  - La **régénération invalide tous les anciens codes**.
- **Réinitialisation** par un super-administrateur, auditée (`ADMIN_MFA_RESET`, acteur ADMIN). Elle supprime le secret et les codes, révoque les sessions, et impose un nouvel enrôlement.

## Clé `ADMIN_SECRETS_KEY`

- **Génération** : `openssl rand -base64 48`, soit 48 octets aléatoires ; 32 octets au minimum.
  - La clé est stockée hors dépôt, dans le gestionnaire de secrets de l'hébergement en production, ou dans `.env` ignoré par Git en local.
  - Elle n'est jamais préfixée `VITE_` ni exposée au frontend.
  - La clé d'exemple de `.env.example` est refusée en production.
- **Séparation des usages** : la clé maître ne sert qu'à dériver, par HKDF-SHA256, des sous-clés indépendantes, chacune réservée à un seul usage (info `ifri-admissions/admin/<usage>/v<version>`) :

| Sous-clé | Usage unique |
|---|---|
| `totp-aes-256-gcm` | Chiffrement AES-256-GCM des secrets TOTP, données associées liées au compte (un secret recopié sur un autre compte ne se déchiffre pas) |
| `pseudonym-hmac-sha256` | Pseudonymes HMAC des identifiants tentés et des adresses IP : clés de limitation de débit et métadonnées d'audit (domaines séparés `email`, `ip`, `admin`) |
| `csrf-hmac-sha256` | Jeton CSRF dérivé de l'empreinte du jeton de session |

- **Rotation préparée** : chaque secret chiffré porte `key_version`, et `ADMIN_SECRETS_KEY_VERSION` (défaut 1) entre dans la dérivation. Un secret d'une autre version est refusé, jamais deviné.
  - Une rotation future demandera un trousseau qui lise l'ancienne version et rechiffre à la connexion. Ce n'est pas implémenté.
- **Perte de la clé** :
  - les secrets TOTP deviennent indéchiffrables : chaque compte doit être réinitialisé (`api admin reset-mfa`) puis réenrôlé ;
  - les pseudonymes déjà écrits dans l'audit ne peuvent plus être rapprochés de nouvelles valeurs ;
  - les jetons CSRF et les compteurs de limitation repartent à zéro ;
  - les mots de passe (argon2id), les sessions et les codes de secours (SHA-256) ne dépendent pas de cette clé.
- **Fuite de la clé** : il faut la changer, incrémenter la version, réinitialiser toutes les MFA et révoquer les sessions.

## Sessions

- **Séparation** : sessions totalement distinctes de celles des candidats, avec une table `admin_sessions` et un cookie `ifri_admin`. Le cookie candidat `ifri_draft` n'est jamais lu par les routes admin, et inversement : ce cloisonnement est testé.
- **Cookie** : `Path=/api/v1/admin`, `HttpOnly`, `SameSite=Strict`, `Secure` en production, sans `Max-Age` (il est effacé à la fermeture du navigateur). Le préfixe `__Host-` est incompatible avec un chemin limité et n'est donc pas utilisé.
- **Jeton** : 256 bits, seule son empreinte SHA-256 est stockée.
- **Étapes** : `MFA_ENROLLMENT` ou `MFA_VERIFICATION` après le mot de passe, puis `ACTIVE`.
  - Une session de pré-authentification n'ouvre que les routes du second facteur et expire après 10 minutes.
  - **Le jeton change** (rotation) au passage à `ACTIVE`, et une nouvelle connexion remplace la session que tenait le navigateur.
- **Expirations** : inactivité 30 min (`ADMIN_SESSION_IDLE_SECONDS`), durée absolue 8 h (`ADMIN_SESSION_ABSOLUTE_SECONDS`). Une session expirée est révoquée (`EXPIRED`) à sa prochaine présentation.
- **Révocation de toutes les sessions du compte** à :
  - la déconnexion (session courante) ;
  - le changement de mot de passe ;
  - la désactivation ;
  - le changement de rôle ou de périmètre ;
  - la réinitialisation de la MFA.
- **CSRF** : sur toute mutation admin, deux contrôles s'ajoutent à `SameSite=Strict`.
  - L'`Origin` doit être exactement `FRONTEND_ORIGIN` et le corps en JSON.
  - Un **jeton synchroniseur** est envoyé dans l'en-tête `X-CSRF-Token` : c'est un HMAC de l'empreinte du jeton de session, renvoyé par `GET /session` et par les réponses de connexion.
  - Il n'est pas stocké, change à chaque rotation, et la comparaison se fait en temps constant.
  - Le client le garde en mémoire, jamais en `localStorage`.

## Anti-force brute et non-énumération

- **Clés de limitation** : compteurs Redis atomiques (scripts Lua) par **IP** et par **identifiant tenté**, tous deux pseudonymisés par HMAC. Aucune adresse email ni IP n'est écrite en clair dans Redis.
- **Ordre de traitement** : les limites sont vérifiées **avant tout calcul argon2**, y compris avant l'argon2 factice d'un compte inexistant. Des connexions avec des emails inventés ne peuvent donc pas saturer la mémoire : un test prouve qu'une requête bloquée ne déclenche aucun argon2.

| Politique | Tentatives / 15 min | Échecs gratuits | Délai progressif | Verrouillage |
|---|---|---|---|---|
| Connexion par IP | 30 | 10 | 1 s doublé, 30 s au maximum | 15 min |
| Connexion par identifiant | 15 | 3 | 2 s doublé, 60 s au maximum | 15 min dès 10 échecs |
| Second facteur, par compte | 10 | 3 | 2 s doublé, 60 s au maximum | 15 min dès 6 échecs |
| Activation, par IP | 20 | 5 | 1 s doublé, 30 s au maximum | 15 min dès 10 échecs |
| Changement de mot de passe, par compte | 10 | 3 | 2 s doublé, 60 s au maximum | 15 min dès 6 échecs |

- **Réponse bloquée** : 429 `ADMIN_RATE_LIMITED` avec `Retry-After`.
- **Message unique** : « Identifiant ou mot de passe incorrect. », que le compte soit inconnu, en attente d'activation, désactivé, ou que le mot de passe soit faux. Le corps de réponse est identique octet pour octet.
- **Temps de réponse** : un compte inexistant ou inutilisable coûte un argon2 complet sur une empreinte factice, exactement **un calcul** comme pour un compte réel. Le test le vérifie par comptage, plus fiable qu'une mesure de durée.
- **Ce que le verrouillage ne révèle pas** : il s'applique à l'identifiant tenté, existant ou non.
- **IP du client** : adresse du socket (`RemoteAddr`), sans confiance dans `X-Forwarded-For`. Derrière le proxy de développement ou un futur Nginx, tous les clients partagent la même IP. La configuration des proxys de confiance est à faire en phase 10.

## Permissions (RBAC)

Le code vérifie des **permissions**, jamais des noms de rôles. Les rôles sont des constantes Go qui regroupent des permissions ; seules les attributions sont en base (`admin_role_assignments`, historisées, révoquées et jamais modifiées).

Rôles IFRI depuis la phase 8 (ADR 109, organigramme dans ADMIN_REVIEW) ; **un seul rôle par compte** (ADR 110, contrôle Go et trigger `guard_single_role`).

| Rôle | Libellé | Permissions |
|---|---|---|
| SUPER_ADMIN | Administrateur technique | `dashboard.view`, `accounts.read`, `accounts.manage`, `audit.read` — aucun accès aux dossiers |
| REVIEWER | Agent du secrétariat exécutif | `dashboard.view`, `applications.read`, `review.claim`, `review.decide`, `corrections.propose`, `ranking.read` |
| REVIEW_LEAD | Responsable du secrétariat exécutif | celles de l'agent + `review.reassign`, `review.revert` |
| DEPUTY_DIRECTOR | Directeur adjoint | `dashboard.view`, `applications.read`, `corrections.decide`, `ranking.read` |
| DIRECTOR | Directeur | `dashboard.view`, `applications.read`, `audit.read` (lecture seule), `ranking.read`, `selection.validate` |

- `SELECTION_MANAGER` et `AUDITOR` (phase 7) sont retirés : révoqués par la migration 00008, historique conservé, nouvelle attribution refusée.
- **Périmètre** : le mécanisme par filière (`Principal.CanFor`, `Principal.Scope`) est conservé mais aucun rôle IFRI n'est limité à une filière ; l'interface n'en propose plus.
- **Refus par défaut** : toute route admin passe par un registre qui **exige** une permission, ou un marqueur explicite (`public`, `session.any`, étapes du second facteur, `self.manage`).
  - Une route sans permission ne peut pas être enregistrée.
  - Un test parcourt toutes les routes et vérifie, pour chacune et pour chaque rôle, la réponse 401 sans session, 403 sans permission, et l'accès avec permission.
  - Une ressource admin inconnue renvoie 404.
- **Ordre du garde** :
  1. Origin et JSON sur les mutations ;
  2. session ;
  3. étape ;
  4. CSRF ;
  5. permission.

## Audit

- **Table** : `audit_events` (ADR 089), acteur `ADMIN` avec l'identifiant du compte, ou `SYSTEM` pour la CLI et pour les tentatives non authentifiées. La cible est `ADMIN_USER`, le cas échéant.
- **Actions sensibles tracées** :

| Domaine | Événements |
|---|---|
| Connexion | connexion réussie (avec le second facteur utilisé) ; mot de passe accepté en attente du second facteur ; connexion échouée (`identifierHash`, `ipHash`, motif interne) ; verrouillage |
| Session | déconnexion |
| Double authentification | MFA enrôlée ; MFA refusée ; MFA réinitialisée (ADMIN ou SYSTEM) ; code de secours utilisé ; codes régénérés |
| Mot de passe | mot de passe changé ; changement de mot de passe refusé ; mot de passe réinitialisé (CLI, SYSTEM) |
| Comptes | compte créé (rôles) ; lien d'activation émis ; compte activé ; lien d'activation invalide ; compte désactivé ; compte réactivé ; rôles ou périmètre modifiés (ajouts et retraits) ; premier super-administrateur créé |

- **Contenu des métadonnées** : jamais d'email tenté, de mot de passe, de code, de jeton, ni d'adresse IP en clair. L'identifiant tenté et l'IP n'y figurent qu'en **HMAC**, avec la sous-clé de pseudonymisation. Un test parcourt toutes les métadonnées à la recherche des valeurs en clair.
- **Rétention** : la durée de conservation de ces données personnelles pseudonymisées est une question ouverte pour l'IFRI (ROADMAP).
- **Lecture** : `audit.read` (super-administrateur, auditeur), en lecture seule. Filtres par type, acteur, période et cible, pages successives (keyset) de 50 événements.
- **Traçage des consultations** : `audit.RecordAccess(ctx, tx, adminID, "APPLICATION", id, purpose)` écrit `ADMIN_DATA_ACCESSED` dans la transaction qui lit les données. La phase 8 l'appellera à chaque ouverture de dossier.

## En-têtes de sécurité

- **API** `/api/v1/admin/*` : toutes les réponses portent ces en-têtes.
  - `Cache-Control: no-store`, `Pragma: no-cache`.
  - `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'`.
  - `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`.
  - `Cross-Origin-Opener-Policy` et `Cross-Origin-Resource-Policy: same-origin`, et une `Permissions-Policy` restrictive.
  - `Strict-Transport-Security` en production.
- **SPA `/admin`**, servie par Vite en développement : un middleware applique `X-Frame-Options: DENY`, `no-store`, `Referrer-Policy`, `COOP`, `Permissions-Policy` et cette CSP :

```
default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:;
font-src 'self'; connect-src 'self' ws://localhost:5173; object-src 'none'; base-uri 'none';
form-action 'self'; frame-ancestors 'none'
```

`'unsafe-inline'` sur les styles et le websocket ne servent qu'au client Vite de développement. Politique de production, à appliquer par Nginx en phase 10 et déjà appliquée par `vite preview` :

```
default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self';
connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'
```

plus `Strict-Transport-Security`. `img-src data:` sert uniquement au QR code d'enrôlement.

## Procédures de secours (accès au serveur requis)

Ces commandes ne sont pas exposées par HTTP. Elles s'exécutent dans le conteneur de l'API avec la configuration de production et sont auditées comme `SYSTEM` (`metadata.via = "CLI"`).

- **Dernier super-administrateur sans téléphone ni codes de secours** :
  1. vérifier l'identité de la personne hors du système ;
  2. `docker compose exec api api admin reset-mfa --email <email>` : supprime le secret et les codes, révoque les sessions ;
  3. la personne se connecte avec son mot de passe et se réenrôle.
- **Compte désactivé par erreur, y compris le dernier super-administrateur bloqué par un tiers** : `api admin reactivate --email <email>`.
- **Mot de passe oublié**, y compris celui du dernier super-administrateur : `api admin reset-password --email <email>`.
  - Elle efface l'empreinte du mot de passe, repasse le compte en `PENDING_ACTIVATION` et révoque ses sessions.
  - Elle imprime un lien d'activation à usage unique (72 h), que l'opérateur transmet par un canal sûr.
  - La double authentification est conservée : la personne se reconnecte ensuite avec son application.
  - Audit `ADMIN_PASSWORD_RESET`, acteur SYSTEM.
  - Il n'existe aucune réinitialisation en libre-service ni par l'interface web : un flux par email est à décider avec l'IFRI (TODO).

## Limites

- Aucun envoi d'email : le lien d'activation est transmis par le créateur, par un canal sûr de son choix.
- La limitation par IP voit l'adresse du proxy en développement. En production, il faudra configurer des proxys de confiance (phase 10).
- Les compteurs de limitation sont dans Redis : une panne de Redis rend la connexion indisponible (503) plutôt que de la laisser sans limite.
- WebAuthn et les clés de sécurité ne sont pas implémentés ; TOTP est le seul second facteur.
- La rotation de `ADMIN_SECRETS_KEY` est préparée par la version de clé, mais pas automatisée.
