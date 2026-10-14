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
| 6 | Moyenne pondérée, soumission atomique, numéro, verrouillage | À faire |
| 7 | Authentification admin, RBAC, journal d'audit | À faire |
| 8 | Back-office : liste, filtres, dossier, preuves OCR, vérification | À faire |
| 9 | Classement par filière, quotas, sélection, liste d'attente, exports | À faire |
| 10 | Mise en production : CI, HTTPS/Nginx, sauvegardes, antivirus, rétention, observabilité | À faire |

## Questions à faire trancher par l'IFRI

Elles bloquent la mise en production, pas le développement. Chaque règle est isolée dans le code pour être modifiable facilement.

1. **Écrit ou oral** : quand une matière (Anglais, Français) apparaît à l'écrit du 1er groupe et à l'oral du 2e groupe, quelle note retenir ? Hypothèse actuelle : écrit du 1er groupe.
2. **Matrice des matières** : confirmer officiellement la matrice qualification × filière (série A, B, F… non couvertes ?).
3. **Arrondi et égalités** : précision de la moyenne et règle de départage en cas d'égalité. (Constat sur un relevé : l'Office du Bac imprime une moyenne tronquée à 2 décimales ; ce n'est pas la règle IFRI.)
4. **Quotas** : nombre de places par filière et par campagne.
5. **Formats DT** : obtenir des relevés DT réels (IMI, DWM, EAP, PM) anonymisés ; leur mise en page est encore inconnue.
6. **Rôles admin** : qui peut vérifier, qui peut valider la sélection finale, faut-il une double validation ?
7. **Dates de campagne** et politique de conservation des documents après la campagne.
8. **Relevés réels** : au moins 10 relevés BAC C/D/E et DT anonymisés, **requis avant la production** pour mesurer la précision réelle ; ils confirmeront aussi la zone de la lettre de série (grande lettre en haut à gauche, observée sur un seul relevé) et la troncature de la moyenne imprimée.

## Phase 5.1 — critères de sortie (clôture du 14 octobre 2026, voir PHASE5_1_VALIDATION)

- [x] Plus de bouton « Analyser mon relevé » ; wizard en 5 étapes ; analyse affichée dans la carte du relevé.
- [x] Upload ou remplacement du relevé → analyse automatique ; refresh → restauration + reprise du polling.
- [x] Profil `BENIN_BAC_GENERAL` : section « Épreuves écrites du premier groupe », grille de cellules, ligne Total (18 cas synthétiques réalistes, dont 2 de bout en bout HTTP).
- [x] Règle d'acceptation : note validée par Points sur sa ligne, ou par les totaux ; sinon `NEEDS_REVIEW` / `GRADE_POINTS_MISMATCH` (ADR 079–081, revérifiée par Go).
- [x] Le relevé réel privé donne les bonnes valeurs, ou échoue proprement sans fausse valeur — **échec propre** (`GRADE_POINTS_MISMATCH`, 13,0 s, 0 valeur fausse avec les deux lecteurs de chiffres) : Français et Anglais validés exacts par leur rangée, note de Mathématiques mal lue et détectée ; série lue exacte.
- [x] Aucune valeur fausse acceptée sur le corpus privé (y compris au niveau des rangées) et sur le synthétique — vrai, mais corpus réel = 1 relevé.
- [x] `make check` vert.
- **Réserve avant production** : corpus privé d'au moins 10 relevés réels anonymisés (BAC C, D, E et DT) évalué dans `OCR_BENCHMARK.md` — dépend de l'IFRI ; harnais prêt (`make ocr-private`).

## Phase 6 — à adapter

Le prompt de phase 6 rédigé précédemment suppose un wizard en 6 étapes avec « Analyse du relevé ». Il faut le corriger : 5 étapes, l'analyse confirmée se trouve dans l'étape Documents.

## Phases 7 à 10 — périmètre

**7. Admin, sécurité** : comptes admin (pas d'inscription publique), mots de passe hashés (argon2id), sessions séparées des sessions candidat, rôles (ex. `REVIEWER`, `SELECTION_MANAGER`, `SUPER_ADMIN`), rate limiting de la connexion, journal d'audit de toutes les actions admin.

**8. Back-office** : tableau par filière (rang, candidat, série, moyenne, statut), recherche par nom, filtres par série et statut, fiche dossier, prévisualisation des PDF, preuves OCR (crop de la ligne extraite à côté des valeurs), statuts administratifs (`EN_VERIFICATION`, `VALIDE`, `REJETE`), correction admin tracée comme un override séparé qui ne modifie jamais le snapshot de soumission.

**9. Classement et sélection** : classement par filière sur la moyenne interne non arrondie, départage selon la règle IFRI, proposition automatique selon les quotas, validation administrative, liste d'attente, gel du classement, exports CSV/PDF.

**10. Production** : CI GitHub (lint, tests, migrations, build des images), Nginx + HTTPS, sauvegardes PostgreSQL et S3 testées par restauration, analyse antivirus et quarantaine des uploads, nettoyage des objets orphelins, rétention, logs structurés et métriques, audit d'accessibilité, test de charge sur la période de clôture, plan de déploiement.
