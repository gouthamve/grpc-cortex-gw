package main

import (
	"flag"
	"log"
	"net/http"
)

var (
	cortexEndpoint string
	listenAddress  string
)

func main() {
	// Register flags.
	flag.StringVar(&cortexEndpoint, "cortex.endpoint", "", "The endpoint of the Cortex distributor. In grpc LB format.")
	flag.StringVar(&listenAddress, "server.listen-address", ":8080", "The listen address for the gateway.")
	flag.Parse()

	handler, err := NewProxy(cortexEndpoint)
	if err != nil {
		panic(err)
	}

	// Register logger.
	http.Handle("/", handler)

	// Run a server with grpcProxy
	log.Fatal(http.ListenAndServe(":8080", nil))
}
