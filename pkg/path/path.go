package path

import (
	"encoding/json"
	"fmt"
	"go/build"
	"os/exec"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/tools/go/packages"
)

type Builder interface {
	Build() (string, error)
	ImportPackage(path string) (*build.Package, error)
}

type (
	module struct {
		Path string
		Dir  string
	}

	builder struct {
		origPath string
		mod      *module
	}
)

func (b *builder) Build() (string, error) {
	var importpath string
	if build.IsLocalImport(b.origPath) {
		var err error
		importpath, err = buildFullImport(b.origPath)
		if err != nil {
			return "", errors.Wrap(err, "faild to build ful import path")
		}
	} else {
		importpath = b.origPath
	}

	if !b.isSupportedReference(importpath) {
		return "", fmt.Errorf("importpath %q is not supported", b.origPath)
	}

	return importpath, nil
}

func NewBuilder(path string) Builder {
	return &builder{
		origPath: path,
		mod:      ModInfo(),
	}
}

func buildFullImport(path string) (string, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName,
	}
	pkgs, err := packages.Load(cfg, path)
	if err != nil {
		return "", err
	}
	if len(pkgs) != 1 {
		return "", fmt.Errorf("found %d pkgs, path(%s) should be unique", len(pkgs), path)
	}
	return pkgs[0].PkgPath, nil
}

// go list -mod=readonly -m -json
// {
//   "Path": "github.com/toshi0607/jctl",
//   "Main": true,
//   "Dir": "/Users/toshi0607/dev/go/src/github.com/toshi0607/jctl",
//   "GoMod": "/Users/toshi0607/dev/go/src/github.com/toshi0607/jctl/go.mod",
//   "GoVersion": "1.13"
// }
func ModInfo() *module {
	output, err := exec.Command("go", "list", "-mod=readonly", "-m", "-json").Output()
	if err != nil {
		return nil
	}
	var info module
	if err := json.Unmarshal(output, &info); err != nil {
		return nil
	}
	return &info
}

func (b *builder) isSupportedReference(path string) bool {
	p, err := b.ImportPackage(path)
	if err != nil {
		return false
	}
	return p.IsCommand()
}

func (b *builder) ImportPackage(path string) (*build.Package, error) {
	if b.mod == nil {
		return build.Import(path, build.Default.GOPATH, build.ImportComment)
	}

	if strings.HasPrefix(path, b.mod.Path) || build.IsLocalImport(path) {
		return build.Import(path, b.mod.Dir, build.ImportComment)
	}
	return nil, errors.New("unimportable pkg for gomodules")
}
