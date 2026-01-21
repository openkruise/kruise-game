To install OpenKruiseGame, you need to install Kruise and Kruise-Game, and require Kubernetes version >= 1.16

## Install Kruise

We recommend that you use Helm V3.5 or later to install Kruise.

```shell
# Firstly add openkruise charts repository if you haven't done this.
$ helm repo add openkruise https://openkruise.github.io/charts/

# [Optional]
$ helm repo update

# Install the latest version.
$ helm install kruise openkruise/kruise --version 1.3.0 --set featureGates="PodProbeMarkerGate=true"
```

## Install Kruise-Game

### Method 1: Helm

```shell
$ helm install kruise-game openkruise/kruise-game --version 0.2.0
```

### Method 2: Compile & Deploy with Yaml

0) Edit Makefile. Change the value of the IMG field to the repository address of Makefile.

1) Compile and package the images of kruise-game-manager.

```bash
make docker-build
```

2) Upload the packaged image to the image repository.

```bash
make docker-push
```

3) Deploy the kruise-game-manager component in a Kubernetes cluster (~/.kube/conf).

```bash
make deploy
```
