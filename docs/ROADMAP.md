# Feuille de route — IFRI Admissions

À placer dans `docs/ROADMAP.md`. Claude Code la met à jour à la fin de chaque phase.

## État au 14 octobre 2026

| Phase | Contenu | État |
|---|---|---|
| 1 | Fondations : monorepo, Docker, CI locale, design system | Fait |
| 2 | Référentiels : campagnes, filières, qualifications, matières, grilles, éligibilité | Fait |
| 3 | Candidat + brouillon + session sécurisée + wizard | Fait |
| 4 | Documents PDF : upload, validation, S3 privé, versionnement, preview | Fait |
| 5 | OCR asynchrone, extraction, confirmation candidat (fixtures synthétiques) | Fait |
| **5.1** | **Analyse automatique à l'upload + OCR fiable sur relevés réels scannés** | **Clôturée le 14 octobre 2026** — réserve : corpus réel ≥ 10 relevés requis avant la production |
| **6** | **Moyenne pondérée, soumission atomique, numéro, verrouillage** | **Fait le 14 octobre 2026** (voir PHASE6_VALIDATION) |
| **7** | **Authentification admin, RBAC, journal d'audit** | **Fait le 14 octobre 2026** (voir PHASE7_VALIDATION) |
| **8** | **Back-office : liste, filtres, dossier, preuves OCR, vérification, corrections** | **Fait le 14 octobre 2026** (voir PHASE8_VALIDATION) |
| 9 | Classement par filière, quotas, sélection, liste d'attente, exports | À faire |
| 10 | Mise en production : CI, HTTPS/Nginx, sauvegardes, antivirus, rétention, observabilité | À faire |

## Questions à faire trancher par l'IFRI

Elles bloquent la mise en production, pas le développement. Chaque règle est isolée dans le code pour être modifiable facilement.

1. **Écrit ou oral** : quand une matière (Anglais, Français) apparaît à l'écrit du 1er groupe et à l'oral du 2e groupe, quelle note retenir ? Hypothèse actuelle : écrit du 1er groupe.
2. **Matrice des matières** : confirmer officiellement la matrice qualification × filière (série A, B, F… non couvertes ?).
3. **Arrondi et égalités** : précision de la moyenne et règle de départage en cas d'égalité. (Constat sur un relevé : l'Office du Bac imprime une moyenne tronquée à 2 décimales ; ce n'est pas la règle IFRI.)
4. **Quotas** : nombre de places par filière et par campagne.
5. **Formats DT** : obtenir des relevés DT réels (IMI, DWM, EAP, PM) anonymisés ; leur mise en page est encore inconnue.
6. **Motifs de rejet** : valider ou ajuster la liste provisoire des motifs de rejet d'un dossier (document illisible, document invalide ou erroné, identité non concordante, relevé incohérent, qualification non éligible, candidature en double, suspicion de fraude, autre motif). Ils sont définis à un seul endroit, avec leurs libellés (table `review_rejection_reasons`, ADR 112).
7. **Dates de campagne** et politique de conservation des documents après la campagne.
8. **Doublons** : que faire de deux candidatures d'une même campagne avec le même email, le même téléphone ou le même relevé ? Elles sont aujourd'hui signalées à l'administration sans être bloquées.
9. **Échecs d'analyse répétés** : après combien d'échecs proposer une vérification manuelle du relevé, et qui la fait ? Le modèle de données est prêt (ADR 092).
10. **Année des numéros** : confirmer l'année de numérotation de chaque campagne réelle (`numbering_year`, une seule année même si la campagne couvre deux années civiles).
11. **Conservation des traces** : durée de conservation du journal d'audit et des adresses IP pseudonymisées (HMAC) des connexions administrateurs. Ce sont des données personnelles.
12. **Comptes administrateurs** : qui crée les comptes, faut-il une réinitialisation du mot de passe par email (aujourd'hui seulement par une commande sur le serveur) et un second facteur matériel (clé de sécurité) ?
13. **Relevés réels** : au moins 10 relevés BAC C/D/E et DT anonymisés, **requis avant la production** pour mesurer la précision réelle ; ils confirmeront aussi la zone de la lettre de série (grande lettre en haut à gauche, observée sur un seul relevé) et la troncature de la moyenne imprimée.

## Phase 5.1 — critères de sortie (clôture du 14 octobre 2026, voir PHASE5_1_VALIDATION)

- [x] Plus de bouton « Analyser mon relevé » ; wizard en 5 étapes ; analyse affichée dans la carte du relevé.
- [x] Upload ou remplacement du relevé → analyse automatique ; refresh → restauration + reprise du polling.
- [x] Profil `BENIN_BAC_GENERAL` : section « Épreuves écrites du premier groupe », grille de cellules, ligne Total (18 cas synthétiques réalistes, dont 2 de bout en bout HTTP).
- [x] Règle d'acceptation : note validée par Points sur sa ligne, ou par les totaux ; sinon `NEEDS_REVIEW` / `GRADE_POINTS_MISMATCH` (ADR 079–081, revérifiée par Go).
- [x] Le relevé réel privé donne les bonnes valeurs, ou échoue proprement sans fausse valeur — **échec propre** (`GRADE_POINTS_MISMATCH`, 13,0 s, 0 valeur fausse avec les deux lecteurs de chiffres) : Français et Anglais validés exacts par leur rangée, note de Mathématiques mal lue et détectée ; série lue exacte.
- [x] Aucune valeur fausse acceptée sur le corpus privé (y compris au niveau des rangées) et sur le synthétique — vrai, mais corpus réel = 1 relevé.
- [x] `make check` vert.
- **Réserve avant production** : corpus privé d'au moins 10 relevés réels anonymisés (BAC C, D, E et DT) évalué dans `OCR_BENCHMARK.md` — dépend de l'IFRI ; harnais prêt (`make ocr-private`).

## Phase 6 — critères de sortie (14 octobre 2026, voir PHASE6_VALIDATION)

- [x] Soumission atomique : verrou, revalidation complète en base, calcul, numéro, snapshot, drapeaux, audit et `SUBMITTED` dans une seule transaction ; rollback total vérifié par injection de panne.
- [x] Codes distincts pour chaque précondition, testés sur PostgreSQL réel ; `ADMISSION_RULES_CHANGED` rend l'analyse obsolète et en relance une.
- [x] Moyenne exacte en `big.Rat` : fraction stockée exactement, colonne à 30 décimales tronquée et `average_is_exact` ; jamais exposée au candidat.
- [x] Numéro `IFRI-{ANNÉE}-{PROGRAMME}-{SERIAL6}` unique et concurrency-safe ; idempotence prouvée par 8 soumissions concurrentes d'un même dossier (un numéro, un snapshot).
- [x] Snapshot et dossier soumis immuables en base ; `APPLICATION_LOCKED` sur toutes les routes candidat ; session en lecture seule prolongée.
- [x] Drapeaux non bloquants (email, téléphone, relevé, série) ; audit `APPLICATION_SUBMITTED` sans PII.
- [x] UI : récapitulatif sans moyenne, dialogue natif `showModal()`, messages par code, confirmation avec copie et impression ; Vitest et Playwright jusqu'à `SUBMITTED`, refresh et mutation refusée compris.
- [x] Migrations up/down/up ; down refusé s'il existe des soumissions ; `make check` vert.

## Phase 7 — critères de sortie (14 octobre 2026, voir PHASE7_VALIDATION)

- [x] Préalable : Playwright sur une stack jetable isolée (`make e2e`, `make check-fast`), sans soumission dans la base de dev ; `numbering_year` du seed = année courante, gelé dès le premier numéro.
- [x] Comptes : aucune inscription publique, premier super-administrateur par CLI, liens d'activation à usage unique (fragment, 72 h, SHA-256), comptes désactivables jamais supprimés, dernier super-administrateur protégé.
- [x] argon2id documenté, politique de 12 caractères et plus, refus des mots de passe courants ; TOTP RFC 6238 (±1 pas, anti-rejeu), codes de secours hachés, réinitialisation auditée ; secours `reset-mfa`, `reactivate`, `reset-password`.
- [x] Sessions séparées, rotation, expirations, révocations ; CSRF Origin + jeton ; limitation par IP et identifiant avant argon2 ; non-énumération.
- [x] RBAC par permissions avec périmètre par filière ; refus par défaut et matrice routes × rôles testée ; permissions des phases 8 et 9 déclarées.
- [x] Audit de chaque action sensible, sans identifiant ni IP en clair ; `RecordAccess` prêt pour la phase 8.
- [x] Interface `/admin` (UI UX Pro Max, `pages/admin.md`), en-têtes stricts ; Vitest et Playwright (CLI, TOTP, création, activation, désactivation).
- [x] `make check` vert ; ADMIN_SECURITY et PHASE7_VALIDATION.

## Phase 8 — critères de sortie (14 octobre 2026, voir PHASE8_VALIDATION)

- [x] Rôles IFRI (agent, responsable, directeur adjoint, directeur, administrateur technique), un seul rôle par compte contrôlé en Go et par trigger ; `SELECTION_MANAGER` et `AUDITOR` retirés ; `audit.read` pour le Directeur ; alerte de couverture DA / Directeur.
- [x] Tableau de bord : compteurs filière × statut, corrections en attente, dossiers non pris en charge, mes dossiers.
- [x] Liste : pagination et tri serveur (moyenne, date, nom), recherche sans accents, par numéro et par email, filtres (filière, série, statut, agent, signalement, correction en attente), moyenne tronquée avec mention ; 5 000 dossiers synthétiques, index, temps mesurés.
- [x] Dossier : identité, série déclarée et lue, aperçu des 4 PDF par flux authentifié, doublons dans les deux sens, historique, preuves OCR à la demande (file et boucle séparées, version exacte du document, coordonnées stockées).
- [x] Revue séparée et historisée (`applications.state` inchangé), prise en charge, validation et rejet par l'agent en charge, motif fermé, réattribution et retour arrière par le responsable, crochet de gel, concurrence optimiste.
- [x] Corrections : proposition par l'agent en charge, décision par le Directeur adjoint seul, quatre yeux, validation bloquée tant qu'une correction attend, snapshot intact, moyenne effective exacte.
- [x] Audit de chaque consultation (dossier, document, preuve) et de chaque action.
- [x] Tests Go (matrice, incompatibilités de rôles, concurrence, quatre yeux, moyenne exacte, audit, doublons, performance), Vitest, Playwright (deux agents et un DA, aperçu PDF, preuve OCR) ; `make check` vert ; ADMIN_REVIEW et PHASE8_VALIDATION.

## Phases 9 et 10 — périmètre

**9. Classement et sélection** : classement par filière des seuls dossiers `VALIDATED` et admissibles, sur la **moyenne effective** de la revue (ADR 114 : fraction exacte après corrections approuvées ; colonne à 30 décimales, puis égalité tranchée par produit en croix, ADR 086), départage selon la règle IFRI, proposition automatique selon les quotas, validation par le Directeur (`selection.validate`), liste d'attente, gel du classement par `ranking_freezes` (crochet posé en phase 8, ADR 115), exports CSV/PDF.

**10. Production** : CI GitHub (lint, tests, migrations, build des images), Nginx + HTTPS, sauvegardes PostgreSQL et S3 testées par restauration, analyse antivirus et quarantaine des uploads, nettoyage des objets orphelins, rétention, logs structurés et métriques, audit d'accessibilité, test de charge sur la période de clôture, plan de déploiement.
