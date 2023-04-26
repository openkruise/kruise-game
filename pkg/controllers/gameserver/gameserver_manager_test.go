package gameserver

import (
	kruiseV1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(gameKruiseV1alpha1.AddToScheme(scheme))
	utilruntime.Must(kruiseV1beta1.AddToScheme(scheme))
	utilruntime.Must(kruiseV1alpha1.AddToScheme(scheme))
}

func TestSyncServiceQualities(t *testing.T) {
	up := intstr.FromInt(20)
	dp := intstr.FromInt(10)
	fakeProbeTime := metav1.Now()
	fakeActionTime := metav1.Now()
	tests := []struct {
		serviceQualities []gameKruiseV1alpha1.ServiceQuality
		podConditions    []corev1.PodCondition
		sqConditions     []gameKruiseV1alpha1.ServiceQualityCondition
		spec             gameKruiseV1alpha1.GameServerSpec
		newSqConditions  []gameKruiseV1alpha1.ServiceQualityCondition
	}{
		{
			serviceQualities: []gameKruiseV1alpha1.ServiceQuality{
				{
					Name:      "healthy",
					Permanent: true,
					ServiceQualityAction: []gameKruiseV1alpha1.ServiceQualityAction{
						{
							State: true,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								UpdatePriority: &up,
							},
						},
						{
							State: false,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								DeletionPriority: &dp,
							},
						},
					},
				},
			},
			podConditions: []corev1.PodCondition{
				{
					Type:          "game.kruise.io/healthy",
					Status:        corev1.ConditionTrue,
					LastProbeTime: fakeProbeTime,
				},
				{
					Type:          "otherA",
					Status:        corev1.ConditionFalse,
					LastProbeTime: fakeProbeTime,
				},
			},
			sqConditions: nil,
			spec: gameKruiseV1alpha1.GameServerSpec{
				UpdatePriority: &up,
			},
			newSqConditions: []gameKruiseV1alpha1.ServiceQualityCondition{
				{
					Name:                     "healthy",
					Status:                   string(corev1.ConditionTrue),
					LastProbeTime:            fakeProbeTime,
					LastActionTransitionTime: fakeActionTime,
				},
			},
		},
		{
			serviceQualities: []gameKruiseV1alpha1.ServiceQuality{
				{
					Name:      "healthy",
					Permanent: true,
					ServiceQualityAction: []gameKruiseV1alpha1.ServiceQualityAction{
						{
							State: true,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								UpdatePriority: &up,
							},
						},
						{
							State: false,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								DeletionPriority: &dp,
							},
						},
					},
				},
			},
			podConditions: []corev1.PodCondition{
				{
					Type:          "game.kruise.io/healthy",
					Status:        corev1.ConditionTrue,
					LastProbeTime: fakeProbeTime,
				},
				{
					Type:          "otherA",
					Status:        corev1.ConditionFalse,
					LastProbeTime: fakeProbeTime,
				},
			},
			sqConditions: []gameKruiseV1alpha1.ServiceQualityCondition{
				{
					Name:                     "healthy",
					Status:                   string(corev1.ConditionFalse),
					LastProbeTime:            fakeProbeTime,
					LastActionTransitionTime: fakeActionTime,
				},
			},
			spec: gameKruiseV1alpha1.GameServerSpec{},
			newSqConditions: []gameKruiseV1alpha1.ServiceQualityCondition{
				{
					Name:                     "healthy",
					Status:                   string(corev1.ConditionTrue),
					LastProbeTime:            fakeProbeTime,
					LastActionTransitionTime: fakeActionTime,
				},
			},
		},
		{
			serviceQualities: []gameKruiseV1alpha1.ServiceQuality{
				{
					Name:      "healthy",
					Permanent: true,
					ServiceQualityAction: []gameKruiseV1alpha1.ServiceQualityAction{
						{
							State: true,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								UpdatePriority: &up,
							},
						},
						{
							State: false,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								DeletionPriority: &dp,
							},
						},
					},
				},
			},
			podConditions: []corev1.PodCondition{
				{
					Type:          "otherA",
					Status:        corev1.ConditionFalse,
					LastProbeTime: fakeProbeTime,
				},
			},
			sqConditions: nil,
			spec:         gameKruiseV1alpha1.GameServerSpec{},
			newSqConditions: []gameKruiseV1alpha1.ServiceQualityCondition{
				{
					Name: "healthy",
				},
			},
		},
		{
			serviceQualities: []gameKruiseV1alpha1.ServiceQuality{
				{
					Name:      "healthy",
					Permanent: true,
					ServiceQualityAction: []gameKruiseV1alpha1.ServiceQualityAction{
						{
							State: true,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								UpdatePriority: &up,
							},
						},
					},
				},
			},
			podConditions: []corev1.PodCondition{
				{
					Type:          "game.kruise.io/healthy",
					Status:        corev1.ConditionFalse,
					LastProbeTime: fakeProbeTime,
				},
				{
					Type:          "otherA",
					Status:        corev1.ConditionFalse,
					LastProbeTime: fakeProbeTime,
				},
			},
			sqConditions: nil,
			spec:         gameKruiseV1alpha1.GameServerSpec{},
			newSqConditions: []gameKruiseV1alpha1.ServiceQualityCondition{
				{
					Name:          "healthy",
					Status:        string(corev1.ConditionFalse),
					LastProbeTime: fakeProbeTime,
				},
			},
		},
		{
			serviceQualities: []gameKruiseV1alpha1.ServiceQuality{
				{
					Name:      "healthy",
					Permanent: false,
					ServiceQualityAction: []gameKruiseV1alpha1.ServiceQualityAction{
						{
							State: true,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								UpdatePriority: &up,
							},
						},
						{
							State: false,
							GameServerSpec: gameKruiseV1alpha1.GameServerSpec{
								DeletionPriority: &dp,
							},
						},
					},
				},
			},
			podConditions: []corev1.PodCondition{
				{
					Type:          "game.kruise.io/healthy",
					Status:        corev1.ConditionTrue,
					LastProbeTime: fakeProbeTime,
				},
				{
					Type:          "otherA",
					Status:        corev1.ConditionFalse,
					LastProbeTime: fakeProbeTime,
				},
			},
			sqConditions: []gameKruiseV1alpha1.ServiceQualityCondition{
				{
					Name:                     "healthy",
					Status:                   string(corev1.ConditionFalse),
					LastProbeTime:            fakeProbeTime,
					LastActionTransitionTime: fakeActionTime,
				},
			},
			spec: gameKruiseV1alpha1.GameServerSpec{
				UpdatePriority: &up,
			},
			newSqConditions: []gameKruiseV1alpha1.ServiceQualityCondition{
				{
					Name:                     "healthy",
					Status:                   string(corev1.ConditionTrue),
					LastProbeTime:            fakeProbeTime,
					LastActionTransitionTime: fakeActionTime,
				},
			},
		},
	}

	for _, test := range tests {
		actualSpec, actualNewSqConditions := syncServiceQualities(test.serviceQualities, test.podConditions, test.sqConditions)
		expectSpec := test.spec
		expectNewSqConditions := test.newSqConditions
		if !reflect.DeepEqual(actualSpec, expectSpec) {
			t.Errorf("expect spec %v but got %v", expectSpec, actualSpec)
		}
		if len(actualNewSqConditions) != len(expectNewSqConditions) {
			t.Errorf("expect sq conditions len %v but got %v", len(expectNewSqConditions), len(actualNewSqConditions))
		}
		for _, expectNewSqCondition := range expectNewSqConditions {
			exist := false
			for _, actualNewSqCondition := range actualNewSqConditions {
				if actualNewSqCondition.Name == expectNewSqCondition.Name {
					exist = true
					if actualNewSqCondition.Status != expectNewSqCondition.Status {
						t.Errorf("expect sq condition status %v but got %v", expectNewSqCondition.Status, actualNewSqCondition.Status)
					}
					if actualNewSqCondition.LastProbeTime != expectNewSqCondition.LastProbeTime {
						t.Errorf("expect sq condition LastProbeTime %v but got %v", expectNewSqCondition.LastProbeTime, actualNewSqCondition.LastProbeTime)
					}
					if actualNewSqCondition.LastActionTransitionTime.IsZero() != expectNewSqCondition.LastActionTransitionTime.IsZero() {
						t.Errorf("expect sq condition LastActionTransitionTime IsZero %v but got %v", expectNewSqCondition.LastActionTransitionTime.IsZero(), actualNewSqCondition.LastActionTransitionTime.IsZero())
					}
					break
				}
			}
			if !exist {
				t.Errorf("expect sq condition %s exist, but actually not", expectNewSqCondition.Name)
			}
		}
	}
}

func TestSyncGsToPod(t *testing.T) {
	up := intstr.FromInt(20)
	dp := intstr.FromInt(10)
	tests := []struct {
		gs       *gameKruiseV1alpha1.GameServer
		pod      *corev1.Pod
		isUpdate bool
	}{
		{
			gs: &gameKruiseV1alpha1.GameServer{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "xxx-0",
					Labels: map[string]string{
						gameKruiseV1alpha1.GameServerOwnerGssKey: "xxx",
					},
				},
				Spec: gameKruiseV1alpha1.GameServerSpec{
					UpdatePriority:   &up,
					DeletionPriority: &dp,
					OpsState:         gameKruiseV1alpha1.WaitToDelete,
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "xxx-0",
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
				},
			},
			isUpdate: true,
		},

		{
			gs: &gameKruiseV1alpha1.GameServer{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "xxx-0",
					Labels: map[string]string{
						gameKruiseV1alpha1.GameServerOwnerGssKey: "xxx",
					},
				},
				Spec: gameKruiseV1alpha1.GameServerSpec{
					UpdatePriority:   &up,
					DeletionPriority: &dp,
					OpsState:         gameKruiseV1alpha1.WaitToDelete,
				},
				Status: gameKruiseV1alpha1.GameServerStatus{
					CurrentState: gameKruiseV1alpha1.Creating,
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "xxx-0",
					Labels: map[string]string{
						gameKruiseV1alpha1.GameServerOpsStateKey:       string(gameKruiseV1alpha1.WaitToDelete),
						gameKruiseV1alpha1.GameServerDeletePriorityKey: dp.String(),
						gameKruiseV1alpha1.GameServerUpdatePriorityKey: up.String(),
						gameKruiseV1alpha1.GameServerStateKey:          string(gameKruiseV1alpha1.Creating),
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
				},
			},
			isUpdate: false,
		},
	}

	for _, test := range tests {
		objs := []client.Object{test.gs, test.pod}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		manager := &GameServerManager{
			client:     c,
			gameServer: test.gs,
			pod:        test.pod,
		}

		isUpdate, err := manager.SyncGsToPod()
		if err != nil {
			t.Error(err)
		}

		if isUpdate != test.isUpdate {
			t.Errorf("expect isUpdate is %v ,but actually is %v", test.isUpdate, isUpdate)
		}
	}
}

func TestSyncNetworkStatus(t *testing.T) {
	fakeTime := metav1.Now()
	portInternal := intstr.FromInt(80)
	portExternal := intstr.FromInt(601)
	tests := []struct {
		gs              *gameKruiseV1alpha1.GameServer
		pod             *corev1.Pod
		gsNetworkStatus gameKruiseV1alpha1.NetworkStatus
	}{
		{
			gs: &gameKruiseV1alpha1.GameServer{
				Status: gameKruiseV1alpha1.GameServerStatus{
					NetworkStatus: gameKruiseV1alpha1.NetworkStatus{},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						gameKruiseV1alpha1.GameServerNetworkType:     "xxx-type",
						gameKruiseV1alpha1.GameServerNetworkConf:     "[{\"name\":\"SlbIds\",\"value\":\"lb-2zev1w12n684h7ymjtpuo\"},{\"name\":\"PortProtocols\",\"value\":\"80\"},{\"name\":\"Fixed\",\"value\":\"true\"}]",
						gameKruiseV1alpha1.GameServerNetworkDisabled: "false",
						gameKruiseV1alpha1.GameServerNetworkStatus:   "{\"internalAddresses\":[{\"ip\":\"172.16.1.132\",\"ports\":[{\"name\":\"80\",\"protocol\":\"TCP\",\"port\":80}],\"portRange\":{}}],\"externalAddresses\":[{\"ip\":\"47.99.47.99\",\"ports\":[{\"name\":\"80\",\"protocol\":\"TCP\",\"port\":601}],\"portRange\":{}}],\"currentNetworkState\":\"Ready\",\"createTime\":null,\"lastTransitionTime\":null}",
					},
				},
			},
			gsNetworkStatus: gameKruiseV1alpha1.NetworkStatus{
				NetworkType:         "xxx-type",
				DesiredNetworkState: gameKruiseV1alpha1.NetworkReady,
				CreateTime:          fakeTime,
				LastTransitionTime:  fakeTime,
			},
		},

		{
			gs: &gameKruiseV1alpha1.GameServer{
				Status: gameKruiseV1alpha1.GameServerStatus{
					NetworkStatus: gameKruiseV1alpha1.NetworkStatus{
						NetworkType:         "xxx-type",
						DesiredNetworkState: gameKruiseV1alpha1.NetworkReady,
						CreateTime:          fakeTime,
						LastTransitionTime:  fakeTime,
					},
				},
			},
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						gameKruiseV1alpha1.GameServerNetworkType:     "xxx-type",
						gameKruiseV1alpha1.GameServerNetworkConf:     "[{\"name\":\"SlbIds\",\"value\":\"lb-2zev1w12n684h7ymjtpuo\"},{\"name\":\"PortProtocols\",\"value\":\"80\"},{\"name\":\"Fixed\",\"value\":\"true\"}]",
						gameKruiseV1alpha1.GameServerNetworkDisabled: "false",
						gameKruiseV1alpha1.GameServerNetworkStatus:   "{\"internalAddresses\":[{\"ip\":\"172.16.1.132\",\"ports\":[{\"name\":\"80\",\"protocol\":\"TCP\",\"port\":80}],\"portRange\":{}}],\"externalAddresses\":[{\"ip\":\"47.99.47.99\",\"ports\":[{\"name\":\"80\",\"protocol\":\"TCP\",\"port\":601}],\"portRange\":{}}],\"currentNetworkState\":\"Ready\",\"createTime\":null,\"lastTransitionTime\":null}",
					},
				},
			},
			gsNetworkStatus: gameKruiseV1alpha1.NetworkStatus{
				NetworkType:         "xxx-type",
				CurrentNetworkState: gameKruiseV1alpha1.NetworkReady,
				DesiredNetworkState: gameKruiseV1alpha1.NetworkReady,
				InternalAddresses: []gameKruiseV1alpha1.NetworkAddress{
					{
						IP: "172.16.1.132",
						Ports: []gameKruiseV1alpha1.NetworkPort{
							{
								Name:     "80",
								Protocol: "TCP",
								Port:     &portInternal,
							},
						},
					},
				},
				ExternalAddresses: []gameKruiseV1alpha1.NetworkAddress{
					{
						IP: "47.99.47.99",
						Ports: []gameKruiseV1alpha1.NetworkPort{
							{
								Name:     "80",
								Protocol: "TCP",
								Port:     &portExternal,
							},
						},
					},
				},
				CreateTime:         fakeTime,
				LastTransitionTime: fakeTime,
			},
		},
	}

	for _, test := range tests {
		objs := []client.Object{test.gs, test.pod}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		manager := &GameServerManager{
			client:     c,
			gameServer: test.gs,
			pod:        test.pod,
		}

		actual := manager.syncNetworkStatus()
		actual.CreateTime = fakeTime
		actual.LastTransitionTime = fakeTime
		if !reflect.DeepEqual(test.gsNetworkStatus, actual) {
			t.Errorf("expect gsNetworkStatus is %v ,but actually is %v", test.gsNetworkStatus, actual)
		}
	}
}
