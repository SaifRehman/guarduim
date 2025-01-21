package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

// Define the GroupVersionResource for Guarduim
var guarduimGVR = schema.GroupVersionResource{
	Group:    "guard.example.com",
	Version:  "v1",
	Resource: "guarduims",
}

// Global dynamic client
var dynClient dynamic.Interface

// Guarduim represents the custom resource structure
type GuarduimSpec struct {
	Username  string `json:"username"`
	Threshold int    `json:"threshold"`
}

type GuarduimStatus struct {
	IsBlocked      bool `json:"isBlocked,omitempty"`
	FailedAttempts int  `json:"failedAttempts,omitempty"` // Track failed attempts
}

type Guarduim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GuarduimSpec   `json:"spec,omitempty"`
	Status GuarduimStatus `json:"status,omitempty"` // Track failed attempts here
}

func main() {
	// Load in-cluster Kubernetes config
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Error creating Kubernetes config: %v", err)
	}

	// Create a dynamic Kubernetes client
	dynClient, err = dynamic.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating dynamic client: %v", err)
	}

	// Create a dynamic informer factory
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynClient, 0, "", nil)
	informer := factory.ForResource(guarduimGVR).Informer()

	// Watch for add and update events
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { handleEvent(obj) },
		UpdateFunc: func(oldObj, newObj interface{}) { handleFailureEvent(oldObj, newObj) },
	})

	stopCh := make(chan struct{})
	defer close(stopCh)

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start the informer in a separate goroutine
	go informer.Run(stopCh)

	// Periodically check for audit log entries
	go func() {
		ticker := time.NewTicker(5 * time.Second) // Set to 30 seconds or any other interval
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				log.Println("Checking audit logs for authentication failures...")
				auditLog := fetchAuditLog()

				// Parse and process audit log
				for _, logEntry := range auditLog {
					processLogEntry(logEntry)
				}
			}
		}
	}()

	log.Println("Guarduim controller is running...")

	<-sigCh
	log.Println("Shutting down Guarduim controller")
}

func processLogEntry(logEntry string) {
	// Parse log entry and find denied login attempts for specific users
	// For simplicity, assuming a specific log format
	if strings.Contains(logEntry, `"decision":"deny"`) {
		username := extractUsernameFromLog(logEntry)
		if username != "" {
			guarduim := Guarduim{
				Spec: GuarduimSpec{
					Username: username,
				},
			}

			// Fetch and update Guarduim resource
			updateGuarduimFailures(guarduim)

			// Block user if exceeded threshold
			if guarduim.Status.FailedAttempts >= guarduim.Spec.Threshold {
				namespace, err := getNamespace()
				if err != nil {
					log.Printf("Error getting namespace: %v", err)
					return
				}
				blockUser(guarduim.Spec.Username, namespace)
			}
		}
	}
}

func extractUsernameFromLog(logEntry string) string {
	// Extract the username from the log entry (customize based on actual log format)
	// This assumes the log entry contains `"user":"username"`
	if strings.Contains(logEntry, `"user":`) {
		start := strings.Index(logEntry, `"user":`) + len(`"user":`)
		end := strings.Index(logEntry[start:], `,`) + start
		username := logEntry[start:end]
		username = strings.Trim(username, `"`)
		return username
	}
	return ""
}

func updateGuarduimFailures(guarduim Guarduim) {
	namespace, err := getNamespace()
	if err != nil {
		log.Printf("Error reading namespace: %v", err)
		return
	}

	resource := dynClient.Resource(guarduimGVR).Namespace(namespace)

	// Fetch the existing Guarduim resource
	existingGuarduim, err := resource.Get(context.TODO(), guarduim.Name, metav1.GetOptions{})
	if err != nil {
		log.Printf("Error fetching Guarduim resource: %v", err)
		return
	}

	// Ensure status field exists
	status, ok := existingGuarduim.Object["status"].(map[string]interface{})
	if !ok {
		status = make(map[string]interface{})
	}

	log.Printf("status: %v", status)

	// Update failedAttempts count
	status["failedAttempts"] = guarduim.Status.FailedAttempts

	// Apply status update
	existingGuarduim.Object["status"] = status
	_, err = resource.UpdateStatus(context.TODO(), existingGuarduim, metav1.UpdateOptions{})
	if err != nil {
		log.Printf("Error updating Guarduim status: %v", err)
	} else {
		log.Printf("Successfully updated Guarduim status: %s with failedAttempts=%d\n", guarduim.Spec.Username, guarduim.Status.FailedAttempts)
	}
}

func handleEvent(obj interface{}) {
	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		log.Println("Could not convert event object to Unstructured")
		return
	}

	// Convert unstructured object to structured Guarduim
	data, err := json.Marshal(unstructuredObj.Object)
	if err != nil {
		log.Printf("Error marshalling object: %v", err)
		return
	}

	var guarduim Guarduim
	err = json.Unmarshal(data, &guarduim)
	if err != nil {
		log.Printf("Error unmarshalling object: %v", err)
		return
	}

	log.Printf("Processing Guarduim: User=%s, FailedAttempts=%d/%d\n",
		guarduim.Spec.Username, guarduim.Status.FailedAttempts, guarduim.Spec.Threshold)

	// Ensure status is populated with failedAttempts if it isn't already
	if guarduim.Status.FailedAttempts == 0 {
		guarduim.Status.FailedAttempts = 0 // Set default value if not set
	}

	// Increment failedAttempts count
	guarduim.Status.FailedAttempts++

	// Update the Guarduim resource with the incremented failedAttempts count
	updateGuarduimFailures(guarduim)

	// Block user if failedAttempts exceed threshold
	if guarduim.Status.FailedAttempts >= guarduim.Spec.Threshold {
		// Get the namespace dynamically
		namespace, err := getNamespace()
		if err != nil {
			log.Printf("Error getting namespace: %v", err)
			return
		}
		// Pass both username and namespace to blockUser
		blockUser(guarduim.Spec.Username, namespace)
	}
}

func handleFailureEvent(oldObj, newObj interface{}) {
	// Convert to unstructured
	newUnstructuredObj, ok := newObj.(*unstructured.Unstructured)
	if !ok {
		log.Println("Failed to convert newObj to Unstructured")
		return
	}

	// Extract username from Guarduim resource
	data, err := json.Marshal(newUnstructuredObj.Object)
	if err != nil {
		log.Printf("Error marshaling object: %v", err)
		return
	}

	var guarduim Guarduim
	err = json.Unmarshal(data, &guarduim)
	if err != nil {
		log.Printf("Error unmarshalling object: %v", err)
		return
	}

	// Fetch the audit log to find failed authentication attempts
	auditLog := fetchAuditLog()

	// Parse the audit log for deny events related to the user
	for _, logEntry := range auditLog {
		if strings.Contains(logEntry, guarduim.Spec.Username) && strings.Contains(logEntry, `"decision":"deny"`) {
			// Increment the failed attempts count if a deny decision is found
			guarduim.Status.FailedAttempts++
			log.Printf("Incrementing failedAttempts for user: %s, New FailedAttempts: %d\n", guarduim.Spec.Username, guarduim.Status.FailedAttempts)

			// Update Guarduim resource with the new failedAttempts count
			updateGuarduimFailures(guarduim)

			// Check if failed attempts exceed the threshold, then block the user
			if guarduim.Status.FailedAttempts >= guarduim.Spec.Threshold {
				namespace, err := getNamespace()
				if err != nil {
					log.Printf("Error getting namespace: %v", err)
					return
				}
				blockUser(guarduim.Spec.Username, namespace)
			}
		}
	}
}

// fetchAuditLog simulates fetching the audit log file from the OpenShift cluster
func fetchAuditLog() []string {
	// Execute the command to fetch the audit log
	cmd := exec.Command("oc", "adm", "node-logs", "--role=master", "--path=oauth-server/audit.log")
	stdout, err := cmd.Output()
	if err != nil {
		log.Printf("Error fetching audit log: %v", err)
		return nil
	}

	// Split the log output into lines
	logLines := strings.Split(string(stdout), "\n")
	return logLines
}

func blockUser(username, namespace string) {
	log.Printf("Blocking user: %s in namespace: %s\n", username, namespace)

	resource := dynClient.Resource(guarduimGVR).Namespace(namespace)

	guarduim, err := resource.Get(context.TODO(), username, metav1.GetOptions{})
	if err != nil {
		log.Printf("Error fetching Guarduim resource: %v", err)
		return
	}

	// Ensure status exists
	status, ok := guarduim.Object["status"].(map[string]interface{})
	if !ok {
		status = make(map[string]interface{})
	}

	// Read failure count safely
	failures, _ := status["failures"].(float64)
	threshold, _ := guarduim.Object["spec"].(map[string]interface{})["threshold"].(float64)

	if int(failures) >= int(threshold) {
		status["isBlocked"] = true
	}

	guarduim.Object["status"] = status

	// Update the resource
	_, err = resource.UpdateStatus(context.TODO(), guarduim, metav1.UpdateOptions{})
	if err != nil {
		log.Printf("Error updating user block status: %v", err)
	}
}

func getNamespace() (string, error) {
	// Assume the namespace is 'default' for now
	// You can dynamically fetch namespace if required using Kubernetes APIs
	return "default", nil
}
