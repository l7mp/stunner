package buildinfo

import "fmt"

type BuildInfo struct {
	Version    string
	CommitHash string
	BuildDate  string
}

func (i BuildInfo) String() string {
	return fmt.Sprintf("version %s (%s) built on %s", i.Version, i.CommitHash, i.BuildDate)
}
