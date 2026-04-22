package controller

import (
	"time"

	"github.com/fl64/ansible-demo/awx-inventory/internal/awx"
)

// AWXClientAdapter wraps awx.Client to implement AWXClientInterface
type AWXClientAdapter struct {
	client *awx.Client
}

// NewAWXClientAdapter creates a new adapter wrapping awx.Client
func NewAWXClientAdapter(client *awx.Client) *AWXClientAdapter {
	return &AWXClientAdapter{client: client}
}

func (a *AWXClientAdapter) GetOrganizationID(name string) (int, error) {
	return a.client.GetOrganizationID(name)
}

func (a *AWXClientAdapter) GetInventoryID(name string) (int, error) {
	return a.client.GetInventoryID(name)
}

func (a *AWXClientAdapter) CreateInventory(name string, orgID int) (int, error) {
	return a.client.CreateInventory(name, orgID)
}

func (a *AWXClientAdapter) CreateOrUpdateHost(invID int, hostName string, hostVars map[string]interface{}) error {
	return a.client.CreateOrUpdateHost(invID, hostName, hostVars)
}

func (a *AWXClientAdapter) DeleteHost(invID int, hostName string) error {
	return a.client.DeleteHost(invID, hostName)
}

func (a *AWXClientAdapter) ListHosts(invID int) ([]HostInfo, error) {
	hosts, err := a.client.ListHosts(invID)
	if err != nil {
		return nil, err
	}

	result := make([]HostInfo, len(hosts))
	for i, h := range hosts {
		result[i] = HostInfo{
			Name:      h.Name,
			Variables: h.Variables,
		}
	}
	return result, nil
}

func (a *AWXClientAdapter) WaitForAWX(timeout, interval time.Duration) error {
	return a.client.WaitForAWX(timeout, interval)
}
