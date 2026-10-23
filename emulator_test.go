package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"cloud.google.com/go/datastore"
)

const testProject = "webnote-test"

// emulatorHost is the Datastore emulator's host:port, or "" if it couldn't be
// started.
var emulatorHost string

// TestMain starts the Datastore emulator for the tests that need one, unless
// DATASTORE_EMULATOR_HOST points at one already.
func TestMain(m *testing.M) {
	os.Exit(runWithEmulator(m))
}

func runWithEmulator(m *testing.M) int {
	emulatorHost = os.Getenv("DATASTORE_EMULATOR_HOST")
	if emulatorHost == "" {
		cmd, host, err := startEmulator()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Datastore emulator not started:", err)
		} else {
			// Kill the whole process group: gcloud starts a Java child.
			defer syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			emulatorHost = host
			os.Setenv("DATASTORE_EMULATOR_HOST", host)
		}
	}
	return m.Run()
}

func startEmulator() (*exec.Cmd, string, error) {
	if _, err := exec.LookPath("gcloud"); err != nil {
		return nil, "", err
	}
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, "", err
	}
	host := l.Addr().String()
	l.Close()
	cmd := exec.Command("gcloud", "beta", "emulators", "datastore", "start",
		"--no-store-on-disk", "--use-firestore-in-datastore-mode",
		"--host-port="+host, "--project="+testProject, "--quiet")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, "", err
	}
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if resp, err := http.Get("http://" + host + "/"); err == nil {
			resp.Body.Close()
			return cmd, host, nil
		}
	}
	syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	return nil, "", fmt.Errorf("emulator didn't start on %s", host)
}

// newTestClient returns a client for an empty emulator datastore.
func newTestClient(t *testing.T) (context.Context, *datastore.Client) {
	t.Helper()
	if emulatorHost == "" {
		t.Skip("needs the Datastore emulator")
	}
	resp, err := http.Post("http://"+emulatorHost+"/reset", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	ctx := context.Background()
	client, err := datastore.NewClient(ctx, testProject)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return ctx, client
}

// emulatorAPI calls the emulator's Cloud Datastore REST API, which can do
// things the Go client can't: write meaning 22, and show the raw format.
func emulatorAPI(t *testing.T, method string, request, response any) {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	url := fmt.Sprintf("http://%s/v1/projects/%s:%s", emulatorHost, testProject, method)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	buf.ReadFrom(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: %s: %s", method, resp.Status, buf.String())
	}
	if response != nil {
		if err := json.Unmarshal(buf.Bytes(), response); err != nil {
			t.Fatal(err)
		}
	}
}

// restKey is a key in the REST API's JSON.
func restKey(kind string, name string) map[string]any {
	element := map[string]any{"kind": kind}
	if name != "" {
		element["name"] = name
	}
	return map[string]any{
		"partitionId": map[string]any{"projectId": testProject},
		"path":        []any{element},
	}
}

// putRawNotes puts a Notes entity with the given blob, optionally marked with
// meaning 22 the way ndb marks a compressed JsonProperty.
func putRawNotes(t *testing.T, w *Workspace, at time.Time, blob []byte, meaning22 bool) {
	t.Helper()
	value := map[string]any{"blobValue": blob, "excludeFromIndexes": true}
	if meaning22 {
		value["meaning"] = 22
	}
	emulatorAPI(t, "commit", map[string]any{
		"mode": "NON_TRANSACTIONAL",
		"mutations": []any{map[string]any{"insert": map[string]any{
			"key": restKey("Notes", ""),
			"properties": map[string]any{
				"workspaceKey":   map[string]any{"keyValue": restKey("Workspace", w.Key.Name)},
				"time":           map[string]any{"timestampValue": at.Format(time.RFC3339Nano)},
				"notesJsonArray": value,
			},
		}}},
	}, nil)
}
