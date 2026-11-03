package main

import (
	"io"
	"log/slog"
	"net"
	"testing"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestListenWithDriftKeepsFreePort(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserving a probe port: %v", err)
	}
	addr := probe.Addr().String()
	probe.Close()

	listener, err := listenWithDrift(addr, quietLogger())
	if err != nil {
		t.Fatalf("listenWithDrift failed: %v", err)
	}
	defer listener.Close()
	if listener.Addr().String() != addr {
		t.Fatalf("free port must be kept: got %s want %s", listener.Addr().String(), addr)
	}
}

func TestListenWithDriftFallsBackWhenBusy(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupying a port: %v", err)
	}
	defer busy.Close()

	listener, err := listenWithDrift(busy.Addr().String(), quietLogger())
	if err != nil {
		t.Fatalf("busy port must drift instead of failing: %v", err)
	}
	defer listener.Close()
	if listener.Addr().String() == busy.Addr().String() {
		t.Fatalf("drift returned the busy port %s", listener.Addr().String())
	}
	// the drifted listener must actually accept connections
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("connecting to the drifted listener: %v", err)
	}
	conn.Close()
}

func TestListenWithDriftKeepsWildcardsWhenBusy(t *testing.T) {
	// occupy one port explicitly to force the conflict on that port only
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupying a port: %v", err)
	}
	defer busy.Close()
	_, port, _ := net.SplitHostPort(busy.Addr().String())

	listener, err := listenWithDrift(":"+port, quietLogger())
	if err != nil {
		t.Fatalf("wildcard address must drift too: %v", err)
	}
	defer listener.Close()
}
