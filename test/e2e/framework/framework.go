package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
	client    *client.Client
	testStart time.Time
}

func NewFrameWork(config *restclient.Config) *Framework {
	kruisegameClient := kruisegameclientset.NewForConfigOrDie(config)
	kubeClient := clientset.NewForConfigOrDie(config)
	return &Framework{
		client: client.NewKubeClient(kruisegameClient, kubeClient),
	}
}

// MarkTestStart records the approximate start time of the current test.
func (f *Framework) MarkTestStart() { f.testStart = time.Now() }

// DumpAuditIfFailed prints a trimmed audit log snippet for the test namespace when the test fails.
func (f *Framework) DumpAuditIfFailed() {
	const auditFile = "/tmp/kind-audit/audit.log"
	if _, err := os.Stat(auditFile); err != nil {
		return
	}
	// Primary: events since test start
	limit := 400
	snippet := FilterAuditLog(auditFile, f.testStart, client.Namespace, limit)
	// Fallback: if nothing matched (e.g., clock skew, strict filters), show a shorter tail
	if snippet == "" {
		limit = 200
		snippet = FilterAuditLog(auditFile, time.Time{}, client.Namespace, limit)
	}
	if snippet != "" {
		fmt.Printf("\n===== AUDIT (last %d matching entries since %s) =====\n%s\n===== END AUDIT =====\n", limit, f.testStart.Format(time.RFC3339), snippet)
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

func (f *Framework) DeployGameServerSetWithNetwork(networkType string, conf []gamekruiseiov1alpha1.NetworkConfParams, ports []corev1.ContainerPort) (*gamekruiseiov1alpha1.GameServerSet, error) {
	gss := f.client.DefaultGameServerSet()
	gss.Spec.Network = &gamekruiseiov1alpha1.Network{
		NetworkType: networkType,
		NetworkConf: conf,
	}
	if len(ports) > 0 && len(gss.Spec.GameServerTemplate.Spec.Containers) > 0 {
		gss.Spec.GameServerTemplate.Spec.Containers[0].Ports = append(gss.Spec.GameServerTemplate.Spec.Containers[0].Ports, ports...)
	}
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

// WaitForGssCounts waits until both Pod and GameServer counts equal desired.
func (f *Framework) WaitForGssCounts(gss *gamekruiseiov1alpha1.GameServerSet, desired int) error {
	gssName := gss.GetName()
	labelSelector := labels.SelectorFromSet(map[string]string{
		gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
	}).String()
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			podList, err := f.client.GetPodList(labelSelector)
			if err != nil {
				return false, nil
			}
			if len(podList.Items) != desired {
				return false, nil
			}
			gsList, err := f.client.GetGameServerList(labelSelector)
			if err != nil {
				return false, nil
			}
			if len(gsList.Items) != desired {
				return false, nil
			}
			return true, nil
		})
}

// WaitForGssObservedGeneration waits until status.observedGeneration reaches at least the targetGeneration.
func (f *Framework) WaitForGssObservedGeneration(targetGeneration int64) error {
	return f.WaitForGss(func(g *gamekruiseiov1alpha1.GameServerSet) (bool, error) {
		if g.Generation < targetGeneration {
			return false, nil
		}
		if g.Status.ObservedGeneration < targetGeneration {
			return false, nil
		}
		return true, nil
	})
}

// WaitForReplicasConverge waits until gss.Spec.Replicas equals desired and
// on timeout returns a detailed snapshot of last observed state to aid debugging.
func (f *Framework) WaitForReplicasConverge(gss *gamekruiseiov1alpha1.GameServerSet, desired int) error {
	gssName := gss.GetName()
	labelSelector := labels.SelectorFromSet(map[string]string{
		gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
	}).String()
	var lastSpecReplicas int
	var lastStatusReplicas int
	var lastCurrentReplicas int
	var lastPodCount, lastGsCount int
	var lastPodOrdinals []int
	err := wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			// Fetch latest GSS
			g, err := f.client.GetGameServerSet()
			if err != nil {
				return false, nil
			}
			if g.Spec.Replicas != nil {
				lastSpecReplicas = int(*g.Spec.Replicas)
			}
			lastStatusReplicas = int(g.Status.Replicas)
			lastCurrentReplicas = int(g.Status.CurrentReplicas)
			// Fetch Pods and GameServers for context
			if podList, err := f.client.GetPodList(labelSelector); err == nil {
				lastPodCount = len(podList.Items)
				lastPodOrdinals = util.GetIndexListFromPodList(podList.Items)
			}
			if gsList, err := f.client.GetGameServerList(labelSelector); err == nil {
				lastGsCount = len(gsList.Items)
			}
			return lastSpecReplicas == desired, nil
		})
	if err != nil {
		return fmt.Errorf(
			"WaitForReplicasConverge timeout: want=%d spec=%d status.replicas=%d status.current=%d pods=%d gs=%d ordinals=%v",
			desired, lastSpecReplicas, lastStatusReplicas, lastCurrentReplicas, lastPodCount, lastGsCount, lastPodOrdinals,
		)
	}
	return nil
}

// GetGameServerSet fetches the current GameServerSet from cluster.
func (f *Framework) GetGameServerSet() (*gamekruiseiov1alpha1.GameServerSet, error) {
	return f.client.GetGameServerSet()
}

// WaitForGssReplicas waits until .spec.replicas equals the desired value.
// WaitForGss fetches GSS periodically and evaluates a predicate until it returns true.
func (f *Framework) WaitForGss(predicate func(*gamekruiseiov1alpha1.GameServerSet) (bool, error)) error {
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			gss, err := f.client.GetGameServerSet()
			if err != nil {
				return false, err
			}
			return predicate(gss)
		})
}

func (f *Framework) ExpectGssCorrect(gss *gamekruiseiov1alpha1.GameServerSet, expectIndex []int) error {
	// First wait until the number of objects matches the expected replica count
	// (avoids relying on spec.replicas which may be delayed).
	desired := len(expectIndex)
	if err := f.WaitForGssCounts(gss, desired); err != nil {
		return err
	}

	gssName := gss.GetName()
	labelSelector := labels.SelectorFromSet(map[string]string{
		gamekruiseiov1alpha1.GameServerOwnerGssKey: gssName,
	}).String()

	// capture last observed snapshot for better error messages
	var lastPodIndexes []int
	var lastPodCount, lastGsCount int

	// Then poll only for whether the ordinals match (while ensuring the counts remain consistent).
	err := wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			podList, err := f.client.GetPodList(labelSelector)
			if err != nil {
				return false, nil
			}
			lastPodCount = len(podList.Items)
			lastPodIndexes = util.GetIndexListFromPodList(podList.Items)

			gsList, err := f.client.GetGameServerList(labelSelector)
			if err != nil {
				return false, nil
			}
			lastGsCount = len(gsList.Items)

			// ensure the counts still match the expected value
			if lastPodCount != desired || lastGsCount != desired {
				return false, nil
			}
			if util.IsSliceEqual(expectIndex, lastPodIndexes) {
				return true, nil
			}
			return false, nil
		})
	if err != nil {
		return fmt.Errorf("ExpectGssCorrect timeout: desired=%d expectedOrdinals=%v lastPodOrdinals=%v lastPodCount=%d lastGsCount=%d", desired, expectIndex, lastPodIndexes, lastPodCount, lastGsCount)
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

// WaitForGsSpecOpsState waits until GameServer.spec.opsState reaches the desired value.
func (f *Framework) WaitForGsSpecOpsState(gsName string, opsState string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			gs, err := f.client.GetGameServer(gsName)
			if err != nil {
				return false, err
			}
			if string(gs.Spec.OpsState) == opsState {
				return true, nil
			}
			return false, nil
		})
}

// WaitForPodOpsStateOrDeleted waits until the pod label opsState equals the target or the pod is deleted.
func (f *Framework) WaitForPodOpsStateOrDeleted(gsName string, opsState string) error {
	var lastPodOpsState, lastPodPhase, lastPodUID, lastGsSpecOps string
	var lastPodErr, lastGsErr error
	var lastDeletionTimestamp string
	err := wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			if pod, err0 := f.client.GetPod(gsName); err0 == nil {
				lastPodOpsState = pod.GetLabels()[gamekruiseiov1alpha1.GameServerOpsStateKey]
				lastPodPhase = string(pod.Status.Phase)
				lastPodUID = string(pod.UID)
				if pod.DeletionTimestamp != nil {
					lastDeletionTimestamp = pod.DeletionTimestamp.Time.Format(time.RFC3339)
				} else {
					lastDeletionTimestamp = "<nil>"
				}
				if lastPodOpsState == opsState {
					return true, nil
				}
			} else {
				if apierrors.IsNotFound(err0) {
					return true, nil
				}
				lastPodErr = err0
			}

			if gs, err1 := f.client.GetGameServer(gsName); err1 == nil {
				lastGsSpecOps = string(gs.Spec.OpsState)
			} else {
				lastGsErr = err1
			}
			return false, nil
		})
	if err != nil {
		return fmt.Errorf("WaitForPodOpsStateOrDeleted timeout: wantOps=%s lastPodOps=%s lastPodPhase=%s lastPodUID=%s lastPodDeletionTs=%s lastGsSpecOps=%s podErr=%v gsErr=%v",
			opsState, lastPodOpsState, lastPodPhase, lastPodUID, lastDeletionTimestamp, lastGsSpecOps, lastPodErr, lastGsErr)
	}
	return nil
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

func (f *Framework) WaitForGsDesiredNetworkState(gsName string, desired gamekruiseiov1alpha1.NetworkState) error {
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			gs, err := f.client.GetGameServer(gsName)
			if err != nil {
				return false, err
			}
			if gs.Status.NetworkStatus.DesiredNetworkState == desired {
				return true, nil
			}
			return false, nil
		})
}

func (f *Framework) WaitForNodePortServiceSelector(gsName string, disabled bool) error {
	const (
		activeKey   = "statefulset.kubernetes.io/pod-name"
		disabledKey = "game.kruise.io/svc-selector-disabled"
	)
	return wait.PollUntilContextTimeout(context.TODO(), 5*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			svc, err := f.client.GetService(gsName)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			selector := svc.Spec.Selector
			_, hasActive := selector[activeKey]
			disabledVal, hasDisabled := selector[disabledKey]
			if disabled {
				if !hasActive && hasDisabled && disabledVal == gsName {
					return true, nil
				}
				return false, nil
			}
			if hasActive && selector[activeKey] == gsName && !hasDisabled {
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
