package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

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
	FailedAttempts int  `json:"failedAttempts,omitempty"` // Only track failed attempts now
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

	go informer.Run(stopCh)
	log.Println("Guarduim controller is running...")

	<-sigCh
	log.Println("Shutting down Guarduim controller")
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

	// Get the current failedAttempts value (default to 0 if not set)
	currentAttempts, ok := status["failedAttempts"].(int)
	if !ok {
		currentAttempts = 0
	}

	// Increment failedAttempts manually
	status["failedAttempts"] = currentAttempts + 1

	// Apply status update
	existingGuarduim.Object["status"] = status
	_, err = resource.UpdateStatus(context.TODO(), existingGuarduim, metav1.UpdateOptions{})
	if err != nil {
		log.Printf("Error updating Guarduim status: %v", err)
	} else {
		log.Printf("Successfully updated Guarduim status: %s with failedAttempts=%d\n", guarduim.Spec.Username, status["failedAttempts"])
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
	oldUnstructured, okOld := oldObj.(*unstructured.Unstructured)
	newUnstructured, okNew := newObj.(*unstructured.Unstructured)

	if !okOld || !okNew {
		log.Println("Could not convert event objects to Unstructured")
		return
	}

	// Convert to structured Guarduim
	var oldGuarduim, newGuarduim Guarduim

	oldData, _ := json.Marshal(oldUnstructured.Object)
	json.Unmarshal(oldData, &oldGuarduim)

	newData, _ := json.Marshal(newUnstructured.Object)
	json.Unmarshal(newData, &newGuarduim)

	// Log the event
	log.Printf("User: %s, Old Failures: %d, New Failures: %d\n",
		newGuarduim.Spec.Username, oldGuarduim.Status.FailedAttempts, newGuarduim.Status.FailedAttempts)

	// Detect authentication failure (when failures increase)
	if newGuarduim.Status.FailedAttempts > oldGuarduim.Status.FailedAttempts {
		log.Printf("Authentication failed for user %s. Current Failures: %d/%d\n",
			newGuarduim.Spec.Username, newGuarduim.Status.FailedAttempts, newGuarduim.Spec.Threshold)

		// Increment the failure count
		newGuarduim.Status.FailedAttempts++
		log.Printf("Incrementing failure count for user %s: New Failures=%d\n",
			newGuarduim.Spec.Username, newGuarduim.Status.FailedAttempts)

		// Update the Guarduim CR to persist the new failure count
		updateGuarduimFailures(newGuarduim)
	}

	// Check if failures exceed the threshold
	if newGuarduim.Status.FailedAttempts >= newGuarduim.Spec.Threshold {
		log.Printf("User %s exceeded failure threshold. Blocking user...\n", newGuarduim.Spec.Username)
		blockUser(newGuarduim.Spec.Username, newGuarduim.Namespace)
	}
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
		log.Printf("Error updating Guarduim status: %v", err)
		return
	}

	log.Printf("User %s has been blocked.\n", username)
}

// getNamespace retrieves the namespace where the controller is running
func getNamespace() (string, error) {
	// The namespace is stored in this file in Kubernetes
	namespaceFile := "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	file, err := os.Open(namespaceFile)
	if err != nil {
		return "", fmt.Errorf("could not open namespace file: %v", err)
	}
	defer file.Close()

	// Read the namespace from the file
	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		namespace := scanner.Text()
		log.Printf("Fetched namespace: %s\n", namespace) // Log to verify
		return namespace, nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading namespace file: %v", err)
	}

	// Return error if we couldn't read the namespace
	return "", fmt.Errorf("namespace not found")
}
