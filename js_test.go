package main

import (
	"os/exec"
	"testing"
)

// TestClientJavaScript runs the client's unit tests (client_test.js).
func TestClientJavaScript(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("needs node")
	}
	out, err := exec.Command("node", "--test", "--test-reporter=dot", "client_test.js").CombinedOutput()
	if err != nil {
		t.Fatalf("node --test client_test.js: %v\n%s", err, out)
	}
}
