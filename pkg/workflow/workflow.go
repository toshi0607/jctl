package workflow

import (
	"context"
	"io"
	"log"

	"github.com/pkg/errors"
	"github.com/toshi0607/jctl/pkg/build"
	"github.com/toshi0607/jctl/pkg/path"
	"github.com/toshi0607/jctl/pkg/publish"
)

type Workflow interface {
	Execute(ctx context.Context, importpath string) (string, error)
}

type workflow struct {
	log *log.Logger
}

func New(outStream io.Writer) Workflow {
	log := log.New(outStream, "workflow: ", log.LstdFlags)
	return &workflow{log: log}
}

func (w *workflow) Execute(ctx context.Context, importpath string) (string, error) {
	builder, err := build.NewBuilder(w.log)
	if err != nil {
		return "", err
	}
	publisher, err := publish.New(w.log)
	if err != nil {
		return "", err
	}
	path, err := path.NewBuilder(importpath).Build()
	if err != nil {
		return "", err
	}

	w.log.Println("building image...")
	img, err := builder.Build(path)
	if err != nil {
		return "", errors.Wrapf(err, "failed to build image, path: %s", path)
	}
	w.log.Println("publishing image...")
	ref, err := publisher.Publish(img, path)
	if err != nil {
		return "", errors.Wrapf(err, "failed to publish image, path: %s", path)
	}
	return ref.Name(), nil
}
