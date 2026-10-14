package importer

import (
	"testing"

	"pulse/internal/store"
)

func TestMatchAccount(t *testing.T) {
	accs := []store.Account{
		{ID: "pich", Name: "Visa Pichincha", Institution: "Banco Pichincha", CardSuffix: "989"},
		{ID: "otra989", Name: "Otra", Institution: "Produbanco", CardSuffix: "989"},
		{ID: "vieja", Name: "Vieja", Institution: "Diners Club", CardSuffix: "666", Archived: true},
		{ID: "nueva", Name: "Nueva", Institution: "Diners Club", CardSuffix: "666"},
		{ID: "sininst", Name: "Sin banco", CardSuffix: "7828"},
	}
	cases := []struct{ inst, suffix, want string }{
		{"Banco Pichincha", "989", "pich"},         // desempata por institución
		{"Produbanco", "989", "otra989"},           // mismo sufijo, otro banco
		{"Titanium (Diners Club)", "666", "nueva"}, // prefiere la no archivada
		{"Produbanco", "7828", "sininst"},          // cuenta sin institución con esos dígitos
		{"Banco Guayaquil", "989", ""},             // dígitos repetidos en otros bancos: no adivina
		{"Banco Pichincha", "", ""},
		{"Banco Pichincha", "1234", ""},
	}
	for _, c := range cases {
		got := MatchAccount(accs, c.inst, c.suffix)
		id := ""
		if got != nil {
			id = got.ID
		}
		if id != c.want {
			t.Errorf("%s …%s → %q, esperaba %q", c.inst, c.suffix, id, c.want)
		}
	}
}

func TestPalabrasCompletas(t *testing.T) {
	if !containsKey("uber eats", "uber eats") || !containsKey("dlc uber eats quito", "uber eats") || containsKey("tiana boutique", "tia") || !containsKey("tia la garzota", "tia") {
		t.Fatal("containsKey debe respetar palabras completas")
	}
}
