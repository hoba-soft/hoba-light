package main

import (
	"fmt"
	"os"

	"github.com/hoba-soft/hoba-light/internal/hobalight"
)

func main() {
	if err := hobalight.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
