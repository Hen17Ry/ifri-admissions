# IFRI Admissions — instructions aux agents

## Périmètre et sources de vérité
Plateforme de candidature de l’Institut de Formation et de Recherche en Informatique (IFRI), Université d’Abomey-Calavi. Deux espaces : candidat et administration. Développement progressif, par petites phases testables. Le bootstrap et les référentiels de phase 2 sont validés. La phase 3 ajoute le parcours candidat en brouillon ; la phase 4 ajoute des PDF privés versionnés, sans OCR ni soumission finale.
Lire `docs/PRODUCT_SPEC.md`, `docs/ARCHITECTURE.md`, `docs/DATA_MODEL.md` et `docs/DECISIONS.md` avant de modifier le projet. Suivre `docs/ROADMAP.md` et `docs/TODO.md`. Les instructions explicites de l’utilisateur priment ; ne commencer aucune phase suivante sans son accord.

## Invariants métier non négociables
- Les matières de classement dépendent du couple **diplôme/série d’origine + filière demandée**, dans une campagne et une version de grille précises.
- `ExpectedSubjectRule` indique uniquement les matières attendues. Aucun coefficient de calcul dans les référentiels, règles ou configuration administrative.
- **Notes ET coefficients proviennent du PDF du candidat**. Aucune valeur par défaut, aucune déduction de coefficient depuis la série. Une valeur absente ou ambiguë bloque le calcul.
- Le candidat consulte les extractions en lecture seule ; il confirme ou refuse, sans modifier notes ni coefficients, y compris via API.
- Un refus impose un nouveau PDF ; après plusieurs échecs, prévoir une vérification manuelle tracée, selon une politique à définir.
- Go calcule `somme(note × coefficient) / somme(coefficients)` avec arithmétique décimale et données complètes, cohérentes, acceptées. Pas de classement sur les arrondis d’affichage.
- Remplacer le relevé ou changer série/filière invalide analyse active, confirmation, moyenne et toute proposition de classement concernée. Aucun résultat ancien ne réactive un dossier.
- Seuls les dossiers administrativement `VERIFIED` et admissibles participent au classement final, par campagne et filière. Quota = proposition ; sélection finale = validation administrative explicite.
- Ne jamais inventer la grille officielle, les admissibilités, les départages ou les seuils OCR. Une combinaison inconnue reste bloquée avec un motif explicite.
- États OCR, dépôt, administration et sélection distincts ; transitions explicites, gardées, transactionnelles et auditées. Aucun booléen unique « validé ».

## Stack et architecture
Vue 3, TypeScript strict, Vite, Vue Router, Pinia, Tailwind CSS, shadcn-vue/Reka UI selon besoin, `lucide-vue-next`. **Aucune dépendance React**.
Go : monolithe modulaire ; Python : worker documentaire séparé ; PostgreSQL : vérité métier ; MinIO/S3 : PDF privés ; Redis : queue/cache ; Nginx : reverse proxy ; Docker Compose : environnement local.
Versions retenues dans les manifests et `docs/DECISIONS.md` ; preuves bootstrap dans `docs/BOOTSTRAP_VALIDATION.md`, preuves des référentiels dans `docs/PHASE2_VALIDATION.md`. Ne pas adopter les exemples React ou anciennes configurations Tailwind d’un skill sans adaptation.
Modules Go : identité, campagnes/référentiels, candidatures, documents, analyses, vérification, classement, audit. Dépendances transport → cas d’usage → domaine ; adaptateurs implémentent les ports. Pas de microservices supplémentaires ni de framework abstrait sans besoin concret.
Go possède les écritures métier PostgreSQL. Python ne décide ni admissibilité ni moyenne ni sélection. Contrats OCR versionnés ; jobs idempotents ; provenance immuable ; migrations SQL versionnées lors de leur introduction.

## Sécurité
Contrôle serveur de propriété du dossier, rôle et périmètre campagne sur chaque ressource et action ; identifiants opaques ne suffisent pas contre IDOR.
HTTPS en production ; sessions opaques révocables, cookies HttpOnly/Secure/SameSite, CSRF ; mots de passe hashés avec algorithme spécialisé ; rate limiting, MFA administrative à prévoir.
PDF uniquement : limites taille/pages/temps, signature et parsing contrôlés, antivirus/quarantaine, rejet des fichiers chiffrés ou dangereux ; OCR isolé sans réseau sortant ni privilèges. Jamais de document public permanent.
Secrets hors dépôt, moindre privilège, journaux sans contenu sensible, audit d’accès et de modification, sauvegardes chiffrées et restauration testée. Conservation et suppression à définir avant production.

## UI/UX obligatoire
Avant chaque interface importante : lire `.agents/skills/ui-ux-pro-max/SKILL.md`, rechercher les recommandations du produit et de la stack Vue, puis lire `design-system/ifri-admissions/MASTER.md` et l’éventuel `pages/<page>.md`. Persister toute dérogation motivée ; ne pas régénérer le master aveuglément.
Style institutionnel, moderne, clair, rassurant ; marine, surfaces claires, hiérarchie et espace. Pas de dégradé violet générique, animation décorative ou template commercial.
21st.dev : inspiration de composition uniquement, réimplémentation Vue et contrôle des licences avant toute reprise d’asset. Icônes Lucide Vue uniquement.
Clavier, focus visible non masqué, labels, erreurs liées aux champs et résumé focalisable, contrastes WCAG 2.2 AA, `prefers-reduced-motion`, états chargement/vide/erreur, responsive 375/768/1024/1440 px. Ne jamais présenter une confiance OCR comme une certitude.

## Méthode de travail
Inspecter l’existant et les instructions locales ; limiter les changements à la phase autorisée. Documenter décisions et hypothèses, ne pas créer des répertoires vides par anticipation. Tests adaptés aux invariants et risques réels, sans données personnelles réelles dans le dépôt. Vérifier les contrats interlangages, concurrence et droits lors de leur implémentation. À chaque livraison : fichiers changés, vérifications, limites et prochaine étape proposée ; attendre l’accord avant celle-ci.

## Référentiels de phase 2
Migrations Goose explicites, seed canonique séparé de DEV_LOCAL non officiel, seed jamais en production. Consulter `docs/ADMISSION_RULES.md` pour matrice et contrat HTTP. Qualification canonique OriginQualification ; quotas nullable par CampaignProgram. Grilles versionnées par offre, publiées immuables ; règles inconnues non autorisées sans inventer de refus officiel. Ne pas ajouter de coefficients. Tests PostgreSQL isolés via profil Compose test ; aucune suppression de volume local pour tester.

## Brouillons de phase 3
Lire CANDIDATE_FLOW et PHASE3_VALIDATION. Autorisation par cookie opaque hashé, jamais UUID fourni ; Origin obligatoire sur mutations JSON et multipart ; secrets/PII absents des logs. Application DRAFT seule éditable, profil incomplet permis en brouillon ; aucune soumission ni numéro final. Qualification/filière contrôlées avec référentiels dans une transaction ; invalidation du choix incompatible. Autosave sérialisé et version optimiste. UI UX master inchangé, override candidate-application pour les cinq étapes actives de phase 5.1.

## Documents de phase 4

Lire DOCUMENT_WORKFLOW et PHASE4_VALIDATION. Codes et libellés côté API ; session → candidature → document courant, aucun accès UUID public. SDK S3 confiné au stockage, qpdf système sans OCR, secrets du compte S3 limité hors dépôt. Anciennes versions conservées ; analyse future liée à document_version_id. Aucun scanner antivirus implémenté dans cette phase : production exige décision de quarantaine/scanner. Ne pas déclarer S3/DB/navigateur validés sans sorties réelles.

## Phase 5 — analyse en validation

Lire OCR_PIPELINE et PHASE5_VALIDATION. Matières exclusivement snapshot de grille publiée ; note et coefficient exclusivement preuves du PDF, jamais defaults. PyMuPDF/Tesseract derrière port moteur, callbacks Go internes 8081, tentative + contexte + document_version contrôlés. Confirmation distincte, lecture seule, rejet impose nouvelle version. Aucun calcul de moyenne, soumission, numéro final ou admin dans cette phase. Mesures synthétiques ≠ validation de précision sur corpus réel.


## Phase 5.1 — corrective autorisée

Analyse automatique à l’upload/remplacement, intégrée à Documents ; aucun bouton OCR ni étape séparée. Garder triggers et historique validés. Lire PHASE5_1_VALIDATION. Profils de layout séparés des règles d’admission ; BAC général premier groupe écrit, hypothèse documentaire à confirmer officiellement. Coefficient retourné = sa propre cellule, jamais dérivé ; note acceptée selon la règle par matière attendue ci-dessous (ADR 079–081). Private-fixtures/Relever.pdf et oracle local ignorés Git et contextes Docker ; pas de PII du corpus réel dans images/logs/tests publics. La phase 6 est décrite plus bas.

Instruction explicite courante : BAC général scanné lu par cellules, acceptation arithmétique contrôlée et indépendante en Go, confiance Tesseract conservée en diagnostic. Le seuil Tesseract n'est plus décisif pour ce profil lorsque les preuves complètes passent. NEEDS_REVIEW est un motif de revue documentaire, pas une ouverture du module d'administration.

Règle BAC en vigueur (décision utilisateur du 14 octobre 2026, ADR 079–082), pour les matières attendues seulement : note lue = Points ÷ Coeff → acceptée ; note illisible et Points ÷ Coeff validé par les totaux (sommes exactes, autres rangées confirmées par leur note lue) → acceptée, origine POINTS_TOTALS ; note lisible mais différente → GRADE_POINTS_MISMATCH. Les autres rangées ne servent qu'aux totaux. Une seule cellule illisible par colonne Coeff/Points/Sur peut être reconstruite depuis le Total (RECONSTRUCTED_FROM_TOTAL), puis confirmée par sa rangée ; jamais retournée comme coefficient ni seule source d'une note. Moyenne affichée : troncature ou arrondi à 2 décimales. Go revérifie tout. Coeff reste exclusivement sa cellule, jamais dérivé depuis Sur. RapidOCR lit les cellules numériques (OCR_RAPIDOCR_ENABLED), Tesseract le reste ; série = grande lettre isolée en haut à gauche. Phase 5.1 clôturée ; corpus réel ≥ 10 relevés requis avant la production.

## Phase 6 — soumission définitive

Lire SUBMISSION_WORKFLOW et PHASE6_VALIDATION. Soumission en une transaction (verrou application et session, revalidation complète en base, calcul, numéro, snapshot, drapeaux, audit, `SUBMITTED`) ; un dossier déjà soumis rejoue le même récépissé avant tout contrôle de version. Moyenne = fraction exacte (`weighted_sum` / `coefficient_sum`), plus une colonne à 30 décimales tronquée et `average_is_exact`, jamais arrondie, jamais exposée au candidat ; le classement comparera les fractions par produit en croix (ADR 086). Numéro `IFRI-{numbering_year}-{PROGRAMME}-{SERIAL6}` par compteur (année, filière), sans trou en pratique sans garantie contractuelle. Snapshot, drapeaux, audit et dossier soumis immuables en base ; ne jamais assouplir ces triggers : les statuts administratifs futurs vont dans une table séparée et historisée, une réouverture sera un événement audité (ADR 088). `audit_events` est générique (`actor_type`, `actor_id`) et ne contient aucune PII. Après soumission : 409 `APPLICATION_LOCKED` partout, sauf `GET …/current` et `GET …/current/submission`. Drapeaux non bloquants. Repli manuel préparé seulement (ADR 092). Aucune administration, classement, notification ni PDF de récépissé dans cette phase.

## Phase 7 — administration

Lire ADMIN_SECURITY et PHASE7_VALIDATION. Aucune inscription publique ; premier super-administrateur et procédures de secours par `api admin …` (serveur seulement, audit SYSTEM), jamais par un seed. Le code vérifie des permissions, jamais des noms de rôles ; toute route admin passe par `admin.Router.Handle` avec une permission (refus par défaut, test de matrice). Limitation de débit avant tout argon2 ; message de connexion unique ; identifiants tentés et IP en HMAC seulement. Sous-clés HKDF à usage unique dérivées de `ADMIN_SECRETS_KEY` ; ne jamais réutiliser une sous-clé pour un autre usage. Sessions admin et candidat cloisonnées. Chaque consultation de données personnelles en phase 8 passe par `audit.RecordAccess`. Playwright tourne sur la stack jetable `make e2e`, jamais sur la base de développement. Interface : UI UX Pro Max et `pages/admin.md`.

## Phase 8 — back-office des dossiers soumis

Lire ADMIN_REVIEW et PHASE8_VALIDATION. Rôles IFRI, **un seul par compte** (Go + trigger `guard_single_role`) : agent et responsable du secrétariat exécutif, Directeur adjoint, Directeur (`audit.read` en lecture seule), administrateur technique sans accès aux dossiers. Revue dans `application_reviews` + `application_review_events` (une version = un événement), jamais dans `applications` ni dans le snapshot. Seul l'agent en charge valide, rejette (motif fermé de `review_rejection_reasons` + commentaire) ou propose une correction ; seul le Directeur adjoint décide une correction, jamais son auteur ; validation bloquée par une correction en attente. Moyenne effective = fraction exacte depuis le snapshot + dernières corrections approuvées, revérifiée par trigger ; **la phase 9 classe dessus**. Toute écriture de revue verrouille la ligne, vérifie version, agent et gel (`ranking_frozen`), écrit la version, l'événement et l'audit dans la même transaction. `audit.RecordAccess` à chaque dossier, document et preuve. Preuves OCR : file `ifri:ocr:proofs:v1` et boucle de worker séparées, version exacte du document et coordonnées stockées, image privée servie par l'API. Seule la route de contenu PDF peut être encadrée (`frame-ancestors 'self'`). Données synthétiques : `api dev synthetic-applications --confirm`, jamais en production. Aucun classement, sélection, export ni notification dans cette phase.
