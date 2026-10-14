package gmail

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"pulse/internal/mailparse"
)

// Gemini es el respaldo opcional para correos de un banco soportado cuya plantilla no
// reconoce ningún parser. Desactivado por defecto: cada persona lo activa (con step-up).
// Solo recibe el asunto y el cuerpo ya redactados (sin nombre, emails ni números largos).
type Gemini struct {
	Key   string
	Model string
	Base  string
	HTTP  *http.Client
}

const maxPrompt = 6000

var ErrGemini = errors.New("gemini no pudo leer el correo")

type extraction struct {
	IsExpense bool   `json:"is_expense"`
	Amount    string `json:"amount"`
	Merchant  string `json:"merchant"`
	Card      string `json:"card_last_digits"`
	Date      string `json:"date"`
}

// Extract devuelve un gasto validado o el motivo por el que no lo es.
func (g *Gemini) Extract(ctx context.Context, e *mailparse.Email, loc *time.Location) (*mailparse.Expense, string, error) {
	names := mailparse.GreetingNames(e.Text)
	text := mailparse.Redact(e.Text, names...)
	if r := []rune(text); len(r) > maxPrompt {
		text = string(r[:maxPrompt])
	}
	prompt := "Eres un extractor de datos. El siguiente texto es una notificación de un banco de Ecuador. " +
		"Si informa una compra o consumo con tarjeta, responde is_expense=true con: amount (número con punto decimal, sin símbolo), " +
		"merchant (establecimiento tal como aparece), card_last_digits (3 o 4 dígitos, o vacío) y date (\"AAAA-MM-DD HH:MM\", hora local). " +
		"Si no es una compra (pago, transferencia, publicidad, aviso), responde is_expense=false. No inventes datos.\n\n" +
		"ASUNTO: " + mailparse.Redact(e.Subject, names...) + "\n\nTEXTO:\n" + text
	str := map[string]string{"type": "STRING"}
	body := map[string]any{
		"contents": []any{map[string]any{"role": "user", "parts": []any{map[string]string{"text": prompt}}}},
		"generationConfig": map[string]any{
			"temperature":      0,
			"responseMimeType": "application/json",
			"responseSchema": map[string]any{"type": "OBJECT", "required": []string{"is_expense"}, "properties": map[string]any{
				"is_expense": map[string]string{"type": "BOOLEAN"}, "amount": str, "merchant": str, "card_last_digits": str, "date": str}},
		},
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(g.Base, "/")+"/models/"+g.Model+":generateContent", strings.NewReader(string(b)))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.Key) // en cabecera, nunca en la URL (acabaría en logs)
	res, err := g.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, "", apiError(res, "gemini")
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 256<<10)).Decode(&out); err != nil || len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return nil, "", ErrGemini
	}
	var x extraction
	if json.Unmarshal([]byte(out.Candidates[0].Content.Parts[0].Text), &x) != nil {
		return nil, "", ErrGemini
	}
	if !x.IsExpense {
		return nil, "Gemini: no es una compra", nil
	}
	exp, why := mailparse.FromExtraction(e, x.Amount, x.Merchant, x.Card, x.Date, loc)
	if exp == nil {
		return nil, "Gemini: " + why, nil
	}
	return exp, "", nil
}
