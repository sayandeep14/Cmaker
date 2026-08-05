package main

import "cmaker/cmd"

// version is set at build time via `-ldflags "-X main.version=..."`
// (goreleaser does this automatically); a plain `go build` leaves it "dev".
var version = "dev"

func main() {
	cmd.SetVersion(version)
	cmd.Execute()
}
