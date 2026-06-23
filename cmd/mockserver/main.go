// Copyright 2026 Query Farm LLC - https://query.farm

// Command mockserver runs a standalone in-memory feed server exposing a known
// RSS 2.0 feed at /rss, an Atom feed at /atom, a JSON Feed at /json, and a
// /malformed endpoint. It is used by the haybarn SQL end-to-end tests: the
// Makefile starts it on a free port, reads the printed PORT line, and points the
// worker's feed functions at it.
//
// Usage:
//
//	mockserver [--addr 127.0.0.1:0]
//
// On startup it prints "PORT:<n>" (the bound TCP port) to stdout so a caller can
// discover the port even when binding to :0.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/Query-farm/vgi-feed/internal/mockfeed"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "TCP address to listen on (host:port; port 0 = pick a free port)")
	flag.Parse()

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("mockserver: listen %q: %v", *addr, err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	fmt.Printf("PORT:%d\n", port)
	_ = os.Stdout.Sync()

	srv := &http.Server{Handler: mockfeed.NewHandler()}
	if err := srv.Serve(lis); err != nil && err != http.ErrServerClosed {
		log.Fatalf("mockserver: serve: %v", err)
	}
}
