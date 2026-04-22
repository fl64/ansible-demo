package awx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AWX Client", func() {
	Describe("WaitForAWX", func() {
		It("should succeed when AWX returns 200 with JSON", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v2/ping/" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"status": "ok"}`))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			err := client.WaitForAWX(5*time.Second, 100*time.Millisecond)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should retry and succeed after initial failures", func() {
			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				if callCount < 3 {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"status": "ok"}`))
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			err := client.WaitForAWX(5*time.Second, 100*time.Millisecond)
			Expect(err).NotTo(HaveOccurred())
			Expect(callCount).To(BeNumerically(">=", 3))
		})

		It("should timeout when AWX never becomes available", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			err := client.WaitForAWX(500*time.Millisecond, 100*time.Millisecond)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("did not become available"))
		})
	})

	Describe("GetOrganizationID", func() {
		It("should return organization ID when found", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"results": []map[string]interface{}{
						{"id": 42},
					},
				})
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			id, err := client.GetOrganizationID("TestOrg")
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal(42))
		})

		It("should return error when organization not found", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"results": []map[string]interface{}{},
				})
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			_, err := client.GetOrganizationID("NonExistent")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		It("should return error on HTTP error", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"detail": "Invalid token"}`))
			}))
			defer server.Close()

			client := NewClient(server.URL, "bad-token")
			_, err := client.GetOrganizationID("TestOrg")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("HTTP 401"))
		})
	})

	Describe("GetInventoryID", func() {
		DescribeTable("should return correct inventory ID",
			func(responseBody string, expectedID int, expectError bool) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(responseBody))
				}))
				defer server.Close()

				client := NewClient(server.URL, "test-token")
				id, err := client.GetInventoryID("test-inventory")
				if expectError {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(err).NotTo(HaveOccurred())
					Expect(id).To(Equal(expectedID))
				}
			},
			Entry("inventory found", `{"results": [{"id": 123}]}`, 123, false),
			Entry("inventory not found", `{"results": []}`, 0, false),
			Entry("invalid JSON", `{invalid}`, 0, true),
		)
	})

	Describe("CreateInventory", func() {
		It("should create inventory and return ID", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/inventories/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusCreated)
					json.NewEncoder(w).Encode(map[string]interface{}{"id": 100})
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			id, err := client.CreateInventory("new-inventory", 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal(100))
		})

		It("should return existing inventory ID on conflict", func() {
			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/inventories/") {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if r.Method == "GET" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"results": []map[string]interface{}{{"id": 50}},
					})
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			id, err := client.CreateInventory("existing-inventory", 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal(50))
			Expect(callCount).To(Equal(2)) // POST failed, then GET found existing
		})
	})

	Describe("GetHostID", func() {
		It("should return host ID when found", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"results": []map[string]interface{}{
						{"id": 200},
					},
				})
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			id, err := client.GetHostID(1, "test-host")
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal(200))
		})

		It("should return 0 when host not found", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"results": []map[string]interface{}{},
				})
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			id, err := client.GetHostID(1, "nonexistent-host")
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal(0))
		})
	})

	Describe("CreateOrUpdateHost", func() {
		It("should create new host when it does not exist", func() {
			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				if r.Method == "GET" && strings.Contains(r.URL.Path, "/hosts/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"results": []map[string]interface{}{},
					})
					return
				}
				if r.Method == "POST" && strings.Contains(r.URL.Path, "/hosts/") {
					w.WriteHeader(http.StatusCreated)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			hostVars := map[string]interface{}{"ansible_host": "192.168.1.1"}
			err := client.CreateOrUpdateHost(1, "new-host", hostVars)
			Expect(err).NotTo(HaveOccurred())
			Expect(callCount).To(Equal(2)) // GET (not found) + POST
		})

		It("should update existing host when it exists", func() {
			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				if r.Method == "GET" && strings.Contains(r.URL.Path, "/hosts/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"results": []map[string]interface{}{{"id": 50}},
					})
					return
				}
				if r.Method == "PATCH" {
					w.WriteHeader(http.StatusOK)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			hostVars := map[string]interface{}{"ansible_host": "192.168.1.2"}
			err := client.CreateOrUpdateHost(1, "existing-host", hostVars)
			Expect(err).NotTo(HaveOccurred())
			Expect(callCount).To(Equal(2)) // GET (found) + PATCH
		})
	})

	Describe("DeleteHost", func() {
		It("should delete existing host", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"results": []map[string]interface{}{{"id": 100}},
					})
					return
				}
				if r.Method == "DELETE" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			err := client.DeleteHost(1, "host-to-delete")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not error when host not found", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"results": []map[string]interface{}{},
					})
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			err := client.DeleteHost(1, "nonexistent-host")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("ListHosts", func() {
		It("should return all hosts from paginated response", func() {
			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)

				if callCount == 1 {
					// First page
					json.NewEncoder(w).Encode(map[string]interface{}{
						"results": []map[string]interface{}{
							{"id": 1, "name": "host-1", "variables": ""},
							{"id": 2, "name": "host-2", "variables": ""},
						},
						"next": "/api/v2/inventories/1/hosts/?page=2",
					})
				} else {
					// Second page
					json.NewEncoder(w).Encode(map[string]interface{}{
						"results": []map[string]interface{}{
							{"id": 3, "name": "host-3", "variables": ""},
						},
						"next": "",
					})
				}
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			hosts, err := client.ListHosts(1)
			Expect(err).NotTo(HaveOccurred())
			Expect(hosts).To(HaveLen(3))
			Expect(hosts[0].Name).To(Equal("host-1"))
			Expect(hosts[2].Name).To(Equal("host-3"))
			Expect(callCount).To(Equal(2))
		})

		It("should return empty list when no hosts", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"results": []map[string]interface{}{},
					"next":    "",
				})
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			hosts, err := client.ListHosts(1)
			Expect(err).NotTo(HaveOccurred())
			Expect(hosts).To(BeEmpty())
		})
	})

	Describe("GetOrCreateGroup", func() {
		It("should return existing group ID", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"results": []map[string]interface{}{
						{"id": 75},
					},
				})
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			id, err := client.GetOrCreateGroup(1, "existing-group")
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal(75))
		})

		It("should create group when not exists", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"results": []map[string]interface{}{},
					})
					return
				}
				if r.Method == "POST" {
					w.WriteHeader(http.StatusCreated)
					json.NewEncoder(w).Encode(map[string]interface{}{"id": 80})
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			id, err := client.GetOrCreateGroup(1, "new-group")
			Expect(err).NotTo(HaveOccurred())
			Expect(id).To(Equal(80))
		})
	})

	Describe("AddHostToGroup", func() {
		It("should not re-add host already in group", func() {
			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				if r.Method == "GET" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"results": []map[string]interface{}{
							{"id": 10}, // Host already in group
						},
					})
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			err := client.AddHostToGroup(1, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(callCount).To(Equal(1)) // Only GET, no POST
		})

		It("should add host to group", func() {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"results": []map[string]interface{}{},
					})
					return
				}
				if r.Method == "POST" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-token")
			err := client.AddHostToGroup(1, 20)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

func ExampleNewClient() {
	// Example demonstrating client creation
	client := NewClient("https://awx.example.com", "secret-token")
	fmt.Printf("Client created with base URL: %s\n", client.baseURL)
	// Output: Client created with base URL: https://awx.example.com
}
