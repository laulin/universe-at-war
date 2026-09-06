# Champs de débris — règle de fidélité

## Comportement attendu

Un champ de débris occupe une position, contient du métal et du cristal, et
n'existe que tant qu'il n'est pas vide. Il est créé ou augmenté dans la
transaction même qui détruit des vaisseaux, jamais après coup.

Les champs sont publics : la vue galaxie affiche leur contenu exact à tous les
joueurs.

## Recyclage

Le recyclage est une mission de flotte, jamais un bouton instantané. Elle exige
au moins un recycleur et vise un champ existant et non vide.

```text
capacité_recyclage = min(recycleurs * capacité_unitaire,
                         cargo total de la flotte - cargo déjà embarqué)
prise = min(capacité_recyclage, métal + cristal du champ)
métal_pris   = min(métal du champ, prise / 2)
cristal_pris = min(cristal du champ, prise / 2)
reste        = prise - métal_pris - cristal_pris
métal_pris   += min(métal restant du champ, reste)
cristal_pris += min(cristal restant du champ, ce qui reste encore)
```

Le prélèvement est une mise à jour gardée qui exige que le champ contienne
encore ce qui est pris ; le champ vidé est supprimé. Une ligne exactement doit
être touchée, sinon la transaction échoue et l'événement est retenté.

## Concurrence

Plusieurs flottes peuvent viser le même champ. Les arrivées sont traitées dans
l'ordre `(échéance, priorité, identifiant)` par l'unique chemin d'écriture : la
seconde flotte ne voit que le reste. La somme de ce que les recycleurs emportent
n'excède jamais ce que le champ contenait.

## Cas limites et invariants

Un champ absent à l'arrivée donne une prise nulle et un rapport qui le dit. Un
champ ne contient jamais de deutérium, ni de quantité négative. Le contenu d'un
champ ne peut pas augmenter autrement que par une destruction d'unités.

## Tests de référence

Table de répartition métal/cristal ; deux recycleurs concurrents sur le même
champ ; champ vidé puis supprimé ; champ absent ; redélivrance d'un événement de
recyclage sans double prélèvement.
