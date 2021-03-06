// Code generated for package main by go-bindata DO NOT EDIT. (@generated)
// sources:
// static/stream.html
package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"net/http"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func bindataRead(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, gz)
	clErr := gz.Close()

	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}
	if clErr != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

type asset struct {
	bytes []byte
	info  os.FileInfo
}

type bindataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

// Name return file name
func (fi bindataFileInfo) Name() string {
	return fi.name
}

// Size return file size
func (fi bindataFileInfo) Size() int64 {
	return fi.size
}

// Mode return file mode
func (fi bindataFileInfo) Mode() os.FileMode {
	return fi.mode
}

// Mode return file modify time
func (fi bindataFileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir return file whether a directory
func (fi bindataFileInfo) IsDir() bool {
	return fi.mode&os.ModeDir != 0
}

// Sys return file is sys mode
func (fi bindataFileInfo) Sys() interface{} {
	return nil
}


type assetFile struct {
	*bytes.Reader
	name            string
	childInfos      []os.FileInfo
	childInfoOffset int
}

type assetOperator struct{}

// Open implement http.FileSystem interface
func (f *assetOperator) Open(name string) (http.File, error) {
	var err error
	if len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}
	content, err := Asset(name)
	if err == nil {
		return &assetFile{name: name, Reader: bytes.NewReader(content)}, nil
	}
	children, err := AssetDir(name)
	if err == nil {
		childInfos := make([]os.FileInfo, 0, len(children))
		for _, child := range children {
			childPath := filepath.Join(name, child)
			info, errInfo := AssetInfo(filepath.Join(name, child))
			if errInfo == nil {
				childInfos = append(childInfos, info)
			} else {
				childInfos = append(childInfos, newDirFileInfo(childPath))
			}
		}
		return &assetFile{name: name, childInfos: childInfos}, nil
	} else {
		// If the error is not found, return an error that will
		// result in a 404 error. Otherwise the server returns
		// a 500 error for files not found.
		if strings.Contains(err.Error(), "not found") {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
}

// Close no need do anything
func (f *assetFile) Close() error {
	return nil
}

// Readdir read dir's children file info
func (f *assetFile) Readdir(count int) ([]os.FileInfo, error) {
	if len(f.childInfos) == 0 {
		return nil, os.ErrNotExist
	}
	if count <= 0 {
		return f.childInfos, nil
	}
	if f.childInfoOffset+count > len(f.childInfos) {
		count = len(f.childInfos) - f.childInfoOffset
	}
	offset := f.childInfoOffset
	f.childInfoOffset += count
	return f.childInfos[offset : offset+count], nil
}

// Stat read file info from asset item
func (f *assetFile) Stat() (os.FileInfo, error) {
	if len(f.childInfos) != 0 {
		return newDirFileInfo(f.name), nil
	}
	return AssetInfo(f.name)
}

// newDirFileInfo return default dir file info
func newDirFileInfo(name string) os.FileInfo {
	return &bindataFileInfo{
		name:    name,
		size:    0,
		mode:    os.FileMode(2147484068), // equal os.FileMode(0644)|os.ModeDir
		modTime: time.Time{}}
}

// AssetFile return a http.FileSystem instance that data backend by asset
func AssetFile() http.FileSystem {
	return &assetOperator{}
}

var _staticStreamHtml = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\x94\x55\x61\x8b\xdc\x36\x10\xfd\xee\x5f\x31\x75\x5b\xd8\x10\x6a\x5f\x28\xfd\xb2\x91\x0d\xed\x35\x90\x96\xa4\x29\xdc\x41\x09\xa5\x10\x59\x9a\xb3\xd5\xc8\x1a\x23\xcd\xde\x76\x39\xf6\xbf\x17\x5b\xeb\xf5\xda\xe7\x5c\xaf\xfe\x70\xe7\x7d\x1e\xcd\xbc\xf7\x34\x1a\x89\xaf\x7e\xfe\x70\x7d\xfb\xf1\xf7\x37\xd0\x70\x6b\xcb\x44\xf4\xff\xc0\x4a\x57\x17\x29\xba\xb4\x4c\x12\xd1\xa0\xd4\x65\x02\x00\x20\xd8\xb0\xc5\xf2\x47\xa5\x30\x04\xb0\x54\x07\x08\xec\x51\xb6\xc6\xd5\x22\x8f\x1f\x63\x60\x50\xde\x74\x0c\x7c\xe8\xb0\x48\x19\xff\xe1\xfc\x6f\x79\x2f\x23\xda\x27\xed\x83\xf6\xc6\x69\xda\x67\xe4\x2c\x49\x0d\x05\xdc\xed\x9c\x62\x43\x0e\x36\x2f\xe0\x61\x88\xe8\x1f\x8b\xdc\x57\xb2\xc6\x61\x80\x02\x34\xa9\x5d\x8b\x8e\xb3\x1a\xf9\x8d\xc5\xfe\xf5\xa7\xc3\x2f\x7a\x93\x8e\x31\xe9\x8b\xd7\xc9\x79\xf1\x39\xa5\xec\x3a\x74\xfa\x1d\xd5\x1b\xc3\xd8\x5e\xe6\x1f\x6b\x68\xba\x51\x9e\xac\x85\xe2\x5c\x2e\x0b\x03\x72\x4b\x1d\x94\x4b\xf0\x2d\x9a\xba\x61\xf8\x6e\xc2\x95\x35\xe8\xf8\x8c\xbf\x7a\x3d\x2f\x31\x86\x45\x26\xd7\x8d\xb1\x3a\x72\x99\xc7\x99\x3b\xd8\x8c\x54\x96\x34\x67\x79\x26\x6e\x8f\x08\x3f\xcd\x6d\x5e\xef\x98\x4c\x6f\xc9\x25\x89\xb8\x3b\x7f\xa6\x7f\x60\x75\x43\xea\x33\x72\xfa\xd7\x9a\x6d\x8a\x9c\x83\x02\x1c\xee\xe1\x1c\xb9\x49\xf7\x61\x9b\xe7\x29\xbc\x9c\xb6\xcb\x92\x92\xfd\x4e\x64\x0d\x05\x86\x97\x90\xe6\xb1\x73\xd2\x85\xfe\x3e\x5f\x46\x4e\x59\x0a\x38\xeb\x09\xbc\xe7\x55\x3f\x90\xa1\xb7\xf1\xb2\x35\x94\x47\xc9\x78\xea\x8e\x4d\xaa\xcd\xfd\xb2\xca\x20\x92\xb1\xcd\x8c\x73\xe8\xdf\xde\xbe\x7f\x07\x05\xa4\xa2\x2a\xaf\xc9\x39\x8c\x15\x07\x0a\x3a\x13\x79\x55\xa6\x8f\x57\x2f\x1a\x6a\x61\xea\xaa\xa6\x16\x43\x90\xf5\xb3\x54\xb1\x3f\xac\xa0\xa3\xde\x36\xd4\x50\xc0\xaf\x37\x1f\x7e\xcb\x3a\xe9\x03\xf6\x59\x32\x2d\x59\xae\xa8\x7c\xa6\x47\xec\xd7\x2c\x5a\xb5\xe9\x93\x60\x5d\x7e\xf3\xd0\x86\x3a\x63\xd3\x62\x60\xd9\x76\x47\x91\xb3\x2e\xe3\x87\x91\x4c\xc4\x3e\xad\x67\x7d\xd2\xbe\xc1\x42\x50\x92\x55\x03\x1b\x5c\xf3\xe7\xf8\x25\xbb\x8f\x80\x36\xe0\x4a\x9f\xfe\xff\x1e\x59\xed\x8f\x8f\xb4\xf3\x50\x79\xda\x07\xf4\xa0\x09\x03\x38\x62\x08\xbb\xae\x23\xcf\xd3\x09\x08\x6b\x6d\xf3\x45\xcd\x51\xcd\xf1\x34\xb7\x44\x1e\xc7\xe4\x69\x4a\x8a\xc0\x07\x8b\x97\x93\x54\x85\x90\xc6\x31\x3b\xcc\xea\x49\x2b\xdd\xa3\xbf\xb3\xb4\xdf\x42\x63\xb4\x46\x17\x2b\x9c\xce\x75\x45\xfa\xf0\x9f\xb1\xfd\xd3\x49\xad\x8d\xab\xb7\x70\x35\x61\xad\xf4\xb5\x71\x33\x68\x6f\x34\x37\x5b\x78\x75\x75\xf5\xed\x04\x36\xc3\x84\x59\xa2\x95\x54\x9f\x6b\x4f\x3b\xa7\xb7\x50\x7b\x79\x98\xf1\xfa\xfa\x3c\xdc\x1f\x56\x17\xec\x1b\xc3\xf8\x24\x93\x89\x70\xf6\x03\xb6\x8f\xff\x5e\x44\x52\x30\xfd\xb9\xdb\x82\xac\x02\xd9\xdd\x65\x62\xa6\x6e\xbb\x8c\xb7\x78\xc7\x8f\x40\x1f\x25\x2e\xd0\x8a\x98\xa9\xdd\xc2\xf7\x97\xe0\x64\xb2\xdc\x31\xcd\x64\x8b\x7c\xd8\xd7\x32\x11\x79\xbc\x5f\x13\xd1\xef\xd0\x78\xcf\xca\xca\x22\x18\x5d\x4c\xd7\x5a\x79\xce\x2a\xd8\x97\x82\x9b\xf2\x76\x3c\x7d\x22\xe7\x66\x40\xde\xc7\x09\x13\x7f\xe7\xec\x4f\xd9\xf2\x21\x5d\x5f\x2a\x96\xe8\x6b\x0e\x97\xfd\xbf\x01\x00\x00\xff\xff\x03\x9c\x16\x21\xfd\x07\x00\x00")

func staticStreamHtmlBytes() ([]byte, error) {
	return bindataRead(
		_staticStreamHtml,
		"static/stream.html",
	)
}

func staticStreamHtml() (*asset, error) {
	bytes, err := staticStreamHtmlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "static/stream.html", size: 2045, mode: os.FileMode(420), modTime: time.Unix(1548364718, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("Asset %s can't read by error: %v", name, err)
		}
		return a.bytes, nil
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// MustAsset is like Asset but panics when Asset would return an error.
// It simplifies safe initialization of global variables.
func MustAsset(name string) []byte {
	a, err := Asset(name)
	if err != nil {
		panic("asset: Asset(" + name + "): " + err.Error())
	}

	return a
}

// AssetInfo loads and returns the asset info for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func AssetInfo(name string) (os.FileInfo, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("AssetInfo %s can't read by error: %v", name, err)
		}
		return a.info, nil
	}
	return nil, fmt.Errorf("AssetInfo %s not found", name)
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() (*asset, error){
	"static/stream.html": staticStreamHtml,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
		cannonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(cannonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for childName := range node.Children {
		rv = append(rv, childName)
	}
	return rv, nil
}

type bintree struct {
	Func     func() (*asset, error)
	Children map[string]*bintree
}

var _bintree = &bintree{nil, map[string]*bintree{
	"static": &bintree{nil, map[string]*bintree{
		"stream.html": &bintree{staticStreamHtml, map[string]*bintree{}},
	}},
}}

// RestoreAsset restores an asset under the given directory
func RestoreAsset(dir, name string) error {
	data, err := Asset(name)
	if err != nil {
		return err
	}
	info, err := AssetInfo(name)
	if err != nil {
		return err
	}
	err = os.MkdirAll(_filePath(dir, filepath.Dir(name)), os.FileMode(0755))
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(_filePath(dir, name), data, info.Mode())
	if err != nil {
		return err
	}
	err = os.Chtimes(_filePath(dir, name), info.ModTime(), info.ModTime())
	if err != nil {
		return err
	}
	return nil
}

// RestoreAssets restores an asset under the given directory recursively
func RestoreAssets(dir, name string) error {
	children, err := AssetDir(name)
	// File
	if err != nil {
		return RestoreAsset(dir, name)
	}
	// Dir
	for _, child := range children {
		err = RestoreAssets(dir, filepath.Join(name, child))
		if err != nil {
			return err
		}
	}
	return nil
}

func _filePath(dir, name string) string {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	return filepath.Join(append([]string{dir}, strings.Split(cannonicalName, "/")...)...)
}
