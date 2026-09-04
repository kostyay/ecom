# ecom

![Agents using the ecom CLI gateway to access ecommerce websites](docs/logo.png)

`ecom` is a command-line tool for product discovery on one commerce provider at a time. It gives stable JSON to agents and scripts. It can also give tables to people.

The compiled providers are Bike-Discount and Wallapop. `ecom` does not compare shops. Run one command for each shop and compare the results in your own program.

<p align="center">
  <img src="docs/demo.gif" alt="ecom CLI demo" width="700">
</p>

## Install

Go 1.27.0 or newer is required.

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
ecom provider help wallapop -o table
ecom search "gravel talla M" --provider wallapop --filter max_distance_km=100 --filter max_price=2000 --sort closest
```

JSON is the default for data commands. Use `-o table` for a human-readable result. Use a kubectl-style JSONPath expression to select fields:

```sh
ecom search "helmet" -o 'jsonpath={.data.items[*].price.display}'
ecom search "helmet" | jq .
```

The CLI does not include jq expressions or a pretty-JSON option. Pipe its compact JSON to `jq` when you need them.

The returned price is the item price that the site shows. Shipping and optional fees are not included by default. `ecom` does not convert currency.

Wallapop supports public `search` and `item` requests without authentication or a browser. Search uses Andorra la Vella as its default center. You can set `latitude`, `longitude`, `max_distance_km`, `min_price`, `max_price`, and `category_id` filters. Run `ecom provider help wallapop` for current sort and paging limits. Wallapop can change or limit its public endpoints.

See [the user guide](docs/user-guide.md) for all commands, configuration, browser setup, cache behavior, and provider limits.

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
Wallapop also requires `pricing.include_shipping: false` and does not accept provider-specific configuration values.

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

`make build` writes `bin/ecom`. Providers are compiled by blank Go imports in `main.go`.
See [the provider author guide](docs/provider-author-guide.md) to create and test an external provider with only the public SDK.

## License

MIT
