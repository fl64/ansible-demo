package controller

import (
	"context"
	"time"

	"github.com/fl64/ansible-demo/awx-inventory/internal/kubernetes"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
)

// AWXClientInterface defines the interface for AWX operations
type AWXClientInterface interface {
	GetOrganizationID(name string) (int, error)
	GetInventoryID(name string) (int, error)
	CreateInventory(name string, orgID int) (int, error)
	CreateOrUpdateHost(invID int, hostName string, hostVars map[string]interface{}) error
	DeleteHost(invID int, hostName string) error
	ListHosts(invID int) ([]HostInfo, error)
	WaitForAWX(timeout, interval time.Duration) error
}

// KubernetesClientInterface defines the interface for Kubernetes operations
type KubernetesClientInterface interface {
	ListVMs() ([]*kubernetes.VirtualMachine, error)
	WatchVMs(ctx context.Context, handler func(watch.Event, *unstructured.Unstructured) error) error
}

// HostInfo represents minimal host info needed by controller
type HostInfo struct {
	Name      string
	Variables string
}
