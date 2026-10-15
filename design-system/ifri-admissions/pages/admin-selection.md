# Classement et sélection — adaptations du master (phase 9)

MASTER.md, `pages/admin.md` et `pages/admin-review.md` restent la référence :
- mêmes tokens, typographie, cibles de 44 px et dialogues natifs ;
- tableaux qui passent en cartes sous 768 px ;
- statuts en texte + pastille.

Cette page ne fixe que la composition de `/admin/classement`.

## Recherches UI UX Pro Max exécutées

- **`ux`**, recommandations retenues :
  - « ranking table sortable rank highlight threshold line » → *Table Handling* : cartes sur mobile, jamais de tableau qui casse la page ; mesure des lignes bornée ;
  - « irreversible action confirmation » → *Confirmation Dialogs* :
    - gel, désistement et promotion en `alertdialog`, focus initial « Annuler » ;
    - *Success Feedback* : un message explicite après chaque action, avec la nouvelle version de la liste ;
  - « status badge color meaning » → *Color Only* : décision en texte (Admis, Liste d'attente, Désisté, Non classé) avec pastille, désisté barré en plus du texte ; une seule région `status` atomique pour les annonces ;
  - « live region polling progress » → *Progress Indicators* : « génération en cours… » tant que le PDF n'est pas prêt, puis lien de téléchargement.
- **`vue`** « async action loading state polling interval cleanup » → arrêt du polling au démontage (`onBeforeUnmount`), données chargées dans `onMounted` et au changement de filière (`watch` sur la sélection).
- **Écartés** : *Haptic Feedback*, *AI Interaction*, *Navigation history* (sans objet) ; aucun tri côté client : l'ordre est celui du serveur, seul juge du classement.

## Composition

- **En-tête** :
  - choix « Filière et campagne », conservé dans l'URL (`?offre=`) ;
  - bandeau d'état « Classement provisoire, recalculé à chaque affichage » (liseré `warning`) ou « Classement gelé et validé … liste version N » (liseré marine, cadenas).
- **Places et moyenne minimale** : formulaire du Directeur tant que le classement n'est pas gelé ; lecture seule sinon.
- **Gel et validation** :
  - liste des conditions non remplies, chacune en clair avec son nombre, et un lien vers les dossiers à traiter ;
  - bouton « Geler et valider le classement » désactivé et relié à cette liste par `aria-describedby` ;
  - dialogue de confirmation récapitulatif (places, seuil, admis, liste d'attente, non classés), qui dit que le gel est **définitif**.
- **Désistements et promotions** (Directeur adjoint, après le gel) : places libres, puis encadré « Prochain candidat de la liste d'attente » avec un seul bouton « Promouvoir ce candidat ». On ne choisit jamais qui promouvoir : l'ordre est imposé.
- **Exports** (Directeur, Directeur adjoint, après le gel) :
  - titre avec la version de la liste ;
  - CSV publics et internes en liens de téléchargement directs ;
  - PDF générés à la demande, avec leur état et leur lien.
- **Tableau** :
  - colonnes : rang, numéro (lien vers le dossier), candidat, série, moyenne tronquée, note de mathématiques, date de soumission, critère de départage, décision ;
  - ligne pleine largeur « Limite des N places » après le dernier rang dans les places ;
  - séparateur « Non classés : moyenne effective inférieure à … » ;
  - mention « Maths non comparées : candidat sans matière mathématique dans le groupe » sous le critère quand le groupe d'égalité l'a sautée.
- **Historique de la liste** : frise version par version (gel, désistements avec motif, promotions, auteur et date).

## Dérogations motivées

- Les lignes « limite des places » et « non classés » sont des lignes de tableau pleine largeur (`colspan`), sans libellé de carte sur mobile : ce sont des séparateurs, pas des données.
- Le bouton « Désistement » est en variante secondaire dans le tableau : la confirmation, elle, est un `alertdialog` en variante `danger`. On garde ainsi un tableau calme malgré des dizaines de lignes.

## Accessibilité et responsive

- Noms accessibles qui reprennent le texte visible (« Désistement : IFRI-… »).
- Annonces atomiques dans une seule région `status`.
- Focus rendu au déclencheur à la fermeture des dialogues.
- Vérifié par Playwright à 375, 768, 1024 et 1440 px, sans défilement horizontal :
  - classement provisoire (Directeur) ;
  - classement gelé (Directeur) ;
  - désistements et promotions (Directeur adjoint).
