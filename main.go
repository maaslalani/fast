package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	final, err := tea.NewProgram(NewModel()).Run()
	if err != nil {
		log.Fatal(err)
	}

	if m, ok := final.(Model); ok && m.err != nil {
		var netErr net.Error
		if errors.As(m.err, &netErr) {
			fmt.Fprintln(os.Stderr, "No internet connection.")
			os.Exit(1)
		}
		log.Fatal(m.err)
	}
}
