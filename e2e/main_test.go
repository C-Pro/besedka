//go:build e2e

package e2e

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var (
	serverBinPath string
)

func TestMain(m *testing.M) {
	// 1. Build the binary
	var err error
	serverBinPath, err = buildServer()
	if err != nil {
		log.Fatalf("Failed to build server: %v", err)
	}
	defer func() { _ = os.Remove(serverBinPath) }()

	// 2. Run tests
	code := m.Run()

	os.Exit(code)
}

func buildServer() (string, error) {
	serverBinPath = filepath.Join(os.TempDir(), "besedka-e2e")

	// Build from the project root
	cmd := exec.Command("go", "build", "-o", serverBinPath, "../main.go")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return serverBinPath, nil
}
