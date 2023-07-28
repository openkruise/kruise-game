## The scaling feature of OpenKruiseGame

OpenKruiseGame allows you to set the states of game servers. You can manually set the value of opsState or DeletionPriority for a game server. You can also use the service quality feature to automatically set the value of opsState or DeletionPriority for a game server. During scale-in, a proper GameServerSet workload is selected for scale-in based on the states of game servers. The scale-in rules are as follows:

1. Scale in game servers based on the opsState values. Scale in the game servers for which the opsState values are `WaitToBeDeleted`, `None`, `Allocated`, and `Maintaining` in sequence.

2. If two or more game servers have the same opsState value, game servers are performed based on the values of DeletionPriority. The game server with the largest DeletionPriority value is deleted first.

3. If two or multiple game servers have the same opsState value and DeletionPriority value, the game server whose name contains the largest sequence number in the end is deleted first.

### Examples

Deploy a game server with five replicas:

```bash
cat <<EOF | kubectl apply -f -
apiVersion: game.kruise.io/v1alpha1
kind: GameServerSet
metadata:
  name: minecraft
  namespace: default
spec:
  replicas: 5
  updateStrategy:
    rollingUpdate:
      podUpdatePolicy: InPlaceIfPossible
  gameServerTemplate:
    spec:
      containers:
        - image: registry.cn-hangzhou.aliyuncs.com/acs/minecraft-demo:1.12.2
          name: minecraft
EOF
```

Five game servers are generated:

```bash
kubectl get gs
NAME          STATE   OPSSTATE   DP    UP
minecraft-0   Ready   None       0     0
minecraft-1   Ready   None       0     0
minecraft-2   Ready   None       0     0
minecraft-3   Ready   None       0     0
minecraft-4   Ready   None       0     0
```

Set DeletionPriority to 10 for minecraft-2:

```bash
kubectl edit gs minecraft-2

...
spec:
  DeletionPriority: 10 # Change the value of DeletionPriority from the initial value 0 to 10.
  opsState: None
  updatePriority: 0
...
```

Manually perform scale-in to reduce the number of the game servers to 4:

```bash
kubectl scale gss minecraft --replicas=4
gameserverset.game.kruise.io/minecraft scale
```

The number of the game servers is changed to 4. The following example shows that minecraft-2 is deleted because it has the largest DeletionPriority value.

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

Set opsState to WaitToBeDeleted for minecraft-3:

```bash
kubectl edit gs minecraft-3

...
spec:
  deletionPriority: 0
  opsState: WaitToBeDeleted # Change the value of opsState from the initial value None to WaitToBeDeleted.
  updatePriority: 0
...
```

Manually perform scale-in to reduce the number of the game servers to 3:

```bash
kubectl scale gss minecraft --replicas=3
gameserverset.game.kruise.io/minecraft scaled
```

The number of replicas for the game server is changed to 3. You can see that minecraft-3 is deleted because its opsState value is WaitToBeDeleted.

```bash
kubectl get gs
NAME          STATE      OPSSTATE          DP    UP
minecraft-0   Ready      None              0     0
minecraft-1   Ready      None              0     0
minecraft-3   Deleting   WaitToBeDeleted   0     0
minecraft-4   Ready      None              0     0

# After a while
...

kubectl get gs
NAME          STATE   OPSSTATE   DP    UP
minecraft-0   Ready   None       0     0
minecraft-1   Ready   None       0     0
minecraft-4   Ready   None       0     0
```

Manually perform scale-out and change the number of replicas for the game server back to 5:

```bash
kubectl scale gss minecraft --replicas=5
gameserverset.game.kruise.io/minecraft scaled
```

The number of replicas for the game server is changed back to 5. You can see that minecraft-2 and minecraft-3 are added for the game server.

```bash
kubectl get gs
NAME          STATE   OPSSTATE   DP    UP
minecraft-0   Ready   None       0     0
minecraft-1   Ready   None       0     0
minecraft-2   Ready   None       0     0
minecraft-3   Ready   None       0     0
minecraft-4   Ready   None       0     0
```

## Configure the auto scaling feature for a game server

GameServerSet supports Horizontal Pod Autoscaler (HPA). You can configure this feature based on the default or custom metrics.

### HPA example

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: minecraft-hpa
spec:
  scaleTargetRef:
    apiVersion: game.kruise.io/v1alpha1
    kind: GameServerSet
    name: minecraft # The name of GameServerSet
  minReplicas: 1
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 50 # The CPU utilization 50% is used for calculation in this example.
```
