# Migrations PostgreSQL

Goose 3.29.0 via un runner Go limité au driver pgx, sans CLI multi-drivers. `00001_references.sql` crée le schéma des référentiels ; `00002_fix_rule_child_guard.sql` corrige la fonction de garde des lignes enfants. Up/Down transactionnels ; la migration déjà appliquée reste inchangée. UUID v4 PostgreSQL `gen_random_uuid()` ; aucune extension requise.

Depuis la racine après construction API :

```powershell
docker compose exec -T api db up
docker compose exec -T api db seed
```

Les migrations sont explicites, jamais exécutées au démarrage HTTP. Un verrou de session PostgreSQL sérialise les migrations. La commande seed utilise une transaction et un verrou distinct ; elle est refusée en production. Référentiels dans `seeds/references.sql`, fixture locale dans `seeds/development.sql`.

`docker compose exec -T api db down` annule la dernière migration et **supprime les tables et leurs données**. Réserver le rollback aux bases de test ou à une opération explicitement préparée. Ne pas modifier une migration déjà appliquée : ajouter une nouvelle version.

`00006_application_submissions.sql` (phase 6) ajoute numéro, snapshot de soumission, drapeaux, audit et immuabilité. Son down **refuse** de s'exécuter s'il existe une soumission, un compteur de numéros, un événement d'audit ou une application non `DRAFT` : il ne détruit jamais de soumission en silence.

`00007_admin_security.sql` (phase 7) ajoute les comptes administrateurs, la MFA, les sessions, les attributions de rôle et le gel de `numbering_year`. Son down refuse de s'exécuter s'il existe un compte administrateur.

Les tests `-tags=integration` créent un schéma temporaire unique sur PostgreSQL réel, appliquent Up, seed deux fois, vérifient contraintes/matrice puis Down/Up ; seul ce schéma est supprimé. Aucun candidat ou document créé.
