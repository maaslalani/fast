package fast

import (
	"bytes"
	"flag"
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

type options struct {
	token        string
	ipPreference ipPreference
	ipSet        bool
	client       bool
	server       bool
}

func Run(args []string, stdout io.Writer) error {
	opts, showHelp, err := parseArgs(args)
	if err != nil {
		return err
	}
	if showHelp {
		_, err := fmt.Fprint(stdout, helpText())
		return err
	}

	test, err := speedtestConfig(connections, opts.token, opts.ipPreference)
	if err != nil {
		return err
	}

	_, err = tea.NewProgram(newModel(test, opts)).Run()
	return err
}

func parseArgs(args []string) (options, bool, error) {
	var opts options
	var help, ipv4, ipv6 bool
	flags := newFlagSet(io.Discard, &opts, &help, &ipv4, &ipv6)
	if err := flags.Parse(args); err != nil {
		return opts, false, fmt.Errorf("%w\n\n%s", err, helpText())
	}
	if help {
		return opts, true, nil
	}
	if flags.NArg() > 0 {
		return opts, false, fmt.Errorf("unknown argument %q\n\n%s", flags.Arg(0), helpText())
	}
	if ipv4 && ipv6 {
		return opts, false, fmt.Errorf("--ipv4 and --ipv6 cannot be used together\n\n%s", helpText())
	}
	if ipv4 {
		opts.ipPreference = preferIPv4
		opts.ipSet = true
	}
	if ipv6 {
		opts.ipPreference = preferIPv6
		opts.ipSet = true
	}
	return opts, false, nil
}

func newFlagSet(output io.Writer, opts *options, help *bool, ipv4 *bool, ipv6 *bool) *flag.FlagSet {
	flags := flag.NewFlagSet("fast", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.Usage = func() {
		fmt.Fprintln(output, "fast tests your internet speed from the command line.")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Usage:")
		fmt.Fprintln(output, "  fast [flags]")
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Flags:")
		flags.PrintDefaults()
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Keyboard:")
		fmt.Fprintln(output, "  q, esc, ctrl+c    Quit")
	}

	flags.BoolVar(&opts.client, "client", false, "Show client info")
	flags.BoolVar(help, "h", false, "Show help")
	flags.BoolVar(help, "help", false, "Show help")
	flags.BoolVar(ipv4, "ipv4", false, "Prefer IPv4 targets")
	flags.BoolVar(ipv6, "ipv6", false, "Prefer IPv6 targets")
	flags.BoolVar(&opts.server, "server", false, "Show server info")
	flags.StringVar(&opts.token, "token", "", "Use an explicit fast.com API `token`")
	return flags
}

func helpText() string {
	var opts options
	var buf bytes.Buffer
	var help, ipv4, ipv6 bool
	newFlagSet(&buf, &opts, &help, &ipv4, &ipv6).Usage()
	return buf.String()
}
