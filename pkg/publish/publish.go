package publish

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/toshi0607/jctl/pkg/gobuild"
	"golang.org/x/tools/go/packages"
)

type Publisher interface {
	Publish(v1.Image, string) (name.Reference, error)
}

type Namer func(string) string

type publisher struct {
	base     string
	rt       http.RoundTripper
	auth     authn.Authenticator
	namer    Namer
	insecure bool
}

var defaultTag = "latest"

func New() (Publisher, error) {
	base := os.Getenv("JCTL_DOCKER_REPO")
	repo, err := name.NewRepository(base)
	if err != nil {
		return nil, err
	}
	auth, err := authn.DefaultKeychain.Resolve(repo.Registry)
	if err != nil {
		return nil, err
	}
	if auth == authn.Anonymous {
		log.Println("No matching credentials were found, falling back on anonymous")
	}

	return &publisher{
		base:  base,
		rt:    http.DefaultTransport,
		auth:  auth,
		namer: packageWithMD5,
	}, nil

}

func (d *publisher) Publish(img v1.Image, s string) (name.Reference, error) {
	s = strings.ToLower(s)

	var os []name.Option
	if d.insecure {
		os = []name.Option{name.Insecure}
	}
	tag, err := name.NewTag(fmt.Sprintf("%s/%s:%s", d.base, d.namer(s), defaultTag), os...)
	if err != nil {
		return nil, err
	}

	if err := remote.Write(tag, img, remote.WithAuth(d.auth), remote.WithTransport(d.rt)); err != nil {
		return nil, err
	}

	h, err := img.Digest()
	if err != nil {
		return nil, err
	}
	dig, err := name.NewDigest(fmt.Sprintf("%s/%s@%s", d.base, d.namer(s), h))
	if err != nil {
		return nil, err
	}
	return &dig, nil
}

func packageWithMD5(importpath string) string {
	hasher := md5.New()
	hasher.Write([]byte(importpath))
	return filepath.Base(importpath) + "-" + hex.EncodeToString(hasher.Sum(nil))
}

func PublishImages(importpath string, pub Publisher, b gobuild.Builder) (name.Reference, error) {
	if isRelative(importpath) {
		var err error
		importpath, err = qualifyLocalImport(importpath)
		if err != nil {
			return nil, err
		}
	}

	// TDOO: builderに実装
	//if !b.IsSupportedReference(importpath) {
	//	return nil, fmt.Errorf("importpath %q is not supported", importpath)
	//}

	img, err := b.Build(importpath)
	if err != nil {
		return nil, fmt.Errorf("error building %q: %v", importpath, err)
	}
	ref, err := pub.Publish(img, importpath)
	if err != nil {
		return nil, fmt.Errorf("error publishing %s: %v", importpath, err)
	}
	return ref, nil
}

func isRelative(path string) bool {
	return path == "." || path == ".." ||
		strings.HasPrefix(path, "./") ||
		strings.HasPrefix(path, "../")
}

func qualifyLocalImport(path string) (string, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName,
	}
	pkgs, err := packages.Load(cfg, path)
	if err != nil {
		return "", err
	}
	if len(pkgs) != 1 {
		return "", fmt.Errorf("found %d local packages, expected 1", len(pkgs))
	}
	return pkgs[0].PkgPath, nil
}
