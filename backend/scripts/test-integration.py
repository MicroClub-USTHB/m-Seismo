#!/usr/bin/env python3
"""Run unit, HTTP and persistence tests using disposable local services.

Requires Python 3, Docker, Go (go.mod version), and Node 20+. Uses fake GitHub
and a local TLS Sentry stub; no external vendor credentials are required.
"""

import argparse
import base64
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.request
import uuid


ROOT = Path(__file__).resolve().parents[2]
BACKEND = ROOT / "backend"


def run(command, *, env=None, cwd=BACKEND, capture=False, input=None):
    try:
        return subprocess.run(
            command, cwd=cwd, env=env, input=input, text=True, check=True,
            stdout=subprocess.PIPE if capture else None,
            stderr=subprocess.PIPE if capture else None,
        )
    except subprocess.CalledProcessError as error:
        if capture and error.stderr:
            print(error.stderr, flush=True)
        raise


def wait_until(check, description):
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        try:
            if check():
                return
        except (OSError, subprocess.SubprocessError):
            pass
        time.sleep(0.2)
    raise RuntimeError("Timed out waiting for " + description)


def free_port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def healthy(url):
    with urllib.request.urlopen(url, timeout=1) as response:
        return response.status == 200


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--postgres-image", default="postgres:17-alpine")
    parser.add_argument("--redis-image", default="redis:7-alpine")
    parser.add_argument("--nats-image", default="nats:2-alpine")
    parser.add_argument("--go", default="go", help="Go executable (default: go on PATH)")
    args = parser.parse_args()
    for command in ("docker", args.go, "node", "bash", "curl"):
        if shutil.which(command) is None:
            parser.error(command + " must be on PATH")
    version = run(["node", "-p", "process.versions.node"], capture=True).stdout.strip()
    if int(version.split(".")[0]) < 20:
        parser.error("Node 20+ is required")

    # Never inherit a developer's real service/provider credentials or target
    # their running database. All endpoints below are allocated for this run.
    env = {
        key: value for key, value in os.environ.items()
        if not key.startswith(("SEISMO_", "FAKEGITHUB_"))
    }
    prefix = "m-seismo-test-" + uuid.uuid4().hex[:10]
    containers, processes, logs = [], [], []
    with tempfile.TemporaryDirectory(prefix=prefix + "-") as directory:
        temporary = Path(directory)
        try:
            def start_service(service, image, port, flags=(), command=()):
                name = prefix + "-" + service
                containers.append(name)
                run([
                    "docker", "run", "--detach", "--rm", "--name", name,
                    "-p", "127.0.0.1::" + str(port), *flags, image, *command,
                ], capture=True)
                info = json.loads(run(["docker", "inspect", name], capture=True).stdout)[0]
                published = info["NetworkSettings"]["Ports"][str(port) + "/tcp"][0]["HostPort"]
                return name, published

            postgres, pg_port = start_service("postgres", args.postgres_image, 5432, (
                "-e", "POSTGRES_USER=seismo", "-e", "POSTGRES_PASSWORD=seismo", "-e", "POSTGRES_DB=seismo",
            ))
            redis, redis_port = start_service("redis", args.redis_image, 6379)
            nats, nats_port = start_service("nats", args.nats_image, 4222, command=("-js", "-m", "8222"))
            # The image starts a temporary Unix-socket-only server during
            # initialization. Wait for the final TCP listener, not that server.
            wait_until(lambda: run(["docker", "exec", postgres, "pg_isready", "-h", "127.0.0.1", "-U", "seismo", "-d", "seismo"], capture=True).returncode == 0, "Postgres")
            wait_until(lambda: run(["docker", "exec", redis, "redis-cli", "ping"], capture=True).stdout.strip() == "PONG", "Redis")
            wait_until(lambda: run(["docker", "exec", nats, "wget", "-qO-", "http://127.0.0.1:8222/healthz"], capture=True).returncode == 0, "NATS")
            for migration in sorted((BACKEND / "db/migrations").glob("*.sql")):
                sql = migration.read_text().split("-- migrate:down")[0]
                run(["docker", "exec", "-i", postgres, "psql", "-U", "seismo", "-d", "seismo", "-v", "ON_ERROR_STOP=1", "-q"], input="BEGIN;\n" + sql + "\nCOMMIT;", capture=True)
            env.update({
                "SEISMO_TEST_DATABASE_URL": f"postgres://seismo:seismo@127.0.0.1:{pg_port}/seismo?sslmode=disable",
                "SEISMO_TEST_REDIS_URL": f"redis://127.0.0.1:{redis_port}/0",
                "SEISMO_TEST_NATS_URL": f"nats://127.0.0.1:{nats_port}",
            })
            print("Running all Go tests with race detection and real isolated services", flush=True)
            # NATS integration tests share a work-queue stream. Packages must
            # run serially so one test's consumer cannot steal another's work.
            run([args.go, "test", "-p", "1", "-count=1", "-race", "-tags", "integration", "./..."], env=env)
            run([args.go, "vet", "-tags", "integration", "./..."], env=env)
            run(["node", "--test"], env=env, cwd=ROOT / "sdks/web")

            print("Running existing HTTP API regression scripts against fake GitHub", flush=True)
            run([args.go, "build", "-o", str(temporary / "seismo"), "."], env=env)
            run([args.go, "build", "-o", str(temporary / "fakegithub"), "./cmd/fakegithub"], env=env)

            def start_process(name, command, process_env):
                log = temporary / (name + ".log")
                logs.append(log)
                with log.open("w") as output:
                    processes.append(subprocess.Popen(command, cwd=BACKEND, env=process_env, stdout=output, stderr=subprocess.STDOUT))

            github_port, backend_port = free_port(), free_port()
            while backend_port == github_port:
                backend_port = free_port()
            github_url, backend_url = f"http://127.0.0.1:{github_port}", f"http://127.0.0.1:{backend_port}"
            key = temporary / "github.pem"
            start_process("fakegithub", [str(temporary / "fakegithub")], {
                **env, "FAKEGITHUB_ADDR": f"127.0.0.1:{github_port}",
                "FAKEGITHUB_PUBLIC_URL": github_url, "FAKEGITHUB_KEY_PATH": str(key),
            })
            wait_until(lambda: healthy(github_url + "/healthz"), "fake GitHub")
            start_process("backend", [str(temporary / "seismo")], {
                **env, "SEISMO_ENV": "development", "SEISMO_AUTH_JWT_SECRET": "integration-only-jwt-secret",
                "SEISMO_DATABASE_URL": env["SEISMO_TEST_DATABASE_URL"],
                "SEISMO_REDIS_URL": env["SEISMO_TEST_REDIS_URL"], "SEISMO_NATS_URL": env["SEISMO_TEST_NATS_URL"],
                "SEISMO_SERVER_HOST": "127.0.0.1", "SEISMO_SERVER_PORT": str(backend_port),
                "SEISMO_SERVER_PUBLIC_URL": backend_url,
                "SEISMO_CRASH_PROVIDERS_ENCRYPTION_KEY": base64.b64encode(os.urandom(32)).decode(),
                "SEISMO_GITHUB_APP_ID": "1", "SEISMO_GITHUB_APP_SLUG": "m-seismo-dev",
                "SEISMO_GITHUB_CLIENT_ID": "fake-client", "SEISMO_GITHUB_CLIENT_SECRET": "fake-secret",
                "SEISMO_GITHUB_PRIVATE_KEY_PATH": str(key),
                "SEISMO_GITHUB_API_URL": github_url + "/", "SEISMO_GITHUB_WEB_URL": github_url + "/",
            })
            wait_until(lambda: healthy(backend_url + "/healthz"), "backend")
            run(["bash", str(BACKEND / "scripts/api/all.sh"), "--fresh"], env={
                **env, "API_URL": backend_url + "/api/v1", "FAKE_GITHUB_URL": github_url,
                "GITHUB_MODE": "fake", "GITHUB_TEST_REPO": "octo-org/seismo-test",
                "STATE_FILE": str(temporary / "api-state.env"), "VERBOSE": "0",
            })
            print("All Go, web, persistence, Sentry HTTP and API regression tests passed", flush=True)
        except BaseException:
            for log in logs:
                print("\nLast lines of " + log.name, flush=True)
                print("\n".join(log.read_text().splitlines()[-20:]), flush=True)
            raise
        finally:
            for process in reversed(processes):
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)
            for name in reversed(containers):
                subprocess.run(["docker", "rm", "--force", name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


if __name__ == "__main__":
    main()
