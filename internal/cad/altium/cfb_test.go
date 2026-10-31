package altium

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A compound file with storages, small and big streams reads back with
// olefile, a strict reader, when it is installed.
func TestCompoundFile(t *testing.T) {
	root := NewStorage()
	root.Stream("FileHeader", []byte("hello"))
	lib := root.Storage("Library")
	lib.Stream("Header", []byte{1, 0, 0, 0})
	big := bytes.Repeat([]byte("0123456789"), 1000)
	lib.Stream("Data", big)
	for i := 0; i < 20; i++ {
		s := root.Storage(fmt.Sprintf("FP%02d", i))
		s.Stream("Data", bytes.Repeat([]byte{byte(i)}, 100+i*37))
		s.Stream("Empty", nil)
	}
	b, err := root.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if len(b)%512 != 0 {
		t.Errorf("%d bytes isn't whole sectors", len(b))
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3")
	}
	if exec.Command(py, "-c", "import olefile").Run() != nil {
		t.Skip("olefile isn't installed")
	}
	f := filepath.Join(t.TempDir(), "t.cfb")
	_ = os.WriteFile(f, b, 0o644)
	out, err := exec.Command(py, "-c", `
import olefile, sys
o = olefile.OleFileIO(sys.argv[1], raise_defects=olefile.DEFECT_INCORRECT)
assert o.openstream('FileHeader').read() == b'hello'
assert o.openstream('Library/Data').read() == b'0123456789' * 1000
for i in range(20):
    assert o.openstream('FP%02d/Data' % i).read() == bytes([i]) * (100 + i * 37), i
    assert o.openstream('FP%02d/Empty' % i).read() == b''
print(len(o.listdir()))
`, f).CombinedOutput()
	if err != nil {
		t.Fatalf("olefile: %v\n%s", err, out)
	}
	if string(bytes.TrimSpace(out)) != "43" {
		t.Errorf("olefile listed %s streams", out)
	}
}
