# Modèle conceptuel de données

Modèle cible ; la phase 2 implémente uniquement les huit tables de référentiels décrites ci-dessous. Identifiants opaques UUID ; timestamps UTC ; montants de notes/coefficients en `NUMERIC` avec précision à fixer selon corpus. Versions immuables pour règles, documents et résultats. Références campagne explicites pour empêcher les croisements entre années.

## Entités
| Entité | Données principales et relations |
|---|---|
| AdmissionCampaign | id, nom, année, opens_at, closes_at, statut ; possède offres de filières et versions de grille |
| CampaignProgram | campagne + Program, quota non négatif, ouverture opérationnelle ; unique (campagne, filière) |
| Program | code stable, nom, actif ; cinq filières initiales |
| OriginQualification | code stable, famille BAC/DT, série/spécialité exacte (C, D, E, IMI, DWM, EAP…), actif |
| RuleSetVersion | campagne, numéro, source officielle, état DRAFT/PUBLISHED, auteur/date ; publication immuable |
| EligibilityRule | version grille, diplôme, filière, admissibilité et conditions documentées ; absence ≠ autorisation |
| Subject | code, libellé ; alias normalisés versionnés pour correspondance OCR |
| ExpectedSubjectRule | règle d’admissibilité + matière canonique + ordre ; ensemble complet pour le couple diplôme/filière ; **aucun coefficient, aucune note** |
| Candidate | identité, email, téléphone ; références d’authentification à décider ; indépendant des campagnes |
| Application | candidat, CampaignProgram, diplôme, année obtention, snapshot identité au dépôt, règle/version utilisée, états dépôt/administration, active_analysis_id, revision, submitted_at |
| ApplicationDocument | dossier, type (DIRECTOR_REQUEST, TRANSCRIPT, IDENTITY, PARENTAL_COMMITMENT), version, clé objet privée, hash, taille/type détecté/pages, statut sécurité, superseded_at |
| Transcript | document relevé exact, application ; lie les analyses d’une même version PDF |
| OCRAnalysis | transcript, révision dossier, version grille/moteur/contrat, état, confiance globale, dates, erreurs, matières attendues figées, tentative/job |
| ExtractedSubject | analyse, matière canonique, libellé brut, note NUMERIC nullable, coefficient NUMERIC nullable, page, confiance ; valeur absente explicite, jamais zéro par défaut |
| ExtractionEvidence | extracted_subject, champ SUBJECT/GRADE/COEFFICIENT, page, bounding box, texte brut, confiance par champ, méthode native/OCR, dimensions/rotation/page coordinate system |
| AnalysisConfirmation | analyse exacte, document/hash, version grille, candidat, décision CORRECT/INCORRECT, date ; immuable |
| AverageCalculation | dossier, analyse acceptée, version algorithme, somme pondérée, somme coefficients, moyenne indicative, scale, provenance ; immuable, activation explicite |
| AdministrativeReview | dossier, reviewer/admin, décision, motif, révision examinée, date ; événements immuables |
| Ranking | campagne/filière, version, quota figé, politique départage/version, date, auteur, statut PROPOSED/APPROVED/INVALIDATED |
| RankingEntry | ranking, application, calculation_id, rang (nullable si égalité non résolue), proposition, décision NOT_DECIDED/SELECTED/WAITING_LIST/NOT_SELECTED, décideur/date |
| AdminUser | identité, email unique normalisé, hash mot de passe, rôle SUPER_ADMIN/ADMIN/REVIEWER, actif, périmètres campagnes, MFA |
| Session | principal/type candidat ou admin, hash du jeton opaque, expiration, révocation ; aucune session brute dans les logs |
| ApplicationTransition | dossier, axe, état avant/après, acteur/service, motif, version, date, correlation_id |
| AuditLog | acteur/type, action, ressource opaque, campagne, horodatage, changements minimisés, correlation_id ; accès et mutations sensibles |
| OCRJob / OutboxEvent | analyse, état durable, idempotency_key, essais, délai, événement publié ; fiabilité Redis, pas entité d’admission |

`ExtractedSubject.page` désigne la page de la ligne ; les preuves de chacun des trois champs sont distinctes. S’il existe plusieurs candidats à une extraction, conserver les alternatives dans un payload versionné sans fabriquer une valeur acceptée. La relation active_analysis doit appartenir au même dossier ; même exigence pour confirmation et calcul.

## Relations structurantes
```mermaid
erDiagram
    AdmissionCampaign ||--o{ CampaignProgram : propose
    Program ||--o{ CampaignProgram : figure
    AdmissionCampaign ||--o{ RuleSetVersion : versionne
    RuleSetVersion ||--o{ EligibilityRule : contient
    OriginQualification ||--o{ EligibilityRule : origine
    Program ||--o{ EligibilityRule : destination
    EligibilityRule ||--o{ ExpectedSubjectRule : attend
    Subject ||--o{ ExpectedSubjectRule : definit
    Candidate ||--o{ Application : depose
    CampaignProgram ||--o{ Application : recoit
    Application ||--o{ ApplicationDocument : versionne
    ApplicationDocument ||--o| Transcript : represente
    Transcript ||--o{ OCRAnalysis : analyse
    OCRAnalysis ||--o{ ExtractedSubject : extrait
    ExtractedSubject ||--o{ ExtractionEvidence : prouve
    OCRAnalysis ||--o{ AnalysisConfirmation : confirme
    Application ||--o{ AverageCalculation : calcule
    CampaignProgram ||--o{ Ranking : classe
    Ranking ||--o{ RankingEntry : contient
    Application ||--o{ RankingEntry : participe
    Application ||--o{ AdministrativeReview : recoit
    AdminUser ||--o{ AdministrativeReview : effectue
```

## Contraintes et historique
- Quota par campagne/filière, jamais sur Program global. Année non nécessairement unique : plusieurs campagnes possibles.
- Hypothèse : un dossier et une filière par candidat/campagne ; contrainte unique à confirmer avant migration. Email n’est pas une preuve d’identité ni une clé métier de dédoublonnage.
- Chaque version document unique (dossier, type, version) ; une seule version courante acceptée par type. Même PDF peut recevoir plusieurs analyses moteur, jamais écrasées.
- Chaque sujet attendu doit avoir une extraction acceptée unique dans l’analyse active ; doublons/absences bloquent l’acceptation. Note valide selon échelle, coefficient > 0 ; NULL représente l’inconnu. Aucun coefficient dans ExpectedSubjectRule, EligibilityRule ou CampaignProgram.
- Contraintes FK/uniques/checks en base complétées par transactions Go pour invariants entre tables. Vérifier cohérence campagne/programme/règle/diplôme. Index futurs sur campagne, filière, diplôme, statuts, moyenne et références jobs ; pagination sans fuite intercampagne.
- Changement d’origine/filière/PDF : nouvelle révision et nouvelles références ; anciennes preuves restent historiques, confirmation et moyenne ne sont plus actives. Dossier rouvert = exclusion du classement courant et invalidation de la proposition concernée.
- Calcul garde ses opérandes ; classement garde son périmètre et ses calculs sources. Publication exige un dernier contrôle transactionnel de fraîcheur des dossiers et quotas.
- Conservation, effacement, anonymisation et exceptions d’audit restent à fixer. Pas de suppression en cascade des preuves d’une décision sans politique validée ; historique ne signifie pas conservation illimitée.

## Schéma effectivement ajouté en phase 2

- `admission_campaigns` : UUID v4, code unique, name, academic_year, starts_at/ends_at timestamptz, statut CHECK DRAFT/OPEN/CLOSED/ARCHIVED, created_at/updated_at. Fermeture strictement après ouverture ; année non unique.
- `programs` : UUID, code unique, name, enabled et timestamps.
- `origin_qualifications` : UUID, code unique, label, family CHECK BAC/DT, enabled. Remplace le nom conceptuel DiplomaType sans décomposer artificiellement diplôme/série/spécialité.
- `subjects` : UUID, code unique, name, enabled. Nom concret de CanonicalSubject, sans valeurs scolaires.
- `campaign_programs` : UUID, FK campagne/filière, unique couple, enabled, available_seats entier nullable CHECK >=0.
- `rule_set_versions` : UUID, FK CampaignProgram, version >0 unique par offre, source, statut DRAFT/PUBLISHED, created_at. Granularité de versionnement par offre campagne/filière (précision du modèle initial par campagne) ; index partiel unique : une grille publiée par offre.
- `program_qualification_eligibilities` : UUID, FK version/qualification, eligible, unique (version, qualification). Aucune duplication de campagne/programme dans cette table. Les qualifications peuvent varier entre versions, jamais être dupliquées dans une version.
- `expected_subject_rules` : UUID, FK éligibilité/matière, position >0, unique (éligibilité, matière) et (éligibilité, position). **Aucune note, aucun coefficient, aucun poids.**

FK restrictives sans cascade de données métier. Les index PK/uniques servent aux recherches par code et à la traversée campagne → offre → grille → qualification → matières ; aucun index métier spéculatif.

Triggers : seules les grilles DRAFT sont modifiables ; publication exige au moins une combinaison éligible, des matières non vides et positions contiguës à partir de 1, aucun sujet sur une combinaison explicitement refusée. Publication directe à l’insertion interdite. Éligibilités et sujets d’une grille publiée immuables, y compris changement de parent ; verrou du parent pour coordonner écriture/publication. Le service refuse aussi les grilles incomplètes ou sujets désactivés. Mise à jour automatique des timestamps à introduire avec les futurs cas d’écriture, absents de cette API de lecture.

```mermaid
erDiagram
    AdmissionCampaign ||--o{ CampaignProgram : propose
    Program ||--o{ CampaignProgram : figure
    CampaignProgram ||--o{ RuleSetVersion : versionne
    RuleSetVersion ||--o{ ProgramQualificationEligibility : contient
    OriginQualification ||--o{ ProgramQualificationEligibility : origine
    ProgramQualificationEligibility ||--o{ ExpectedSubjectRule : attend
    Subject ||--o{ ExpectedSubjectRule : definit
```

Toutes les autres entités restent conceptuelles et ne possèdent aucune table ni endpoint dans cette phase.

## Schéma ajouté en phase 3

Migration 00003, migrations 00001/00002 inchangées. `candidates` : UUID, last_name/first_names/email/phone, created_at/updated_at ; chaînes vides autorisées pour brouillon incomplet, longueurs bornées, email trim/lower, téléphone vide ou syntaxe internationale. Email non unique : la possession d’un email ne donne pas un droit d’accès, aucune fusion automatique de personnes.

`applications` : UUID, candidate_id, campaign_id, campaign_program_id nullable, origin_qualification_id nullable, graduation_year nullable, state CHECK DRAFT/SUBMITTED/CANCELLED (seul DRAFT utilisable via API), version positive, timestamps. Unique candidat/campagne ; FK composite offre/campagne interdit croisements d’années ; filière nécessite qualification ; année CHECK 1900–9999, borne année courante validée en Go. Index campagne et index unique candidat/campagne ; aucun numéro officiel.

`application_draft_sessions` : UUID, application_id, secret_hash bytea 32 octets unique, expires_at, revoked_at nullable, created_at ; expiration après création, index application pour révocation future. Aucun secret brut en base. Autorisation par session, pas par application UUID. Profil, qualification et filière modifiables uniquement pour DRAFT/campagne ouverte dans des transactions verrouillant application/session ; contrôle de concurrence par version. Enregistrement de notes/document/OCR hors périmètre.
