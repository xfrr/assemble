package main

import (
	"context"
	"fmt"

	assemble "github.com/xfrr/assemble"
	"github.com/xfrr/assemble/example/di"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Build the container using the compiled Assemble function
	container, err := di.AssembleServer()
	if err != nil {
		panic(err)
	}

	// Alternatively, use the assemble.Assemble function with the Core variable
	// to build the container dynamically
	// container, err := assemble.Assemble(Core)
	// if err != nil {
	// 	panic(err)
	// }

	// Start the container
	if startErr := container.Start(context.Background()); startErr != nil {
		panic(startErr)
	}

	// Resolve after Start using the container directly (it implements assemble.Resolver)
	_, err = assemble.Get[*di.Server](ctx, container)
	if err != nil {
		panic(err)
	}

	logger, err := assemble.Get[di.SimpleLogger](ctx, container)
	if err != nil {
		panic(err)
	}

	fmt.Println("----- Creation Order DOT -----")
	fmt.Println(container.ExportCreationOrderDOT())
	fmt.Println("----- Creation Order PlantUML -----")
	fmt.Println(container.ExportCreationOrderPlantUML())
	logger.Info("Server is running...")

	if shutdownErr := container.Shutdown(context.Background()); shutdownErr != nil {
		panic(shutdownErr)
	}
}
