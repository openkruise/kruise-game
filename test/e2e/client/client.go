package client

import (
	"context"
	"fmt"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	kruisegameclientset "github.com/openkruise/kruise-game/pkg/client/clientset/versioned"
	apps "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/utils/pointer"
	"time"
)

const (
	Namespace         = "e2e-test"
	GameServerSet     = "default-gss"
	GameContainerName = "default-game"
)

type Client struct {
	kruisegameClient kruisegameclientset.Interface
	kubeClint        clientset.Interface
}

func NewKubeClient(kruisegameClient kruisegameclientset.Interface, kubeClint clientset.Interface) *Client {
	return &Client{
		kruisegameClient: kruisegameClient,
		kubeClint:        kubeClint,
	}
}

func (client *Client) CreateNamespace() error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      Namespace,
			Namespace: Namespace,
		},
	}
	_, err := client.kubeClint.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
	return err
}

func (client *Client) DeleteNamespace() error {
	return wait.PollImmediate(5*time.Second, 3*time.Minute,
		func() (done bool, err error) {
			err = client.kubeClint.CoreV1().Namespaces().Delete(context.TODO(), Namespace, metav1.DeleteOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					return true, nil
				}
			}
			return false, nil
		})
}

func (client *Client) DefaultGameServerSet() *gameKruiseV1alpha1.GameServerSet {
	return &gameKruiseV1alpha1.GameServerSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GameServerSet,
			Namespace: Namespace,
		},
		Spec: gameKruiseV1alpha1.GameServerSetSpec{
			Replicas: pointer.Int32Ptr(3),
			UpdateStrategy: gameKruiseV1alpha1.UpdateStrategy{
				Type: apps.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &gameKruiseV1alpha1.RollingUpdateStatefulSetStrategy{
					PodUpdatePolicy: kruiseV1beta1.InPlaceIfPossiblePodUpdateStrategyType,
				},
			},
			GameServerTemplate: gameKruiseV1alpha1.GameServerTemplate{
				PodTemplateSpec: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  GameContainerName,
								Image: "nginx:1.9.7",
							},
						},
					},
				},
			},
		},
	}
}

func (client *Client) CreateGameServerSet(gss *gameKruiseV1alpha1.GameServerSet) (*gameKruiseV1alpha1.GameServerSet, error) {
	if gss == nil {
		return nil, fmt.Errorf("gss is nil")
	}
	return client.kruisegameClient.GameV1alpha1().GameServerSets(Namespace).Create(context.TODO(), gss, metav1.CreateOptions{})
}

func (client *Client) UpdateGameServerSet(gss *gameKruiseV1alpha1.GameServerSet) (*gameKruiseV1alpha1.GameServerSet, error) {
	return client.kruisegameClient.GameV1alpha1().GameServerSets(Namespace).Update(context.TODO(), gss, metav1.UpdateOptions{})
}

func (client *Client) DeleteGameServerSet() error {
	return wait.PollImmediate(3*time.Second, time.Minute, func() (done bool, err error) {
		err = client.kruisegameClient.GameV1alpha1().GameServerSets(Namespace).Delete(context.TODO(), GameServerSet, metav1.DeleteOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
		}
		return false, nil
	})
}

func (client *Client) GetGameServerSet() (*gameKruiseV1alpha1.GameServerSet, error) {
	return client.kruisegameClient.GameV1alpha1().GameServerSets(Namespace).Get(context.TODO(), GameServerSet, metav1.GetOptions{})
}

func (client *Client) PatchGameServer(gsName string, data []byte) (*gameKruiseV1alpha1.GameServer, error) {
	return client.kruisegameClient.GameV1alpha1().GameServers(Namespace).Patch(context.TODO(), gsName, types.MergePatchType, data, metav1.PatchOptions{})
}

func (client *Client) PatchGameServerSet(data []byte) (*gameKruiseV1alpha1.GameServerSet, error) {
	return client.kruisegameClient.GameV1alpha1().GameServerSets(Namespace).Patch(context.TODO(), GameServerSet, types.MergePatchType, data, metav1.PatchOptions{})
}

func (client *Client) GetGameServerList(labelSelector string) (*gameKruiseV1alpha1.GameServerList, error) {
	return client.kruisegameClient.GameV1alpha1().GameServers(Namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector})
}

func (client *Client) GetPodList(labelSelector string) (*corev1.PodList, error) {
	return client.kubeClint.CoreV1().Pods(Namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector})
}

func (client *Client) GetPod(podName string) (*corev1.Pod, error) {
	return client.kubeClint.CoreV1().Pods(Namespace).Get(context.TODO(), podName, metav1.GetOptions{})
}
