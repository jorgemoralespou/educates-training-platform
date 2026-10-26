package oci

import (
	"archive/tar"
	"bufio"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/pkg/errors"

	regv1 "github.com/google/go-containerregistry/pkg/v1"
)

// DefaultTimestamp is the fixed modification time used for every entry when
// SOURCE_DATE_EPOCH is not set, so that two builds of the same content produce
// the same digests. It must be positive: a negative epoch makes tar skip
// entries and yields an empty layer.
var DefaultTimestamp = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// Timestamp returns the fixed time to stamp on every entry, honouring the
// reproducible builds convention when SOURCE_DATE_EPOCH is set.
func Timestamp(environ func(string) string) (time.Time, error) {
	value := environ("SOURCE_DATE_EPOCH")

	if value == "" {
		return DefaultTimestamp, nil
	}

	seconds, err := strconv.ParseInt(value, 10, 64)

	if err != nil {
		return time.Time{}, errors.Wrapf(err, "unable to parse SOURCE_DATE_EPOCH %q", value)
	}

	if seconds <= 0 {
		return time.Time{}, errors.Errorf("SOURCE_DATE_EPOCH must be a positive number of seconds, got %q", value)
	}

	return time.Unix(seconds, 0).UTC(), nil
}

// LayerEntry is one directory or file to write into a layer.
type LayerEntry struct {
	// Path is where the entry sits in the layer, slash-separated and relative
	// to the layer root.
	Path string

	// Directory makes the entry a directory, which has no source.
	Directory bool

	// Source is the file on disk a file entry takes its content and execute
	// bits from.
	Source string
}

// LayerOption adjusts what BuildLayer writes.
type LayerOption func(*layerOptions)

type layerOptions struct {
	appended []appendedFile
}

type appendedFile struct {
	path     string
	contents []byte
}

// AppendFile writes a file held in memory after the sorted entries, readable
// by everyone. Files are appended in the order the options are given.
func AppendFile(path string, contents []byte) LayerOption {
	return func(options *layerOptions) {
		options.appended = append(options.appended, appendedFile{path: path, contents: contents})
	}
}

// Layer is a gzip-compressed OCI layer written by BuildLayer. Its tar is held
// in a temporary file which is removed from its directory as soon as it is
// created, so nothing is left behind however the process ends. The file stays
// readable until Close, which releases the space it takes.
type Layer struct {
	regv1.Layer

	file *os.File
}

// Close releases the temporary file holding the layer. The layer, and any
// image holding it, cannot be read or pushed once it is closed.
func (l *Layer) Close() error {
	return l.file.Close()
}

// IsMacOSMetadataFile reports, by its name, a metadata file macOS leaves
// beside the files it describes, which a layer never holds: .DS_Store and
// AppleDouble files starting with "._". Nothing else starting with a dot is
// skipped, so a layer may hold dotfiles.
func IsMacOSMetadataFile(name string) bool {
	return name == ".DS_Store" || strings.HasPrefix(name, "._")
}

// layerItem is an entry accepted into the layer, at its cleaned path.
type layerItem struct {
	path  string
	entry LayerEntry
}

// BuildLayer writes a reproducible layer from entries. The caller decides
// which directories get an entry; the builder writes no others, and no entry
// for the layer root.
//
// Every entry carries the given timestamp, root ownership and a normalised
// mode: directories 0755, files 0755 when the source has any execute bit and
// 0644 otherwise. Entries are written sorted by path, in PAX format, with
// directory names ending in a slash. An entry with a macOS artefact anywhere
// in its path is skipped. A file source which is a symbolic link, a hard link,
// a device, a named pipe or a socket cannot be published, and every such
// source is named in one error.
//
// File content is streamed from its source into a temporary file holding the
// tar, which the layer reads from whenever its bytes are needed.
func BuildLayer(entries []LayerEntry, timestamp time.Time, options ...LayerOption) (*Layer, error) {
	settings := &layerOptions{}

	for _, option := range options {
		option(settings)
	}

	items, err := acceptEntries(entries)

	if err != nil {
		return nil, err
	}

	for i, file := range settings.appended {
		cleaned, err := cleanLayerPath(file.path)

		if err != nil {
			return nil, err
		}

		if cleaned == "" {
			return nil, errors.Errorf("the file %q appended to the layer has no name", file.path)
		}

		settings.appended[i].path = cleaned
	}

	file, err := os.CreateTemp("", "educates-layer-*.tar")

	if err != nil {
		return nil, errors.Wrap(err, "unable to create a temporary file for the layer")
	}

	built := false

	defer func() {
		if !built {
			file.Close()
		}
	}()

	if err := os.Remove(file.Name()); err != nil {
		return nil, errors.Wrap(err, "unable to remove the name of the temporary file for the layer")
	}

	buffered := bufio.NewWriterSize(file, 1<<16)

	if err := writeTar(buffered, items, settings.appended, timestamp); err != nil {
		return nil, err
	}

	if err := buffered.Flush(); err != nil {
		return nil, errors.Wrap(err, "unable to write the layer")
	}

	size, err := file.Seek(0, io.SeekCurrent)

	if err != nil {
		return nil, errors.Wrap(err, "unable to measure the layer")
	}

	// Each read gets its own section of the file, so the layer can be read
	// several times, and concurrently, as a push does.
	layer, err := tarball.LayerFromOpener(
		func() (io.ReadCloser, error) {
			return io.NopCloser(io.NewSectionReader(file, 0, size)), nil
		},
		tarball.WithMediaType(types.OCILayer),
	)

	if err != nil {
		return nil, errors.Wrap(err, "unable to read back the layer")
	}

	built = true

	return &Layer{Layer: layer, file: file}, nil
}

// acceptEntries cleans the entries' paths, drops the skipped ones and checks
// every file source, then sorts what is left by path.
func acceptEntries(entries []LayerEntry) ([]layerItem, error) {
	items := make([]layerItem, 0, len(entries))

	var rejected []string

	for _, entry := range entries {
		cleaned, err := cleanLayerPath(entry.Path)

		if err != nil {
			return nil, err
		}

		if cleaned == "" || hasMacOSMetadataPart(cleaned) {
			continue
		}

		if !entry.Directory {
			info, err := os.Lstat(entry.Source)

			if err != nil {
				return nil, errors.Wrapf(err, "unable to read %s", entry.Source)
			}

			if problem := unpublishable(info); problem != "" {
				rejected = append(rejected, entry.Source+" is "+problem)
				continue
			}
		}

		items = append(items, layerItem{path: cleaned, entry: entry})
	}

	if len(rejected) != 0 {
		sort.Strings(rejected)

		return nil, errors.Errorf("these entries cannot be published: %s", strings.Join(rejected, ", "))
	}

	// A stable order keeps the layer digest reproducible. Paths are compared
	// without the slash a directory name gets, so a directory sorts before
	// what it holds.
	sort.SliceStable(items, func(i, j int) bool { return items[i].path < items[j].path })

	return items, nil
}

// CleanRelativePath puts a slash-separated path into its form relative to a
// root, reporting the root itself as empty. A leading slash also stands for
// the root. A path which leads outside the root is reported as not inside it,
// for the caller to refuse in its own words.
func CleanRelativePath(name string) (cleaned string, inside bool) {
	cleaned = path.Clean(strings.TrimLeft(name, "/"))

	if cleaned == "." {
		return "", true
	}

	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}

	return cleaned, true
}

// cleanLayerPath puts a path into its form in the layer, reporting the layer
// root as empty. A path which leaves the layer is refused.
func cleanLayerPath(name string) (string, error) {
	cleaned, inside := CleanRelativePath(name)

	if !inside {
		return "", errors.Errorf("the path %q is outside the layer", name)
	}

	return cleaned, nil
}

func hasMacOSMetadataPart(layerPath string) bool {
	for _, part := range strings.Split(layerPath, "/") {
		if IsMacOSMetadataFile(part) {
			return true
		}
	}

	return false
}

// unpublishable says what a file source is when it cannot be published, and
// returns nothing for an ordinary file. Symlinks, devices, sockets and named
// pipes cannot survive every way a layer is delivered, so they are refused
// rather than dropped.
func unpublishable(info os.FileInfo) string {
	mode := info.Mode()

	switch {
	case mode.IsRegular():
		// A hard link is an ordinary file as far as the mode is concerned, so
		// it is caught by its link count.
		if isHardLink(info) {
			return "a hard link"
		}

		return ""
	case mode&os.ModeSymlink != 0:
		return "a symbolic link"
	case mode&os.ModeDevice != 0:
		return "a device"
	case mode&os.ModeNamedPipe != 0:
		return "a named pipe"
	case mode&os.ModeSocket != 0:
		return "a socket"
	case mode.IsDir():
		return "a directory"
	}

	return "an unsupported file type"
}

// isHardLink reports a regular file with more than one directory entry
// pointing at it. Such a file would be published as an independent copy by
// some deliveries and dropped by others, so it is refused. Where the link
// count is not available the file is accepted, which is the behaviour on a
// filesystem that does not report one.
func isHardLink(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)

	if !ok {
		return false
	}

	return stat.Nlink > 1
}

// writeTar writes the accepted entries and then the appended files.
func writeTar(writer io.Writer, items []layerItem, appended []appendedFile, timestamp time.Time) error {
	tarWriter := tar.NewWriter(writer)

	for _, item := range items {
		header := entryHeader(item.path, timestamp)

		if item.entry.Directory {
			header.Typeflag = tar.TypeDir
			header.Name = item.path + "/"
			header.Mode = 0o755

			if err := tarWriter.WriteHeader(header); err != nil {
				return errors.Wrapf(err, "unable to write the directory %s", item.path)
			}

			continue
		}

		if err := writeFile(tarWriter, header, item.entry.Source); err != nil {
			return err
		}
	}

	for _, file := range appended {
		header := entryHeader(file.path, timestamp)
		header.Typeflag = tar.TypeReg
		header.Mode = 0o644
		header.Size = int64(len(file.contents))

		if err := tarWriter.WriteHeader(header); err != nil {
			return errors.Wrapf(err, "unable to write the header for %s", file.path)
		}

		if _, err := tarWriter.Write(file.contents); err != nil {
			return errors.Wrapf(err, "unable to write %s", file.path)
		}
	}

	if err := tarWriter.Close(); err != nil {
		return errors.Wrap(err, "unable to complete the layer")
	}

	return nil
}

// writeFile streams one file into the tar. The header takes the size and
// execute bits of the file as it is opened.
func writeFile(tarWriter *tar.Writer, header *tar.Header, source string) error {
	file, err := os.Open(source)

	if err != nil {
		return errors.Wrapf(err, "unable to read %s", source)
	}

	defer file.Close()

	info, err := file.Stat()

	if err != nil {
		return errors.Wrapf(err, "unable to read %s", source)
	}

	header.Typeflag = tar.TypeReg
	header.Size = info.Size()

	// Any execute bit in the source makes the file executable for everyone;
	// otherwise it is readable for everyone.
	if info.Mode()&0o111 != 0 {
		header.Mode = 0o755
	} else {
		header.Mode = 0o644
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return errors.Wrapf(err, "unable to write the header for %s", header.Name)
	}

	if _, err := io.CopyN(tarWriter, file, header.Size); err != nil {
		return errors.Wrapf(err, "unable to write %s", header.Name)
	}

	return nil
}

// entryHeader holds what every entry shares: root ownership, the fixed
// timestamp and the PAX format.
func entryHeader(name string, timestamp time.Time) *tar.Header {
	return &tar.Header{
		Name:    name,
		ModTime: timestamp,
		Uid:     0,
		Gid:     0,
		Uname:   "root",
		Gname:   "root",
		Format:  tar.FormatPAX,
	}
}
