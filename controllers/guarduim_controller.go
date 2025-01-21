package controllers

import (
	"context"
	"time"

	guardv1 "github.com/SaifRehman/guarduim/api/v1"
	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// GuarduimReconciler reconciles a Guarduim object
type GuarduimReconciler struct {
	client.Client
	Log logr.Logger
}

// +kubebuilder:rbac:groups=guard.example.com,resources=guarduims,verbs=get;list;watch;create;update;patch;delete

func (r *GuarduimReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	_ = log.FromContext(ctx)

	// Fetch the Guarduim instance
	guarduim := &guardv1.Guarduim{}
	err := r.Get(ctx, req.NamespacedName, guarduim)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Check if the user has exceeded the failed attempts threshold
	if guarduim.Spec.Threshold <= guarduim.Status.FailedAttempts {
		// Block the user
		guarduim.Status.IsBlocked = true
	}

	// Update the status of the Guarduim
	err = r.Status().Update(ctx, guarduim)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Requeue every 5 seconds to check for failed logins periodically
	return reconcile.Result{
		RequeueAfter: time.Second * 5,
	}, nil
}

func (r *GuarduimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Create the controller and handle any errors
	ctrl, err := controller.New("guarduim-controller", mgr, controller.Options{
		Reconciler: r,
	})
	if err != nil {
		return err
	}

	// Set up the watch for the Guarduim resource
	err = ctrl.Watch(
		&source.Kind{Type: &guardv1.Guarduim{}}, // Ensure the correct type is used here
		&handler.EnqueueRequestForObject{},
		predicate.NewPredicateFuncs(func(object client.Object) bool {
			// Add any filtering logic if needed
			return true
		}),
	)
	if err != nil {
		return err
	}

	return nil
}
