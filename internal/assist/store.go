package assist

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// DayUsage is what one model used on one day.
type DayUsage struct {
	Date      string  `json:"date"` // local day, 2006-01-02
	Model     string  `json:"model"`
	Questions int     `json:"questions"`
	Usage     Usage   `json:"usage"`
	Cost      float64 `json:"cost"` // estimated, US dollars
}

type settings struct {
	Model string     `json:"model"`
	Days  []DayUsage `json:"days"`
}

// Store keeps the assistant's settings and a day-by-day record of its usage
// in a small JSON file.
type Store struct {
	path string

	mu     sync.Mutex
	loaded bool
	s      settings
}

// keepDays is how long daily records are kept.
const keepDays = 400

// OpenStore uses the file at path; it is read on first use.
func OpenStore(path string) *Store { return &Store{path: path} }

func (st *Store) load() {
	if st.loaded {
		return
	}
	st.loaded = true
	if b, err := os.ReadFile(st.path); err == nil {
		_ = json.Unmarshal(b, &st.s)
	}
}

func (st *Store) save() error {
	b, err := json.MarshalIndent(st.s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(st.path), 0o700); err != nil {
		return err
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.path)
}

// Model is the chosen model, or the default if none was chosen.
func (st *Store) Model() Model {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.load()
	if m, ok := FindModel(st.s.Model); ok {
		return m
	}
	m, _ := FindModel(DefaultModel)
	return m
}

// SetModel remembers the chosen model.
func (st *Store) SetModel(id string) error {
	if _, ok := FindModel(id); !ok {
		return errors.New("unknown model")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.load()
	st.s.Model = id
	return st.save()
}

// Record adds one request's usage. question marks the first request of a
// question, so each question is counted once however many steps it takes.
func (st *Store) Record(at time.Time, model string, u Usage, cost float64, question bool) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.load()
	day := at.Format("2006-01-02")
	i := -1
	for j := range st.s.Days {
		if st.s.Days[j].Date == day && st.s.Days[j].Model == model {
			i = j
			break
		}
	}
	if i < 0 {
		st.s.Days = append(st.s.Days, DayUsage{Date: day, Model: model})
		i = len(st.s.Days) - 1
	}
	d := &st.s.Days[i]
	d.Usage = d.Usage.Add(u)
	d.Cost += cost
	if question {
		d.Questions++
	}
	cutoff := at.AddDate(0, 0, -keepDays).Format("2006-01-02")
	kept := st.s.Days[:0]
	for _, d := range st.s.Days {
		if d.Date >= cutoff {
			kept = append(kept, d)
		}
	}
	st.s.Days = kept
	return st.save()
}

// Period totals usage over some span of days.
type Period struct {
	Cost      float64 `json:"cost"`
	Questions int     `json:"questions"`
	// Tokens sent to the model (cached or not) and written by it.
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
}

func (p *Period) add(d DayUsage) {
	p.Cost += d.Cost
	p.Questions += d.Questions
	p.InputTokens += d.Usage.Input + d.Usage.CacheWrite + d.Usage.CacheRead
	p.OutputTokens += d.Usage.Output
}

// ModelPeriod is one model's share of a period.
type ModelPeriod struct {
	Model string `json:"model"`
	Name  string `json:"name"`
	Period
}

// DayTotal is one day's total across models.
type DayTotal struct {
	Date      string  `json:"date"`
	Cost      float64 `json:"cost"`
	Questions int     `json:"questions"`
}

// UsageSummary is what the usage panel shows.
type UsageSummary struct {
	Today Period `json:"today"`
	Month Period `json:"month"`
	All   Period `json:"all"`
	// Since is the first day on record ("" if nothing is).
	Since string `json:"since"`
	// ByModel splits this month by model, most expensive first.
	ByModel []ModelPeriod `json:"byModel"`
	// Days is the last 30 days, oldest first, including empty days.
	Days []DayTotal `json:"days"`
}

// Summary totals the record as of now.
func (st *Store) Summary(now time.Time) UsageSummary {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.load()

	today := now.Format("2006-01-02")
	month := now.Format("2006-01")
	const span = 30
	first := now.AddDate(0, 0, -(span - 1)).Format("2006-01-02")

	var sum UsageSummary
	byModel := map[string]*ModelPeriod{}
	byDay := map[string]*DayTotal{}
	for _, d := range st.s.Days {
		sum.All.add(d)
		if sum.Since == "" || d.Date < sum.Since {
			sum.Since = d.Date
		}
		if d.Date == today {
			sum.Today.add(d)
		}
		if d.Date[:7] == month {
			sum.Month.add(d)
			mp := byModel[d.Model]
			if mp == nil {
				name := d.Model
				if m, ok := FindModel(d.Model); ok {
					name = m.Name
				}
				mp = &ModelPeriod{Model: d.Model, Name: name}
				byModel[d.Model] = mp
			}
			mp.add(d)
		}
		if d.Date >= first {
			t := byDay[d.Date]
			if t == nil {
				t = &DayTotal{Date: d.Date}
				byDay[d.Date] = t
			}
			t.Cost += d.Cost
			t.Questions += d.Questions
		}
	}
	for _, mp := range byModel {
		sum.ByModel = append(sum.ByModel, *mp)
	}
	sort.Slice(sum.ByModel, func(i, j int) bool { return sum.ByModel[i].Cost > sum.ByModel[j].Cost })
	for i := span - 1; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		if t := byDay[day]; t != nil {
			sum.Days = append(sum.Days, *t)
		} else {
			sum.Days = append(sum.Days, DayTotal{Date: day})
		}
	}
	return sum
}
