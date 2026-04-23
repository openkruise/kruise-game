package framework

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// WaitForPodRunning waits until the pod is in Running phase and ready
func (f *Framework) WaitForPodRunning(podName string) error {
	return wait.PollUntilContextTimeout(context.TODO(), 2*time.Second, 3*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			pod, err := f.client.GetPod(podName)
			if err != nil {
				return false, err
			}
			if pod.Status.Phase != corev1.PodRunning {
				return false, nil
			}
			for _, cond := range pod.Status.Conditions {
				if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
					return true, nil
				}
			}
			return false, nil
		})
}
