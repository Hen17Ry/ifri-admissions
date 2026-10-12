# Roadmap progressive

Chaque phase nécessite un accord avant démarrage. Livrer une petite tranche, ses vérifications et ses limites ; ne pas construire l’ensemble du produit en une tâche.

| Phase | Livrable limité | Validation et condition de sortie |
|---|---|---|
| 0 — Fondations (présente livraison) | Règles agents, spécifications, architecture, modèle, OCR prévu, ADR, master UI | Cohérence des documents, invariants et liens ; aucun code applicatif |
| 1 — Bootstrap technique | Vue/TS minimal, Go minimal, worker Python vide, manifests verrouillés, Compose local, README, CI de base | Build Vue, compilation Go, import Python, healthchecks et persistance locale ; aucun moteur OCR ni parcours métier |
| 2 — Campagnes et règles | Migrations et référentiels versionnés, contrats essentiels | Tests matrice par couple et campagne, absence de coefficients configurés, combinaison inconnue bloquée ; grille officielle requise pour activer des admissions |
| 3 — Identité et autorisation | Auth candidat décidée, sessions admin, RBAC/périmètres | Tests accès propriétaire, rôles, IDOR, CSRF, révocation et rate limiting |
| 4 — Brouillon candidat | Identité/origine/filière et sauvegarde ; première interface conforme au master | Validation serveur, reprise, accessibilité clavier et responsive ; aucune extraction réelle |
| 5 — Documents privés | Upload PDF, quarantaine, versions, accès protégé | Tests types trompeurs, limites, refus, accès croisé, remplacement et stockage orphelin |
| 6 — Orchestration OCR | Contrat, outbox, Redis, worker simulé avec résultats synthétiques | Tests doublons, crash, ACK perdu, retry, résultat périmé, erreurs documentaires |
| 7 — Prototype OCR mesuré | Corpus et benchmark natif/OCR, moteur retenu | Exactitude par champ et overlays vérifiés ; seuils fondés sur corpus, licences examinées |
| 8 — Confirmation et moyenne | Résultat en lecture seule, confirmation/refus, formule Go | Tentative de modification via API refusée ; calcul décimal, données absentes bloquées, remplacement invalide confirmation/moyenne |
| 9 — Soumission et vérification | Guards, liste/fiche admin, correction/rejet/audit | Transitions illégales refusées, concurrence, permissions Reviewer/Admin, délais correction décidés |
| 10 — Classement et sélection | Snapshots par campagne/filière, quotas, validation admin | Exclusion non VERIFIED, tri exact, égalités officielles, quota, revalidation avant publication ; aucune décision automatique |
| 11 — Préproduction | Sécurité, accessibilité, sauvegarde/restauration, observabilité et documentation exploitation | Parcours E2E, restauration complète, revue sécurité, conservation validée et critères de performance mesurés |

Les tests de domaine commencent avec les règles réellement introduites ; tests de contrats et d’intégration suivent les frontières risquées. Cette préparation ne prétend pas les avoir exécutés. Revoir le design system via UI UX Pro Max avant chaque interface importante ; overrides au besoin seulement.
