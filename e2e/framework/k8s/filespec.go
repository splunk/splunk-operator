package k8s

import (
	"path"
	"path/filepath"
	"strings"
)

type pathSpec interface {
	String() string
}

// localPath represents a client-native path.
type localPath struct {
	file string
}

func newLocalPath(fileName string) localPath {
	file := stripTrailingSlash(fileName)
	return localPath{file: file}
}

func (p localPath) String() string {
	return p.file
}

func (p localPath) Dir() localPath {
	return newLocalPath(filepath.Dir(p.file))
}

func (p localPath) Base() localPath {
	return newLocalPath(filepath.Base(p.file))
}

func (p localPath) Clean() localPath {
	return newLocalPath(filepath.Clean(p.file))
}

func (p localPath) Join(elem pathSpec) localPath {
	return newLocalPath(filepath.Join(p.file, elem.String()))
}

func (p localPath) Glob() (matches []string, err error) {
	return filepath.Glob(p.file)
}

func (p localPath) StripSlashes() localPath {
	return newLocalPath(stripLeadingSlash(p.file))
}

// remotePath represents a unix path.
type remotePath struct {
	file string
}

func newRemotePath(fileName string) remotePath {
	file := strings.ReplaceAll(stripTrailingSlash(fileName), `\`, "/")
	return remotePath{file: file}
}

func (p remotePath) String() string {
	return p.file
}

func (p remotePath) Dir() remotePath {
	return newRemotePath(path.Dir(p.file))
}

func (p remotePath) Base() remotePath {
	return newRemotePath(path.Base(p.file))
}

func (p remotePath) Clean() remotePath {
	return newRemotePath(path.Clean(p.file))
}

func (p remotePath) Join(elem pathSpec) remotePath {
	return newRemotePath(path.Join(p.file, elem.String()))
}

func (p remotePath) StripShortcuts() remotePath {
	p = p.Clean()
	return newRemotePath(stripPathShortcuts(p.file))
}

func (p remotePath) StripSlashes() remotePath {
	return newRemotePath(stripLeadingSlash(p.file))
}

func stripTrailingSlash(file string) string {
	if len(file) == 0 {
		return file
	}
	if file != "/" && strings.HasSuffix(string(file[len(file)-1]), "/") {
		return file[:len(file)-1]
	}
	return file
}

func stripLeadingSlash(file string) string {
	return strings.TrimLeft(file, `/\`)
}

func stripPathShortcuts(p string) string {
	newPath := p
	trimmed := strings.TrimPrefix(newPath, "../")

	for trimmed != newPath {
		newPath = trimmed
		trimmed = strings.TrimPrefix(newPath, "../")
	}

	if newPath == "." || newPath == ".." {
		newPath = ""
	}

	if len(newPath) > 0 && string(newPath[0]) == "/" {
		return newPath[1:]
	}

	return newPath
}
