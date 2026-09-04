// Package version reads version data from Go build metadata.
package version

import (
	"runtime"
	"runtime/debug"
)

const developmentVersion = "devel"

// Info is the machine-readable version response.
type Info struct {
	Version     string `json:"version"`
	GoVersion   string `json:"go_version"`
	VCSRevision string `json:"vcs_revision,omitempty"`
	VCSTime     string `json:"vcs_time,omitempty"`
	VCSModified bool   `json:"vcs_modified,omitzero"`
}

// Current returns version data for the running executable.
func Current() Info {
	info := Info{
		Version:   developmentVersion,
		GoVersion: runtime.Version(),
	}

	build, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	if build.Main.Version != "" && build.Main.Version != "(devel)" {
		info.Version = build.Main.Version
	}
	for _, setting := range build.Settings {
		switch setting.Key {
		case "vcs.revision":
			info.VCSRevision = setting.Value
		case "vcs.time":
			info.VCSTime = setting.Value
		case "vcs.modified":
			info.VCSModified = setting.Value == "true"
		}
	}

	return info
}
