package app

import (
	"strings"
	"testing"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

func TestResolveProduct(t *testing.T) {
	tests := []struct {
		name     string
		arg      string
		province string
		want     ProductInfo
	}{
		{
			name:     "default is butano",
			province: "28",
			want:     ProductInfo{Alias: "butano", MaterialID: "153", MaxQty: 9},
		},
		{
			name:     "default in the Canary Islands is butano canarias",
			province: "35",
			want:     ProductInfo{Alias: "butano", MaterialID: "409", MaxQty: 9},
		},
		{
			name:     "default in Santa Cruz de Tenerife is butano canarias",
			province: "38",
			want:     ProductInfo{Alias: "butano", MaterialID: "409", MaxQty: 9},
		},
		{
			name: "aliases match case-insensitively",
			arg:  "PROPANO",
			want: ProductInfo{Alias: "propano", MaterialID: "239", MaxQty: 24},
		},
		{
			name: "numeric material without an alias",
			arg:  "620",
			want: ProductInfo{MaterialID: "620", MaxQty: 24},
		},
		{
			name: "numeric material with an alias",
			arg:  "153",
			want: ProductInfo{Alias: "butano", MaterialID: "153", MaxQty: 9},
		},
		{
			name: "canarias alias",
			arg:  "butano-canarias",
			want: ProductInfo{Alias: "butano-canarias", MaterialID: "409", MaxQty: 9},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveProduct(tt.arg, tt.province)
			if err != nil {
				t.Fatalf("ResolveProduct(%q, %q): %v", tt.arg, tt.province, err)
			}
			if got != tt.want {
				t.Errorf("ResolveProduct(%q, %q) = %+v, want %+v", tt.arg, tt.province, got, tt.want)
			}
		})
	}

	t.Run("unknown product", func(t *testing.T) {
		_, err := ResolveProduct("gasoil", "")
		typed := requireErrorCode(t, err, "unknown_product")
		if typed.Category != apperr.Usage {
			t.Errorf("category = %v, want %v", typed.Category, apperr.Usage)
		}
		for _, alias := range []string{
			"butano", "butano-ligera", "propano", "propano-35", "autogas", "butano-canarias",
		} {
			if !strings.Contains(typed.Message, alias) {
				t.Errorf("message %q does not list the alias %q", typed.Message, alias)
			}
		}
	})
}
