# Introduction

## What is Kruise-Game?
Kruise-Game is a subproject of OpenKruise, to solve the problem of game server landing in Kubernetes.

<img width="250px" src="../../images/logo.jpg" alt="OpenKruiseGame logo"/>

Kruise-Game utilizes the features of [Kruise](https://github.com/openkruise/kruise), including:
- In-Place Update
- Update sequence
- Ordinals reserve(skip)
- Pod probe marker
- …

## Why is Kruise-Game?
Game servers are stateful services, and there are differences in the operation and maintenance of each game server, which also increases with time. In Kubernetes, general workloads manages a batch of game servers according to pod templates, which cannot take into account the differences in game server status. Batch management and directional management are in conflict in k8s. **Kruise-Game** was born to resolve that. Kruise-Game contains two CRDs, GameServer and GameServerSet:

- `GameServer` is responsible for the management of game server status. Users can customize the game server status to reflect the differences between game servers;
- `GameServerSet` is responsible for batch management of game servers. Users can customize update/reduction strategies according to the status of game servers.

## Features
- Game server status management
    - Mark game servers status without effecting to its lifecycle
- Flexible scaling/deletion mechanism
    - Support scaling down by user-defined status & priority
    - Support specifying game server to delete directly
- Flexible update mechanism
    - Support hot update (in-place update)
    - Support updating game server by user-defined priority
    - Can control the range of the game servers to be updated
    - Can control the pace of the entire update process
- Custom service quality
    - Support probing game servers‘ containers and mark game servers status automatically

## What's Next
Here are some recommended next steps:

- Start to [Install kruise-game](installation.md).
- Learn Kruise-Game's [Basic Usage](../tutorials/basic_usage.md).