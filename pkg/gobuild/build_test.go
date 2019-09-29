package gobuild

import (
	"fmt"
	"testing"
)

//func build() (string, error) {
//	path := "../testdata/cmd/test"
//	tmpDir, err := ioutil.TempDir("", "jctl")
//	if err != nil {
//		return "", err
//	}
//	file := filepath.Join(tmpDir, "out")
//	args := make([]string, 0, 6)
//	args = append(args, "build")
//	args = append(args, "-o", file)
//	args = append(args, path)
//	cmd := exec.Command("go", args...)
//	defaultEnv := []string{
//		"CGO_ENABLED=0",
//		"GOOS=" + platform.OS,
//		"GOARCH=" + platform.Architecture,
//	}
//	cmd.Env = append(defaultEnv, os.Environ()...)
//}

func TestBuilder_Build(t *testing.T) {
	i := moduleInfo()
	fmt.Println(i)
}
