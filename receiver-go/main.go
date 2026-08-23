package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
)

func main() {
	host := flag.String("host", envOrDefault("HOST", "127.0.0.1"), "TCP/IPv4 listen host")
	defaultPort, err := envIntOrDefault("PORT", 9000)
	if err != nil {
		log.Fatal(err)
	}
	port := flag.Int("port", defaultPort, "TCP listen port")
	flag.Parse()

	if *port < 1 || *port > 65535 {
		log.Fatal("port must be between 1 and 65535")
	}
	address := net.JoinHostPort(*host, strconv.Itoa(*port))
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		log.Fatalf("listen on %s: %v", address, err)
	}
	defer listener.Close()

	log.Printf("receiver listening on %s", listener.Addr())
	if err := Serve(listener); err != nil {
		log.Fatal(err)
	}
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envIntOrDefault(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: must be an integer", name, value)
	}
	return parsed, nil
}
