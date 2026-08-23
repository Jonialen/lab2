package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"time"
)

const (
	maxLineBytes            = 1_048_576
	maxConcurrentClients    = 64
	connectionOperationTime = 10 * time.Second
)

func Serve(listener net.Listener) error {
	return serve(listener, maxConcurrentClients, connectionOperationTime)
}

func serve(listener net.Listener, maxClients int, operationTimeout time.Duration) error {
	clients := make(chan struct{}, maxClients)
	for {
		connection, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			if temporary, ok := err.(net.Error); ok && temporary.Temporary() {
				continue
			}
			return err
		}
		select {
		case clients <- struct{}{}:
			go func() {
				defer connection.Close()
				defer func() { <-clients }()
				handleConnection(connection, operationTimeout)
			}()
		default:
			_ = connection.Close()
		}
	}
}

func handleConnection(connection net.Conn, operationTimeout time.Duration) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("recovered client handler panic from %s: %v", connection.RemoteAddr(), recovered)
		}
	}()

	reader := bufio.NewReaderSize(connection, maxLineBytes+1)
	encoder := json.NewEncoder(connection)
	for {
		if err := connection.SetReadDeadline(time.Now().Add(operationTimeout)); err != nil {
			return
		}
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			encodeResponse(connection, encoder, invalidResponse(nil, "line_too_long", "request line exceeds 1,048,576 bytes"), operationTimeout)
			return
		}
		if errors.Is(err, io.EOF) {
			return
		}
		if err != nil {
			return
		}

		line = line[:len(line)-1]
		if len(line) > maxLineBytes {
			encodeResponse(connection, encoder, invalidResponse(nil, "line_too_long", "request line exceeds 1,048,576 bytes"), operationTimeout)
			return
		}
		response := processLine(line)
		if !encodeResponse(connection, encoder, response, operationTimeout) {
			return
		}
	}
}

func encodeResponse(connection net.Conn, encoder *json.Encoder, response Response, operationTimeout time.Duration) bool {
	if err := connection.SetWriteDeadline(time.Now().Add(operationTimeout)); err != nil {
		return false
	}
	return encoder.Encode(response) == nil
}
