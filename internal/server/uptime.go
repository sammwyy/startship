package server

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"time"
)

const (
	uptimeDay       = 24 * time.Hour
	uptimeRetention = 90 * uptimeDay
)

type uptimeBucket struct {
	At     time.Time `json:"at"`
	Checks int       `json:"checks"`
	Uptime float64   `json:"uptime"`
}

type uptimeCheck struct {
	At        time.Time `json:"at"`
	OK        bool      `json:"ok"`
	LatencyMS float64   `json:"latency_ms"`
	Error     string    `json:"error,omitempty"`
}

type uptimeIncident struct {
	Start time.Time  `json:"start"`
	End   *time.Time `json:"end"`
	Cause string     `json:"cause"`
}

type uptimeSummary struct {
	State     string           `json:"state"`
	Uptime24h *float64         `json:"uptime_24h"`
	Uptime7d  *float64         `json:"uptime_7d"`
	Uptime30d *float64         `json:"uptime_30d"`
	History   []uptimeBucket   `json:"history"`
	Checks    []uptimeCheck    `json:"checks"`
	Incidents []uptimeIncident `json:"incidents"`
}

func (a *Server) watchUptime(ctx context.Context) {
	interval := 30 * time.Second
	if raw := os.Getenv("STARTSHIP_UPTIME_INTERVAL"); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed >= time.Millisecond {
			interval = parsed
		} else {
			log.Print("invalid STARTSHIP_UPTIME_INTERVAL; using 30s")
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	pruneAt := time.Time{}
	for {
		if ctx.Err() != nil {
			return
		}
		now := time.Now()
		if !now.Before(pruneAt) {
			if err := a.pruneUptime(now); err != nil {
				log.Printf("prune uptime: %v", err)
			}
			pruneAt = now.Add(time.Hour)
		}
		for _, p := range a.store.List("projects", "") {
			if ctx.Err() != nil {
				return
			}
			if p.UptimeMonitoring {
				a.sampleUptime(p)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *Server) pruneUptime(now time.Time) error {
	cutoff := now.Add(-uptimeRetention).UnixMilli()
	if _, err := a.store.DB.Exec("DELETE FROM uptime_checks WHERE at < ? OR project_id NOT IN (SELECT id FROM records WHERE kind='projects')", cutoff); err != nil {
		return err
	}
	if _, err := a.store.DB.Exec("DELETE FROM uptime_incidents WHERE ended < ? OR project_id NOT IN (SELECT id FROM records WHERE kind='projects')", cutoff); err != nil {
		return err
	}
	_, err := a.store.DB.Exec("DELETE FROM status_pages WHERE workspace_id NOT IN (SELECT id FROM records WHERE kind='workspaces')")
	return err
}

func (a *Server) sampleUptime(p Record) {
	if p.Active == "" || p.Sleeping {
		return
	}
	release, err := a.store.Get(p.Active)
	if err != nil || release.Kind != "releases" || release.ProjectID != p.ID {
		return
	}
	probed, latency, cause := false, float64(0), ""
	for _, id := range release.ContainerReleases {
		d, err := a.store.Get(id)
		if err != nil {
			probed, cause = true, "Active deployment is unavailable"
			break
		}
		if d.StaticDir != "" {
			continue
		}
		probed = true
		start := time.Now()
		if d.Status != "running" || d.IP == "" || d.Port == 0 {
			cause = "Deployment has no probe address"
			break
		}
		err = probeOnce(d, d.IP)
		latency = max(latency, float64(time.Since(start))/float64(time.Millisecond))
		if err != nil {
			cause = err.Error()
			break
		}
	}
	if !probed {
		return
	}
	if err := a.commitUptime(p, time.Now(), latency, cause); err != nil {
		log.Printf("record uptime: %v", err)
	}
}

func (a *Server) commitUptime(p Record, at time.Time, latency float64, cause string) error {
	a.store.Lock()
	defer a.store.Unlock()
	current, err := a.store.Get(p.ID)
	if err != nil || current.Active != p.Active || current.Sleeping || !current.UptimeMonitoring {
		return nil
	}
	if err := a.recordUptime(p.ID, at, latency, cause); err != nil {
		return err
	}
	a.live.publish(liveEvent{changeKey: "uptime:" + p.ID})
	return nil
}

func (a *Server) recordUptime(project string, at time.Time, latency float64, cause string) error {
	tx, err := a.store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ts := at.UnixMilli()
	var previousAt int64
	var previousOK bool
	var previousCause string
	err = tx.QueryRow("SELECT at,ok,error FROM uptime_checks WHERE project_id=? ORDER BY at DESC LIMIT 1", project).Scan(&previousAt, &previousOK, &previousCause)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	previousFailed := err == nil && !previousOK
	if _, err = tx.Exec("INSERT INTO uptime_checks VALUES(?,?,?,?,?)", project, ts, cause == "", latency, cause); err != nil {
		return err
	}
	if cause == "" {
		_, err = tx.Exec("UPDATE uptime_incidents SET ended=? WHERE project_id=? AND ended IS NULL", ts, project)
	} else if previousFailed {
		_, err = tx.Exec("INSERT OR IGNORE INTO uptime_incidents(project_id,started,cause) VALUES(?,?,?)", project, previousAt, previousCause)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func uptimePercent(up, total int) *float64 {
	if total == 0 {
		return nil
	}
	value := 100 * float64(up) / float64(total)
	return &value
}

func (a *Server) summarizeUptime(p Record, now time.Time, days int, includeChecks bool) (uptimeSummary, error) {
	out := uptimeSummary{State: "pending", History: []uptimeBucket{}, Checks: []uptimeCheck{}, Incidents: []uptimeIncident{}}
	day := now.UTC().Truncate(uptimeDay)
	first := day.Add(-time.Duration(days-1) * uptimeDay)
	for at := first; !at.After(day); at = at.Add(uptimeDay) {
		out.History = append(out.History, uptimeBucket{At: at})
	}
	err := a.fillUptime(&out, p, now, first, includeChecks)
	return out, err
}

func (a *Server) fillUptime(out *uptimeSummary, p Record, now, first time.Time, includeChecks bool) error {
	cutoff := now.Add(-uptimeRetention).UnixMilli()
	w24, w7, w30 := now.Add(-uptimeDay).UnixMilli(), now.Add(-7*uptimeDay).UnixMilli(), now.Add(-30*uptimeDay).UnixMilli()
	rows, err := a.store.DB.Query(`SELECT at/?,count(*),sum(ok),
		sum(at>=?),sum((at>=?)*ok),sum(at>=?),sum((at>=?)*ok),sum(at>=?),sum((at>=?)*ok)
		FROM uptime_checks WHERE project_id=? AND at>=? AND at<=? GROUP BY 1`,
		uptimeDay.Milliseconds(), w24, w24, w7, w7, w30, w30, p.ID, max(cutoff, min(first.UnixMilli(), w30)), now.UnixMilli())
	if err != nil {
		return err
	}
	var totals [6]int
	for rows.Next() {
		var day int64
		var total, up int
		var counts [6]int
		if err := rows.Scan(&day, &total, &up, &counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5]); err != nil {
			rows.Close()
			return err
		}
		for i, count := range counts {
			totals[i] += count
		}
		if i := day - first.UnixMilli()/uptimeDay.Milliseconds(); i >= 0 && i < int64(len(out.History)) {
			out.History[i].Checks, out.History[i].Uptime = total, *uptimePercent(up, total)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	out.Uptime24h, out.Uptime7d, out.Uptime30d = uptimePercent(totals[1], totals[0]), uptimePercent(totals[3], totals[2]), uptimePercent(totals[5], totals[4])
	rows, err = a.store.DB.Query("SELECT started,ended,cause FROM uptime_incidents WHERE project_id=? AND (ended IS NULL OR ended>=?) ORDER BY started DESC LIMIT 20", p.ID, cutoff)
	if err != nil {
		return err
	}
	for rows.Next() {
		var started int64
		var ended sql.NullInt64
		var cause string
		if err := rows.Scan(&started, &ended, &cause); err != nil {
			rows.Close()
			return err
		}
		i := uptimeIncident{Start: time.UnixMilli(started).UTC(), Cause: cause}
		if ended.Valid {
			end := time.UnixMilli(ended.Int64).UTC()
			i.End = &end
		}
		out.Incidents = append(out.Incidents, i)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	limit := 1
	if includeChecks {
		limit = 80
	}
	if !p.UptimeMonitoring {
		out.State = "disabled"
	} else if p.Sleeping {
		out.State = "sleeping"
	} else if p.Active != "" && a.staticProject(p) {
		out.State = "static"
	} else if p.Active != "" && len(out.Incidents) > 0 && out.Incidents[0].End == nil {
		out.State = "down"
	}
	rows, err = a.store.DB.Query("SELECT at,ok,latency_ms,error FROM uptime_checks WHERE project_id=? AND at>=? AND at<=? ORDER BY at DESC LIMIT ?", p.ID, cutoff, now.UnixMilli(), limit)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ts int64
		var check uptimeCheck
		if err := rows.Scan(&ts, &check.OK, &check.LatencyMS, &check.Error); err != nil {
			return err
		}
		check.At = time.UnixMilli(ts).UTC()
		if out.State == "pending" && p.Active != "" {
			if check.OK {
				out.State = "operational"
			} else {
				out.State = "degraded"
			}
		}
		if includeChecks {
			out.Checks = append(out.Checks, check)
		}
	}
	return rows.Err()
}

func (a *Server) projectUptime(w http.ResponseWriter, r *http.Request, p Record) {
	if r.Method != http.MethodGet {
		fail(w, 405, "method not allowed")
		return
	}
	data, err := a.summarizeUptime(p, time.Now(), 30, true)
	if err != nil {
		log.Printf("uptime summary: %v", err)
		fail(w, 500, "uptime unavailable")
		return
	}
	reply(w, 200, data)
}
