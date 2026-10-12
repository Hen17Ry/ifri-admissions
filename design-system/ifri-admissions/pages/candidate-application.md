# Parcours candidat — adaptations du master

MASTER.md reste la source principale : aucune nouvelle palette, police ou règle d’espacement.

Recherches UI UX Pro Max réellement exécutées : university admissions application, government institutional form/education workflow, multi-step form progress stepper, accessible form validation error summary, responsive forms, autosave feedback et stack Vue. La recommandation générée/persistée a proposé Portfolio Grid, puis Enterprise Gateway au retry : compositions inadaptées, écartées. Les recommandations vérifiées Government Portal (Accessible & Ethical), Focusable Error Summary, Progress Indicators et Mobile First sont retenues. Pas de correspondance spécifique fiable pour autosave ; adaptation produit basée sur feedback de sauvegarde, sans haptique.

- Contenu max 1120 px : formulaire max 760 px + aide contextuelle 256 px uniquement à partir de 1024 px. Mobile : une colonne, aide sous le contenu, marges du master.
- Bandeau institutionnel léger, nom de campagne, titre et explication ; pas de hero commercial ni grande carte isolée. Formulaire en sections avec rythme vertical 24/32 px.
- Cinq étapes depuis la phase 4. Liste ordonnée, aria-current=step, numéro/texte/état terminé. Mobile : numéros et nom courant, noms complets disponibles au lecteur d’écran ; boutons de retour pour étapes déjà accessibles.
- Radios natives dans des cartes à grande cible ; select natif pour sept qualifications, sans combobox complexe. Tous les champs ont labels/aides/erreurs reliés. Navigation native clavier conservée.
- Résumé d’erreurs lié aux champs et focus première erreur après validation ; titre focalisable aux changements d’étape. Une région status pour sauvegarde, aucun toast répété.
- Autosave après pause 800 ms pour valeurs valides ; écritures sérialisées, validation d’étape attend la sauvegarde. Enregistrement / Enregistré / Modifications non enregistrées explicites. Retour sans perte de saisie, backend source de vérité.
- Aucun quota affiché, aucune promesse d’admission. Vérification = récapitulatif du brouillon, sans bouton de soumission.
- Skeleton statique, chargement nommé, erreurs réseau avec réessai explicite, campagne fermée sans formulaire, expiration expliquée sans retry automatique.
- Transitions couleur/opacity seulement, 150 ms du master ; reduced-motion déjà global. Pas de mouvement de panneaux ou de GSAP.

Inspiration de composition uniquement : [stepper](https://21st.dev/blog/react-onboarding-stepper-components), [Choicebox](https://21st.dev/@shugar/components/choicebox-1), [radios accessibles](https://21st.dev/@a11ypros/components/a11y-radio/with-error). Total d’étapes visible, retour et progression lisibles, grandes zones de choix, fieldset/legend ; réimplémentation Vue sans code React ni asset copié.

## Adaptations Documents — phase 4

Master inchangé. Recherches UI UX Pro Max exécutées : secure university admission document upload, file upload validation errors, drag drop accessible, uploaded file card replace document, retry file upload feedback, PDF preview mobile upload et preview dialog accessibility. Résultats institutionnels University/Government Portal, erreurs proches du champ, récupération et Mobile First retenus. Aucune correspondance spécifique fiable pour replacement/preview ; résultats haptique/AI écartés, choix produit explicites ci-dessous.

- Quatre destinations distinctes en lignes/cartes légères, label obligatoire visible, format PDF et limite réelle API. Pas de dropzone globale ambiguë.
- Sélecteur natif utilisable au clavier ; drag-drop non nécessaire. Cibles et focus du master, erreurs liées au champ, annonce Envoi en cours sans pourcentage fictif.
- Nom long autorisé à revenir à la ligne ; actions Voir/Remplacer restent accessibles sur mobile. Le fichier courant reste visible jusqu’à succès du remplacement.
- Preview dans nouvel onglet natif annoncé au lecteur d’écran ; aucune modal ni viewer complexe. Résumé Documents reprend les mêmes libellés API.
- Progression cinq étapes ; document complet relu au serveur avant Vérification, aucune soumission. Transitions discrètes existantes et reduced-motion conservés.

Inspiration de composition : [guide uploads 21st.dev](https://21st.dev/blog/react-file-upload-components), [file upload card](https://docs.21st.dev/@sean0205/components/file-upload/card-upload). Réimplémentation Vue avec contrôles natifs ; aucun code React, package React ou asset repris.
