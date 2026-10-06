module github.com/FlexEbat/RemnaForge

go 1.22

require golang.org/x/net v0.34.0

// golang.org/x/net is resolved through its GitHub mirror; keep this in
// mind when bumping the version.
replace golang.org/x/net v0.34.0 => github.com/golang/net v0.34.0
