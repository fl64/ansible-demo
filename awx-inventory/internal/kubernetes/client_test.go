package kubernetes

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var _ = Describe("Kubernetes Client", func() {
	Describe("UnstructuredToVM", func() {
		DescribeTable("should correctly convert unstructured object to VirtualMachine",
			func(obj *unstructured.Unstructured, expectedNS, expectedName, expectedIP string, expectedLabels map[string]string) {
				vm := UnstructuredToVM(obj)

				Expect(vm.Namespace).To(Equal(expectedNS))
				Expect(vm.Name).To(Equal(expectedName))
				Expect(vm.IP).To(Equal(expectedIP))
				Expect(vm.Labels).To(Equal(expectedLabels))
			},
			Entry("VM with all fields",
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "test-vm",
							"namespace": "default",
							"labels": map[string]interface{}{
								"app": "nginx",
								"env": "prod",
							},
						},
						"status": map[string]interface{}{
							"ipAddress": "192.168.1.100",
						},
					},
				},
				"default", "test-vm", "192.168.1.100",
				map[string]string{"app": "nginx", "env": "prod"},
			),
			Entry("VM without IP",
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "vm-no-ip",
							"namespace": "test-ns",
						},
					},
				},
				"test-ns", "vm-no-ip", "",
				map[string]string{},
			),
			Entry("VM without labels",
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name":      "vm-no-labels",
							"namespace": "default",
						},
						"status": map[string]interface{}{
							"ipAddress": "10.0.0.1",
						},
					},
				},
				"default", "vm-no-labels", "10.0.0.1",
				map[string]string{},
			),
			Entry("VM without metadata",
				&unstructured.Unstructured{
					Object: map[string]interface{}{},
				},
				"", "", "",
				map[string]string{},
			),
			Entry("VM with partial metadata",
				&unstructured.Unstructured{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{
							"name": "vm-partial",
						},
					},
				},
				"", "vm-partial", "",
				map[string]string{},
			),
		)

		It("should handle empty unstructured object", func() {
			vm := UnstructuredToVM(&unstructured.Unstructured{Object: nil})
			Expect(vm.Namespace).To(BeEmpty())
			Expect(vm.Name).To(BeEmpty())
			Expect(vm.IP).To(BeEmpty())
			Expect(vm.Labels).To(BeEmpty())
		})
	})

	Describe("VirtualMachine struct", func() {
		It("should store all fields correctly", func() {
			vm := &VirtualMachine{
				Name:      "my-vm",
				Namespace: "my-ns",
				IP:        "172.16.0.50",
				Labels: map[string]string{
					"tier": "frontend",
				},
			}

			Expect(vm.Name).To(Equal("my-vm"))
			Expect(vm.Namespace).To(Equal("my-ns"))
			Expect(vm.IP).To(Equal("172.16.0.50"))
			Expect(vm.Labels["tier"]).To(Equal("frontend"))
		})
	})
})

func TestKubernetesClient(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kubernetes Client Suite")
}
