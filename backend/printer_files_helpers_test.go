package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

func ftpTestAddr(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("RRC_FTP_TEST_ADDR")
	if addr == "" {
		t.Skip("set RRC_FTP_TEST_ADDR=host:port to run the FTP wire test")
	}
	return addr
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	parts := strings.Split(addr, ":")
	if len(parts) != 2 {
		t.Fatalf("bad RRC_FTP_TEST_ADDR %q", addr)
	}
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatalf("bad port in %q", addr)
	}
	return parts[0], port
}
