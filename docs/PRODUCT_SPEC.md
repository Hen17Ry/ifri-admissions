# Spécification produit

## Objet et périmètre
Dématérialiser dépôt, analyse des relevés, vérification administrative, calcul de moyenne, classement et présélection des admissions IFRI/UAC. Plusieurs campagnes annuelles indépendantes. Ce document décrit la cible ; cette livraison prépare uniquement les fondations.

## Campagnes et référentiels
`AdmissionCampaign` : nom, année, ouverture, fermeture, statut `DRAFT / OPEN / CLOSED / ARCHIVED`. Horodatages stockés en UTC, affichés en Africa/Porto-Novo ; hypothèse : intervalle d’ouverture [ouverture, fermeture[. Une campagne fermée ne reçoit plus de nouvelles soumissions ; vérification et sélection peuvent continuer.
Filières : Intelligence Artificielle, Sécurité Informatique, Génie Logiciel, Internet et Multimédia, Systèmes Embarqués et Internet des Objets (SEIoT).
Distinguer admissibilité (`EligibilityRule`) et matières attendues (`ExpectedSubjectRule`). Les règles sont versionnées, rattachées à une campagne et figées pour chaque analyse. Grille incomplète : ne pas ouvrir la combinaison aux soumissions finales.

### Matrice précisée en phase 2

L’instruction utilisateur du 12 octobre 2026 précise BAC C/D = Anglais et BAC E = Étude de Fabrication pour les cinq filières, ainsi que IA/SI et IM + DT/PM. La matrice détaillée et le contrat de lecture sont dans `ADMISSION_RULES.md` ; ils remplacent les ambiguïtés des exemples historiques ci-dessous. Ils ne constituent pas une campagne officielle de production ni des quotas validés.

### Exemples historiques de phase 0, non assimilés à une grille officielle complète
| Filière | Origine | Matières à rechercher |
|---|---|---|
| Génie Logiciel, Internet et Multimédia, SEIoT | BAC C/D/E | Mathématiques ; Français ; Anglais ou Étude de Fabrication selon série exacte |
| Génie Logiciel, Internet et Multimédia, SEIoT | DT/IMI | Mathématiques appliquées ; Français ; Technologie des systèmes informatiques |
| Génie Logiciel, Internet et Multimédia | DT/DWM | Mathématiques appliquées ; Français ; Sites et Applications Web |
| SEIoT | DT/EAP | Sciences appliquées ; Français ; Étude électronique |
| Sécurité Informatique, Intelligence Artificielle | Selon grille officielle | À compléter |

C, D et E devront être des entrées distinctes ; l’alternative du troisième sujet n’est pas résolue par ces exemples. Ne pas choisir arbitrairement l’un des deux. Aucun coefficient dans cette matrice.

## Parcours candidat
1. Accéder de manière authentifiée à son dossier (mécanisme candidat à décider).
2. Choisir une campagne ouverte ; saisir nom, prénoms, email, téléphone, diplôme/série exacts, année d’obtention et filière.
3. Déposer les quatre PDF : demande au Directeur IFRI, relevé BAC/DT, carte nationale ou CIP, engagement parental. Afficher présence, contrôle du fichier et version.
4. Déterminer les matières attendues puis lancer l’analyse asynchrone du relevé accepté par les contrôles de sécurité. Afficher attente, traitement, succès partiel, échec et action utile.
5. Afficher nom canonique, libellé détecté utile, note et coefficient, en lecture seule. Confirmation seulement si toutes les données attendues sont cohérentes ; absence ou ambiguïté = traitement bloqué.
6. Choix explicite « Les informations sont correctes » ou « Les informations sont incorrectes ». Le refus impose un nouveau relevé et invalide les résultats antérieurs. Après un nombre d’échecs à définir : revue manuelle.
7. Soumettre définitivement après validation des champs, des quatre documents, de l’admissibilité et de l’analyse active confirmée. Fournir référence et date de dépôt ; bloquer la modification ordinaire après soumission.
8. Consulter statut et motifs de correction. Une réouverture administrative autorise seulement les corrections demandées et une nouvelle soumission auditée.

Un changement de série/filière en brouillon relance la détermination des matières et l’analyse. Après dépôt, il nécessite une réouverture autorisée. Une confirmation référence exactement une version du document, de l’analyse et des règles.

## Calcul et classement
Go calcule la moyenne pondérée à partir des seules notes et coefficients du relevé actif accepté. Coefficients strictement positifs ; note numérique dans l’échelle documentée ; valeurs manquantes, doublons non résolus et somme nulle interdisent le calcul. Hypothèse à confirmer : notes sur 20, sans conversion automatique d’une autre échelle.
Exemple : (15×4 + 13×2 + 14×2)/(4+2+2) = 14,25. Les coefficients de cet exemple sont extraits, jamais configurés.
Moyenne indicative après confirmation, utilisable dans le classement final uniquement après vérification administrative. Conserver opérandes et provenance ; affichage à deux décimales proposé, calcul/classement avant arrondi.
Classement séparé par campagne et filière, des seuls dossiers dont la revue est validée, sur la moyenne effective exacte. Départage confirmé par l’IFRI (phase 9) : note de mathématiques, date de soumission, numéro. Snapshot immuable au gel, reproductible, lié aux moyennes et règles sources. Places et moyenne minimale par campagne/filière. La proposition est automatique ; le gel vaut validation explicite par le Directeur. Contrat : RANKING_SELECTION.
Décisions administratives : `SELECTED`, `WAITING_LIST`, `NOT_SELECTED`. `NOT_SELECTED` signifie admissible mais non retenu ; `REJECTED` signifie dossier administrativement rejeté.

## Administration et droits
Liste paginée : campagne, filière, série/diplôme, statuts, recherche candidat, tri moyenne. Fiche : identité, origine, filière, documents privés prévisualisables, matières/notes/coefficients, moyenne, statuts OCR et administratifs, provenance et historique.
| Action | REVIEWER | ADMIN | SUPER_ADMIN |
|---|---|---|---|
| Consulter les dossiers dans son périmètre | Oui | Oui | Oui |
| Vérifier, attester VERIFIED, demander correction motivée | Oui | Oui | Oui |
| Rejeter avec motif ; valider sélection | Non | Oui | Oui |
| Gestion opérationnelle des candidatures/classements | Non | Oui | Oui |
| Campagnes, référentiels, quotas, utilisateurs, configuration | Non | Non | Oui |

Cette répartition précise « lecture et vérification » du Reviewer, à confirmer avant implémentation RBAC. Aucun rôle ne configure de coefficients. La correction manuelle éventuelle d’une extraction doit provenir du PDF et être versionnée/auditée ; droits et approbation à décider avant cette fonction.

## Machines à états
Les noms demandés sont conservés, mais répartis pour éviter les états contradictoires.
| Axe | États |
|---|---|
| Dépôt (`Application`) | DRAFT, DOCUMENTS_UPLOADED, SUBMITTED |
| Analyse active (`OCRAnalysis`) | QUEUED, OCR_PROCESSING, OCR_FAILED, AWAITING_CONFIRMATION, NEEDS_CORRECTION, MANUAL_REVIEW, OCR_VALIDATED |
| Administration (`Application`) | NOT_STARTED, UNDER_REVIEW, NEEDS_CORRECTION, VERIFIED, REJECTED |
| Sélection (`RankingEntry`) | NOT_DECIDED, SELECTED, WAITING_LIST, NOT_SELECTED |

`OCR_VALIDATED` = analyse active acceptée par le candidat (ou circuit manuel explicitement défini), pas validation administrative. `NEEDS_CORRECTION` s’affiche avec son axe et son motif.

| Transition | Acteur et garde |
|---|---|
| DRAFT → DOCUMENTS_UPLOADED | Candidat ; informations et documents requis complets/acceptés |
| DOCUMENTS_UPLOADED → DRAFT | Document requis retiré ou information requise invalidée |
| QUEUED → OCR_PROCESSING → AWAITING_CONFIRMATION | Worker via Go ; résultat complet et cohérent |
| OCR_PROCESSING → OCR_FAILED | Erreur technique ou extraction inutilisable, motif explicite |
| AWAITING_CONFIRMATION → OCR_VALIDATED | Candidat propriétaire ; confirmation de l’analyse courante |
| AWAITING_CONFIRMATION → NEEDS_CORRECTION | Candidat ; refus enregistré, nouveau PDF requis |
| OCR_FAILED/NEEDS_CORRECTION → MANUAL_REVIEW | Politique d’échecs à définir ; aucune validation automatique |
| Nouveau PDF → nouvelle analyse QUEUED | Candidat si modification autorisée ; anciennes analyses conservées et rendues inactives |
| DOCUMENTS_UPLOADED → SUBMITTED | Candidat ; campagne ouverte, admissible, OCR_VALIDATED actif, moyenne calculable |
| NOT_STARTED → UNDER_REVIEW | Reviewer/Admin ; dossier SUBMITTED |
| UNDER_REVIEW → VERIFIED/NEEDS_CORRECTION | Reviewer/Admin ; décision motivée, contrôles complets |
| UNDER_REVIEW → REJECTED | Admin ; rejet motivé |
| NEEDS_CORRECTION → UNDER_REVIEW | Nouvelle soumission complète après réouverture administrative |
| VERIFIED → UNDER_REVIEW | Admin ; réouverture motivée, invalidation classement/sélection |
| NOT_DECIDED → SELECTED/WAITING_LIST/NOT_SELECTED | Admin ; classement courant et dossiers toujours VERIFIED |

Réouverture : dépôt repasse en DRAFT, administration reste NEEDS_CORRECTION jusqu’à nouvelle soumission ; délai de correction hors campagne à définir, bloqué par défaut. Rejet terminal hors procédure exceptionnelle à spécifier. Toute transition non listée est refusée ; concurrence contrôlée par version du dossier. L’interface présente un statut principal dérivé et le détail de chaque axe.

## Critères transversaux
Contrôles serveur, RBAC et anti-IDOR ; documents privés ; audit ; accessibilité WCAG 2.2 AA ; navigation clavier ; responsive 375/768/1024/1440 ; reprise de brouillon et feedback réseau. Délais, taille PDF, seuil de confiance, nombre d’essais, quotas et conservation sont des politiques à déterminer, pas des constantes inventées.
