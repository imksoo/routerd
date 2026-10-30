// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/imksoo/routerd/pkg/api"
)

// NTP owns its generated fragment, not every file or service of the provider.
// NextDigest retains an interrupted exact write; Withdrawing retains a reload
// that must be retried after the fragment was removed.
type ntpConfigRecord struct {
	Name          string `json:"name"`
	Provider      string `json:"provider"`
	Path          string `json:"path"`
	Token         string `json:"token"`
	Digest        string `json:"digest"`
	NextDigest    string `json:"nextDigest"`
	Withdrawing   bool   `json:"withdrawing"`
	ReloadPending bool   `json:"reloadPending"`
}

type ntpConfigOwner struct {
	store Store
	kind  string
}

func (o ntpConfigOwner) read() ([]ntpConfigRecord, error) {
	status, err := o.store.ReadObjectStatus(api.RouterAPIVersion, "Router", "_"+o.kind+"-configs")
	if err != nil || status == nil {
		return nil, err
	}
	var records []ntpConfigRecord
	switch raw := status["configs"].(type) {
	case []ntpConfigRecord:
		records = append(records, raw...)
	case []any:
		for _, value := range raw {
			fields, ok := value.(map[string]any)
			if !ok {
				return nil, errors.New("invalid NTP config record")
			}
			var r ntpConfigRecord
			for key, target := range map[string]*string{"name": &r.Name, "provider": &r.Provider, "path": &r.Path, "token": &r.Token, "digest": &r.Digest, "nextDigest": &r.NextDigest} {
				text, ok := fields[key].(string)
				if !ok {
					return nil, fmt.Errorf("invalid NTP config %s", key)
				}
				*target = text
			}
			r.Withdrawing, ok = fields["withdrawing"].(bool)
			if !ok {
				return nil, errors.New("invalid NTP withdrawal state")
			}
			r.ReloadPending, ok = fields["reloadPending"].(bool)
			if !ok {
				return nil, errors.New("invalid NTP reload state")
			}
			records = append(records, r)
		}
	default:
		return nil, errors.New("invalid NTP config inventory")
	}
	seen := map[string]bool{}
	for _, r := range records {
		token, err := hex.DecodeString(r.Token)
		if r.Name == "" || seen[r.Name] || !filepath.IsAbs(r.Path) || err != nil || len(token) != 16 || r.Provider != "chrony" && r.Provider != "ntpd" && r.Provider != "systemd-timesyncd" || r.Digest == "" && r.NextDigest == "" {
			return nil, errors.New("invalid NTP config identity")
		}
		for _, digest := range []string{r.Digest, r.NextDigest} {
			if digest != "" {
				raw, err := hex.DecodeString(digest)
				if err != nil || len(raw) != sha256.Size {
					return nil, errors.New("invalid NTP config digest")
				}
			}
		}
		seen[r.Name] = true
	}
	return records, nil
}

func (o ntpConfigOwner) get(name string) (*ntpConfigRecord, error) {
	records, err := o.read()
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		if r.Name == name {
			return &r, nil
		}
	}
	return nil, nil
}

func (o ntpConfigOwner) save(name string, before, after *ntpConfigRecord) error {
	records, err := o.read()
	if err != nil {
		return err
	}
	index := -1
	for i := range records {
		if records[i].Name == name {
			index = i
			break
		}
	}
	if before == nil && index >= 0 || before != nil && (index < 0 || records[index] != *before) {
		return errors.New("NTP config ownership changed while observing")
	}
	if after == nil {
		records = append(records[:index], records[index+1:]...)
	} else if index < 0 {
		records = append(records, *after)
	} else {
		records[index] = *after
	}
	if records == nil {
		records = []ntpConfigRecord{}
	}
	return o.store.SaveObjectStatus(api.RouterAPIVersion, "Router", "_"+o.kind+"-configs", map[string]any{"configs": records})
}

func ntpConfigFile(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("NTP config path is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	after, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, after) {
		return nil, errors.New("NTP config file changed while reading")
	}
	return data, nil
}

func ntpConfigDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
func (r ntpConfigRecord) matches(data []byte) bool {
	digest := ntpConfigDigest(data)
	return digest == r.Digest || r.NextDigest != "" && digest == r.NextDigest
}

func (o ntpConfigOwner) require(r ntpConfigRecord) error {
	current, err := o.get(r.Name)
	if err != nil {
		return err
	}
	if current == nil || *current != r {
		return errors.New("NTP config ownership changed while observing")
	}
	return nil
}

func (o ntpConfigOwner) write(name, provider, path string, data []byte, dryRun bool) (bool, *ntpConfigRecord, error) {
	r, err := o.get(name)
	if err != nil {
		return false, nil, err
	}
	original := r
	current, readErr := ntpConfigFile(path)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return false, nil, readErr
	}
	if dryRun {
		if r != nil {
			data = append([]byte("# routerd NTP ownership: "+r.Token+"\n"), data...)
		}
		return !bytes.Equal(current, data), nil, nil
	}
	if r == nil {
		if readErr == nil {
			return false, nil, errors.New("NTP config exists without acquired ownership")
		}
		token := make([]byte, 16)
		if _, err := rand.Read(token); err != nil {
			return false, nil, err
		}
		r = &ntpConfigRecord{Name: name, Provider: provider, Path: path, Token: hex.EncodeToString(token)}
	} else {
		if r.Provider != provider || r.Path != path || r.Withdrawing {
			return false, nil, errors.New("previous NTP config withdrawal is incomplete")
		}
		if readErr == nil && !r.matches(current) {
			return false, nil, errors.New("NTP config was replaced or edited outside its owner")
		}
	}
	data = append([]byte("# routerd NTP ownership: "+r.Token+"\n"), data...)
	if bytes.Equal(current, data) && readErr == nil && r.NextDigest == "" {
		if err := o.require(*r); err != nil {
			return false, nil, err
		}
		return r.ReloadPending, r, nil
	}
	desired := *r
	desired.NextDigest = ntpConfigDigest(data)
	desired.ReloadPending = true
	if err := o.save(name, original, &desired); err != nil {
		return false, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, nil, err
	}
	if errors.Is(readErr, os.ErrNotExist) {
		if err := o.require(desired); err != nil {
			return false, nil, err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return false, nil, err
		}
		_, writeErr := file.Write(data)
		syncErr := file.Sync()
		closeErr := file.Close()
		if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
			return false, nil, err
		}
	} else if !bytes.Equal(current, data) {
		file, err := os.CreateTemp(filepath.Dir(path), ".routerd-ntp-*")
		if err != nil {
			return false, nil, err
		}
		temporary := file.Name()
		defer os.Remove(temporary)
		_, writeErr := file.Write(data)
		modeErr := file.Chmod(0644)
		syncErr := file.Sync()
		closeErr := file.Close()
		if err := errors.Join(writeErr, modeErr, syncErr, closeErr); err != nil {
			return false, nil, err
		}
		if err := o.require(desired); err != nil {
			return false, nil, err
		}
		fresh, err := ntpConfigFile(path)
		if err != nil {
			return false, nil, err
		}
		if !bytes.Equal(fresh, current) {
			return false, nil, errors.New("NTP config changed before replacement")
		}
		if err := os.Rename(temporary, path); err != nil {
			return false, nil, err
		}
	}
	fresh, err := ntpConfigFile(path)
	if err != nil {
		return false, nil, err
	}
	if !bytes.Equal(fresh, data) {
		return false, nil, errors.New("NTP config write was not confirmed")
	}
	applied := desired
	applied.Digest, applied.NextDigest = desired.NextDigest, ""
	if err := o.save(name, &desired, &applied); err != nil {
		return false, nil, err
	}
	return true, &applied, nil
}

func (o ntpConfigOwner) withdraw(ctx context.Context, r ntpConfigRecord, dryRun bool, reload func(context.Context, string) error) (bool, error) {
	if dryRun {
		return true, nil
	}
	if !r.Withdrawing {
		next := r
		next.Withdrawing = true
		if err := o.save(r.Name, &r, &next); err != nil {
			return false, err
		}
		r = next
	}
	data, err := ntpConfigFile(r.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err == nil {
		if !r.matches(data) {
			return false, errors.New("NTP config withdrawal found a foreign replacement")
		}
		if err := o.require(r); err != nil {
			return false, err
		}
		if err := os.Remove(r.Path); err != nil {
			return false, err
		}
	}
	if err := reload(ctx, r.Provider); err != nil {
		return false, err
	}
	if _, err := os.Lstat(r.Path); !errors.Is(err, os.ErrNotExist) {
		return false, errors.New("NTP config absence is not confirmed after provider reload")
	}
	if err := o.save(r.Name, &r, nil); err != nil {
		return false, err
	}
	return true, nil
}

func (o ntpConfigOwner) check(r ntpConfigRecord) error {
	if err := o.require(r); err != nil {
		return err
	}
	data, err := ntpConfigFile(r.Path)
	if err != nil {
		return err
	}
	if ntpConfigDigest(data) != r.Digest || r.NextDigest != "" || r.Withdrawing {
		return errors.New("NTP applied config identity changed")
	}
	return o.require(r)
}

func (o ntpConfigOwner) confirm(r ntpConfigRecord) error {
	if err := o.check(r); err != nil {
		return err
	}
	next := r
	next.ReloadPending = false
	return o.save(r.Name, &r, &next)
}

func (o ntpConfigOwner) command(r ntpConfigRecord, command outputCommandFunc) outputCommandFunc {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if err := o.check(r); err != nil {
			return nil, err
		}
		return command(ctx, name, args...)
	}
}
