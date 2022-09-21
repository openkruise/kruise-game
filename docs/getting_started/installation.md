# Installation

## Install manually

### 0. Edit Makefile, changing {IMG}

### 1. Build docker image with the kruise-game controller manager.
```shell
make docker-build
```

### 2. Push docker image with the kruise-game controller manager.
```shell
make docker-push
```

### 3. Deploy kruise-game controller manager to the K8s cluster.
```shell
make deploy
```

## Uninstall manually
```shell
make undeploy
```

