package cli

import (
	"fmt"
	"io"
	"log"

	"github.com/jessevdk/go-flags"
	"github.com/pkg/errors"
	"github.com/toshi0607/jctl/pkg/gobuild"
	"github.com/toshi0607/jctl/pkg/kubernetes"
)

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
		return errors.Wrapf(err, "failed to parse. Config: %s", &c.Config)
	}

	if c.Config.Version {
		return fmt.Errorf("gig version %s", c.Version)
	}

	if c.Config.Help || c.Config.Args.Path == "" {
		p.WriteHelp(c.ErrStream)
		return errors.New("")
	}

	return nil
}

func (c *cli) Run() int {
	err := c.initConfig()
	if err != nil {
		fmt.Fprintln(c.ErrStream, err)
		return 1
	}

	builder, err := gobuild.MakeBuilder()
	if err != nil {
		log.Fatal(err)
	}
	publisher, err := gobuild.NewDefault()
	if err != nil {
		log.Fatal(err)
	}
	ref, err := gobuild.PublishImages(c.Config.Args.Path, publisher, builder)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("published")

	k, err := kubernetes.New(c.OutStream, c.ErrStream, c.Config.Namespace, c.Config.KubeConfig)
	if err != nil {
		log.Fatal(err)
	}

	err = k.Create(ref.Name())
	if err != nil {
		log.Fatal(err)
	}

	return 0
}
