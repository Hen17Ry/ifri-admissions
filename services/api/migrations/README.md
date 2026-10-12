# Migrations

Goose v3.29.0 est retenu pour les futures migrations SQL PostgreSQL, exécutées explicitement hors démarrage API. Aucun schéma métier ni migration artificielle dans cette phase : pgx Ping valide la connexion.

Lors de la phase schéma, intégrer un petit runner limité au driver pgx plutôt que tous les drivers du CLI Goose. Chaque migration aura sections Up/Down, version ordonnée et tests PostgreSQL. Le runner et la dépendance seront installés dans cette phase, pas ajoutés inutilément à l’API actuelle.
