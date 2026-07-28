// This directory holds the frontend and contains no Go code of its own.
//
// It carries a go.mod purely to cut the subtree out of the root module: npm
// installs at least one dependency that vendors a Go package
// (flatted/golang/pkg/flatted), and without this file `go build ./...`,
// `go vet ./...` and `go test ./...` compile that third-party code as part of
// the dashboard. CI never noticed because it runs the Go tests before
// `npm ci`, so node_modules does not exist yet there — the breakage only ever
// showed up locally, where it also meant an unrelated upstream package could
// fail the whole build.
module opencode-dashboard/web

go 1.26
