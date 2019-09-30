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
		"normal": {
			args:        []string{"path", "github.com/toshi0607/jctl/testdata/cmd/long_hello_world"},
			wantOutputs: []string{"job finished", "job created"},
			wantCode:    0,
		},
	}

	for _, te := range tests {
		te := te
		stream := new(bytes.Buffer)
		cli := New(stream, stream, "test")
		os.Args = te.args
		status := cli.Run()

		if status != te.wantCode {
			t.Errorf("ExitStatus=%d, want %d", status, te.wantCode)
		}
		for _, v := range te.wantOutputs {
			wantOut := fmt.Sprintf(v)
			if !strings.Contains(stream.String(), wantOut) {
				t.Errorf("[%s] actual: %s, want: %s", te.args[0], stream.String(), wantOut)
			}
		}
	}
}

// TODO: 出力先を全部書き換え
