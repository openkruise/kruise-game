# Kruise-game

## Introduction
Kruise-Game is a subproject of OpenKruise for solving the problem of game server landing in Kubernetes.

<img width="250px" src="./docs/images/logo.jpg" alt="OpenKruiseGame logo"/>

Kruise-Game utilizes the features of [Kruise](https://github.com/openkruise/kruise), including:
- In-Place Update
- Update sequence
- Ordinals reserve(skip)
- Pod probe marker
- …

## Why is Kruise-Game?
Game servers are stateful services, and there are differences in the operation and maintenance of each game server, which also increase with time. In Kubernetes, general workloads manage a batch of game servers according to pod templates, which cannot take into account the differences in game server status. Batch management and directional management conflict in K8s. **Kruise-Game** was born to resolve that. Kruise-Game contains two CRDs, GameServer and GameServerSet:

- `GameServer` is responsible for the management of the game server status. Users can customize the game server status to reflect the differences between game servers;
- `GameServerSet` is responsible for batch management of game servers. Users can customize update/reduction strategies according to the status of game servers.

Features:
- Game server status management
    - Mark the game servers status without affecting its lifecycle
- Flexible scaling/deletion mechanism
    - Support scaling down by user-defined status & priority
    - Support specifying the game server to delete directly
- Flexible update mechanism
    - Support hot update (in-place update)
    - Support updating game server by user-defined priority
    - Can control the range of the game servers to be updated
    - Can control the pace of the entire update process
- Custom service quality
    - Support probing game servers‘ containers and marking game server status automatically

## Quick Start

- [Installation](./docs/getting_started/installation.md)
- [Basic Usage](./docs/tutorials/basic_usage.md)

## License

Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
