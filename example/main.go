package main

import (
	"context"
	"fmt"

	assemble "github.com/xfrr/assemble"
)

func main() {
	// Build the container
	container, err := assemble.Assemble()
	if err != nil {
		panic(err)
	}

	// Start the container
	if err := container.Start(context.Background()); err != nil {
		panic(err)
	}

	// Resolve after Start using the container directly (it implements assemble.Resolver)
	srv, err := assemble.Get[*Server](container)
	if err != nil {
		panic(err)
	}

	fmt.Println("----- Creation Order DOT -----")
	fmt.Println(container.ExportCreationOrderDOT())
	fmt.Println("----- Creation Order PlantUML -----")
	fmt.Println(container.ExportCreationOrderPlantUML())
	srv.log.Infof("Server is live. Middlewares: %d", len(srv.mw))

	if err := container.Shutdown(context.Background()); err != nil {
		panic(err)
	}
}
