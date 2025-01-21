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
		UpdateFunc: func(oldObj, newObj interface{}) { handleEvent(newObj) },
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

// Process Guarduim events
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

	log.Printf("Processing Guarduim: User=%s, Failures=%d/%d\n",
		guarduim.Spec.Username, guarduim.Spec.Failures, guarduim.Spec.Threshold)

	// Increment failure count
	guarduim.Spec.Failures++

	// Update the Guarduim resource with the incremented failures count
	updateGuarduimFailures(guarduim)

	// Block user if failures exceed threshold
	if guarduim.Spec.Failures >= guarduim.Spec.Threshold {
		blockUser(guarduim.Spec.Username)
	}
}
func updateGuarduimFailures(guarduim Guarduim) {
	// Read the namespace dynamically
	namespace, err := getNamespace()
	if err != nil {
		log.Printf("Error reading namespace: %v", err)
		return
	}

	resource := dynClient.Resource(guarduimGVR).Namespace(namespace)

	// Fetch the existing resource
	existingGuarduim, err := resource.Get(context.TODO(), guarduim.Metadata.Name, metav1.GetOptions{})
	if err != nil {
		log.Printf("Error fetching Guarduim resource: %v", err)
		return
	}

	// Update the failures count
	if existingGuarduim.Object["spec"] == nil {
		existingGuarduim.Object["spec"] = make(map[string]interface{})
	}
	existingGuarduim.Object["spec"].(map[string]interface{})["failures"] = guarduim.Spec.Failures

	// Apply the update
	_, err = resource.Update(context.TODO(), existingGuarduim, metav1.UpdateOptions{})
	if err != nil {
		log.Printf("Error updating Guarduim failures: %v", err)
	} else {
		log.Printf("Updated Guarduim: User=%s, New Failures=%d\n", guarduim.Spec.Username, guarduim.Spec.Failures)
	}
}

// blockUser updates the Guarduim resource to block a user
func blockUser(username string) {
	// Read the namespace dynamically from the environment file
	namespace, err := getNamespace()
	if err != nil {
		log.Printf("Error reading namespace: %v", err)
		return
	}

	log.Printf("Blocking user: %s in namespace: %s\n", username, namespace)

	// Retrieve the Guarduim resource based on the username
	resource := dynClient.Resource(guarduimGVR).Namespace(namespace) // Use dynamic namespace

	guarduim, err := resource.Get(context.TODO(), username, metav1.GetOptions{})
	if err != nil {
		log.Printf("Error fetching Guarduim resource: %v", err)
		return
	}

	// Check if the spec has an 'isBlocked' field and update it
	if guarduim.Object["spec"] == nil {
		guarduim.Object["spec"] = make(map[string]interface{})
	}

	// Check if the user has exceeded the failure threshold and block the user
	if guarduim.Object["spec"].(map[string]interface{})["failures"].(int) >= guarduim.Object["spec"].(map[string]interface{})["threshold"].(int) {
		guarduim.Object["spec"].(map[string]interface{})["isBlocked"] = true
	}

	// Update the failed attempts and isBlocked status in the Guarduim resource
	if guarduim.Object["status"] == nil {
		guarduim.Object["status"] = make(map[string]interface{})
	}

	guarduim.Object["status"].(map[string]interface{})["failedAttempts"] = guarduim.Object["spec"].(map[string]interface{})["failures"]
	guarduim.Object["status"].(map[string]interface{})["isBlocked"] = guarduim.Object["spec"].(map[string]interface{})["isBlocked"]

	// Update the resource
	_, err = resource.Update(context.TODO(), guarduim, metav1.UpdateOptions{})
	if err != nil {
		log.Printf("Error updating Guarduim resource: %v", err)
		return
	}

	log.Printf("User %s has been blocked.\n", username)
}

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
		return scanner.Text(), nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading namespace file: %v", err)
	}

	// Return error if we couldn't read the namespace
	return "", fmt.Errorf("namespace not found")
}
