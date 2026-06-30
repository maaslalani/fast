package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/maaslalani/fast/internal/fast"
)

func main() {
	if err := fast.Run(); err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) {
			fmt.Fprintln(os.Stderr, "No internet connection.")
			os.Exit(1)
		}
		log.Fatal(err)
	}
}
