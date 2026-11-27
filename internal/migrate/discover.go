package migrate

import "context"

func DetectPlatform(ctx context.Context, host Host, platform Platform) DetectedPlatform {
	if host == nil {
		host = OSHost{}
	}
	switch platform {
	case PlatformOpenship:
		return DetectOpenship(ctx, host)
	case PlatformDokploy:
		return DetectDokploy(ctx, host)
	case PlatformCoolify:
		return DetectCoolify(ctx, host)
	default:
		return DetectedPlatform{
			Platform: platform,
			Name:     string(platform),
			Status:   "error",
			Error:    "unsupported platform",
			Projects: []DiscoveredProject{},
			Services: []DiscoveredService{},
		}
	}
}

func DetectAll(ctx context.Context, host Host) []DetectedPlatform {
	if host == nil {
		host = OSHost{}
	}
	out := make([]DetectedPlatform, 0, len(AllPlatforms))
	for _, p := range AllPlatforms {
		out = append(out, DetectPlatform(ctx, host, p))
	}
	return out
}

// RedactPlatform strips secret values (env values, git tokens, service passwords)
// while preserving keys, counts, domains, volumes, and log metadata for safe API responses.
func RedactPlatform(dp DetectedPlatform) DetectedPlatform {
	out := dp
	out.Projects = make([]DiscoveredProject, len(dp.Projects))
	for i, p := range dp.Projects {
		p.Env = nil
		p.GitToken = ""
		if len(p.Logs) > 0 {
			trimmed := make([]DiscoveredLog, len(p.Logs))
			for j, l := range p.Logs {
				l.Content = ""
				trimmed[j] = l
			}
			p.Logs = trimmed
		}
		out.Projects[i] = p
	}
	out.Services = make([]DiscoveredService, len(dp.Services))
	for i, s := range dp.Services {
		s.Config = nil
		out.Services[i] = s
	}
	return out
}
