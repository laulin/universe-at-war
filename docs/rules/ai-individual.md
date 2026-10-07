# Intelligence artificielle individuelle — règle de fidélité

## Comportement attendu

Une intelligence artificielle est un joueur comme un autre : elle possède un
compte, un empire, des ressources et des flottes, et elle passe exactement par
les mêmes cas d'usage qu'un humain. Elle paie ses constructions, attend ses
files, consomme son deutérium, doit espionner pour savoir, et perd ses flottes.

Elle est aussi imparfaite et observable : elle dort, elle agit sur des rapports
qui vieillissent, elle se trompe, et son archétype la pousse à des choix qu'un
autre archétype ne ferait pas. Une IA parfaite est indésirable.

Un compte d'IA porte `kind = 'ai'` et n'a aucun identifiant de connexion : la
page de login ne peut rien en faire, et aucune session ne peut lui être
attribuée. Il n'existe aucun chemin qui donne à une IA une information qu'un
joueur n'obtiendrait pas.

## Trois niveaux de décision

| Niveau | Horizon | Décide |
| --- | --- | --- |
| stratégique | heures à jours | l'équilibre entre économie et militaire, la ligne de recherche, l'expansion |
| opérationnel | minutes à heures | qui espionner, quelle cible vaut un raid, quand mettre la flotte à l'abri |
| tactique | l'instant | la composition, la vitesse, le cargo, les recycleurs |

Une flotte de raid emporte ses vaisseaux de combat et juste assez de soutes pour
le butin espéré ; un ramassage de débris emporte juste assez de recycleurs.

Chaque réflexion parcourt les trois niveaux. Elle entretient une petite avance
payée dans les files économiques, puis n'engage au plus qu'une mission. La
réflexion reste donc un rythme stratégique et non le métronome de chaque niveau
de mine : dans un univers rapide, une file peut avancer entre deux réflexions
sans laisser l'empire artificiel à l'arrêt.

## Réflexion et jitter

Chaque IA porte sa propre seed, tirée à sa création et persistée. La prochaine
réflexion est calculée à partir de l'intervalle configuré et d'un décalage
déterministe :

```text
jitter  = intervalle × (tirage(seed, numéro_de_tick) × 2 × amplitude − amplitude)
prochaine = maintenant + max(1 s, intervalle + jitter)
```

avec `amplitude = 0,25`. Le tirage vient d'une source seedée par
`seed ⊕ numéro_de_tick`, si bien que rejouer l'univers depuis les mêmes seeds
rejoue exactement les mêmes horaires. Un événement `ai_think` durable porte
cette échéance sous la clé `ai-think:<joueur>` : le worker dort entre deux
réflexions et reprend après un redémarrage.

## Plage d'activité

Chaque IA a une heure d'ouverture et une heure de fermeture, en UTC, qui peuvent
enjamber minuit. En dehors de cette plage :

- la production continue, les missions déjà lancées continuent, les événements
  se résolvent ;
- **aucune décision nouvelle n'est prise** ;
- la réflexion suivante est reportée à la prochaine ouverture, plus le jitter.

La section `ai` du ruleset porte les valeurs recommandées de l'univers —
intervalle de réflexion, heure d'ouverture, heure de fermeture — que
l'administrateur reprend ou ajuste pour chaque joueur artificiel qu'il crée.

Une plage dont l'ouverture égale la fermeture décrit une IA toujours éveillée.
Ce report est appliqué dans la transaction de l'événement, avant toute
délibération : le sommeil n'est pas une préférence, c'est une règle.

## Mémoire

La mémoire d'une IA ne contient que ce qu'elle a le droit de savoir :

- ses propres rapports d'espionnage, de combat et de recyclage ;
- les lignes publiques de la carte galactique ;
- ce qu'elle a elle-même décidé et lancé.

Chaque souvenir porte la date de l'observation. Rien d'autre n'y entre : il
n'existe aucune requête qui lise l'état d'un adversaire.

## Fraîcheur d'une information

```text
âge = maintenant − date_d_observation
fraîcheur = 1                                    si âge ≤ recent_report_seconds
          = 1 − (âge − recent) / (péremption − recent)   si âge < péremption
          = 0                                    sinon
```

avec `péremption = 12 × recent_report_seconds`. Une information de fraîcheur
nulle ne vaut rien : elle ne déclenche aucun raid, mais elle n'est pas effacée
pour autant — une IA peut agir sur un rapport vieillissant et se tromper.

## Rôle des vaisseaux

Un raid part avec le nombre de vaisseaux que l'IA croit nécessaire et juste ce
qu'il faut de soutes pour rapporter. Le rôle
se lit dans le catalogue seul : un vaisseau **combat** si son arme est non nulle
et vaut au moins le centième de sa soute ; sinon il **transporte** si sa soute
atteint 1000 unités ; sinon il reste au sol. Une sonde n'emporte rien d'utile et
ne part donc jamais en raid.

## Score d'une cible

Un raid ne se décide que sur un rapport d'espionnage de l'IA elle-même.

```text
butin       = pillage_ratio × (métal + cristal + deutérium vus)
puissance(u)= Σ n_i × (arme_i + bouclier_i + coque_i)
score       = butin × fraîcheur × avidité
            − puissance(défense vue) × prudence
            − distance
```

Le raid n'est lancé que si toutes ces conditions tiennent :

- le rapport est **complet**, c'est-à-dire que son niveau a atteint le rang qui
  révèle les défenses : une section vide veut alors dire une planète vide, et
  non une ignorance ;
- `fraîcheur > 0` : sans rapport exploitable, l'IA espionne d'abord ;
- `score > seuil_de_raid` de l'archétype ;
- `puissance(flotte envoyée) ≥ puissance(défense vue) × marge` de l'archétype ;
- la planète visée n'appartient ni à l'IA ni à son alliance.

Une cible déjà attaquée entre en refroidissement (30 minutes pour un raider,
une heure pour un fleeter ou un opportuniste, deux heures par défaut). À la fin
du délai, le rapport antérieur au combat est invalidé : un nouveau raid exige
un nouvel espionnage. Une attaque déjà en vol vers cette position interdit
également tout doublon.

L'estimation se fait sur ce que le rapport dit, pas sur la vérité : un rapport
ancien peut donc envoyer l'IA à la perte, et c'est voulu.

## Erreur de dimensionnement d'un raid

Même avec un rapport exact, l'IA peut mal traduire la défense observée en nombre
de vaisseaux. À chaque raid, elle tire depuis sa seed un facteur reproductible
appliqué à la force qu'elle voulait engager :

| Difficulté | Facteur de dimensionnement |
| --- | ---: |
| `easy` | 0,15 à 1,15 |
| `normal` ou inconnue | 0,45 à 1,15 |
| `hard` | 0,90 à 1,05 |

Elle sélectionne ensuite juste assez de vaisseaux pour atteindre ce besoin
estimé, au lieu d'envoyer automatiquement tout ce qu'elle possède. La marge de
sécurité de l'archétype continue de compter : un raider débutant peut donc
sous-dimensionner franchement sa flotte, tandis qu'un mineur très prudent reste
souvent protégé par sa marge malgré une mauvaise estimation. Le résolveur de
combat ne corrige rien et utilise les forces réelles ; l'erreur peut donc
détruire la flotte. Un facteur très faible ou très élevé est inscrit dans la
raison de la décision pour rendre ce comportement diagnostiquable.

## Archétypes

Un archétype est un jeu de préférences, jamais un script. Toutes les valeurs
sont bornées et documentées ici.

| Archétype | Économie | Avidité | Prudence | Marge | Sondes | Défense | Seuil de raid | Met à l'abri |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| `cautious_miner` | 0,90 | 0,30 | 1,50 | 4,0 | 3 | 0,25 | 12 000 | toujours |
| `raider` | 0,40 | 1,20 | 0,60 | 1,5 | 2 | 0,05 | 1 500 | si chargée |
| `fleeter` | 0,50 | 0,80 | 0,80 | 2,0 | 3 | 0,10 | 3 000 | toujours |
| `turtle` | 0,70 | 0,20 | 2,00 | 6,0 | 2 | 0,45 | 20 000 | jamais |
| `opportunist` | 0,55 | 1,00 | 0,90 | 2,0 | 3 | 0,10 | 2 500 | si chargée |
| `scout` | 0,60 | 0,50 | 1,20 | 3,0 | 5 | 0,10 | 6 000 | toujours |
| `logistician` | 0,80 | 0,40 | 1,20 | 3,0 | 2 | 0,20 | 9 000 | toujours |
| `defender` | 0,65 | 0,30 | 1,60 | 4,0 | 2 | 0,40 | 15 000 | toujours |

« Économie » règle notamment la profondeur prudente des files ; « Défense » la
part de la production réservée aux défenses. Les archétypes ont aussi des
priorités finies de bâtiments, de recherches et de flotte : le raider avance le
chantier et les propulsions, l'éclaireur le laboratoire, l'espionnage et les
slots, la tortue les défenses, le logisticien les soutes, recycleurs et colonies.
Une priorité a toujours un niveau cible : elle ne peut donc pas monopoliser la
progression indéfiniment.

## Planificateur économique

À chaque réflexion, l'IA entretient une file de deux à trois constructions sur
**chaque planète** (jamais sur une lune), en suivant cette priorité :

1. **Énergie** : si la consommation dépasse la production, une centrale
   solaire.
2. **Stockage** : si un stock est plein à plus de 90 %, le stockage
   correspondant.
3. **Installations** : usine de robots, laboratoire puis chantier spatial, dès
   que le niveau des mines dépasse le palier documenté (5, 8, puis 10).
4. **Mines** : la mine dont le niveau est le plus en retard sur les proportions
   cibles `métal 1 · cristal 0,66 · deutérium 0,4`.

L'IA prend la première option de cette liste que le jeu accepte réellement,
c'est-à-dire débloquée et payable. Une priorité qu'elle voit sans pouvoir la
payer est nommée dans la trace (« en économisant pour … ») et laissée à la
réflexion suivante : ainsi elle ne se bloque jamais, quitte à faire un choix
sous-optimal.

La recherche entretient deux entrées dans le meilleur laboratoire de l'empire.
Elle progresse par jalons d'ouverture, puis par objectifs propres à l'archétype,
sans répéter éternellement la première technologie. Le chantier garde deux à
trois lots d'avance par planète. Avant le combat, il protège un socle utile :
sondes, éventuel vaisseau de colonisation, recycleurs et cargos. Ensuite il tire
la part de défense de l'archétype depuis la seed de l'IA et son numéro de tick,
et considère aussi les vaisseaux avancés débloqués. La quantité commandée est
la moitié de ce que le stock permet, bornée par la taille de lot configurée.

Les profondeurs sont bornées et toutes les commandes paient immédiatement les
mêmes coûts qu'un humain. Elles compensent la cadence de réflexion ; elles ne
confèrent ni réduction de durée ni production gratuite.

## Expansion

Dès que l'astrophysique ouvre un emplacement et qu'un vaisseau de colonisation
est stationné, l'IA cherche une position vide dans la carte publique autour de
l'une de ses planètes et lance une mission `colonize` ordinaire. Une seule
colonisation peut être en vol. La nouvelle planète est nommée à la réflexion
suivante et entre immédiatement dans le planificateur multi-planètes.

## Recyclage

Un champ de débris est public : il se lit sur la carte, sans rapport et sans
espionnage. Une IA qui possède des recycleurs et voit un champ dans son système
y envoie juste ce qu'il faut de recycleurs pour le lever, dans la limite de ce
qu'elle possède. Elle s'y met après avoir renoncé à un raid : une bataille laisse
des débris, et la réflexion suivante va les chercher.

## Budgets

Une réflexion ne lance jamais plus d'une mission. Les files économiques sont
bornées à trois entrées au maximum et une commande de chantier ne prend qu'une
part de ce que le stock permet. Un lot de réflexions est borné par la taille de
lot du worker : un univers plein d'IA ne monopolise pas la boucle.

## Mise à l'abri

Avant de dormir, une IA dont l'archétype le demande envoie sa flotte en
transport vers un autre de ses corps, chargée de ce que la capacité restante
après carburant peut porter. Elle charge métal, cristal puis jusqu'à 80 % du
deutérium. Le mode `loaded` ne part que s'il a effectivement quelque chose à
mettre à l'abri ; le mode `always` protège la flotte même à vide.
Une IA qui n'a qu'un seul corps ne peut pas se mettre à l'abri de cette
manière : elle enregistre la raison et garde sa flotte au sol. C'est une limite
assumée de ce jalon.

## Traces

Chaque réflexion écrit ses décisions : le niveau, l'action, l'issue (`done`,
`skipped`, `failed`) et une raison courte. Les 250 décisions les plus récentes
de chaque IA sont conservées ; cette fenêtre suffit au diagnostic sans faire
croître la base sans borne. Les métriques locales exposent en plus les volumes
par action et issue, ainsi que des familles bornées de motifs de refus (file,
ressources, sondes, cadence de reconnaissance, renseignement, flotte).

## Coût

Une réflexion complète — inventaire, construction, recherche, chantier,
renseignement — coûte de l'ordre de vingt millisecondes sur une machine de
développement, pour une réflexion toutes les cinq minutes de temps de jeu. Entre
deux échéances, le worker dort : aucune boucle active ne tourne.

## Cas limites et invariants

Une IA ne prend aucune décision hors de sa plage. Le même état et la même seed
donnent la même décision. Deux archétypes sur le même état ne donnent pas la
même priorité. Aucune IA ne construit sans payer ni n'arrive sans voyager. Une
IA sans empire ne fait rien. Une IA retirée ne réfléchit plus et son événement
est annulé.

## Tests de référence

Développement d'un empire vide et de plusieurs planètes ; colonisation autonome ; identité des coûts et des délais avec un
humain ; absence d'action hors horaires ; reproductibilité à seed égale ;
divergence entre archétypes ; espionnage obligatoire avant un raid significatif ;
nouvel espionnage après un raid ; raid sur rapport périmé refusé ; cargo et composition cohérents ; mise à l'abri
avant le sommeil ; isolation structurelle du paquet `internal/ai` ; plusieurs
centaines de cycles sans boucle active.
