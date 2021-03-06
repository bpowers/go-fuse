package pathfs

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/bpowers/go-fuse/fuse"
	"github.com/bpowers/go-fuse/fuse/nodefs"
)

var xattrGolden = map[string][]byte{
	"user.attr1": []byte("val1"),
	"user.attr2": []byte("val2")}
var xattrFilename = "filename"

type XAttrTestFs struct {
	tester   *testing.T
	filename string
	attrs    map[string][]byte

	FileSystem
}

func NewXAttrFs(nm string, m map[string][]byte) *XAttrTestFs {
	x := &XAttrTestFs{
		filename:   nm,
		attrs:      make(map[string][]byte, len(m)),
		FileSystem: NewDefaultFileSystem(),
	}

	for k, v := range m {
		x.attrs[k] = v
	}
	return x
}

func (fs *XAttrTestFs) GetAttr(name string, context *fuse.Context) (*fuse.Attr, fuse.Status) {
	a := &fuse.Attr{}
	if name == "" || name == "/" {
		a.Mode = fuse.S_IFDIR | 0700
		return a, fuse.OK
	}
	if name == fs.filename {
		a.Mode = fuse.S_IFREG | 0600
		return a, fuse.OK
	}
	return nil, fuse.ENOENT
}

func (fs *XAttrTestFs) SetXAttr(name string, attr string, data []byte, flags int, context *fuse.Context) fuse.Status {
	fs.tester.Log("SetXAttr", name, attr, string(data), flags)
	if name != fs.filename {
		return fuse.ENOENT
	}
	dest := make([]byte, len(data))
	copy(dest, data)
	fs.attrs[attr] = dest
	return fuse.OK
}

func (fs *XAttrTestFs) GetXAttr(name string, attr string, context *fuse.Context) ([]byte, fuse.Status) {
	if name != fs.filename {
		return nil, fuse.ENOENT
	}
	v, ok := fs.attrs[attr]
	if !ok {
		return nil, fuse.ENODATA
	}
	fs.tester.Log("GetXAttr", string(v))
	return v, fuse.OK
}

func (fs *XAttrTestFs) ListXAttr(name string, context *fuse.Context) (data []string, code fuse.Status) {
	if name != fs.filename {
		return nil, fuse.ENOENT
	}

	for k := range fs.attrs {
		data = append(data, k)
	}
	return data, fuse.OK
}

func (fs *XAttrTestFs) RemoveXAttr(name string, attr string, context *fuse.Context) fuse.Status {
	if name != fs.filename {
		return fuse.ENOENT
	}
	_, ok := fs.attrs[attr]
	fs.tester.Log("RemoveXAttr", name, attr, ok)
	if !ok {
		return fuse.ENODATA
	}
	delete(fs.attrs, attr)
	return fuse.OK
}

func readXAttr(p, a string) (val []byte, err error) {
	val = make([]byte, 1024)
	return getXAttr(p, a, val)
}

func xattrTestCase(t *testing.T, nm string) (mountPoint string, cleanup func()) {
	xfs := NewXAttrFs(nm, xattrGolden)
	xfs.tester = t
	mountPoint, err := ioutil.TempDir("", "go-fuse-xattr_test")
	if err != nil {
		t.Fatalf("TempDir failed: %v", err)
	}

	nfs := NewPathNodeFs(xfs, nil)
	state, _, err := nodefs.MountRoot(mountPoint, nfs.Root(), nil)
	if err != nil {
		t.Fatalf("TempDir failed: %v", err)
	}
	state.SetDebug(VerboseTest())

	go state.Serve()
	return mountPoint, func() {
		state.Unmount()
		os.RemoveAll(mountPoint)
	}
}

func TestXAttrNoExist(t *testing.T) {
	nm := xattrFilename
	mountPoint, clean := xattrTestCase(t, nm)
	defer clean()

	mounted := filepath.Join(mountPoint, nm)
	_, err := os.Lstat(mounted)
	if err != nil {
		t.Error("Unexpected stat error", err)
	}

	val, err := readXAttr(mounted, "noexist")
	if err == nil {
		t.Error("Expected GetXAttr error", val)
	}
}

func TestXAttrRead(t *testing.T) {
	nm := xattrFilename
	mountPoint, clean := xattrTestCase(t, nm)
	defer clean()

	mounted := filepath.Join(mountPoint, nm)
	attrs, err := listXAttr(mounted)
	readback := make(map[string][]byte)
	if err != nil {
		t.Error("Unexpected ListXAttr error", err)
	} else {
		for _, a := range attrs {
			val, err := readXAttr(mounted, a)
			if err != nil {
				t.Errorf("GetXAttr(%q) failed: %v", a, err)
			}
			readback[a] = val
		}
	}

	if len(readback) != len(xattrGolden) {
		t.Error("length mismatch", xattrGolden, readback)
	} else {
		for k, v := range readback {
			if bytes.Compare(xattrGolden[k], v) != 0 {
				t.Error("val mismatch", k, v, xattrGolden[k])
			}
		}
	}

	err = sysSetxattr(mounted, "third", []byte("value"), 0)
	if err != nil {
		t.Error("Setxattr error", err)
	}
	val, err := readXAttr(mounted, "third")
	if err != nil || string(val) != "value" {
		t.Error("Read back set xattr:", err, string(val))
	}

	sysRemovexattr(mounted, "third")
	val, err = readXAttr(mounted, "third")
	if err != syscall.ENODATA {
		t.Error("Data not removed?", err, val)
	}
}
