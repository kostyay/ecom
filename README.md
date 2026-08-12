# ecom

`ecom` is a command-line tool for product discovery on one commerce provider at a time. It gives stable JSON to agents and scripts. It can also give tables to people.

The first compiled provider is Bike-Discount. `ecom` does not compare shops. Run one command for each shop and compare the results in your own program.

## Install

Go 1.26.5 or newer is required.

```sh
go install github.com/kostyay/ecom@latest
```

You can also build the repository:

```sh
make build
./bin/ecom version
```

## Start

The default provider is `bike-discount`.

```sh
ecom provider help bike-discount -o table
ecom filters deals -o table
ecom search "powertube" -o table
ecom deals --page 1 --page-size 48
ecom item "https://www.bike-discount.de/en/yamaha-500-wh-36v/13.6ah-frame-battery"
```

JSON is the default for data commands. Use `-o table` for a human-readable result. Use a kubectl-style JSONPath expression to select fields:

```sh
ecom search "helmet" -o 'jsonpath={.data.items[*].price.display}'
```

The returned price is the item price that the site shows. Shipping and optional fees are not included by default. `ecom` does not convert currency.

See [the user guide](docs/user-guide.md) for all commands, configuration, browser setup, cache behavior, and Bike-Discount limits.

## Configuration

The optional file is `ecom/config.yaml` in the operating system user configuration directory. For example, this is usually `~/.config/ecom/config.yaml` on Linux and `~/Library/Application Support/ecom/config.yaml` on macOS. You can use another file with `--config`.

```yaml
provider: bike-discount

market:
  country: DE
  language: en
  currency: EUR

pricing:
  include_shipping: false

cache:
  path: ""
  ttl: 24h
  max_size: 512MiB
  max_response_size: 20MiB

network:
  requests_per_second: 1
  max_concurrent_http: 2
  max_concurrent_browser: 1
  retries: 3

browser:
  cdp_address: ""
  headed: false
  interactive_timeout: 5m

providers:
  bike-discount:
    page_size: 48

log:
  level: info
  file: ""
```

Flags override environment variables. Environment variables override the file. For example, `ECOM_MARKET_CURRENCY=EUR` sets `market.currency`.

Bike-Discount supports only `pricing.include_shipping: false`. It returns `invalid_provider_config` before a network request if this value is `true`.

## Develop

Run the full repository check before you send a change:

```sh
make quality
```

The quality check runs the format check, linter, vet, full offline tests, focused
fixture and conformance tests, documentation example tests, race tests, and the
build. The standard suite does not access a live website and does not start a
browser. Tests read the checked-in fixtures but do not change them. Run
`make fmt` separately when you want to change source formatting.

`make build` writes `bin/ecom`. The provider is compiled by a blank Go import in `main.go`.
See [the provider author guide](docs/provider-author-guide.md) to create and test an external provider with only the public SDK.

## License

MIT
