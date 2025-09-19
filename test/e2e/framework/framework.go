package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	kruisegameclientset "github.com/openkruise/kruise-game/pkg/client/clientset/versioned"
	"github.com/openkruise/kruise-game/pkg/util"
	"github.com/openkruise/kruise-game/test/e2e/client"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
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
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			err = f.client.DeleteGameServerSet()
			if err != nil && !apierrors.IsNotFound(err) {
				{
					return false, err
				}
			}

			labelSelector := labels.SelectorFromSet(map[string]string{
				gamekruiseiov1alpha1.GameServerOwnerGssKey: client.GameServerSet,
			}).String()
			podList, err := f.client.GetPodList(labelSelector)
			if err != nil {
				return false, err
			}
			if len(podList.Items) != 0 {
				return false, nil
			}
			return true, nil
		})
}

func (f *Framework) DeployGameServerSet() (*gamekruiseiov1alpha1.GameServerSet, error) {
	gss := f.client.DefaultGameServerSet()
	return f.client.CreateGameServerSet(gss)
}

func (f *Framework) GetGameServer(name string) (*gamekruiseiov1alpha1.GameServer, error) {
	return f.client.GetGameServer(name)
}

func (f *Framework) DeployGameServerSetWithReclaimPolicy(reclaimPolicy gamekruiseiov1alpha1.GameServerReclaimPolicy) (*gamekruiseiov1alpha1.GameServerSet, error) {
	gss := f.client.DefaultGameServerSet()
	gss.Spec.GameServerTemplate.ReclaimPolicy = reclaimPolicy
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

func (f *Framework) GameServerScale(gss *gamekruiseiov1alpha1.GameServerSet, desireNum int, reserveGsId *intstr.IntOrString) (*gamekruiseiov1alpha1.GameServerSet, error) {
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

// PatchGssSpec applies a generic spec-only merge patch using a map of fields.
func (f *Framework) PatchGssSpec(specFields map[string]interface{}) (*gamekruiseiov1alpha1.GameServerSet, error) {
	patch := map[string]interface{}{"spec": specFields}
	data, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	return f.client.PatchGameServerSet(data)
}

// PatchGss applies a generic merge patch to the GameServerSet resource.
func (f *Framework) PatchGss(patch map[string]interface{}) (*gamekruiseiov1alpha1.GameServerSet, error) {
	data, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	return f.client.PatchGameServerSet(data)
}

// PatchGameServerSpec applies a merge patch to GameServer.spec using provided fields.
func (f *Framework) PatchGameServerSpec(gsName string, specFields map[string]interface{}) (*gamekruiseiov1alpha1.GameServer, error) {
	patch := map[string]interface{}{"spec": specFields}
	data, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	return f.client.PatchGameServer(gsName, data)
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
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
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
			gsList, err := f.client.GetGameServerList(labelSelector)
			if err != nil {
				return false, err
			}
			if len(gsList.Items) != int(*gss.Spec.Replicas) {
				return false, nil
			}

			return true, nil
		})
}

func (f *Framework) WaitForUpdated(gss *gamekruiseiov1alpha1.GameServerSet, name, image string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 10*time.Second, 10*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
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
		return fmt.Errorf("current pods and expected pods do not correspond, actual podIndexList: %v", podIndexList)
	}

	return nil
}

func (f *Framework) WaitForGsOpsStateUpdate(gsName string, opsState string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
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
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
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
	var uid types.UID
	podName := client.GameServerSet + "-" + strconv.Itoa(index)

	// get
	if err := wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {

			pod, err := f.client.GetPod(podName)
			if err != nil {
				return false, err
			}
			uid = pod.UID
			return true, nil
		}); err != nil {
		return err
	}

	// delete
	if err := f.client.DeletePod(podName); err != nil {
		return err
	}

	// check
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			pod, err := f.client.GetPod(podName)
			if err != nil {
				return false, err
			}
			if pod.UID == uid {
				return false, nil
			}
			return true, nil
		})
}

func (f *Framework) WaitForPodDeleted(podName string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			_, err = f.client.GetPod(podName)
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, nil
		})
}

func (f *Framework) ExpectGsCorrect(gsName, opsState, dp, up string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 5*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			gs, err := f.client.GetGameServer(gsName)
			if err != nil {
				return false, nil
			}

			if gs.Status.DeletionPriority.String() != dp || gs.Status.UpdatePriority.String() != up || string(gs.Spec.OpsState) != opsState {
				return false, nil
			}
			return true, nil
		})
}

func (f *Framework) WaitForGsUpdatePriorityUpdated(gsName string, updatePriority string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
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

func (f *Framework) WaitForGsNetworkDisabled(gsName string, want bool) error {
	var lastPodValue, lastSpecValue string
	var lastSpecNil bool
	var lastPodErr, lastGsErr error
	wantStr := strconv.FormatBool(want)

	err := wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			specMatched := false
			if gs, err0 := f.client.GetGameServer(gsName); err0 == nil {
				lastSpecValue = strconv.FormatBool(ptr.Deref(gs.Spec.NetworkDisabled, false))
				lastSpecNil = gs.Spec.NetworkDisabled == nil
				lastGsErr = nil
				if lastSpecValue == wantStr {
					specMatched = true
				}
			} else {
				lastGsErr = err0
			}

			if pod, err1 := f.client.GetPod(gsName); err1 == nil {
				lastPodValue = pod.GetLabels()[gamekruiseiov1alpha1.GameServerNetworkDisabled]
				lastPodErr = nil
				if specMatched && lastPodValue == wantStr {
					return true, nil
				}
			} else {
				lastPodErr = err1
			}
			return false, nil
		})
	if err != nil {
		return fmt.Errorf("WaitForGsNetworkDisabled timeout: want=%s lastPod=%s lastSpec=%s lastSpecNil=%t podErr=%v gsErr=%v",
			wantStr, lastPodValue, lastSpecValue, lastSpecNil, lastPodErr, lastGsErr)
	}
	return nil
}
