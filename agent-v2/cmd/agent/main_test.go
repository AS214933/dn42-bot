package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCountConfFilesFiltersPrefixAndNumericName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := []string{
		"dn42-4242421234.conf",
		"dn42-4242421816.conf",
		"dn42-not-a-number.conf",
		"wg0.conf",
		"README.txt",
	}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	got, err := countConfFiles(dir, "dn42-")
	if err != nil {
		t.Fatalf("countConfFiles returned error: %v", err)
	}
	if got != 2 {
		t.Fatalf("countConfFiles = %d, want 2", got)
	}
}

func TestCountPeersIgnoresNonPeerConfigFiles(t *testing.T) {
	t.Parallel()

	wgDir := t.TempDir()
	birdDir := t.TempDir()
	for _, path := range []string{
		filepath.Join(wgDir, "dn42-4242421234.conf"),
		filepath.Join(wgDir, "wg0.conf"),
		filepath.Join(birdDir, "4242421234.conf"),
		filepath.Join(birdDir, "bird.conf"),
	} {
		if err := os.WriteFile(path, []byte{}, 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	wgCount, birdCount, err := countPeers(wgDir, birdDir)
	if err != nil {
		t.Fatalf("countPeers returned error: %v", err)
	}
	if wgCount != 1 || birdCount != 1 {
		t.Fatalf("countPeers = (%d, %d), want (1, 1)", wgCount, birdCount)
	}
}
