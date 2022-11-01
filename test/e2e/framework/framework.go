package framework

import (
	"encoding/json"
	"fmt"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	kruisegameclientset "github.com/openkruise/kruise-game/pkg/client/clientset/versioned"
	"github.com/openkruise/kruise-game/pkg/util"
	"github.com/openkruise/kruise-game/test/e2e/client"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"strconv"
	"strings"
	"time"
)

type Framework struct {
	client *client.Client
}

func NewFrameWork(config *restclient.Config) *Framework {
	kruisegameClient := kruisegameclientset.NewForConfigOrDie(config)
	kubeClient := clientset.NewForConfigOrDie(config)
	return &Framework{
		client: client.NewKubeClient(kruisegameClient, kubeClient),
	}
}

func (f *Framework) BeforeSuit() error {
	err := f.client.CreateNamespace()
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			err = f.client.DeleteGameServerSet()
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

func (f *Framework) AfterSuit() error {
	return f.client.DeleteNamespace()
}

func (f *Framework) AfterEach() error {
	return f.client.DeleteGameServerSet()
}

func (f *Framework) DeployGameServerSet() (*gamekruiseiov1alpha1.GameServerSet, error) {
	gss := f.client.DefaultGameServerSet()
	return f.client.CreateGameServerSet(gss)
}

func (f *Framework) DeployGssWithServiceQualities() (*gamekruiseiov1alpha1.GameServerSet, error) {
	gss := f.client.DefaultGameServerSet()
	up := intstr.FromInt(20)
	dp := intstr.FromInt(10)
	sqs := []gamekruiseiov1alpha1.ServiceQuality{
		{
			Name:          "healthy",
			ContainerName: client.GameContainerName,
			Probe: corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					Exec: &corev1.ExecAction{
						Command: []string{"/bin/sh", "-c", "ls /"},
					},
				},
			},
			Permanent: false,
			ServiceQualityAction: []gamekruiseiov1alpha1.ServiceQualityAction{
				{
					State: true,
					GameServerSpec: gamekruiseiov1alpha1.GameServerSpec{
						UpdatePriority: &up,
					},
				},
				{
					State: false,
					GameServerSpec: gamekruiseiov1alpha1.GameServerSpec{
						DeletionPriority: &dp,
					},
				},
			},
		},
	}
	gss.Spec.ServiceQualities = sqs
	return f.client.CreateGameServerSet(gss)
}

func (f *Framework) GameServerScale(gss *gamekruiseiov1alpha1.GameServerSet, desireNum int, reserveGsId *int) (*gamekruiseiov1alpha1.GameServerSet, error) {
	// TODO: change patch type
	newReserves := gss.Spec.ReserveGameServerIds
	if reserveGsId != nil {
		newReserves = append(newReserves, *reserveGsId)
	}

	numJson := map[string]interface{}{"spec": map[string]interface{}{"replicas": desireNum, "reserveGameServerIds": newReserves}}
	data, err := json.Marshal(numJson)
	if err != nil {
		return nil, err
	}
	return f.client.PatchGameServerSet(data)
}

func (f *Framework) ImageUpdate(gss *gamekruiseiov1alpha1.GameServerSet, name, image string) (*gamekruiseiov1alpha1.GameServerSet, error) {
	var newContainers []corev1.Container
	for _, c := range gss.Spec.GameServerTemplate.Spec.Containers {
		newContainer := c
		if c.Name == name {
			newContainer.Image = image
		}
		newContainers = append(newContainers, newContainer)
	}

	conJson := map[string]interface{}{"spec": map[string]interface{}{"gameServerTemplate": map[string]interface{}{"spec": map[string]interface{}{"containers": newContainers}}}}
	data, err := json.Marshal(conJson)
	if err != nil {
		return nil, err
	}
	return f.client.PatchGameServerSet(data)
}

func (f *Framework) MarkGameServerOpsState(gsName string, opsState string) (*gamekruiseiov1alpha1.GameServer, error) {
	osJson := map[string]interface{}{"spec": map[string]string{"opsState": opsState}}
	data, err := json.Marshal(osJson)
	if err != nil {
		return nil, err
	}
	return f.client.PatchGameServer(gsName, data)
}

func (f *Framework) ChangeGameServerDeletionPriority(gsName string, deletionPriority string) (*gamekruiseiov1alpha1.GameServer, error) {
	dpJson := map[string]interface{}{"spec": map[string]string{"deletionPriority": deletionPriority}}
	data, err := json.Marshal(dpJson)
	if err != nil {
		return nil, err
	}
	return f.client.PatchGameServer(gsName, data)
}

func (f *Framework) WaitForGsCreated(gss *gamekruiseiov1alpha1.GameServerSet) error {
	return wait.PollImmediate(5*time.Second, 3*time.Minute,
		func() (done bool, err error) {
			gssName := gss.GetName()
			labelSelector := labels.SelectorFromSet(map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
			}).String()
			podList, err := f.client.GetPodList(labelSelector)
			if err != nil {
				return false, err
			}
			if len(podList.Items) != int(*gss.Spec.Replicas) {
				return false, nil
			}
			return true, nil
		})
}

func (f *Framework) WaitForUpdated(gss *gamekruiseiov1alpha1.GameServerSet, name, image string) error {
	return wait.PollImmediate(10*time.Second, 10*time.Minute,
		func() (done bool, err error) {
			gssName := gss.GetName()
			labelSelector := labels.SelectorFromSet(map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
			}).String()
			podList, err := f.client.GetPodList(labelSelector)
			if err != nil {
				return false, err
			}
			updated := 0

			for _, pod := range podList.Items {
				for _, c := range pod.Status.ContainerStatuses {
					if name == c.Name && strings.Contains(c.Image, image) {
						updated++
						break
					}
				}
			}

			if gss.Spec.UpdateStrategy.RollingUpdate == nil || gss.Spec.UpdateStrategy.RollingUpdate.Partition == nil {
				if int32(updated) != *gss.Spec.Replicas {
					return false, nil
				}
			} else {
				if int32(updated) != *gss.Spec.Replicas-*gss.Spec.UpdateStrategy.RollingUpdate.Partition {
					return false, nil
				}
			}
			return true, nil
		})
}

func (f *Framework) ExpectGssCorrect(gss *gamekruiseiov1alpha1.GameServerSet, expectIndex []int) error {

	if err := f.WaitForGsCreated(gss); err != nil {
		return err
	}

	gssName := gss.GetName()
	labelSelector := labels.SelectorFromSet(map[string]string{
		gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
	}).String()

	podList, err := f.client.GetPodList(labelSelector)
	if err != nil {
		return err
	}

	podIndexList := util.GetIndexListFromPodList(podList.Items)

	if !util.IsSliceEqual(expectIndex, podIndexList) {
		return fmt.Errorf("current pods and expected pods do not correspond")
	}

	return nil
}

func (f *Framework) WaitForGsOpsStateUpdate(gsName string, opsState string) error {
	return wait.PollImmediate(5*time.Second, 1*time.Minute,
		func() (done bool, err error) {
			pod, err := f.client.GetPod(gsName)
			if err != nil {
				return false, err
			}
			currentOpsState := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOpsStateKey]
			if currentOpsState == opsState {
				return true, nil
			}
			return false, nil
		})
}

func (f *Framework) WaitForGsDeletionPriorityUpdated(gsName string, deletionPriority string) error {
	return wait.PollImmediate(5*time.Second, 1*time.Minute,
		func() (done bool, err error) {
			pod, err := f.client.GetPod(gsName)
			if err != nil {
				return false, err
			}
			currentPriority := pod.GetLabels()[gamekruiseiov1alpha1.GameServerDeletePriorityKey]
			if currentPriority == deletionPriority {
				return true, nil
			}
			return false, nil
		})
}

func (f *Framework) DeletePodDirectly(index int) error {
	gsName := client.GameServerSet + "-" + strconv.Itoa(index)
	return f.client.DeletePod(gsName)
}

func (f *Framework) ExpectGsCorrect(gsName, opsState, dp, up string) error {
	gs, err := f.client.GetGameServer(gsName)
	if err != nil {
		return err
	}

	if gs.Status.DeletionPriority.String() != dp || gs.Status.UpdatePriority.String() != up || string(gs.Spec.OpsState) != opsState {
		return fmt.Errorf("current GameServer is wrong")
	}

	return nil
}

func (f *Framework) WaitForGsUpdatePriorityUpdated(gsName string, updatePriority string) error {
	return wait.PollImmediate(5*time.Second, 1*time.Minute,
		func() (done bool, err error) {
			pod, err := f.client.GetPod(gsName)
			if err != nil {
				return false, err
			}
			currentPriority := pod.GetLabels()[gamekruiseiov1alpha1.GameServerUpdatePriorityKey]
			if currentPriority == updatePriority {
				return true, nil
			}
			return false, nil
		})
}
