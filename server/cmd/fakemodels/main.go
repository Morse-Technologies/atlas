package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

const (
	defaultAddr   = ":8080"
	healthTimeout = 3 * time.Second
	readTimeout   = 30 * time.Second
)

func main() {
	addr := flag.String("addr", defaultAddr, "listen address")
	health := flag.Bool("health", false, "check a running instance and exit (Compose health check; the image has no curl)")
	flag.Parse()
	if *health {
		os.Exit(checkHealth(*addr))
	}
	server := &http.Server{Addr: *addr, Handler: routes(), ReadHeaderTimeout: readTimeout}
	log.Printf("fake models listening on %s", *addr)
	log.Fatal(server.ListenAndServe())
}

func checkHealth(addr string) int {
	client := http.Client{Timeout: healthTimeout}
	resp, err := client.Get("http://127.0.0.1" + addr + "/health")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
