package config

import (
	"testing"

	adapterconfig "github.com/godaddy-x/wallet-adapter/config"
)

func TestEffectiveDustLimitSatsDefault(t *testing.T) {
	cfg := NewConfig("BTC")
	if got := cfg.EffectiveDustLimitSats(); got != DefaultDustLimitSats {
		t.Fatalf("got %d want %d", got, DefaultDustLimitSats)
	}
}

func TestBuildConfigFromConfigerDustLimitSats(t *testing.T) {
	cfg := BuildConfigFromConfiger(adapterconfig.MapConfig(map[string]string{
		"dustLimitSats": "100",
	}), "BTC")
	if cfg.EffectiveDustLimitSats() != 100 {
		t.Fatalf("dustLimitSats = %d want 100", cfg.EffectiveDustLimitSats())
	}
}

func TestValidateDustConfigPanicsAboveMax(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for dustLimitSats above max")
		}
	}()
	cfg := NewConfig("BTC")
	cfg.DustLimitSats = MaxDustLimitSats + 1
	cfg.ValidateDustConfig()
}

func TestEffectiveDustLimitSatsAtMax(t *testing.T) {
	cfg := NewConfig("BTC")
	cfg.DustLimitSats = MaxDustLimitSats
	cfg.ValidateDustConfig()
	if got := cfg.EffectiveDustLimitSats(); got != MaxDustLimitSats {
		t.Fatalf("got %d want %d", got, MaxDustLimitSats)
	}
}
