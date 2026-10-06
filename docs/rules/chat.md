# Messagerie

La messagerie relie les joueurs sans exposer leurs données de jeu. Elle possède
deux types de conversation :

- une conversation privée entre exactement deux joueurs ;
- un canal commun, unique pour chaque alliance.

Depuis la carte de la galaxie, l'enveloppe d'un monde adverse ouvre directement
la conversation privée avec son propriétaire. La page **Messagerie** retrouve
ensuite toutes les conversations déjà ouvertes. La page d'une alliance donne
aussi accès à son canal commun.

## Messages

Un message contient du texte Unicode, un GIF, ou les deux. Le texte est limité
à 2 000 caractères. Les emoji sont du texte ordinaire : la palette de
l'interface ne fait qu'insérer les plus courants dans le champ de saisie.

Un GIF est référencé par une URL HTTPS de 2 048 octets au plus. Le serveur ne le
télécharge pas et ne le recopie pas dans la base. Le navigateur le charge sans
envoyer de référent HTTP. Ce choix évite un stockage de médias et toute
dépendance à une API commerciale, mais l'hébergeur du GIF voit l'adresse IP du
navigateur qui l'affiche.

Chaque envoi porte une clé créée par le navigateur. Rejouer la même requête
après une interruption réseau ne crée pas de doublon.

## Mise à jour dynamique

La page demande les nouveaux messages toutes les deux secondes et ne transfère
que ceux dont l'identifiant suit le dernier déjà affiché. L'envoi et la
réception ne rechargent donc pas la page. Sans JavaScript, le même formulaire
envoie le message puis revient à la conversation : la fonction principale reste
disponible.

L'indication « en train d'écrire » est éphémère. Elle expire après cinq secondes,
n'est jamais enregistrée en base et disparaît simplement si le serveur
redémarre. Elle n'appartient pas à l'historique de la conversation.

## Confidentialité et administration

- Seuls les deux participants lisent et alimentent une conversation privée.
- Seuls les membres actuels d'une alliance lisent et alimentent son canal.
  Quitter l'alliance coupe immédiatement l'accès, sans supprimer l'historique.
- Les administrateurs disposent d'une vue globale en lecture seule sur toutes
  les conversations persistées.
- Le rôle de modérateur ne donne aucun accès global à la messagerie.
- Un administrateur qui possède aussi le rôle joueur conserve ses accès de
  joueur ordinaires ; le pouvoir de supervision reste attaché au rôle
  administrateur et n'autorise pas à écrire au nom d'un participant.

Les messages sont échappés au rendu comme tout contenu fourni par un joueur.
Les contrôles d'accès sont répétés à chaque lecture, envoi et signal de frappe ;
connaître l'identifiant d'une conversation ne suffit jamais à l'ouvrir.
