# Soumission définitive — phase 6

Moyenne pondérée, soumission atomique, numéro de candidature, verrouillage et signaux pour l'administration. Hors périmètre : administration, classement, notifications, PDF de récépissé. Décisions : DECISIONS (ADR 083–094). Preuves : PHASE6_VALIDATION.

## Parcours candidat

- **Vérification** (5ᵉ et dernière étape) : récapitulatif complet (informations, parcours, filière, quatre documents, matières, notes et coefficients lus sur le relevé), **sans la moyenne**. Les boutons « Modifier … » restent disponibles tant que la candidature n'est pas soumise.
- **Soumission** : le bouton « Soumettre ma candidature » ouvre un `<dialog>` natif par `showModal()`. Le fond est inerte et le focus reste piégé dans le dialogue, les deux par le navigateur. Rôle `alertdialog`, titre et description liés, texte « Après soumission, vous ne pourrez plus modifier votre candidature », focus initial sur « Annuler ». Escape ferme le dialogue, sauf pendant l'envoi. Le focus est rendu explicitement au bouton d'origine.
- **Pendant l'envoi** : « Soumission en cours… », les deux boutons sont désactivés et le dialogue est en `aria-busy`. Le délai réseau est de 20 s ; un nouvel essai est sans risque, car la soumission est idempotente.
- **Erreurs** : message en français choisi par le client selon le code, puis action utile :

| Code | Action côté interface |
|---|---|
| `ADMISSION_RULES_CHANGED`, `TRANSCRIPT_ANALYSIS_REQUIRED`, `TRANSCRIPT_ANALYSIS_OUTDATED` | Retour à Documents, rechargement de l'analyse : la nouvelle analyse s'affiche dans la carte du relevé et doit être confirmée |
| `APPLICATION_NOT_ELIGIBLE` | Retour à Filière souhaitée |
| `APPLICATION_INCOMPLETE` | Retour à la première étape incomplète, erreurs des champs affichées |
| `VERSION_CONFLICT`, `CAMPAIGN_CLOSED`, 401 | Session bloquée, bouton « Recharger le brouillon » |
| `APPLICATION_LOCKED` | Rechargement, qui mène à la confirmation (soumise dans un autre onglet) |
| `NETWORK_ERROR`, `SUBMISSION_FAILED` | Nouvel essai : si la soumission avait abouti, le même numéro s'affiche |

- **Confirmation** (`/candidature/confirmation`) : numéro très visible, « Copier le numéro » (presse-papiers, annonce dans une région `status`), « Imprimer le récépissé », filière, diplôme, date en heure du Bénin, et le conseil de conservation avec la date limite de consultation. La page n'a **aucun** bouton de modification. Sa feuille `@media print` sert de récépissé : boutons masqués, numéro encadré sur une ligne, aucun aplat de couleur.
- **Après un refresh** : `/candidature` lit le brouillon et, s'il est `SUBMITTED`, redirige vers la confirmation (`router.replace`) sans créer de brouillon ni charger documents et analyse. La confirmation relit le récépissé (`GET …/current/submission`). Un dossier non soumis est renvoyé vers `/candidature`.

## Contrat HTTP

Les deux routes sont sous `/api/v1/applications/drafts`, seul chemin où le cookie de session est envoyé.

| Route | Entrée | Sortie |
|---|---|---|
| `POST …/current/submission` | Origin = FRONTEND_ORIGIN, JSON `{ "version": n }`, champs inconnus refusés | 200 récépissé, cookie prolongé (même réponse à la première soumission et aux suivantes) |
| `GET …/current/submission` | cookie | 200 récépissé ; 404 `SUBMISSION_NOT_FOUND` si le dossier n'est pas soumis |

Le récépissé contient `applicationNumber`, `submittedAt`, `candidate` (nom, prénoms), `campaign`, `program`, `qualification`, `graduationYear` et `sessionExpiresAt`. Il ne contient jamais la moyenne. `GET …/current` (brouillon) expose aussi `applicationNumber` et `submittedAt`.

## Transaction de soumission

Une seule transaction PostgreSQL en READ COMMITTED, tout ou rien (`submissions.Repository.Submit`). Rien ne vient de l'écran : tout est relu et revérifié en base.

1. Session par hash du cookie, puis `SELECT … FOR UPDATE` de l'application et de la session.
2. Déjà `SUBMITTED` → **rejeu** : même récépissé, sans contrôle de version. C'est ce qui couvre le double clic, le retry réseau et les deux requêtes concurrentes : la seconde attend le verrou de ligne, puis relit `SUBMITTED`.
3. Autre état que `DRAFT` → `APPLICATION_LOCKED`. Version différente → `VERSION_CONFLICT`.
4. Campagne verrouillée `FOR SHARE`, puis statut `OPEN` et période en cours → sinon `CAMPAIGN_CLOSED`.
5. Profil complet et valide (normalisation de la phase 3), qualification, année, filière, les quatre documents avec une version courante non remplacée → sinon `APPLICATION_INCOMPLETE` avec `fields`.
6. Couple qualification × filière résolu sur la grille publiée (offre, programme et qualification actifs) → sinon `APPLICATION_NOT_ELIGIBLE`. Une règle rendue incomplète, par exemple une matière désactivée, relève du point 7.
7. Analyse non obsolète de l'application, verrouillée. Si sa grille diffère de la grille publiée, ou si ses matières attendues (id, position) diffèrent → `ADMISSION_RULES_CHANGED`. L'analyse passe alors `OBSOLETE`, `refresh_transcript_analysis` met une nouvelle analyse en file, et **cette invalidation est commitée**. La soumission, elle, ne l'est pas.
8. Analyse `SUCCEEDED` + `CONFIRMED`, liée à la version courante du relevé, au programme et à la qualification du dossier. Sinon : `TRANSCRIPT_ANALYSIS_OUTDATED` si une analyse a déjà été confirmée pour ce dossier (relevé ou parcours changé depuis), `TRANSCRIPT_ANALYSIS_REQUIRED` sinon.
9. Matières extraites relues depuis `ocr_extracted_subjects` : exactement les matières attendues, dans l'ordre, note dans [0, 20], coefficient > 0, preuve lisible. Sinon `TRANSCRIPT_ANALYSIS_REQUIRED`, avec un avertissement journalisé qui ne contient que des identifiants.
10. Calcul exact (voir plus bas).
11. `pg_advisory_xact_lock` par campagne : les soumissions d'une même campagne sont sérialisées et deux doublons simultanés se voient toujours. Calcul des drapeaux.
12. Numéro : incrément du compteur (année de numérotation, filière).
13. Insertions : snapshot, matières, documents, drapeaux, `audit_events`. Puis `UPDATE applications SET state='SUBMITTED', application_number, submitted_at=now(), version=version+1` (avec garde `state='DRAFT' AND version=n`), prolongation de la session, lecture du récépissé, `COMMIT`.

Ordre des verrous, identique aux autres écritures : application et session, campagne, analyse, verrou consultatif de campagne, ligne du compteur. Tout échec avant le `COMMIT` annule l'ensemble, compteur compris. Le test d'injection de panne le vérifie.

## Calcul de la moyenne

- Σ(note × coeff) / Σ coeff en `math/big.Rat`, à partir des valeurs `NUMERIC` persistées lues en texte, jamais par un `float`. Aucun calcul dans Vue.
- Stockage, sans arrondi :
  - `weighted_sum` et `coefficient_sum` sont stockées **exactement**. Ce sont des sommes et produits de décimales, donc des décimales finies. La fraction `weighted_sum / coefficient_sum` est la valeur de référence.
  - `average numeric(32,30)` est cette fraction **tronquée** à 30 décimales, jamais arrondie. `average_is_exact` vaut vrai si rien n'a été tronqué ; 31/3 donne 10,333…333 avec `false`.
  - `calculation_version = 1`.
- PostgreSQL vérifie lui-même que :
  - `average × coefficient_sum ≤ weighted_sum < (average + 10⁻³⁰) × coefficient_sum` ;
  - le drapeau d'exactitude est cohérent ;
  - au commit, les deux sommes correspondent aux matières copiées.
- L'Office du Bac tronque la moyenne qu'il imprime ; la règle de l'IFRI reste inconnue. Toute règle future (troncature, arrondi, départage) s'appliquera à la fraction exacte. Le classement de la phase 9 départagera deux colonnes décimales égales par produit en croix des fractions (ADR 086).

## Numéro de candidature

Format `IFRI-{ANNÉE}-{PROGRAMME}-{SERIAL6}`, par exemple `IFRI-2026-GL-000001`. Il est attribué uniquement à la soumission.

- **ANNÉE** = `admission_campaigns.numbering_year` (`smallint NOT NULL`, 2000–9999), jamais la date de soumission : une campagne à cheval sur deux années civiles garde une seule année. La migration remplit les campagnes existantes avec l'année d'ouverture, dans le fuseau Africa/Porto-Novo. La campagne locale `DEV_LOCAL`, ouverte en 2000, numérote donc en 2000.
- **Mécanisme** : table `application_number_counters (numbering_year, program_id) → last_serial`, `INSERT … ON CONFLICT DO UPDATE SET last_serial = last_serial + 1 RETURNING`, dans la transaction de soumission.
  - Deux soumissions de la même filière se sérialisent sur la ligne du compteur ; la première insertion d'une paire est protégée par l'insertion spéculative de PostgreSQL.
  - L'incrément faisant partie de la transaction, un rollback l'annule aussi : la séquence est **sans trou en pratique**. Ce n'est **pas** une garantie contractuelle ; une évolution future, une restauration partielle ou une intervention manuelle pourraient en créer, et c'est acceptable.
  - Un trigger n'autorise que l'incrément de 1 : pas de décrément, pas de suppression, pas de `TRUNCATE`. L'unicité est garantie séparément par `UNIQUE(application_number)`.
  - Au-delà de 999 999 numéros pour une année et une filière, la soumission est refusée avec `APPLICATION_NUMBERS_EXHAUSTED`.
- **Écarté** : une `SEQUENCE` PostgreSQL. Elle n'est pas transactionnelle (trous à chaque rollback) et il en faudrait une par année et par filière.

## Snapshot et immuabilité

- **Tables**
  - `application_submissions` (une par application) : références vers l'analyse, la version et le SHA-256 du relevé, la grille, l'offre, le programme, la qualification et l'année ; copie de l'identité au moment du dépôt ; sommes, moyenne, exactitude, version du calcul ; `validation_path = 'CANDIDATE_CONFIRMED_OCR'`.
  - `application_submission_subjects` : par matière, code et libellé, position, note et coefficient bruts et retenus, référence vers la ligne extraite, et **méthode de validation** :
    - `DIRECT_READ` : chemin générique, couche texte ou OCR au-dessus du seuil ;
    - `ROW_ARITHMETIC` : note lue et confirmée par Points = Note × Coeff sur sa rangée (BAC par cellules) ;
    - `TABLE_TOTALS` : note illisible, dérivée de Points ÷ Coeff et validée par les totaux du tableau ;
    - `reconstructed_cells` : cellules `points` ou `scale` de la rangée reconstruites depuis le Total. Le coefficient n'est jamais reconstruit.
  - `application_submission_documents` : les quatre versions soumises et leur SHA-256.
- **Garde-fous en base**
  - **Triggers à l'insertion** : chaque matière copiée doit être identique, valeur pour valeur, à la ligne extraite de l'analyse référencée ; chaque document doit être la version courante du dossier.
  - **Contrainte différée** : au commit, une application `SUBMITTED` a son snapshot complet (quatre documents, matières attendues), cohérent avec l'application, la candidature et le numéro (année de la campagne, code de la filière).
  - **CHECK** sur `applications` : `DRAFT` ⇒ ni numéro ni date ; `SUBMITTED` ⇒ numéro et date.
- **Immuabilité par triggers** :
  - les tables de snapshot, les drapeaux et `audit_events` refusent UPDATE, DELETE et TRUNCATE ;
  - une application qui n'est plus `DRAFT` refuse toute modification ou suppression, de même que son candidat, ses documents et ses versions de documents, y compris l'ajout d'une version ;
  - l'analyse référencée par une soumission ne change plus, pas même vers `OBSOLETE` ;
  - `ocr_extracted_subjects` refuse en plus l'insertion après l'enregistrement du résultat ou après la décision du candidat, ainsi que TRUNCATE.
- **Correctif de la phase 5** : `refresh_transcript_analysis` sortait du contexte toute application non `DRAFT` et rendait donc son analyse `OBSOLETE` au passage à `SUBMITTED`. La fonction s'arrête désormais immédiatement hors `DRAFT` ; le comportement des brouillons est inchangé.

## Signaux pour l'administration (non bloquants)

Calculés à la soumission, sous le verrou de campagne, contre les seules candidatures **déjà soumises** de la même campagne :

- `DUPLICATE_EMAIL` : même email normalisé (minuscules, sans espaces) ;
- `DUPLICATE_PHONE` : même téléphone normalisé (E.164) ;
- `DUPLICATE_TRANSCRIPT` : même SHA-256 de relevé ;
- `SERIES_UNREADABLE` : qualification BAC sans série lue sur le relevé (lettre illisible, ou chemin générique qui ne lit pas la série).

Chaque drapeau de doublon référence l'autre candidature (`related_application_id`). La première candidature d'une paire ne porte aucun drapeau : la fiche admin de la phase 8 devra afficher le lien dans les deux sens. Rien n'est bloqué : la politique de doublons est une question pour l'IFRI.

## Verrouillage et session après soumission

- **Côté backend**, après `drafts.Editable`, contrôlé avant la version optimiste, toutes les routes candidat renvoient 409 `APPLICATION_LOCKED` :
  - les PATCH profil, parcours et filière ;
  - l'envoi de document ;
  - la confirmation et le rejet d'analyse ;
  - les lectures de documents, de leur contenu et de l'analyse.
- **La session reste valable en lecture seule** pour deux GET seulement : `…/current` (état et numéro, utilisé pour la redirection) et `…/current/submission` (récépissé). `POST /drafts` avec ce cookie renvoie le dossier soumis, jamais un nouveau brouillon. `POST …/submission` rejoue le récépissé.
- **Durée** : à la soumission, `expires_at = greatest(expiration actuelle, fin de campagne, now() + 7 jours)`, et le cookie est réémis avec la même valeur et la nouvelle échéance. Les navigateurs plafonnent la durée de vie d'un cookie (environ 400 jours pour Chromium), ce qui reste au-delà d'une campagne.

## Audit et journaux

- `audit_events` est générique dès cette phase :
  - `event_type` ;
  - `actor_type` CANDIDATE, ADMIN ou SYSTEM, et `actor_id` nullable ;
  - `target_type` et `target_id` ;
  - `metadata` JSON d'identifiants techniques ;
  - `occurred_at`.

  La phase 7 la réutilise sans migration.
- `APPLICATION_SUBMITTED` : acteur CANDIDATE (id du candidat), cible APPLICATION, et dans les métadonnées seulement des identifiants opaques (soumission, analyse, grille, version du relevé), le chemin de validation, la version du calcul, le nombre de matières et les codes des drapeaux. Ni nom, ni email, ni téléphone, ni note, ni moyenne, ni numéro.
- Journal applicatif : `application submitted` avec `application_id`, `submission_id` et `replay`. Les erreurs internes sont masquées (`SUBMISSION_FAILED`).

## Repli manuel (préparé, non implémenté)

`validation_path` est une colonne contrainte par CHECK. `ocr_analysis_id`, les sommes, la moyenne et la version du calcul sont nullables sous un CHECK conditionnel, NOT NULL seulement pour `CANDIDATE_CONFIRMED_OCR`. Une future soumission « vérification manuelle requise », après plusieurs échecs d'analyse, demandera seulement :

- d'élargir ces CHECK à `MANUAL_REVIEW_REQUIRED` ;
- d'ajouter une table de transcription administrative séparée, qui ne modifie jamais le snapshot ;
- de compter les tentatives par version de relevé.

Tout cela sans migration destructive. Voir l'ADR 092.

## Limites connues

- Le cookie est le seul accès du candidat : sur un autre navigateur, ou après expiration, le récépissé n'est plus consultable en ligne, d'où le conseil de copier ou d'imprimer le numéro. Un nouveau brouillon redevient possible ; les drapeaux de doublon le signaleront à sa soumission. La consultation par numéro + code envoyé par email est notée pour la phase 10 ou plus tard (TODO).
- Le récépissé imprimable est la page HTML ; aucun PDF de récépissé n'est généré, c'est hors périmètre.
- `CANCELLED` n'a toujours aucune transition ; le verrouillage la traite comme `SUBMITTED`.
