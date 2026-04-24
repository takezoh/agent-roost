package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// fcClient talks to a single Firecracker process via its unix-socket API.
type fcClient struct {
	hc   *http.Client
	base string
}

func newFCClient(sockPath string) *fcClient {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
		},
	}
	return &fcClient{
		hc:   &http.Client{Transport: tr, Timeout: 10 * time.Second},
		base: "http://localhost",
	}
}

func (c *fcClient) put(path string, body any) error {
	return c.do(http.MethodPut, path, body)
}

func (c *fcClient) patch(path string, body any) error {
	return c.do(http.MethodPatch, path, body)
}

func (c *fcClient) do(method, path string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, c.base+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("FC API %s %s: %d %s", method, path, resp.StatusCode, string(raw))
	}
	return nil
}

// waitAPIReady polls the Firecracker API socket until it accepts connections.
// Firecracker takes a few milliseconds to create the socket after exec.
func (c *fcClient) waitAPIReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := c.hc.Get(c.base + "/")
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("FC API not ready after %s", timeout)
}

// --- request body types --------------------------------------------------

type bootSource struct {
	KernelImagePath string `json:"kernel_image_path"`
	BootArgs        string `json:"boot_args"`
}

type drive struct {
	DriveID      string `json:"drive_id"`
	PathOnHost   string `json:"path_on_host"`
	IsRootDevice bool   `json:"is_root_device"`
	IsReadOnly   bool   `json:"is_read_only"`
}

type vsockDevice struct {
	GuestCID uint32 `json:"guest_cid"`
	UDSPath  string `json:"uds_path"`
}

type machineConfig struct {
	VCPUCount  int `json:"vcpu_count"`
	MemSizeMiB int `json:"mem_size_mib"`
}

type snapshotCreate struct {
	SnapshotType string `json:"snapshot_type"`
	SnapshotPath string `json:"snapshot_path"`
	MemFilePath  string `json:"mem_file_path"`
}

type snapshotLoad struct {
	SnapshotPath string         `json:"snapshot_path"`
	MemBackend   memBackend     `json:"mem_backend"`
	ResumeVM     bool           `json:"resume_vm"`
}

type memBackend struct {
	BackendPath string `json:"backend_path"`
	BackendType string `json:"backend_type"` // "File"
}

type action struct {
	ActionType string `json:"action_type"`
}
