# ecom

`ecom` is a machine-readable command-line platform for e-commerce utilities. It is designed for agents and scripts that help people search e-commerce sites and find good deals.

The project is at an early stage. The first version provides the CLI base, configuration, structured logs, shell completion, and version output. It does not yet connect to e-commerce sites.

## Install

Go 1.26.5 or newer is required.

```sh
go install github.com/kostyay/ecom@latest
```

## Use

```sh
ecom
ecom version
ecom version --json
ecom completion zsh
```

Commands do not ask questions unless interactive behavior is explicitly enabled. Future data commands will write result data to standard output. Errors and diagnostics will use standard error.

## Configuration

The optional configuration file is stored in the operating system user configuration directory under `ecom/config.yaml`.

```yaml
log:
  level: info
  file: ""
```

Flags have the highest priority. Environment variables have the next priority. The configuration file has the lowest priority.

```sh
ECOM_LOG_LEVEL=debug ecom version
ecom version --log-level warn
ecom version --log-file ./ecom.log
```

Logs use JSON. The default log file is `ecom/ecom.log` in the operating system user cache directory. A log file is limited to 10 MB. The logger keeps three old files and removes files after seven days.

## Develop

```sh
make fmt
make lint
make test
make build
```

`make build` writes the executable to `bin/ecom`. The linter does not lint test files. `go test` still compiles and runs test code.

## License

MIT
