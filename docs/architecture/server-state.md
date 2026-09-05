# Machine d'état du serveur

États persistés :

- `BOOTSTRAP_PENDING` : base créée, administrateur initial disponible ;
- `SETUP_IN_PROGRESS` : administrateur authentifié, configuration non validée ;
- `RUNNING` : jeu et simulation actifs ;
- `PAUSED` : mutations de jeu et temps d'univers suspendus selon la politique ;
- `MAINTENANCE` : accès réservé aux opérations administratives locales.

## Transitions

| Depuis | Vers | Commande et garde |
| --- | --- | --- |
| absence de DB | `BOOTSTRAP_PENDING` | migrations et bootstrap réussis dans une transaction |
| `BOOTSTRAP_PENDING` | `SETUP_IN_PROGRESS` | premier accès au wizard par un admin authentifié |
| `SETUP_IN_PROGRESS` | `RUNNING` | ruleset complet valide et confirmation explicite |
| `RUNNING` | `PAUSED` | admin autorisé, motif audité |
| `PAUSED` | `RUNNING` | admin autorisé, reprise auditéе |
| `RUNNING` ou `PAUSED` | `MAINTENANCE` | commande locale ou admin autorisé |
| `MAINTENANCE` | état précédent | contrôles d'intégrité réussis |

Il n'existe pas de transition inverse vers `BOOTSTRAP_PENDING`. Refaire le setup
nécessite une procédure administrative future, explicite et auditée.

## Accès HTTP

- avant `RUNNING` : login, logout, setup, assets et health minimal seulement ;
- dans `PAUSED` : consultation et administration autorisées, mutations de jeu
  refusées avec une erreur métier stable ;
- dans `MAINTENANCE` : seules authentification, maintenance et health sont
  exposées ;
- les handlers interrogent une politique centrale, pas des booléens locaux.


## Inscriptions

Le parcours d'inscription n'existe que dans l'état `RUNNING` et seulement si la
politique du ruleset actif vaut `open`. Dans tout autre cas, `/register` répond
404 et la page de connexion n'affiche aucun lien : l'existence même du parcours
ne renseigne pas sur la configuration. Un compte créé reçoit le seul rôle
`PLAYER`, sans obligation de changer son mot de passe, et l'inscription est
auditée. Les politiques `closed` et `invitation` refusent la création ; les
invitations proprement dites arrivent avec le jalon de finition.
