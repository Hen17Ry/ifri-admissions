# IFRI Admissions — instructions pour Claude Code

Ce projet a été démarré avec un autre agent. Les règles historiques restent valables :

@AGENTS.md

Sources de vérité, à lire avant toute phase :
- Cahier des charges : `docs/CAHIER_DES_CHARGES.pdf`
- Produit : `docs/PRODUCT_SPEC.md`
- Architecture et modèle : `docs/ARCHITECTURE.md`, `docs/DATA_MODEL.md`
- Décisions déjà prises : `docs/DECISIONS.md` (ne pas les rediscuter sans raison)
- Parcours candidat : `docs/CANDIDATE_FLOW.md`, `docs/DOCUMENT_WORKFLOW.md`
- OCR : `docs/OCR_PIPELINE.md`, `docs/OCR_BENCHMARK.md`
- Reste à faire : `docs/TODO.md`, `docs/ROADMAP.md`
- Design : `design-system/ifri-admissions/MASTER.md` (+ `pages/candidate-application.md`)

## Stack

Vue 3 + TypeScript + Vite + Pinia + Tailwind + Reka UI / shadcn-vue (aucune dépendance React)
· API Go (REST `/api/v1`, routes internes `/internal/...`)
· PostgreSQL (migrations numérotées `000NN_*.sql`)
· Stockage S3-compatible privé (MinIO en local)
· Redis Streams (consumer group, livraison au moins une fois)
· Worker Python 3.14 (uv, PyMuPDF, Tesseract 5 fra+eng, OpenCV, RapidFuzz)
· Docker Compose.

## Règles non négociables

1. **Note et coefficient viennent exclusivement du relevé.** Jamais de coefficient configuré, par défaut ou codé en dur. Les règles métier ne déterminent que les matières attendues (qualification + filière).
2. **Le candidat ne modifie jamais** une matière, une note ou un coefficient extrait. Il confirme, ou rejette et fournit une nouvelle version du relevé.
3. **Une analyse OCR référence une `document_version_id` précise** et un contexte (programme, qualification, version de grille). Un changement de contexte ou de version la rend `OBSOLETE`. Aucun résultat n'est réutilisé silencieusement.
4. **Une note n'est acceptée que si elle est vérifiée** : `Points == Note × Coeff` sur la ligne, ou cohérence des totaux du tableau (Σ Coeff, Σ Points, Σ Sur). `Sur == 20 × Coeff` valide le coefficient, jamais la note. Ne jamais abaisser un seuil pour faire passer un test.
5. **Calculs métier en décimal exact** (NUMERIC / décimal Go). Jamais de float. La moyenne est calculée uniquement côté backend.
6. **Sécurité** : aucun IDOR (autorisation par session → application → ressource), bucket privé, pas de PII dans les logs, Redis ou les messages d'erreur, secrets jamais exposés au frontend.
7. **Migrations** : ne jamais modifier une migration déjà appliquée ; en créer une nouvelle.

## Données personnelles réelles

`services/ocr/tests/private-fixtures/` contient de vrais relevés. Ce dossier est gitignoré.
- Ne jamais commiter, copier dans une image Docker, ni recopier ces fichiers ou leurs données dans des fixtures publiques.
- Ne jamais afficher dans la conversation, les logs ou les rapports : nom, date de naissance, numéro de table, photo.
- Les rapports décrivent ces documents anonymement (« real scanned BAC D transcript »).
- Les images de debug vont dans `/tmp/...`, jamais dans le dépôt.

## Méthode de travail

- Une phase à la fois. Commencer en lisant les docs ci-dessus et l'état réel du code ; proposer un plan avant d'écrire du code.
- Ne pas réécrire une fondation validée par une phase précédente.
- Fin de phase : exécuter réellement toutes les validations (frontend build/typecheck/lint/format/Vitest, Go gofmt/vet/test, Python pytest/Ruff, migrations up/down/up, Docker healthy, Playwright), écrire `docs/PHASE<N>_VALIDATION.md`, mettre à jour `docs/TODO.md` et `docs/ROADMAP.md`.
- Ne jamais déclarer un test « réussi » sans l'avoir exécuté. Si quelque chose n'a pas pu être testé (navigateur, CI…), l'écrire explicitement.
- Ne pas commiter ni pousser : l'utilisateur fait les checkpoints Git lui-même.
- Interface en français, code et base en anglais.
