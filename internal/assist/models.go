package assist

// Model is a Claude model the assistant can answer with.
type Model struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Blurb string `json:"blurb"`
	// Typical is a rough cost of one question, for the picker.
	Typical string `json:"typical"`

	Price Price `json:"-"`
	// LongPrice replaces Price for a request whose prompt is longer than
	// LongAbove tokens (zero: one price at any length).
	LongPrice Price `json:"-"`
	LongAbove int64 `json:"-"`
	// Fallback: the API can retry a declined request on another model.
	Fallback bool `json:"-"`
}

// Models the user can pick from, most capable first. List prices per
// million tokens, with 5-minute cache writes.
var Models = []Model{
	{
		ID: "claude-opus-5-5", Name: "Claude Opus 5.5",
		Blurb:    "Most thorough. For tricky problems and long investigations.",
		Typical:  "about 4¢ a question",
		Price:    Price{Input: 4, CacheWrite: 5, CacheRead: 0.20, Output: 20},
		Fallback: true,
	},
	{
		ID: "claude-sonnet-5-5", Name: "Claude Sonnet 5.5",
		Blurb:    "Fast and capable. Good for most questions.",
		Typical:  "about 2¢ a question",
		Price:    Price{Input: 2, CacheWrite: 2.50, CacheRead: 0.10, Output: 10},
		Fallback: true,
	},
	{
		ID: "claude-haiku-5-5", Name: "Claude Haiku 5.5",
		Blurb:     "Quickest and cheapest. Fine for simple lookups.",
		Typical:   "well under 1¢ a question",
		Price:     Price{Input: 0.10, CacheWrite: 0.125, CacheRead: 0.01, Output: 0.50},
		LongPrice: Price{Input: 0.50, CacheWrite: 0.625, CacheRead: 0.05, Output: 2.50},
		LongAbove: 100_000,
		// Haiku has no server-side fallback; the API rejects the field.
	},
}

// DefaultModel balances quality and cost for everyday questions.
const DefaultModel = "claude-sonnet-5-5"

// FindModel looks a model up by id.
func FindModel(id string) (Model, bool) {
	for _, m := range Models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// RequestCost is the cost of one request's usage on this model, in dollars.
func (m Model) RequestCost(u Usage) float64 {
	if m.LongAbove > 0 && u.Input+u.CacheWrite+u.CacheRead > m.LongAbove {
		return u.Cost(m.LongPrice)
	}
	return u.Cost(m.Price)
}
