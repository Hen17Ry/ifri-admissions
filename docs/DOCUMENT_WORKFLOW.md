# Documents de candidature — phase 4

## Flux

Upload authentifié → fichier temporaire borné → validation PDF → SHA-256 → objet S3 privé → transaction PostgreSQL → version courante → prévisualisation authentifiée.

Quatre codes applicatifs stables : REQUEST_LETTER, TRANSCRIPT, IDENTITY_DOCUMENT, PARENTAL_COMMITMENT. Le serveur fournit leurs libellés français et leur caractère obligatoire. Aucun OCR, extraction, soumission ou numéro officiel.

## Validation et limites

DOCUMENT_MAX_BYTES fixe la limite par fichier : 10485760 octets (10 MiB) en développement ; valeur explicite requise en production, plage 1 KiB–50 MiB. GET documents expose maxBytes pour l’interface. Multipart contient un seul champ file ; extension .pdf, MIME application/pdf, signature %PDF-, contenu non vide et taille contrôlés côté serveur. Fichier temporaire privé supprimé après traitement, deux validations/envois simultanés maximum par processus, délai requête 60 secondes.

qpdf est un prérequis système de l’API, installé dans l’image Docker. --check doit réussir sans avertissement ; les PDF chiffrés sont refusés. L’inspection des objets JSON refuse JavaScript, actions automatiques, lancement, pièces jointes et contenu riche. Le parseur dispose d’un délai de 10 secondes et d’une sortie JSON bornée. Ce contrôle structurel ne constitue pas un antivirus. Scanner/quarantaine, limites de ressources renforcées, politique de pages et conservation restent à définir avant exposition en production.

Le nom original est nettoyé (chemins, caractères de contrôle, longueur) pour affichage et Content-Disposition ; il ne construit jamais la clé S3. Le serveur génère application UUID + type fixe + version UUID. SHA-256 est stocké, aucune clé/URL/empreinte de session n’est exposée par les DTO.

## Cohérence et versions

ApplicationDocument est unique par candidature/type. DocumentVersion possède un numéro positif unique, une clé S3 unique, une empreinte SHA-256 et replaced_at. Une FK composite différée et des triggers différés assurent que le pointeur courant appartient au document et désigne sa seule version non remplacée.

Chaque envoi crée une version ; remplacement = ancienne version marquée remplacée et conservée, nouvelle version courante. Verrou transactionnel du brouillon + X-Application-Version évitent les remplacements concurrents : 409 exige un rechargement. Le choix de filière et l’état DRAFT restent contrôlés par la session existante ; campagne fermée interdit les mutations.

S3 précède la transaction DB. Échec storage ou DB certain : tentative de suppression de l’objet nouvellement créé, avec contexte de nettoyage indépendant borné à 5 secondes. Commit dont le résultat est indéterminé : objet conservé pour éviter de supprimer un objet éventuellement référencé. Échec de nettoyage journalisé sans secret ; réconciliation/garbage collection à prévoir. Aucun effacement de l’ancienne version utile. Une future analyse OCR référencera document_version_id ; aucune table OCR ajoutée.

## Stockage et accès

Port Go Objects (Put/Get/Delete) indépendant du fournisseur ; SDK MinIO uniquement dans l’adaptateur S3. MinIO local, AWS S3/R2/Garage possibles avec endpoint, région et credentials compatibles. Initialiseur local crée un utilisateur API distinct du compte racine ; policy limitée à GetObject/PutObject/DeleteObject sous applications/* du bucket privé. Aucune ACL publique.

Prévisualisation via proxy Go, après session → candidature → document courant. Aucun endpoint UUID public ni URL présignée. Content-Type application/pdf, nosniff, no-store, Content-Disposition inline ; route download impose attachment. CSP sandbox et Referrer-Policy no-referrer. ServeContent permet Range pour le lecteur PDF natif ; ouverture dans un nouvel onglet, sans moteur PDF frontend.

## Contrat HTTP

Préfixe : /api/v1/applications/drafts/current/documents.

| Méthode / suffixe | Fonction |
|---|---|
| GET / | Quatre documents, métadonnées courantes, complete, maxBytes, applicationVersion |
| POST /{documentType} | Ajouter/remplacer ; multipart file + X-Application-Version ; 201 avec liste actualisée |
| GET /{documentType}/content | PDF courant inline, session obligatoire |
| GET /{documentType}/download | PDF courant en téléchargement, session obligatoire |

Mutation : Origin strictement FRONTEND_ORIGIN, cookie HttpOnly, credentials CORS explicites. Erreurs sûres au format du module drafts : 400 paramètres/type/version, 401 session absente/expirée/révoquée, 403 Origin, 404 document absent dans ce dossier, 409 conflit/campagne fermée, 413 taille, 415 multipart requis, 422 PDF invalide/chiffré/dangereux, 500 erreur interne masquée.

## Interface

Cinq étapes : informations → parcours → filière → documents → vérification. Chaque type a son propre sélecteur natif accessible, état d’envoi réel et erreur locale. Aucun pourcentage simulé ni drag-drop obligatoire. Le document courant reste visible pendant un remplacement jusqu’à acquittement. Continuer relit la complétude serveur et exige complete=true ; refresh restaure via le même cookie. Aucun secret dans localStorage.

Tests synthétiques exclusivement. documents_smoke.py laisse un brouillon de développement et ses versions, sans soumission ; tests d’intégration utilisent un schéma temporaire et nettoient uniquement leurs objets.
