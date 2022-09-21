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

package gameserverset

import (
	"context"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/util"
	utildiscovery "github.com/openkruise/kruise-game/pkg/util/discovery"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
)

var (
	controllerKind = gamekruiseiov1alpha1.SchemeGroupVersion.WithKind("GameServerSet")
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
	recorder := mgr.GetEventRecorderFor("gameserverset-controller")
	return &GameServerSetReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		recorder: recorder,
	}
}

func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	klog.Info("Starting GameServerSet Controller")
	c, err := controller.New("gameserverset-controller", mgr, controller.Options{Reconciler: r, MaxConcurrentReconciles: concurrentReconciles})
	if err != nil {
		klog.Error(err)
		return err
	}

	if err = c.Watch(&source.Kind{Type: &gamekruiseiov1alpha1.GameServerSet{}}, &handler.EnqueueRequestForObject{}); err != nil {
		klog.Error(err)
		return err
	}

	if err = watchPod(c); err != nil {
		klog.Error(err)
		return err
	}

	if err = watchWorkloads(c); err != nil {
		klog.Error(err)
		return err
	}

	return nil
}

// watch pod
func watchPod(c controller.Controller) (err error) {

	if err := c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.Funcs{
		CreateFunc: func(createEvent event.CreateEvent, limitingInterface workqueue.RateLimitingInterface) {
			pod := createEvent.Object.(*corev1.Pod)
			if gssName, exist := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]; exist {
				limitingInterface.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      gssName,
					Namespace: pod.GetNamespace(),
				}})
			}
		},
		UpdateFunc: func(updateEvent event.UpdateEvent, limitingInterface workqueue.RateLimitingInterface) {
			pod := updateEvent.ObjectNew.(*corev1.Pod)
			if gssName, exist := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]; exist {
				limitingInterface.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      gssName,
					Namespace: pod.GetNamespace(),
				}})
			}
		},
		DeleteFunc: func(deleteEvent event.DeleteEvent, limitingInterface workqueue.RateLimitingInterface) {
			pod := deleteEvent.Object.(*corev1.Pod)
			if gssName, exist := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]; exist {
				limitingInterface.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      gssName,
					Namespace: pod.GetNamespace(),
				}})
			}
		},
	}); err != nil {
		return err
	}
	return nil
}

// watch workloads
func watchWorkloads(c controller.Controller) (err error) {
	if err := c.Watch(&source.Kind{Type: &kruiseV1beta1.StatefulSet{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &gamekruiseiov1alpha1.GameServerSet{},
	}); err != nil {
		return err
	}
	return nil
}

// GameServerSetReconciler reconciles a GameServerSet object
type GameServerSetReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=game.kruise.io,resources=gameserversets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=game.kruise.io,resources=gameserversets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=game.kruise.io,resources=gameserversets/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the GameServerSet object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *GameServerSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	namespacedName := req.NamespacedName

	// get GameServerSet
	gss := &gamekruiseiov1alpha1.GameServerSet{}
	err := r.Get(ctx, namespacedName, gss)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		klog.Errorf("Failed to find GameServerSet %s in %s,because of %s.", namespacedName.Name, namespacedName.Namespace, err.Error())
		return reconcile.Result{}, err
	}

	// get advanced statefulset
	asts := &kruiseV1beta1.StatefulSet{}
	err = r.Get(ctx, namespacedName, asts)
	if err != nil {
		if errors.IsNotFound(err) {
			err = r.initAsts(gss)
			if err != nil {
				klog.Errorf("failed to create advanced statefulset %s in %s,because of %s.", namespacedName.Name, namespacedName.Namespace, err.Error())
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}
		klog.Errorf("failed to find advanced statefulset %s in %s,because of %s.", namespacedName.Name, namespacedName.Namespace, err.Error())
		return reconcile.Result{}, err
	}

	// get actual Pod list
	podList := &corev1.PodList{}
	err = r.List(ctx, podList, &client.ListOptions{
		Namespace: gss.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(map[string]string{
			gamekruiseiov1alpha1.GameServerOwnerGssKey: gss.GetName(),
		})})
	if err != nil {
		klog.Errorf("failed to list GameServers of GameServerSet %s in %s.", gss.GetName(), gss.GetNamespace())
		return reconcile.Result{}, err
	}

	gsm := NewGameServerSetManager(gss, asts, podList.Items, r.Client)

	// scale game servers
	if gsm.IsNeedToScale() {
		err = gsm.GameServerScale()
		if err != nil {
			klog.Errorf("GameServerSet %s failed to scale GameServers in %s,because of %s.", namespacedName.Name, namespacedName.Namespace, err.Error())
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	// update workload
	if gsm.IsNeedToUpdateWorkload() {
		err = gsm.UpdateWorkload()
		if err != nil {
			klog.Errorf("GameServerSet %s failed to synchronize workload in %s,because of %s.", namespacedName.Name, namespacedName.Namespace, err.Error())
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	// TODO sync PodProbeMarker
	err = gsm.SyncPodProbeMarker()
	if err != nil {
		klog.Errorf("GameServerSet %s failed to synchronize PodProbeMarker in %s,because of %s.", namespacedName.Name, namespacedName.Namespace, err.Error())
		return reconcile.Result{}, err
	}

	// sync GameServerSet Status
	err = gsm.SyncStatus()
	if err != nil {
		klog.Errorf("GameServerSet %s failed to synchronize its status in %s,because of %s.", namespacedName.Name, namespacedName.Namespace, err.Error())
		return reconcile.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GameServerSetReconciler) SetupWithManager(mgr ctrl.Manager) (c controller.Controller, err error) {
	c, err = ctrl.NewControllerManagedBy(mgr).
		For(&gamekruiseiov1alpha1.GameServerSet{}).Build(r)
	return c, err
}

func (r *GameServerSetReconciler) initAsts(gss *gamekruiseiov1alpha1.GameServerSet) error {
	asts := &kruiseV1beta1.StatefulSet{}
	asts.Namespace = gss.GetNamespace()
	asts.Name = gss.GetName()

	// set owner reference
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
	asts.SetOwnerReferences(ors)

	// set label
	astsLabels := make(map[string]string)
	astsLabels[gamekruiseiov1alpha1.AstsHashKey] = util.GetAstsHash(gss)
	asts.SetLabels(astsLabels)

	// set label selector
	asts.Spec.Selector = &metav1.LabelSelector{
		MatchLabels: map[string]string{gamekruiseiov1alpha1.GameServerOwnerGssKey: gss.GetName()},
	}

	// set replicas
	asts.Spec.Replicas = gss.Spec.Replicas

	asts = util.GetNewAstsFromGss(gss, asts)

	return r.Client.Create(context.Background(), asts)
}
