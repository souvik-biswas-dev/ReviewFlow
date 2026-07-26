#!/usr/bin/env node
//
// Updates the ReviewFlow stack on the VPS.
//
// Install (one-time, or after changing this file):
//
//     cp /opt/reviewflow/repo/reviewflow/server/scripts/deploy.mjs /opt/reviewflow/deploy.mjs
//
// It has no dependencies and is not compiled — only Node itself is required.
//
// Usage: node /opt/reviewflow/deploy.mjs
//
// Steps, in order:
//  1. git pull --ff-only in the repo clone
//  2. docker compose up -d --build backend
//  3. docker image prune, filtered to this compose project's label
//  4. poll the loopback health URL until 200 or timeout
//
// Any failing step aborts with a non-zero exit, so it's safe to chain
// (e.g. from CI over SSH) later.

import { execFileSync, spawnSync } from 'node:child_process';
import { setTimeout as sleep } from 'node:timers/promises';

const STACK_DIR = '/opt/reviewflow';
const REPO_DIR = '/opt/reviewflow/repo';
const SERVICE = 'backend';

// Compose project name (the `name:` key in docker-compose.prod.yml); used to
// prune only this project's dangling images.
const COMPOSE_PROJECT = 'reviewflow';

const HEALTH_URL = 'http://127.0.0.1:8092/health';

// The container has no healthcheck, so this poll is the authoritative "deploy
// worked" signal. 90s is generous: the compose dependency gate already waited
// for Mongo before starting the backend.
const HEALTH_TIMEOUT_MS = 90_000;
const POLL_EVERY_MS = 2_000;

const started = Date.now();

const oldRev = gitRev();
step('git pull --ff-only');
run(REPO_DIR, 'git', ['pull', '--ff-only']);

const newRev = gitRev();
if (oldRev === newRev) {
  console.log(`no new commits (still at ${short(oldRev)}) — rebuilding anyway`);
} else {
  console.log(`updated ${short(oldRev)} -> ${short(newRev)}`);
}

step(`docker compose up -d --build ${SERVICE}`);
run(STACK_DIR, 'docker', ['compose', 'up', '-d', '--build', SERVICE]);

step('prune dangling images from this project');
run(STACK_DIR, 'docker', [
  'image',
  'prune',
  '-f',
  '--filter',
  `label=com.docker.compose.project=${COMPOSE_PROJECT}`,
]);

step(`poll ${HEALTH_URL}`);
await waitHealthy();

const elapsed = Math.round((Date.now() - started) / 1000);
console.log(`\ndeploy OK in ${elapsed}s (running ${short(newRev)})`);

/** Prints a section header so the output reads as a checklist. */
function step(name) {
  console.log(`\n==> ${name}`);
}

/** Executes a command in dir, streaming its output; any failure is fatal. */
function run(dir, command, args) {
  const result = spawnSync(command, args, { cwd: dir, stdio: 'inherit' });
  if (result.error || result.status !== 0) {
    console.error(`deploy: ${command} ${args.join(' ')}: ${result.error ?? `exit ${result.status}`}`);
    process.exit(1);
  }
}

/**
 * The repo's current HEAD, or "unknown" if git fails (the deploy should not
 * abort just because the rev can't be read).
 */
function gitRev() {
  try {
    return execFileSync('git', ['-C', REPO_DIR, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim();
  } catch {
    return 'unknown';
  }
}

/** Abbreviates a commit hash for display. */
function short(rev) {
  return rev.length > 10 ? rev.slice(0, 10) : rev;
}

/**
 * Polls HEALTH_URL until it returns 200 (which requires a live Mongo ping
 * server-side) or the timeout elapses.
 */
async function waitHealthy() {
  const deadline = Date.now() + HEALTH_TIMEOUT_MS;
  let lastErr = '';

  while (Date.now() < deadline) {
    try {
      const resp = await fetch(HEALTH_URL, { signal: AbortSignal.timeout(3_000) });
      const body = (await resp.text()).slice(0, 512).trim();
      if (resp.ok) {
        console.log(`healthy: ${body}`);
        return;
      }
      lastErr = `status ${resp.status}: ${body}`;
    } catch (err) {
      lastErr = err instanceof Error ? err.message : String(err);
    }
    await sleep(POLL_EVERY_MS);
  }

  console.error(`deploy: backend not healthy after ${HEALTH_TIMEOUT_MS / 1000}s (last: ${lastErr})`);
  console.error(`inspect with: docker compose -f ${STACK_DIR}/docker-compose.yml logs ${SERVICE}`);
  process.exit(1);
}
