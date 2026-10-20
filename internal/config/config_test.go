package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyConfigDefaultsAndPolicyPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sqzarr.toml")
	if err := os.WriteFile(path, []byte("[transcoder]\nencoder = \"vaapi\"\n[plex]\nenabled = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Transcoder.HardwareDecode != "auto" || cfg.Transcoder.LowPower != "auto" {
		t.Fatalf("legacy config lost defaults: %+v", cfg.Transcoder)
	}
	if err := UpdateFile(path, map[string]string{"hardware_decode": `"off"`, "low_power": `"off"`}); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Transcoder.HardwareDecode != "off" || cfg.Transcoder.LowPower != "off" || cfg.Transcoder.Encoder != "vaapi" {
		t.Fatalf("policies not persisted in transcoder section: %+v", cfg.Transcoder)
	}
}

func TestInvalidTranscodePolicy(t *testing.T) {
	for _, key := range []string{"hardware_decode", "low_power"} {
		t.Run(key, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sqzarr.toml")
			if err := os.WriteFile(path, []byte("[transcoder]\n"+key+" = \"force\"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
}
