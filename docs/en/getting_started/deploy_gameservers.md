You can use GameServerSet to deploy game servers. A simple deployment case is as follows:

```bash
cat <<EOF | kubectl apply -f -
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
EOF
```

After the GameServerSet is created, three game servers and three corresponding pods appear in the cluster, because the specified number of replicas is 3.

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
