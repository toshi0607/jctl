package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/pkg/errors"
	"github.com/toshi0607/jctl/pkg/gobuild"
	"github.com/toshi0607/jctl/pkg/path"
)

const (
	appDir               = "/jctl-app"
	defaultAppFilename   = "jctl-app"
	jctlDataRoot         = "/var/app/jctl"
	defaultBaseImagePath = "gcr.io/distroless/static:latest"
	author               = "github.com/toshi0607/jctl"
	modeReadExec         = 0555
)

type Builder interface {
	Build(importpath string) (v1.Image, error)
}

type builder struct {
	log          *log.Logger
	baseImage    v1.Image
	creationTime v1.Time
}

func NewBuilder(log *log.Logger) (Builder, error) {
	log.SetPrefix("build: ")
	base, err := getBaseImage()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get base image")
	}
	return &builder{
		log:          log,
		baseImage:    base,
		creationTime: v1.Time{Time: time.Now()},
	}, nil
}

func (b *builder) Build(path string) (v1.Image, error) {
	cf, err := b.baseImage.ConfigFile()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get config file")
	}
	platform := v1.Platform{
		OS:           cf.OS,
		Architecture: cf.Architecture,
	}

	file, err := gobuild.Build(path, platform.OS, platform.Architecture)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to build Go app, path: %s", path)
	}
	defer os.RemoveAll(filepath.Dir(file))

	var layers []mutate.Addendum
	dataLayerBuf, err := b.tarJctldata(path)
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
			Author:    author,
			CreatedBy: "jctl " + path,
			Comment:   "jctl contents, at $JCTL_DATA_PATH",
		},
	})
	appPath := filepath.Join(appDir, appFileName(path))
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
			Author:    author,
			CreatedBy: "jctl " + path,
			Comment:   "go build output, at " + appPath,
		},
	})
	withApp, err := mutate.Append(b.baseImage, layers...)
	if err != nil {
		return nil, err
	}
	cfg, err := withApp.ConfigFile()
	if err != nil {
		return nil, err
	}
	cfg = cfg.DeepCopy()
	cfg.Config.Entrypoint = []string{appPath}
	cfg.Config.Env = append(cfg.Config.Env, "JCTL_DATA_PATH="+jctlDataRoot)
	cfg.Author = author

	image, err := mutate.ConfigFile(withApp, cfg)
	if err != nil {
		return nil, errors.New("failed to get config file")
	}

	return mutate.CreatedAt(image, b.creationTime)
}

func tarBinary(name, binary string) (*bytes.Buffer, error) {
	buf := bytes.NewBuffer(nil)
	gw, err := gzip.NewWriterLevel(buf, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

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
		Mode:     modeReadExec,
	}
	if err := tw.WriteHeader(header); err != nil {
		return nil, err
	}
	if _, err := io.Copy(tw, file); err != nil {
		return nil, err
	}

	return buf, nil
}

func tarAddDirectories(tw *tar.Writer, dir string) error {
	if dir == "." || dir == string(filepath.Separator) {
		return nil
	}
	if err := tarAddDirectories(tw, filepath.Dir(dir)); err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{
		Name:     dir,
		Typeflag: tar.TypeDir,
		Mode:     modeReadExec,
	}); err != nil {
		return err
	}

	return nil
}

func appFileName(importpath string) string {
	base := filepath.Base(importpath)
	if base == "." || base == string(filepath.Separator) {
		return defaultAppFilename
	}
	return base
}

func (g *builder) tarJctldata(importpath string) (*bytes.Buffer, error) {
	buf := bytes.NewBuffer(nil)
	gw, err := gzip.NewWriterLevel(buf, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	root, err := g.jctlDataPath(importpath)
	if err != nil {
		return nil, err
	}

	return buf, walkRecursive(tw, root, jctlDataRoot)
}

// respect google/ko
func walkRecursive(tw *tar.Writer, root, chroot string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if path == root {
			return tw.WriteHeader(&tar.Header{
				Name:     chroot,
				Typeflag: tar.TypeDir,
				Mode:     modeReadExec,
			})
		}
		if err != nil {
			return err
		}
		if info.Mode().IsDir() {
			return nil
		}
		newPath := filepath.Join(chroot, path[len(root):])

		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}

		info, err = os.Stat(path)
		if err != nil {
			return err
		}

		if info.Mode().IsDir() {
			return walkRecursive(tw, path, newPath)
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		if err := tw.WriteHeader(&tar.Header{
			Name:     newPath,
			Size:     info.Size(),
			Typeflag: tar.TypeReg,
			Mode:     modeReadExec,
		}); err != nil {
			return err
		}
		_, err = io.Copy(tw, file)
		return err
	})
}

func (g *builder) jctlDataPath(importpath string) (string, error) {
	b := path.NewBuilder(importpath)
	p, err := b.ImportPackage(importpath)
	if err != nil {
		return "", err
	}
	return filepath.Join(p.Dir, "jctldata"), nil
}

func getBaseImage() (v1.Image, error) {
	ref, err := name.ParseReference(defaultBaseImagePath)
	if err != nil {
		return nil, err
	}
	return remote.Image(ref, remote.WithAuthFromKeychain(authn.DefaultKeychain))
}
