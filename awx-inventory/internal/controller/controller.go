package controller

import (
	"context"
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

// Controller manages the inventory updater
type Controller struct {
	awxClient    *awx.Client
	k8sClient    *kubernetes.Client
	organization string
	prefix       string
	// Cache of inventory IDs by namespace
	inventoryCache map[string]int
}

// New creates a new controller
func New(awxURL, awxToken, prefix, organization, namespace string) (*Controller, error) {
	awxClient := awx.NewClient(awxURL, awxToken)

	k8sClient, err := kubernetes.NewClient(namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return &Controller{
		awxClient:      awxClient,
		k8sClient:      k8sClient,
		organization:   organization,
		prefix:         prefix,
		inventoryCache: make(map[string]int),
	}, nil
}

// Initialize initializes the controller
func (c *Controller) Initialize() error {
	// Wait for AWX
	timeout := 300 * time.Second
	interval := 5 * time.Second
	if timeoutStr := os.Getenv("AWX_WAIT_TIMEOUT"); timeoutStr != "" {
		if d, err := time.ParseDuration(timeoutStr + "s"); err == nil {
			timeout = d
		}
	}
	if intervalStr := os.Getenv("AWX_WAIT_INTERVAL"); intervalStr != "" {
		if d, err := time.ParseDuration(intervalStr + "s"); err == nil {
			interval = d
		}
	}

	log.Printf("Waiting for AWX availability...")
	if err := c.awxClient.WaitForAWX(timeout, interval); err != nil {
		return fmt.Errorf("failed to wait for AWX: %w", err)
	}
	log.Printf("AWX is available")

	// Verify organization exists
	_, err := c.awxClient.GetOrganizationID(c.organization)
	if err != nil {
		return fmt.Errorf("failed to get organization ID: %w", err)
	}

	log.Printf("Controller initialized. Inventories will be created per namespace as needed.")
	return nil
}

// getOrCreateInventoryForNamespace gets or creates inventory for a namespace
func (c *Controller) getOrCreateInventoryForNamespace(namespace string) (int, error) {
	// Check cache first
	if invID, exists := c.inventoryCache[namespace]; exists {
		return invID, nil
	}

	// Build inventory name: prefix + namespace (or just namespace if prefix is empty)
	var inventoryName string
	if c.prefix != "" {
		inventoryName = fmt.Sprintf("%s %s", c.prefix, namespace)
	} else {
		inventoryName = namespace
	}

	// Get organization ID
	orgID, err := c.awxClient.GetOrganizationID(c.organization)
	if err != nil {
		return 0, fmt.Errorf("failed to get organization ID: %w", err)
	}

	// Get or create inventory
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

	// Cache the inventory ID
	c.inventoryCache[namespace] = invID
	return invID, nil
}

// handleVMAdded handles ADDED or MODIFIED events
func (c *Controller) handleVMAdded(vm *kubernetes.VirtualMachine) error {
	// Get or create inventory for this namespace
	invID, err := c.getOrCreateInventoryForNamespace(vm.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get inventory for namespace '%s': %w", vm.Namespace, err)
	}

	hostName := vm.Name

	hostVars := map[string]interface{}{
		"vm_name":      vm.Name,
		"vm_namespace": vm.Namespace,
		"labels":       vm.Labels,
		"ansible_host": vm.IP,
	}

	return c.awxClient.CreateOrUpdateHost(invID, hostName, hostVars)
}

// handleVMDeleted handles DELETED events
func (c *Controller) handleVMDeleted(namespace, name string) error {
	// Get inventory for this namespace
	invID, err := c.getOrCreateInventoryForNamespace(namespace)
	if err != nil {
		return fmt.Errorf("failed to get inventory for namespace '%s': %w", namespace, err)
	}

	hostName := name
	return c.awxClient.DeleteHost(invID, hostName)
}

// handleWatchEvent handles a watch event
func (c *Controller) handleWatchEvent(event watch.Event, obj *unstructured.Unstructured) error {
	namespace, found, _ := unstructured.NestedString(obj.Object, "metadata", "namespace")
	if !found {
		return nil
	}

	name, found, _ := unstructured.NestedString(obj.Object, "metadata", "name")
	if !found {
		return nil
	}

	switch event.Type {
	case watch.Added:
		// Log ADDED events (new VMs)
		log.Printf("Event: ADDED for VM '%s' in namespace '%s'", name, namespace)
		vm := kubernetes.UnstructuredToVM(obj)

		if vm.IP == "" {
			log.Printf("WARN: VM '%s' in namespace '%s' has no IP address, skipping", name, namespace)
			return nil
		}

		return c.handleVMAdded(vm)

	case watch.Modified:
		// Only process MODIFIED if VM has IP (avoid spam for VMs without IP)
		vm := kubernetes.UnstructuredToVM(obj)

		if vm.IP == "" {
			// Silently skip VMs without IP to reduce log spam
			return nil
		}

		// Only log if we're actually processing it
		log.Printf("Event: MODIFIED for VM '%s' in namespace '%s' (IP: %s)", name, namespace, vm.IP)
		return c.handleVMAdded(vm)

	case watch.Deleted:
		return c.handleVMDeleted(namespace, name)

	default:
		log.Printf("WARN: Unknown event type: %s", event.Type)
		return nil
	}
}

// Run starts the controller
func (c *Controller) Run(ctx context.Context) error {
	if err := c.Initialize(); err != nil {
		return err
	}

	log.Printf("Starting VirtualMachine resources watch...")
	log.Printf("Note: Watch will process all existing VMs as ADDED events on startup")
	log.Printf("Inventories will be created per namespace as needed")

	return c.k8sClient.WatchVMs(ctx, c.handleWatchEvent)
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
