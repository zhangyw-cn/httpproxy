package hosts

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	profileSuffix = ".hosts"
	maxNameRunes  = 64
)

var (
	// ErrInvalidName reports a profile name that cannot be stored safely.
	ErrInvalidName = errors.New("invalid profile name")
	// ErrExists reports a profile name that is already taken, ignoring case.
	ErrExists = errors.New("profile already exists")
	// ErrNotFound reports a profile file that does not exist.
	ErrNotFound = errors.New("profile not found")
)

// Profiles manages the hosts profile files stored in <root>/hosts.
type Profiles struct {
	dir string
}

// OpenProfiles opens the hosts profile store below root, creating the hosts
// directory with mode 0700 when it does not exist. root is the work
// directory's .httpproxy directory.
func OpenProfiles(root string) (*Profiles, error) {
	dir := filepath.Join(root, "hosts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create hosts profile directory: %w", err)
	}
	return &Profiles{dir: dir}, nil
}

// Dir reports the directory that holds the profile files.
func (p *Profiles) Dir() string {
	return p.dir
}

// ValidName reports whether name is a usable profile name. Leading and
// trailing spaces are ignored. After trimming, a valid name is 1-64 runes,
// does not start with ".", is not "." or "..", and contains only Unicode
// letters, Unicode digits, space, '-', '_' or '.'. Path separators, control
// characters, and other punctuation are rejected.
func ValidName(name string) bool {
	_, ok := cleanName(name)
	return ok
}

// cleanName trims name and reports whether the result is a valid profile name.
func cleanName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", false
	}
	if strings.HasPrefix(name, ".") {
		return "", false
	}
	if utf8.RuneCountInString(name) > maxNameRunes {
		return "", false
	}
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
		case r == ' ', r == '-', r == '_', r == '.':
		default:
			return "", false
		}
	}
	return name, true
}

// List returns the sorted names of the stored profiles. Hidden files,
// subdirectories, files without the .hosts suffix, and files whose stem is not
// a valid profile name are ignored. Ordering is case-insensitive, with the raw
// name breaking ties.
func (p *Profiles) List() ([]string, error) {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		return nil, fmt.Errorf("list hosts profiles: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		file := entry.Name()
		if strings.HasPrefix(file, ".") || !strings.HasSuffix(file, profileSuffix) {
			continue
		}
		stem := strings.TrimSuffix(file, profileSuffix)
		name, ok := cleanName(stem)
		if !ok || name != stem {
			continue
		}
		names = append(names, name)
	}

	sort.Slice(names, func(i, j int) bool {
		lowerI, lowerJ := strings.ToLower(names[i]), strings.ToLower(names[j])
		if lowerI != lowerJ {
			return lowerI < lowerJ
		}
		return names[i] < names[j]
	})
	return names, nil
}

// Read returns the body of the named profile.
func (p *Profiles) Read(name string) ([]byte, error) {
	path, err := p.profilePath(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read hosts profile: %w", err)
	}
	return data, nil
}

// Write atomically replaces the body of the named profile with data. The
// profile must already exist. The file is written to a temporary file in the
// hosts directory and renamed into place, so readers see either the old or the
// new body, never a partial one.
func (p *Profiles) Write(name string, data []byte) error {
	path, err := p.profilePath(name)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("stat hosts profile: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ErrNotFound
	}

	tmp, err := os.CreateTemp(p.dir, ".hosts-*")
	if err != nil {
		return fmt.Errorf("create temporary hosts file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temporary hosts file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write hosts profile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write hosts profile: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace hosts profile: %w", err)
	}
	return nil
}

// Create creates an empty named profile. Names are compared case-insensitively.
func (p *Profiles) Create(name string) error {
	name, ok := cleanName(name)
	if !ok {
		return ErrInvalidName
	}
	if _, found, err := p.find(name); err != nil {
		return err
	} else if found {
		return ErrExists
	}

	f, err := os.OpenFile(p.filePath(name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ErrExists
		}
		return fmt.Errorf("create hosts profile: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("create hosts profile: %w", err)
	}
	return nil
}

// Rename renames oldName to newName. Both names are validated, and the new
// name must not already exist, ignoring case.
func (p *Profiles) Rename(oldName, newName string) error {
	oldName, ok := cleanName(oldName)
	if !ok {
		return ErrInvalidName
	}
	newName, ok = cleanName(newName)
	if !ok {
		return ErrInvalidName
	}

	names, err := p.List()
	if err != nil {
		return err
	}
	var oldExists, newExists bool
	for _, name := range names {
		if strings.EqualFold(name, oldName) {
			oldExists = true
		}
		if strings.EqualFold(name, newName) {
			newExists = true
		}
	}
	if !oldExists {
		return ErrNotFound
	}
	if newExists {
		return ErrExists
	}

	if err := os.Rename(p.filePath(oldName), p.filePath(newName)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("rename hosts profile: %w", err)
	}
	return nil
}

// Delete removes the named profile.
func (p *Profiles) Delete(name string) error {
	path, err := p.profilePath(name)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("delete hosts profile: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ErrNotFound
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return fmt.Errorf("delete hosts profile: %w", err)
	}
	return nil
}

// find reports the stored profile name matching name, ignoring case.
func (p *Profiles) find(name string) (string, bool, error) {
	names, err := p.List()
	if err != nil {
		return "", false, err
	}
	for _, existing := range names {
		if strings.EqualFold(existing, name) {
			return existing, true, nil
		}
	}
	return "", false, nil
}

// profilePath validates name and returns its file path.
func (p *Profiles) profilePath(name string) (string, error) {
	name, ok := cleanName(name)
	if !ok {
		return "", ErrInvalidName
	}
	return p.filePath(name), nil
}

// filePath returns the file path for an already validated name.
func (p *Profiles) filePath(name string) string {
	return filepath.Join(p.dir, name+profileSuffix)
}
