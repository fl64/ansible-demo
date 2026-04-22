package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/fl64/ansible-demo/awx-inventory/internal/awx"
	"github.com/fl64/ansible-demo/awx-inventory/internal/kubernetes"
)

const (
	maxRetries  = 3
	retryDelay  = 5 * time.Second
	defaultWaitTimeout  = 300 * time.Second
	defaultWaitInterval = 5 * time.Second
)

// Controller manages the inventory updater
type Controller struct {
	awxClient    AWXClientInterface
	k8sClient    KubernetesClientInterface
	organization string
	prefix       string
	// Cache of inventory IDs by namespace
	inventoryCache map[string]int
}

// New creates a new controller with real clients
func New(awxURL, awxToken, prefix, organization, namespace string) (*Controller, error) {
	awxClient := awx.NewClient(awxURL, awxToken)
	k8sClient, err := kubernetes.NewClient(namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return &Controller{
		awxClient:      NewAWXClientAdapter(awxClient),
		k8sClient:      k8sClient,
		organization:   organization,
		prefix:         prefix,
		inventoryCache: make(map[string]int),
	}, nil
}

// NewWithClients creates a new controller with provided clients (for testing)
func NewWithClients(awxClient AWXClientInterface, k8sClient KubernetesClientInterface, organization, prefix string) *Controller {
	return &Controller{
		awxClient:      awxClient,
		k8sClient:      k8sClient,
		organization:   organization,
		prefix:         prefix,
		inventoryCache: make(map[string]int),
	}
}

// getDurationFromEnv parses duration from environment variable with fallback
func getDurationFromEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value + "s"); err == nil {
			return d
		}
	}
	return defaultValue
}

// Initialize initializes the controller
func (c *Controller) Initialize() error {
	timeout := getDurationFromEnv("AWX_WAIT_TIMEOUT", defaultWaitTimeout)
	interval := getDurationFromEnv("AWX_WAIT_INTERVAL", defaultWaitInterval)

	log.Printf("Waiting for AWX availability...")
	if err := c.awxClient.WaitForAWX(timeout, interval); err != nil {
		return fmt.Errorf("failed to wait for AWX: %w", err)
	}
	log.Printf("AWX is available")

	if _, err := c.awxClient.GetOrganizationID(c.organization); err != nil {
		return fmt.Errorf("failed to get organization ID: %w", err)
	}

	log.Printf("Controller initialized. Inventories will be created per namespace as needed.")
	return nil
}

// buildInventoryName builds inventory name from prefix and namespace
func (c *Controller) buildInventoryName(namespace string) string {
	if c.prefix != "" {
		return fmt.Sprintf("%s %s", c.prefix, namespace)
	}
	return namespace
}

// getOrCreateInventoryForNamespace gets or creates inventory for a namespace
func (c *Controller) getOrCreateInventoryForNamespace(namespace string) (int, error) {
	if invID, exists := c.inventoryCache[namespace]; exists {
		return invID, nil
	}

	inventoryName := c.buildInventoryName(namespace)
	orgID, err := c.awxClient.GetOrganizationID(c.organization)
	if err != nil {
		return 0, fmt.Errorf("failed to get organization ID: %w", err)
	}

	invID, err := c.awxClient.GetInventoryID(inventoryName)
	if err != nil {
		return 0, fmt.Errorf("failed to get inventory ID: %w", err)
	}

	if invID == 0 {
		log.Printf("Creating inventory '%s' for namespace '%s'...", inventoryName, namespace)
		invID, err = c.awxClient.CreateInventory(inventoryName, orgID)
		if err != nil {
			return 0, fmt.Errorf("failed to create inventory: %w", err)
		}
		log.Printf("Inventory '%s' created with ID: %d", inventoryName, invID)
	} else {
		log.Printf("Inventory '%s' already exists with ID: %d", inventoryName, invID)
	}

	c.inventoryCache[namespace] = invID
	return invID, nil
}

// retryWithBackoff executes a function with retry logic
func retryWithBackoff(operation func() error, operationName string) error {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := operation()
		if err == nil {
			return nil
		}

		if attempt < maxRetries {
			log.Printf("WARN: %s failed (attempt %d/%d): %v, retrying...", operationName, attempt, maxRetries, err)
			time.Sleep(retryDelay)
			continue
		}

		return fmt.Errorf("%s failed after %d attempts: %w", operationName, maxRetries, err)
	}
	return fmt.Errorf("unexpected error in retryWithBackoff")
}

// handleVMAdded handles ADDED or MODIFIED events
func (c *Controller) handleVMAdded(vm *kubernetes.VirtualMachine) error {
	hostVars := map[string]interface{}{
		"vm_name":      vm.Name,
		"vm_namespace": vm.Namespace,
		"labels":       vm.Labels,
		"ansible_host": vm.IP,
	}

	err := retryWithBackoff(func() error {
		invID, err := c.getOrCreateInventoryForNamespace(vm.Namespace)
		if err != nil {
			return err
		}
		return c.awxClient.CreateOrUpdateHost(invID, vm.Name, hostVars)
	}, fmt.Sprintf("create/update host '%s' in namespace '%s'", vm.Name, vm.Namespace))

	if err == nil {
		log.Printf("Host '%s' synced (ansible_host=%s)", vm.Name, vm.IP)
	}
	return err
}

// handleVMDeleted handles DELETED events
func (c *Controller) handleVMDeleted(namespace, name string) error {
	return retryWithBackoff(func() error {
		invID, err := c.getOrCreateInventoryForNamespace(namespace)
		if err != nil {
			return err
		}
		return c.awxClient.DeleteHost(invID, name)
	}, fmt.Sprintf("delete host '%s' in namespace '%s'", name, namespace))
}

// extractVMInfo extracts namespace and name from unstructured object
func extractVMInfo(obj *unstructured.Unstructured) (namespace, name string, ok bool) {
	var found bool
	namespace, found, _ = unstructured.NestedString(obj.Object, "metadata", "namespace")
	if !found {
		return "", "", false
	}
	name, found, _ = unstructured.NestedString(obj.Object, "metadata", "name")
	if !found {
		return "", "", false
	}
	return namespace, name, true
}

// shouldProcessVM checks if VM should be processed (has IP address)
func shouldProcessVM(vm *kubernetes.VirtualMachine, eventType watch.EventType) bool {
	if vm.IP == "" {
		if eventType == watch.Added {
			log.Printf("WARN: VM '%s' in namespace '%s' has no IP address, skipping", vm.Name, vm.Namespace)
		}
		return false
	}
	return true
}

// handleWatchEvent handles a watch event
func (c *Controller) handleWatchEvent(event watch.Event, obj *unstructured.Unstructured) error {
	namespace, name, ok := extractVMInfo(obj)
	if !ok {
		return nil
	}

	var err error
	switch event.Type {
	case watch.Added:
		log.Printf("Event: ADDED for VM '%s' in namespace '%s'", name, namespace)
		vm := kubernetes.UnstructuredToVM(obj)
		if shouldProcessVM(vm, event.Type) {
			err = c.handleVMAdded(vm)
		}

	case watch.Modified:
		vm := kubernetes.UnstructuredToVM(obj)
		if shouldProcessVM(vm, event.Type) {
			log.Printf("Event: MODIFIED for VM '%s' in namespace '%s' (IP: %s)", name, namespace, vm.IP)
			err = c.handleVMAdded(vm)
		}

	case watch.Deleted:
		log.Printf("Event: DELETED for VM '%s' in namespace '%s'", name, namespace)
		err = c.handleVMDeleted(namespace, name)
		if err == nil {
			log.Printf("Host '%s' removed from inventory", name)
		}

	default:
		log.Printf("WARN: Unknown event type: %s", event.Type)
		return nil
	}

	if err != nil {
		log.Printf("ERROR: Failed to process event %s for VM '%s' in namespace '%s': %v", event.Type, name, namespace, err)
	}

	return nil
}

// Run starts the controller
func (c *Controller) Run(ctx context.Context) error {
	if err := c.Initialize(); err != nil {
		return err
	}

	// Sync: remove stale hosts from AWX that have no corresponding VM in K8s
	log.Printf("Syncing inventories with K8s state...")
	if err := c.syncInventories(); err != nil {
		log.Printf("WARN: sync failed: %v, continuing...", err)
	}

	log.Printf("Starting VirtualMachine resources watch...")
	log.Printf("Inventories will be created per namespace as needed")

	return c.k8sClient.WatchVMs(ctx, c.handleWatchEvent)
}

// syncInventories removes stale hosts from AWX that have no corresponding VM in K8s
func (c *Controller) syncInventories() error {
	// Get all VMs from Kubernetes
	k8sVMs, err := c.k8sClient.ListVMs()
	if err != nil {
		return fmt.Errorf("failed to list VMs: %w", err)
	}

	// Build a map of existing VMs: "namespace/name" -> true
	k8sHostKeys := make(map[string]bool)
	namespaceSet := make(map[string]bool)
	for _, vm := range k8sVMs {
		if vm.IP != "" {
			k8sHostKeys[vm.Namespace+"/"+vm.Name] = true
			namespaceSet[vm.Namespace] = true
		}
	}
	log.Printf("Sync: found %d VMs with IPs in K8s across %d namespaces", len(k8sHostKeys), len(namespaceSet))

	// For each namespace that has VMs, check AWX inventory
	for ns := range namespaceSet {
		invID, err := c.getOrCreateInventoryForNamespace(ns)
		if err != nil {
			log.Printf("Sync: failed to get inventory for namespace '%s': %v", ns, err)
			continue
		}
		if invID == 0 {
			log.Printf("Sync: no inventory found for namespace '%s', skipping", ns)
			continue
		}

		// Get all hosts from AWX inventory
		awxHosts, err := c.awxClient.ListHosts(invID)
		if err != nil {
			log.Printf("Sync: failed to list hosts in inventory '%s': %v", ns, err)
			continue
		}

		// Remove hosts that don't exist in K8s
		removedCount := 0
		for _, host := range awxHosts {
			// Parse vm_namespace from host variables
			vmNS, vmName := extractHostVMInfo(host.Variables)
			if vmNS == "" || vmName == "" {
				// Can't determine VM info, skip (might be manually added host)
				continue
			}

			key := vmNS + "/" + vmName
			if !k8sHostKeys[key] {
				log.Printf("Sync: removing stale host '%s' (VM: %s/%s) from inventory '%s'", host.Name, vmNS, vmName, ns)
				if err := c.awxClient.DeleteHost(invID, host.Name); err != nil {
					log.Printf("Sync: failed to delete host '%s': %v", host.Name, err)
				} else {
					removedCount++
				}
			}
		}
		if removedCount > 0 {
			log.Printf("Sync: removed %d stale host(s) from inventory '%s'", removedCount, ns)
		} else {
			log.Printf("Sync: inventory '%s' is clean (%d hosts)", ns, len(awxHosts))
		}
	}

	return nil
}

// extractHostVMInfo extracts vm_namespace and vm_name from AWX host variables
func extractHostVMInfo(variables string) (namespace, name string) {
	if variables == "" {
		return "", ""
	}

	var vars map[string]interface{}
	if err := json.Unmarshal([]byte(variables), &vars); err != nil {
		return "", ""
	}

	if ns, ok := vars["vm_namespace"].(string); ok {
		namespace = ns
	}
	if n, ok := vars["vm_name"].(string); ok {
		name = n
	}

	return namespace, name
}

// Start starts the controller with signal handling
func (c *Controller) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, shutting down...", sig)
		cancel()
	}()

	return c.Run(ctx)
}
