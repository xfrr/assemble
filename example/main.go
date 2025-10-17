package main

import (
	"context"
	"fmt"

	assemble "github.com/xfrr/assemble"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Build the container
	container, err := assemble.Assemble(Core)
	if err != nil {
		panic(err)
	}

	// Start the container
	if startErr := container.Start(context.Background()); startErr != nil {
		panic(startErr)
	}

	// Resolve after Start using the container directly (it implements assemble.Resolver)
	srv, err := assemble.Get[*Server](ctx, container)
	if err != nil {
		panic(err)
	}

	fmt.Println("----- Creation Order DOT -----")
	fmt.Println(container.ExportCreationOrderDOT())
	fmt.Println("----- Creation Order PlantUML -----")
	fmt.Println(container.ExportCreationOrderPlantUML())
	srv.logger.Info("Server is running...")

	if shutdownErr := container.Shutdown(context.Background()); shutdownErr != nil {
		panic(shutdownErr)
	}
}
