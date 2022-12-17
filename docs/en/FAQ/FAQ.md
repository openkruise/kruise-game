## FAQ

### Will a game server be deleted if a GameServer object is accidentally deleted?

No. A game server is not deleted if a GameServer object is accidentally deleted. GameServer only records the state information about different O&M operations on a game server. If GameServer is deleted, another GameServer object that uses the default settings is created. In this case, your GameServer is recreated based on the default configurations of the game server template defined in GameServerSet.


### How do we better integrate the matching service and auto scaling to prevent players from being forced to log out?
   
The service quality capability can be used to convert players' tasks of a game to the state of GameServer. The matching service perceives the state of GameServer and controls the number of replicas for the scale-in or scale-out. GameServerSet also determines the sequence of deletion based on the state of GameServer, thus achieving smooth logout.


### How do we use OpenKruiseGame in Massive Multiplayer Online Role-Playing Games (MMORPGs)?

You can use OpenKruiseGame and KubeVela together to perform game server orchestration in complex scenarios. You can use GameServerSet to manage a single role server.

