package events

import (
	"context"
	"fmt"
	"strings"
	"time"

	pmetrics "github.com/awslabs/operatorpkg/metrics"
	"github.com/awslabs/operatorpkg/object"
	"github.com/awslabs/operatorpkg/singleton"
	"github.com/samber/lo"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/clock"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type Controller[T client.Object] struct {
	gvk        schema.GroupVersionKind
	startTime  time.Time
	kubeClient client.Client
	EventCount pmetrics.CounterMetric
	EventWatch watch.Interface
}

func NewController[T client.Object](ctx context.Context, client client.Client, clock clock.Clock, kubernetesInterface kubernetes.Interface) *Controller[T] {
	gvk := object.GVK(object.New[T]())
	return &Controller[T]{
		gvk:        gvk,
		startTime:  clock.Now(),
		kubeClient: client,
		EventCount: eventTotalMetric(strings.ToLower(gvk.Kind)),
		EventWatch: lo.Must(kubernetesInterface.CoreV1().Events("").Watch(ctx, metav1.ListOptions{
			FieldSelector: fmt.Sprintf("involvedObject.kind=%s,involvedObject.apiVersion=%s", gvk.Kind, gvk.GroupVersion().String()),
		})),
	}
}

func (c *Controller[T]) Register(ctx context.Context, m manager.Manager) error {
	return controllerruntime.NewControllerManagedBy(m).
		Named(fmt.Sprintf("operatorpkg.%s.events", strings.ToLower(c.gvk.Kind))).
		WatchesRawSource(singleton.Source()).
		Complete(singleton.AsChannelObjectReconciler(c.EventWatch.ResultChan(), c))
}

func (c *Controller[T]) Reconcile(ctx context.Context, event *v1.Event) (reconcile.Result, error) {
	// We check if the event was created in the lifetime of this controller
	// since we don't duplicate metrics on controller restart or lease handover
	if c.startTime.Before(event.LastTimestamp.Time) {
		c.EventCount.Inc(map[string]string{
			pmetrics.LabelType:   event.Type,
			pmetrics.LabelReason: event.Reason,
		})
	}

	return reconcile.Result{RequeueAfter: singleton.RequeueImmediately}, nil
}
