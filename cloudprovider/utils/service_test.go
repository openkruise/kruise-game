package utils

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kruisePub "github.com/openkruise/kruise-api/apps/pub"
	kruiseV1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(gamekruiseiov1alpha1.AddToScheme(scheme))
	utilruntime.Must(kruiseV1beta1.AddToScheme(scheme))
	utilruntime.Must(kruiseV1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
}

func TestAllowNotReadyContainers(t *testing.T) {
	tests := []struct {
		// input
		pod         *corev1.Pod
		svc         *corev1.Service
		gss         *gamekruiseiov1alpha1.GameServerSet
		isSvcShared bool
		podElse     []*corev1.Pod
		// output
		inplaceUpdateNotReadyBlocker string
		isSvcUpdated                 bool
	}{
		// When svc is not shared, pod updated, svc should not publish NotReadyAddresses
		{
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case0-0",
					UID:       "xxx0",
					Labels: map[string]string{
						kruisePub.LifecycleStateKey:                string(kruisePub.LifecycleStateUpdating),
						gamekruiseiov1alpha1.GameServerOwnerGssKey: "case0",
					},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  "name_A",
							Image: "v1.0",
						},
						{
							Name:  "name_B",
							Image: "v1.0",
						},
					},
				},
			},
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					PublishNotReadyAddresses: true,
				},
			},
			gss: &gamekruiseiov1alpha1.GameServerSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "GameServerSet",
					APIVersion: "game.kruise.io/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case0",
					UID:       "xxx0",
				},
				Spec: gamekruiseiov1alpha1.GameServerSetSpec{
					Network: &gamekruiseiov1alpha1.Network{
						NetworkConf: []gamekruiseiov1alpha1.NetworkConfParams{
							{
								Name:  gamekruiseiov1alpha1.AllowNotReadyContainersNetworkConfName,
								Value: "name_B",
							},
						},
					},
					GameServerTemplate: gamekruiseiov1alpha1.GameServerTemplate{
						PodTemplateSpec: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "name_A",
										Image: "v1.0",
									},
									{
										Name:  "name_B",
										Image: "v1.0",
									},
								},
							},
						},
					},
				},
			},
			isSvcShared:                  false,
			inplaceUpdateNotReadyBlocker: "true",
			isSvcUpdated:                 true,
		},
		// When svc is not shared & pod is pre-updating & svc PublishNotReadyAddresses is false, svc should publish NotReadyAddresses
		{
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case1-0",
					UID:       "xxx0",
					Labels: map[string]string{
						kruisePub.LifecycleStateKey:                       string(kruisePub.LifecycleStatePreparingUpdate),
						gamekruiseiov1alpha1.InplaceUpdateNotReadyBlocker: "true",
						gamekruiseiov1alpha1.GameServerOwnerGssKey:        "case1",
					},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  "name_A",
							Image: "v1.0",
						},
						{
							Name:  "name_B",
							Image: "v1.0",
						},
					},
				},
			},
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					PublishNotReadyAddresses: false,
				},
			},
			gss: &gamekruiseiov1alpha1.GameServerSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "GameServerSet",
					APIVersion: "game.kruise.io/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case1",
					UID:       "xxx0",
				},
				Spec: gamekruiseiov1alpha1.GameServerSetSpec{
					Network: &gamekruiseiov1alpha1.Network{
						NetworkConf: []gamekruiseiov1alpha1.NetworkConfParams{
							{
								Name:  gamekruiseiov1alpha1.AllowNotReadyContainersNetworkConfName,
								Value: "name_B",
							},
						},
					},
					GameServerTemplate: gamekruiseiov1alpha1.GameServerTemplate{
						PodTemplateSpec: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "name_A",
										Image: "v1.0",
									},
									{
										Name:  "name_B",
										Image: "v2.0",
									},
								},
							},
						},
					},
				},
			},
			isSvcShared:                  false,
			inplaceUpdateNotReadyBlocker: "true",
			isSvcUpdated:                 true,
		},
		// When svc is not shared & pod is pre-updating & svc PublishNotReadyAddresses is true, finalizer of pod should be removed to enter next stage
		{
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case2-0",
					UID:       "xxx0",
					Labels: map[string]string{
						kruisePub.LifecycleStateKey:                string(kruisePub.LifecycleStatePreparingUpdate),
						gamekruiseiov1alpha1.GameServerOwnerGssKey: "case2",
					},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  "name_A",
							Image: "v1.0",
						},
						{
							Name:  "name_B",
							Image: "v1.0",
						},
					},
				},
			},
			svc: &corev1.Service{
				Spec: corev1.ServiceSpec{
					PublishNotReadyAddresses: true,
				},
			},
			gss: &gamekruiseiov1alpha1.GameServerSet{
				TypeMeta: metav1.TypeMeta{
					Kind:       "GameServerSet",
					APIVersion: "game.kruise.io/v1alpha1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "xxx",
					Name:      "case2",
					UID:       "xxx0",
				},
				Spec: gamekruiseiov1alpha1.GameServerSetSpec{
					Network: &gamekruiseiov1alpha1.Network{
						NetworkConf: []gamekruiseiov1alpha1.NetworkConfParams{
							{
								Name:  gamekruiseiov1alpha1.AllowNotReadyContainersNetworkConfName,
								Value: "name_B",
							},
						},
					},
					GameServerTemplate: gamekruiseiov1alpha1.GameServerTemplate{
						PodTemplateSpec: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:  "name_A",
										Image: "v1.0",
									},
									{
										Name:  "name_B",
										Image: "v1.0",
									},
								},
							},
						},
					},
				},
			},
			isSvcShared:                  false,
			inplaceUpdateNotReadyBlocker: "false",
			isSvcUpdated:                 false,
		},
	}

	for i, test := range tests {
		objs := []client.Object{test.gss, test.pod, test.svc}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		actual, err := AllowNotReadyContainers(c, context.TODO(), test.pod, test.svc, test.isSvcShared)
		if err != nil {
			t.Errorf("case %d: %s", i, err.Error())
		}
		if actual != test.isSvcUpdated {
			t.Errorf("case %d: expect isSvcUpdated is %v but actually got %v", i, test.isSvcUpdated, actual)
		}
		if test.pod.GetLabels()[gamekruiseiov1alpha1.InplaceUpdateNotReadyBlocker] != test.inplaceUpdateNotReadyBlocker {
			t.Errorf("case %d: expect inplaceUpdateNotReadyBlocker is %v but actually got %v", i, test.inplaceUpdateNotReadyBlocker, test.pod.GetLabels()[gamekruiseiov1alpha1.InplaceUpdateNotReadyBlocker])
		}
	}
}
