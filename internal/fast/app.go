package fast

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
)

var version = "dev"

type options struct {
	token        string
	ipPreference ipPreference
	ipSet        bool
	client       bool
	server       bool
	down         bool
	up           bool
	json         bool
	noTUI        bool
	duration     time.Duration
	connections  int
	version      bool
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
	if opts.version {
		_, err := fmt.Fprintf(stdout, "fast %s\n", version)
		return err
	}

	test, err := speedtestConfig(opts.connections, opts.token, opts.ipPreference)
	if err != nil {
		return err
	}
	if opts.json || opts.noTUI || !isTerminal(stdout) {
		result, err := runTest(test, opts)
		if err != nil {
			return err
		}
		if opts.json {
			return json.NewEncoder(stdout).Encode(result)
		}
		return printResult(stdout, result)
	}

	_, err = tea.NewProgram(newModel(test, opts)).Run()
	return err
}

func parseArgs(args []string) (options, bool, error) {
	opts := defaultOptions()
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
	if opts.connections <= 0 {
		return opts, false, fmt.Errorf("invalid --connections value: must be greater than zero\n\n%s", helpText())
	}
	if opts.duration <= 0 {
		return opts, false, fmt.Errorf("invalid --duration value: must be greater than zero\n\n%s", helpText())
	}
	if !opts.down && !opts.up {
		opts.down = true
		opts.up = true
	}
	return opts, false, nil
}

func defaultOptions() options {
	return options{
		duration:    defaultDuration,
		connections: defaultConnections,
	}
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
	flags.BoolVar(&opts.down, "download", false, "Measure download speed")
	flags.BoolVar(help, "h", false, "Show help")
	flags.BoolVar(help, "help", false, "Show help")
	flags.BoolVar(&opts.up, "upload", false, "Measure upload speed")
	flags.IntVar(&opts.connections, "connections", defaultConnections, "Number of parallel `connections`")
	flags.Var(durationValue{duration: &opts.duration}, "duration", "Measure each transfer for `duration`")
	flags.BoolVar(ipv4, "ipv4", false, "Prefer IPv4 targets")
	flags.BoolVar(ipv6, "ipv6", false, "Prefer IPv6 targets")
	flags.BoolVar(&opts.server, "server", false, "Show server info")
	flags.BoolVar(&opts.json, "json", false, "Print results as JSON")
	flags.BoolVar(&opts.noTUI, "no-tui", false, "Print plain text instead of the terminal UI")
	flags.StringVar(&opts.token, "token", "", "Use an explicit fast.com API `token`")
	flags.BoolVar(&opts.version, "version", false, "Show version")
	return flags
}

func helpText() string {
	opts := defaultOptions()
	var buf bytes.Buffer
	var help, ipv4, ipv6 bool
	newFlagSet(&buf, &opts, &help, &ipv4, &ipv6).Usage()
	return buf.String()
}

func parseDuration(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil {
		seconds, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			return 0, err
		}
		duration = time.Duration(seconds) * time.Second
	}
	if duration <= 0 {
		return 0, fmt.Errorf("must be greater than zero")
	}
	return duration, nil
}

type durationValue struct {
	duration *time.Duration
}

func (v durationValue) String() string {
	if v.duration == nil {
		return ""
	}
	return v.duration.String()
}

func (v durationValue) Set(value string) error {
	duration, err := parseDuration(value)
	if err != nil {
		return err
	}
	*v.duration = duration
	return nil
}

func isTerminal(w io.Writer) bool {
	file, ok := w.(interface{ Fd() uintptr })
	return ok && isatty.IsTerminal(file.Fd())
}
