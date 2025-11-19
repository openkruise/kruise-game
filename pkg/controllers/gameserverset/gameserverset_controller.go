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
	"flag"

	kruisev1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/logging"
	"github.com/openkruise/kruise-game/pkg/tracing"
	"github.com/openkruise/kruise-game/pkg/util"
	utildiscovery "github.com/openkruise/kruise-game/pkg/util/discovery"
)

func init() {
	flag.IntVar(&concurrentReconciles, "gameserverset-workers", concurrentReconciles, "Max concurrent workers for GameServerSet controller.")
}

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
	klog.InfoS("Starting controller", "event", "controller.start", "controller", "gameserverset", "workers", concurrentReconciles)
	c, err := controller.New("gameserverset-controller", mgr, controller.Options{Reconciler: r, MaxConcurrentReconciles: concurrentReconciles})
	if err != nil {
		klog.Error(err)
		return err
	}

	if err = c.Watch(source.Kind(mgr.GetCache(), &gamekruiseiov1alpha1.GameServerSet{}, &handler.TypedEnqueueRequestForObject[*gamekruiseiov1alpha1.GameServerSet]{})); err != nil {
		klog.Error(err)
		return err
	}

	if err = c.Watch(source.Kind(mgr.GetCache(), &kruisev1alpha1.PodProbeMarker{}, &handler.TypedEnqueueRequestForObject[*kruisev1alpha1.PodProbeMarker]{}, predicate.TypedFuncs[*kruisev1alpha1.PodProbeMarker]{
		UpdateFunc: func(e event.TypedUpdateEvent[*kruisev1alpha1.PodProbeMarker]) bool {
			oldScS := e.ObjectOld
			newScS := e.ObjectNew
			return oldScS.Status.ObservedGeneration != newScS.Status.ObservedGeneration
		},
	})); err != nil {
		klog.Error(err)
		return err
	}

	if err = watchPod(mgr, c); err != nil {
		klog.Error(err)
		return err
	}

	if err = watchWorkloads(mgr, c); err != nil {
		klog.Error(err)
		return err
	}

	return nil
}

// watch pod
func watchPod(mgr manager.Manager, c controller.Controller) (err error) {
	if err := c.Watch(source.Kind(mgr.GetCache(), &corev1.Pod{}, &handler.TypedFuncs[*corev1.Pod]{
		CreateFunc: func(ctx context.Context, createEvent event.TypedCreateEvent[*corev1.Pod], limitingInterface workqueue.RateLimitingInterface) {
			pod := createEvent.Object
			if gssName, exist := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]; exist {
				limitingInterface.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      gssName,
					Namespace: pod.GetNamespace(),
				}})
			}
		},
		UpdateFunc: func(ctx context.Context, updateEvent event.TypedUpdateEvent[*corev1.Pod], limitingInterface workqueue.RateLimitingInterface) {
			pod := updateEvent.ObjectNew
			if gssName, exist := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]; exist {
				limitingInterface.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      gssName,
					Namespace: pod.GetNamespace(),
				}})
			}
		},
		DeleteFunc: func(ctx context.Context, deleteEvent event.TypedDeleteEvent[*corev1.Pod], limitingInterface workqueue.RateLimitingInterface) {
			pod := deleteEvent.Object
			if gssName, exist := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]; exist {
				limitingInterface.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      gssName,
					Namespace: pod.GetNamespace(),
				}})
			}
		},
	})); err != nil {
		return err
	}
	return nil
}

// watch workloads
func watchWorkloads(mgr manager.Manager, c controller.Controller) (err error) {
	if err := c.Watch(source.Kind(mgr.GetCache(), &kruiseV1beta1.StatefulSet{}, handler.TypedEnqueueRequestForOwner[*kruiseV1beta1.StatefulSet](
		mgr.GetScheme(),
		mgr.GetRESTMapper(),
		&gamekruiseiov1alpha1.GameServerSet{},
		handler.OnlyControllerOwner(),
	))); err != nil {
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
	namespacedName := req.NamespacedName

	// Create root span for GameServerSet Reconcile (SERVER span kind)
	tracer := otel.Tracer("okg-controller-manager")
	ctx, span := tracer.Start(ctx, "reconcile game_server_set",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("k8s.namespace.name", namespacedName.Namespace),
			tracing.AttrGameServerSetNamespace(namespacedName.Namespace),
			tracing.AttrGameServerSetName(namespacedName.Name),
		))
	defer span.End()

	logger := logging.FromContextWithTrace(ctx).WithValues(
		tracing.FieldGameServerSetNamespace, namespacedName.Namespace,
		tracing.FieldGameServerSetName, namespacedName.Name,
	)

	// get GameServerSet
	gss := &gamekruiseiov1alpha1.GameServerSet{}
	err := r.Get(ctx, namespacedName, gss)
	if err != nil {
		if errors.IsNotFound(err) {
			span.SetAttributes(attribute.String("reconcile.trigger", "delete"))
			span.SetStatus(codes.Ok, "GameServerSet not found (deleted)")
			return reconcile.Result{}, nil
		}
		logger.Error(err, "failed to get GameServerSet")
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get GameServerSet")
		return reconcile.Result{}, err
	}

	span.SetAttributes(tracing.AttrGameServerSetName(gss.GetName()))
	span.SetAttributes(attribute.String("reconcile.trigger", "update"))

	gsm := NewGameServerSetManager(gss, r.Client, r.recorder, logger)
	// The serverless scenario PodProbeMarker takes effect during the Webhook phase, so need to create the PodProbeMarker in advance.
	span.AddEvent("gameserverset.reconcile.sync_podprobemarker.start")
	err, done := gsm.SyncPodProbeMarker(ctx)
	if err != nil {
		logger.Error(err, "failed to sync PodProbeMarker")
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to sync PodProbeMarker")
		return reconcile.Result{}, err
	} else if !done {
		span.AddEvent("gameserverset.reconcile.sync_podprobemarker.waiting")
		span.SetAttributes(attribute.Bool("podprobemarker.synced", false))
		return reconcile.Result{}, nil
	}
	span.AddEvent("gameserverset.reconcile.sync_podprobemarker.success")

	// get advanced statefulset
	asts := &kruiseV1beta1.StatefulSet{}
	err = r.Get(ctx, namespacedName, asts)
	if err != nil {
		if errors.IsNotFound(err) {
			span.SetAttributes(attribute.String("reconcile.action", "create_statefulset"))
			err = r.initAsts(ctx, gss)
			if err != nil {
				logger.Error(err, "failed to create Advanced StatefulSet")
				span.RecordError(err)
				span.SetStatus(codes.Error, "failed to create StatefulSet")
				return reconcile.Result{}, err
			}
			r.recorder.Event(gss, corev1.EventTypeNormal, CreateWorkloadReason, "created Advanced StatefulSet")
			span.SetStatus(codes.Ok, "StatefulSet created successfully")
			return reconcile.Result{}, nil
		}
		logger.Error(err, "failed to get Advanced StatefulSet")
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get StatefulSet")
		return reconcile.Result{}, err
	}

	// get actual Pod list
	podList := &corev1.PodList{}
	err = r.List(ctx, podList, &client.ListOptions{
		Namespace: gss.GetNamespace(),
		LabelSelector: labels.SelectorFromSet(map[string]string{
			gamekruiseiov1alpha1.GameServerOwnerGssKey: gss.GetName(),
		}),
	})
	if err != nil {
		logger.Error(err, "failed to list GameServers",
			tracing.FieldGameServerSetNamespace, gss.GetNamespace(),
			tracing.FieldGameServerSetName, gss.GetName())
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to list Pods")
		return reconcile.Result{}, err
	}

	span.SetAttributes(attribute.Int("pods.count", len(podList.Items)))

	gsm.SyncStsAndPodList(asts, podList.Items)

	// kill game servers
	newReplicas := gsm.GetReplicasAfterKilling()
	if *gss.Spec.Replicas != *newReplicas {
		span.SetAttributes(
			attribute.String("reconcile.action", "kill_gameservers"),
			attribute.Int("replicas.old", int(*gss.Spec.Replicas)),
			attribute.Int("replicas.new", int(*newReplicas)),
		)
		gss.Spec.Replicas = newReplicas
		err = r.Client.Update(ctx, gss)
		if err != nil {
			logger.Error(err, "failed to update replicas after kill")
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to update replicas")
			return reconcile.Result{}, err
		}
		span.SetStatus(codes.Ok, "GameServers killed successfully")
		return reconcile.Result{}, nil
	}

	// scale game servers
	if gsm.IsNeedToScale() {
		span.SetAttributes(attribute.String("reconcile.action", "scale_gameservers"))
		err = gsm.GameServerScale(ctx)
		if err != nil {
			logger.Error(err, "failed to scale GameServers")
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to scale GameServers")
			return reconcile.Result{}, err
		}
		span.SetStatus(codes.Ok, "GameServers scaled successfully")
		return reconcile.Result{}, nil
	}

	// update workload
	if gsm.IsNeedToUpdateWorkload() {
		span.SetAttributes(attribute.String("reconcile.action", "update_workload"))
		err = gsm.UpdateWorkload(ctx)
		if err != nil {
			logger.Error(err, "failed to update workload")
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to update workload")
			return reconcile.Result{}, err
		}
		r.recorder.Event(gss, corev1.EventTypeNormal, UpdateWorkloadReason, "updated Advanced StatefulSet")
		span.SetStatus(codes.Ok, "workload updated successfully")
		return reconcile.Result{}, nil
	}

	// sync GameServerSet Status
	err = gsm.SyncStatus(ctx)
	if err != nil {
		logger.Error(err, "failed to sync status")
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to sync status")
		return reconcile.Result{}, err
	}

	span.SetStatus(codes.Ok, "Reconcile completed successfully")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GameServerSetReconciler) SetupWithManager(mgr ctrl.Manager) (c controller.Controller, err error) {
	c, err = ctrl.NewControllerManagedBy(mgr).
		For(&gamekruiseiov1alpha1.GameServerSet{}).Build(r)
	return c, err
}

func (r *GameServerSetReconciler) initAsts(ctx context.Context, gss *gamekruiseiov1alpha1.GameServerSet) error {
	asts := &kruiseV1beta1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: "apps.kruise.io/v1beta1",
		},
	}
	asts.Namespace = gss.GetNamespace()
	asts.Name = gss.GetName()

	// set owner reference
	ors := make([]metav1.OwnerReference, 0)
	or := metav1.OwnerReference{
		APIVersion:         gss.APIVersion,
		Kind:               gss.Kind,
		Name:               gss.GetName(),
		UID:                gss.GetUID(),
		Controller:         ptr.To[bool](true),
		BlockOwnerDeletion: ptr.To[bool](true),
	}
	ors = append(ors, or)
	asts.SetOwnerReferences(ors)

	// set annotations
	astsAns := make(map[string]string)
	astsAns[gamekruiseiov1alpha1.AstsHashKey] = util.GetAstsHash(gss)
	asts.SetAnnotations(astsAns)

	// set label selector
	asts.Spec.Selector = &metav1.LabelSelector{
		MatchLabels: map[string]string{gamekruiseiov1alpha1.GameServerOwnerGssKey: gss.GetName()},
	}

	// set replicas
	asts.Spec.Replicas = gss.Spec.Replicas
	asts.Spec.ReserveOrdinals = gss.Spec.ReserveGameServerIds

	// set ServiceName
	asts.Spec.ServiceName = gss.Spec.ServiceName
	if asts.Spec.ServiceName == "" {
		// default: GameServerSet name
		asts.Spec.ServiceName = gss.Name
	}

	asts = util.GetNewAstsFromGss(gss.DeepCopy(), asts)

	return r.Client.Create(ctx, asts)
}
