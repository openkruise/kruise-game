/*
Copyright 2022 The Kruise Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gameserver

import (
	"context"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	utildiscovery "github.com/openkruise/kruise-game/pkg/util/discovery"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"time"
)

var (
	controllerKind = gamekruiseiov1alpha1.SchemeGroupVersion.WithKind("GameServer")
	// leave it to batch size
	concurrentReconciles = 10
)

func Add(mgr manager.Manager) error {
	if !utildiscovery.DiscoverGVK(controllerKind) {
		return nil
	}
	return add(mgr, newReconciler(mgr))
}

func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	recorder := mgr.GetEventRecorderFor("gameserver-controller")
	return &GameServerReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		recorder: recorder,
	}
}

func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	klog.Info("Starting GameServer Controller")
	c, err := controller.New("gameserver-controller", mgr, controller.Options{Reconciler: r, MaxConcurrentReconciles: concurrentReconciles})
	if err != nil {
		klog.Error(err)
		return err
	}
	if err = c.Watch(&source.Kind{Type: &gamekruiseiov1alpha1.GameServer{}}, &handler.EnqueueRequestForObject{}); err != nil {
		klog.Error(err)
		return err
	}
	if err = watchPod(c); err != nil {
		klog.Error(err)
		return err
	}

	return nil
}

// GameServerReconciler reconciles a GameServer object
type GameServerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	recorder record.EventRecorder
}

func watchPod(c controller.Controller) error {
	if err := c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.Funcs{
		CreateFunc: func(createEvent event.CreateEvent, limitingInterface workqueue.RateLimitingInterface) {
			pod := createEvent.Object.(*corev1.Pod)
			if _, exist := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]; exist {
				limitingInterface.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      pod.GetName(),
					Namespace: pod.GetNamespace(),
				}})
			}
		},
		UpdateFunc: func(updateEvent event.UpdateEvent, limitingInterface workqueue.RateLimitingInterface) {
			newPod := updateEvent.ObjectNew.(*corev1.Pod)
			if _, exist := newPod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]; exist {
				limitingInterface.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      newPod.GetName(),
					Namespace: newPod.GetNamespace(),
				}})
			}
		},
		DeleteFunc: func(deleteEvent event.DeleteEvent, limitingInterface workqueue.RateLimitingInterface) {
			pod := deleteEvent.Object.(*corev1.Pod)
			if _, exist := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]; exist {
				limitingInterface.Add(reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: deleteEvent.Object.GetNamespace(),
						Name:      deleteEvent.Object.GetName(),
					},
				})
			}
		},
	}); err != nil {
		return err
	}
	return nil
}

//+kubebuilder:rbac:groups=game.kruise.io,resources=gameservers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=game.kruise.io,resources=gameservers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=game.kruise.io,resources=gameservers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the GameServer object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *GameServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	namespacedName := req.NamespacedName

	// get pod
	pod := &corev1.Pod{}
	err := r.Get(ctx, namespacedName, pod)
	podFound := true
	if err != nil {
		if errors.IsNotFound(err) {
			podFound = false
		} else {
			klog.Errorf("failed to find pod %s in %s, because of %s.", namespacedName.Name, namespacedName.Namespace, err.Error())
			return reconcile.Result{}, err
		}
	}

	// get GameServer
	gs := &gamekruiseiov1alpha1.GameServer{}
	err = r.Get(ctx, namespacedName, gs)
	gsFound := true
	if err != nil {
		if errors.IsNotFound(err) {
			gsFound = false
		} else {
			klog.Errorf("failed to find GameServer %s in %s, because of %s.", namespacedName.Name, namespacedName.Namespace, err.Error())
			return reconcile.Result{}, err
		}
	}

	if podFound && !gsFound {
		err := r.initGameServer(pod)
		if err != nil && !errors.IsAlreadyExists(err) && !errors.IsNotFound(err) {
			klog.Errorf("failed to create GameServer %s in %s, because of %s.", namespacedName.Name, namespacedName.Namespace, err.Error())
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	if !podFound {
		if gsFound && gs.GetLabels()[gamekruiseiov1alpha1.GameServerDeletingKey] == "true" {
			err := r.Client.Delete(context.Background(), gs)
			if err != nil && !errors.IsNotFound(err) {
				klog.Errorf("failed to delete GameServer %s in %s, because of %s.", namespacedName.Name, namespacedName.Namespace, err.Error())
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	gsm := NewGameServerManager(gs, pod, r.Client)

	gss, err := r.getGameServerSet(pod)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		klog.Errorf("failed to get GameServerSet for GameServer %s in %s, because of %s.", namespacedName.Name, namespacedName.Namespace, err.Error())
		return reconcile.Result{}, err
	}

	podUpdated, err := gsm.SyncGsToPod()
	if err != nil || podUpdated {
		return reconcile.Result{Requeue: podUpdated, RequeueAfter: 3 * time.Second}, err
	}

	err = gsm.SyncPodToGs(gss)
	if err != nil {
		return reconcile.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GameServerReconciler) SetupWithManager(mgr ctrl.Manager) (c controller.Controller, err error) {
	c, err = ctrl.NewControllerManagedBy(mgr).
		For(&gamekruiseiov1alpha1.GameServer{}).Build(r)
	return c, err
}

func (r *GameServerReconciler) getGameServerSet(pod *corev1.Pod) (*gamekruiseiov1alpha1.GameServerSet, error) {
	gssName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
	gss := &gamekruiseiov1alpha1.GameServerSet{}
	err := r.Client.Get(context.Background(), types.NamespacedName{
		Namespace: pod.GetNamespace(),
		Name:      gssName,
	}, gss)
	return gss, err
}

func (r *GameServerReconciler) initGameServer(pod *corev1.Pod) error {
	gs := &gamekruiseiov1alpha1.GameServer{}
	gs.Name = pod.GetName()
	gs.Namespace = pod.GetNamespace()

	// set owner reference
	gss, err := r.getGameServerSet(pod)
	if err != nil {
		return err
	}
	ors := make([]metav1.OwnerReference, 0)
	or := metav1.OwnerReference{
		APIVersion:         gss.APIVersion,
		Kind:               gss.Kind,
		Name:               gss.GetName(),
		UID:                gss.GetUID(),
		Controller:         pointer.BoolPtr(true),
		BlockOwnerDeletion: pointer.BoolPtr(true),
	}
	ors = append(ors, or)
	gs.OwnerReferences = ors

	// set NetWork
	gs.Spec.NetworkDisabled = false

	// set OpsState
	gs.Spec.OpsState = gamekruiseiov1alpha1.None

	// set UpdatePriority
	updatePriority := intstr.FromInt(0)
	gs.Spec.UpdatePriority = &updatePriority

	// set deletionPriority
	deletionPriority := intstr.FromInt(0)
	gs.Spec.DeletionPriority = &deletionPriority

	return r.Client.Create(context.Background(), gs)
}
