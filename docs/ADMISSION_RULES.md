# Référentiels d’admission — phase 2

Source : matrice explicitement fournie par l’utilisateur pour la phase 2, le 12 octobre 2026. Aucun PDF officiel/version de communiqué n’a été ajouté au dépôt. Ces données sont utilisables comme référence de développement, pas comme campagne officielle de production.

## Matrice chargée dans la fixture DEV_LOCAL

| Filière | Qualification | Matières dans l’ordre |
|---|---|---|
| IA, SI, GL, IM, SEIOT | BAC_C, BAC_D | MATH ; FRENCH ; ENGLISH |
| IA, SI, GL, IM, SEIOT | BAC_E | MATH ; FRENCH ; MANUFACTURING_STUDY |
| SI, GL, IM, SEIOT | DT_IMI | APPLIED_MATH ; FRENCH ; INFORMATION_SYSTEMS_TECH |
| GL, IM | DT_DWM | APPLIED_MATH ; FRENCH ; WEB_SITES_APPS |
| IM | DT_PM | IT_PROJECT ; FRENCH ; EDITING |
| SEIOT | DT_EAP | APPLIED_SCIENCES ; FRENCH ; ELECTRONICS_STUDY |

23 combinaisons éligibles, 69 règles de matières, 5 filières, 7 qualifications, 11 matières. Aucun coefficient, aucune note. Chaque série BAC possède ses propres lignes ; aucune règle générique C/D/E.

Les autres combinaisons sont **non autorisées en l’absence de règle**, motif `RULE_NOT_DEFINED`. Ce n’est pas une affirmation d’inéligibilité officielle définitive. Une règle explicite `eligible=false` donne le motif `INELIGIBLE`, sans matière.

La campagne `DEV_LOCAL` est nommée « DÉMONSTRATION LOCALE — NON OFFICIELLE », année `DEVELOPMENT`, ouverte du 2000-01-01 au 2100-01-01 pour permettre les essais sans modification annuelle. Ces dates sont des fixtures. Tous les quotas sont NULL (non définis), jamais interprétés comme zéro. Aucune sélection n’est implémentée.

## API de lecture

| GET | Paramètres | Réponse 200 |
|---|---|---|
| /api/v1/campaigns/current | campaign facultatif | Objet campagne ouverte |
| /api/v1/programs | campaign, qualification facultatifs | Tableau offres ouvertes, availableSeats nullable ; filtre qualification éligible |
| /api/v1/origin-qualifications | Aucun | Tableau qualifications canoniques activées |
| /api/v1/programs/{programCode}/eligible-qualifications | campaign facultatif | Tableau qualifications autorisées dans la grille publiée |
| /api/v1/eligibility | program et qualification obligatoires ; campaign facultatif | Résolution complète |
| /api/v1/expected-subjects | Identiques | Même résolution complète, évitant une liste vide ambiguë |

Sans `campaign`, seule une campagne `OPEN` à l’instant présent est sélectionnée, intervalle [starts_at, ends_at[. Aucune : 404 `NO_CURRENT_CAMPAIGN`. Plusieurs : 409 `AMBIGUOUS_CURRENT_CAMPAIGN` ; envoyer alors le code explicitement. Une campagne explicite inconnue : 404 `CAMPAIGN_NOT_FOUND` ; hors dates ou non OPEN : 409 `CAMPAIGN_NOT_OPEN`.

Codes : majuscules, chiffres et underscore, début par lettre, longueur 1–64. Paramètre manquant, vide, répété, inconnu, mal encodé ou code invalide : 400 `INVALID_PARAMETERS`. Programme ou qualification inconnu/désactivé : 404 `PROGRAM_NOT_FOUND` / `QUALIFICATION_NOT_FOUND`. Code connu sans règle publiée/offre activée : 200 avec `eligible=false`, `reason=RULE_NOT_DEFINED`, `expectedSubjects=[]`. Refus explicite : 200 avec `reason=INELIGIBLE`. Grille éligible incomplète ou matière désactivée : 409 `RULE_SET_INCOMPLETE`. Erreur technique : 500 `INTERNAL_ERROR`, sans détail SQL/secret.

Les erreurs ont la forme `{ "code": "PROGRAM_NOT_FOUND", "message": "..." }`. Les tableaux vides sont des tableaux JSON, pas null. Les deux endpoints de résolution renvoient campagne, filière, qualification, eligible, reason, ruleSetVersion (numéro nullable) et expectedSubjects. Un sujet possède **exactement code, name, position**. Le code de campagne + code filière + numéro identifient la grille ; aucun coefficient n’est exposé.

Les listes excluent référentiels désactivés et offres sans grille publiée ; aucun endpoint de mutation ni authentification ajouté. Ordre déterministe par code, matières par position. Limite HTTP de résolution : 3 secondes. Les endpoints techniques et CORS existants sont conservés ; GET versionnés autorisés pour l’origine configurée. Vite préserve `/api/v1`, tout en retirant `/api` pour `/health` et `/ready`.

## Chargement et vérification

Le schéma ne contient aucune donnée de seed. Le seed transactionnel est idempotent, sérialisé, accepté uniquement en development/test ; il ne remplace pas une grille publiée. Les grilles sont construites DRAFT, contrôlées, puis publiées et immuables. Une évolution crée une nouvelle campagne/grille ; remplacement d’une version publiée dans la même offre à cadrer avant CRUD administratif.

Voir README pour les commandes. `infra/scripts/references_smoke.py` vérifie l’API réellement démarrée et imprime les réponses GL/DT_DWM et GL/BAC_E. Exécution hôte réussie : les réponses réelles GL/DT_DWM et GL/BAC_E figurent dans PHASE2_VALIDATION.
