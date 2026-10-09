// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/pin/tftp/v3"
)

func TestReadHandler_ServesFile(t *testing.T) {
	dir := t.TempDir()
	want := []byte("ipxe-binary-content")
	if err := os.WriteFile(filepath.Join(dir, "ipxe.efi"), want, 0600); err != nil {
		t.Fatal(err)
	}

	// Bind to an ephemeral UDP port (not 69, which requires root).
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := conn.LocalAddr().String()
	conn.Close()

	s := tftp.NewServer(readHandler(dir), nil)
	go func() { _ = s.ListenAndServe(addr) }()
	t.Cleanup(s.Shutdown)

	c, err := tftp.NewClient(addr)
	if err != nil {
		t.Fatalf("tftp client: %v", err)
	}
	wt, err := c.Receive("ipxe.efi", "octet")
	if err != nil {
		t.Fatalf("tftp Receive: %v", err)
	}
	var buf bytes.Buffer
	if _, err := wt.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("got %q, want %q", buf.Bytes(), want)
	}
}

func TestReadHandler_MissingFile(t *testing.T) {
	dir := t.TempDir()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := conn.LocalAddr().String()
	conn.Close()

	s := tftp.NewServer(readHandler(dir), nil)
	go func() { _ = s.ListenAndServe(addr) }()
	t.Cleanup(s.Shutdown)

	c, err := tftp.NewClient(addr)
	if err != nil {
		t.Fatalf("tftp client: %v", err)
	}
	_, err = c.Receive("missing.efi", "octet")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestReadHandler_PathTraversalBlocked(t *testing.T) {
	dir := t.TempDir()
	secret := []byte("secret")
	// Write a file one level above the root — should not be reachable.
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "secret.efi"), secret, 0600); err != nil {
		t.Fatal(err)
	}

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := conn.LocalAddr().String()
	conn.Close()

	s := tftp.NewServer(readHandler(dir), nil)
	go func() { _ = s.ListenAndServe(addr) }()
	t.Cleanup(s.Shutdown)

	c, err := tftp.NewClient(addr)
	if err != nil {
		t.Fatalf("tftp client: %v", err)
	}
	_, err = c.Receive("../secret.efi", "octet")
	if err == nil {
		t.Error("expected error for path traversal, got nil")
	}
}
