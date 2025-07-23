package webhook

import (
	"context"
	"reflect"
	"testing"

	gameKruiseV1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(gameKruiseV1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
}

func TestPatchContainers(t *testing.T) {
	tests := []struct {
		gs            *gameKruiseV1alpha1.GameServer
		oldPod        *corev1.Pod
		newContainers []corev1.Container
	}{
		// case 0
		{
			gs: nil,
			oldPod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "A",
							Image: "A-v1",
						},
					},
				},
			},
			newContainers: []corev1.Container{
				{
					Name:  "A",
					Image: "A-v1",
				},
			},
		},

		// case 1
		{
			gs: &gameKruiseV1alpha1.GameServer{
				Spec: gameKruiseV1alpha1.GameServerSpec{
					Containers: []gameKruiseV1alpha1.GameServerContainer{
						{
							Name:  "A",
							Image: "A-v2",
							Resources: corev1.ResourceRequirements{
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
								Limits: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceCPU: resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			oldPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						gameKruiseV1alpha1.GameServerOwnerGssKey: "xxx",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "A",
							Image: "A-v1",
							Resources: corev1.ResourceRequirements{
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
						{
							Name:  "B",
							Image: "B-v1",
						},
					},
				},
			},

			newContainers: []corev1.Container{
				{
					Name:  "A",
					Image: "A-v2",
					Resources: corev1.ResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
						Limits: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceCPU: resource.MustParse("1"),
						},
					},
				},
				{
					Name:  "B",
					Image: "B-v1",
				},
			},
		},

		// case 2
		{
			gs: &gameKruiseV1alpha1.GameServer{
				Spec: gameKruiseV1alpha1.GameServerSpec{
					Containers: []gameKruiseV1alpha1.GameServerContainer{
						{
							Name: "A",
							Resources: corev1.ResourceRequirements{
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceCPU: resource.MustParse("1"),
								},
							},
						},
					},
				},
			},
			oldPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						gameKruiseV1alpha1.GameServerOwnerGssKey: "xxx",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "A",
							Image: "A-v1",
						},
					},
				},
			},

			newContainers: []corev1.Container{
				{
					Name:  "A",
					Image: "A-v1",
					Resources: corev1.ResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceCPU: resource.MustParse("1"),
						},
					},
				},
			},
		},
	}

	for i, test := range tests {
		expect := test.newContainers
		var objs []client.Object
		if test.gs != nil {
			objs = append(objs, test.gs)
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
		newPod, err := patchContainers(c, test.oldPod, context.Background())
		if err != nil {
			t.Error(err)
		}
		actual := newPod.Spec.Containers
		if !reflect.DeepEqual(expect, actual) {
			t.Errorf("case %d: expect new containers %v, but actually got %v", i, expect, actual)
		}
	}
}

func TestGetPodFromRequest(t *testing.T) {
	tests := []struct {
		req admission.Request
		pod *corev1.Pod
	}{
		// case 0
		{
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Delete,
					Object: runtime.RawExtension{
						Raw: []byte(`{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "name": "foo",
        "namespace": "default"
    },
    "spec": {
        "containers": [
            {
                "image": "bar:v2",
                "name": "bar"
            }
        ]
    }
}`),
					},
					OldObject: runtime.RawExtension{
						Raw: []byte(`{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "name": "foo",
        "namespace": "default"
    },
    "spec": {
        "containers": [
            {
                "image": "bar:v1",
                "name": "bar"
            }
        ]
    }
}`),
					},
				},
			},
			pod: &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Image: "bar:v1", Name: "bar"},
					},
				},
			},
		},

		// case 1
		{
			req: admission.Request{
				AdmissionRequest: admissionv1.AdmissionRequest{
					Operation: admissionv1.Update,
					Object: runtime.RawExtension{
						Raw: []byte(`{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "name": "foo",
        "namespace": "default"
    },
    "spec": {
        "containers": [
            {
                "image": "bar:v2",
                "name": "bar"
            }
        ]
    }
}`),
					},
					OldObject: runtime.RawExtension{
						Raw: []byte(`{
    "apiVersion": "v1",
    "kind": "Pod",
    "metadata": {
        "name": "foo",
        "namespace": "default"
    },
    "spec": {
        "containers": [
            {
                "image": "bar:v1",
                "name": "bar"
            }
        ]
    }
}`),
					},
				},
			},
			pod: &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Image: "bar:v2", Name: "bar"},
					},
				},
			},
		},
	}

	decoder := admission.NewDecoder(runtime.NewScheme())

	for i, test := range tests {
		actual, err := getPodFromRequest(test.req, decoder)
		if err != nil {
			t.Error(err)
		}
		expect := test.pod
		if !reflect.DeepEqual(actual, expect) {
			t.Errorf("case %d: expect pod %v, but actually got %v", i, expect, actual)
		}
	}
}
