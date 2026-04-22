package controller

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/fl64/ansible-demo/awx-inventory/internal/kubernetes"
)

// ============================================================================
// Mocks
// ============================================================================

type MockAWXClient struct {
	mu sync.Mutex

	orgID        int
	orgIDErr     error
	organization string

	inventories  map[string]int
	createInvErr error
	getInvErr    error

	hosts           map[int][]HostInfo
	getHostErr      error
	createHostErr   error
	deleteHostErr   error
	deleteHostCalls []DeleteHostCall

	ListHostsCalls []int
}

type DeleteHostCall struct {
	InvID    int
	HostName string
}

func NewMockAWXClient() *MockAWXClient {
	return &MockAWXClient{
		orgID:       1,
		inventories: make(map[string]int),
		hosts:       make(map[int][]HostInfo),
	}
}

func (m *MockAWXClient) WaitForAWX(timeout, interval time.Duration) error {
	return nil
}

func (m *MockAWXClient) GetOrganizationID(name string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.orgIDErr != nil {
		return 0, m.orgIDErr
	}
	m.organization = name
	return m.orgID, nil
}

func (m *MockAWXClient) GetInventoryID(name string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getInvErr != nil {
		return 0, m.getInvErr
	}
	if id, exists := m.inventories[name]; exists {
		return id, nil
	}
	return 0, nil
}

func (m *MockAWXClient) CreateInventory(name string, orgID int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createInvErr != nil {
		return 0, m.createInvErr
	}
	if id, exists := m.inventories[name]; exists {
		return id, nil
	}
	id := len(m.inventories) + 100
	m.inventories[name] = id
	m.hosts[id] = []HostInfo{}
	return id, nil
}

func (m *MockAWXClient) CreateOrUpdateHost(invID int, hostName string, hostVars map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createHostErr != nil {
		return m.createHostErr
	}

	hosts := m.hosts[invID]
	for i, h := range hosts {
		if h.Name == hostName {
			hosts[i].Variables = varsToString(hostVars)
			return nil
		}
	}
	m.hosts[invID] = append(hosts, HostInfo{Name: hostName, Variables: varsToString(hostVars)})
	return nil
}

func (m *MockAWXClient) DeleteHost(invID int, hostName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteHostCalls = append(m.deleteHostCalls, DeleteHostCall{InvID: invID, HostName: hostName})

	if m.deleteHostErr != nil {
		return m.deleteHostErr
	}

	hosts := m.hosts[invID]
	for i, h := range hosts {
		if h.Name == hostName {
			m.hosts[invID] = append(hosts[:i], hosts[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *MockAWXClient) ListHosts(invID int) ([]HostInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ListHostsCalls = append(m.ListHostsCalls, invID)

	if m.getHostErr != nil {
		return nil, m.getHostErr
	}
	return m.hosts[invID], nil
}

func varsToString(vars map[string]interface{}) string {
	if vars == nil {
		return "{}"
	}
	result := "{"
	for k, v := range vars {
		if result != "{" {
			result += ","
		}
		switch val := v.(type) {
		case string:
			result += fmt.Sprintf("\"%s\":\"%s\"", k, val)
		default:
			result += fmt.Sprintf("\"%s\":\"%v\"", k, val)
		}
	}
	return result + "}"
}

type MockK8sClient struct {
	mu  sync.Mutex
	vms []*kubernetes.VirtualMachine
	err error
}

func NewMockK8sClient() *MockK8sClient {
	return &MockK8sClient{
		vms: []*kubernetes.VirtualMachine{},
	}
}

func (m *MockK8sClient) ListVMs() ([]*kubernetes.VirtualMachine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	return m.vms, nil
}

func (m *MockK8sClient) WatchVMs(ctx context.Context, handler func(watch.Event, *unstructured.Unstructured) error) error {
	return nil
}

func (m *MockK8sClient) AddVM(vm *kubernetes.VirtualMachine) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vms = append(m.vms, vm)
}

func (m *MockK8sClient) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

// ============================================================================
// Ginkgo Tests - syncInventories
// ============================================================================

var _ = Describe("Controller syncInventories Integration", func() {
	Describe("syncInventories", func() {
		Context("no VMs in Kubernetes", func() {
			It("should handle empty state", func() {
				awxClient := NewMockAWXClient()
				k8sClient := NewMockK8sClient()
				controller := NewWithClients(awxClient, k8sClient, "Default", "")

				_, err := controller.getOrCreateInventoryForNamespace("empty-ns")
				Expect(err).NotTo(HaveOccurred())

				err = controller.syncInventories()
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("VM exists in K8s and AWX", func() {
			It("should NOT delete host that exists in both", func() {
				awxClient := NewMockAWXClient()
				k8sClient := NewMockK8sClient()
				controller := NewWithClients(awxClient, k8sClient, "Default", "")

				k8sClient.AddVM(&kubernetes.VirtualMachine{
					Name:      "active-vm",
					Namespace: "test-ns",
					IP:        "192.168.1.10",
				})

				invID, err := controller.getOrCreateInventoryForNamespace("test-ns")
				Expect(err).NotTo(HaveOccurred())

				awxClient.hosts[invID] = []HostInfo{
					{Name: "active-vm", Variables: `{"vm_namespace":"test-ns","vm_name":"active-vm"}`},
				}

				err = controller.syncInventories()
				Expect(err).NotTo(HaveOccurred())

				Expect(awxClient.deleteHostCalls).To(BeEmpty())
			})
		})

		Context("manual hosts without vm_namespace", func() {
			It("should NOT delete manual host", func() {
				awxClient := NewMockAWXClient()
				k8sClient := NewMockK8sClient()
				controller := NewWithClients(awxClient, k8sClient, "Default", "")

				k8sClient.AddVM(&kubernetes.VirtualMachine{
					Name:      "real-vm",
					Namespace: "test-ns",
					IP:        "192.168.1.1",
				})

				invID, err := controller.getOrCreateInventoryForNamespace("test-ns")
				Expect(err).NotTo(HaveOccurred())

				awxClient.hosts[invID] = []HostInfo{
					{Name: "manual-host", Variables: `{"ansible_host":"10.0.0.1"}`},
				}

				err = controller.syncInventories()
				Expect(err).NotTo(HaveOccurred())

				Expect(awxClient.deleteHostCalls).To(BeEmpty())
			})
		})

		Context("getOrCreateInventoryForNamespace", func() {
			It("should cache inventory after first creation", func() {
				awxClient := NewMockAWXClient()
				k8sClient := NewMockK8sClient()
				controller := NewWithClients(awxClient, k8sClient, "Default", "")

				invID1, err := controller.getOrCreateInventoryForNamespace("test-ns")
				Expect(err).NotTo(HaveOccurred())
				Expect(invID1).To(BeNumerically(">=", 100))

				invID2, err := controller.getOrCreateInventoryForNamespace("test-ns")
				Expect(err).NotTo(HaveOccurred())
				Expect(invID2).To(Equal(invID1))

				Expect(len(awxClient.inventories)).To(Equal(1))
			})
		})

		Context("buildInventoryName", func() {
			It("should build name with prefix", func() {
				awxClient := NewMockAWXClient()
				k8sClient := NewMockK8sClient()
				c := NewWithClients(awxClient, k8sClient, "org", "prefix")
				Expect(c.buildInventoryName("default")).To(Equal("prefix default"))
			})

			It("should build name without prefix", func() {
				awxClient := NewMockAWXClient()
				k8sClient := NewMockK8sClient()
				c := NewWithClients(awxClient, k8sClient, "org", "")
				Expect(c.buildInventoryName("default")).To(Equal("default"))
			})
		})
	})
})

// ============================================================================
// Standard Go Tests (table-driven, no Ginkgo)
// ============================================================================

func TestSyncStaleHosts(t *testing.T) {
	awx := NewMockAWXClient()
	k8s := NewMockK8sClient()
	ctrl := NewWithClients(awx, k8s, "Default", "")

	k8s.AddVM(&kubernetes.VirtualMachine{
		Name:      "active-vm",
		Namespace: "test-ns",
		IP:        "192.168.1.1",
	})

	invID, err := ctrl.getOrCreateInventoryForNamespace("test-ns")
	if err != nil {
		t.Fatalf("failed to create inventory: %v", err)
	}

	awx.hosts[invID] = []HostInfo{
		{Name: "stale-vm", Variables: `{"vm_namespace":"test-ns","vm_name":"stale-vm"}`},
	}

	err = ctrl.syncInventories()
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if len(awx.deleteHostCalls) != 1 {
		t.Errorf("expected 1 DeleteHost call, got %d", len(awx.deleteHostCalls))
	}
}

func TestSyncManualHostsNotDeleted(t *testing.T) {
	awx := NewMockAWXClient()
	k8s := NewMockK8sClient()
	ctrl := NewWithClients(awx, k8s, "Default", "")

	k8s.AddVM(&kubernetes.VirtualMachine{
		Name:      "real-vm",
		Namespace: "test-ns",
		IP:        "192.168.1.1",
	})

	invID, err := ctrl.getOrCreateInventoryForNamespace("test-ns")
	if err != nil {
		t.Fatalf("failed to create inventory: %v", err)
	}

	awx.hosts[invID] = []HostInfo{
		{Name: "manual-host", Variables: `{"ansible_host":"10.0.0.1"}`},
	}

	err = ctrl.syncInventories()
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if len(awx.deleteHostCalls) != 0 {
		t.Errorf("expected 0 DeleteHost calls, got %d", len(awx.deleteHostCalls))
	}
}

func TestSyncInventoriesBasic(t *testing.T) {
	awx := NewMockAWXClient()
	k8s := NewMockK8sClient()
	ctrl := NewWithClients(awx, k8s, "Default", "")

	k8s.AddVM(&kubernetes.VirtualMachine{
		Name:      "test-vm",
		Namespace: "default",
		IP:        "192.168.1.1",
	})

	_, err := ctrl.getOrCreateInventoryForNamespace("default")
	if err != nil {
		t.Fatalf("failed to create inventory: %v", err)
	}

	err = ctrl.syncInventories()
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if len(awx.ListHostsCalls) != 1 {
		t.Errorf("expected 1 ListHosts call, got %d", len(awx.ListHostsCalls))
	}
}
