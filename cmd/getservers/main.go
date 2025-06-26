package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	api "github.com/palSagnik/aphros/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	addr := flag.String("addr", ":8400", "service address") 
	flag.Parse()
	
	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	
	client := api.NewLogClient(conn)
	ctx := context.Background()
	res, err := client.GetServers(ctx, &api.GetServersRequest{})
	if err != nil {
		log.Fatal(err)
	}
	
	fmt.Println("servers:")
	for _, server := range res.Servers {
		fmt.Printf("- id: %s, rpc_addr: %s\n",
			server.Id, server.RpcAddr)
	}
}