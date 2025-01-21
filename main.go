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
type Guarduim struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Username  string `json:"username"`
		Threshold int    `json:"threshold"`
		Failures  int    `json:"failures"`
	} `json:"spec"`
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

	log.Printf("[BEFORE] Processing Guarduim: User=%s, Failures=%d/%d\n",
		guarduim.Spec.Username, guarduim.Spec.Failures, guarduim.Spec.Threshold)

	// Update the Guarduim status
	updateGuarduimFailures(guarduim)

	// Block user if failures exceed threshold
	if guarduim.Spec.Failures >= guarduim.Spec.Threshold {
		namespace, err := getNamespace()
		if err != nil {
			log.Printf("Error reading namespace: %v", err)
			return
		}
		blockUser(guarduim.Spec.Username, namespace)
	}
}

func updateGuarduimFailures(guarduim Guarduim) {
	namespace, err := getNamespace()
	if err != nil {
		log.Printf("Error reading namespace: %v", err)
		return
	}
	log.Printf("namespace isss: %v", namespace)

	resource := dynClient.Resource(guarduimGVR).Namespace(namespace)

	// Fetch the existing resource
	existingGuarduim, err := resource.Get(context.TODO(), guarduim.Metadata.Name, metav1.GetOptions{})

	if err != nil {
		log.Printf("Error fetching Guarduim resource: %v", err)
		return
	}

	// Ensure the status field exists
	status, ok := existingGuarduim.Object["status"].(map[string]interface{})
	if !ok {
		status = make(map[string]interface{})
	}

	// Get the current failures count from status (default to 0 if not found)
	failures, _ := status["failures"].(float64) // CRDs store numbers as float64
	newFailures := int(failures) + 1
	status["failures"] = newFailures

	// Update the status with the incremented failure count
	existingGuarduim.Object["status"] = status

	log.Printf("Updating Guarduim: User=%s, New Failures=%d\n", guarduim.Spec.Username, newFailures)
	log.Printf("Existing Guarduim: %+v", existingGuarduim)

	// Apply the update to the status field (not spec)
	_, err = resource.UpdateStatus(context.TODO(), existingGuarduim, metav1.UpdateOptions{})
	if err != nil {
		log.Printf("Error updating Guarduim status: %v", err)
	} else {
		log.Printf("Successfully updated Guarduim status: %s with Failures=%d\n", guarduim.Spec.Username, newFailures)
	}
}

// Process Guarduim events
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
		newGuarduim.Spec.Username, oldGuarduim.Spec.Failures, newGuarduim.Spec.Failures)

	// Detect authentication failure (when failures increase)
	if newGuarduim.Spec.Failures > oldGuarduim.Spec.Failures {
		log.Printf("Authentication failed for user %s. Current Failures: %d/%d\n",
			newGuarduim.Spec.Username, newGuarduim.Spec.Failures, newGuarduim.Spec.Threshold)

		// Increment the failure count
		newGuarduim.Spec.Failures++
		log.Printf("Incrementing failure count for user %s: New Failures=%d\n",
			newGuarduim.Spec.Username, newGuarduim.Spec.Failures)

		// Update the Guarduim CR to persist the new failure count
		updateGuarduimFailures(newGuarduim)
	}

	// Check if failures exceed the threshold
	if newGuarduim.Spec.Failures >= newGuarduim.Spec.Threshold {
		log.Printf("User %s exceeded failure threshold. Blocking user...\n", newGuarduim.Spec.Username)
		blockUser(newGuarduim.Metadata.Name, newGuarduim.Metadata.Namespace)
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
