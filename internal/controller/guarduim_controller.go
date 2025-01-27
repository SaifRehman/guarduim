package controller

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	v1 "github.com/SaifRehman/guarduim/api/v1"
	"github.com/go-logr/logr"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;create;delete;update;patch

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

	// Block user if threshold exceeded, otherwise unblock
	if guarduim.Status.Blocked {
		err := r.blockUser(ctx, guarduim.Spec.Username)
		if err != nil {
			log.Error(err, "Failed to block user")
			return reconcile.Result{}, err
		}
	} else {
		err := r.unblockUser(ctx, guarduim.Spec.Username)
		if err != nil {
			log.Error(err, "Failed to unblock user")
			return reconcile.Result{}, err
		}
	}

	// Requeue after 30 seconds
	return reconcile.Result{
		RequeueAfter: 30 * time.Second,
	}, nil
}

// createBlockedUserClusterRole ensures the ClusterRole exists
func (r *GuarduimReconciler) createBlockedUserClusterRole(ctx context.Context) error {
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "blocked-user",
		},
		Rules: []rbacv1.PolicyRule{}, // Empty rules mean no permissions
	}

	// Check if ClusterRole already exists
	err := r.Client.Get(ctx, client.ObjectKey{Name: "blocked-user"}, clusterRole)
	if err != nil {
		if errors.IsNotFound(err) {
			return r.Client.Create(ctx, clusterRole)
		}
		return err
	}

	return nil
}

// blockUser creates a ClusterRoleBinding for the blocked user
func (r *GuarduimReconciler) blockUser(ctx context.Context, username string) error {
	// Ensure the ClusterRole exists
	if err := r.createBlockedUserClusterRole(ctx); err != nil {
		return err
	}

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "block-user-" + username,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     "User",
				Name:     username,
				APIGroup: "rbac.authorization.k8s.io",
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     "blocked-user",
			APIGroup: "rbac.authorization.k8s.io",
		},
	}

	// Check if ClusterRoleBinding exists
	err := r.Client.Get(ctx, client.ObjectKey{Name: "block-user-" + username}, clusterRoleBinding)
	if err != nil {
		if errors.IsNotFound(err) {
			return r.Client.Create(ctx, clusterRoleBinding)
		}
		return err
	}

	return nil
}

// unblockUser removes the ClusterRoleBinding
func (r *GuarduimReconciler) unblockUser(ctx context.Context, username string) error {
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: "block-user-" + username}, clusterRoleBinding)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil // Already unblocked
		}
		return err
	}

	return r.Client.Delete(ctx, clusterRoleBinding)
}

// SetupWithManager sets up the controller with the Manager.
func (r *GuarduimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Guarduim{}).
		Complete(r)
}
