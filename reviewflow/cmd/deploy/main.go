// Command deploy updates the ReviewFlow stack on the VPS.
//
// Install (one-time, or after changing this file):
//
//	cd /opt/reviewflow/repo/reviewflow
//	docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp \
//	    -v "$PWD":/src -w /src golang:1.26-alpine \
//	    go build -trimpath -o /src/deploy ./cmd/deploy
//	mv deploy /opt/reviewflow/deploy
//
// Usage: /opt/reviewflow/deploy
//
// Steps, in order:
//  1. git pull --ff-only in the repo clone
//  2. docker compose up -d --build backend
//  3. docker image prune, filtered to this compose project's label
//  4. poll the loopback health URL until 200 or timeout
//
// Any failing step aborts with a non-zero exit, so it's safe to chain
// (e.g. from CI over SSH) later.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	stackDir = "/opt/reviewflow"
	repoDir  = "/opt/reviewflow/repo"
	service  = "backend"

	// Compose project name (the `name:` key in docker-compose.yml); used to
	// prune only this project's dangling images.
	composeProject = "reviewflow"

	healthURL = "http://127.0.0.1:8092/health"

	// The distroless image has no container healthcheck, so this poll is the
	// authoritative "deploy worked" signal. 90s is generous: the compose
	// dependency gate already waited for Mongo before starting the backend.
	healthTimeout = 90 * time.Second
	pollEvery     = 2 * time.Second
)

func main() {
	start := time.Now()

	oldRev := gitRev()
	step("git pull --ff-only")
	run(repoDir, "git", "pull", "--ff-only")

	newRev := gitRev()
	if oldRev == newRev {
		fmt.Printf("no new commits (still at %s) — rebuilding anyway\n", short(oldRev))
	} else {
		fmt.Printf("updated %s -> %s\n", short(oldRev), short(newRev))
	}

	step("docker compose up -d --build " + service)
	run(stackDir, "docker", "compose", "up", "-d", "--build", service)

	step("prune dangling images from this project")
	run(stackDir, "docker", "image", "prune", "-f",
		"--filter", "label=com.docker.compose.project="+composeProject)

	step("poll " + healthURL)
	waitHealthy()

	fmt.Printf("\ndeploy OK in %s (running %s)\n",
		time.Since(start).Round(time.Second), short(newRev))
}

// step prints a section header so the output reads as a checklist.
func step(name string) {
	fmt.Printf("\n==> %s\n", name)
}

// run executes a command in dir, streaming its output; any failure is fatal.
func run(dir string, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "deploy: %s %s: %v\n", name, strings.Join(args, " "), err)
		os.Exit(1)
	}
}

// gitRev returns the repo's current HEAD, or "unknown" if git fails (the
// deploy should not abort just because the rev can't be read).
func gitRev() string {
	out, err := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// short abbreviates a commit hash for display.
func short(rev string) string {
	if len(rev) > 10 {
		return rev[:10]
	}
	return rev
}

// waitHealthy polls healthURL until it returns 200 (which requires a live
// Mongo ping server-side) or the timeout elapses.
func waitHealthy() {
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(healthTimeout)

	var lastErr string
	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if err != nil {
			lastErr = err.Error()
		} else {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				fmt.Printf("healthy: %s\n", strings.TrimSpace(string(body)))
				return
			}
			lastErr = fmt.Sprintf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		time.Sleep(pollEvery)
	}

	fmt.Fprintf(os.Stderr, "deploy: backend not healthy after %s (last: %s)\n", healthTimeout, lastErr)
	fmt.Fprintln(os.Stderr, "inspect with: docker compose -f "+stackDir+"/docker-compose.yml logs "+service)
	os.Exit(1)
}
