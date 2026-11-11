package brew

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Job statuses.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusDone      = "done"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

const (
	maxJobs       = 40
	maxOutputLine = 3000
)

// Job is one brew command run on the user's behalf.
type Job struct {
	ID        int      `json:"id"`
	Title     string   `json:"title"`
	Command   string   `json:"command"`
	Status    string   `json:"status"`
	NeedsSudo bool     `json:"needsSudo"`
	SudoIssue string   `json:"sudoIssue"` // why sudo failed, see the Sudo* constants
	Mutates   bool     `json:"mutates"`
	QueuedAt  int64    `json:"queuedAt"`
	StartedAt int64    `json:"startedAt"`
	EndedAt   int64    `json:"endedAt"`
	Output    []string `json:"output"`

	args    []string
	lenient bool // a non-zero exit still counts as done (e.g. doctor's warnings)
	admin   bool // runs as `sudo brew`, asking for the admin password
	reclaim string
	after   *Job // runs only if this job finished successfully
	cancel  context.CancelFunc
}

// Opts adjusts how a mutating job runs.
type Opts struct {
	// Admin runs the command as `sudo brew ...`, for services that run as
	// root. The password is asked for through the macOS dialog.
	Admin bool
	// After is the ID of a job that must finish successfully first;
	// otherwise this one is skipped.
	After int
	// Reclaim, with Admin, is a folder handed back to this account once the
	// command succeeds. With no command it is the whole job.
	Reclaim string
}

// JobOutput is a batch of new output lines for a job.
type JobOutput struct {
	ID    int      `json:"id"`
	Lines []string `json:"lines"`
}

// Runner executes jobs one at a time; brew holds a global lock, so running
// mutating commands in parallel only makes them fail.
type Runner struct {
	client *Client
	emit   func(event string, data any)

	mu     sync.Mutex
	jobs   []*Job
	nextID int
	queue  chan *Job
}

// NewRunner starts the worker. emit is called for "job:update" (a Job without
// output) and "job:output" (a JobOutput).
func NewRunner(c *Client, emit func(event string, data any)) *Runner {
	r := &Runner{client: c, emit: emit, queue: make(chan *Job, 256), nextID: 1}
	go r.loop()
	return r
}

// Enqueue schedules `brew args...`.
func (r *Runner) Enqueue(title string, mutates bool, args ...string) Job {
	return r.enqueue(title, mutates, false, Opts{}, args)
}

// EnqueueWith schedules a mutating `brew args...` with the given options.
func (r *Runner) EnqueueWith(title string, o Opts, args ...string) Job {
	return r.enqueue(title, true, false, o, args)
}

// EnqueueReport schedules a command whose output is the point: brew exiting
// non-zero (as `doctor` does when it has warnings) is not a failure.
func (r *Runner) EnqueueReport(title string, args ...string) Job {
	return r.enqueue(title, false, true, Opts{}, args)
}

func (r *Runner) enqueue(title string, mutates, lenient bool, o Opts, args []string) Job {
	r.mu.Lock()
	command := "brew " + strings.Join(args, " ")
	if o.Admin {
		command = "sudo " + command
	}
	if o.Admin && o.Reclaim != "" {
		chown := "sudo chown -R $USER " + o.Reclaim
		if len(args) == 0 {
			command = chown
		} else {
			command += " && " + chown
		}
	}
	j := &Job{
		ID: r.nextID, Title: title, Command: command,
		Status: StatusQueued, Mutates: mutates, QueuedAt: time.Now().Unix(),
		Output: []string{}, args: args, lenient: lenient, admin: o.Admin, reclaim: o.Reclaim,
	}
	if o.After != 0 {
		for _, prev := range r.jobs {
			if prev.ID == o.After {
				j.after = prev
			}
		}
	}
	r.nextID++
	r.jobs = append(r.jobs, j)
	r.trimLocked()
	meta := j.meta()
	r.mu.Unlock()

	r.emit("job:update", meta)
	r.queue <- j
	return meta
}

// trimLocked drops the oldest finished jobs beyond the history limit.
func (r *Runner) trimLocked() {
	for len(r.jobs) > maxJobs {
		dropped := false
		for i, j := range r.jobs {
			if j.Status != StatusQueued && j.Status != StatusRunning {
				r.jobs = append(r.jobs[:i], r.jobs[i+1:]...)
				dropped = true
				break
			}
		}
		if !dropped {
			return
		}
	}
}

func (j *Job) meta() Job {
	m := *j
	m.Output = []string{}
	m.args, m.cancel, m.after = nil, nil, nil
	return m
}

// List returns all jobs, oldest first, including their output so far.
func (r *Runner) List() []Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Job, 0, len(r.jobs))
	for _, j := range r.jobs {
		m := j.meta()
		m.Output = append([]string{}, j.Output...)
		out = append(out, m)
	}
	return out
}

// Cancel stops a queued or running job.
func (r *Runner) Cancel(id int) {
	r.mu.Lock()
	var meta *Job
	for _, j := range r.jobs {
		if j.ID != id {
			continue
		}
		switch j.Status {
		case StatusQueued:
			j.Status = StatusCancelled
			j.EndedAt = time.Now().Unix()
			m := j.meta()
			meta = &m
		case StatusRunning:
			if j.cancel != nil {
				j.cancel()
			}
		}
	}
	r.mu.Unlock()
	if meta != nil {
		r.emit("job:update", *meta)
	}
}

func (r *Runner) loop() {
	for j := range r.queue {
		r.run(j)
	}
}

func (r *Runner) setStatus(j *Job, mutate func()) {
	r.mu.Lock()
	mutate()
	meta := j.meta()
	r.mu.Unlock()
	r.emit("job:update", meta)
}

func (r *Runner) run(j *Job) {
	r.mu.Lock()
	if j.Status != StatusQueued {
		r.mu.Unlock()
		return
	}
	// Jobs run in order, so the prerequisite has already finished.
	if j.after != nil && j.after.Status != StatusDone {
		j.Status = StatusCancelled
		j.EndedAt = time.Now().Unix()
		j.Output = append(j.Output, "Skipped: “"+j.after.Title+"” didn't finish.")
		meta := j.meta()
		r.mu.Unlock()
		r.emit("job:update", meta)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.cancel = cancel
	j.Status = StatusRunning
	j.StartedAt = time.Now().Unix()
	meta := j.meta()
	r.mu.Unlock()
	defer cancel()
	r.emit("job:update", meta)

	cmd := r.client.command(ctx, j.args...)
	if j.admin {
		cmd = r.client.sudoCommand(ctx, j.reclaim, j.args...)
	}
	if j.Mutates {
		// Lets brew ask for the admin password through a macOS dialog.
		if helper, cleanup, err := newAskpass(j.Title, r.client.IconPath, askpassGiveUp); err == nil {
			defer cleanup()
			cmd.Env = append(cmd.Env, "SUDO_ASKPASS="+helper)
		}
	}
	// Own process group, so cancelling also stops whatever brew spawned.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second

	// A real pipe (not io.Pipe) so grandchildren inheriting it can't block Wait
	// forever: WaitDelay closes it for us.
	pr, pw, err := os.Pipe()
	if err != nil {
		r.finish(j, err, ctx)
		return
	}
	cmd.Stdout, cmd.Stderr = pw, pw

	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		r.finish(j, err, ctx)
		return
	}
	pw.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.pump(j, pr)
	}()
	err = cmd.Wait()
	// Give the reader a moment to drain, then force it closed.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	pr.Close()
	<-done
	r.finish(j, err, ctx)
}

func (r *Runner) finish(j *Job, err error, ctx context.Context) {
	if err != nil && ctx.Err() == nil {
		if !isExitError(err) {
			r.appendLines(j, []string{"Error: " + err.Error()})
		}
	}
	r.setStatus(j, func() {
		j.EndedAt = time.Now().Unix()
		switch {
		case ctx.Err() != nil:
			j.Status = StatusCancelled
		case err != nil && !(j.lenient && isExitError(err)):
			j.Status = StatusFailed
		default:
			j.Status = StatusDone
		}
		j.cancel = nil
	})
}

func isExitError(err error) bool {
	_, ok := err.(*exec.ExitError)
	return ok
}

// pump reads combined output, splitting on both \n and \r so progress bars
// become lines, and forwards it in small batches.
func (r *Runner) pump(j *Job, rd io.Reader) {
	sc := bufio.NewScanner(rd)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	sc.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		for i, b := range data {
			if b == '\n' || b == '\r' {
				return i + 1, data[:i], nil
			}
		}
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	})

	lines := make(chan string, 256)
	go func() {
		defer close(lines)
		blank := true // collapse runs of blank lines, drop leading ones
		for sc.Scan() {
			l := strings.TrimRight(stripANSI(sc.Text()), " \t")
			if l == "" && blank {
				continue
			}
			blank = l == ""
			lines <- l
		}
	}()

	var batch []string
	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()
	flush := func() {
		if len(batch) > 0 {
			r.appendLines(j, batch)
			batch = nil
		}
	}
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				flush()
				return
			}
			batch = append(batch, l)
		case <-tick.C:
			flush()
		}
	}
}

func (r *Runner) appendLines(j *Job, lines []string) {
	r.mu.Lock()
	for _, l := range lines {
		if issue := sudoIssue(l); issue != "" {
			j.NeedsSudo = true
			// A wrong password is followed by "no password was provided" if
			// the retry is cancelled; keep the first, more specific reason.
			if j.SudoIssue == "" || issue == SudoWrong {
				j.SudoIssue = issue
			}
		}
	}
	j.Output = append(j.Output, lines...)
	if over := len(j.Output) - maxOutputLine; over > 0 {
		j.Output = append([]string{}, j.Output[over:]...)
	}
	r.mu.Unlock()
	r.emit("job:output", JobOutput{ID: j.ID, Lines: lines})
}

// stripANSI removes terminal escape sequences.
func stripANSI(s string) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		if i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
		} else if i+1 < len(s) && s[i+1] == ']' {
			// OSC: ends at BEL or ESC \
			i += 2
			for i < len(s) && s[i] != 0x07 && !(s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\') {
				i++
			}
			if i < len(s) && s[i] == 0x1b {
				i++
			}
		} else {
			i++
		}
	}
	return b.String()
}
