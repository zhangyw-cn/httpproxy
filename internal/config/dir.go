package config

import (
	"fmt"
	"os"
	"path/filepath"
)

type Dir struct {
	path string
}

func (d *Dir) Path() string {
	return d.path
}

func Open(workdir string) (*Dir, Values, error) {
	abs, err := filepath.Abs(workdir)
	if err != nil {
		return nil, Values{}, err
	}
	path := filepath.Join(abs, ".httpproxy")
	info, err := os.Lstat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, Values{}, err
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			return nil, Values{}, err
		}
	} else if !info.IsDir() {
		return nil, Values{}, fmt.Errorf("%s is not a directory", path)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return nil, Values{}, err
	}

	gi := filepath.Join(path, ".gitignore")
	if _, err := os.Lstat(gi); err != nil {
		if !os.IsNotExist(err) {
			return nil, Values{}, err
		}
		if err := os.WriteFile(gi, []byte("*\n"), 0o644); err != nil {
			return nil, Values{}, err
		}
	}

	d := &Dir{path: path}
	cfg := filepath.Join(path, "config.yaml")
	data, err := os.ReadFile(cfg)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, Values{}, err
		}
		v := Defaults()
		if err := d.Save(v); err != nil {
			return nil, Values{}, err
		}
		return d, v, nil
	}
	v, err := ParseYAML(data)
	if err != nil {
		return nil, Values{}, err
	}
	return d, v, nil
}

func (d *Dir) Save(v Values) error {
	data := MarshalYAML(v)
	if _, err := ParseYAML(data); err != nil {
		return err
	}
	f, err := os.CreateTemp(d.path, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	remove := true
	defer func() {
		if remove {
			os.Remove(tmp)
		}
	}()
	if err := os.Chmod(tmp, 0o644); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(d.path, "config.yaml")); err != nil {
		return err
	}
	remove = false
	return nil
}

func ChmodPrivate(path string) error {
	return os.Chmod(path, 0o700)
}
