package gobuild

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	gb "go/build"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
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
}

type Module struct {
	Path string
	Dir  string
}

func MakeBuilder() (Builder, error) {
	return &builder{}, nil
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
