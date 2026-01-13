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
	"flag"
	"reflect"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/pkg/logging"
	"github.com/openkruise/kruise-game/pkg/telemetryfields"
	"github.com/openkruise/kruise-game/pkg/tracing"
	"github.com/openkruise/kruise-game/pkg/util"
	utildiscovery "github.com/openkruise/kruise-game/pkg/util/discovery"
)

func init() {
	flag.IntVar(&concurrentReconciles, "gameserver-workers", concurrentReconciles, "Max concurrent workers for GameServer controller.")
}

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
	klog.InfoS("Starting controller", "event", "controller.start", "controller", "gameserver", "workers", concurrentReconciles)
	c, err := controller.New("gameserver-controller", mgr, controller.Options{Reconciler: r, MaxConcurrentReconciles: concurrentReconciles})
	if err != nil {
		klog.Error(err)
		return err
	}
	if err = c.Watch(source.Kind(mgr.GetCache(),
		&gamekruiseiov1alpha1.GameServer{},
		&handler.TypedEnqueueRequestForObject[*gamekruiseiov1alpha1.GameServer]{})); err != nil {
		klog.Error(err)
		return err
	}
	if err = watchPod(mgr, c); err != nil {
		klog.Error(err)
		return err
	}
	if err = watchNode(mgr, c); err != nil {
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

func watchPod(mgr manager.Manager, c controller.Controller) error {
	if err := c.Watch(source.Kind(mgr.GetCache(), &corev1.Pod{}, &handler.TypedFuncs[*corev1.Pod]{
		CreateFunc: func(ctx context.Context, createEvent event.TypedCreateEvent[*corev1.Pod], limitingInterface workqueue.RateLimitingInterface) {
			pod := createEvent.Object
			if _, exist := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]; exist {
				limitingInterface.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      pod.GetName(),
					Namespace: pod.GetNamespace(),
				}})
			}
		},
		UpdateFunc: func(ctx context.Context, updateEvent event.TypedUpdateEvent[*corev1.Pod], limitingInterface workqueue.RateLimitingInterface) {
			newPod := updateEvent.ObjectNew
			if _, exist := newPod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]; exist {
				limitingInterface.Add(reconcile.Request{NamespacedName: types.NamespacedName{
					Name:      newPod.GetName(),
					Namespace: newPod.GetNamespace(),
				}})
			}
		},
		DeleteFunc: func(ctx context.Context, deleteEvent event.TypedDeleteEvent[*corev1.Pod], limitingInterface workqueue.RateLimitingInterface) {
			pod := deleteEvent.Object
			if _, exist := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]; exist {
				limitingInterface.Add(reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: deleteEvent.Object.GetNamespace(),
						Name:      deleteEvent.Object.GetName(),
					},
				})
			}
		},
	})); err != nil {
		return err
	}
	return nil
}

func watchNode(mgr manager.Manager, c controller.Controller) error {
	cli := mgr.GetClient()

	// watch node condition change
	if err := c.Watch(source.Kind(mgr.GetCache(), &corev1.Node{}, &handler.TypedFuncs[*corev1.Node]{
		UpdateFunc: func(ctx context.Context, updateEvent event.TypedUpdateEvent[*corev1.Node], limitingInterface workqueue.RateLimitingInterface) {
			nodeNew := updateEvent.ObjectNew
			nodeOld := updateEvent.ObjectOld
			if reflect.DeepEqual(nodeNew.Status.Conditions, nodeOld.Status.Conditions) {
				return
			}
			podList := &corev1.PodList{}
			ownerGss, _ := labels.NewRequirement(gamekruiseiov1alpha1.GameServerOwnerGssKey, selection.Exists, []string{})
			err := cli.List(context.Background(), podList, &client.ListOptions{
				LabelSelector: labels.NewSelector().Add(*ownerGss),
				FieldSelector: fields.Set{"spec.nodeName": nodeNew.Name}.AsSelector(),
			})
			if err != nil {
				klog.Errorf("List Pods By NodeName failed: %s", err.Error())
				return
			}
			for _, pod := range podList.Items {
				klog.Infof("Watch Node %s Conditions Changed, adding pods %s/%s in reconcile queue", nodeNew.Name, pod.Namespace, pod.Name)
				limitingInterface.Add(reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: pod.GetNamespace(),
						Name:      pod.GetName(),
					},
				})
			}
		},
	})); err != nil {
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
	namespacedName := req.NamespacedName

	// Create root span for Reconcile (SERVER span kind)
	tracer := otel.Tracer("okg-controller-manager")
	ctx, span := tracer.Start(ctx, tracing.SpanReconcileGameServer,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			tracing.AttrK8sNamespaceName(namespacedName.Namespace),
			tracing.AttrGameServerNamespace(namespacedName.Namespace),
			tracing.AttrGameServerName(namespacedName.Name),
		))
	defer span.End()

	logger := logging.FromContextWithTrace(ctx).WithValues(
		telemetryfields.FieldGameServerNamespace, req.Namespace,
		telemetryfields.FieldGameServerName, req.Name,
	)

	// get pod
	pod := &corev1.Pod{}
	err := r.Get(ctx, namespacedName, pod)
	podFound := true
	if err != nil {
		if errors.IsNotFound(err) {
			podFound = false
		} else {
			logger.Error(err, "failed to find pod",
				telemetryfields.FieldGameServerNamespace, namespacedName.Namespace,
				telemetryfields.FieldGameServerName, namespacedName.Name)
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to get pod")
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
			logger.Error(err, "failed to find GameServer",
				telemetryfields.FieldGameServerNamespace, namespacedName.Namespace,
				telemetryfields.FieldGameServerName, namespacedName.Name)
			return reconcile.Result{}, err
		}
	}

	gssName := ""
	if podFound {
		gssName = pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
	}
	if gssName == "" && gsFound {
		gssName = gs.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
	}
	if gssName == "" {
		if idx := strings.LastIndex(namespacedName.Name, "-"); idx > 0 {
			gssName = namespacedName.Name[:idx]
		}
	}
	if gssName != "" {
		span.SetAttributes(
			tracing.AttrGameServerSetName(gssName),
			tracing.AttrGameServerSetNamespace(namespacedName.Namespace),
		)
		logger = logger.WithValues(
			telemetryfields.FieldGameServerSetNamespace, namespacedName.Namespace,
			telemetryfields.FieldGameServerSetName, gssName,
		)
	}

	if podFound && !gsFound {
		span.SetAttributes(tracing.AttrReconcileTrigger("create"))
		span.AddEvent(tracing.EventGameServerReconcileBootstrap,
			trace.WithAttributes(
				attribute.String("action", "init gameserver from pod"),
				attribute.String(telemetryfields.FieldK8sPodUID, string(pod.GetUID())),
			))
		gss, err := r.getGameServerSet(ctx, pod)
		if err != nil {
			if errors.IsNotFound(err) {
				return reconcile.Result{}, nil
			}
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to get GameServerSet")
			return reconcile.Result{}, err
		}
		err = r.initGameServerByPod(ctx, gss, pod)
		if err != nil && !errors.IsAlreadyExists(err) {
			logger.Error(err, "failed to create GameServer",
				telemetryfields.FieldGameServerNamespace, namespacedName.Namespace,
				telemetryfields.FieldGameServerName, namespacedName.Name)
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to create GameServer")
			return reconcile.Result{}, err
		}
		span.AddEvent(tracing.EventGameServerReconcileGameServerInitialized,
			trace.WithAttributes(
				tracing.AttrGameServerName(pod.GetName()),
				attribute.String("gss", gss.GetName()),
			))
		return reconcile.Result{}, nil
	}

	if !podFound {
		span.SetAttributes(tracing.AttrReconcileTrigger("delete"))
		shouldDelete := gsFound && gs.GetLabels()[gamekruiseiov1alpha1.GameServerDeletingKey] == "true"
		span.AddEvent(tracing.EventGameServerReconcileCleanup,
			trace.WithAttributes(
				tracing.AttrGameServerName(namespacedName.Name),
				attribute.Bool("markedForDeletion", shouldDelete),
			))
		if shouldDelete {
			err := r.Delete(ctx, gs)
			if err != nil && !errors.IsNotFound(err) {
				logger.Error(err, "failed to delete GameServer",
					telemetryfields.FieldGameServerNamespace, namespacedName.Namespace,
					telemetryfields.FieldGameServerName, namespacedName.Name)
				span.RecordError(err)
				span.SetStatus(codes.Error, "failed to delete GameServer")
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	span.SetAttributes(tracing.AttrReconcileTrigger("update"))

	gsm := NewGameServerManager(gs, pod, r.Client, r.recorder, logger)

	span.AddEvent(tracing.EventGameServerReconcileManagerReady,
		trace.WithAttributes(
			tracing.AttrGameServerName(gs.GetName()),
			attribute.String(telemetryfields.FieldK8sPodUID, string(pod.GetUID())),
		))
	gss, err := r.getGameServerSet(ctx, pod)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get GameServerSet",
			telemetryfields.FieldGameServerNamespace, namespacedName.Namespace,
			telemetryfields.FieldGameServerName, namespacedName.Name)
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get GameServerSet")
		return reconcile.Result{}, err
	}

	span.AddEvent(tracing.EventGameServerReconcileSyncGsToPodStart,
		trace.WithAttributes(
			tracing.AttrGameServerSetName(gss.GetName()),
			tracing.AttrGameServerName(gs.GetName()),
		))
	err = gsm.SyncGsToPod(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "SyncGsToPod failed")
		return reconcile.Result{RequeueAfter: 3 * time.Second}, err
	}
	span.AddEvent(tracing.EventGameServerReconcileSyncGsToPodSuccess,
		trace.WithAttributes(
			attribute.Int(telemetryfields.FieldPodLabelCount, len(pod.GetLabels())),
			attribute.Int(telemetryfields.FieldPodAnnotationCount, len(pod.GetAnnotations())),
		))

	span.AddEvent(tracing.EventGameServerReconcileSyncPodToGsStart,
		trace.WithAttributes(
			tracing.AttrGameServerSetName(gss.GetName()),
			tracing.AttrGameServerName(gs.GetName()),
		))
	err = gsm.SyncPodToGs(ctx, gss)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "SyncPodToGs failed")
		return reconcile.Result{}, err
	}
	span.AddEvent(tracing.EventGameServerReconcileSyncPodToGsSuccess,
		trace.WithAttributes(
			attribute.String(telemetryfields.FieldGameServerResourceVersion, gs.ResourceVersion),
		))

	if gsm.WaitOrNot() {
		span.AddEvent(tracing.EventGameServerReconcileWaitNetworkState,
			trace.WithAttributes(
				attribute.String(telemetryfields.FieldDesired, string(gs.Status.NetworkStatus.DesiredNetworkState)),
				attribute.String(telemetryfields.FieldCurrent, string(gs.Status.NetworkStatus.CurrentNetworkState)),
			))
		span.SetAttributes(tracing.AttrReconcileRequeue(true))
		return ctrl.Result{RequeueAfter: NetworkIntervalTime}, nil
	}

	span.SetStatus(codes.Ok, "Reconcile completed successfully")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GameServerReconciler) SetupWithManager(mgr ctrl.Manager) (c controller.Controller, err error) {
	c, err = ctrl.NewControllerManagedBy(mgr).
		For(&gamekruiseiov1alpha1.GameServer{}).Build(r)
	return c, err
}

func (r *GameServerReconciler) getGameServerSet(ctx context.Context, pod *corev1.Pod) (*gamekruiseiov1alpha1.GameServerSet, error) {
	gssName := pod.GetLabels()[gamekruiseiov1alpha1.GameServerOwnerGssKey]
	gss := &gamekruiseiov1alpha1.GameServerSet{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: pod.GetNamespace(),
		Name:      gssName,
	}, gss)
	return gss, err
}

func (r *GameServerReconciler) initGameServerByPod(ctx context.Context, gss *gamekruiseiov1alpha1.GameServerSet, pod *corev1.Pod) error {
	// default fields
	gs := util.InitGameServer(gss, pod.Name)

	if gss.Spec.GameServerTemplate.ReclaimPolicy == gamekruiseiov1alpha1.CascadeGameServerReclaimPolicy || gss.Spec.GameServerTemplate.ReclaimPolicy == "" {
		// rewrite ownerReferences
		ors := make([]metav1.OwnerReference, 0)
		or := metav1.OwnerReference{
			APIVersion:         pod.APIVersion,
			Kind:               pod.Kind,
			Name:               pod.GetName(),
			UID:                pod.GetUID(),
			Controller:         ptr.To[bool](true),
			BlockOwnerDeletion: ptr.To[bool](true),
		}
		ors = append(ors, or)
		gs.OwnerReferences = ors
	}

	return r.Create(ctx, gs)
}
