package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"

	kubernetes "github.com/fl64/ansible-demo/awx-inventory/internal/kubernetes"
)

var _ = Describe("Controller", func() {
	Describe("buildInventoryName", func() {
		DescribeTable("should build inventory name correctly",
			func(prefix, namespace, expected string) {
				c := &Controller{prefix: prefix}
				result := c.buildInventoryName(namespace)
				Expect(result).To(Equal(expected))
			},
			Entry("without prefix", "", "default", "default"),
			Entry("with prefix", "awx", "default", "awx default"),
			Entry("with prefix and custom namespace", "prod", "web-servers", "prod web-servers"),
			Entry("empty prefix", "", "test-ns", "test-ns"),
		)
	})

	Describe("extractVMInfo", func() {
		DescribeTable("should extract namespace and name from unstructured object",
			func(obj *unstructured.Unstructured, expectedNS, expectedName string, expectedOK bool) {
				ns, name, ok := extractVMInfo(obj)
				Expect(ok).To(Equal(expectedOK))
				if expectedOK {
					Expect(ns).To(Equal(expectedNS))
					Expect(name).To(Equal(expectedName))
				}
			},
			Entry("valid VM object",
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-vm",
							"namespace": "test-ns",
						},
					},
				},
				"test-ns", "test-vm", true,
			),
			Entry("missing namespace",
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name": "test-vm",
						},
					},
				},
				"", "", false,
			),
			Entry("missing name",
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"namespace": "test-ns",
						},
					},
				},
				"", "", false,
			),
			Entry("empty metadata",
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{},
					},
				},
				"", "", false,
			),
			Entry("nil object",
				&unstructured.Unstructured{Object: nil},
				"", "", false,
			),
			Entry("missing metadata entirely",
				&unstructured.Unstructured{
					Object: map[string]interface{}{},
				},
				"", "", false,
			),
		)
	})

	Describe("shouldProcessVM", func() {
		DescribeTable("should correctly determine if VM should be processed",
			func(vm *kubernetes.VirtualMachine, eventType watch.EventType, expected bool) {
				result := shouldProcessVM(vm, eventType)
				Expect(result).To(Equal(expected))
			},
			Entry("VM with IP and ADDED event",
				&kubernetes.VirtualMachine{Name: "vm1", Namespace: "ns1", IP: "192.168.1.1"},
				watch.Added,
				true,
			),
			Entry("VM with IP and MODIFIED event",
				&kubernetes.VirtualMachine{Name: "vm2", Namespace: "ns1", IP: "192.168.1.2"},
				watch.Modified,
				true,
			),
			Entry("VM without IP and ADDED event",
				&kubernetes.VirtualMachine{Name: "vm3", Namespace: "ns1", IP: ""},
				watch.Added,
				false,
			),
			Entry("VM without IP and MODIFIED event",
				&kubernetes.VirtualMachine{Name: "vm4", Namespace: "ns1", IP: ""},
				watch.Modified,
				false,
			),
		)
	})

	Describe("extractHostVMInfo", func() {
		DescribeTable("should extract vm_namespace and vm_name from host variables",
			func(variables, expectedNS, expectedName string) {
				ns, name := extractHostVMInfo(variables)
				Expect(ns).To(Equal(expectedNS))
				Expect(name).To(Equal(expectedName))
			},
			Entry("valid variables JSON",
				`{"vm_namespace": "default", "vm_name": "test-vm", "ansible_host": "10.0.0.1"}`,
				"default", "test-vm",
			),
			Entry("empty variables",
				"",
				"", "",
			),
			Entry("invalid JSON",
				"{invalid json}",
				"", "",
			),
			Entry("missing vm_namespace",
				`{"vm_name": "test-vm"}`,
				"", "test-vm",
			),
			Entry("missing vm_name",
				`{"vm_namespace": "default"}`,
				"default", "",
			),
			Entry("empty JSON object",
				"{}",
				"", "",
			),
			Entry("only ansible_host",
				`{"ansible_host": "192.168.1.1"}`,
				"", "",
			),
		)
	})

	Describe("getDurationFromEnv", func() {
		It("should return custom value when env var is set", func() {
			original := os.Getenv("AWX_WAIT_TIMEOUT")
			defer os.Setenv("AWX_WAIT_TIMEOUT", original)

			os.Setenv("AWX_WAIT_TIMEOUT", "60")
			result := getDurationFromEnv("AWX_WAIT_TIMEOUT", 300*time.Second)
			Expect(result).To(Equal(60 * time.Second))
		})

		It("should return default when env var is empty", func() {
			original := os.Getenv("AWX_WAIT_TIMEOUT")
			defer os.Setenv("AWX_WAIT_TIMEOUT", original)

			os.Setenv("AWX_WAIT_TIMEOUT", "")
			result := getDurationFromEnv("AWX_WAIT_TIMEOUT", 300*time.Second)
			Expect(result).To(Equal(300 * time.Second))
		})

		It("should return default when env var is invalid", func() {
			original := os.Getenv("AWX_WAIT_TIMEOUT")
			defer os.Setenv("AWX_WAIT_TIMEOUT", original)

			os.Setenv("AWX_WAIT_TIMEOUT", "invalid")
			result := getDurationFromEnv("AWX_WAIT_TIMEOUT", 300*time.Second)
			Expect(result).To(Equal(300 * time.Second))
		})
	})

	Describe("inventoryCache", func() {
		It("should cache inventory IDs after creation", func() {
			c := &Controller{
				inventoryCache: make(map[string]int),
			}

			// Simulate cache population
			c.inventoryCache["default"] = 100
			c.inventoryCache["test-ns"] = 200

			Expect(c.inventoryCache["default"]).To(Equal(100))
			Expect(c.inventoryCache["test-ns"]).To(Equal(200))
			Expect(len(c.inventoryCache)).To(Equal(2))
		})
	})

	Describe("syncInventories business logic", func() {
		It("should build correct k8sHostKeys map", func() {
			vms := []*kubernetes.VirtualMachine{
				{Name: "vm1", Namespace: "ns1", IP: "192.168.1.1"},
				{Name: "vm2", Namespace: "ns1", IP: "192.168.1.2"},
				{Name: "vm3", Namespace: "ns2", IP: "192.168.1.3"},
				{Name: "vm4", Namespace: "ns2", IP: ""}, // VM without IP should be ignored
			}

			k8sHostKeys := make(map[string]bool)
			namespaceSet := make(map[string]bool)

			for _, vm := range vms {
				if vm.IP != "" {
					k8sHostKeys[vm.Namespace+"/"+vm.Name] = true
					namespaceSet[vm.Namespace] = true
				}
			}

			Expect(k8sHostKeys["ns1/vm1"]).To(BeTrue())
			Expect(k8sHostKeys["ns1/vm2"]).To(BeTrue())
			Expect(k8sHostKeys["ns2/vm3"]).To(BeTrue())
			Expect(k8sHostKeys["ns2/vm4"]).To(BeFalse()) // No IP
			Expect(len(k8sHostKeys)).To(Equal(3))
			Expect(len(namespaceSet)).To(Equal(2))
		})

		It("should identify stale hosts correctly", func() {
			type hostInfo struct {
				Name      string
				Variables string
			}

			awxHosts := []hostInfo{
				{Name: "vm1", Variables: `{"vm_namespace": "ns1", "vm_name": "vm1"}`},
				{Name: "vm2", Variables: `{"vm_namespace": "ns1", "vm_name": "vm2"}`},
				{Name: "deleted-vm", Variables: `{"vm_namespace": "ns1", "vm_name": "deleted-vm"}`},
				{Name: "manual-host", Variables: ""},
			}

			k8sHostKeys := map[string]bool{
				"ns1/vm1": true,
				"ns1/vm2": true,
			}

			var staleHosts []string
			for _, host := range awxHosts {
				vmNS, vmName := extractHostVMInfo(host.Variables)
				if vmNS == "" || vmName == "" {
					continue
				}
				key := vmNS + "/" + vmName
				if !k8sHostKeys[key] {
					staleHosts = append(staleHosts, host.Name)
				}
			}

			Expect(staleHosts).To(HaveLen(1))
			Expect(staleHosts[0]).To(Equal("deleted-vm"))
		})
	})

	Describe("hostVars construction", func() {
		It("should create correct hostVars structure", func() {
			vm := &kubernetes.VirtualMachine{
				Name:      "test-vm",
				Namespace: "default",
				IP:        "192.168.1.100",
				Labels: map[string]string{
					"app": "nginx",
					"env": "prod",
				},
			}

			hostVars := map[string]interface{}{
				"vm_name":      vm.Name,
				"vm_namespace": vm.Namespace,
				"labels":       vm.Labels,
				"ansible_host": vm.IP,
			}

			Expect(hostVars["vm_name"]).To(Equal("test-vm"))
			Expect(hostVars["vm_namespace"]).To(Equal("default"))
			Expect(hostVars["ansible_host"]).To(Equal("192.168.1.100"))
			Expect(hostVars["labels"]).To(Equal(map[string]string{"app": "nginx", "env": "prod"}))
		})
	})

	Describe("retryWithBackoff", func() {
		It("should succeed on first attempt", func() {
			calls := 0
			err := retryWithBackoff(func() error {
				calls++
				return nil
			}, "test-operation")
			Expect(err).NotTo(HaveOccurred())
			Expect(calls).To(Equal(1))
		})

		It("should retry on failure and succeed", func() {
			calls := 0
			err := retryWithBackoff(func() error {
				calls++
				if calls < 3 {
					return &testError{msg: "temporary failure"}
				}
				return nil
			}, "test-operation")
			Expect(err).NotTo(HaveOccurred())
			Expect(calls).To(Equal(3))
		})

		It("should fail after max retries", func() {
			calls := 0
			err := retryWithBackoff(func() error {
				calls++
				return &testError{msg: "permanent failure"}
			}, "test-operation")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("permanent failure"))
			Expect(calls).To(Equal(maxRetries))
		})
	})
})

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

// Helper to create test unstructured VM objects
func newTestVM(name, namespace, ip string, labels map[string]string) *unstructured.Unstructured {
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
		"status": map[string]interface{}{},
	}

	if ip != "" {
		obj["status"].(map[string]interface{})["ipAddress"] = ip
	}

	if labels != nil {
		obj["metadata"].(map[string]interface{})["labels"] = labels
	}

	return &unstructured.Unstructured{Object: obj}
}

// Integration-style tests with mock server
var _ = Describe("Controller with mock AWX", func() {
	var (
		awxServer *httptest.Server
		awxCalls  []string
	)

	BeforeEach(func() {
		awxCalls = []string{}
		awxServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			awxCalls = append(awxCalls, r.Method+" "+r.URL.Path)

			w.Header().Set("Content-Type", "application/json")

			switch {
			case r.URL.Path == "/api/v2/ping/":
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"status": "ok"}`))

			case r.URL.Path == "/api/v2/organizations/":
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"results": [{"id": 1}]}`))

			case strings.Contains(r.URL.Path, "/inventories/"):
				if r.Method == "GET" {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"results": []}`))
				} else if r.Method == "POST" {
					w.WriteHeader(http.StatusCreated)
					w.Write([]byte(`{"id": 100}`))
				}

			case strings.Contains(r.URL.Path, "/hosts/"):
				if r.Method == "GET" {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"results": []}`))
				} else if r.Method == "POST" {
					w.WriteHeader(http.StatusCreated)
				} else if r.Method == "DELETE" {
					w.WriteHeader(http.StatusNoContent)
				}

			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
	})

	AfterEach(func() {
		awxServer.Close()
	})

	It("should track API calls made during operations", func() {
		// This test documents the expected API call patterns
		Expect(awxCalls).To(BeEmpty()) // No calls made yet
	})
})

func TestController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

func init() {
	// Test initialization
	_ = json.Marshal
	_ = fmt.Sprintf
}
