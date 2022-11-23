## 安装Kruise

建议采用 helm v3.5+ 来安装 Kruise

```shell
# Firstly add openkruise charts repository if you haven't do this.
$ helm repo add openkruise https://openkruise.github.io/charts/

# [Optional]
$ helm repo update

# Install the latest version.
$ helm install kruise openkruise/kruise --version 1.3.0 --set featureGates="PodProbeMarkerGate=true"
```

## 安装Kruise-Game

### 编译安装

0) 编辑Makefile，更改其中{IMG}字段，将其改为自身的仓库地址

1) 编译并打包kurise-game-manager镜像

```bash
make docker-build
```

2) 将打包完成的镜像上传至镜像仓库

```bash
make docker-push
```

3) 在Kubernetes集群（~/.kube/conf）部署kruise-game-manager组件

```bash
make deploy
```
