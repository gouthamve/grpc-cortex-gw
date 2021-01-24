package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
)

var (
	cortexEndpoint string
	listenAddress  string
	tenantID       string
)

func main() {
	// Register flags.
	flag.StringVar(&cortexEndpoint, "cortex.endpoint", "", "The endpoint of the Cortex distributor. In grpc LB format.")
	flag.StringVar(&tenantID, "cortex.tenant-id", "", "What tenant ID to set.")
	flag.StringVar(&listenAddress, "server.listen-address", ":8080", "The listen address for the gateway.")
	flag.Parse()

	httpgrpcProxy, err := NewProxy(cortexEndpoint, tenantID)
	if err != nil {
		panic(err)
	}

	r := mux.NewRouter()
	r.Handle("/api/v1/push/influx/write", HandlerForInfluxLine(httpgrpcProxy))
	r.PathPrefix("/").Handler(httpgrpcProxy)

	loggedRouter := handlers.LoggingHandler(os.Stdout, r)
	// http.Handle("/", httpgrpcProxy)
	http.Handle("/", loggedRouter)

	log.Fatal(http.ListenAndServe(":8080", nil))
}
