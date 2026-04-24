package config

// MergeSandbox returns the effective SandboxConfig for a project by merging
// user-scope and project-scope settings. Merge rules:
//   - project == nil: return user unchanged
//   - Mode, Docker.Image, Docker.Network (scalars): project wins when non-empty
//   - Docker.ExtraArgs, ExtraMounts, ForwardEnv (lists): user + project concat
//   - Docker.Env (map): user keys as base, project keys overwrite
func MergeSandbox(user SandboxConfig, project *SandboxConfig) SandboxConfig {
	if project == nil {
		return user
	}
	out := SandboxConfig{
		Mode: user.Mode,
		Docker: DockerConfig{
			Image:       user.Docker.Image,
			Network:     user.Docker.Network,
			ExtraArgs:   appendSlice(user.Docker.ExtraArgs, project.Docker.ExtraArgs),
			ExtraMounts: appendSlice(user.Docker.ExtraMounts, project.Docker.ExtraMounts),
			ForwardEnv:  appendSlice(user.Docker.ForwardEnv, project.Docker.ForwardEnv),
			Env:         mergeMaps(user.Docker.Env, project.Docker.Env),
		},
	}
	if project.Mode != "" {
		out.Mode = project.Mode
	}
	if project.Docker.Image != "" {
		out.Docker.Image = project.Docker.Image
	}
	if project.Docker.Network != "" {
		out.Docker.Network = project.Docker.Network
	}
	return out
}

func appendSlice(base, extra []string) []string {
	if len(extra) == 0 {
		return append([]string(nil), base...)
	}
	return append(append([]string(nil), base...), extra...)
}

func mergeMaps(base, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}
