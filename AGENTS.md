# IFRI Admissions — instructions aux agents

## Périmètre et sources de vérité
Plateforme de candidature de l’Institut de Formation et de Recherche en Informatique (IFRI), Université d’Abomey-Calavi. Deux espaces : candidat et administration. Développement progressif, par petites phases testables. Le bootstrap et les référentiels de phase 2 sont validés. La phase 3 ajoute uniquement le parcours candidat en brouillon, avec session sécurisée et persistance, sans documents/OCR/soumission finale.
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
Lire CANDIDATE_FLOW et PHASE3_VALIDATION. Autorisation par cookie opaque hashé, jamais UUID fourni ; Origin obligatoire sur mutations JSON ; secrets/PII absents des logs. Application DRAFT seule éditable, profil incomplet permis en brouillon ; aucune soumission ni numéro final. Qualification/filière contrôlées avec référentiels dans une transaction ; invalidation du choix incompatible. Autosave sérialisé et version optimiste. UI UX master inchangé, override candidate-application pour les quatre étapes.
