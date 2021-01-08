package main

import (
	"context"
	"fmt"
	"os"

	"github.com/tanaton/asset/app"
)

func main() {
	defer func() {
		if err := recover(); err != nil {
			fmt.Fprintf(os.Stderr, "Error:\n%s", err)
			os.Exit(1)
		}
	}()
	os.Exit(_main())
}

func _main() int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a := app.New()
	if err := a.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error:%s\n", err)
		return 1
	}
	return 0
}
