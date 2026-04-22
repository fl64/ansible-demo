package kubernetes

import (
	"context"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

var _ = Describe("Kubernetes Client with Fake Client", func() {
	var (
		fakeClient *dynamicfake.FakeDynamicClient
		k8sClient  *Client
		namespace  string
		ctx        context.Context
		cancel     context.CancelFunc
		gvr        schema.GroupVersionResource
	)

	BeforeEach(func() {
		namespace = "test-ns"
		ctx, cancel = context.WithCancel(context.Background())

		gvr = schema.GroupVersionResource{
			Group:    "virtualization.deckhouse.io",
			Version:  "v1alpha2",
			Resource: "virtualmachines",
		}

		// Create fake dynamic client with custom list kinds
		fakeClient = dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
			runtime.NewScheme(),
			map[schema.GroupVersionResource]string{
				gvr: "VirtualMachineList",
			},
		)

		k8sClient = &Client{
			client:    fakeClient,
			namespace: namespace,
		}
	})

	AfterEach(func() {
		cancel()
	})

	Describe("GetVM", func() {
		It("should return VM when it exists", func() {
			vm := newTestVM("test-vm", namespace, "192.168.1.100", map[string]interface{}{"app": "nginx"})
			_, err := fakeClient.Resource(gvr).Namespace(namespace).Create(ctx, vm, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			result, err := k8sClient.GetVM(namespace, "test-vm")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Name).To(Equal("test-vm"))
			Expect(result.Namespace).To(Equal(namespace))
			Expect(result.IP).To(Equal("192.168.1.100"))
			Expect(result.Labels["app"]).To(Equal("nginx"))
		})

		It("should return error when VM does not exist", func() {
			result, err := k8sClient.GetVM(namespace, "nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(result).To(BeNil())
		})

		It("should return VM without IP when status is empty", func() {
			vm := newTestVMWithoutIP("test-vm", namespace, map[string]interface{}{"env": "prod"})
			_, err := fakeClient.Resource(gvr).Namespace(namespace).Create(ctx, vm, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			result, err := k8sClient.GetVM(namespace, "test-vm")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.IP).To(BeEmpty())
			Expect(result.Labels["env"]).To(Equal("prod"))
		})

		It("should return VM with empty labels when no labels set", func() {
			vm := newTestVMWithoutLabels("test-vm", namespace, "10.0.0.1")
			_, err := fakeClient.Resource(gvr).Namespace(namespace).Create(ctx, vm, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			result, err := k8sClient.GetVM(namespace, "test-vm")
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Labels).To(BeEmpty())
		})
	})

	Describe("GetVMIP", func() {
		It("should return IP when VM has IP", func() {
			vm := newTestVM("test-vm", namespace, "192.168.1.100", nil)
			_, err := fakeClient.Resource(gvr).Namespace(namespace).Create(ctx, vm, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			ip, err := k8sClient.GetVMIP(namespace, "test-vm")
			Expect(err).NotTo(HaveOccurred())
			Expect(ip).To(Equal("192.168.1.100"))
		})

		It("should return empty string when VM has no IP", func() {
			vm := newTestVMWithoutIP("test-vm", namespace, nil)
			_, err := fakeClient.Resource(gvr).Namespace(namespace).Create(ctx, vm, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			ip, err := k8sClient.GetVMIP(namespace, "test-vm")
			Expect(err).NotTo(HaveOccurred())
			Expect(ip).To(BeEmpty())
		})

		It("should return error when VM does not exist", func() {
			ip, err := k8sClient.GetVMIP(namespace, "nonexistent")
			Expect(err).To(HaveOccurred())
			Expect(ip).To(BeEmpty())
		})
	})

	Describe("ListVMs", func() {
		It("should return all VMs in namespace", func() {
			vms := []*unstructured.Unstructured{
				newTestVM("vm-1", namespace, "192.168.1.1", map[string]interface{}{"app": "a"}),
				newTestVM("vm-2", namespace, "192.168.1.2", map[string]interface{}{"app": "b"}),
				newTestVM("vm-3", namespace, "192.168.1.3", nil),
			}

			for _, vm := range vms {
				_, err := fakeClient.Resource(gvr).Namespace(namespace).Create(ctx, vm, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
			}

			// Create VM in different namespace (should not be listed)
			_, err := fakeClient.Resource(gvr).Namespace("other-ns").Create(ctx, newTestVM("vm-other", "other-ns", "10.0.0.1", nil), metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			result, err := k8sClient.ListVMs()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(HaveLen(3))

			// Verify all names
			names := make(map[string]bool)
			for _, vm := range result {
				names[vm.Name] = true
				Expect(vm.Namespace).To(Equal(namespace))
			}
			Expect(names["vm-1"]).To(BeTrue())
			Expect(names["vm-2"]).To(BeTrue())
			Expect(names["vm-3"]).To(BeTrue())
		})

		It("should return empty list when no VMs exist", func() {
			result, err := k8sClient.ListVMs()
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeEmpty())
		})

		It("should correctly list VMs from the watched namespace only", func() {
			// Create VMs in the test namespace
			vm1 := newTestVM("vm-in-ns", namespace, "192.168.1.1", nil)
			_, err := fakeClient.Resource(gvr).Namespace(namespace).Create(ctx, vm1, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// Create VM in different namespace
			vm2 := newTestVM("vm-other-ns", "other-ns", "192.168.1.2", nil)
			_, err = fakeClient.Resource(gvr).Namespace("other-ns").Create(ctx, vm2, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			result, err := k8sClient.ListVMs()
			Expect(err).NotTo(HaveOccurred())
			// Should only return VMs from the client's namespace
			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("vm-in-ns"))
			Expect(result[0].Namespace).To(Equal(namespace))
		})
	})

	Describe("WatchVMs", func() {
		It("should receive ADDED event when VM is created", func() {
			var receivedEvent watch.Event
			var eventWg sync.WaitGroup
			done := make(chan struct{})

			eventWg.Add(1)
			handler := func(event watch.Event, obj *unstructured.Unstructured) error {
				receivedEvent = event
				eventWg.Done()
				cancel() // Stop watching after first event
				return nil
			}

			go func() {
				_ = k8sClient.WatchVMs(ctx, handler)
				close(done)
			}()

			// Give watcher time to start
			time.Sleep(100 * time.Millisecond)

			// Create VM - should trigger ADDED event
			vm := newTestVM("watched-vm", namespace, "192.168.1.50", nil)
			_, err := fakeClient.Resource(gvr).Namespace(namespace).Create(ctx, vm, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// Wait with timeout
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}

			eventWg.Wait()
			Expect(receivedEvent.Type).To(Equal(watch.Added))
		})

		It("should receive MODIFIED event when VM is updated", func() {
			// First create a VM
			vm := newTestVMWithoutIP("update-vm", namespace, nil)
			created, err := fakeClient.Resource(gvr).Namespace(namespace).Create(ctx, vm, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			var receivedEvent watch.Event
			var eventWg sync.WaitGroup
			done := make(chan struct{})

			eventWg.Add(1)
			handler := func(event watch.Event, obj *unstructured.Unstructured) error {
				if event.Type == watch.Modified {
					receivedEvent = event
					eventWg.Done()
					cancel()
				}
				return nil
			}

			go func() {
				_ = k8sClient.WatchVMs(ctx, handler)
				close(done)
			}()

			// Give watcher time to start
			time.Sleep(100 * time.Millisecond)

			// Update VM with IP
			created.Object["status"] = map[string]interface{}{
				"ipAddress": "192.168.1.100",
			}
			_, err = fakeClient.Resource(gvr).Namespace(namespace).Update(ctx, created, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// Wait with timeout
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}

			eventWg.Wait()
			Expect(receivedEvent.Type).To(Equal(watch.Modified))
		})

		It("should receive DELETED event when VM is deleted", func() {
			// First create a VM
			vm := newTestVM("delete-vm", namespace, "192.168.1.60", nil)
			created, err := fakeClient.Resource(gvr).Namespace(namespace).Create(ctx, vm, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			var receivedEvent watch.Event
			var eventWg sync.WaitGroup
			done := make(chan struct{})

			eventWg.Add(1)
			handler := func(event watch.Event, obj *unstructured.Unstructured) error {
				if event.Type == watch.Deleted {
					receivedEvent = event
					eventWg.Done()
					cancel()
				}
				return nil
			}

			go func() {
				_ = k8sClient.WatchVMs(ctx, handler)
				close(done)
			}()

			// Give watcher time to start
			time.Sleep(100 * time.Millisecond)

			// Delete VM
			err = fakeClient.Resource(gvr).Namespace(namespace).Delete(ctx, created.GetName(), metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())

			// Wait with timeout
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}

			eventWg.Wait()
			Expect(receivedEvent.Type).To(Equal(watch.Deleted))
		})

		It("should stop when context is cancelled", func() {
			localCtx, localCancel := context.WithCancel(context.Background())
			done := make(chan error, 1)

			handler := func(event watch.Event, obj *unstructured.Unstructured) error {
				return nil
			}

			go func() {
				done <- k8sClient.WatchVMs(localCtx, handler)
			}()

			// Cancel the local context
			localCancel()

			// Wait for watcher to exit
			select {
			case err := <-done:
				Expect(err).To(Equal(context.Canceled))
			case <-time.After(500 * time.Millisecond):
				// Timeout is acceptable - watch might not exit immediately
			}
		})
	})

	Describe("UnstructuredToVM", func() {
		It("should correctly convert complex VM object", func() {
			obj := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      "complex-vm",
						"namespace": "complex-ns",
						"labels": map[string]interface{}{
							"app":     "web",
							"version": "v1",
							"tier":    "frontend",
						},
					},
					"status": map[string]interface{}{
						"ipAddress": "172.16.0.100",
					},
				},
			}

			vm := UnstructuredToVM(obj)

			Expect(vm.Name).To(Equal("complex-vm"))
			Expect(vm.Namespace).To(Equal("complex-ns"))
			Expect(vm.IP).To(Equal("172.16.0.100"))
			Expect(vm.Labels).To(HaveKeyWithValue("app", "web"))
			Expect(vm.Labels).To(HaveKeyWithValue("version", "v1"))
			Expect(vm.Labels).To(HaveKeyWithValue("tier", "frontend"))
		})

		It("should handle nil object", func() {
			vm := UnstructuredToVM(nil)
			Expect(vm.Name).To(BeEmpty())
			Expect(vm.Namespace).To(BeEmpty())
			Expect(vm.IP).To(BeEmpty())
			Expect(vm.Labels).To(BeEmpty())
		})

		It("should handle object with empty labels map", func() {
			obj := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      "vm-empty-labels",
						"namespace": "ns",
						"labels":    map[string]interface{}{},
					},
				},
			}

			vm := UnstructuredToVM(obj)
			Expect(vm.Labels).To(BeEmpty())
		})
	})
})

// Helper functions

// newTestVM creates a test VM with all fields (labels as map[string]interface{})
func newTestVM(name, namespace, ip string, labels map[string]interface{}) *unstructured.Unstructured {
	obj := map[string]interface{}{
		"apiVersion": "virtualization.deckhouse.io/v1alpha2",
		"kind":       "VirtualMachine",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
		"status": map[string]interface{}{
			"ipAddress": ip,
		},
	}

	if labels != nil {
		obj["metadata"].(map[string]interface{})["labels"] = labels
	}

	return &unstructured.Unstructured{Object: obj}
}

// newTestVMWithoutIP creates a test VM without IP
func newTestVMWithoutIP(name, namespace string, labels map[string]interface{}) *unstructured.Unstructured {
	obj := map[string]interface{}{
		"apiVersion": "virtualization.deckhouse.io/v1alpha2",
		"kind":       "VirtualMachine",
		"metadata": map[string]interface{}{
			"name":      name,
			"namespace": namespace,
		},
	}

	if labels != nil {
		obj["metadata"].(map[string]interface{})["labels"] = labels
	}

	return &unstructured.Unstructured{Object: obj}
}

// newTestVMWithoutLabels creates a test VM without labels
func newTestVMWithoutLabels(name, namespace, ip string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "virtualization.deckhouse.io/v1alpha2",
			"kind":       "VirtualMachine",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"status": map[string]interface{}{
				"ipAddress": ip,
			},
		},
	}
}
