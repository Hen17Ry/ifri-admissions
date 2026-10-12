# Validation phase 4 — 2026-10-12

Phase 4 validée pour le périmètre local demandé : backend, stockage, HTTP et parcours navigateur exécutés avec succès. PASS signifie commande réellement exécutée ; NON TESTÉ indique absence de preuve fonctionnelle ; FAIL identifie une commande échouée.

| Contrôle | Résultat | Preuve / limite |
|---|---|---|
| Installation dépendances | PASS | go mod tidy et npm install exécutés sur l’hôte ; go.sum/package-lock officiels |
| Build Vue, typecheck, ESLint, Prettier | PASS | Commandes npm exécutées localement |
| Vitest | PASS | 20 tests, trois fichiers ; wizard et documents |
| go test ./..., go vet ./... | PASS | Modules documents/drafts/references et bootstrap |
| Compilation intégration Go | PASS | -tags=integration -run '^$' ; aucune exécution DB/S3 revendiquée |
| pytest, Ruff worker | PASS | 10 tests ; check et format |
| Ruff scripts smoke | PASS | Check et format |
| Configuration Compose normale et profil test | PASS | docker compose config --quiet |
| Accès Docker depuis Codex | FAIL | Socket Docker : permission denied ; aucun conteneur nouveau validé ici |
| qpdf réel, PDF chiffré et actions actives | PASS | TestRealQPDF exécuté dans le conteneur sur l’hôte |
| PostgreSQL migration 00004, contraintes, rollback/réapplication | PASS | db up et TestPostgresDocuments ; suites drafts/references également PASS sur l’hôte |
| S3 put/get/delete, policy privée, historique | PASS | TestS3PrivateRoundtrip et TestPostgresDocuments sur MinIO réel |
| Scénario HTTP documents | PASS | Quatre PDF, complete=true, remplacement version 2, preview, 401/404/409/403 et bucket privé : sortie hôte |
| Rebuild / cinq services healthy | PASS | Sortie hôte docker compose ps : cinq healthy ; minio-init Exited (0) |
| Démarrage de la suite Playwright dans Codex | FAIL | webServer exit 1 ; Vite confirme listen EPERM sur 127.0.0.1:5173 |
| Parcours navigateur sur l’hôte | PASS | Dernière exécution : 1 passed (19.0s), scénario 14.9s ; création, saisie, upload, remplacement, preview, récapitulatif et restauration |
| PDF preview dans le navigateur | PASS | Clic réel + réponse PDF authentifiée + octets exacts ; rendu natif confirmé par capture utilisateur |
| Responsive 375/768/1024/1440 et zoom CSS 200 % | PASS | Assertions sans overflow et captures ; Documents/review inspectés visuellement aux largeurs demandées |
| Clavier/focus/erreurs/fichier long exercés par le scénario | PASS | Focus première erreur et Tab, champs natifs et fichiers longs exercés ; aucun audit lecteur d’écran exhaustif |
| CI GitHub | NON TESTÉ | Hors validation locale, aucune réussite revendiquée |

## Sécurité testée localement

Tests unitaires : absence/expiration de session, Origin, faux PDF, taille, nom nettoyé, clé serveur, SHA-256, contrôle DRAFT, conflits, complétude, nettoyage simulé sur échec storage/DB, conservation sur commit indéterminé, en-têtes PDF. Tests intégration prévus : IDOR entre deux sessions, versions conservées et pointeur courant, bucket anonyme interdit. Une simulation unitaire ne remplace pas la preuve S3/PostgreSQL réelle.

## Commandes hôte exécutées avec succès (racine)

```sh
docker compose up -d --build --wait --wait-timeout 240 api
docker compose exec -T api db up
docker compose --profile test run --build --rm references-tests
uv run --project services/ocr --locked --no-sync python infra/scripts/smoke.py
uv run --project services/ocr --locked --no-sync python infra/scripts/documents_smoke.py
docker compose ps --all
```

## Validation navigateur restante

Chromium installé sur l’hôte. Dans apps/web :

```sh
npm run test:browser
```

Le navigateur utilise l’API réelle et la campagne locale DEV_LOCAL ; aucune API mockée dans cette suite. Captures ignorées par Git sous apps/web/browser-artifacts. Ne pas publier les cookies, documents personnels ou secrets. Le rapport sera actualisé à réception des sorties et après inspection des captures. Aucune étape OCR autorisée.

Diagnostic hôte suivant : HTTP 504 sur /node_modules/.vite/deps/{pinia,vue,vue-router}.js. Playwright démarre maintenant son propre serveur avec --force ; arrêter le serveur Vite existant sur 5173 avant test:browser. Pas de retry ajouté ni de contrôle supprimé.

Nouvelle exécution navigateur : chargement initial corrigé, uploads et remplacement atteints ; timeout sur load du viewer PDF natif. Test ajusté pour vérifier commit de navigation, réponse PDF réelle et contenu authentifié exact sans attendre un événement HTML inadapté. Vérification/rechargement et zoom restent à exécuter.

Captures documents-complete inspectées visuellement à 375/768/1024/1440 : aucun débordement visible, nom long replié, actions et aide lisibles ; mise en page une colonne mobile et aide latérale desktop. Récapitulatif et zoom non encore atteints. Locale navigateur fr-FR configurée pour les contrôles natifs.

Exécution suivante : popup PDF avec URL vide côté Playwright ; waitForURL ne convient pas non plus au viewer natif dans cet environnement. Test corrigé : popup réel + réponse de navigation déclenchée par le clic (200/206, PDF inline), puis requête avec même session et vérification exacte des octets. Aucun timeout augmenté, aucun statut d’erreur ignoré. Le rendu visuel interne du lecteur PDF reste NON TESTÉ jusqu’à confirmation manuelle.

Preview visuelle : PASS confirmé par capture utilisateur du lecteur PDF sur la route protégée (aucune donnée du document recopiée dans le dépôt). Parcours navigateur atteint également vérification et restauration. Zoom 200 % à 375 px : FAIL, titre long et stepper débordent ; correction du reflow (retour des mots, header flexible, stepper très étroit) en attente de réexécution.

## État final

Dernière sortie navigateur fournie par l’utilisateur : 1 passed (19.0s). Les échecs Vite/attentes PDF et le débordement au zoom décrits ci-dessus sont historiques et corrigés. Captures review-zoom200-375/768 et review-1024/1440 inspectées : pas de contenu tronqué ; les mots se replient à très faible largeur effective, le focus du titre est visible. Zoom simulé par CSS zoom=2, pas une validation indépendante du zoom natif de tous les navigateurs. Pas d’audit exhaustif WCAG/lecteur d’écran, ni Safari/Firefox. Les contrôles locaux et hôte réussis ne constituent pas une certification de production.

Aucun OCR, soumission, numéro officiel ou espace administrateur développé. Scanner/quarantaine, réconciliation, conservation et CI restent futurs. Aucun commit/push effectué.
