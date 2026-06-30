package fast

import (
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const help = `fast tests your internet speed from the command line.

Usage:
  fast [flags]

Flags:
  -h, --help          Show help
      --ipv4          Prefer IPv4 targets
      --ipv6          Prefer IPv6 targets
      --token token   Use an explicit fast.com API token

Keyboard:
  q, esc, ctrl+c    Quit
`

type options struct {
	token        string
	ipPreference ipPreference
	ipSet        bool
}

func Run(args []string, stdout io.Writer) error {
	opts, showHelp, err := parseArgs(args)
	if err != nil {
		return err
	}
	if showHelp {
		_, err := fmt.Fprint(stdout, help)
		return err
	}

	testTargets, err := targets(connections, opts.token, opts.ipPreference)
	if err != nil {
		return err
	}

	_, err = tea.NewProgram(newModel(testTargets)).Run()
	return err
}

func parseArgs(args []string) (options, bool, error) {
	var opts options
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-h", "--help":
			return opts, true, nil
		case "--ipv4":
			if opts.ipSet && opts.ipPreference != preferIPv4 {
				return opts, false, fmt.Errorf("--ipv4 and --ipv6 cannot be used together\n\n%s", help)
			}
			opts.ipPreference = preferIPv4
			opts.ipSet = true
		case "--ipv6":
			if opts.ipSet && opts.ipPreference != preferIPv6 {
				return opts, false, fmt.Errorf("--ipv4 and --ipv6 cannot be used together\n\n%s", help)
			}
			opts.ipPreference = preferIPv6
			opts.ipSet = true
		case "--token":
			i++
			if i >= len(args) || args[i] == "" || strings.HasPrefix(args[i], "-") {
				return opts, false, fmt.Errorf("missing value for --token\n\n%s", help)
			}
			opts.token = args[i]
		default:
			if strings.HasPrefix(arg, "--token=") {
				opts.token = strings.TrimPrefix(arg, "--token=")
				if opts.token == "" {
					return opts, false, fmt.Errorf("missing value for --token\n\n%s", help)
				}
				continue
			}
			return opts, false, fmt.Errorf("unknown argument %q\n\n%s", arg, help)
		}
	}
	return opts, false, nil
}
