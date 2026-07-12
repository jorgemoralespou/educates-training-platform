package cmd

/*
Project information.
*/
type ProjectInfo struct {
	Version         string
	ImageRepository string
	// GitCommit is the short commit the binary was built from, with a
	// "-dirty" suffix when the working tree had uncommitted changes. It
	// is informational only (shown by `educates version`) — unlike
	// Version, it never feeds image-tag resolution. Empty for a plain
	// `go build` with no ldflags.
	GitCommit string
	// BuildDate is the commit date (YYYY-MM-DD) of GitCommit. Empty when
	// GitCommit is.
	BuildDate string
}

/*
Populate project information.

NOTE: This is expected to be provided with values corresponding to any defaults
but where they could have been overridden at compile time as part of a release
of the Educates CLI.
*/
func NewProjectInfo(version string, imageRepository string, gitCommit string, buildDate string) ProjectInfo {
	return ProjectInfo{
		Version:         version,
		ImageRepository: imageRepository,
		GitCommit:       gitCommit,
		BuildDate:       buildDate,
	}
}
