package main

import (
	"log"

	"github.com/palSagnik/aphros/internal/server"
)

func main() {
	httpServer := server.NewHTTPServer(":8080")
	log.Fatal(httpServer.ListenAndServe())
}
