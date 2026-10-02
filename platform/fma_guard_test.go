package platform

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestPlatformAssemblyHasNoFMA keeps the platform's frozen floating-point
// calculations bit-identical on amd64 and arm64. Go permits fused
// multiply-add contractions, so explicit float64 conversions in production
// code must keep every intermediate product rounded.
func TestPlatformAssemblyHasNoFMA(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("Go toolchain is unavailable; cannot inspect platform assembly")
	}

	for _, check := range []struct {
		arch     string
		mnemonic []string
	}{
		{arch: "arm64", mnemonic: []string{"FMADD", "FMSUB", "FNMADD", "FNMSUB"}},
		{arch: "amd64", mnemonic: []string{"VFMADD", "VFMSUB"}},
	} {
		t.Run(check.arch, func(t *testing.T) {
			command := exec.Command(goTool, "build", "-gcflags=-S", ".")
			command.Env = append(os.Environ(), "GOARCH="+check.arch, "CGO_ENABLED=0")
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("cross-build GOARCH=%s: %v\n%s", check.arch, err, output)
			}
			for _, line := range strings.Split(string(output), "\n") {
				for _, mnemonic := range check.mnemonic {
					if strings.Contains(line, mnemonic) {
						t.Fatalf("GOARCH=%s emitted forbidden fused multiply-add %s:\n%s", check.arch, mnemonic, line)
					}
				}
			}
		})
	}
}
