package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/ccfos/nightingale/v6/pkg/opsdemo"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:39708", "Loopback address")
	data := flag.String("data", "", "Private, initially empty data directory")
	ident := flag.String("ident", "ops-demo", "Test target identity")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		log.Fatal("ops-demo must listen on an explicit loopback address")
	}
	demo, err := opsdemo.New(*data, *ident)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: *listen, Handler: demo, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	log.Fatal(server.ListenAndServe())
}
