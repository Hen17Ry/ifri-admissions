# Phase 5 — validation finale

Phase 5 réalisée. PASS désigne une exécution réelle, sur le poste Codex ou sur l’hôte avec sorties fournies par l’utilisateur. Aucune moyenne, soumission définitive, numéro officiel, administration ou classement ajouté.

| Contrôle | Résultat | Preuve |
|---|---|---|
| Frontend build / typecheck / ESLint / Prettier | PASS | Commandes locales exécutées ; typecheck/lint revalidés après dernier changement de test |
| Vitest | PASS | 26 tests, quatre fichiers ; six étapes, API, documents, lecture seule, confirmation/rejet et autosave puis navigation |
| Go gofmt / go vet / go test | PASS | Exécutions locales et suite Docker réelle fournie par l’hôte |
| PostgreSQL | PASS | Migration 00005 depuis phase 4, base neuve, rollback/réapplication dans schémas isolés ; db up sur API courante |
| Redis | PASS | Enqueue, groupe, consommation, reclaim/replay/ACK dans intégration ; pipeline réel QUEUED → PROCESSING → SUCCEEDED |
| S3 | PASS | Test privé put/get/delete ; lecture réelle du relevé privé par worker via proxy interne Go ; bucket anonyme refusé |
| Python | PASS | uv lock/sync officiels ; pytest Docker : 26 tests en 5,39 s sur Python 3.14.8 ; Ruff check/format locaux |
| Fixtures native/OCR | PASS | 20 fixtures synthétiques ; six textes et six scans nominaux avec valeurs exactes ; négatifs refusés |
| HTTP OCR | PASS | Six couples métier depuis référentiel API ; confirmation/restauration ; rejet, remplacement scan et nouvelle analyse |
| Sécurité | PASS pour contrôles exercés | Session absente 401, stale confirmation 409, route interne absente du port public 404 ; secret interne, validation champs numériques et sujets, résultats immuables, obsolescence testés |
| Docker | PASS | Rebuild complet ; API/worker/PostgreSQL/Redis/MinIO healthy ; init MinIO Exited 0 |
| Playwright | PASS | Deux scénarios en 40,0 s : confirmation/restauration et rejet/remplacement/nouvelle analyse |
| Responsive | PASS pour checkpoints exercés | 375/768/1024/1440 : profil avec erreurs, profil, parcours, filière, documents vide/erreur/complet et vérification ; absence de débordement horizontal ; captures produites |
| Accessibilité | Contrôles ciblés PASS | Focus premier champ invalide, navigation Tab, lecture seule, libellés et états ; pas d’audit exhaustif lecteur d’écran |
| Agrandissement | CSS zoom 200 % PASS | Assertions et captures du récapitulatif ; zoom natif navigateur non testé |
| Git | Vérifié | Modifications et nouveaux fichiers phase 5 présents ; aucun commit ni push |

Les captures du récapitulatif aux quatre largeurs et du zoom CSS mobile ont été inspectées : contenu et nom de fichier long se replient, focus visible, tableau en lecture seule. À 200 % sur 375 px, les mots et cellules se replient fortement : contenu accessible mais confort réduit. L’étape Analyse isolée n’a pas de checkpoint de capture dédié à chaque largeur ; le résultat est contrôlé dans le récapitulatif. Les états processing et succès sont exercés par le parcours réel, sans pourcentage simulé.

## Corrections validées

Timeout TLS GHCR : uv 0.12.3 installé via PyPI et dépendances conservées sous uv.lock ; le build de validation référence directement le service worker pour éviter une ancienne image locale. Fixtures vérifiées avant création d’un brouillon smoke.

Scan BAC_C : Frangais correspond à Français (0,875), mais Anglais (0,8) déclenchait à tort une ambiguïté malgré son score sous le seuil 0,85. La marge de 0,08 ne compare désormais que deux candidats admissibles. Tests de régression : bruit OCR accepté avec preuves et valeurs exactes, deux candidats admissibles proches toujours refusés ; benchmark réel repassé.

Playwright : synchronisation avec réponse réelle d’autosave du programme puis réponse du clic Ajouter les documents. Aucune navigation forcée ni contournement ; les deux scénarios passent.

## Architecture et limites

Analyse liée à document_version_id et snapshot grille/filière/qualification/matières. Rejet d’une version de PDF valable globalement, même après changement académique. Aucun coefficient issu des référentiels. Decimal Python, NUMERIC PostgreSQL et validation rationnelle exacte Go. Callback et traitement idempotents ; ledger des tentatives ; résultats obsolètes historisés mais jamais courants.

Le worker n’a ni accès libre PostgreSQL ni credentials S3 : il lit le PDF exact via API interne protégée sur listener non publié. Redis ne transporte pas de PII candidat. Les preuves conservées sont limitées aux matières et valeurs, avec bbox normalisées origine haut gauche ; pas de texte intégral OCR enregistré/loggué.

Mesures et limites synthétiques : voir OCR_BENCHMARK.md. CI GitHub non validée dans cette phase. Audit production et corpus officiel anonymisé restent requis.

Une validation sur un corpus anonymisé de relevés réels représentant les différents formats BAC/DT est nécessaire avant mise en production.
