package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/tsuru/deploy-agent/v2/api/v1alpha1"
)

var (
	addr = flag.String("addr", "localhost:4444", "the address to connect to")
)

func main() {
	flag.Parse()

	if len(flag.Args()) < 1 {
		log.Fatal("requires at least one file to deploy")
	}

	filename := flag.Args()[0]
	file, err := os.Open(filename)
	if err != nil {
		log.Fatalf("failed to read file from fs: %v", err)
	}
	defer file.Close()

	// Set up a connection to the server.

	conn, err := grpc.Dial(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	c := pb.NewBuildClient(conn)

	// Contact the server and print out its response.
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	data, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("failed to read all file: %v", err)
	}

	stream, err := c.Build(ctx, &pb.BuildRequest{
		DeployOrigin:      pb.DeployOrigin_DEPLOY_ORIGIN_SOURCE_FILES,
		SourceImage:       "docker.io/tsuru/scratch:latest",
		DestinationImages: []string{"192.168.1.20:5000/my-app:latest", "192.168.1.20:5000/my-app:v1"},
		Data:              data,
	})
	if err != nil {
		log.Fatalf("failed to build: %v", err)
	}

	// read messages from server

	for {
		r, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			log.Fatalln("failed to receive message from server", err)
		}

		switch r.Data.(type) {
		case *pb.BuildResponse_TsuruConfig:
			fmt.Println("--> Tsuru app files:")
			fmt.Printf("\tTsuru YAML:\n")
			fmt.Println(r.GetTsuruConfig().TsuruYaml)
			fmt.Printf("\tProcfile:\n")
			fmt.Println(r.GetTsuruConfig().Procfile)
			fmt.Println()

		case *pb.BuildResponse_Output:
			fmt.Print(r.GetOutput())
		}
	}
}
