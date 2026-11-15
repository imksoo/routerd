// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestSSHHostKeyComparison(t *testing.T) {
	_, ed, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for name, private := range map[string]any{"ed25519": ed, "rsa": rsaKey, "ecdsa": ec} {
		t.Run(name, func(t *testing.T) {
			openssh, err := ssh.MarshalPrivateKey(private, "example")
			if err != nil {
				t.Fatal(err)
			}
			desired := pem.EncodeToMemory(openssh)
			signer, err := ssh.ParsePrivateKey(desired)
			if err != nil {
				t.Fatal(err)
			}
			pkcs8, err := x509.MarshalPKCS8PrivateKey(private)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "hostkey")
			if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8}), 0600); err != nil {
				t.Fatal(err)
			}
			public := ssh.MarshalAuthorizedKey(signer.PublicKey())
			public = append(public[:len(public)-1], []byte(" existing-host-comment\n")...)
			if err := os.WriteFile(path+".pub", public, 0644); err != nil {
				t.Fatal(err)
			}
			changed, err := sshHostKeyNeedsUpdate(path, desired, signer.PublicKey())
			if err != nil || changed {
				t.Fatalf("equivalent keys with a comment must not need update: %v", err)
			}
			_, other, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			otherBlock, err := ssh.MarshalPrivateKey(other, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, pem.EncodeToMemory(otherBlock), 0600); err != nil {
				t.Fatal(err)
			}
			if changed, err = sshHostKeyNeedsUpdate(path, desired, signer.PublicKey()); err != nil || !changed {
				t.Fatalf("different private key must need update: %v", err)
			}
			if err := os.WriteFile(path, desired, 0600); err != nil {
				t.Fatal(err)
			}
			otherSigner, err := ssh.NewSignerFromKey(other)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path+".pub", ssh.MarshalAuthorizedKey(otherSigner.PublicKey()), 0644); err != nil {
				t.Fatal(err)
			}
			if changed, err = sshHostKeyNeedsUpdate(path, desired, signer.PublicKey()); err != nil || !changed {
				t.Fatalf("different public key must need update: %v", err)
			}
			if err := os.WriteFile(path+".pub", []byte("invalid\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if changed, err = sshHostKeyNeedsUpdate(path, desired, signer.PublicKey()); err != nil || !changed {
				t.Fatalf("invalid public key must need update: %v", err)
			}
			if err := os.Remove(path + ".pub"); err != nil {
				t.Fatal(err)
			}
			if changed, err = sshHostKeyNeedsUpdate(path, desired, signer.PublicKey()); err != nil || !changed {
				t.Fatalf("missing public key must need update: %v", err)
			}
			if err := os.WriteFile(path+".pub", public, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0644); err != nil {
				t.Fatal(err)
			}
			if changed, err = sshHostKeyNeedsUpdate(path, desired, signer.PublicKey()); err != nil || !changed {
				t.Fatalf("unsafe private permissions must need update: %v", err)
			}
			if err := os.Chmod(path, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path+".pub", 0666); err != nil {
				t.Fatal(err)
			}
			if changed, err = sshHostKeyNeedsUpdate(path, desired, signer.PublicKey()); err != nil || !changed {
				t.Fatalf("unsafe public permissions must need update: %v", err)
			}
			if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
				t.Fatal(err)
			}
			if changed, err = sshHostKeyNeedsUpdate(path, desired, signer.PublicKey()); err != nil || !changed {
				t.Fatalf("invalid private key must need update: %v", err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if changed, err = sshHostKeyNeedsUpdate(path, desired, signer.PublicKey()); err != nil || !changed {
				t.Fatalf("missing private key must need update: %v", err)
			}
		})
	}
}
