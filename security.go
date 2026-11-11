package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"dev.vimukthid/brewctl/internal/brew"
)

// SecurityState is what the Security screen shows.
type SecurityState struct {
	Report   *brew.VulnReport `json:"report"` // nil until the first check
	Monitor  bool             `json:"monitor"`
	Scanning bool             `json:"scanning"`
	Error    string           `json:"error"`
	// Background says how daily checks run while monitoring is on: "on"
	// (also while the app is closed), "unavailable" (this build can't set
	// that up, so only while it's open) or "error" (setting it up failed).
	Background      string `json:"background"`
	BackgroundError string `json:"backgroundError"`
	// Notifications is "allowed", "off" (not allowed in System Settings,
	// or not asked yet) or "unavailable" (falls back to a script).
	Notifications string `json:"notifications"`
}

const (
	monitorEvery = 24 * time.Hour
	// monitorDue is when a check counts as due; a little under a day, so a
	// wake-up that comes slightly early doesn't skip a whole day.
	monitorDue  = monitorEvery - 30*time.Minute
	monitorTick = 15 * time.Minute
)

func (s *BrewService) store() *brew.SecurityStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.security == nil {
		dir, err := os.UserConfigDir()
		if err != nil {
			dir = os.TempDir()
		}
		s.security = brew.OpenSecurityStore(filepath.Join(dir, "dev.vimukthid.brewctl", "security.json"))
	}
	return s.security
}

// ServiceStartup sets up native notifications and, in the app, keeps the
// login agent in step with the "Check daily" setting.
func (s *BrewService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	s.startNotifications(ctx, options)
	if !s.background {
		go s.syncAgent()
	}
	return nil
}

// startNotifications uses macOS notifications when the app runs from its
// bundle. Anywhere else (the browser build) they're unavailable and Brewctl
// falls back to a script notification.
func (s *BrewService) startNotifications(ctx context.Context, options application.ServiceOptions) {
	s.mu.Lock()
	s.notesOn = "unavailable"
	s.mu.Unlock()
	if agentExecutable() == "" {
		return
	}
	ns := notifications.New()
	if err := ns.ServiceStartup(ctx, options); err != nil {
		return
	}
	ns.OnNotificationResponse(func(r notifications.NotificationResult) {
		if r.Error != nil {
			return
		}
		// Every Brewctl notification is about Security.
		s.mu.Lock()
		s.pendingView = "security"
		s.mu.Unlock()
		emit("app:navigate", "security")
		if app := application.Get(); app != nil {
			app.Show()
		}
	})
	s.mu.Lock()
	s.notes = ns
	s.mu.Unlock()
	go func() {
		// If daily checks were turned on before Brewctl could notify, ask now.
		// macOS only shows the prompt if it hasn't been answered before.
		if !s.background && s.store().Monitor() {
			_, _ = ns.RequestNotificationAuthorization()
		}
		s.refreshNotificationAccess()
	}()
}

func (s *BrewService) refreshNotificationAccess() {
	s.mu.Lock()
	ns := s.notes
	s.mu.Unlock()
	if ns == nil {
		return
	}
	state := "off"
	if ok, err := ns.CheckNotificationAuthorization(); err == nil && ok {
		state = "allowed"
	}
	s.mu.Lock()
	changed := s.notesOn != state
	s.notesOn = state
	s.mu.Unlock()
	if changed {
		emit("security:update", s.securityState())
	}
}

// PendingView returns, once, the view a clicked notification asked for.
func (s *BrewService) PendingView() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.pendingView
	s.pendingView = ""
	return v
}

// OpenNotificationSettings opens Brewctl's page in System Settings.
func (s *BrewService) OpenNotificationSettings() error {
	return exec.Command("/usr/bin/open", "x-apple.systempreferences:com.apple.Notifications-Settings.extension?id=dev.vimukthid.brewctl").Run()
}

// syncAgent installs or removes the login agent to match the setting, and
// re-points it if the app has moved.
func (s *BrewService) syncAgent() {
	st := s.store()
	exe := agentExecutable()
	var err error
	switch {
	case !st.Monitor():
		err = removeAgent()
	case exe != "" && !agentInstalled(exe):
		err = installAgent(exe)
	}
	s.mu.Lock()
	s.agentErr = ""
	if err != nil {
		s.agentErr = err.Error()
	}
	s.mu.Unlock()
}

func (s *BrewService) securityState() SecurityState {
	st := s.store()
	state := SecurityState{Report: st.Report(), Monitor: st.Monitor(), Error: st.LastError()}
	if state.Monitor {
		exe := agentExecutable()
		switch {
		case exe == "":
			state.Background = "unavailable"
		case agentInstalled(exe):
			state.Background = "on"
		default:
			state.Background = "error"
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state.Scanning = s.scanning
	state.Notifications = s.notesOn
	if state.Background == "error" {
		state.BackgroundError = s.agentErr
	}
	return state
}

// Security returns the last vulnerability check without running a new one.
func (s *BrewService) Security(ctx context.Context) SecurityState {
	return s.securityState()
}

// SecurityScan checks installed tools against the OSV.dev database now.
func (s *BrewService) SecurityScan(ctx context.Context) SecurityState {
	s.scan(false)
	return s.securityState()
}

// SetSecurityMonitor turns the daily check on or off. Turning it on sets up
// the login agent, asks once for permission to notify, and checks right away
// if the last check is stale.
func (s *BrewService) SetSecurityMonitor(ctx context.Context, on bool) SecurityState {
	st := s.store()
	st.SetMonitor(on)
	s.syncAgent()
	if on {
		s.mu.Lock()
		ns := s.notes
		s.mu.Unlock()
		if ns != nil {
			go func() {
				_, _ = ns.RequestNotificationAuthorization()
				s.refreshNotificationAccess()
				emit("security:update", s.securityState())
			}()
		}
		if time.Since(time.Unix(st.CheckedAt(), 0)) > monitorDue {
			go s.scan(false)
		}
	}
	return s.securityState()
}

// SecurityAcknowledge clears the "new" marks once the user has seen them.
func (s *BrewService) SecurityAcknowledge(ctx context.Context) SecurityState {
	s.store().Acknowledge()
	return s.securityState()
}

// scan runs one check. It does nothing if one is already running, here or
// in the background agent. Background scans notify about anything new.
func (s *BrewService) scan(background bool) {
	st := s.store()
	s.mu.Lock()
	if s.scanning {
		s.mu.Unlock()
		return
	}
	s.scanning = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.scanning = false
		s.mu.Unlock()
		emit("security:update", s.securityState())
	}()

	unlock, ok := st.TryLock()
	if !ok {
		return
	}
	defer unlock()
	emit("security:update", s.securityState())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	c, _, err := s.ready(ctx)
	if err != nil {
		st.SetError(err.Error())
		return
	}
	rep, err := c.Vulns(ctx)
	if err != nil {
		st.SetError(err.Error())
		return
	}
	fresh := st.Record(rep)
	if !background || len(fresh) == 0 {
		return
	}
	n := 0
	names := make([]string, 0, len(fresh))
	for _, f := range fresh {
		n += len(f.Vulns)
		names = append(names, f.Formula)
	}
	title := "1 new vulnerability in your Homebrew tools"
	if n > 1 {
		title = fmt.Sprintf("%d new vulnerabilities in your Homebrew tools", n)
	}
	s.notify(ctx, title, "Affects "+strings.Join(names, ", ")+". Click to review.")
}

// notify posts a macOS notification from Brewctl, or through a script when
// that isn't available or allowed.
func (s *BrewService) notify(ctx context.Context, title, body string) {
	s.mu.Lock()
	ns := s.notes
	s.mu.Unlock()
	if ns != nil {
		if ok, err := ns.CheckNotificationAuthorization(); err == nil && ok {
			err := ns.SendNotification(notifications.NotificationOptions{
				ID:       fmt.Sprintf("brewctl-vulns-%d", time.Now().UnixNano()),
				Title:    title,
				Body:     body,
				ThreadID: "security",
				Data:     map[string]interface{}{"view": "security"},
			})
			if err == nil {
				return
			}
		} else if err == nil {
			return // the user turned Brewctl's notifications off; respect that
		}
	}
	brew.Notify(ctx, title, body)
}

// monitor re-checks once a day while the app is open, if the user turned
// monitoring on, and picks up checks the background agent made meanwhile.
func (s *BrewService) monitor() {
	time.Sleep(20 * time.Second)
	last := s.store().CheckedAt()
	for {
		st := s.store()
		if st.Monitor() && time.Since(time.Unix(st.CheckedAt(), 0)) > monitorDue {
			s.scan(true)
		}
		if at := st.CheckedAt(); at != last {
			last = at
			emit("security:update", s.securityState())
		}
		time.Sleep(monitorTick)
	}
}

// runBackgroundCheck is what the login agent runs. It exits at once unless
// monitoring is on and a check is due; then it checks, notifies about
// anything new as Brewctl, and quits. It never shows a window.
func runBackgroundCheck() {
	svc := &BrewService{background: true}
	st := svc.store()
	if !st.Monitor() {
		_ = removeAgent() // turned off while the app couldn't tidy up
		return
	}
	if time.Since(time.Unix(st.CheckedAt(), 0)) < monitorDue {
		return
	}
	app := application.New(application.Options{
		Name:     "Brewctl",
		Services: []application.Service{application.NewService(svc)},
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyAccessory,
		},
	})
	go func() {
		// Give the app a moment to finish launching before notifying.
		time.Sleep(2 * time.Second)
		svc.scan(true)
		time.Sleep(time.Second)
		app.Quit()
	}()
	_ = app.Run()
}
