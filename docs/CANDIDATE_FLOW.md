# Parcours candidat — phases 3 à 5

## Périmètre

Route `/candidature`, layout candidat distinct, six étapes : informations personnelles → parcours académique → filière éligible → documents → analyse du relevé → vérification du brouillon. Le récapitulatif ne soumet rien et n’attribue aucun numéro officiel. Aucun OCR, moyenne, classement, notification ou authentification de compte.

## Persistance et navigation

Au chargement, GET du brouillon courant via cookie. Session absente : lire la campagne ouverte et proposer « Commencer mon brouillon », sans création silencieuse. Session valide : restaurer depuis PostgreSQL, conserver le même UUID et reprendre la première étape incomplète ou le récapitulatif. Campagne fermée : état dédié sans formulaire éditable ; GET du brouillon reste lisible pour déterminer sa campagne, la mutation est refusée.

Création transactionnelle Candidate + Application DRAFT + DraftSession. Les champs du profil peuvent rester vides dans un brouillon incomplet ; les valeurs non vides sont validées. Tous les champs doivent être remplis pour avancer à l’étape 2 dans le wizard ; cette validation de complétude ne constitue pas une soumission.

Autosave après pause de 800 ms pour une section valide, requêtes sérialisées, version optimiste envoyée avec chaque PATCH. Continuer attend l’enregistrement avant d’avancer. Retour conserve les valeurs en mémoire ; aucune PII dans localStorage/sessionStorage. « Modifications non enregistrées » et avertissement navigateur si fermeture avant acquittement. Une saisie invalide ne remplace pas les valeurs enregistrées : le backend reste la source de vérité, l’interface ne promet pas de restaurer des frappes non enregistrées.

Conflit entre onglets : 409 DRAFT_CONFLICT, arrêt de l’autosave et rechargement explicite. Expiration/révocation : 401 explicite, cookie supprimé, message et aucune boucle de retry. Réinitialiser via rechargement ne crée pas automatiquement de nouveau brouillon. Aucun accès à l’ancien brouillon depuis une autre session ni récupération par email dans cette phase.

## Session et sécurité

32 octets crypto/rand (256 bits), base64url sans padding. Seul SHA-256 du secret est stocké en bytea ; le brut est placé exclusivement dans le cookie `ifri_draft`. Cookie HttpOnly, SameSite=Lax, sans Domain, Path `/api/v1/applications/drafts`, Secure en production HTTPS. Expiration absolue configurable DRAFT_SESSION_TTL_SECONDS : défaut local 604800 (7 jours), valeur explicite obligatoire en production, plage 300–2592000 secondes. Pas de prolongation silencieuse ; revoked_at permet révocation côté serveur. Rotation à prévoir lors d’une future promotion vers compte/session authentifiée, aucune fixation par secret fourni à la création.

Toutes les mutations exigent Origin exactement FRONTEND_ORIGIN. Les mutations de profil/parcours exigent application/json ; les envois documentaires exigent multipart/form-data avec un unique fichier PDF et X-Application-Version. CORS explicite et credentials=true ; fetch credentials=include, aucun wildcard. Origine absente refusée, sans fallback Referer ambigu ; SameSite est une défense complémentaire, pas le seul contrôle. [OWASP CSRF](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html), [OWASP sessions](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html).

Autorisation exclusivement par hash de session lié à l’application : aucun GET/PATCH /applications/{uuid}, aucun DELETE public. DTO explicites sans hash ni secret/session id/candidate id. Corps JSON limité à 8 KiB, champs inconnus refusés, requêtes SQL paramétrées. Logs de route/méthode/durée, jamais corps/cookie/identité ; erreur SQL masquée. Création limitée à 5/minute/IP via RemoteAddr (aucune confiance dans X-Forwarded-For), mémoire bornée 1024 IP ; limite locale au processus, à adapter au déploiement. Le navigateur partagé reste un détenteur d’accès tant que le cookie est valide : ce mécanisme n’est pas une vérification d’identité ou d’email.

## Validation et référentiels

Noms : trim, Unicode/accents conservés, pas de majuscules automatiques, caractères de contrôle refusés, limites 100/150 caractères. Email : trim + lowercase, syntaxe mail.ParseAddress sans display name, domaine avec point, <=254 octets ; aucune vérification de possession. Téléphone : suppression des séparateurs espaces/tirets/parenthèses, 00 converti en +, syntaxe internationale + suivie de 7–15 chiffres, premier chiffre non nul. Pas de restriction au Bénin ; contrôle de forme E.164, pas de vérification de l’attribution du numéro. Année entière entre 1900 et l’année UTC courante, contrôlée côté serveur.

Qualifications et filières issues de l’API de phase 2. Programme enregistré par FK CampaignProgram, qualification par FK OriginQualification. La mutation de filière résout campagne + qualification + filière côté serveur (règles publiées) ; non éligible = 422 APPLICATION_NOT_ELIGIBLE. Changement de qualification : revérification du choix courant, retrait automatique s’il n’est plus autorisé, dans la même transaction. DT_DWM/GL puis DT_EAP retire GL ; BAC_E/GL est autorisé. Aucun coefficient configuré ni nouveau calcul.

## Contrat HTTP

| Méthode et route | JSON | Résultat |
|---|---|---|
| POST /api/v1/applications/drafts | `{ "campaign": "DEV_LOCAL" }` ou `{}` pour campagne courante unique | 201 brouillon + cookie ; session déjà valide : 200 même brouillon |
| GET /api/v1/applications/drafts/current | Aucun | 200 brouillon détenu par session ; 401 absente/expirée/révoquée |
| PATCH …/current/profile | `{ "version": 1, "profile": { "lastName": "…", "firstNames": "…", "email": "…", "phone": "…" } }` | 200 profil normalisé, version incrémentée |
| PATCH …/current/academic-background | `{ "version": 2, "qualification": "DT_DWM", "graduationYear": 2025 }` | 200 qualification/année et éventuelle filière retirée |
| PATCH …/current/program | `{ "version": 3, "program": "GL" }` | 200 choix autorisé ; sinon 422 |

Brouillon : id interne, profile, campaign, qualification nullable, graduationYear nullable, program nullable, state DRAFT, version, updatedAt, sessionExpiresAt. Erreurs des nouveaux endpoints : `{ "error": { "code": "…", "message": "…", "fields": {} } }`, fields omis si aucun champ concerné. Codes 400 INVALID_PARAMETERS, 401 DRAFT_SESSION_REQUIRED/EXPIRED/REVOKED, 403 ORIGIN_FORBIDDEN, 409 DRAFT_CONFLICT/CAMPAIGN_NOT_OPEN, 415 JSON_REQUIRED, 422 VALIDATION_FAILED/APPLICATION_NOT_ELIGIBLE, 429 RATE_LIMITED, 500 INTERNAL_ERROR. Les référentiels existants gardent leur contrat ; le client comprend les deux formats sans les réécrire.

## UI et validation visuelle

Master intact ; adaptation dans design-system/ifri-admissions/pages/candidate-application.md, recherches UI UX Pro Max et inspirations 21st.dev documentées. Vue native Input/Select/Radio, aucune dépendance React/Reka non nécessaire pour ces primitives. Labels visibles, erreurs liées, résumé lié aux champs, focus première erreur puis titre d’étape, region status, stepper ol/aria-current/états textuels, cibles >=44 px et reduced-motion global.

Quatre largeurs à vérifier réellement sur l’hôte : 375/768/1024/1440 ; zoom 200 %, clavier (Tab, Shift+Tab, flèches radio), overflow, labels longs, erreurs, chargement, état vide, campagne fermée. Aucun rendu visuel n’est présenté comme validé avant observation navigateur.

## Étape Documents (phase 4)

Quatre destinations explicites, labels et limite fournis par API. Sélecteur natif au clavier, envoi réel sans pourcentage simulé, prévisualisation authentifiée en nouvel onglet et remplacement conservant l’ancienne version jusqu’à succès. Chargement/restauration et erreurs de session ne créent pas de brouillon silencieux. Continuer relit complete serveur ; récapitulatif inclut les quatre pièces sans soumission. Contrat complet : DOCUMENT_WORKFLOW.

## Phase 5 — analyse en cours d’intégration

Six étapes : informations → parcours → filière → documents → analyse → vérification. GET …/current/transcript-analysis expose seulement l’analyse fraîche autorisée et verificationReady ; POST …/confirm et …/reject exigent Origin, JSON, session, analysisId courant et version du brouillon. Aucun PATCH de note/coefficient. REJECTED impose un nouveau relevé. Échec/LOW_CONFIDENCE : consignes de lisibilité et remplacement, sans stacktrace ou score brut.

Confirmation/rejet indépendants du statut OCR. Modification de filière/qualification ou remplacement du relevé invalide l’analyse en DB ; retour vers une ancienne combinaison ne réactive jamais une confirmation ancienne. Aucune moyenne ni soumission finale dans cette phase.

Après rejet, retour Documents avec le relevé mis en évidence et focus du sélecteur. Le bouton Analyse reste bloqué jusqu’au remplacement du relevé, même si les autres documents sont complets. Changement puis retour de contexte ne contourne pas le rejet : le serveur impose une nouvelle version.

## Parcours actif — phase 5.1 corrective

Cinq étapes : Vos informations → Votre parcours → Filière souhaitée → Documents → Vérification. L’étape OCR séparée et le bouton Analyser mon relevé sont supprimés. Analyse et confirmation apparaissent sous la carte du relevé automatiquement dès son upload/remplacement.

Le composant de lecture suit la version TRANSCRIPT et le contexte académique ; polling au montage pour QUEUED/PROCESSING, arrêt terminal et nettoyage à l’unmount. Refresh restaure sans créer d’analyse. Pendant un autre upload, polling/confirmation sont suspendus pour éviter un conflit local artificiel de version ; à son acquittement, la lecture serveur réactualise la complétude. Données extraites jamais éditables. Confirmation puis quatre documents complets autorisent Vérification via contrôle serveur. Rejet reste dans Documents ; nouvelle version obligatoire. Annonces async polies sans déplacement de focus.
