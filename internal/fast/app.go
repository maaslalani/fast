package fast

import (
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

const help = `fast tests your internet speed from the command line.

Usage:
  fast [flags]

Flags:
  -h, --help    Show help

Keyboard:
  q, esc, ctrl+c    Quit
`

func Run(args []string, stdout io.Writer) error {
	for _, arg := range args {
		switch arg {
		case "-h", "--help":
			_, err := fmt.Fprint(stdout, help)
			return err
		default:
			return fmt.Errorf("unknown argument %q\n\n%s", arg, help)
		}
	}

	urls, err := targets(connections)
	if err != nil {
		return err
	}

	_, err = tea.NewProgram(newModel(urls)).Run()
	return err
}
