package main

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func TestConnectionHandlesMultipleLinesAndSurvivesInvalidInput(t *testing.T) {
	listener, done := startTestServer(t)
	defer stopTestServer(t, listener, done)

	connection, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if _, err := connection.Write(append([]byte("not-json\n"), append(validRequest("after-error", "hamming-secded-13-8", 1, "1000100100010", 0), '\n')...)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	first := readResponse(t, reader)
	second := readResponse(t, reader)
	assertResponse(t, first, "invalid_request", "invalid_json", "")
	assertResponse(t, second, "ok", "", "A")

	// A new connection confirms malformed client input did not terminate the listener.
	secondConnection, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer secondConnection.Close()
	if _, err := secondConnection.Write(append(validRequest("still-alive", "hamming-secded-13-8", 1, "1000100100010", 0), '\n')); err != nil {
		t.Fatal(err)
	}
	assertResponse(t, readResponse(t, bufio.NewReader(secondConnection)), "ok", "", "A")
}

func TestLineTooLongReturnsErrorAndClosesOnlyThatConnection(t *testing.T) {
	listener, done := startTestServer(t)
	defer stopTestServer(t, listener, done)

	connection, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write([]byte(strings.Repeat("x", maxLineBytes+1) + "\n")); err != nil {
		t.Fatal(err)
	}
	response := readResponse(t, bufio.NewReader(connection))
	assertResponse(t, response, "invalid_request", "line_too_long", "")
	connection.Close()

	probe, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatalf("listener stopped after long line: %v", err)
	}
	probe.Close()
}

func TestIdleClientTimesOutWithoutStoppingListener(t *testing.T) {
	listener, done := startConfiguredTestServer(t, 1, 100*time.Millisecond)
	defer stopTestServer(t, listener, done)

	idle, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	if _, err := idle.Write([]byte("{")); err != nil {
		t.Fatal(err)
	}
	if err := idle.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := idle.Read(make([]byte, 1)); err == nil {
		t.Fatal("idle connection remained open")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatalf("client deadline expired before the server closed the idle connection: %v", err)
	}

	probe, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatalf("listener stopped after idle timeout: %v", err)
	}
	defer probe.Close()
	if _, err := probe.Write(append(validRequest("after-timeout", "hamming-secded-13-8", 1, "1000100100010", 0), '\n')); err != nil {
		t.Fatal(err)
	}
	assertResponse(t, readResponse(t, bufio.NewReader(probe)), "ok", "", "A")
}

func TestConcurrentClientLimitRejectsExcessAndRecovers(t *testing.T) {
	listener, done := startConfiguredTestServer(t, 1, time.Second)
	defer stopTestServer(t, listener, done)

	first, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := first.Write(append(validRequest("first-client", "hamming-secded-13-8", 1, "1000100100010", 0), '\n')); err != nil {
		t.Fatal(err)
	}
	firstReader := bufio.NewReader(first)
	assertResponse(t, readResponse(t, firstReader), "ok", "", "A")

	excess, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := excess.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := excess.Read(make([]byte, 1)); err == nil {
		t.Fatal("excess connection was not closed")
	}
	excess.Close()

	if _, err := first.Write(append(validRequest("after-limit", "hamming-secded-13-8", 1, "1000100100010", 0), '\n')); err != nil {
		t.Fatal(err)
	}
	assertResponse(t, readResponse(t, firstReader), "ok", "", "A")
}

func startTestServer(t *testing.T) (net.Listener, <-chan error) {
	return startConfiguredTestServer(t, maxConcurrentClients, connectionOperationTime)
}

func startConfiguredTestServer(t *testing.T, maxClients int, operationTimeout time.Duration) (net.Listener, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- serve(listener, maxClients, operationTimeout) }()
	return listener, done
}

func stopTestServer(t *testing.T, listener net.Listener, done <-chan error) {
	t.Helper()
	listener.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("server returned: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("server did not stop")
	}
}

func readResponse(t *testing.T, reader *bufio.Reader) Response {
	t.Helper()
	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatal(err)
	}
	return response
}
