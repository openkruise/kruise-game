# Basic Usage

## Requirements
- Installation of Kruise, Reference [Install OpenKruise](https://openkruise.io/zh/docs/installation/).
- Installation of Kruise-Game, Reference [Install Kruise-Game](../getting_started/installation.md)

## Deploy GameServerSet
This is an example of GameServerSet, which manages 3 game servers.
```yaml
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: minecraft
  namespace: default
spec:
  replicas: 3
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  gameServerTemplate:
    spec:
      containers:
        - image: registry.cn-hangzhou.aliyuncs.com/acs/minecraft-demo:1.12.2
          name: minecraft
```
When the deployment is complete, the cluster will generate 1 GameServerset, 3 GameServers, and 3 Pods corresponding to GameServers
```bash
kubectl get gss
NAME        AGE
minecraft   9s

kubectl get gs
NAME          STATE   OPSSTATE   DP    UP
minecraft-0   Ready   None       0     0
minecraft-1   Ready   None       0     0
minecraft-2   Ready   None       0     0

kubectl get pod
NAME            READY   STATUS    RESTARTS   AGE
minecraft-0     1/1     Running   0          10s
minecraft-1     1/1     Running   0          10s
minecraft-2     1/1     Running   0          10s
```

## Game servers scale up
Directly adjust the number of replicas to the desired number
```bash
kubectl scale gss minecraft --replicas=5
gameserverset.game.kruise.io/minecraft scaled
```

The number of game servers eventually became 5
```bash
kubectl get gs
NAME          STATE   OPSSTATE   DP    UP
minecraft-0   Ready   None       0     0
minecraft-1   Ready   None       0     0
minecraft-2   Ready   None       0     0
minecraft-3   Ready   None       0     0
minecraft-4   Ready   None       0     0
```

## Game servers scale down by deletion priority
Manually set the GameServer deletionPriority (you can set the deletionPriority automatically through the ServiceQuality function)
```yaml
kubectl edit gs minecraft-2

...
spec:
  deletionPriority: 10 #initial value is 0，turn it up to 10
  opsState: None
  updatePriority: 0
...
```
Scale down
```bash
kubectl scale gss minecraft --replicas=4
gameserverset.game.kruise.io/minecraft scale
```

The numbers of game servers eventually became 4. It can be found that the No.2 gs with the highest deletionPriority has been deleted.
```bash
kubectl get gs
NAME          STATE      OPSSTATE   DP    UP
minecraft-0   Ready      None       0     0
minecraft-1   Ready      None       0     0
minecraft-2   Deleting   None       10    0
minecraft-3   Ready      None       0     0
minecraft-4   Ready      None       0     0

# After a while
...

kubectl get gs
NAME          STATE   OPSSTATE   DP    UP
minecraft-0   Ready   None       0     0
minecraft-1   Ready   None       0     0
minecraft-3   Ready   None       0     0
minecraft-4   Ready   None       0     0
```

## Game servers scale down by OpsState
Manually set the GameServer OpsState to `WaitToBeDeleted` (you can set the OpsState automatically through the ServiceQuality function)

```yaml
kubectl edit gs minecraft-3

...
spec:
  deletionPriority: 0 
  opsState: WaitToBeDeleted #Initialization is None, will be changed to WaitToBeDeleted
  updatePriority: 0
...
```
Scale down
```bash
kubectl scale gss minecraft --replicas=3
gameserverset.game.kruise.io/minecraft scaled
```

The numbers of game servers eventually became 3. It can be found that the No.3 gs with WaitToBeDeleted OpsState has been deleted.
```bash
kubectl get gs
NAME          STATE      OPSSTATE   DP    UP
minecraft-0   Ready      None       0     0
minecraft-1   Ready      None       0     0
minecraft-3   Deleting   None       10    0
minecraft-4   Ready      None       0     0

# After a while
...

kubectl get gs
NAME          STATE   OPSSTATE   DP    UP
minecraft-0   Ready   None       0     0
minecraft-1   Ready   None       0     0
minecraft-4   Ready   None       0     0
```

## Specify game server offline
Specify the game server with serial No.1 to go offline
```yaml
kubectl edit gss minecraft

...
spec:
  replicas: 2 #replicas is reduced by 1, adjusted to 2
  reserveGameServerIds: 
  - 1 #specify serial No.1
...
```

The numbers of game servers eventually became 2. It can be found that the No.1 gs has been deleted.
```bash
kubectl get gs
NAME          STATE      OPSSTATE   DP   UP
minecraft-0   Ready      None       0    0
minecraft-1   Deleting   None       0    0
minecraft-4   Ready      None       0    0

# After a while
...

kubectl get gs
NAME          STATE   OPSSTATE   DP    UP
minecraft-0   Ready   None       0     0
minecraft-4   Ready   None       0     0
```

## Game servers update by update priority

Manually set the GameServer updatePriority (you can set the updatePriority automatically through the ServiceQuality function)

```yaml
kubectl edit gs minecraft-0

...
spec:
  deletionPriority: 0
  opsState: None
  updatePriority: 10 #initial value is 0，turn it up to 10
...
```

Update game servers' image
```yaml
kubectl edit gss minecraft

...
spec:
  gameServerTemplate:
    spec:
      containers:
      - image: registry.cn-hangzhou.aliyuncs.com/acs/minecraft-demo:1.13.0 #update images tag to 1.13.0
        name: minecraft
...

```

Pay attention to the update process, you can find that the GameServer with a larger updatePriority is updated first
```bash
kubectl get gs
NAME          STATE      OPSSTATE   DP    UP
minecraft-0   Updating   None       0     10
minecraft-4   Ready      None       0     0

# After a while
... 

kubectl get gs
NAME          STATE      OPSSTATE   DP    UP
minecraft-0   Ready      None       0     10
minecraft-4   Updating   None       0     0

# After a while
... 

kubectl get gs
NAME          STATE   OPSSTATE   DP    UP
minecraft-0   Ready   None       0     10
minecraft-4   Ready   None       0     0
```