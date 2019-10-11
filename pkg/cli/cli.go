package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jessevdk/go-flags"
	"github.com/pkg/errors"
	"github.com/toshi0607/jctl/pkg/build"
	"github.com/toshi0607/jctl/pkg/kubernetes"
	"github.com/toshi0607/jctl/pkg/path"
	"github.com/toshi0607/jctl/pkg/publish"
)

const defaultTimeoutSecond = 5 * time.Minute

type CLI interface {
	Run() int
}

type (
	cli struct {
		OutStream, ErrStream io.Writer
		Version              string
		Config               config
	}

	config struct {
		Namespace  string `short:"s" long:"namespace" default:"default" description:""`
		Version    bool   `short:"v" long:"version" description:"Show version"`
		Help       bool   `short:"h" long:"help" description:"Show this help message"`
		KubeConfig string `long:"kubeconfig" description:"absolute path to K8s credential"`
		TimeoutSec int    `short:"t" long:"timeoutsec" description:"timeout second"`
		TTLSec     int32  `long:"ttlsec" description:"TTLSecondsAfterFinished of Job. This is alpha feature since v1.12" default:"300"`
		Args       struct {
			Path string
		} `positional-args:"yes"`
	}
)

func New(outStream, errStream io.Writer, version string) CLI {
	return &cli{
		OutStream: outStream,
		ErrStream: errStream,
		Version:   version,
		Config:    config{},
	}
}

func (c *cli) initConfig() error {
	p := flags.NewParser(&c.Config, flags.None)
	_, err := p.Parse()
	if err != nil {
		return errors.Wrap(err, "failed to parse config")
	}

	if c.Config.Version {
		return fmt.Errorf("jctl version %s", c.Version)
	}

	if c.Config.Help || c.Config.Args.Path == "" {
		p.WriteHelp(c.ErrStream)
		return errors.New("")
	}

	return nil
}

func (c *cli) Run() int {
	ctx := context.Background()

	err := c.initConfig()
	if err != nil {
		fmt.Fprintln(c.ErrStream, err)
		return 1
	}
	timeout := defaultTimeoutSecond
	if c.Config.TimeoutSec != 0 {
		timeout = time.Duration(c.Config.TimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	builder, err := build.NewBuilder(c.OutStream)
	if err != nil {
		fmt.Fprintln(c.ErrStream, err)
		return 1
	}
	publisher, err := publish.New(c.OutStream)
	if err != nil {
		fmt.Fprintln(c.ErrStream, err)
		return 1
	}
	path, err := path.NewBuilder(c.Config.Args.Path).Build()
	if err != nil {
		fmt.Fprintln(c.ErrStream, err)
		return 1
	}

	fmt.Fprintln(c.OutStream, "building image...")
	img, err := builder.Build(path)
	if err != nil {
		fmt.Fprintln(c.ErrStream, errors.Wrapf(err, "failed to build image, path: %s", path))
		return 1
	}
	fmt.Fprintln(c.OutStream, "publishing image...")
	ref, err := publisher.Publish(img, path)
	if err != nil {
		fmt.Fprintln(c.ErrStream, errors.Wrapf(err, "failed to publish image, path: %s", path))
		return 1
	}

	k, err := kubernetes.New(c.OutStream, c.Config.Namespace, c.Config.KubeConfig, c.Config.TTLSec)
	if err != nil {
		fmt.Fprintln(c.ErrStream, err)
		return 1
	}

	err = k.Create(ctx, ref.Name())
	if err != nil {
		fmt.Fprintln(c.ErrStream, err)
		return 1
	}

	return 0
}
