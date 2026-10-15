# Classement, gel, sélection et exports (phase 9)

Contrat de la phase 9 :
- classement des dossiers validés d'une filière, places et moyenne minimale ;
- gel et validation par le Directeur ;
- désistements et promotions par le Directeur adjoint ;
- exports publics et internes.

Décisions : ADR 122 à 131 (DECISIONS). Preuves : PHASE9_VALIDATION. Revue des dossiers : ADMIN_REVIEW.

Hors périmètre : consultation des résultats par le candidat, notifications, mise en production.

## Qui est classé

Seuls les dossiers dont la revue est **Validé** (`VALIDATED`) dans la filière et la campagne sont classés. Les dossiers rejetés ne figurent dans aucun classement ; aucun dossier « À vérifier » ou « En vérification » ne peut l'être (le gel l'interdit).

## Ordre du classement

Du plus fort au plus faible (calcul `selection.Rank`, version de calcul 1) :

1. **Moyenne effective** (après corrections approuvées, ADR 114), comparée **exactement par produit en croix** des fractions `Σ(note × coeff) / Σ coeff` : `a.w × b.c` contre `b.w × a.c`. Jamais sur la colonne décimale ni sur l'affichage. 40/3 et 80/6 sont égales ; 40/3 dépasse sa propre troncature à 30 décimales.
2. **Note effective de mathématiques**, la plus forte d'abord.
   - Il s'agit de *Mathématiques* (code `MATH`) ou de *Mathématiques appliquées* (code `APPLIED_MATH`, DT).
   - La matière est identifiée **par son code**, jamais par son libellé. La liste des codes est dans la seule table `ranking_math_subjects`.
   - La note est celle du relevé, ou la dernière correction approuvée.
   - Le critère ne s'applique que dans un **groupe d'égalité exacte de moyenne** dont **tous** les membres ont une matière mathématique. Sinon il est sauté pour tout le groupe (voir ci-dessous).
3. **Date de soumission**, la plus ancienne d'abord.
4. **Numéro de candidature**, pour un ordre total et déterministe.

Pour chaque candidat, le classement enregistre et affiche **le critère qui l'a départagé du précédent** :
- Moyenne, Note de mathématiques, Date de soumission ou Numéro de candidature ;
- rien pour le premier.

Quand les mathématiques ont été sautées pour un groupe, l'écran et l'export interne l'indiquent : « Maths non comparées : candidat sans matière mathématique dans le groupe ».

Une grille qui contiendrait **plusieurs** matières mathématiques rend le départage indéterminé. Le gel est alors refusé avec un motif explicite (`MATH_AMBIGUOUS`). Aucun cas n'existe dans la matrice actuelle.

### Pourquoi la règle s'applique par groupe d'égalité, et pas deux à deux

Règle confirmée par l'IFRI : « si l'un des deux candidats n'a pas de matière mathématique, ce critère est sauté pour cette comparaison ». Appliquée **paire par paire**, elle ne donne pas toujours un ordre.

Trois candidats ont exactement la même moyenne :

| Candidat | Mathématiques | Soumis |
|---|---|---|
| A | 10 | 1er |
| B | aucune (DT/EAP) | 2e |
| C | 15 | 3e |

Comparaisons deux à deux :
- C passe devant A (mathématiques, 15 > 10) ;
- A passe devant B (pas de mathématiques chez B : date, A plus ancien) ;
- B passe devant C (pas de mathématiques chez B : date, B plus ancien).

On obtient C > A > B > C, un **cycle** : aucun classement ne respecte ces trois comparaisons, et le résultat d'un tri dépendrait de l'ordre de lecture des dossiers.

**Règle retenue (décision utilisateur, ADR 122)** : le critère est sauté **pour tout le groupe** dès qu'un de ses membres n'a pas de matière mathématique. Dans l'exemple, A, B et C sont départagés par la date : **A, B, C**. L'ordre est toujours défini et reproductible.

Effet : dans un tel groupe, C, qui a la meilleure note de mathématiques, passe après A et B. Sans B, C serait passé devant A. La présence d'un candidat sans mathématiques change donc l'ordre des autres membres de son groupe d'égalité, et seulement de ce groupe.

L'IFRI tranchera entre cette règle et une règle alternative qu'elle proposerait (ROADMAP, question 3). Exemples :
- classer après les autres, dans le groupe, les candidats sans note de mathématiques ;
- retenir une autre matière pour certaines séries.

## Places, moyenne minimale et décisions

- **Places** : colonne `campaign_programs.available_seats` (quota de la phase 2, réutilisée), de **1 à 10 000**.
- **Moyenne minimale** : facultative, aucune par défaut, `campaign_programs.minimum_average`, de 0 à 20 avec 2 décimales au plus. La comparaison est exacte et « au moins égale » : `w ≥ seuil × c`.
- Les deux sont saisis par le Directeur (`ranking.configure`) et modifiables jusqu'au gel. Chaque changement est audité (`RANKING_SETTINGS_CHANGED`, avec les valeurs avant et après). Après le gel, un trigger refuse tout changement.
- **Décisions** :
  - les N premiers (N = places) sont **Admis** ;
  - les autres candidats classés sont en **Liste d'attente**, dans l'ordre ;
  - sous la moyenne minimale, un candidat est **Non classé**, sans rang, même s'il reste des places. Il apparaît après les classés, dans l'export interne seulement.
- Une égalité exacte pile sur la ligne des places est départagée par les critères ci-dessus : il y a toujours exactement N admis (ou moins s'il y a moins de classés).

## Classement provisoire

- Lisible à tout moment par le Directeur, le Directeur adjoint et le responsable du secrétariat (`ranking.read`, lecture seule). Les agents et l'administrateur technique n'y ont pas accès.
- **Calculé à la volée** à chaque affichage, sans stockage. La ligne « Limite des N places » est matérialisée. Chaque consultation est auditée (`ADMIN_DATA_ACCESSED`, `RANKING`).
- **Indicateurs de blocage du gel** :
  - `CAMPAIGN_OPEN` : période de candidature non terminée ;
  - `DOSSIERS_PENDING` : dossiers À vérifier ou En vérification, avec leur nombre et un lien vers la liste ;
  - `CORRECTIONS_PENDING` : corrections en attente ;
  - `SEATS_MISSING` : nombre de places non défini ;
  - `MATH_AMBIGUOUS`.

## Gel et validation (Directeur, `selection.validate`)

La transaction :

1. **verrouille la campagne** (`FOR UPDATE`) : les soumissions en cours, qui la verrouillent en partage, se terminent avant ; un dossier soumis à la dernière seconde est donc vu ;
2. verrouille l'offre filière/campagne, puis **toutes les revues de la filière** (`FOR UPDATE`) : une prise en charge, un retour arrière ou une correction concurrente (qui verrouillent d'abord la ligne de revue) attend, puis échoue avec `REVIEW_FROZEN` ; à l'inverse, un gel qui attend voit le dossier remis en vérification et est refusé ;
3. vérifie les **préconditions**, qui sont toutes revérifiées par le trigger `guard_ranking_freeze` :
   - la période de candidature est terminée (statut `CLOSED` ou `ARCHIVED`, ou `ends_at` passé) ;
   - tous les dossiers soumis de la filière sont Validés ou Rejetés ;
   - aucune correction n'est en attente ;
   - le nombre de places est défini.

   En plus, les places et le seuil envoyés doivent être ceux affichés au Directeur (sinon `RANKING_SETTINGS_CHANGED`).
4. écrit `ranking_freezes` (places, seuil, nombres de classés, non classés et admis, version de calcul) : **le verrou de la phase 8 bloque dès lors toute revue et toute correction de la filière** ;
5. écrit le **snapshot immuable** `ranking_entries`, une ligne par dossier validé :
   - position et rang ;
   - moyenne effective : fraction (sommes exactes), décimale à 30 chiffres et affichage tronqué ;
   - matière et note de mathématiques ;
   - état du critère mathématique (`NOT_NEEDED`, `COMPARED`, `SKIPPED`) ;
   - date de soumission, numéro, critère décisif et décision initiale ;
6. audite `RANKING_FROZEN`.

Garde-fous en base (migration 00009) :
- **Écriture des lignes** : une ligne du snapshot ne s'écrit que dans la transaction de son gel (`frozen_at = now()`). Elle doit recopier exactement un dossier validé de la filière : sommes, moyenne, numéro, date.
- **Contrôle différé au commit** :
  - le snapshot couvre exactement les dossiers validés ;
  - les rangs sont contigus et les décisions suivent les places et le seuil ;
  - chaque paire de positions voisines est ordonnée par produit en croix ;
  - chaque critère décisif est cohérent avec ses valeurs (par exemple, « Note de mathématiques » seulement si le critère était comparé et que la note est plus haute).
- **Immuabilité** : snapshot, gel et événements n'acceptent ni UPDATE, ni DELETE, ni TRUNCATE. Le down de 00009 est refusé dès qu'un gel, un export ou un seuil existe.
- **Aucun dégel** dans l'interface ni dans l'API. La conduite à tenir en cas de fraude découverte après le gel est une question IFRI (ROADMAP, question 4).

## Après le gel : désistements et promotions (Directeur adjoint, `selection.manage`)

- **Désistement**, motif obligatoire : d'un **admis** (y compris d'un promu) ou d'un candidat **en liste d'attente**. Un non classé ou un candidat déjà désisté ne peut pas se désister.
- **Promotion** : seulement s'il existe une **place libre** (moins d'admis que de places), et seulement du **premier candidat en liste d'attente** dans l'ordre du gel, que le système propose. On ne peut pas sauter un candidat. S'il ne donne pas suite, on enregistre d'abord son désistement, puis le suivant est proposé. Le promu garde son **rang du gel**.
- Plusieurs désistements puis plusieurs promotions peuvent s'enchaîner.
- **Ajout seul** : `selection_events` (désistement ou promotion, motif, auteur, date). La décision courante de chaque candidat (Admis, Liste d'attente, Désisté, Non classé) se **déduit** de la décision initiale et des événements : une promotion rend admis, un désistement rend désisté. Le snapshot n'est jamais modifié.
- **Version de la liste** : 1 au gel, +1 à chaque événement. Chaque action envoie la version affichée : si la liste a changé entre-temps, elle est refusée en 409 `SELECTION_VERSION_CONFLICT`, et deux décisions simultanées ne peuvent pas s'appliquer toutes les deux. Le trigger `guard_selection_event` revérifie en base, sous verrou de la ligne de gel :
  - la version suivante ;
  - l'état courant du candidat ;
  - la place libre ;
  - l'ordre de la liste d'attente.
- Audit : `SELECTION_WITHDRAWN`, `SELECTION_PROMOTED` (décision précédente, version).

## Permissions

Toujours un seul rôle par compte (ADR 110).

| Permission | Rôles | Usage |
|---|---|---|
| `ranking.read` | Directeur, Directeur adjoint, responsable du secrétariat | Classement provisoire et gelé |
| `ranking.configure` | Directeur | Places et moyenne minimale |
| `selection.validate` | Directeur | Gel et validation |
| `selection.manage` | Directeur adjoint | Désistements et promotions |
| `selection.export` | Directeur, Directeur adjoint | Exports CSV et PDF |

L'agent du secrétariat n'a plus `ranking.read` (il l'avait, sans usage, en phase 8).

## Exports

- **Après le gel seulement**, à la version courante de la liste.
- Chaque export porte la filière, la campagne, la version de la liste avec sa date (et celle du gel) et la date de génération, en heure du Bénin.
- **Version publique** (affichage officiel) :
  - colonnes : rang du gel, numéro, nom, prénoms, décision actuelle ;
  - seulement les **Admis** et la **Liste d'attente**, par rang ;
  - aucune moyenne, aucun critère, aucun email, aucun téléphone ; pas de désisté ni de non classé.
- **Version interne** : tous les candidats classés et non classés, avec en plus :
  - la position ;
  - la série ;
  - la moyenne effective affichée (tronquée) ;
  - la note de mathématiques et l'état du critère mathématique ;
  - la date de soumission, le critère décisif, la décision au gel et la décision actuelle ;
  - les places, la moyenne minimale et des notes de lecture.
- **CSV**, produit par Go à la demande (`GET …/export.csv?variant=public|internal`) :
  - UTF-8 avec BOM, séparateur `;`, fins de ligne CRLF ;
  - en-tête de métadonnées puis tableau ;
  - **neutralisation des formules** : une cellule qui commence par `=`, `+`, `-`, `@`, tabulation ou retour chariot est **préfixée d'une apostrophe**, sans autre modification. Exemple : le nom synthétique `-Tiret` devient `'-Tiret`.
- **PDF**, produit par le **worker** (ADR 130) :
  - `POST …/exports` crée une demande (version de la liste figée) ;
  - Go place l'identifiant seul sur la file `ifri:exports:v1` ; **aucun nom ne passe par Redis** ;
  - la boucle dédiée du worker récupère le document par `GET /internal/exports/{id}/payload` (réseau privé, jeton OCR). Go y a déjà décidé de chaque cellule ;
  - le worker met en page avec PyMuPDF et DejaVu Sans (A4, portrait pour la liste publique, paysage pour l'interne) : en-tête de tableau répété à chaque page, pied de page avec la version et le numéro de page, polices sous-ensemblées ;
  - il renvoie le fichier par `POST …/document`. L'API n'accepte qu'un PDF (`%PDF-` … `%%EOF`) de 10 Mio au plus, le stocke dans le bucket privé (`selection/<offre>/exports/<id>.pdf`) et le sert en pièce jointe par `GET …/exports/{id}/content`. Le compte S3 de l'API a donc droit au préfixe `selection/*` en plus de `applications/*` (initialiseur MinIO ; à reproduire en production).
  - Une demande non distribuée est relancée toutes les 5 s (puis 2 min après un envoi) et échoue après 10 min.
  - **Pourquoi le worker** :
    - PyMuPDF et une police couvrant les noms béninois (ɔ, ɛ, Ɖ…) y sont déjà ;
    - Go n'a aucune dépendance PDF à ajouter ;
    - le texte du PDF public se relit dans les tests Python pour vérifier qu'aucune moyenne n'y figure.
- **Audit** : chaque génération est auditée (`SELECTION_EXPORTED` : format, variante, version, nombre de lignes). Chaque téléchargement d'un PDF l'est aussi (`ADMIN_DATA_ACCESSED`, `SELECTION_EXPORT:<variante>`).

## API

| Méthode et chemin (`/api/v1/admin`) | Permission |
|---|---|
| `GET /selection/offers` | `ranking.read` |
| `GET /selection/offers/{id}` | `ranking.read` |
| `PUT /selection/offers/{id}/settings` `{seats, minimumAverage}` | `ranking.configure` |
| `POST /selection/offers/{id}/freeze` `{seats, minimumAverage}` (valeurs affichées) | `selection.validate` |
| `POST /selection/offers/{id}/withdrawals` `{applicationId, reason, listVersion}` | `selection.manage` |
| `POST /selection/offers/{id}/promotions` `{applicationId, listVersion}` | `selection.manage` |
| `GET /selection/offers/{id}/export.csv?variant=public\|internal` | `selection.export` |
| `POST /selection/offers/{id}/exports` `{variant}` | `selection.export` |
| `GET /selection/exports/{id}` et `GET /selection/exports/{id}/content` | `selection.export` |

Codes :
- `RANKING_FROZEN`, `RANKING_NOT_FROZEN`, `RANKING_SETTINGS_CHANGED` ;
- `RANKING_FREEZE_BLOCKED` (champs = conditions non remplies) ;
- `SELECTION_VERSION_CONFLICT`, `SELECTION_STATE_INVALID`, `SELECTION_NO_FREE_SEAT`, `SELECTION_ORDER` ;
- `CANDIDATE_NOT_RANKED`, `EXPORT_NOT_READY`.

## Développement

```sh
docker compose exec api api dev synthetic-applications --count 6 --program IA --confirm
docker compose exec api api dev close-campaign --confirm          # fin de la période DEV_LOCAL, nécessaire au gel
```

Les deux commandes sont refusées en production et auditées. `close-campaign` est irréversible dans une base de développement : seule la recréation du volume rouvre la campagne. `make e2e` exclut la filière IA de ses 5 000 dossiers synthétiques pour que `selection.spec.ts` la classe et la gèle.
