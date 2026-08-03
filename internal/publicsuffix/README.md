# internal/publicsuffix

This package is vendored verbatim from
[`golang.org/x/net/publicsuffix`](https://github.com/golang/net/tree/master/publicsuffix)
(BSD-3-Clause, see `LICENSE` in this directory).

## Why vendored instead of a normal `go.mod` dependency

The build environment used to develop this project only allows module
fetches from a handful of whitelisted domains (github.com, pypi.org,
npmjs.com, crates.io, ...) and does not allow `proxy.golang.org` / the
default Go module proxy. `golang.org/x/net` can't be `go get`-ed under
that restriction, but its source is mirrored on GitHub
(`github.com/golang/net`), which *is* reachable - so the specific package
we need was copied in directly instead.

`publicsuffix` only depends on the Go standard library (`fmt`,
`net/http/cookiejar`, `net/netip`, `strings`, `embed`), so vendoring just
this one package (rather than the whole `golang.org/x/net` module) is
sufficient - no transitive dependencies to worry about.

## Why we need this at all

`internal/domain.ExtractDomain()` (a port of the original bash script's
`extract_domain()`) originally just took the last two dot-separated labels
of a domain - which is wrong for any domain under a multi-label public
suffix like `.co.uk`, `.com.au`, `.co.jp`, etc. (`sub.example.co.uk` would
incorrectly reduce to `co.uk` instead of `example.co.uk`). That's a bug
inherited from the original bash `awk` one-liner, not something introduced
during porting - but "give a good implementation, not a byte-for-byte
copy of a known bug" is exactly the kind of thing that's worth fixing
properly.

`EffectiveTLDPlusOne()` implements the real [Public Suffix
List](https://publicsuffix.org/) algorithm, so it correctly returns
`example.co.uk` for `sub.example.co.uk`, `example.com` for
`sub.example.com`, etc.

## Updating

The embedded suffix list (`data/nodes`, `data/text`, `data/children`) is a
point-in-time snapshot from publicsuffix.org (see the `version` string in
`table.go`). Public suffix rules do change occasionally; re-vendoring this
directory from an up-to-date `golang.org/x/net/publicsuffix` checkout is
safe and requires no code changes on our side (`internal/domain` only
calls the stable `EffectiveTLDPlusOne` function).
