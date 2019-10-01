package publish

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
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
	"github.com/toshi0607/jctl/pkg/build"
	"github.com/toshi0607/jctl/pkg/path"
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
	repoName := os.Getenv("JCTL_DOCKER_REPO")
	if repoName == "" {
		return nil, errors.New("JCTL_DOCKER_REPO environment variable is required")
	}
	repo, err := name.NewRepository(repoName)
	if err != nil {
		return nil, err
	}
	auth, err := authn.DefaultKeychain.Resolve(repo.Registry)
	if err != nil {
		return nil, err
	}
	if auth == authn.Anonymous {
		log.Println("no credentials matched, fall back on anonymous")
	}

	return &publisher{
		base:  repoName,
		rt:    http.DefaultTransport,
		auth:  auth,
		namer: packageWithMD5,
	}, nil

}

func (d *publisher) Publish(img v1.Image, path string) (name.Reference, error) {
	path = strings.ToLower(path)

	var os []name.Option
	if d.insecure {
		os = []name.Option{name.Insecure}
	}
	tag, err := name.NewTag(fmt.Sprintf("%s/%s:%s", d.base, d.namer(path), defaultTag), os...)
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
	dig, err := name.NewDigest(fmt.Sprintf("%s/%s@%s", d.base, d.namer(path), h))
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

func PublishImages(importpath string, pub Publisher, b build.Builder) (name.Reference, error) {
	path, err := path.NewBuilder(importpath).Build()
	if err != nil {
		return nil, err
	}

	img, err := b.Build(path)
	if err != nil {
		return nil, fmt.Errorf("error building %q: %v", path, err)
	}
	ref, err := pub.Publish(img, path)
	if err != nil {
		return nil, fmt.Errorf("error publishing %s: %v", path, err)
	}
	return ref, nil
}
