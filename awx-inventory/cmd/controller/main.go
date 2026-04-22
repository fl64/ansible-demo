package main

import (
	"log"
	"os"

	"github.com/fl64/ansible-demo/awx-inventory/internal/controller"
)

func main() {
	// Get configuration from environment
	awxURL := getEnv("AWX_URL", "https://awx.example.com")
	awxToken := getEnv("AWX_TOKEN", "")
	inventoryPrefix := getEnv("INVENTORY_PREFIX", "")
	orgName := getEnv("ORGANIZATION", "Default")
	namespace := getEnv("NAMESPACE", "")

	if awxToken == "" {
		log.Fatal("AWX_TOKEN environment variable is required")
	}

	// Create controller
	ctrl, err := controller.New(awxURL, awxToken, inventoryPrefix, orgName, namespace)
	if err != nil {
		log.Fatalf("Failed to create controller: %v", err)
	}

	// Start controller
	if err := ctrl.Start(); err != nil {
		log.Fatalf("Controller error: %v", err)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
