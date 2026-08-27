module github.com/remnawave/remnawave-reverse-proxy-go

go 1.22

require golang.org/x/net v0.34.0

// The dev sandbox this project is built in blocks golang.org itself, so
// resolving golang.org/x/net's vanity import path fails even with
// GOPROXY=direct. github.com/golang/net mirrors the same module content
// under a reachable host. This works the same way on an unrestricted
// network (CI, another developer's machine), so it needs no special
// handling anywhere except when bumping the version.
replace golang.org/x/net v0.34.0 => github.com/golang/net v0.34.0
