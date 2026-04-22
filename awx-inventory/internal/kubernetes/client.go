package kubernetes

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
)

// Client handles communication with Kubernetes API
type Client struct {
	client    dynamic.Interface
	namespace string
}

// NewClient creates a new Kubernetes client
func NewClient(namespace string) (*Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	// Set default config
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return &Client{
		client:    client,
		namespace: namespace,
	}, nil
}

// VirtualMachine represents a VirtualMachine resource
type VirtualMachine struct {
	Name      string
	Namespace string
	IP        string
	Labels    map[string]string
}

// GetVMIP retrieves IP address from VirtualMachine status
func (k *Client) GetVMIP(namespace, name string) (string, error) {
	gvr := schema.GroupVersionResource{
		Group:    "virtualization.deckhouse.io",
		Version:  "v1alpha2",
		Resource: "virtualmachines",
	}

	var obj *unstructured.Unstructured
	var err error

	if k.namespace != "" {
		obj, err = k.client.Resource(gvr).Namespace(k.namespace).Get(context.TODO(), name, metav1.GetOptions{})
	} else {
		obj, err = k.client.Resource(gvr).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	}

	if err != nil {
		return "", err
	}

	ip, found, err := unstructured.NestedString(obj.Object, "status", "ipAddress")
	if err != nil || !found {
		return "", nil
	}

	return ip, nil
}

// GetVM retrieves VirtualMachine resource
func (k *Client) GetVM(namespace, name string) (*VirtualMachine, error) {
	gvr := schema.GroupVersionResource{
		Group:    "virtualization.deckhouse.io",
		Version:  "v1alpha2",
		Resource: "virtualmachines",
	}

	var obj *unstructured.Unstructured
	var err error

	if k.namespace != "" {
		obj, err = k.client.Resource(gvr).Namespace(k.namespace).Get(context.TODO(), name, metav1.GetOptions{})
	} else {
		obj, err = k.client.Resource(gvr).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	}

	if err != nil {
		return nil, err
	}

	vm := &VirtualMachine{
		Name:      name,
		Namespace: namespace,
	}

	// Get IP
	ip, found, _ := unstructured.NestedString(obj.Object, "status", "ipAddress")
	if found {
		vm.IP = ip
	}

	// Get labels
	labels, found, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")
	if found {
		vm.Labels = labels
	} else {
		vm.Labels = make(map[string]string)
	}

	return vm, nil
}

// UnstructuredToVM converts unstructured.Unstructured to VirtualMachine
func UnstructuredToVM(obj *unstructured.Unstructured) *VirtualMachine {
	namespace, found, _ := unstructured.NestedString(obj.Object, "metadata", "namespace")
	if !found {
		namespace = ""
	}

	name, found, _ := unstructured.NestedString(obj.Object, "metadata", "name")
	if !found {
		name = ""
	}

	vm := &VirtualMachine{
		Name:      name,
		Namespace: namespace,
	}

	// Get IP
	ip, found, _ := unstructured.NestedString(obj.Object, "status", "ipAddress")
	if found {
		vm.IP = ip
	}

	// Get labels
	labels, found, _ := unstructured.NestedStringMap(obj.Object, "metadata", "labels")
	if found {
		vm.Labels = labels
	} else {
		vm.Labels = make(map[string]string)
	}

	return vm
}

// ListVMs lists all VirtualMachine resources
func (k *Client) ListVMs() ([]*VirtualMachine, error) {
	gvr := schema.GroupVersionResource{
		Group:    "virtualization.deckhouse.io",
		Version:  "v1alpha2",
		Resource: "virtualmachines",
	}

	var list *unstructured.UnstructuredList
	var err error

	if k.namespace != "" {
		list, err = k.client.Resource(gvr).Namespace(k.namespace).List(context.TODO(), metav1.ListOptions{})
	} else {
		list, err = k.client.Resource(gvr).List(context.TODO(), metav1.ListOptions{})
	}

	if err != nil {
		return nil, err
	}

	var vms []*VirtualMachine
	for _, item := range list.Items {
		namespace, found, _ := unstructured.NestedString(item.Object, "metadata", "namespace")
		if !found {
			continue
		}

		name, found, _ := unstructured.NestedString(item.Object, "metadata", "name")
		if !found {
			continue
		}

		vm := &VirtualMachine{
			Name:      name,
			Namespace: namespace,
		}

		// Get IP
		ip, found, _ := unstructured.NestedString(item.Object, "status", "ipAddress")
		if found {
			vm.IP = ip
		}

		// Get labels
		labels, found, _ := unstructured.NestedStringMap(item.Object, "metadata", "labels")
		if found {
			vm.Labels = labels
		} else {
			vm.Labels = make(map[string]string)
		}

		vms = append(vms, vm)
	}

	return vms, nil
}

// WatchVMs watches for VirtualMachine resource changes
func (k *Client) WatchVMs(ctx context.Context, handler func(watch.Event, *unstructured.Unstructured) error) error {
	gvr := schema.GroupVersionResource{
		Group:    "virtualization.deckhouse.io",
		Version:  "v1alpha2",
		Resource: "virtualmachines",
	}

	var watcher watch.Interface
	var err error

	if k.namespace != "" {
		watcher, err = k.client.Resource(gvr).Namespace(k.namespace).Watch(ctx, metav1.ListOptions{})
	} else {
		watcher, err = k.client.Resource(gvr).Watch(ctx, metav1.ListOptions{})
	}

	if err != nil {
		return fmt.Errorf("failed to start watch: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, ok := <-watcher.ResultChan():
			if !ok {
				// Channel closed, restart watch
				time.Sleep(5 * time.Second)
				return k.WatchVMs(ctx, handler)
			}

			obj, ok := event.Object.(*unstructured.Unstructured)
			if !ok {
				continue
			}

			if err := handler(event, obj); err != nil {
				return err
			}
		}
	}
}
