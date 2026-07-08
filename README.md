# Fast

Test your internet speed from the command-line, powered by [fast.com](https://fast.com).

<img src="vhs/demo.gif" width="600" alt="fast running a speed test in the terminal" />

### Usage

```
Usage:
  fast [flags]

Flags:
  -client
        Show client info
  -connections connections
        Number of parallel connections (default 8)
  -download
        Measure download speed
  -duration duration
        Measure each transfer for duration (default 10s)
  -h    Show help
  -help
        Show help
  -ipv4
        Prefer IPv4 targets
  -ipv6
        Prefer IPv6 targets
  -json
        Print results as JSON
  -no-tui
        Print plain text instead of the terminal UI
  -server
        Show server info
  -token token
        Use an explicit fast.com API token
  -upload
        Measure upload speed
  -version
        Show version

Keyboard:
  q, esc, ctrl+c    Quit
```

`fast` measures your upload and download speed against the nearest Netflix Open Connect
servers and reports it in megabits per second, right inline in your terminal,
along with your ping to that server.

### Installation

Install with Go:

```sh
go install github.com/maaslalani/fast@main
```

Or download a binary from the [releases](https://github.com/maaslalani/fast/releases).

## License

[MIT](https://github.com/maaslalani/fast/blob/master/LICENSE)

## Feedback

I'd love to hear your feedback on improving `fast`.

Feel free to reach out via:
* [Email](mailto:maas@lalani.dev)
* [Twitter](https://twitter.com/maaslalani)
* [GitHub issues](https://github.com/maaslalani/fast/issues/new)

---

<sub><sub>z</sub></sub><sub>z</sub>z
