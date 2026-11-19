package server

import (
	"context"
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
	wakeMu     sync.Mutex
	wake       *wakeCall
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
		return nil, fmt.Errorf("project has no containers")
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
	deployments, err := a.sleepDeployments(p)
	if err != nil {
		return err
	}
	if idle {
		s.activityMu.Lock()
		if s.last.IsZero() {
			s.last = time.Now()
		}
		due := p.SleepAfter > 0 && time.Since(s.last) >= time.Duration(p.SleepAfter)*time.Minute
		s.activityMu.Unlock()
		if !due {
			return nil
		}
	}
	for _, d := range deployments {
		if err := command("", os.Stderr, "docker", "stop", d.Container); err != nil {
			return err
		}
	}
	if err = a.store.Update(id, func(r *Record) { r.Sleeping = true }); err != nil {
		return err
	}
	a.refresh()
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
	}
	if err := a.store.Update(id, func(r *Record) { r.Sleeping = false }); err != nil {
		return err
	}
	a.projectActivity(id)
	a.refresh()
	log.Printf("Project %q (%s) woke in %.2fs", p.Name, p.ID, time.Since(start).Seconds())
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
			for _, p := range a.store.List("projects", "") {
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
