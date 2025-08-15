package gameserver

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
)

func TestPolyMessageReason(t *testing.T) {
	tests := []struct {
		message       string
		reason        string
		newMessage    string
		newReason     string
		resultMessage string
		resultReason  string
	}{
		// case 0
		{
			message:       "",
			reason:        "reason_0",
			newMessage:    "message_1",
			newReason:     "reason_1",
			resultMessage: "; message_1",
			resultReason:  "reason_0; reason_1",
		},
		// case 1
		{
			message:       "message_0",
			reason:        "",
			newMessage:    "message_1",
			newReason:     "reason_1",
			resultMessage: "message_0; message_1",
			resultReason:  "; reason_1",
		},
		// case 2
		{
			message:       "",
			reason:        "",
			newMessage:    "message_1",
			newReason:     "reason_1",
			resultMessage: "message_1",
			resultReason:  "reason_1",
		},
		// case 3
		{
			message:       "message_0",
			reason:        "reason_0",
			newMessage:    "message_1",
			newReason:     "reason_1",
			resultMessage: "message_0; message_1",
			resultReason:  "reason_0; reason_1",
		},
		// case 4
		{
			message:       "NodeStatusUnknown",
			reason:        "Kubelet stopped posting node status.",
			newMessage:    "NodeStatusUnknown",
			newReason:     "Kubelet stopped posting node status.",
			resultMessage: "NodeStatusUnknown",
			resultReason:  "Kubelet stopped posting node status.",
		},
		// case 5
		{
			reason:        "MemoryPressure:NodeStatusUnknown",
			message:       "Kubelet stopped posting node status.",
			newReason:     "PIDPressure:NodeStatusUnknown",
			newMessage:    "Kubelet stopped posting node status.",
			resultReason:  "MemoryPressure:NodeStatusUnknown; PIDPressure:NodeStatusUnknown",
			resultMessage: "Kubelet stopped posting node status.",
		},
	}

	for i, test := range tests {
		actualMessage, actualReason := polyMessageReason(test.message, test.reason, test.newMessage, test.newReason)
		if test.resultMessage != actualMessage {
			t.Errorf("case %d: expect message is %s, but actually is %s", i, test.resultMessage, actualMessage)
		}
		if test.resultReason != actualReason {
			t.Errorf("case %d: expect reason is %s, but actually is %s", i, test.resultReason, actualReason)
		}
	}
}

func TestPolyCondition(t *testing.T) {
	tests := []struct {
		before []gamekruiseiov1alpha1.GameServerCondition
		after  gamekruiseiov1alpha1.GameServerCondition
	}{
		// case 0
		{
			before: []gamekruiseiov1alpha1.GameServerCondition{
				{
					Type:    gamekruiseiov1alpha1.PersistentVolumeNormal,
					Status:  corev1.ConditionFalse,
					Message: "message_0",
					Reason:  "reason_0",
				},
				{
					Type:    gamekruiseiov1alpha1.PersistentVolumeNormal,
					Status:  corev1.ConditionFalse,
					Message: "message_1",
					Reason:  "reason_1",
				},
			},
			after: gamekruiseiov1alpha1.GameServerCondition{
				Type:    gamekruiseiov1alpha1.PersistentVolumeNormal,
				Status:  corev1.ConditionFalse,
				Message: "message_0; message_1",
				Reason:  "reason_0; reason_1",
			},
		},
		// case 1
		{
			before: []gamekruiseiov1alpha1.GameServerCondition{
				{
					Type:    gamekruiseiov1alpha1.PersistentVolumeNormal,
					Status:  corev1.ConditionFalse,
					Message: "message_0",
					Reason:  "reason_0",
				},
				{
					Type:    gamekruiseiov1alpha1.PodNormal,
					Status:  corev1.ConditionFalse,
					Message: "message_1",
					Reason:  "reason_1",
				},
			},
			after: gamekruiseiov1alpha1.GameServerCondition{
				Type:    gamekruiseiov1alpha1.PersistentVolumeNormal,
				Status:  corev1.ConditionFalse,
				Message: "message_0",
				Reason:  "reason_0",
			},
		},
		// case 2
		{
			before: []gamekruiseiov1alpha1.GameServerCondition{
				{
					Type:    gamekruiseiov1alpha1.PersistentVolumeNormal,
					Status:  corev1.ConditionTrue,
					Message: "message_0",
					Reason:  "reason_0",
				},
				{
					Type:    gamekruiseiov1alpha1.PodNormal,
					Status:  corev1.ConditionFalse,
					Message: "message_1",
					Reason:  "reason_1",
				},
			},
			after: gamekruiseiov1alpha1.GameServerCondition{
				Type:    gamekruiseiov1alpha1.PodNormal,
				Status:  corev1.ConditionFalse,
				Message: "message_1",
				Reason:  "reason_1",
			},
		},
		// case 3
		{
			before: []gamekruiseiov1alpha1.GameServerCondition{},
			after:  gamekruiseiov1alpha1.GameServerCondition{},
		},
		// case 4
		{
			before: []gamekruiseiov1alpha1.GameServerCondition{
				{
					Type:    gamekruiseiov1alpha1.PersistentVolumeNormal,
					Status:  corev1.ConditionTrue,
					Message: "message_0",
					Reason:  "reason_0",
				},
				{
					Type:    gamekruiseiov1alpha1.PersistentVolumeNormal,
					Status:  corev1.ConditionTrue,
					Message: "message_1",
					Reason:  "reason_1",
				},
			},
			after: gamekruiseiov1alpha1.GameServerCondition{
				Type:    gamekruiseiov1alpha1.PersistentVolumeNormal,
				Status:  corev1.ConditionTrue,
				Message: "message_0; message_1",
				Reason:  "reason_0; reason_1",
			},
		},
	}

	for i, test := range tests {
		actual := polyCondition(test.before)
		test.after.LastProbeTime = actual.LastProbeTime
		test.after.LastTransitionTime = actual.LastTransitionTime
		if !reflect.DeepEqual(test.after, actual) {
			t.Errorf("case %d: expect condition is %v, but actually is %v", i, test.after, actual)
		}
	}
}

func TestPvNotFoundCondition(t *testing.T) {
	tests := []struct {
		pvcName   string
		namespace string
		condition gamekruiseiov1alpha1.GameServerCondition
	}{
		{
			pvcName:   "pvc_0",
			namespace: "default",
			condition: gamekruiseiov1alpha1.GameServerCondition{
				Type:    gamekruiseiov1alpha1.PersistentVolumeNormal,
				Status:  corev1.ConditionFalse,
				Reason:  pvNotFoundReason,
				Message: "There is no pv which pvc default/pvc_0 is bound with",
			},
		},
	}

	for i, test := range tests {
		actual := pvNotFoundCondition(test.namespace, test.pvcName)
		test.condition.LastProbeTime = actual.LastProbeTime
		test.condition.LastTransitionTime = actual.LastTransitionTime
		if !reflect.DeepEqual(test.condition, actual) {
			t.Errorf("case %d: expect condition is %v ,but actually is %v", i, test.condition, actual)
		}
	}
}

func TestPvcNotFoundCondition(t *testing.T) {
	tests := []struct {
		pvcName   string
		namespace string
		condition gamekruiseiov1alpha1.GameServerCondition
	}{
		{
			pvcName:   "pvc_0",
			namespace: "default",
			condition: gamekruiseiov1alpha1.GameServerCondition{
				Type:    gamekruiseiov1alpha1.PersistentVolumeNormal,
				Status:  corev1.ConditionFalse,
				Reason:  pvcNotFoundReason,
				Message: "There is no pvc named default/pvc_0 in cluster",
			},
		},
	}

	for i, test := range tests {
		actual := pvcNotFoundCondition(test.namespace, test.pvcName)
		test.condition.LastProbeTime = actual.LastProbeTime
		test.condition.LastTransitionTime = actual.LastTransitionTime
		if !reflect.DeepEqual(test.condition, actual) {
			t.Errorf("case %d: expect condition is %v ,but actually is %v", i, test.condition, actual)
		}
	}
}

func TestGetPersistentVolumeConditions(t *testing.T) {
	tests := []struct {
		pvs       []*corev1.PersistentVolume
		condition gamekruiseiov1alpha1.GameServerCondition
	}{
		// case 0
		{
			pvs: []*corev1.PersistentVolume{
				{
					Status: corev1.PersistentVolumeStatus{
						Message: "message_0",
						Reason:  "reason_0",
					},
				},
				{
					Status: corev1.PersistentVolumeStatus{
						Message: "message_1",
						Reason:  "reason_1",
					},
				},
			},
			condition: gamekruiseiov1alpha1.GameServerCondition{
				Type:    gamekruiseiov1alpha1.PersistentVolumeNormal,
				Status:  corev1.ConditionFalse,
				Message: "message_0; message_1",
				Reason:  "reason_0; reason_1",
			},
		},
		// case 1
		{
			pvs: []*corev1.PersistentVolume{
				{
					Status: corev1.PersistentVolumeStatus{
						Message: "message_0",
						Reason:  "reason_0",
					},
				},
				{
					Status: corev1.PersistentVolumeStatus{
						Message: "",
						Reason:  "",
					},
				},
			},
			condition: gamekruiseiov1alpha1.GameServerCondition{
				Type:    gamekruiseiov1alpha1.PersistentVolumeNormal,
				Status:  corev1.ConditionFalse,
				Message: "message_0",
				Reason:  "reason_0",
			},
		},
		// case 2
		{
			pvs: []*corev1.PersistentVolume{
				{
					Status: corev1.PersistentVolumeStatus{
						Message: "",
						Reason:  "",
					},
				},
				{
					Status: corev1.PersistentVolumeStatus{
						Message: "",
						Reason:  "",
					},
				},
			},
			condition: gamekruiseiov1alpha1.GameServerCondition{
				Type:   gamekruiseiov1alpha1.PersistentVolumeNormal,
				Status: corev1.ConditionTrue,
			},
		},
	}

	for i, test := range tests {
		actual := getPersistentVolumeConditions(test.pvs)
		test.condition.LastProbeTime = actual.LastProbeTime
		test.condition.LastTransitionTime = actual.LastTransitionTime
		if !reflect.DeepEqual(test.condition, actual) {
			t.Errorf("case %d: expect condition is %v, but actually is %v", i, test.condition, actual)
		}
	}
}

func TestGetNodeConditions(t *testing.T) {
	tests := []struct {
		node      *corev1.Node
		condition gamekruiseiov1alpha1.GameServerCondition
	}{
		// case 0
		{
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:    corev1.NodeReady,
							Status:  corev1.ConditionTrue,
							Reason:  "KubeletReady",
							Message: "kubelet is posting ready status",
						},
						{
							Type:    corev1.NodeDiskPressure,
							Status:  corev1.ConditionFalse,
							Reason:  "KubeletHasNoDiskPressure",
							Message: "kubelet has no disk pressure",
						},
					},
				},
			},
			condition: gamekruiseiov1alpha1.GameServerCondition{
				Type:   gamekruiseiov1alpha1.NodeNormal,
				Status: corev1.ConditionTrue,
			},
		},
		// case 1
		{
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:    corev1.NodeReady,
							Status:  corev1.ConditionFalse,
							Reason:  "KubeletNotReady",
							Message: "kubelet is not posting ready status",
						},
						{
							Type:    corev1.NodeDiskPressure,
							Status:  corev1.ConditionTrue,
							Reason:  "KubeletHasDiskPressure",
							Message: "kubelet has disk pressure",
						},
					},
				},
			},
			condition: gamekruiseiov1alpha1.GameServerCondition{
				Type:    gamekruiseiov1alpha1.NodeNormal,
				Status:  corev1.ConditionFalse,
				Reason:  "Ready:KubeletNotReady; DiskPressure:KubeletHasDiskPressure",
				Message: "kubelet is not posting ready status; kubelet has disk pressure",
			},
		},
		// case 2
		{
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:    corev1.NodeDiskPressure,
							Status:  corev1.ConditionUnknown,
							Reason:  "NodeStatusUnknown",
							Message: "Kubelet stopped posting node status.",
						},
						{
							Type:    corev1.NodeMemoryPressure,
							Status:  corev1.ConditionUnknown,
							Reason:  "NodeStatusUnknown",
							Message: "Kubelet stopped posting node status.",
						},
					},
				},
			},
			condition: gamekruiseiov1alpha1.GameServerCondition{
				Type:    gamekruiseiov1alpha1.NodeNormal,
				Status:  corev1.ConditionFalse,
				Reason:  "DiskPressure:NodeStatusUnknown; MemoryPressure:NodeStatusUnknown",
				Message: "Kubelet stopped posting node status.",
			},
		},
	}

	for i, test := range tests {
		actual := getNodeConditions(test.node)
		test.condition.LastProbeTime = actual.LastProbeTime
		test.condition.LastTransitionTime = actual.LastTransitionTime
		if !reflect.DeepEqual(test.condition, actual) {
			t.Errorf("case %d: expect condition is %v, but actually is %v", i, test.condition, actual)
		}
	}
}

func TestGetPodConditions(t *testing.T) {
	tests := []struct {
		pod       *corev1.Pod
		condition gamekruiseiov1alpha1.GameServerCondition
	}{
		// case 0
		{
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:    corev1.PodScheduled,
							Status:  corev1.ConditionFalse,
							Reason:  "Reason_0",
							Message: "Message_0",
						},
					},
				},
			},
			condition: gamekruiseiov1alpha1.GameServerCondition{
				Type:    gamekruiseiov1alpha1.PodNormal,
				Status:  corev1.ConditionFalse,
				Reason:  "Reason_0",
				Message: "Message_0",
			},
		},
		// case 1
		{
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:    corev1.ContainersReady,
							Status:  corev1.ConditionTrue,
							Reason:  "",
							Message: "",
						},
						{
							Type:    corev1.PodReady,
							Status:  corev1.ConditionFalse,
							Reason:  "Readiness Failed",
							Message: "container game-server readiness probe failed",
						},
					},
				},
			},
			condition: gamekruiseiov1alpha1.GameServerCondition{
				Type:    gamekruiseiov1alpha1.PodNormal,
				Status:  corev1.ConditionFalse,
				Reason:  "Readiness Failed",
				Message: "container game-server readiness probe failed",
			},
		},
		// case 2
		{
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 137,
									Message:  "message_0",
									Reason:   "reason_0",
								},
							},
						},
					},
					Conditions: []corev1.PodCondition{
						{
							Type:    corev1.ContainersReady,
							Status:  corev1.ConditionFalse,
							Reason:  "reason_1",
							Message: "message_1",
						},
						{
							Type:    corev1.PodReady,
							Status:  corev1.ConditionFalse,
							Reason:  "reason_2",
							Message: "message_2",
						},
					},
				},
			},
			condition: gamekruiseiov1alpha1.GameServerCondition{
				Type:    gamekruiseiov1alpha1.PodNormal,
				Status:  corev1.ConditionFalse,
				Reason:  "reason_1; ContainerTerminated:reason_0",
				Message: "message_1; message_0 ExitCode: 137",
			},
		},
		// case 3
		{
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{
									Message: "message_0",
									Reason:  "reason_0",
								},
							},
						},
					},
					Conditions: []corev1.PodCondition{
						{
							Type:    corev1.PodInitialized,
							Status:  corev1.ConditionFalse,
							Reason:  "reason_1",
							Message: "message_1",
						},
						{
							Type:    corev1.PodReady,
							Status:  corev1.ConditionFalse,
							Reason:  "reason_2",
							Message: "message_2",
						},
					},
				},
			},
			condition: gamekruiseiov1alpha1.GameServerCondition{
				Type:    gamekruiseiov1alpha1.PodNormal,
				Status:  corev1.ConditionFalse,
				Reason:  "reason_1; reason_0",
				Message: "message_1; message_0",
			},
		},
		// case 4
		{
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:    corev1.PodScheduled,
							Status:  corev1.ConditionTrue,
							Reason:  "",
							Message: "",
						},
					},
				},
			},
			condition: gamekruiseiov1alpha1.GameServerCondition{
				Type:   gamekruiseiov1alpha1.PodNormal,
				Status: corev1.ConditionTrue,
			},
		},
	}

	for i, test := range tests {
		actual := getPodConditions(test.pod)
		test.condition.LastProbeTime = actual.LastProbeTime
		test.condition.LastTransitionTime = actual.LastTransitionTime
		if !reflect.DeepEqual(test.condition, actual) {
			t.Errorf("case %d: expect condition is %v, but actually is %v", i, test.condition, actual)
		}
	}
}

func TestIsConditionEqual(t *testing.T) {
	tests := []struct {
		a      gamekruiseiov1alpha1.GameServerCondition
		b      gamekruiseiov1alpha1.GameServerCondition
		result bool
	}{
		{
			a:      gamekruiseiov1alpha1.GameServerCondition{},
			b:      gamekruiseiov1alpha1.GameServerCondition{},
			result: true,
		},
		{
			a: gamekruiseiov1alpha1.GameServerCondition{
				Type:   gamekruiseiov1alpha1.PodNormal,
				Status: corev1.ConditionFalse,
			},
			b:      gamekruiseiov1alpha1.GameServerCondition{},
			result: false,
		},
		{
			a: gamekruiseiov1alpha1.GameServerCondition{
				Type:   gamekruiseiov1alpha1.PodNormal,
				Status: corev1.ConditionFalse,
			},
			b: gamekruiseiov1alpha1.GameServerCondition{
				Type:   gamekruiseiov1alpha1.PodNormal,
				Status: corev1.ConditionFalse,
			},
			result: true,
		},
		{
			a: gamekruiseiov1alpha1.GameServerCondition{
				Type:    gamekruiseiov1alpha1.PodNormal,
				Status:  corev1.ConditionFalse,
				Message: "xxx",
			},
			b: gamekruiseiov1alpha1.GameServerCondition{
				Type:   gamekruiseiov1alpha1.PodNormal,
				Status: corev1.ConditionFalse,
				Reason: "xxx",
			},
			result: false,
		},
		{
			a: gamekruiseiov1alpha1.GameServerCondition{
				Type:   gamekruiseiov1alpha1.PodNormal,
				Status: corev1.ConditionFalse,
				Reason: "xxx",
			},
			b: gamekruiseiov1alpha1.GameServerCondition{
				Type:   gamekruiseiov1alpha1.PodNormal,
				Status: corev1.ConditionFalse,
				Reason: "xxx",
			},
			result: true,
		},
		{
			a: gamekruiseiov1alpha1.GameServerCondition{
				Type:   gamekruiseiov1alpha1.PodNormal,
				Status: corev1.ConditionFalse,
				Reason: "",
			},
			b: gamekruiseiov1alpha1.GameServerCondition{
				Type:   gamekruiseiov1alpha1.PodNormal,
				Status: corev1.ConditionFalse,
			},
			result: true,
		},
		{
			a: gamekruiseiov1alpha1.GameServerCondition{
				Type:          gamekruiseiov1alpha1.PodNormal,
				Status:        corev1.ConditionTrue,
				LastProbeTime: metav1.Now(),
			},
			b: gamekruiseiov1alpha1.GameServerCondition{
				Type:          gamekruiseiov1alpha1.PodNormal,
				Status:        corev1.ConditionTrue,
				LastProbeTime: metav1.Now(),
			},
			result: true,
		},
	}

	for i, test := range tests {
		actual := isConditionEqual(test.a, test.b)
		if test.result != actual {
			t.Errorf("case %d: expect result is %v, but actually is %v", i, test.result, actual)
		}
	}
}

func TestGetGsCondition(t *testing.T) {
	tests := []struct {
		conditions    []gamekruiseiov1alpha1.GameServerCondition
		conditionType gamekruiseiov1alpha1.GameServerConditionType
		result        gamekruiseiov1alpha1.GameServerCondition
	}{
		// case 0
		{
			conditions:    []gamekruiseiov1alpha1.GameServerCondition{},
			conditionType: gamekruiseiov1alpha1.PodNormal,
			result:        gamekruiseiov1alpha1.GameServerCondition{},
		},
		// case 1
		{
			conditions: []gamekruiseiov1alpha1.GameServerCondition{
				{
					Type:    gamekruiseiov1alpha1.NodeNormal,
					Status:  corev1.ConditionFalse,
					Message: "message_0",
					Reason:  "reason_0",
				},
				{
					Type:    gamekruiseiov1alpha1.PodNormal,
					Status:  corev1.ConditionFalse,
					Message: "message_1",
					Reason:  "reason_1",
				},
			},
			conditionType: gamekruiseiov1alpha1.PodNormal,
			result: gamekruiseiov1alpha1.GameServerCondition{
				Type:    gamekruiseiov1alpha1.PodNormal,
				Status:  corev1.ConditionFalse,
				Message: "message_1",
				Reason:  "reason_1",
			},
		},
		// case 2
		{
			conditions: []gamekruiseiov1alpha1.GameServerCondition{
				{
					Type:    gamekruiseiov1alpha1.NodeNormal,
					Status:  corev1.ConditionFalse,
					Message: "message_0",
					Reason:  "reason_0",
				},
				{
					Type:    gamekruiseiov1alpha1.PodNormal,
					Status:  corev1.ConditionFalse,
					Message: "message_1",
					Reason:  "reason_1",
				},
			},
			conditionType: gamekruiseiov1alpha1.PersistentVolumeNormal,
			result:        gamekruiseiov1alpha1.GameServerCondition{},
		},
	}

	for i, test := range tests {
		actual := getGsCondition(test.conditions, test.conditionType)
		if test.result != actual {
			t.Errorf("case %d: expect condition is %v, but actually is %v", i, test.result, actual)
		}
	}
}
