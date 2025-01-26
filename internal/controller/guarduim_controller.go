package controller

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"

	v1 "github.com/SaifRehman/guarduim/api/v1"
	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// GuarduimReconciler reconciles a Guarduim object
type GuarduimReconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

//+kubebuilder:rbac:groups=guard.example.com,resources=guarduims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=guard.example.com,resources=guarduims/status,verbs=get;update;patch

func (r *GuarduimReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := r.Log.WithValues("guarduim", req.NamespacedName)

	// Fetch Guarduim instance
	guarduim := &v1.Guarduim{}
	err := r.Client.Get(ctx, req.NamespacedName, guarduim)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	// Execute command to count authentication failures
	cmd := exec.Command("sh", "-c",
		`oc adm node-logs --role=master --path=oauth-server/audit.log | grep 'authentication.openshift.io/decision":"deny' | grep 'authentication.openshift.io/username":"`+guarduim.Spec.Username+`"' | wc -l`)

	output, err := cmd.Output()
	if err != nil {
		log.Error(err, "Failed to execute command")
		return reconcile.Result{}, err
	}

	// Parse the failure count
	failureCount := strings.TrimSpace(string(output))
	count := 0
	if failureCount != "" {
		count, err = strconv.Atoi(failureCount)
		if err != nil {
			log.Error(err, "Failed to convert failure count to int")
			return reconcile.Result{}, err
		}
	}

	// Update the status
	guarduim.Status.FailureCount = count
	guarduim.Status.Blocked = count > guarduim.Spec.Threshold

	err = r.Client.Status().Update(ctx, guarduim)
	if err != nil {
		log.Error(err, "Failed to update Guarduim status")
		return reconcile.Result{}, err
	}

	// Return reconcile result to retry after 30 seconds
	return reconcile.Result{
		RequeueAfter: 30 * time.Second,
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GuarduimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Guarduim{}).
		Complete(r)
}
