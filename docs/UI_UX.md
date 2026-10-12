# Direction UI/UX

## Démarche réellement exécutée
Skill disponible : `.agents/skills/ui-ux-pro-max/SKILL.md`, scripts Python opérationnels. Produit web hybride : formulaire candidat et outil administratif documentaire, langue initiale française. Aucune maquette ni interface n’est implémentée.

Commandes exécutées depuis la racine :
```bash
python3 .agents/skills/ui-ux-pro-max/scripts/search.py "institutional university admission administrative document workflow" --design-system -p "ifri-admissions" -f markdown
python3 .agents/skills/ui-ux-pro-max/scripts/search.py "university admissions dashboard" --design-system -p "ifri-admissions" -f markdown
python3 .agents/skills/ui-ux-pro-max/scripts/search.py "professional readable dashboard" --domain typography -n 2
python3 .agents/skills/ui-ux-pro-max/scripts/search.py "error summary validation" --domain ux -n 3
python3 .agents/skills/ui-ux-pro-max/scripts/search.py "forms accessible dashboard" --stack vue -n 3
python3 .agents/skills/ui-ux-pro-max/scripts/search.py "institutional university admission administrative document workflow" --design-system --persist -p "ifri-admissions" --output-dir /home/h-ze-dev/Documents/ifri-admissions
```
La première recherche apporte une base Minimalism & Swiss Style et marine ; ses sections « Contact Sales », vidéo et logos ne correspondent pas aux admissions. Le second pattern Portfolio Grid n’est pas pertinent non plus. Aucune correspondance de layout vérifiée dans ces sorties ; les compositions suivantes sont des décisions produit explicites, appuyées sur les règles générales du skill. Les deux polices sérif proposées sont remplacées par Corporate Trust (Lexend + Source Sans 3) de la recherche complémentaire. La recommandation de validation Vue ne vaut pas adoption d’une bibliothèque : à décider au formulaire, sans dupliquer la validation serveur.
Persistance réalisée suivant le mécanisme master/pages ; master édité en synthèse vérifiée. Source de vérité : `design-system/ifri-admissions/MASTER.md`. Aucun override de page créé avant sa conception. Lire d’abord master, puis override éventuel ; ce dernier prime pour sa page seulement.

## Direction choisie
Institutionnelle, contemporaine, calme, premium par la qualité de composition et de finition. Fond clair, surfaces blanches, marine pour navigation et action principale ; bleu pour liens, vert réservé au succès, ocre aux alertes. Les couleurs sont provisoires, pas présentées comme une charte IFRI officielle. Logos officiels à obtenir, ne pas en inventer.
Lexend pour titres, Source Sans 3 pour texte/UI ; fontes locales au futur bootstrap après vérification des licences, fallback système. Chiffres tabulaires pour notes et moyennes. Pas de hero vidéo, gradient violet, effet verre, animation permanente ni chiffres décoratifs.

## Compositions proposées
Candidat : bandeau institution/campagne + progression « Informations → Documents → Vérification du relevé → Soumission ». Une action principale par étape, sauvegarde et retour explicites. Récapitulatif avant dépôt final. Upload par type, bouton natif accessible en plus du glisser-déposer ; statut sécurité puis statut analyse distincts. Erreur d’extraction : expliquer ce qui manque et proposer le remplacement, pas un éditeur de notes.
Confirmation : table en lecture seule matière/note/coefficient ; les deux choix explicites, aucun champ modifiable ni valeur cachée corrigible. Afficher provenance du relevé et avertissement concret si extraction incomplète ; progression sans pourcentage inventé. Revue manuelle clairement annoncée.
Administration : navigation latérale sobre sur grand écran, menu accessible sur petit écran ; campagne toujours visible ; filtres/recherche, table paginée et tri explicite. Fiche en sections identité/documents/extraction/revue/historique. À partir de 1024 px si espace suffisant : PDF et données côte à côte ; sur mobile, sections séquentielles. Actions sensibles séparées du défilement, motif obligatoire, confirmation des conséquences.
Classement : filière + campagne + version/état de classement + quota visibles ; proposition distincte de décision administrative. Aucun bouton ambigu « valider tout » sans résumé exact des conséquences.

## Inspiration 21st.dev
Pages réellement consultées : [cards](https://21st.dev/community/components/s/card) et [navigation](https://21st.dev/community/components/s/navigation-menu). Elles proposent des compositions React ; reprendre seulement l’idée de groupes en cards sobres et de navigation structurée, adaptées au dossier/document. Aucune évaluation visuelle exhaustive des previews interactives dans cette phase. Le catalogue [21st.dev](https://21st.dev/) confirme son orientation React ; aucune installation de registry React, aucun TSX importé. Pour les prochains écrans : examiner un exemple précis, conserver sa référence et recréer le pattern en Vue avec shadcn-vue/Reka ; vérifier licence si asset ou code repris.

## Accessibilité et états
Objectif WCAG 2.2 AA : contraste texte ≥ 4,5:1, grands textes et éléments non textuels utiles ≥ 3:1 ; focus visible non masqué, navigation clavier, lien d’évitement, titres hiérarchisés et libellés permanents. Boutons web visés 44×44 CSS px pour confort tactile ; ne pas confondre px web et unités natives du skill.
Erreurs sous chaque champ, `aria-describedby`/`aria-invalid` ; après soumission échouée, résumé focalisable avec liens aux champs. Statuts annoncés via région live modérée, sans déplacer le focus pendant OCR. Dialogues avec focus contenu/restitué, Escape et action annuler ; aucune interaction uniquement au survol.
Chargement : skeleton réservé sans saut de layout, réduit ou statique si reduced-motion ; erreur réseau conserve les données et permet retry ; état vide explique cause et prochaine action ; « aucun dossier » distinct d’un filtre sans résultat. Toast seulement en complément des informations persistantes nécessaires.
Vérifier 375/768/1024/1440 px, zoom 200 %, clavier, lecteur d’écran, labels longs, réseau lent et prefers-reduced-motion. Éviter défilement horizontal global ; table mobile présentée en lignes empilées ou région nommée contrôlée si indispensable. Aucun test d’interface n’est revendiqué avant son implémentation.

## Mise en œuvre technique de phase 1
`apps/web/src/pages/TechnicalPage.vue` reprend les tokens et compositions simples du master, sans second design system ni override. Consultation du skill et recherches `status live screen reader` (domaine ux) et `composition typescript` (stack vue) exécutées avant création. L’annonce du résultat est contextualisée dans une seule région status ; chaque dépendance a un libellé et un état textuel, aucune disponibilité inventée. Button natif, focus visible, lien d’évitement et reduced-motion.
Fontsource Lexend 5.3.0 et Source Sans 3 5.3.1 sont déclarés pour servir les fontes localement ; leurs licences OFL-1.1 ont été vérifiées via les sources Fontsource. Aucun appel Google Fonts. Prettier exécuté ; le rendu réel, les quatre largeurs, le clavier et les tests de composant attendent l’installation npm, bloquée dans cet environnement. Aucune conformité UI intégrale revendiquée.
