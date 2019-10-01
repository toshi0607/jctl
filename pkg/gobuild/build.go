package gobuild

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

func Build(importpath, goos, goarch string) (string, error) {
	tmpDir, err := ioutil.TempDir("", "jctl")
	if err != nil {
		return "", err
	}
	file := filepath.Join(tmpDir, "out")

	args := make([]string, 0, 3)
	args = append(args, "build")
	args = append(args, "-o", file)
	args = append(args, importpath)
	cmd := exec.Command("go", args...)

	defaultEnv := []string{
		"CGO_ENABLED=0",
		"GOOS=" + goos,
		"GOARCH=" + goarch,
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
