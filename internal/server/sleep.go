package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

type sleepRuntime struct {
	mu       sync.Mutex
	projects map[string]*projectSleep
}

type wakeCall struct {
	done chan struct{}
	err  error
}

type projectSleep struct {
	mu         sync.Mutex
	activityMu sync.Mutex
	last       time.Time
	inFlight   int
	wakeMu     sync.Mutex
	wake       *wakeCall
}

func (a *Server) projectRequest(id string) func() {
	s := a.projectSleepState(id)
	s.activityMu.Lock()
	s.inFlight++
	s.last = time.Now()
	s.activityMu.Unlock()
	return func() {
		s.activityMu.Lock()
		s.inFlight--
		s.last = time.Now()
		s.activityMu.Unlock()
	}
}

var errStaticSleep = errors.New("static-only projects cannot sleep: no runtime containers")

func (a *Server) staticProject(p Record) bool {
	release, err := a.store.Get(p.Active)
	if err != nil || len(release.ContainerReleases) == 0 {
		return false
	}
	for _, id := range release.ContainerReleases {
		d, err := a.store.Get(id)
		if err != nil || d.StaticDir == "" {
			return false
		}
	}
	return true
}

func (a *Server) projectSleepState(id string) *projectSleep {
	a.sleep.mu.Lock()
	defer a.sleep.mu.Unlock()
	if a.sleep.projects == nil {
		a.sleep.projects = map[string]*projectSleep{}
	}
	if a.sleep.projects[id] == nil {
		a.sleep.projects[id] = &projectSleep{}
	}
	return a.sleep.projects[id]
}

func (a *Server) projectActivity(id string) {
	s := a.projectSleepState(id)
	s.activityMu.Lock()
	s.last = time.Now()
	s.activityMu.Unlock()
}

func (a *Server) sleepDeployments(p Record) ([]Record, error) {
	release, err := a.store.Get(p.Active)
	if err != nil || release.Kind != "releases" || release.Status != "running" {
		return nil, fmt.Errorf("project has no running active release")
	}
	var deployments []Record
	for _, id := range release.ContainerReleases {
		d, err := a.store.Get(id)
		if err != nil || d.Status != "running" {
			return nil, fmt.Errorf("project has no healthy deployment")
		}
		if d.Container != "" {
			deployments = append(deployments, d)
		}
	}
	if len(deployments) == 0 {
		return nil, errStaticSleep
	}
	return deployments, nil
}

func (a *Server) sleepProject(id string, idle bool) error {
	s := a.projectSleepState(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := a.store.Get(id)
	if err != nil || p.Sleeping {
		return err
	}
	if p.Kind == "previews" {
		p.SleepAfter = a.previewSleepAfter(p)
	}
	deployments, err := a.sleepDeployments(p)
	if err != nil {
		return err
	}
	if idle {
		s.activityMu.Lock()
		if s.last.IsZero() {
			s.last = time.Now()
		}
		due := p.SleepAfter > 0 && s.inFlight == 0 && time.Since(s.last) >= time.Duration(p.SleepAfter)*time.Minute
		s.activityMu.Unlock()
		if !due {
			return nil
		}
	}
	if err = a.store.Update(id, func(r *Record) { r.Sleeping = true }); err != nil {
		return err
	}
	a.refresh()
	revert := func() error {
		err := a.store.Update(id, func(r *Record) { r.Sleeping = false })
		a.refresh()
		return err
	}
	if idle {
		s.activityMu.Lock()
		busy := s.inFlight > 0 || time.Since(s.last) < time.Duration(p.SleepAfter)*time.Minute
		s.activityMu.Unlock()
		if busy {
			return revert()
		}
	}
	var stopped []Record
	for _, d := range deployments {
		if err := command("", os.Stderr, "docker", "stop", d.Container); err != nil {
			for _, old := range stopped {
				if e := command("", os.Stderr, "docker", "start", old.Container); e != nil {
					log.Printf("Project %q (%s) sleep rollback: %v", p.Name, p.ID, e)
				}
			}
			return errors.Join(err, revert())
		}
		stopped = append(stopped, d)
	}
	log.Printf("Project %q (%s) sleeping after %d min idle", p.Name, p.ID, p.SleepAfter)
	return nil
}

func (a *Server) wakeProject(ctx context.Context, id string) error {
	a.projectActivity(id)
	s := a.projectSleepState(id)
	s.wakeMu.Lock()
	call := s.wake
	if call == nil {
		call = &wakeCall{done: make(chan struct{})}
		s.wake = call
		go func() {
			call.err = a.startSleepingProject(id, s)
			if call.err != nil {
				log.Printf("Project %s failed to wake: %v", id, call.err)
			}
			s.wakeMu.Lock()
			s.wake = nil
			close(call.done)
			s.wakeMu.Unlock()
		}()
	}
	s.wakeMu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-call.done:
		return call.err
	}
}

func (a *Server) startSleepingProject(id string, s *projectSleep) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := a.store.Get(id)
	if err != nil || !p.Sleeping {
		return err
	}
	start := time.Now()
	deployments, err := a.sleepDeployments(p)
	if err != nil {
		return err
	}
	for _, d := range deployments {
		if err := command("", os.Stderr, "docker", "start", d.Container); err != nil {
			return err
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, len(deployments))
	for _, d := range deployments {
		wg.Go(func() {
			results <- a.reconnectRelease(d)
		})
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			return err
		}
	}
	if err := a.store.Update(id, func(r *Record) { r.Sleeping = false }); err != nil {
		return err
	}
	a.projectActivity(id)
	a.refresh()
	log.Printf("Project %q (%s) woke in %.2fs", p.Name, p.ID, time.Since(start).Seconds())
	return nil
}

func (a *Server) reconnectRelease(d Record) error {
	if d.Network != "" {
		if err := connectSelf(d.Network); err != nil {
			return err
		}
	}
	ip, err := containerIP(d.Container, d.Network)
	if err != nil {
		return err
	}
	app, err := a.store.Get(d.AppID)
	if err != nil {
		return err
	}
	app.Port, app.Pipeline = d.Port, d.Pipeline
	if err := probeRelease(app, d.Container, ip, os.Stderr); err != nil {
		return err
	}
	if err := a.store.Update(d.ID, func(r *Record) { r.IP = ip }); err != nil {
		return err
	}
	return nil
}

func (a *Server) watchIdle(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, p := range append(a.store.List("projects", ""), a.store.List("previews", "")...) {
				if p.Kind == "previews" {
					p.SleepAfter = a.previewSleepAfter(p)
				}
				if p.SleepAfter > 0 && !p.Sleeping {
					if _, err := a.sleepDeployments(p); err != nil {
						continue
					}
					if err := a.sleepProject(p.ID, true); err != nil {
						log.Printf("Project idle sleep: %v", err)
					}
				}
			}
		}
	}
}
