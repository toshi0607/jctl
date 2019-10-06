// +build e2e

package cli

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestCli_Run(t *testing.T) {
	tests := map[string]struct {
		args        []string
		wantOutputs []string
		wantCode    int
	}{
		"with import path": {
			args:        []string{"path", "github.com/toshi0607/jctl/testdata/cmd/hello_world"},
			wantOutputs: []string{"job finished", "job created"},
			wantCode:    0,
		},
		"with relative path": {
			args:        []string{"path", "../../testdata/cmd/hello_world"},
			wantOutputs: []string{"job finished", "job created"},
			wantCode:    0,
		},
		"with timeout option": {
			args:        []string{"path", "github.com/toshi0607/jctl/testdata/cmd/long_hello_world", "-t", "5"},
			wantOutputs: []string{"job execution timeout", "job created"},
			wantCode:    1,
		},
		"with version": {
			args:        []string{"path", "-v"},
			wantOutputs: []string{"jctl version"},
			wantCode:    1,
		},
		"with invalid flag": {
			args:        []string{"path", "-foo"},
			wantOutputs: []string{"failed to parse config"},
			wantCode:    1,
		},
	}

	for name, te := range tests {
		te := te
		stream := new(bytes.Buffer)
		cli := New(stream, stream, "test")
		os.Args = te.args
		status := cli.Run()

		if status != te.wantCode {
			t.Errorf("[%s] status got: %d, want: %d", name, status, te.wantCode)
		}
		for _, v := range te.wantOutputs {
			wantOut := fmt.Sprintf(v)
			if !strings.Contains(stream.String(), wantOut) {
				t.Errorf("[%s] got: %s, want: %s", name, stream.String(), wantOut)
			}
		}
	}
}
