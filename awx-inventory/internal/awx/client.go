package awx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client handles communication with AWX API
type Client struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewClient creates a new AWX client
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// WaitForAWX waits for AWX to become available
func (c *Client) WaitForAWX(timeout, interval time.Duration) error {
	deadline := time.Now().Add(timeout)
	attempt := 0

	for time.Now().Before(deadline) {
		attempt++
		req, err := http.NewRequest("GET", c.baseURL+"/api/v2/ping/", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)

		resp, err := c.client.Do(req)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}

		time.Sleep(interval)
	}

	return fmt.Errorf("AWX did not become available within %v", timeout)
}

// GetOrganizationID retrieves organization ID by name
func (c *Client) GetOrganizationID(name string) (int, error) {
	urlStr := c.baseURL + "/api/v2/organizations/?name=" + url.QueryEscape(name)
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("failed to get organization: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Results []struct {
			ID int `json:"id"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	if len(result.Results) == 0 {
		return 0, fmt.Errorf("organization '%s' not found", name)
	}

	return result.Results[0].ID, nil
}

// GetInventoryID retrieves inventory ID by name
func (c *Client) GetInventoryID(name string) (int, error) {
	urlStr := c.baseURL + "/api/v2/inventories/?name=" + url.QueryEscape(name)
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("failed to get inventory: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Results []struct {
			ID int `json:"id"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	if len(result.Results) == 0 {
		return 0, nil
	}

	return result.Results[0].ID, nil
}

// CreateInventory creates a new inventory
func (c *Client) CreateInventory(name string, orgID int) (int, error) {
	payload := map[string]interface{}{
		"name":         name,
		"organization": orgID,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	urlStr := c.baseURL + "/api/v2/inventories/"
	req, err := http.NewRequest("POST", urlStr, bytes.NewBuffer(jsonData))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 201 {
		var result struct {
			ID int `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return 0, err
		}
		return result.ID, nil
	} else if resp.StatusCode == 400 {
		// Inventory might already exist, try to get it
		return c.GetInventoryID(name)
	}

	body, _ := io.ReadAll(resp.Body)
	return 0, fmt.Errorf("failed to create inventory: HTTP %d, body: %s", resp.StatusCode, string(body))
}

// GetHostID retrieves host ID by name in inventory
func (c *Client) GetHostID(invID int, hostName string) (int, error) {
	urlStr := fmt.Sprintf("%s/api/v2/inventories/%d/hosts/?name=%s", c.baseURL, invID, url.QueryEscape(hostName))
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0, nil
	}

	var result struct {
		Results []struct {
			ID int `json:"id"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	if len(result.Results) == 0 {
		return 0, nil
	}

	return result.Results[0].ID, nil
}

// GetOrCreateGroup gets or creates a group in inventory
func (c *Client) GetOrCreateGroup(invID int, groupName string) (int, error) {
	// Try to get existing group
	urlStr := fmt.Sprintf("%s/api/v2/inventories/%d/groups/?name=%s", c.baseURL, invID, url.QueryEscape(groupName))
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		var result struct {
			Results []struct {
				ID int `json:"id"`
			} `json:"results"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return 0, err
		}

		if len(result.Results) > 0 {
			return result.Results[0].ID, nil
		}
	}

	// Create group
	payload := map[string]interface{}{
		"name": groupName,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	urlStr = fmt.Sprintf("%s/api/v2/inventories/%d/groups/", c.baseURL, invID)
	req, err = http.NewRequest("POST", urlStr, bytes.NewBuffer(jsonData))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err = c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 201 {
		var result struct {
			ID int `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return 0, err
		}
		return result.ID, nil
	}

	body, _ := io.ReadAll(resp.Body)
	return 0, fmt.Errorf("failed to create group: HTTP %d, body: %s", resp.StatusCode, string(body))
}

// AddHostToGroup adds a host to a group
func (c *Client) AddHostToGroup(groupID, hostID int) error {
	// Check if host is already in group
	urlStr := fmt.Sprintf("%s/api/v2/groups/%d/hosts/", c.baseURL, groupID)
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		var result struct {
			Results []struct {
				ID int `json:"id"`
			} `json:"results"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return err
		}

		for _, host := range result.Results {
			if host.ID == hostID {
				// Host already in group
				return nil
			}
		}
	}

	// Add host to group
	payload := map[string]int{
		"id": hostID,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err = http.NewRequest("POST", urlStr, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err = c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 201 || resp.StatusCode == 204 {
		return nil
	} else if resp.StatusCode == 400 {
		// Host might already be in group (race condition)
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("failed to add host to group: HTTP %d, body: %s", resp.StatusCode, string(body))
}

// CreateOrUpdateHost creates or updates a host in inventory
func (c *Client) CreateOrUpdateHost(invID int, hostName string, hostVars map[string]interface{}) error {
	hostID, _ := c.GetHostID(invID, hostName)

	// Convert hostVars to JSON string
	varsJSON, err := json.Marshal(hostVars)
	if err != nil {
		return err
	}

	if hostID > 0 {
		// Update existing host
		payload := map[string]interface{}{
			"name":      hostName,
			"variables": string(varsJSON),
		}

		jsonData, err := json.Marshal(payload)
		if err != nil {
			return err
		}

		urlStr := fmt.Sprintf("%s/api/v2/hosts/%d/", c.baseURL, hostID)
		req, err := http.NewRequest("PATCH", urlStr, bytes.NewBuffer(jsonData))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("failed to update host: HTTP %d, body: %s", resp.StatusCode, string(body))
		}

		return nil
	}

	// Create new host
	payload := map[string]interface{}{
		"name":      hostName,
		"inventory": invID,
		"variables": string(varsJSON),
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	urlStr := fmt.Sprintf("%s/api/v2/inventories/%d/hosts/", c.baseURL, invID)
	req, err := http.NewRequest("POST", urlStr, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to create host: HTTP %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// DeleteHost deletes a host from inventory
func (c *Client) DeleteHost(invID int, hostName string) error {
	hostID, err := c.GetHostID(invID, hostName)
	if err != nil || hostID == 0 {
		return nil // Host not found, nothing to delete
	}

	urlStr := fmt.Sprintf("%s/api/v2/hosts/%d/", c.baseURL, hostID)
	req, err := http.NewRequest("DELETE", urlStr, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 204 && resp.StatusCode != 404 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to delete host: HTTP %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}
