package config

import (
	"testing"
)

func TestMergeSandbox_NilProject(t *testing.T) {
	user := SandboxConfig{Mode: "docker", Docker: DockerConfig{Image: "node:22"}}
	got := MergeSandbox(user, nil)
	if got.Mode != "docker" {
		t.Errorf("Mode = %q, want docker", got.Mode)
	}
	if got.Docker.Image != "node:22" {
		t.Errorf("Image = %q, want node:22", got.Docker.Image)
	}
}

func TestMergeSandbox_ModeOverride(t *testing.T) {
	user := SandboxConfig{Mode: "docker"}
	project := &SandboxConfig{Mode: "direct"}
	got := MergeSandbox(user, project)
	if got.Mode != "direct" {
		t.Errorf("Mode = %q, want direct (project wins)", got.Mode)
	}
}

func TestMergeSandbox_ModeEmpty_UserWins(t *testing.T) {
	user := SandboxConfig{Mode: "docker"}
	project := &SandboxConfig{} // no mode set
	got := MergeSandbox(user, project)
	if got.Mode != "docker" {
		t.Errorf("Mode = %q, want docker (project empty, user wins)", got.Mode)
	}
}

func TestMergeSandbox_ImageOverride(t *testing.T) {
	user := SandboxConfig{Docker: DockerConfig{Image: "node:22"}}
	project := &SandboxConfig{Docker: DockerConfig{Image: "alpine:latest"}}
	got := MergeSandbox(user, project)
	if got.Docker.Image != "alpine:latest" {
		t.Errorf("Image = %q, want alpine:latest (project wins)", got.Docker.Image)
	}
}

func TestMergeSandbox_ImageEmpty_UserWins(t *testing.T) {
	user := SandboxConfig{Docker: DockerConfig{Image: "node:22"}}
	project := &SandboxConfig{} // no image
	got := MergeSandbox(user, project)
	if got.Docker.Image != "node:22" {
		t.Errorf("Image = %q, want node:22 (project empty, user wins)", got.Docker.Image)
	}
}

func TestMergeSandbox_ListConcat(t *testing.T) {
	user := SandboxConfig{
		Docker: DockerConfig{
			ExtraArgs:   []string{"--read-only"},
			ExtraMounts: []string{"/a:/a:ro"},
			ForwardEnv:  []string{"API_KEY"},
		},
	}
	project := &SandboxConfig{
		Docker: DockerConfig{
			ExtraArgs:   []string{"--shm-size=256m"},
			ExtraMounts: []string{"~/.claude:/home/user/.claude:rw"},
			ForwardEnv:  []string{"DB_URL"},
		},
	}
	got := MergeSandbox(user, project)
	if len(got.Docker.ExtraArgs) != 2 {
		t.Errorf("ExtraArgs = %v, want 2 items (base+project)", got.Docker.ExtraArgs)
	}
	if len(got.Docker.ExtraMounts) != 2 {
		t.Errorf("ExtraMounts = %v, want 2 items (base+project)", got.Docker.ExtraMounts)
	}
	if len(got.Docker.ForwardEnv) != 2 {
		t.Errorf("ForwardEnv = %v, want 2 items (base+project)", got.Docker.ForwardEnv)
	}
}

func TestMergeSandbox_ListConcat_ProjectEmpty(t *testing.T) {
	user := SandboxConfig{Docker: DockerConfig{ForwardEnv: []string{"API_KEY"}}}
	project := &SandboxConfig{}
	got := MergeSandbox(user, project)
	if len(got.Docker.ForwardEnv) != 1 || got.Docker.ForwardEnv[0] != "API_KEY" {
		t.Errorf("ForwardEnv = %v, want [API_KEY]", got.Docker.ForwardEnv)
	}
}

func TestMergeSandbox_EnvMapMerge(t *testing.T) {
	user := SandboxConfig{Docker: DockerConfig{Env: map[string]string{"HOME": "/home/user", "FOO": "base"}}}
	project := &SandboxConfig{Docker: DockerConfig{Env: map[string]string{"FOO": "project", "BAR": "added"}}}
	got := MergeSandbox(user, project)
	if got.Docker.Env["HOME"] != "/home/user" {
		t.Errorf("Env[HOME] = %q, want /home/user (base survives)", got.Docker.Env["HOME"])
	}
	if got.Docker.Env["FOO"] != "project" {
		t.Errorf("Env[FOO] = %q, want project (project wins)", got.Docker.Env["FOO"])
	}
	if got.Docker.Env["BAR"] != "added" {
		t.Errorf("Env[BAR] = %q, want added (project adds key)", got.Docker.Env["BAR"])
	}
}

func TestMergeSandbox_EnvMap_BothNil(t *testing.T) {
	user := SandboxConfig{}
	project := &SandboxConfig{}
	got := MergeSandbox(user, project)
	if got.Docker.Env != nil {
		t.Errorf("Env = %v, want nil (both nil)", got.Docker.Env)
	}
}

func TestMergeSandbox_NetworkOverride(t *testing.T) {
	user := SandboxConfig{Docker: DockerConfig{Network: "roost-sandbox"}}
	project := &SandboxConfig{Docker: DockerConfig{Network: "project-net"}}
	got := MergeSandbox(user, project)
	if got.Docker.Network != "project-net" {
		t.Errorf("Network = %q, want project-net", got.Docker.Network)
	}
}

func TestMergeSandbox_DoesNotMutateInput(t *testing.T) {
	user := SandboxConfig{Docker: DockerConfig{ForwardEnv: []string{"KEY_A"}}}
	project := &SandboxConfig{Docker: DockerConfig{ForwardEnv: []string{"KEY_B"}}}
	got := MergeSandbox(user, project)
	got.Docker.ForwardEnv = append(got.Docker.ForwardEnv, "KEY_C")
	if len(user.Docker.ForwardEnv) != 1 {
		t.Errorf("user ForwardEnv mutated: %v", user.Docker.ForwardEnv)
	}
	if len(project.Docker.ForwardEnv) != 1 {
		t.Errorf("project ForwardEnv mutated: %v", project.Docker.ForwardEnv)
	}
}
