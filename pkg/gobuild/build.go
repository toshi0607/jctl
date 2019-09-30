package gobuild

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	gb "go/build"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"golang.org/x/tools/go/packages"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// Where jctldataRoot lives in the image.
const jctldataRoot = "/var/app/jctl"

type Builder interface {
	Build(importpath string) (v1.Image, error)
}

type builder struct {
	baseImage    v1.Image
	creationTime v1.Time
	module       *Module
	// build builder Goアプリケーションをビルドするための関数
}

type Module struct {
	Path string
	Dir  string
}

func MakeBuilder() (Builder, error) {
	return &builder{
		module: moduleInfo(),
	}, nil
}

// go list -mod=readonly -m -json
// {
//   "Path": "github.com/toshi0607/jctl",
//   "Main": true,
//   "Dir": "/Users/toshi0607/dev/go/src/github.com/toshi0607/jctl",
//   "GoMod": "/Users/toshi0607/dev/go/src/github.com/toshi0607/jctl/go.mod",
//   "GoVersion": "1.13"
// }
func moduleInfo() *Module {
	output, err := exec.Command("go", "list", "-mod=readonly", "-m", "-json").Output()
	if err != nil {
		return nil
	}
	var info Module
	if err := json.Unmarshal(output, &info); err != nil {
		return nil
	}
	return &info
}

func (b *builder) Build(importpath string) (v1.Image, error) {
	base, err := getBaseImage(importpath)
	if err != nil {
		return nil, err
	}
	cf, err := base.ConfigFile()
	if err != nil {
		return nil, err
	}
	platform := v1.Platform{
		OS:           cf.OS,
		Architecture: cf.Architecture,
	}

	file, err := build(importpath, platform)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(filepath.Dir(file))

	var layers []mutate.Addendum
	dataLayerBuf, err := b.tarJctldata(importpath)
	if err != nil {
		return nil, err
	}
	dataLayerBytes := dataLayerBuf.Bytes()
	dataLayer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return ioutil.NopCloser(bytes.NewBuffer(dataLayerBytes)), nil
	})
	if err != nil {
		return nil, err
	}
	layers = append(layers, mutate.Addendum{
		Layer: dataLayer,
		History: v1.History{
			Author:    "jctl",
			CreatedBy: "jctl " + importpath,
			Comment:   "jctl contents, at $JCTL_DATA_PATH",
		},
	})
	appPath := filepath.Join(appDir, appFilename(importpath))
	// Construct a tarball with the binary and produce a layer.
	binaryLayerBuf, err := tarBinary(appPath, file)
	if err != nil {
		return nil, err
	}
	binaryLayerBytes := binaryLayerBuf.Bytes()
	binaryLayer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return ioutil.NopCloser(bytes.NewBuffer(binaryLayerBytes)), nil
	})
	if err != nil {
		return nil, err
	}
	layers = append(layers, mutate.Addendum{
		Layer: binaryLayer,
		History: v1.History{
			Author:    "jctl",
			CreatedBy: "jctl " + importpath,
			Comment:   "go build output, at " + appPath,
		},
	})
	// Augment the base image with our application layer.
	withApp, err := mutate.Append(base, layers...)
	if err != nil {
		return nil, err
	}
	// Start from a copy of the base image's config file, and set
	// the entrypoint to our app.
	cfg, err := withApp.ConfigFile()
	if err != nil {
		return nil, err
	}
	cfg = cfg.DeepCopy()
	cfg.Config.Entrypoint = []string{appPath}
	cfg.Config.Env = append(cfg.Config.Env, "JCTL_DATA_PATH="+jctldataRoot)
	// cfg.ContainerConfig = cfg.Config
	cfg.Author = "github.com/toshi0607/jctl"

	image, err := mutate.ConfigFile(withApp, cfg)
	if err != nil {
		return nil, err
	}

	empty := v1.Time{}
	if b.creationTime != empty {
		return mutate.CreatedAt(image, b.creationTime)
	}
	return image, nil
}

func tarBinary(name, binary string) (*bytes.Buffer, error) {
	buf := bytes.NewBuffer(nil)
	// Compress this before calling tarball.LayerFromOpener, since it eagerly
	// calculates digests and diffids. This prevents us from double compressing
	// the layer when we have to actually upload the blob.
	//
	// https://github.com/google/go-containerregistry/issues/413
	gw, _ := gzip.NewWriterLevel(buf, gzip.BestSpeed)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	// write the parent directories to the tarball archive
	if err := tarAddDirectories(tw, filepath.Dir(name)); err != nil {
		return nil, err
	}

	file, err := os.Open(binary)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	header := &tar.Header{
		Name:     name,
		Size:     stat.Size(),
		Typeflag: tar.TypeReg,
		// Use a fixed Mode, so that this isn't sensitive to the directory and umask
		// under which it was created. Additionally, windows can only set 0222,
		// 0444, or 0666, none of which are executable.
		Mode: 0555,
	}
	// write the header to the tarball archive
	if err := tw.WriteHeader(header); err != nil {
		return nil, err
	}
	// copy the file data to the tarball
	if _, err := io.Copy(tw, file); err != nil {
		return nil, err
	}

	return buf, nil
}

func tarAddDirectories(tw *tar.Writer, dir string) error {
	if dir == "." || dir == string(filepath.Separator) {
		return nil
	}

	// Write parent directories first
	if err := tarAddDirectories(tw, filepath.Dir(dir)); err != nil {
		return err
	}

	// write the directory header to the tarball archive
	if err := tw.WriteHeader(&tar.Header{
		Name:     dir,
		Typeflag: tar.TypeDir,
		// Use a fixed Mode, so that this isn't sensitive to the directory and umask
		// under which it was created. Additionally, windows can only set 0222,
		// 0444, or 0666, none of which are executable.
		Mode: 0555,
	}); err != nil {
		return err
	}

	return nil
}

func appFilename(importpath string) string {
	base := filepath.Base(importpath)

	// If we fail to determine a good name from the importpath then use a
	// safe default.
	if base == "." || base == string(filepath.Separator) {
		return defaultAppFilename
	}

	return base
}

const (
	appDir             = "/jctl-app"
	defaultAppFilename = "jctl-app"
)

func (g *builder) tarJctldata(importpath string) (*bytes.Buffer, error) {
	buf := bytes.NewBuffer(nil)
	gw, _ := gzip.NewWriterLevel(buf, gzip.BestSpeed)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	root, err := g.jctldataPath(importpath)
	if err != nil {
		return nil, err
	}

	return buf, walkRecursive(tw, root, jctldataRoot)
}

// Walk does not follow symbolic links.
func walkRecursive(tw *tar.Writer, root, chroot string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if path == root {
			// Add an entry for the root directory of our walk.
			return tw.WriteHeader(&tar.Header{
				Name:     chroot,
				Typeflag: tar.TypeDir,
				Mode:     0555,
			})
		}
		if err != nil {
			return err
		}
		// Skip other directories.
		if info.Mode().IsDir() {
			return nil
		}
		newPath := filepath.Join(chroot, path[len(root):])

		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}

		// Chase symlinks.
		info, err = os.Stat(path)
		if err != nil {
			return err
		}
		// Skip other directories.
		if info.Mode().IsDir() {
			return walkRecursive(tw, path, newPath)
		}

		// Open the file to copy it into the tarball.
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		// Copy the file into the image tarball.
		if err := tw.WriteHeader(&tar.Header{
			Name:     newPath,
			Size:     info.Size(),
			Typeflag: tar.TypeReg,
			Mode:     0555,
		}); err != nil {
			return err
		}
		_, err = io.Copy(tw, file)
		return err
	})
}

func (g *builder) jctldataPath(s string) (string, error) {
	p, err := g.importPackage(s)
	if err != nil {
		return "", err
	}
	return filepath.Join(p.Dir, "jctldata"), nil
}

func (g *builder) importPackage(s string) (*gb.Package, error) {
	if g.module == nil {
		return gb.Import(s, gb.Default.GOPATH, gb.ImportComment)
	}
	if strings.HasPrefix(s, g.module.Path) || gb.IsLocalImport(s) {
		return gb.Import(s, g.module.Dir, gb.ImportComment)
	}

	return nil, errors.New("unmatched importPackage with gomodules")
}

func getBaseImage(s string) (v1.Image, error) {
	ref, err := name.ParseReference("gcr.io/distroless/static:latest")
	if err != nil {
		return nil, err
	}
	return remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
}

func build(ip string, platform v1.Platform) (string, error) {
	tmpDir, err := ioutil.TempDir("", "ko")
	if err != nil {
		return "", err
	}
	file := filepath.Join(tmpDir, "out")

	args := make([]string, 0, 3)
	args = append(args, "build")
	args = append(args, "-o", file)
	args = append(args, ip)
	cmd := exec.Command("go", args...)

	// Last one wins
	defaultEnv := []string{
		"CGO_ENABLED=0",
		"GOOS=" + platform.OS,
		"GOARCH=" + platform.Architecture,
	}
	cmd.Env = append(defaultEnv, os.Environ()...)

	var output bytes.Buffer
	cmd.Stderr = &output
	cmd.Stdout = &output

	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		return "", err
	}
	return file, nil
}

// baseimgageの参照
// remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))

type Publisher interface {
	Publish(v1.Image, string) (name.Reference, error)
}

type Namer func(string) string

type demon struct {
	namer Namer
	tags  []string
}

type defaultPublisher struct {
	base     string
	t        http.RoundTripper
	auth     authn.Authenticator
	namer    Namer
	tags     []string
	insecure bool
}

const (
	// LocalDomain is a sentinel "registry" that represents side-loading images into the daemon.
	LocalDomain = "ko.local"
)

// local only
func MakePublisher() (Publisher, error) {
	namer := packageWithMD5
	repoName := os.Getenv("JCTL_DOCKER_REPO")
	if repoName == LocalDomain { // or local flag
		return NewDaemon(namer, []string{"latest"}), nil
	}
	if repoName == "" {
		return nil, errors.New("KO_DOCKER_REPO environment variable is unset")
	}
	_, err := name.NewRepository(repoName)
	if err != nil {
		return nil, fmt.Errorf("failed to parse environment variable KO_DOCKER_REPO=%q as repository: %v", repoName, err)
	}

	return nil, nil
}

var defaultTags = []string{"latest"}

func NewDefault() (Publisher, error) {
	base := os.Getenv("JCTL_DOCKER_REPO")
	repo, err := name.NewRepository(base)
	if err != nil {
		return nil, err
	}
	// keysはauthn.Keychain
	auth, err := authn.DefaultKeychain.Resolve(repo.Registry)
	if err != nil {
		return nil, err
	}
	if auth == authn.Anonymous {
		log.Println("No matching credentials were found, falling back on anonymous")
	}

	return &defaultPublisher{
		base:  base,
		t:     http.DefaultTransport,
		auth:  auth,
		namer: packageWithMD5,
		tags:  defaultTags,
	}, nil

}

func identity(in string) string { return in }

func (d *defaultPublisher) Publish(img v1.Image, s string) (name.Reference, error) {
	// https://github.com/google/go-containerregistry/issues/212
	s = strings.ToLower(s)

	// tag latest 1回で固定
	for _, tagName := range d.tags {

		var os []name.Option
		if d.insecure {
			os = []name.Option{name.Insecure}
		}
		tag, err := name.NewTag(fmt.Sprintf("%s/%s:%s", d.base, d.namer(s), tagName), os...)
		if err != nil {
			return nil, err
		}

		// TODO: This is slow because we have to load the image multiple times.
		// Figure out some way to publish the manifest with another tag.
		if err := remote.Write(tag, img, remote.WithAuth(d.auth), remote.WithTransport(d.t)); err != nil {
			return nil, err
		}
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

func NewDaemon(namer Namer, tags []string) Publisher {
	return &demon{namer, tags}
}

// Publish implements publish.Interface
// add log
func (d *demon) Publish(img v1.Image, s string) (name.Reference, error) {
	// https://github.com/google/go-containerregistry/issues/212
	s = strings.ToLower(s)

	h, err := img.Digest()
	if err != nil {
		return nil, err
	}

	digestTag, err := name.NewTag(fmt.Sprintf("%s/%s:%s", LocalDomain, d.namer(s), h.Hex))
	if err != nil {
		return nil, err
	}

	if _, err := daemon.Write(digestTag, img); err != nil {
		return nil, err
	}

	// tag latest 1回で固定
	for _, tagName := range d.tags {
		tag, err := name.NewTag(fmt.Sprintf("%s/%s:%s", LocalDomain, d.namer(s), tagName))
		if err != nil {
			return nil, err
		}

		err = daemon.Tag(digestTag, tag)

		if err != nil {
			return nil, err
		}
	}

	return &digestTag, nil
}

func PublishImages(importpath string, pub Publisher, b Builder) (name.Reference, error) {
	if IsLocalImport(importpath) {
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

func IsLocalImport(path string) bool {
	return path == "." || path == ".." ||
		strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../")
}

func qualifyLocalImport(importpath string) (string, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName,
	}
	pkgs, err := packages.Load(cfg, importpath)
	if err != nil {
		return "", err
	}
	if len(pkgs) != 1 {
		return "", fmt.Errorf("found %d local packages, expected 1", len(pkgs))
	}
	return pkgs[0].PkgPath, nil
}
