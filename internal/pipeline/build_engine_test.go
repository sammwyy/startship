package pipeline

import "testing"

func TestBuildEngineValidation(t *testing.T) {
	for _, engine := range []string{"", "docker", "nixpacks", "buildpacks", "unknown"} {
		t.Run(engine, func(t *testing.T) {
			p := Pipeline{Build: Build{Engine: engine, Image: "node:22-alpine"}}
			if err := p.Validate(); (err != nil) != (engine == "unknown") {
				t.Fatal(err)
			}
			p.Build.Image = ""
			if err := p.Validate(); (err != nil) != (engine != "nixpacks" && engine != "buildpacks") {
				t.Fatal(err)
			}
		})
	}
	for _, engine := range []string{"nixpacks", "buildpacks"} {
		p := Pipeline{Build: Build{Engine: engine, Image: "ignored image", Dockerfile: "../ignored", Commands: []string{"", "ignored\ncommand"}}}
		if err := p.Validate(); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Dockerfile(); err == nil {
			t.Fatal("generated Dockerfile for external engine")
		}
		p.Test.Commands = []string{""}
		if err := p.Validate(); err == nil {
			t.Fatal("invalid test was ignored")
		}
		p.Test.Commands = nil
		p.Deploy.Artifact = "../escape"
		if err := p.Validate(); err == nil {
			t.Fatal("unsafe deploy artifact was ignored")
		}
	}
}
