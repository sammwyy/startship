package cli

import (
	"fmt"
	"net/url"
	"strings"
	"text/tabwriter"

	"github.com/sammwyy/startship/internal/migrate"
)

const migrateUsage = `usage:
  startship migrate detect [--platform openship|dokploy|coolify]
  startship migrate import <openship|dokploy|coolify> --all (--workspace WORKSPACE | --create-workspace NAME) [options]
  startship migrate import <openship|dokploy|coolify> --project ID[:NEW_NAME]... [--service ID[:NEW_NAME]...] (--workspace WORKSPACE | --create-workspace NAME) [options]
  startship migrate jobs [JOB_ID]`

func (c *cli) runMigrate(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(c.out, migrateUsage)
		return nil
	}
	op, rest := args[0], args[1:]
	switch op {
	case "detect", "scan":
		return c.runMigrateDetect(rest)
	case "import", "run":
		return c.runMigrateImport(rest)
	case "jobs", "status":
		return c.runMigrateJobs(rest)
	default:
		return fmt.Errorf("unknown migrate command %q\n%s", op, migrateUsage)
	}
}

func (c *cli) runMigrateDetect(args []string) error {
	var platform string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--platform" && i+1 < len(args):
			i++
			platform = args[i]
		case strings.HasPrefix(args[i], "--platform="):
			platform = strings.TrimPrefix(args[i], "--platform=")
		case !strings.HasPrefix(args[i], "-") && platform == "":
			platform = args[i]
		default:
			return fmt.Errorf("unexpected argument %q\n%s", args[i], migrateUsage)
		}
	}

	path := "system/migrations/detect"
	if platform != "" {
		path += "?platform=" + url.QueryEscape(platform)
	}
	v, err := c.request("GET", path, nil)
	if err != nil {
		return err
	}
	if c.raw {
		return c.print(v)
	}
	return c.printMigrateDetect(v)
}

func (c *cli) printMigrateDetect(v any) error {
	root, _ := v.(map[string]any)
	platforms, _ := root["platforms"].([]any)
	if len(platforms) == 0 {
		fmt.Fprintln(c.out, "No competitor installations detected.")
		return nil
	}

	w := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PLATFORM\tSTATUS\tPROJECTS\tSERVICES\tDETAILS")
	for _, raw := range platforms {
		m, _ := raw.(map[string]any)
		projs, _ := m["projects"].([]any)
		svcs, _ := m["services"].([]any)
		details := field(m, "error")
		if details == "" {
			details = field(m, "data_dir")
		}
		if details == "" {
			details = field(m, "db_container")
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\n", field(m, "platform"), field(m, "status"), len(projs), len(svcs), details)
	}
	_ = w.Flush()

	for _, raw := range platforms {
		m, _ := raw.(map[string]any)
		if m["detected"] != true {
			continue
		}
		projs, _ := m["projects"].([]any)
		svcs, _ := m["services"].([]any)
		if len(projs) > 0 {
			fmt.Fprintf(c.out, "\n%s projects:\n", field(m, "name"))
			pw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(pw, "ID\tNAME\tSOURCE\tENV\tVOLUMES\tDOMAINS\tLOGS")
			for _, pr := range projs {
				pm, _ := pr.(map[string]any)
				vols, _ := pm["volumes"].([]any)
				doms, _ := pm["domains"].([]any)
				src := field(pm, "git_url")
				if src == "" {
					src = field(pm, "docker_image")
				}
				if src == "" {
					src = field(pm, "source_kind")
				}
				fmt.Fprintf(pw, "%s\t%s\t%s\t%v\t%d\t%d\t%v\n",
					field(pm, "id"), field(pm, "name"), src,
					pm["env_count"], len(vols), len(doms), pm["log_count"])
			}
			_ = pw.Flush()
		}
		if len(svcs) > 0 {
			fmt.Fprintf(c.out, "\n%s services:\n", field(m, "name"))
			sw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(sw, "ID\tNAME\tENGINE\tVERSION\tVOLUMES\tCOMPATIBLE")
			for _, sr := range svcs {
				sm, _ := sr.(map[string]any)
				vols, _ := sm["volumes"].([]any)
				fmt.Fprintf(sw, "%s\t%s\t%s\t%s\t%d\t%v\n",
					field(sm, "id"), field(sm, "name"), field(sm, "engine"),
					field(sm, "version"), len(vols), sm["compatible"])
			}
			_ = sw.Flush()
		}
	}
	return nil
}

func parseSelectionSpec(spec string) (id, targetName string) {
	spec = strings.TrimSpace(spec)
	for _, sep := range []string{"=", ":"} {
		if idx := strings.Index(spec, sep); idx > 0 {
			return strings.TrimSpace(spec[:idx]), strings.TrimSpace(spec[idx+1:])
		}
	}
	return spec, ""
}

func (c *cli) runMigrateImport(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("platform required\n%s", migrateUsage)
	}

	var (
		platformRaw     string
		importAll       bool
		workspaceRef    string
		createWorkspace string
		singleName      string
		projectSpecs    []string
		serviceSpecs    []string
		copyVolumes     = true
		importDomains   = true
		importEnv       = true
		importLogs      = true
		startServices   = true
		deployProjects  = false
		asyncMode       = false
	)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("missing value for %s", arg)
			}
			i++
			return args[i], nil
		}
		switch {
		case arg == "--all":
			importAll = true
		case arg == "--workspace" || arg == "-w":
			v, err := next()
			if err != nil {
				return err
			}
			workspaceRef = v
		case strings.HasPrefix(arg, "--workspace="):
			workspaceRef = strings.TrimPrefix(arg, "--workspace=")
		case arg == "--create-workspace":
			v, err := next()
			if err != nil {
				return err
			}
			createWorkspace = v
		case strings.HasPrefix(arg, "--create-workspace="):
			createWorkspace = strings.TrimPrefix(arg, "--create-workspace=")
		case arg == "--project" || arg == "-p":
			v, err := next()
			if err != nil {
				return err
			}
			projectSpecs = append(projectSpecs, v)
		case strings.HasPrefix(arg, "--project="):
			projectSpecs = append(projectSpecs, strings.TrimPrefix(arg, "--project="))
		case arg == "--service" || arg == "-s":
			v, err := next()
			if err != nil {
				return err
			}
			serviceSpecs = append(serviceSpecs, v)
		case strings.HasPrefix(arg, "--service="):
			serviceSpecs = append(serviceSpecs, strings.TrimPrefix(arg, "--service="))
		case arg == "--name":
			v, err := next()
			if err != nil {
				return err
			}
			singleName = v
		case strings.HasPrefix(arg, "--name="):
			singleName = strings.TrimPrefix(arg, "--name=")
		case arg == "--no-volumes":
			copyVolumes = false
		case arg == "--no-domains":
			importDomains = false
		case arg == "--no-env":
			importEnv = false
		case arg == "--no-logs":
			importLogs = false
		case arg == "--no-start":
			startServices = false
		case arg == "--deploy":
			deployProjects = true
		case arg == "--async":
			asyncMode = true
		case !strings.HasPrefix(arg, "-") && platformRaw == "":
			platformRaw = arg
		default:
			return fmt.Errorf("unexpected argument %q\n%s", arg, migrateUsage)
		}
	}

	plat, ok := migrate.ParsePlatform(platformRaw)
	if !ok {
		return fmt.Errorf("unsupported platform %q (expected openship, dokploy, or coolify)", platformRaw)
	}
	if !importAll && len(projectSpecs) == 0 && len(serviceSpecs) == 0 {
		return fmt.Errorf("specify --all or at least one --project / --service\n%s", migrateUsage)
	}
	if workspaceRef == "" && strings.TrimSpace(createWorkspace) == "" {
		return fmt.Errorf("specify --workspace WORKSPACE or --create-workspace NAME")
	}

	targetWorkspaceID := ""
	if workspaceRef != "" {
		id, err := c.resolve("workspaces", workspaceRef)
		if err != nil {
			return err
		}
		targetWorkspaceID = id
	}

	mode := "cherry_pick"
	if importAll {
		mode = "all"
	}

	var projects []migrate.ProjectSelection
	for _, spec := range projectSpecs {
		id, name := parseSelectionSpec(spec)
		if id != "" {
			projects = append(projects, migrate.ProjectSelection{SourceID: id, TargetName: name})
		}
	}
	if singleName != "" && len(projects) == 1 && projects[0].TargetName == "" {
		projects[0].TargetName = singleName
	}

	var services []migrate.ServiceSelection
	for _, spec := range serviceSpecs {
		id, name := parseSelectionSpec(spec)
		if id != "" {
			services = append(services, migrate.ServiceSelection{SourceID: id, TargetName: name})
		}
	}

	req := migrate.ImportRequest{
		Platform:            plat,
		Mode:                mode,
		TargetWorkspaceID:   targetWorkspaceID,
		CreateWorkspaceName: strings.TrimSpace(createWorkspace),
		CopyVolumes:         &copyVolumes,
		ImportDomains:       &importDomains,
		ImportEnv:           &importEnv,
		ImportLogs:          &importLogs,
		StartServices:       &startServices,
		DeployProjects:      &deployProjects,
		Projects:            projects,
		Services:            services,
	}

	path := "system/migrations/import"
	if !asyncMode {
		path += "?sync=true"
	}
	v, err := c.request("POST", path, req)
	if err != nil {
		return err
	}
	if c.raw {
		return c.print(v)
	}
	return c.printMigrateJob(v)
}

func (c *cli) runMigrateJobs(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("expected at most one JOB_ID\n%s", migrateUsage)
	}
	if len(args) == 1 {
		v, err := c.request("GET", "system/migrations/jobs/"+url.PathEscape(args[0]), nil)
		if err != nil {
			return err
		}
		if c.raw {
			return c.print(v)
		}
		return c.printMigrateJob(v)
	}
	v, err := c.request("GET", "system/migrations/jobs", nil)
	if err != nil {
		return err
	}
	if c.raw {
		return c.print(v)
	}
	root, _ := v.(map[string]any)
	jobs, _ := root["jobs"].([]any)
	if len(jobs) == 0 {
		fmt.Fprintln(c.out, "No migration jobs yet.")
		return nil
	}
	w := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tPLATFORM\tMODE\tSTATUS\tWORKSPACE\tPROJECTS\tSERVICES")
	for _, raw := range jobs {
		m, _ := raw.(map[string]any)
		projs, _ := m["projects"].([]any)
		svcs, _ := m["services"].([]any)
		ws := field(m, "target_workspace_name")
		if ws == "" {
			ws = field(m, "target_workspace_id")
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%d\n",
			field(m, "id"), field(m, "platform"), field(m, "mode"),
			field(m, "status"), ws, len(projs), len(svcs))
	}
	return w.Flush()
}

func (c *cli) printMigrateJob(v any) error {
	m, _ := v.(map[string]any)
	fmt.Fprintf(c.out, "Migration %s (%s, %s): %s\n",
		field(m, "id"), field(m, "platform"), field(m, "mode"), field(m, "status"))
	if ws := field(m, "target_workspace_name"); ws != "" {
		fmt.Fprintf(c.out, "Target workspace: %s (%s)\n", ws, field(m, "target_workspace_id"))
	}
	if errMsg := field(m, "error"); errMsg != "" {
		fmt.Fprintf(c.out, "Error: %s\n", errMsg)
	}
	if projs, _ := m["projects"].([]any); len(projs) > 0 {
		fmt.Fprintln(c.out, "\nProjects:")
		w := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "SOURCE\tTARGET\tSTATUS\tENV\tVOLUMES\tDOMAINS\tLOGS")
		for _, raw := range projs {
			pm, _ := raw.(map[string]any)
			fmt.Fprintf(w, "%s\t%s\t%s\t%v\t%v\t%v\t%v\n",
				field(pm, "source_name"), field(pm, "project_name"), field(pm, "status"),
				pm["env_count"], pm["volumes_copied"], pm["domains_imported"], pm["logs_imported"])
		}
		_ = w.Flush()
	}
	if svcs, _ := m["services"].([]any); len(svcs) > 0 {
		fmt.Fprintln(c.out, "\nServices:")
		w := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "SOURCE\tTARGET\tENGINE\tSTATUS\tVOLUMES\tATTACHED")
		for _, raw := range svcs {
			sm, _ := raw.(map[string]any)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%v\t%v\n",
				field(sm, "source_name"), field(sm, "service_name"), field(sm, "engine"),
				field(sm, "status"), sm["volumes_copied"], sm["attached_projects"])
		}
		_ = w.Flush()
	}
	return nil
}
