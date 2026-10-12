# IFRI Admissions — Master design system

Source de vérité UI du projet. Version initiale, 2026-10-12. Généré via UI UX Pro Max `--design-system --persist`, puis adapté et vérifié au regard du produit ; démarche dans `docs/UI_UX.md`. Overrides autorisés dans `pages/<page>.md`, uniquement pour la page concernée et avec raison explicite. Aucun écran créé à ce stade.

## Intentions et décisions
Minimalism & Swiss Style : espace, grille, hiérarchie, contraste et interactions sobres. Palette marine issue de la recommandation initiale. Le layout commercial Enterprise Gateway et le retry Portfolio Grid sont écartés, faute de correspondance au workflow documentaire. Composition produit choisie : parcours candidat par étapes et administration par liste/fiche. Typographie Corporate Trust de la recherche complémentaire : Lexend / Source Sans 3 ; remplace EB Garamond / Crimson Text pour la lisibilité des formulaires et tables.
Identité provisoire, sans prétention de charte IFRI officielle. Thème clair initial ; mode sombre différé jusqu’à validation séparée de tous ses contrastes.

## Tokens couleur — valeurs de référence
| Token sémantique | Valeur | Usage |
|---|---|---|
| primary / on-primary | #1E3A5F / #FFFFFF | Action principale, navigation sélectionnée |
| link / on-link | #2563EB / #FFFFFF | Liens et actions secondaires fortes |
| background / foreground | #F8FAFC / #0F172A | Fond global et texte |
| surface / on-surface | #FFFFFF / #0F172A | Cards, formulaires, tableaux |
| muted / on-muted | #E9EEF5 / #475569 | Fonds secondaires et texte secondaire |
| divider | #CBD5E1 | Séparation décorative, pas seule indication de contrôle |
| control-border | #64748B | Limite visible des champs sur fond blanc |
| success / on-success | #15803D / #FFFFFF | Succès accompagné de texte/icône |
| warning / on-warning | #A16207 / #FFFFFF | Alerte accompagnée de texte/icône |
| destructive / on-destructive | #DC2626 / #FFFFFF | Rejet/erreur explicite |
| focus-ring | #1E3A5F | Anneau focus 3 px, offset 2 px |

Le vert CTA initial #16A34A est remplacé par #15803D pour utiliser un texte blanc lisible ; l’action principale reste marine, le vert indique uniquement le succès. Les composants consomment des tokens sémantiques, jamais des hex locaux. Sur surface sombre, focus clair à définir/tester ; un anneau marine ne suffit pas.

## Typographie, dimensions et mouvement
- Titres Lexend 600/700 ; corps Source Sans 3 400/600 ; fallback sans-serif système. Auto-hébergement envisagé ; pas d’appel Google Fonts depuis les dossiers sensibles.
- Échelle : 12 px métadonnées rares, 14 px libellés secondaires, 16 px corps/champs, 18/24/32 px titres. Interligne corps 1,5 ; texte explicatif environ 65–75 caractères par ligne. Chiffres tabulaires pour colonnes numériques.
- Espacement : 4, 8, 12, 16, 24, 32, 48, 64 px. Rayon : 6 px champs/boutons, 12 px cards/dialogues ; badge arrondi seulement pour statut bref.
- Bordures 1 px ; ombre discrète réservée overlays (référence 0 8px 24px rgba(15,23,42,0.12)), pas de décoration systématique.
- Contrôles visés hauteur ≥ 44 px, icônes Lucide Vue 16/20/24 px, trait cohérent 2 px ; icônes décoratives aria-hidden, boutons icône nommés.
- Mouvement : feedback opacity/couleur 120–180 ms ; overlays 180–220 ms ; pas d’animation bloquante. Reduced-motion supprime transformations et skeleton animé. Aucune dépendance GSAP nécessaire.
- Z-index : base 0, header 10, menu 20, overlay 40, notification 50 ; focus toujours dégagé.

## Layout responsive
| Largeur de vérification | Comportement |
|---|---|
| 375 px | Une colonne, marges 16 px, étapes compactes nommées, menu replié, pas de scroll global horizontal |
| 768 px | Marges 24 px, champs en deux colonnes si leur sens le permet ; fiche encore séquentielle |
| 1024 px | Navigation admin latérale ~240 px, fiche PDF/données en deux panneaux seulement si lisibles |
| 1440 px | Marges 32 px, contenu max ~1280 px ; formulaires max ~760 px, tables/panneaux plus larges |

Ces valeurs guident la conception ; ne pas forcer une double colonne qui rend des libellés/documents illisibles. Aucun override page nécessaire avant réalisation.

## Contrats de composants
| Composant | Règle |
|---|---|
| Bouton | Une action principale par section/étape ; pending nommé et soumission doublée empêchée ; désactivation expliquée |
| Champ | Label permanent, aide, erreur liée ; serveur fait autorité ; données préservées après échec |
| Upload PDF | Type attendu, limites affichées lorsqu’elles sont fixées ; browse natif et drag/drop ; progression réelle, état contrôle distinct de l’OCR |
| Stepper | Étape courante annoncée (`aria-current=step`), étapes nommées ; possibilité de retour selon règles métier |
| Table extraction | Matière/note/coefficient en lecture seule, pas de faux inputs ; provenance et données absentes explicites |
| Table admin | Headers sémantiques, `aria-sort`, pagination et filtres nommés ; tri serveur ; mobile lisible |
| Badge | Texte + couleur ; distinguer OCR, vérification et sélection |
| Dialogue | Titre, description, focus contenu/restitué, Escape, annuler ; motif pour action sensible |
| Empty/error/skeleton | Cause, action utile et stabilité layout ; distinguer vide/filtre/erreur réseau |
| PDF/provenance | Accès privé autorisé, zoom et navigation page accessibles, annotation champ source sans couleur seule |

## Validation obligatoire avant livraison UI
Clavier, focus non masqué, contrastes WCAG 2.2 AA, zoom 200 %, lecteur d’écran, reduced-motion, quatre largeurs cibles. Les erreurs multi-champs gardent les erreurs inline ET un résumé focalisé. Aucune confirmation autorisée sur une extraction ambiguë. Ne pas faire croire que la confiance OCR est une probabilité certaine.
Contrastes des paires ci-dessus calculés lors de la préparation ; vérifier encore les états hover/disabled, overlays et compositions réelles lors de l’implémentation. La conformité d’une interface non construite n’est pas revendiquée.

## Inspiration et exclusions
21st.dev : cards et navigation comme références de composition, liens dans UI_UX ; recréer avec Vue et shadcn-vue/Reka. Pas de React, TSX, gradient violet, carrousel, hero marketing, effets 3D ou police monospace pour tout le dashboard. Une bibliothèque de composants accessibles n’exonère pas de vérifier les usages.
