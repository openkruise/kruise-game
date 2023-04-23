## Metrics available

OKG by default exposes game server related Prometheus metrics, including:

| Name | Description                                             | Type      |
| --- |------------------------------------------------|---------|
| GameServersStateCount | Number of game servers in different states     | gauge   |
| GameServersOpsStateCount | Number of game servers in different ops states | gauge   |
| GameServersTotal | Total number of game servers that have existed | counter |
| GameServerSetsReplicasCount | Number of replicas for each GameServerSet      | gauge     |
| GameServerDeletionPriority | Deletion priority for game servers             | gauge     |
| GameServerUpdatePriority | Update priority for game servers               | gauge     |


## Monitoring Dashboard

### Dashboard Import
1. Import ./config/prometheus/grafana.json to Grafana
2. Choose data source
3. Replace UID and complete the import

### Dashboard Introduction
The imported dashboard is shown below:

<p align="center">
  <img src="../../images/gra-dash.png" width="90%"/>
</p>

From top to bottom, it includes:

- First row: number of GameServers in each current state, and a pie chart showing the proportion of GameServers in each current state
- Second row: line chart showing the number of GameServers in each state over time
- Third row: line chart showing the changes in deletion and update priorities for GameServers (can be filtered by namespace and gsName in the top-left corner)
- Fourth and fifth rows: line charts showing the number of GameServers in different states for each GameServerSet (can be filtered by namespace and gssName in the top-left corner)