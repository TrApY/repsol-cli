package app

import (
	"fmt"
	"strings"

	"github.com/felipe9962/repsol-cli/internal/apperr"
)

// largeBottleMaterials lists the materials the web sells in batches of up to
// 24. Every other material is capped at 9.
var largeBottleMaterials = map[string]bool{
	"210": true,
	"230": true,
	"239": true,
	"320": true,
	"620": true,
}

// productAliases maps every accepted product alias to its material id, in the
// order the aliases are listed to the user.
var productAliases = []struct {
	alias      string
	materialID string
}{
	{alias: "butano", materialID: "153"},
	{alias: "butano-ligera", materialID: "154"},
	{alias: "propano", materialID: "239"},
	{alias: "propano-35", materialID: "210"},
	{alias: "autogas", materialID: "230"},
	{alias: "butano-canarias", materialID: "409"},
}

// ProductInfo is a product resolved to its material id and per-order maximum.
type ProductInfo struct {
	// Alias is the canonical alias, or empty for a material id without one.
	Alias string
	// MaterialID is the numeric material id as the API names it.
	MaterialID string
	// MaxQty is the maximum number of bottles in one order.
	MaxQty int
}

// ResolveProduct turns a product alias or a numeric material id into a
// ProductInfo. An empty argument means butano, which resolves to the Canarias
// material when the province is one of the two Canary Islands provinces.
func ResolveProduct(arg, provinceCode string) (ProductInfo, error) {
	name := strings.ToLower(strings.TrimSpace(arg))
	if name == "" {
		name = "butano"
	}

	if isDigits(name) {
		return ProductInfo{Alias: AliasFor(name), MaterialID: name, MaxQty: maxQuantity(name)}, nil
	}

	materialID, ok := materialForAlias(name)
	if !ok {
		return ProductInfo{}, unknownProduct(arg)
	}
	if name == "butano" && isCanaryProvince(provinceCode) {
		materialID = "409"
	}
	return ProductInfo{Alias: name, MaterialID: materialID, MaxQty: maxQuantity(materialID)}, nil
}

// AliasFor returns the canonical alias of a material id, or empty when the
// material has no alias.
func AliasFor(materialID string) string {
	for _, p := range productAliases {
		if p.materialID == materialID {
			return p.alias
		}
	}
	return ""
}

// materialForAlias resolves one lower-case alias.
func materialForAlias(alias string) (string, bool) {
	for _, p := range productAliases {
		if p.alias == alias {
			return p.materialID, true
		}
	}
	return "", false
}

// maxQuantity is the web's per-order maximum for a material.
func maxQuantity(materialID string) int {
	if largeBottleMaterials[materialID] {
		return 24
	}
	return 9
}

// isCanaryProvince reports whether a province code is one of the Canary
// Islands provinces, which sell the Canarias bottle.
func isCanaryProvince(provinceCode string) bool {
	return provinceCode == "35" || provinceCode == "38"
}

// isDigits reports whether s is a non-empty run of ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// unknownProduct builds the error for an argument that is neither an alias nor
// a material id. It lists every accepted alias.
func unknownProduct(arg string) *apperr.Error {
	aliases := make([]string, 0, len(productAliases))
	for _, p := range productAliases {
		aliases = append(aliases, p.alias)
	}
	return apperr.New(apperr.Usage, "unknown_product",
		fmt.Sprintf("unknown product %q: use %s, or a numeric material id", arg, strings.Join(aliases, ", ")))
}
